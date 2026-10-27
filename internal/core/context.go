package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/kees/atlas/internal/auth"
)

// Project context (ADR-0015): compact, curated knowledge per project for people and agents.

const (
	maxContextTitle   = 160
	maxContextSummary = 1000
	maxContextBody    = 12 * 1024 // a detail answer must fit the 16 KiB MCP result limit
	maxContextSources = 10
	maxSourceNote     = 200
	maxSourceText     = 200
	snippetRunes      = 160
	contextWarnAt     = 200 // active entries; above this bootstrap asks for pruning
	contextMaxBytes   = 16 * 1024
	contextMinBytes   = 1024
	contextDefBytes   = 4096
	maxReferences     = 20
)

// ContextCategories is the fixed category list in bootstrap order.
var ContextCategories = []string{"architecture", "decision", "convention", "pitfall", "dependency", "reference"}

var (
	contextKeyRe    = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
	commitSHARe     = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	contentHashRe   = regexp.MustCompile(`^[A-Za-z0-9:_.+/=-]{1,128}$`)
	contextReserved = []string{"bootstrap", "search"}
	secretFiles     = regexp.MustCompile(`(?i)(^|/)(\.env[^/]*|id_(rsa|dsa|ecdsa|ed25519)[^/]*|[^/]*\.(pem|key|p12|pfx|jks|keystore)|\.netrc|\.npmrc|credentials[^/]*|secrets?\.[^/]*)$`)
	contextFields   = map[string]bool{"key": true, "category": true, "title": true, "summary": true, "body": true, "status": true,
		"superseded_by": true, "verification_state": true, "source_kind": true, "pinned": true, "version": true, "created_by": true,
		"updated_by": true, "created_at": true, "updated_at": true, "verified_at": true, "verified_by": true, "sources": true}
)

// ContextSource points at where a claim comes from. Only the fields of its type are used.
type ContextSource struct {
	Type        string `json:"type"` // file, ticket, url or adr
	Repository  string `json:"repository,omitempty"`
	Path        string `json:"path,omitempty"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	Ticket      string `json:"ticket,omitempty"`
	URL         string `json:"url,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	Note        string `json:"note,omitempty"`
	VerifiedAt  *int64 `json:"verified_at,omitempty"`
}

// ref is the short form agents see: file:docs/security.md, ticket:EXO-4.
func (c ContextSource) ref() string {
	switch c.Type {
	case "ticket":
		return "ticket:" + c.Ticket
	case "url":
		return "url:" + c.URL
	}
	return c.Type + ":" + c.Path
}

// identity says whether two sources point at the same thing, whatever their hash.
func (c ContextSource) identity() string {
	return strings.Join([]string{c.Type, c.Repository, c.Path, c.Ticket, c.URL}, "|")
}

type ContextEntry struct {
	ID           int64           `json:"-"`
	Key          string          `json:"key"`
	Category     string          `json:"category"`
	Title        string          `json:"title"`
	Summary      string          `json:"summary"`
	Body         string          `json:"body,omitempty"`
	Status       string          `json:"status"`
	SupersededBy string          `json:"superseded_by,omitempty"`
	Verification string          `json:"verification_state"`
	SourceKind   string          `json:"source_kind"`
	Pinned       bool            `json:"pinned"`
	Version      int64           `json:"version"`
	CreatedBy    string          `json:"created_by"`
	UpdatedBy    string          `json:"updated_by"`
	CreatedAt    int64           `json:"created_at"`
	UpdatedAt    int64           `json:"updated_at"`
	VerifiedAt   *int64          `json:"verified_at"`
	VerifiedBy   string          `json:"verified_by,omitempty"`
	Sources      []ContextSource `json:"sources"`
}

// ContextInput creates or changes an entry. Nil fields stay as they are; creating needs category, title and summary.
type ContextInput struct {
	Key             string           `json:"key,omitempty"`
	Category        *string          `json:"category,omitempty"`
	Title           *string          `json:"title,omitempty"`
	Summary         *string          `json:"summary,omitempty"`
	Body            *string          `json:"body,omitempty"`
	Pinned          *bool            `json:"pinned,omitempty"`
	Sources         *[]ContextSource `json:"sources,omitempty"`
	ExpectedVersion int64            `json:"expected_version,omitempty"`
	IdempotencyKey  string           `json:"-"`
}

// ContextResult is what every context write returns.
type ContextResult struct {
	Entry          ContextEntry `json:"entry"`
	ContextVersion int64        `json:"context_version"`
	// Replayed is set when an earlier result for the same idempotency key was returned.
	Replayed bool `json:"replayed,omitempty"`
}

// ContextFilter selects entries. Items are paged by offset cursor and fitted to MaxBytes.
type ContextFilter struct {
	Categories []string
	Status     string // active (default), superseded, archived or all
	Pinned     *bool
	Q          string
	Keys       []string
	Fields     []string
	Compact    bool // key, category, title, snippet, state and refs only (search)
	Cursor     string
	Limit      int
	MaxBytes   int // 0: no byte budget
	WithBody   bool
}

