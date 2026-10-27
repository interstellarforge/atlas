package core

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kees/atlas/internal/auth"
)

// MaxImportRows bounds one import; it runs in a single transaction.
const MaxImportRows = 20000

// ExportItem is an item plus the things that hang off it. Its JSON fields are a superset of ImportRow's,
// so an export file can be imported again.
type ExportItem struct {
	Item
	Checklist   []ChecklistItem `json:"checklist"`
	Comments    []Comment       `json:"comments"`
	Links       Links           `json:"links"`
	Attachments []Attachment    `json:"attachments"`
}

// Export is the JSON export document of one project (format 1).
type Export struct {
	Format     int          `json:"format"`
	Project    string       `json:"project"`
	Name       string       `json:"name"`
	ExportedAt int64        `json:"exported_at"`
	Workflow   Workflow     `json:"workflow"`
	Items      []ExportItem `json:"items"`
}

// ExportProject reads everything of a project the actor may see, archived items included.
func (s *Service) ExportProject(ctx context.Context, a auth.Actor, key string) (Export, error) {
	p, _, err := s.access(s.db.R, a, key, actExport)
	if err != nil {
		return Export{}, err
	}
	w, err := s.GetWorkflow(ctx, a, key)
	if err != nil {
		return Export{}, err
	}
	out := Export{Format: 1, Project: p.Key, Name: p.Name, ExportedAt: s.nowMs(), Workflow: w, Items: []ExportItem{}}
	cursor := ""
	for {
		page, err := s.ListItems(ctx, a, ItemFilter{Project: key, IncludeArchived: true, Limit: maxPage, Cursor: cursor, Fields: []string{"key"}})
		if err != nil {
			return out, err
		}
		keys := make([]string, 0, len(page.Items))
		for _, m := range page.Items {
			k, _ := m["key"].(string)
			keys = append(keys, k)
		}
		items, err := s.exportPage(a, keys)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, items...)
		if page.NextCursor == nil {
			return out, nil
		}
		cursor = *page.NextCursor
	}
}

// CSVCell neutralises spreadsheet formulas in user-provided text; ParseImportCSV undoes it.
func CSVCell(v string) string {
	if v != "" && strings.ContainsRune(csvFormulaStart, rune(v[0])) {
		return "'" + v
	}
	return v
}

const csvFormulaStart = "=+-@\t\r"

var csvHeader = []string{"key", "type", "title", "description", "status", "priority", "parent", "labels", "due_at", "milestone", "archived"}

// WriteCSV writes one row per item; labels are joined with ";", due_at is an RFC 3339 date.
func (e Export) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	_ = cw.Write(csvHeader)
	for _, it := range e.Items {
		due, parent, milestone, archived := "", "", "", ""
		if it.DueAt != nil {
			due = time.UnixMilli(*it.DueAt).UTC().Format(time.RFC3339)
		}
		if it.Parent != nil {
			parent = *it.Parent
		}
		if it.Milestone != nil {
			milestone = *it.Milestone
		}
		if it.ArchivedAt != nil {
			archived = "true"
		}
		_ = cw.Write([]string{CSVCell(it.Key), CSVCell(it.Type), CSVCell(it.Title), CSVCell(it.Description), CSVCell(it.Status), CSVCell(it.Priority),
			CSVCell(parent), CSVCell(strings.Join(it.Labels, ";")), due, CSVCell(milestone), archived})
	}
	cw.Flush()
	return cw.Error()
}

