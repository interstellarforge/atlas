// Package db owns SQLite connections and migrations.
//
// Profile: WAL, foreign keys on, busy timeout 5 s, synchronous FULL (NORMAL is an
// opt-in). One serialized write connection (BEGIN IMMEDIATE) and a small read pool.
package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const readPoolSize = 4

type Options struct {
	// Synchronous is "FULL" (default) or "NORMAL".
	Synchronous string
}

type DB struct {
	// W is the single write connection; its transactions start with BEGIN IMMEDIATE.
	W *sql.DB
	// R is a small pool of read-only connections.
	R    *sql.DB
	Path string
}

func Open(path string, o Options) (*DB, error) {
	if o.Synchronous == "" {
		o.Synchronous = "FULL"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	w, err := sql.Open("sqlite", dsn(abs, o.Synchronous, true))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := w.Ping(); err != nil {
		w.Close()
		return nil, fmt.Errorf("open write connection: %w", err)
	}

	r, err := sql.Open("sqlite", dsn(abs, o.Synchronous, false))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(readPoolSize)
	if err := r.Ping(); err != nil {
		w.Close()
		r.Close()
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	return &DB{W: w, R: r, Path: abs}, nil
}

func (d *DB) Close() error {
	werr := d.W.Close()
	rerr := d.R.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

// dsn applies the pragmas to every connection the driver opens.
func dsn(path, synchronous string, write bool) string {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous("+synchronous+")")
	if write {
		q.Set("_txlock", "immediate")
	} else {
		q.Add("_pragma", "query_only(1)")
	}
	return "file:" + path + "?" + q.Encode()
}
