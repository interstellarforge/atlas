// Package seed generates a reproducible dataset of work items for benchmarks.
package seed

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"time"
)

type Options struct {
	Items    int
	Projects int
	Seed     uint64
}

var statusSpecs = []struct {
	name, category string
}{
	{"Backlog", "todo"}, {"Todo", "todo"}, {"In Progress", "active"}, {"Review", "active"}, {"Done", "completed"},
}

// Run fills the database in one transaction. Hierarchy per project: epics > stories > tasks (about 1 : 5 : 25).
func Run(w *sql.DB, o Options) error {
	if o.Projects < 1 {
		o.Projects = 3
	}
	rng := rand.New(rand.NewPCG(o.Seed, o.Seed^0x9e3779b97f4a7c15))
	now := time.Now().UnixMilli()

	tx, err := w.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insItem, err := tx.Prepare(`INSERT INTO work_items
		(key, project_id, parent_id, path, type, title, status_id, priority, rank, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`)
	if err != nil {
		return err
	}
	defer insItem.Close()

	priorities := []string{"low", "normal", "normal", "high", "urgent"}
	perProject := o.Items / o.Projects
	for p := 0; p < o.Projects; p++ {
		pkey := fmt.Sprintf("P%02d", p+1)
		res, err := tx.Exec(`INSERT INTO projects (key, name, created_at) VALUES (?, ?, ?)`, pkey, "Seed project "+pkey, now)
		if err != nil {
			return err
		}
		pid, _ := res.LastInsertId()

		statuses := make([]int64, len(statusSpecs))
		for i, s := range statusSpecs {
			r, err := tx.Exec(`INSERT INTO project_statuses (project_id, name, position, category) VALUES (?, ?, ?, ?)`,
				pid, s.name, i, s.category)
			if err != nil {
				return err
			}
			statuses[i], _ = r.LastInsertId()
		}

		n := 0
		add := func(parentID int64, parentPath, typ string) (int64, string, error) {
			n++
			key := fmt.Sprintf("%s-%d", pkey, n)
			status := statuses[rng.IntN(len(statuses))]
			var parent any
			if parentID != 0 {
				parent = parentID
			}
			// Path needs the row id, which is only known after insert; patch it below.
			r, err := insItem.Exec(key, pid, parent, "", typ, fmt.Sprintf("%s %s %d", typ, pkey, n), status,
				priorities[rng.IntN(len(priorities))], fmt.Sprintf("%010d", n), now, now)
			if err != nil {
				return 0, "", err
			}
			id, _ := r.LastInsertId()
			path := fmt.Sprintf("%s%d/", parentPath, id)
			if parentPath == "" {
				path = fmt.Sprintf("/%d/", id)
			}
			if _, err := tx.Exec(`UPDATE work_items SET path = ? WHERE id = ?`, path, id); err != nil {
				return 0, "", err
			}
			return id, path, nil
		}

		for n < perProject {
			eid, epath, err := add(0, "", "epic")
			if err != nil {
				return err
			}
			for s := 0; s < 5 && n < perProject; s++ {
				sid, spath, err := add(eid, epath, "story")
				if err != nil {
					return err
				}
				for t := 0; t < 5 && n < perProject; t++ {
					typ := "task"
					if rng.IntN(10) == 0 {
						typ = "bug"
					}
					if _, _, err := add(sid, spath, typ); err != nil {
						return err
					}
				}
			}
		}
		if _, err := tx.Exec(`UPDATE projects SET next_item_number = ? WHERE id = ?`, n+1, pid); err != nil {
			return err
		}
	}
	return tx.Commit()
}
