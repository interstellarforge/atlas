// Package mcp exposes Atlas over the Model Context Protocol (Streamable HTTP).
// Tools are thin adapters over core services; they hold no business logic.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
)

const (
	serverName    = "atlas"
	serverVersion = "0.1.0"
	// searchLimit keeps default results small; agents page with next_cursor.
	searchLimit    = 25
	searchLimitMax = 100
)

// Handler serves /mcp. Each request gets its own stateless server bound to the actor the
// auth middleware resolved, so a revoked token stops working immediately and no actor
// outlives the request that authenticated it.
func Handler(svc *core.Service) http.Handler {
	return sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		a, _ := auth.ActorFrom(r.Context())
		return newServer(svc, a)
	}, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

func newServer(svc *core.Service, a auth.Actor) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: serverName, Version: serverVersion}, nil)
	readOnly := &sdk.ToolAnnotations{ReadOnlyHint: true}

	sdk.AddTool(s, &sdk.Tool{
		Name:        "projects_list",
		Description: "List the projects you can access (key, name, archived).",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
		ps, err := svc.ListProjects(ctx, a)
		if err != nil {
			return nil, nil, err
		}
		items := make([]map[string]any, 0, len(ps))
		for _, p := range ps {
			items = append(items, map[string]any{"key": p.Key, "name": p.Name, "archived": p.ArchivedAt != nil})
		}
		return result(map[string]any{"items": items})
	})

	type workflowIn struct {
		Project string `json:"project" jsonschema:"project key, e.g. EXO"`
	}
	sdk.AddTool(s, &sdk.Tool{
		Name:        "workflow_get",
		Description: "Get a project's item types, allowed parents, statuses, status transitions, labels and custom fields.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in workflowIn) (*sdk.CallToolResult, any, error) {
		w, err := svc.GetWorkflow(ctx, a, in.Project)
		if err != nil {
			return nil, nil, err
		}
		statuses := make([]map[string]any, 0, len(w.Statuses))
		for _, st := range w.Statuses {
			m := map[string]any{"name": st.Name, "category": st.Category}
			if st.WIPLimit != nil {
				m["wip_limit"] = *st.WIPLimit
			}
			statuses = append(statuses, m)
		}
		return result(map[string]any{
			"project": w.Project, "types": w.Types, "parents": w.Hierarchy, "statuses": statuses,
			"transitions": w.Transitions, "labels": w.Labels, "custom_fields": w.CustomFields,
		})
	})

	type searchIn struct {
		Project         string   `json:"project,omitempty" jsonschema:"project key; omit to search all accessible projects"`
		Type            string   `json:"type,omitempty" jsonschema:"epic, story, task or bug"`
		Status          string   `json:"status,omitempty" jsonschema:"status name"`
		Assignee        *int64   `json:"assignee,omitempty" jsonschema:"user id"`
		Priority        string   `json:"priority,omitempty" jsonschema:"low, normal, high or urgent"`
		Label           string   `json:"label,omitempty"`
		Parent          string   `json:"parent,omitempty" jsonschema:"parent item key, e.g. EXO-12"`
		Under           string   `json:"under,omitempty" jsonschema:"item key; returns that item and all its descendants"`
		Query           string   `json:"query,omitempty" jsonschema:"case-insensitive title substring"`
		UpdatedSince    *int64   `json:"updated_since,omitempty" jsonschema:"unix milliseconds"`
		IncludeArchived bool     `json:"include_archived,omitempty"`
		Fields          []string `json:"fields,omitempty" jsonschema:"fields to return; default key,type,title,status,priority,assignee,parent,blocked,version"`
		Limit           int      `json:"limit,omitempty" jsonschema:"page size, default 25, max 100"`
		Cursor          string   `json:"cursor,omitempty" jsonschema:"next_cursor from a previous result"`
	}
	sdk.AddTool(s, &sdk.Tool{
		Name:        "items_search",
		Description: "Search and filter work items. Compact by default; use fields to request more (e.g. description, labels).",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in searchIn) (*sdk.CallToolResult, any, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = searchLimit
		}
		page, err := svc.ListItems(ctx, a, core.ItemFilter{
			Project: in.Project, Type: in.Type, Status: in.Status, Priority: in.Priority, Label: in.Label,
			Parent: in.Parent, Under: in.Under, Q: in.Query, Assignee: in.Assignee, UpdatedSince: in.UpdatedSince,
			IncludeArchived: in.IncludeArchived, Cursor: in.Cursor, Limit: min(limit, searchLimitMax), Fields: in.Fields,
		})
		if err != nil {
			return nil, nil, err
		}
		return result(page)
	})

	type getIn struct {
		Key    string   `json:"key" jsonschema:"item key, e.g. EXO-42"`
		Fields []string `json:"fields,omitempty" jsonschema:"limit the result to these fields; default is the full item with links"`
	}
	sdk.AddTool(s, &sdk.Tool{
		Name:        "item_get",
		Description: "Read one work item in detail, including its links. Pass fields to keep the result small.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in getIn) (*sdk.CallToolResult, any, error) {
		it, err := svc.GetItem(ctx, a, in.Key)
		if err != nil {
			return nil, nil, err
		}
		if len(in.Fields) > 0 {
			m, err := core.Select(it, in.Fields)
			if err != nil {
				return nil, nil, err
			}
			return result(m)
		}
		links, err := svc.ListLinks(ctx, a, in.Key)
		if err != nil {
			return nil, nil, err
		}
		return result(map[string]any{"item": it, "links": links})
	})

	return s
}

// result returns v as one compact JSON text block. Structured output is not set: it would
// repeat the same payload and double the tokens an agent pays for.
func result(v any) (*sdk.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, nil, nil
}
