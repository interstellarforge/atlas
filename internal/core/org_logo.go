package core

import (
	"context"
	"database/sql"
	"errors"
	"io"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/media"
)

// Organization logo (ADR-0031 point 9). The image goes through the upload pipeline (limits, magic bytes, pixel
// count) and only a re-encoded thumbnail is ever served, which drops metadata. Only the organization's ADMIN
// changes it; its members (and the Atlas admin, who sees the organization's name anyway) can read it.

// SetOrgLogo stores the image as the logo of the organization.
func (s *Service) SetOrgLogo(ctx context.Context, a auth.Actor, orgKey, filename string, r io.Reader) error {
	if s.media == nil {
		return rule("uploads are not enabled")
	}
	if _, err := s.adminOfOrg(s.db.R, a, orgKey); err != nil { // before the upload is read
		return err
	}
	saved, err := s.media.Save(r, cleanFilename(filename))
	switch {
	case errors.Is(err, media.ErrTooLarge):
		return ErrTooLarge
	case errors.Is(err, media.ErrType), errors.Is(err, media.ErrCorrupt):
		return invalid("%s", err.Error())
	case err != nil:
		return err
	}
	if _, err := s.media.Thumb(saved.Path); err != nil { // also proves the image decodes
		s.media.RemoveAll(saved.Path)
		return invalid("%s", err.Error())
	}
	var old sql.NullString
	err = s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, orgKey)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT logo_path FROM organization_settings WHERE organization_id = ?`, o.ID).Scan(&old); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO organization_settings (organization_id, logo_path) VALUES (?, ?)
			ON CONFLICT (organization_id) DO UPDATE SET logo_path = excluded.logo_path`, o.ID, saved.Path); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "org.logo", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"set": true}})
	})
	if err != nil {
		s.media.RemoveAll(saved.Path)
		return err
	}
	if old.Valid {
		s.media.RemoveAll(old.String)
	}
	return nil
}

func (s *Service) RemoveOrgLogo(ctx context.Context, a auth.Actor, orgKey string) error {
	var old sql.NullString
	err := s.tx(func(tx *sql.Tx) error {
		o, err := s.adminOfOrg(tx, a, orgKey)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT logo_path FROM organization_settings WHERE organization_id = ?`, o.ID).Scan(&old); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(`UPDATE organization_settings SET logo_path = NULL WHERE organization_id = ?`, o.ID); err != nil {
			return err
		}
		return s.journal(ctx, tx, a, change{Operation: "org.logo", Target: orgTarget(o.Key), Event: "admin.updated", Changes: map[string]any{"set": false}})
	})
	if err == nil && old.Valid && s.media != nil {
		s.media.RemoveAll(old.String)
	}
	return err
}

// OrgLogoThumb returns the thumbnail path of the logo for anyone who may see the organization.
func (s *Service) OrgLogoThumb(ctx context.Context, a auth.Actor, orgKey string) (string, error) {
	if s.media == nil {
		return "", ErrNotFound
	}
	o, err := s.authorizeOrg(s.db.R, a, orgKey, orgView)
	if err != nil {
		return "", err
	}
	var p sql.NullString
	if err := s.db.R.QueryRow(`SELECT logo_path FROM organization_settings WHERE organization_id = ?`, o.ID).Scan(&p); err != nil || !p.Valid {
		return "", ErrNotFound
	}
	return s.media.Thumb(p.String)
}
