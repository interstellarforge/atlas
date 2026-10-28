package core

import "testing"

func TestSortClause(t *testing.T) {
	for _, name := range []string{"", "key", "title", "type", "status", "priority", "assignee", "due", "updated", "rank"} {
		if _, _, err := sortClause(name); err != nil {
			t.Errorf("sort %q: %v", name, err)
		}
		if name == "" {
			continue
		}
		if _, keyset, err := sortClause("-" + name); err != nil || keyset {
			t.Errorf("sort -%s: err=%v keyset=%v, want valid offset paging", name, err, keyset)
		}
	}
	if _, keyset, _ := sortClause("key"); !keyset {
		t.Error("key must keep keyset paging")
	}
	if _, _, err := sortClause("bogus"); err == nil {
		t.Error("unknown sort accepted")
	}
}
