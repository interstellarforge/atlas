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
	p, _, err := s.access(s.db.R, a, key, roleViewer)
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
		for _, m := range page.Items {
			k, _ := m["key"].(string)
			ei := ExportItem{}
			if ei.Item, err = s.GetItem(ctx, a, k); err != nil {
				return out, err
			}
			if ei.Checklist, err = s.ListChecklist(ctx, a, k); err != nil {
				return out, err
			}
			if ei.Comments, err = s.ListComments(ctx, a, k); err != nil {
				return out, err
			}
			if ei.Links, err = s.ListLinks(ctx, a, k); err != nil {
				return out, err
			}
			if ei.Attachments, err = s.ListAttachments(ctx, a, k); err != nil {
				return out, err
			}
			out.Items = append(out.Items, ei)
		}
		if page.NextCursor == nil {
			return out, nil
		}
		cursor = *page.NextCursor
	}
}

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
		_ = cw.Write([]string{it.Key, it.Type, it.Title, it.Description, it.Status, it.Priority, parent, strings.Join(it.Labels, ";"), due, milestone, archived})
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
				return strings.TrimSpace(rec[i])
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
	p, _, err := s.access(tx, a, project, roleMember)
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
