package core_test

import (
	"context"
	"testing"

	"github.com/kees/atlas/internal/core"
)

func TestSearchFindsDescriptionAndFollowsEdits(t *testing.T) {
	e := setup(t)
	e.project("FT")
	ctx := context.Background()
	it, err := e.svc.CreateItem(ctx, e.admin, core.CreateItem{Project: "FT", Type: "task", Title: "Login", Description: "Rotate the keycloak certificate"})
	if err != nil {
		t.Fatal(err)
	}
	found := func(q string) int {
		page, err := e.svc.ListItems(ctx, e.admin, core.ItemFilter{Project: "FT", Q: q})
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		return len(page.Items)
	}
	if found("keycl") != 1 || found("login") != 1 || found(`"keycloak" AND (`) != 0 {
		t.Fatal("description prefix, title substring or hostile query misbehaves")
	}
	if _, err := e.svc.UpdateItem(ctx, e.admin, it.Key, it.Version, map[string]any{"description": "Renew the saml metadata"}); err != nil {
		t.Fatal(err)
	}
	if found("keycloak") != 0 || found("saml") != 1 {
		t.Fatal("index did not follow the description edit")
	}
}