type ContextPage struct {
	Project        string           `json:"project"`
	ContextVersion int64            `json:"context_version"`
	Items          []map[string]any `json:"items"`
	Warnings       []string         `json:"warnings,omitempty"`
	NextCursor     string           `json:"next_cursor,omitempty"`
}

// ContextBootstrap is the compact starting picture of a project: pinned active entries without bodies.
type ContextBootstrap struct {
	Project        string           `json:"project"`
	ContextVersion int64            `json:"context_version"`
	Entries        []map[string]any `json:"entries"`
	References     []string         `json:"references,omitempty"`
	Warnings       []string         `json:"warnings,omitempty"`
	NextCursor     string           `json:"next_cursor,omitempty"`
}

// ---- validation ----

func contextKey(v string) error {
	if !contextKeyRe.MatchString(v) {
		return invalid("key: lowercase letters, digits and -, starting with a letter, 2 to 64 characters")
	}
	if slices.Contains(contextReserved, v) {
		return invalid("key %q is reserved", v)
	}
	return nil
}

func validSourcePath(p string) error {
	switch {
	case p == "" || len(p) > maxSourceText:
		return invalid("source path is required, at most %d characters", maxSourceText)
	case strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") || (len(p) > 1 && p[1] == ':'):
		return invalid("source path must be relative to the repository")
	case slices.Contains(strings.Split(p, "/"), ".."):
		return invalid("source path cannot contain ..")
	case secretFiles.MatchString(path.Clean(p)):
		return invalid("source path points at a file that may hold secrets")
	}
	return nil
}

func validateSources(list []ContextSource) ([]ContextSource, error) {
	if len(list) > maxContextSources {
		return nil, invalid("at most %d sources per entry", maxContextSources)
	}
	out := make([]ContextSource, 0, len(list))
	for _, c := range list {
		n := ContextSource{Type: c.Type, Repository: strings.TrimSpace(c.Repository), CommitSHA: strings.ToLower(strings.TrimSpace(c.CommitSHA)),
			ContentHash: strings.TrimSpace(c.ContentHash), Note: strings.TrimSpace(c.Note)}
		if len(n.Note) > maxSourceNote || len(n.Repository) > maxSourceText {
			return nil, invalid("source note is at most %d characters, repository at most %d", maxSourceNote, maxSourceText)
		}
		if n.CommitSHA != "" && !commitSHARe.MatchString(n.CommitSHA) {
			return nil, invalid("commit_sha must be 7 to 64 hex characters")
		}
		if n.ContentHash != "" && !contentHashRe.MatchString(n.ContentHash) {
			return nil, invalid("content_hash is at most 128 characters of letters, digits and : _ . + / = -")
		}
		switch c.Type {
		case "file", "adr":
			if err := validSourcePath(strings.TrimSpace(c.Path)); err != nil {
				return nil, err
			}
			n.Path = path.Clean(strings.TrimSpace(c.Path))
		case "ticket":
			t := strings.ToUpper(strings.TrimSpace(c.Ticket))
			if _, err := projectOfKey(t); err != nil {
				return nil, invalid("ticket must be an item key such as EXO-12")
			}
			n.Ticket = t
		case "url":
			u := strings.TrimSpace(c.URL)
			if !strings.HasPrefix(u, "https://") || len(u) > 500 || strings.ContainsAny(u, " \t\r\n") {
				return nil, invalid("url must be an https:// address of at most 500 characters")
			}
			n.URL = u
		default:
			return nil, invalid("source type must be file, ticket, url or adr")
		}
		out = append(out, n)
	}
	return out, nil
}

// sourceKind is derived, never supplied: a caller cannot claim a source it did not give.
func sourceKind(list []ContextSource) string {
	kinds := map[string]bool{}
	for _, c := range list {
		switch c.Type {
		case "file", "adr":
			kinds["repository"] = true
		case "ticket":
			kinds["ticket"] = true
		default:
			kinds["manual"] = true
		}
	}
	switch {
	case len(kinds) > 1:
		return "derived"
	case kinds["repository"]:
		return "repository"
	case kinds["ticket"]:
		return "ticket"
	}
	return "manual"
}

func slugify(title string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if len(s) < 2 || s[0] < 'a' || s[0] > 'z' {
		s = "entry-" + s
	}
	return strings.TrimSuffix(s, "-")
}

// ---- reading ----

func (s *Service) contextVersion(q queryer, projectID int64) (int64, error) {
	var v int64
	err := q.QueryRow(`SELECT context_version FROM projects WHERE id = ?`, projectID).Scan(&v)
	return v, err
}

const contextFrom = ` FROM project_context_entries e LEFT JOIN project_context_entries sb ON sb.id = e.superseded_by `