// ImportRow is one item to import. Key (optional) is the key in the source system; Parent refers to
// another row's Key, or to an existing item of the target project.
type ImportRow struct {
	Key         string         `json:"key"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Parent      *string        `json:"parent"`
	Status      string         `json:"status"`
	Priority    string         `json:"priority"`
	Labels      []string       `json:"labels"`
	DueAt       *int64         `json:"due_at"`
	Milestone   *string        `json:"milestone"`
	CustomData  map[string]any `json:"custom_data"`
}

// ParseImportCSV reads the CSV shape WriteCSV produces. Unknown columns are ignored; type and title are required.
func ParseImportCSV(r io.Reader) ([]ImportRow, error) {
	cr := csv.NewReader(r)
	head, err := cr.Read()
	if err != nil {
		return nil, invalid("csv: missing header row")
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	if _, ok := col["title"]; !ok {
		return nil, invalid("csv: header needs a title column")
	}
	if _, ok := col["type"]; !ok {
		return nil, invalid("csv: header needs a type column")
	}
	var rows []ImportRow
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, invalid("csv line %d: %v", line, err)
		}
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(rec) {
				v := strings.TrimSpace(rec[i])
				if len(v) > 1 && v[0] == '\'' && strings.ContainsRune(csvFormulaStart, rune(v[1])) {
					v = v[1:]
				}
				return v
			}
			return ""
		}
		row := ImportRow{Key: get("key"), Type: get("type"), Title: get("title"), Description: get("description"),
			Status: get("status"), Priority: get("priority")}
		if v := get("parent"); v != "" {
			row.Parent = &v
		}
		if v := get("milestone"); v != "" {
			row.Milestone = &v
		}
		if v := get("labels"); v != "" {
			for _, l := range strings.Split(v, ";") {
				if l = strings.TrimSpace(l); l != "" {
					row.Labels = append(row.Labels, l)
				}
			}
		}
		if v := get("due_at"); v != "" {
			ms, err := parseDue(v)
			if err != nil {
				return nil, invalid("csv line %d: due_at %q is not an RFC 3339 date or unix milliseconds", line, v)
			}
			row.DueAt = &ms
		}
		rows = append(rows, row)
	}
}

func parseDue(v string) (int64, error) {
	if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
		return ms, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UnixMilli(), nil
		}
	}
	return 0, fmt.Errorf("bad date")
}

type ImportIssue struct {
	Row     int    `json:"row"` // 1-based position in the file
	Key     string `json:"key,omitempty"`
	Message string `json:"message"`
}

type ImportReport struct {
	DryRun  bool `json:"dry_run"`
	Applied bool `json:"applied"` // false for a dry run and whenever any row has an error: nothing is written
	Created int  `json:"created"`
	// Duplicates are rows whose type, title and parent already exist; they are skipped unless allowDuplicates.
	Duplicates []ImportIssue `json:"duplicates"`
	Errors     []ImportIssue `json:"errors"`
	// Mapping is source key -> key in the target project, for rows that had a key.
	Mapping map[string]string `json:"mapping"`
}

// ImportItems creates the rows in the explicitly named project. The target is validated by running the real
// create rules inside one transaction: with dryRun, or when any row fails, that transaction is rolled back,
// so an import is all-or-nothing and a dry run reports exactly what the real run would do.
func (s *Service) ImportItems(ctx context.Context, a auth.Actor, project string, rows []ImportRow, dryRun, allowDuplicates bool) (ImportReport, error) {
	rep := ImportReport{DryRun: dryRun, Duplicates: []ImportIssue{}, Errors: []ImportIssue{}, Mapping: map[string]string{}}
	if len(rows) == 0 || len(rows) > MaxImportRows {
		return rep, invalid("an import needs 1-%d rows", MaxImportRows)
	}
	order, issues := importOrder(rows)
	rep.Errors = append(rep.Errors, issues...)

	tx, err := s.db.W.Begin()
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()
	p, _, err := s.access(tx, a, project, actImport)
	if err != nil {
		return rep, err
	}
	var idb [8]byte
	if _, err := rand.Read(idb[:]); err != nil {
		return rep, err
	}
	bs := *s
	bs.outer = tx
	bctx := WithBatch(ctx, hex.EncodeToString(idb[:]))

	bad := map[int]bool{}
	for _, is := range issues {
		bad[is.Row-1] = true
	}
	for _, i := range order {
		row := rows[i]
		if bad[i] {
			continue
		}
		in := CreateItem{Project: p.Key, Type: row.Type, Title: row.Title, Description: row.Description, Status: row.Status,
			Priority: row.Priority, Labels: row.Labels, DueAt: row.DueAt, CustomData: row.CustomData}
		if row.Milestone != nil {
			in.Milestone = *row.Milestone
		}
		if row.Parent != nil && *row.Parent != "" {
			if k, ok := rep.Mapping[*row.Parent]; ok {
				in.Parent = k
			} else {
				in.Parent = *row.Parent
			}
		}
		if !allowDuplicates {
			if existing, err := findDuplicate(tx, p.ID, in); err != nil {
				return rep, err
			} else if existing != "" {
				rep.Duplicates = append(rep.Duplicates, ImportIssue{Row: i + 1, Key: existing, Message: "same type, title and parent already exist"})
				if row.Key != "" {
					rep.Mapping[row.Key] = existing // children attach to the existing item
				}
				continue
			}
		}
		if _, err := tx.Exec(`SAVEPOINT row`); err != nil {
			return rep, err
		}
		it, opErr := bs.CreateItem(bctx, a, in)
		if opErr != nil {
			if _, err := tx.Exec(`ROLLBACK TO row`); err != nil {
				return rep, err
			}
		}
		if _, err := tx.Exec(`RELEASE row`); err != nil {
			return rep, err
		}
		if opErr != nil {
			info := ErrorInfo(opErr)
			if info.Code == "internal" {
				return rep, opErr
			}
			rep.Errors = append(rep.Errors, ImportIssue{Row: i + 1, Key: row.Key, Message: info.Message})
			continue
		}
		rep.Created++
		if row.Key != "" {
			rep.Mapping[row.Key] = it.Key
		}
	}
	sort.Slice(rep.Errors, func(i, j int) bool { return rep.Errors[i].Row < rep.Errors[j].Row })

	if dryRun || len(rep.Errors) > 0 {
		return rep, nil // deferred Rollback discards everything
	}
	if err := tx.Commit(); err != nil {
		return rep, err
	}
	rep.Applied = true
	if s.notify != nil {
		s.notify()
	}
	return rep, nil
}

// importOrder returns row indexes parents-first and reports structural problems of the file itself:
// duplicate source keys and parent cycles. A parent that is not a row key is assumed to be an existing item.
func importOrder(rows []ImportRow) ([]int, []ImportIssue) {
	var issues []ImportIssue
	byKey := map[string]int{}
	for i, r := range rows {
		if r.Key == "" {
			continue
		}
		if _, dup := byKey[r.Key]; dup {
			issues = append(issues, ImportIssue{Row: i + 1, Key: r.Key, Message: "key appears more than once in the file"})
			continue
		}
		byKey[r.Key] = i
	}
	depth := make([]int, len(rows))
	for i := range rows {
		seen := map[int]bool{i: true}
		for cur := i; rows[cur].Parent != nil; {
			next, ok := byKey[*rows[cur].Parent]
			if !ok {
				break
			}
			if seen[next] {
				issues = append(issues, ImportIssue{Row: i + 1, Key: rows[i].Key, Message: "parent chain loops back to this row"})
				break
			}
			seen[next] = true
			depth[i]++
			cur = next
		}
	}
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return depth[order[a]] < depth[order[b]] })
	return order, issues
}

func findDuplicate(tx *sql.Tx, projectID int64, in CreateItem) (string, error) {
	var parentID any
	if in.Parent != "" {
		var id int64
		err := tx.QueryRow(`SELECT id FROM work_items WHERE key = ? AND project_id = ?`, in.Parent, projectID).Scan(&id)
		if err == sql.ErrNoRows {
			return "", nil // the create reports the unknown parent
		}
		if err != nil {
			return "", err
		}
		parentID = id
	}
	var key string
	err := tx.QueryRow(`SELECT key FROM work_items WHERE project_id = ? AND type = ? AND title = ? AND parent_id IS ?`,
		projectID, in.Type, strings.TrimSpace(in.Title), parentID).Scan(&key)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return key, err
}

// exportPage loads the details of one page of items with one query per kind instead of five per item.
// The caller has checked access to the project.
func (s *Service) exportPage(a auth.Actor, keys []string) ([]ExportItem, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	in := "(" + strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",") + ")"
	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}
	byKey := make(map[string]*ExportItem, len(keys))
	ids := map[int64]string{}
	out := make([]ExportItem, len(keys))
	for i, k := range keys {
		r, err := loadRow(s.db.R, k)
		if err != nil {
			return nil, err
		}
		out[i] = ExportItem{Item: r.Item, Checklist: []ChecklistItem{}, Comments: []Comment{}, Links: Links{Outgoing: []Link{}, Incoming: []Link{}}, Attachments: []Attachment{}}
		byKey[k] = &out[i]
		ids[r.id] = k
	}
	idArgs := make([]any, 0, len(ids))
	for id := range ids {
		idArgs = append(idArgs, id)
	}
	idIn := "(" + strings.TrimSuffix(strings.Repeat("?,", len(idArgs)), ",") + ")"

	rows, err := s.db.R.Query(`SELECT i.key, c.id, c.text, c.done FROM checklist_items c JOIN work_items i ON i.id = c.item_id
		WHERE i.key IN `+in+` ORDER BY c.position, c.id`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var c ChecklistItem
		if err := rows.Scan(&k, &c.ID, &c.Text, &c.Done); err != nil {
			rows.Close()
			return nil, err
		}
		byKey[k].Checklist = append(byKey[k].Checklist, c)
	}
	rows.Close()

	if rows, err = s.db.R.Query(`SELECT i.key, c.id, c.actor, c.body, c.created_at FROM comments c
		JOIN work_items i ON i.id = c.item_id WHERE i.key IN `+in+` ORDER BY c.id`, args...); err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var c Comment
		if err := rows.Scan(&k, &c.ID, &c.Actor, &c.Body, &c.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		byKey[k].Comments = append(byKey[k].Comments, c)
	}
	rows.Close()

	if rows, err = s.db.R.Query(attachmentSelect+`WHERE i.key IN `+in+` ORDER BY c.id`, args...); err != nil {
		return nil, err
	}
	for rows.Next() {
		att, err := scanAttachment(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		byKey[att.Item].Attachments = append(byKey[att.Item].Attachments, att)
	}
	rows.Close()

	readable, err := s.readableProjects(s.db.R, a)
	if err != nil {
		return nil, err
	}
	for _, dir := range []struct{ self, other string }{{"from_id", "to_id"}, {"to_id", "from_id"}} {
		if rows, err = s.db.R.Query(`SELECT l.`+dir.self+`, l.type, o.key, o.title, st.name, o.project_id FROM item_links l
			JOIN work_items o ON o.id = l.`+dir.other+` JOIN project_statuses st ON st.id = o.status_id
			WHERE l.`+dir.self+` IN `+idIn+` ORDER BY l.id`, idArgs...); err != nil {
			return nil, err
		}
		for rows.Next() {
			var self, pid int64
			var l Link
			if err := rows.Scan(&self, &l.Type, &l.Key, &l.Title, &l.Status, &pid); err != nil {
				rows.Close()
				return nil, err
			}
			if _, ok := readable[pid]; !ok { // never reveal items from projects the actor cannot read
				continue
			}
			e := byKey[ids[self]]
			if dir.self == "from_id" {
				e.Links.Outgoing = append(e.Links.Outgoing, l)
			} else {
				e.Links.Incoming = append(e.Links.Incoming, l)
			}
		}
		rows.Close()
	}
	return out, nil
}