func contextColumns(withBody bool) string {
	body := "''"
	if withBody {
		body = "e.body"
	}
	return `SELECT e.id, e.key, e.category, e.title, e.summary, ` + body + `, e.status, COALESCE(sb.key, ''), e.verification_state, e.source_kind,
		e.pinned, e.version, e.created_by, e.updated_by, e.created_at, e.updated_at, e.verified_at, e.verified_by`
}

func scanContext(rows *sql.Rows) ([]ContextEntry, error) {
	defer rows.Close()
	out := []ContextEntry{}
	for rows.Next() {
		var e ContextEntry
		var verified sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Key, &e.Category, &e.Title, &e.Summary, &e.Body, &e.Status, &e.SupersededBy, &e.Verification, &e.SourceKind,
			&e.Pinned, &e.Version, &e.CreatedBy, &e.UpdatedBy, &e.CreatedAt, &e.UpdatedAt, &verified, &e.VerifiedBy); err != nil {
			return nil, err
		}
		if verified.Valid {
			e.VerifiedAt = &verified.Int64
		}
		e.Sources = []ContextSource{}
		out = append(out, e)
	}
	return out, rows.Err()
}

// loadSources fills the Sources of the entries with one query.
func loadSources(q queryer, list []ContextEntry) error {
	if len(list) == 0 {
		return nil
	}
	byID := make(map[int64]*ContextEntry, len(list))
	args := make([]any, len(list))
	for i := range list {
		byID[list[i].ID] = &list[i]
		args[i] = list[i].ID
	}
	rows, err := q.Query(`SELECT entry_id, source_type, repository, file_path, commit_sha, ticket_key, url, content_hash, note, verified_at
		FROM project_context_sources WHERE entry_id IN (`+placeholders(len(list))+`) ORDER BY id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var c ContextSource
		var verified sql.NullInt64
		if err := rows.Scan(&id, &c.Type, &c.Repository, &c.Path, &c.CommitSHA, &c.Ticket, &c.URL, &c.ContentHash, &c.Note, &verified); err != nil {
			return err
		}
		if verified.Valid {
			c.VerifiedAt = &verified.Int64
		}
		byID[id].Sources = append(byID[id].Sources, c)
	}
	return rows.Err()
}

func contextEntry(q queryer, projectID int64, key string, withBody bool) (ContextEntry, error) {
	rows, err := q.Query(contextColumns(withBody)+contextFrom+`WHERE e.project_id = ? AND e.key = ?`, projectID, key)
	if err != nil {
		return ContextEntry{}, err
	}
	list, err := scanContext(rows)
	if err != nil {
		return ContextEntry{}, err
	}
	if len(list) == 0 {
		return ContextEntry{}, ErrNotFound
	}
	return list[0], loadSources(q, list)
}

// GetContext reads one entry in full, with body and sources.
func (s *Service) GetContext(ctx context.Context, a auth.Actor, project, key string) (ContextEntry, error) {
	p, _, err := s.access(s.db.R, a, project, actView)
	if err != nil {
		return ContextEntry{}, err
	}
	return contextEntry(s.db.R, p.ID, key, true)
}

func (e ContextEntry) state() string {
	if e.Verification == "verified" {
		return ""
	}
	return e.Verification
}

func truncateRunes(v string, n int) string {
	if utf8.RuneCountInString(v) <= n {
		return v
	}
	r := []rune(v)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// compact is the small shape of bootstrap and search: unverified and stale entries say so, verified ones stay quiet.
func (e ContextEntry) compact(snippet, refs bool) map[string]any {
	m := map[string]any{"key": e.Key, "category": e.Category, "title": e.Title, "summary": e.Summary}
	if snippet {
		delete(m, "summary")
		m["snippet"] = truncateRunes(e.Summary, snippetRunes)
	}
	if st := e.state(); st != "" {
		m["state"] = st
	}
	if e.Status != "active" {
		m["status"] = e.Status
	}
	if refs && len(e.Sources) > 0 {
		r := make([]string, len(e.Sources))
		for i, c := range e.Sources {
			r[i] = c.ref()
		}
		m["refs"] = r
	}
	return m
}

func (e ContextEntry) selectFields(fields []string) (map[string]any, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	var all map[string]any
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		if !contextFields[f] {
			return nil, invalid("unknown field %q", f)
		}
		out[f] = all[f]
	}
	return out, nil
}

var listFields = []string{"key", "category", "title", "summary", "status", "superseded_by", "verification_state", "source_kind", "pinned", "version", "updated_at", "verified_at", "sources"}

// fit returns how many of the items fit the byte budget once wrapped in a result of overhead bytes.
// At least one item is returned so paging always advances; an entry is bounded well below the minimum budget.
func fit(items []map[string]any, overhead, maxBytes int) (int, error) {
	if maxBytes <= 0 {
		return len(items), nil
	}
	used := overhead
	for i, it := range items {
		b, err := json.Marshal(it)
		if err != nil {
			return 0, err
		}
		used += len(b) + 1
		if used > maxBytes && i > 0 {
			return i, nil
		}
	}
	return len(items), nil
}

// trim drops trailing items until the whole serialized page fits maxBytes (at least one item stays), so a
// budget covers the wrapper too and the cursor always points at the first item that was left out.
func (p *ContextPage) trim(offset, maxBytes int) error {
	for maxBytes > 0 && len(p.Items) > 1 {
		b, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if len(b) <= maxBytes {
			return nil
		}
		p.Items = p.Items[:len(p.Items)-1]
		p.NextCursor = encodeOffsetCursor(offset + len(p.Items))
	}
	return nil
}

// ClampContextBytes applies the default and the limits of a byte budget.
func ClampContextBytes(n int) int { return clampBytes(n) }

func clampBytes(n int) int {
	switch {
	case n <= 0:
		return contextDefBytes
	case n < contextMinBytes:
		return contextMinBytes
	case n > contextMaxBytes:
		return contextMaxBytes
	}
	return n
}

// activeCount counts active entries, for the pruning warning.
func activeCount(q queryer, projectID int64) (int, error) {
	var n int
	err := q.QueryRow(`SELECT COUNT(*) FROM project_context_entries WHERE project_id = ? AND status = 'active'`, projectID).Scan(&n)
	return n, err
}

func pruneWarning(n int) []string {
	if n <= contextWarnAt {
		return nil
	}
	return []string{"project has more than 200 active context entries; archive or merge old ones"}
}

// contextOrder sorts by category in bootstrap order, then newest first.
const contextOrder = `CASE e.category WHEN 'architecture' THEN 0 WHEN 'decision' THEN 1 WHEN 'convention' THEN 2 WHEN 'pitfall' THEN 3 WHEN 'dependency' THEN 4 ELSE 5 END,
	e.updated_at DESC, e.key`

func (s *Service) contextQuery(q queryer, projectID int64, f ContextFilter, pinnedOnly bool) ([]ContextEntry, int, error) {
	conds, args := []string{"e.project_id = ?"}, []any{projectID}
	switch f.Status {
	case "", "active":
		conds = append(conds, "e.status = 'active'")
	case "superseded", "archived":
		conds = append(conds, "e.status = ?")
		args = append(args, f.Status)
	case "all":
	default:
		return nil, 0, invalid("status must be active, superseded, archived or all")
	}
	if len(f.Categories) > 0 {
		for _, c := range f.Categories {
			if !slices.Contains(ContextCategories, c) {
				return nil, 0, invalid("category must be one of %s", strings.Join(ContextCategories, ", "))
			}
			args = append(args, c)
		}
		conds = append(conds, "e.category IN ("+placeholders(len(f.Categories))+")")
	}
	if pinnedOnly {
		conds = append(conds, "e.pinned = 1")
	} else if f.Pinned != nil {
		conds = append(conds, "e.pinned = ?")
		args = append(args, *f.Pinned)
	}
	if len(f.Keys) > 0 {
		for _, k := range f.Keys {
			args = append(args, k)
		}
		conds = append(conds, "e.key IN ("+placeholders(len(f.Keys))+")")
	}
	order := contextOrder
	if f.Q != "" {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Q)
		conds = append(conds, `(e.title LIKE ? ESCAPE '\' OR e.summary LIKE ? ESCAPE '\')`)
		args = append(args, "%"+esc+"%", "%"+esc+"%")
		order = `(e.title = ? COLLATE NOCASE) DESC, ` + contextOrder
	}
	offset := 0
	if f.Cursor != "" {
		var err error
		if offset, err = decodeOffsetCursor(f.Cursor); err != nil {
			return nil, 0, err
		}
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := contextColumns(f.WithBody) + contextFrom + `WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	if f.Q != "" {
		args = append(args, f.Q) // the ORDER BY placeholder comes after the WHERE ones
	}
	rows, err := q.Query(query, append(args, limit+1, offset)...)
	if err != nil {
		return nil, 0, err
	}
	list, err := scanContext(rows)
	if err != nil {
		return nil, 0, err
	}
	return list, offset, loadSources(q, list)
}

// ListContext lists, filters and searches entries (view). A byte budget pages the answer instead of cutting it.
func (s *Service) ListContext(ctx context.Context, a auth.Actor, project string, f ContextFilter) (ContextPage, error) {
	p, _, err := s.access(s.db.R, a, project, actView)
	if err != nil {
		return ContextPage{}, err
	}
	list, offset, err := s.contextQuery(s.db.R, p.ID, f, false)
	if err != nil {
		return ContextPage{}, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	more := len(list) > limit
	if more {
		list = list[:limit]
	}
	items := make([]map[string]any, len(list))
	for i, e := range list {
		switch {
		case len(f.Fields) > 0:
			items[i], err = e.selectFields(f.Fields)
		case f.Compact:
			items[i] = e.compact(true, true)
		default:
			items[i], err = e.selectFields(listFields)
		}
		if err != nil {
			return ContextPage{}, err
		}
	}
	n, err := fit(items, 160, f.MaxBytes)
	if err != nil {
		return ContextPage{}, err
	}
	if n < len(items) {
		items, more = items[:n], true
	}
	page := ContextPage{Project: p.Key, Items: items}
	if more {
		page.NextCursor = encodeOffsetCursor(offset + n)
	}
	if page.ContextVersion, err = s.contextVersion(s.db.R, p.ID); err != nil {
		return ContextPage{}, err
	}
	active, err := activeCount(s.db.R, p.ID)
	if err != nil {
		return ContextPage{}, err
	}
	page.Warnings = pruneWarning(active)
	return page, page.trim(offset, f.MaxBytes)
}

// ContextBootstrapFor composes the bootstrap of a project (view): pinned active entries, deterministic, no LLM.
func (s *Service) ContextBootstrapFor(ctx context.Context, a auth.Actor, project string, categories []string, limit int, cursor string, maxBytes int) (ContextBootstrap, error) {
	p, _, err := s.access(s.db.R, a, project, actView)
	if err != nil {
		return ContextBootstrap{}, err
	}
	f := ContextFilter{Categories: categories, Cursor: cursor, Limit: limit}
	list, offset, err := s.contextQuery(s.db.R, p.ID, f, true)
	if err != nil {
		return ContextBootstrap{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	more := len(list) > limit
	if more {
		list = list[:limit]
	}
	items := make([]map[string]any, len(list))
	for i, e := range list {
		items[i] = e.compact(false, false)
	}
	maxBytes = clampBytes(maxBytes)
	n, err := fit(items, 220, maxBytes)
	if err != nil {
		return ContextBootstrap{}, err
	}
	version, err := s.contextVersion(s.db.R, p.ID)
	if err != nil {
		return ContextBootstrap{}, err
	}
	active, err := activeCount(s.db.R, p.ID)
	if err != nil {
		return ContextBootstrap{}, err
	}
	for ; ; n-- {
		b := ContextBootstrap{Project: p.Key, ContextVersion: version, Entries: items[:n], Warnings: pruneWarning(active)}
		seen := map[string]bool{}
		for _, e := range list[:n] {
			for _, c := range e.Sources {
				if r := c.ref(); !seen[r] && len(b.References) < maxReferences {
					seen[r] = true
					b.References = append(b.References, r)
				}
			}
		}
		if more || n < len(items) {
			b.NextCursor = encodeOffsetCursor(offset + n)
		}
		raw, err := json.Marshal(b)
		if err != nil {
			return ContextBootstrap{}, err
		}
		if len(raw) <= maxBytes || n <= 1 {
			return b, nil
		}
	}
}

// ContextDetails returns full entries (with body and sources) for the keys, as many as fit the byte budget.
func (s *Service) ContextDetails(ctx context.Context, a auth.Actor, project string, keys []string, cursor string, maxBytes int, fields []string) (ContextPage, error) {
	if len(keys) == 0 || len(keys) > 20 {
		return ContextPage{}, invalid("give 1-20 keys")
	}
	p, _, err := s.access(s.db.R, a, project, actView)
	if err != nil {
		return ContextPage{}, err
	}
	offset := 0
	if cursor != "" {
		if offset, err = decodeOffsetCursor(cursor); err != nil {
			return ContextPage{}, err
		}
	}
	if offset > len(keys) {
		return ContextPage{}, invalid("invalid cursor")
	}
	var items []map[string]any
	for _, k := range keys[offset:] {
		e, err := contextEntry(s.db.R, p.ID, k, true)
		if err != nil {
			return ContextPage{}, err // an unknown key is reported, not skipped
		}
		if len(fields) > 0 {
			m, err := e.selectFields(fields)
			if err != nil {
				return ContextPage{}, err
			}
			items = append(items, m)
			continue
		}
		m, err := e.selectFields(append(append([]string{}, listFields...), "body", "created_by", "updated_by", "created_at", "verified_by"))
		if err != nil {
			return ContextPage{}, err
		}
		items = append(items, m)
	}
	n, err := fit(items, 160, clampBytes(maxBytes))
	if err != nil {
		return ContextPage{}, err
	}
	page := ContextPage{Project: p.Key, Items: items[:n]}
	if offset+n < len(keys) {
		page.NextCursor = encodeOffsetCursor(offset + n)
	}
	if page.ContextVersion, err = s.contextVersion(s.db.R, p.ID); err != nil {
		return ContextPage{}, err
	}
	return page, page.trim(offset, clampBytes(maxBytes))
}

// ---- writing ----

// ctxWrite runs one context mutation: access check, optional idempotent replay, the change, the stored result.
func (s *Service) ctxWrite(a auth.Actor, project string, act action, idemKey string, req any, f func(tx *sql.Tx, p Project) (ContextEntry, error)) (ContextResult, error) {
	if len(idemKey) > maxIdempotency {
		return ContextResult{}, invalid("idempotency key is longer than %d characters", maxIdempotency)
	}
	var res ContextResult
	err := s.tx(func(tx *sql.Tx) error {
		p, _, err := s.access(tx, a, project, act)
		if err != nil {
			return err
		}
		var hash string
		if idemKey != "" {
			if hash, err = requestHash(struct {
				Project string
				Req     any
			}{p.Key, req}); err != nil {
				return err
			}
			replayed, err := idemReplay(tx, a, idemKey, hash, &res)
			if err != nil || replayed {
				res.Replayed = replayed
				return err
			}
		}
		e, err := f(tx, p)
		if err != nil {
			return err
		}
		v, err := s.contextVersion(tx, p.ID)
		if err != nil {
			return err
		}
		res = ContextResult{Entry: e, ContextVersion: v}
		if idemKey != "" {
			return s.idemStore(tx, a, idemKey, hash, res)
		}
		return nil
	})
	return res, err
}

func contextConflict(e ContextEntry) error {
	return &ConflictError{Msg: "context entry was changed by someone else", Current: map[string]any{
		"key": e.Key, "version": e.Version, "title": e.Title, "summary": e.Summary}}
}

// UpsertContext creates an entry, or changes an existing one when the key exists (write; updates need expected_version).
// New and edited entries are never verified: only a human with context.verify does that.
func (s *Service) UpsertContext(ctx context.Context, a auth.Actor, project string, in ContextInput) (ContextResult, error) {
	if in.Key != "" {
		if err := contextKey(in.Key); err != nil {
			return ContextResult{}, err
		}
	}
	if in.Category != nil && !slices.Contains(ContextCategories, *in.Category) {
		return ContextResult{}, invalid("category must be one of %s", strings.Join(ContextCategories, ", "))
	}
	var title, summary *string
	for _, f := range []struct {
		in, out **string
		name    string
		max     int
	}{{&in.Title, &title, "title", maxContextTitle}, {&in.Summary, &summary, "summary", maxContextSummary}} {
		if *f.in == nil {
			continue
		}
		v := strings.TrimSpace(**f.in)
		if v == "" || utf8.RuneCountInString(v) > f.max {
			return ContextResult{}, invalid("%s is required, at most %d characters", f.name, f.max)
		}
		*f.out = &v
	}
	if in.Body != nil && len(*in.Body) > maxContextBody {
		return ContextResult{}, invalid("body is at most %d bytes", maxContextBody)
	}
	var sources *[]ContextSource
	if in.Sources != nil {
		list, err := validateSources(*in.Sources)
		if err != nil {
			return ContextResult{}, err
		}
		sources = &list
	}

	return s.ctxWrite(a, project, actContextWrite, in.IdempotencyKey, in, func(tx *sql.Tx, p Project) (ContextEntry, error) {
		key := in.Key
		var old ContextEntry
		var err error
		exists := false
		if key != "" {
			if old, err = contextEntry(tx, p.ID, key, true); err == nil {
				exists = true
			} else if !errors.Is(err, ErrNotFound) {
				return ContextEntry{}, err
			}
		}
		now := s.nowMs()
		if !exists {
			if in.ExpectedVersion != 0 {
				return ContextEntry{}, ErrNotFound
			}
			if in.Category == nil || title == nil || summary == nil {
				return ContextEntry{}, invalid("category, title and summary are required to create an entry")
			}
			if key == "" {
				if key, err = s.freeContextKey(tx, p.ID, slugify(*title)); err != nil {
					return ContextEntry{}, err
				}
			}
			body := ""
			if in.Body != nil {
				body = *in.Body
			}
			var list []ContextSource
			if sources != nil {
				list = *sources
			}
			res, err := tx.Exec(`INSERT INTO project_context_entries (project_id, key, category, title, summary, body, source_kind, pinned, created_by, updated_by, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.ID, key, *in.Category, *title, *summary, body, sourceKind(list), in.Pinned != nil && *in.Pinned,
				a.String(), a.String(), now, now)
			if err != nil {
				return ContextEntry{}, err
			}
			id, _ := res.LastInsertId()
			if err := replaceSources(tx, id, list, nil); err != nil {
				return ContextEntry{}, err
			}
			if err := s.bumpContext(tx, p.ID); err != nil {
				return ContextEntry{}, err
			}
			if err := s.journal(ctx, tx, a, change{Operation: "context.create", Target: p.Key + "/context/" + key, Event: "context.updated",
				Project: p.Key, ItemKey: key, Changes: map[string]any{"category": *in.Category}}); err != nil {
				return ContextEntry{}, err
			}
			return contextEntry(tx, p.ID, key, false)
		}

		if in.ExpectedVersion <= 0 {
			return ContextEntry{}, invalid("expected_version is required to update an entry")
		}
		if in.ExpectedVersion != old.Version {
			return ContextEntry{}, contextConflict(old)
		}
		next := old
		var changed []string
		set := func(name string, differs bool) {
			if differs {
				changed = append(changed, name)
			}
		}
		if in.Category != nil {
			set("category", *in.Category != old.Category)
			next.Category = *in.Category
		}
		if title != nil {
			set("title", *title != old.Title)
			next.Title = *title
		}
		if summary != nil {
			set("summary", *summary != old.Summary)
			next.Summary = *summary
		}
		if in.Body != nil {
			set("body", *in.Body != old.Body)
			next.Body = *in.Body
		}
		stale := false
		if sources != nil {
			differs := !slices.EqualFunc(*sources, old.Sources, func(x, y ContextSource) bool { x.VerifiedAt, y.VerifiedAt = nil, nil; return x == y })
			set("sources", differs)
			next.Sources = *sources
			stale = hashChanged(old.Sources, *sources)
		}
		content := len(changed) > 0
		if in.Pinned != nil {
			set("pinned", *in.Pinned != old.Pinned)
			next.Pinned = *in.Pinned
		}
		if len(changed) == 0 {
			return old, nil // nothing to change: no new version, no audit entry
		}
		switch {
		case old.Verification == "verified" && stale:
			next.Verification = "stale"
		case old.Verification == "verified" && content:
			next.Verification = "unverified"
		}
		next.SourceKind = sourceKind(next.Sources)
		res, err := tx.Exec(`UPDATE project_context_entries SET category = ?, title = ?, summary = ?, body = ?, pinned = ?, verification_state = ?, source_kind = ?,
			version = version + 1, updated_by = ?, updated_at = ? WHERE id = ? AND version = ?`,
			next.Category, next.Title, next.Summary, next.Body, next.Pinned, next.Verification, next.SourceKind, a.String(), now, old.ID, old.Version)
		if err != nil {
			return ContextEntry{}, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ContextEntry{}, contextConflict(old)
		}
		if sources != nil {
			if err := replaceSources(tx, old.ID, *sources, old.Sources); err != nil {
				return ContextEntry{}, err
			}
		}
		if old.Status == "active" {
			if err := s.bumpContext(tx, p.ID); err != nil {
				return ContextEntry{}, err
			}
		}
		if err := s.journal(ctx, tx, a, change{Operation: "context.update", Target: p.Key + "/context/" + key, Event: "context.updated",
			Project: p.Key, ItemKey: key, Changes: map[string]any{"fields": changed, "version": old.Version + 1}}); err != nil {
			return ContextEntry{}, err
		}
		return contextEntry(tx, p.ID, key, false)
	})
}

// hashChanged reports whether a source that was kept now has another content hash than the one stored.
func hashChanged(old, next []ContextSource) bool {
	for _, n := range next {
		if n.ContentHash == "" {
			continue
		}
		for _, o := range old {
			if o.ContentHash != "" && o.identity() == n.identity() && o.ContentHash != n.ContentHash {
				return true
			}
		}
	}
	return false
}

// replaceSources rewrites an entry's sources. A source that is unchanged keeps its verification time.
func replaceSources(tx *sql.Tx, entryID int64, list, old []ContextSource) error {
	if _, err := tx.Exec(`DELETE FROM project_context_sources WHERE entry_id = ?`, entryID); err != nil {
		return err
	}
	for _, c := range list {
		var verified any
		for _, o := range old {
			if o.VerifiedAt != nil && o.identity() == c.identity() && o.ContentHash == c.ContentHash {
				verified = *o.VerifiedAt
			}
		}
		if _, err := tx.Exec(`INSERT INTO project_context_sources (entry_id, source_type, repository, file_path, commit_sha, ticket_key, url, content_hash, note, verified_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, entryID, c.Type, c.Repository, c.Path, c.CommitSHA, c.Ticket, c.URL, c.ContentHash, c.Note, verified); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) bumpContext(tx *sql.Tx, projectID int64) error {
	_, err := tx.Exec(`UPDATE projects SET context_version = context_version + 1 WHERE id = ?`, projectID)
	return err
}

// freeContextKey finds an unused key: base, base-2, base-3, ...
func (s *Service) freeContextKey(q queryer, projectID int64, base string) (string, error) {
	if slices.Contains(contextReserved, base) {
		base += "-entry"
	}
	for i := 1; i < 1000; i++ {
		k := base
		if i > 1 {
			suffix := "-" + itoa(int64(i))
			k = base[:min(len(base), 64-len(suffix))] + suffix
		}
		var n int
		if err := q.QueryRow(`SELECT COUNT(*) FROM project_context_entries WHERE project_id = ? AND key = ?`, projectID, k).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return k, nil
		}
	}
	return "", rule("could not find a free key for %q", base)
}

// lifecycle runs archive, supersede and verify: they share lookup, version check and bookkeeping.
func (s *Service) lifecycle(ctx context.Context, a auth.Actor, project, key string, version int64, idemKey, op string, act action, req any,
	apply func(tx *sql.Tx, p Project, e ContextEntry, now int64) (changes map[string]any, err error)) (ContextResult, error) {
	return s.ctxWrite(a, project, act, idemKey, req, func(tx *sql.Tx, p Project) (ContextEntry, error) {
		e, err := contextEntry(tx, p.ID, key, false)
		if err != nil {
			return ContextEntry{}, err
		}
		if version > 0 && version != e.Version {
			return ContextEntry{}, contextConflict(e)
		}
		now := s.nowMs()
		changes, err := apply(tx, p, e, now)
		if err != nil {
			return ContextEntry{}, err
		}
		if e.Status == "active" {
			if err := s.bumpContext(tx, p.ID); err != nil {
				return ContextEntry{}, err
			}
		}
		if err := s.journal(ctx, tx, a, change{Operation: "context." + op, Target: p.Key + "/context/" + key, Event: "context.updated",
			Project: p.Key, ItemKey: key, Changes: changes}); err != nil {
			return ContextEntry{}, err
		}
		return contextEntry(tx, p.ID, key, false)
	})
}

// ArchiveContext hides an entry from bootstrap and search. Entries are never deleted. version 0 skips the version check.
func (s *Service) ArchiveContext(ctx context.Context, a auth.Actor, project, key string, version int64, idemKey string) (ContextResult, error) {
	return s.lifecycle(ctx, a, project, key, version, idemKey, "archive", actContextWrite, map[string]any{"op": "archive", "key": key, "version": version},
		func(tx *sql.Tx, p Project, e ContextEntry, now int64) (map[string]any, error) {
			if e.Status == "archived" {
				return nil, rule("entry is already archived")
			}
			_, err := tx.Exec(`UPDATE project_context_entries SET status = 'archived', pinned = 0, version = version + 1, updated_by = ?, updated_at = ? WHERE id = ?`,
				a.String(), now, e.ID)
			return map[string]any{"from": e.Status}, err
		})
}

// SupersedeContext replaces an entry by another active one of the same project.
func (s *Service) SupersedeContext(ctx context.Context, a auth.Actor, project, key, by string, version int64, idemKey string) (ContextResult, error) {
	if by == "" || by == key {
		return ContextResult{}, invalid("by must be the key of another entry")
	}
	return s.lifecycle(ctx, a, project, key, version, idemKey, "supersede", actContextWrite, map[string]any{"op": "supersede", "key": key, "by": by, "version": version},
		func(tx *sql.Tx, p Project, e ContextEntry, now int64) (map[string]any, error) {
			if e.Status != "active" {
				return nil, rule("only an active entry can be superseded")
			}
			var byID int64
			var byStatus string
			if err := tx.QueryRow(`SELECT id, status FROM project_context_entries WHERE project_id = ? AND key = ?`, p.ID, by).Scan(&byID, &byStatus); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, invalid("unknown entry %q", by)
				}
				return nil, err
			}
			if byStatus != "active" {
				return nil, rule("the replacement must be active")
			}
			_, err := tx.Exec(`UPDATE project_context_entries SET status = 'superseded', superseded_by = ?, pinned = 0, version = version + 1, updated_by = ?, updated_at = ? WHERE id = ?`,
				byID, a.String(), now, e.ID)
			return map[string]any{"superseded_by": by}, err
		})
}

// VerifyContext marks an active entry verified. Only a human with context.verify can: a token never verifies.
func (s *Service) VerifyContext(ctx context.Context, a auth.Actor, project, key string, version int64) (ContextResult, error) {
	return s.lifecycle(ctx, a, project, key, version, "", "verify", actContextVerify, nil,
		func(tx *sql.Tx, p Project, e ContextEntry, now int64) (map[string]any, error) {
			if a.Kind != auth.KindHuman {
				return nil, ErrForbidden
			}
			if e.Status != "active" {
				return nil, rule("only an active entry can be verified")
			}
			if _, err := tx.Exec(`UPDATE project_context_entries SET verification_state = 'verified', verified_at = ?, verified_by = ?, version = version + 1,
				updated_by = ?, updated_at = ? WHERE id = ?`, now, a.String(), a.String(), now, e.ID); err != nil {
				return nil, err
			}
			_, err := tx.Exec(`UPDATE project_context_sources SET verified_at = ? WHERE entry_id = ?`, now, e.ID)
			return map[string]any{"from": e.Verification}, err
		})
}
