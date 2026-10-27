// Package mcp exposes Atlas over the Model Context Protocol (Streamable HTTP).
// Tools are thin adapters over core services; they hold no business logic.
package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
	"github.com/kees/atlas/internal/events"
)

const (
	serverName    = "atlas"
	serverVersion = "0.1.0"
	// searchLimit keeps default results small; agents page with next_cursor.
	searchLimit    = 25
	searchLimitMax = 100
	boardLimit     = 10
	boardLimitMax  = 50
	changesLimit   = 50
	recentComments = 10
)

// Handler serves /mcp. Each request gets its own stateless server bound to the actor the
// auth middleware resolved, so a revoked token stops working immediately and no actor
// outlives the request that authenticated it.
func Handler(svc *core.Service, feed *events.Feed, o Options) http.Handler {
	if o.WritesPerHour <= 0 {
		o.WritesPerHour = writesPerHour
	}
	if o.Metrics == nil {
		o.Metrics = NewMetrics()
	}
	g := &guards{
		writes:   newLimiter(o.WritesPerHour, time.Hour),
		archives: newLimiter(massArchivePerHour, time.Hour),
		metrics:  o.Metrics,
	}
	h := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		a, _ := auth.ActorFrom(r.Context())
		return newServer(svc, feed, g, a)
	}, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return rateLimit(newLimiter(requestsPerMinute, time.Minute), h)
}

// Options tunes the server; zero values use the defaults.
type Options struct {
	WritesPerHour int      // mutation quota per token per hour
	Metrics       *Metrics // shared so the API can serve the aggregate; nil keeps a private one
}

// guards are the process-wide counters shared by every per-request server.
type guards struct {
	writes, archives *limiter
	metrics          *Metrics
}

func newServer(svc *core.Service, feed *events.Feed, g *guards, a auth.Actor) *sdk.Server {
	writes := g.writes
	s := sdk.NewServer(&sdk.Implementation{Name: serverName, Version: serverVersion}, nil)
	s.AddReceivingMiddleware(logCalls(a, g.metrics))
	addResources(s, svc, a)
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
		return result(workflowView(w))
	})

	type searchIn struct {
		Project         string   `json:"project,omitempty" jsonschema:"project key; omit to search all accessible projects"`
		Type            string   `json:"type,omitempty" jsonschema:"epic, story, task or bug"`
		Status          string   `json:"status,omitempty" jsonschema:"status name"`
		Assignee        *int64   `json:"assignee,omitempty" jsonschema:"user id"`
		Priority        string   `json:"priority,omitempty" jsonschema:"low, normal, high or urgent"`
		Label           string   `json:"label,omitempty"`
		Milestone       string   `json:"milestone,omitempty" jsonschema:"milestone name"`
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
			Project: in.Project, Type: in.Type, Status: in.Status, Priority: in.Priority, Label: in.Label, Milestone: in.Milestone,
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
		Description: "Read one work item in detail: links, checklist and the last 10 comments. Pass fields to keep the result small.",
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
		checklist, err := svc.ListChecklist(ctx, a, in.Key)
		if err != nil {
			return nil, nil, err
		}
		comments, err := svc.ListComments(ctx, a, in.Key)
		if err != nil {
			return nil, nil, err
		}
		if len(comments) > recentComments {
			comments = comments[len(comments)-recentComments:]
		}
		return result(map[string]any{"item": it, "links": links, "checklist": checklist, "comments": comments})
	})

	type boardIn struct {
		Project string `json:"project" jsonschema:"project key"`
		Limit   int    `json:"limit,omitempty" jsonschema:"cards per column, default 10, max 50"`
	}
	sdk.AddTool(s, &sdk.Tool{
		Name:        "board_get",
		Description: "Compact board overview: every status column with its count and first cards, plus seq for changes_since.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in boardIn) (*sdk.CallToolResult, any, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = boardLimit
		}
		b, err := svc.Board(ctx, a, in.Project, core.BoardFilter{Limit: min(limit, boardLimitMax), Fields: boardFields})
		if err != nil {
			return nil, nil, err
		}
		return result(b)
	})

	type changesIn struct {
		Since int64 `json:"since" jsonschema:"seq from a previous result (board_get or changes_since); 0 starts at the oldest change"`
		Limit int   `json:"limit,omitempty" jsonschema:"max events, default 50, max 200"`
	}
	sdk.AddTool(s, &sdk.Tool{
		Name:        "changes_since",
		Description: "Changes you may see after a cursor. Continue from seq while more is true; resync_required means re-read via board_get or items_search.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in changesIn) (*sdk.CallToolResult, any, error) {
		if in.Since < 0 {
			return nil, nil, errors.New("since must be >= 0")
		}
		limit := in.Limit
		if limit <= 0 {
			limit = changesLimit
		}
		b, err := feed.Since(a, in.Since, min(limit, 200))
		if err != nil {
			return nil, nil, err
		}
		return result(b)
	})

	sdk.AddTool(s, &sdk.Tool{
		Name: "items_write",
		Description: "Create, update or move work items, 1-25 operations per call. Updates and moves need the item's version. " +
			"Atomic by default (first failure applies nothing); partial=true applies what it can and reports each result. " +
			"Set idempotency_key so a retry returns the first result instead of repeating the writes. " +
			"Archiving more than 10 items per hour needs confirm_mass_archive=true.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(false)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in writeIn) (*sdk.CallToolResult, any, error) {
		if r, ok := quota(writes, a, len(in.Operations)); !ok {
			return r, nil, nil
		}
		if n := in.archives(); n > 0 && !in.ConfirmMassArchive {
			if wait, ok := g.archives.take(a.String(), n); !ok {
				return errorResult(map[string]any{"error": core.BatchError{Code: "confirmation_required",
					Message: "more than " + strconv.Itoa(massArchivePerHour) + " archives per hour; repeat with confirm_mass_archive=true if intended (window resets in " + wait.Round(time.Second).String() + ")"},
					"applied": false})
			}
		}
		ops := make([]core.BatchOp, len(in.Operations))
		for i, o := range in.Operations {
			ops[i] = o.toCore()
		}
		out, err := svc.Batch(ctx, a, ops, in.Partial, in.IdempotencyKey)
		var oe *core.OpError
		if errors.As(err, &oe) {
			info := core.ErrorInfo(oe.Err)
			return errorResult(map[string]any{"error": info, "operation": oe.Index, "applied": false})
		}
		if err != nil {
			return nil, nil, err
		}
		return result(out)
	})

	type commentIn struct {
		Key  string `json:"key" jsonschema:"item key, e.g. EXO-42"`
		Body string `json:"body"`
	}
	sdk.AddTool(s, &sdk.Tool{
		Name:        "comment_add",
		Description: "Add a comment to a work item (recorded under your token). Use it for progress updates and questions.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(false)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in commentIn) (*sdk.CallToolResult, any, error) {
		if r, ok := quota(writes, a, 1); !ok {
			return r, nil, nil
		}
		c, err := svc.AddComment(ctx, a, in.Key, in.Body)
		if err != nil {
			return nil, nil, err
		}
		return result(c)
	})
	type checklistIn struct {
		Key string   `json:"key" jsonschema:"item key, e.g. EXO-42"`
		Add []string `json:"add,omitempty" jsonschema:"texts of new checklist items"`
		Set []struct {
			ID   int64   `json:"id"`
			Done *bool   `json:"done,omitempty"`
			Text *string `json:"text,omitempty"`
		} `json:"set,omitempty" jsonschema:"change existing items (tick, untick or rename)"`
		Remove []int64 `json:"remove,omitempty" jsonschema:"ids of items to remove"`
	}
	sdk.AddTool(s, &sdk.Tool{
		Name:        "checklist_write",
		Description: "Add, tick/untick, rename or remove checklist items of a work item. Items are changed one by one in the order add, set, remove; read the checklist with item_get.",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(false)},
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in checklistIn) (*sdk.CallToolResult, any, error) {
		n := len(in.Add) + len(in.Set) + len(in.Remove)
		if n == 0 || n > core.MaxBatch {
			return nil, nil, fmt.Errorf("give 1-%d changes", core.MaxBatch)
		}
		if r, ok := quota(writes, a, n); !ok {
			return r, nil, nil
		}
		var id [8]byte
		_, _ = rand.Read(id[:])
		ctx = core.WithBatch(ctx, hex.EncodeToString(id[:])) // one batch id groups this call's audit entries
		for _, text := range in.Add {
			if _, err := svc.AddChecklistItem(ctx, a, in.Key, text); err != nil {
				return nil, nil, err
			}
		}
		for _, c := range in.Set {
			if _, err := svc.UpdateChecklistItem(ctx, a, in.Key, c.ID, core.ChecklistPatch{Text: c.Text, Done: c.Done}); err != nil {
				return nil, nil, err
			}
		}
		for _, id := range in.Remove {
			if err := svc.RemoveChecklistItem(ctx, a, in.Key, id); err != nil {
				return nil, nil, err
			}
		}
		items, err := svc.ListChecklist(ctx, a, in.Key)
		if err != nil {
			return nil, nil, err
		}
		return result(map[string]any{"checklist": items})
	})

	type attachmentsIn struct {
		Key string `json:"key" jsonschema:"item key, e.g. EXO-42"`
	}
	sdk.AddTool(s, &sdk.Tool{
		Name:        "attachments",
		Description: "List an item's image attachments: filename, mime, size, pixels and a url path to fetch them (needs your token). No file contents are returned.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in attachmentsIn) (*sdk.CallToolResult, any, error) {
		list, err := svc.ListAttachments(ctx, a, in.Key)
		if err != nil {
			return nil, nil, err
		}
		out := make([]map[string]any, 0, len(list))
		for _, at := range list {
			out = append(out, map[string]any{"id": at.ID, "filename": at.Filename, "mime": at.Mime, "size": at.Size,
				"width": at.Width, "height": at.Height, "url": at.URL, "thumb_url": at.ThumbURL})
		}
		return result(map[string]any{"items": out})
	})

	return s
}

// quota charges n mutations to the actor's hourly budget; when it is spent the agent gets a readable error.
func quota(l *limiter, a auth.Actor, n int) (*sdk.CallToolResult, bool) {
	wait, ok := l.take(a.String(), n)
	if ok {
		return nil, true
	}
	r, _, _ := errorResult(map[string]any{"error": core.BatchError{Code: "rate_limited",
		Message: "write quota used up; retry in " + wait.Round(time.Second).String()}, "applied": false})
	return r, false
}

// result returns v as one compact JSON text block. Structured output is not set: it would
// repeat the same payload and double the tokens an agent pays for.
func result(v any) (*sdk.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	if len(b) > maxResponse {
		// Never cut JSON in half: tell the agent how to ask for less.
		b, _ = json.Marshal(map[string]any{"error": map[string]any{"code": "too_large",
			"message": "result is " + strconv.Itoa(len(b)) + " bytes (max " + strconv.Itoa(maxResponse) + "); use fields, a smaller limit or cursor"}})
		return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, nil, nil
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, nil, nil
}

// errorResult is a tool-level error the agent can read and act on (e.g. a 409 with the current state).
func errorResult(v any) (*sdk.CallToolResult, any, error) {
	r, _, err := result(v)
	if r != nil {
		r.IsError = true
	}
	return r, nil, err
}

func ptr[T any](v T) *T { return &v }

// boardFields keeps board cards small; agents fetch more with item_get.
var boardFields = []string{"key", "type", "title", "priority", "assignee", "version"}

type writeIn struct {
	Operations     []opIn `json:"operations" jsonschema:"1-25 operations"`
	Partial        bool   `json:"partial,omitempty" jsonschema:"apply operations independently instead of all-or-nothing"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// ConfirmMassArchive lifts the hourly archive guard; archiving is the only way to remove items.
	ConfirmMassArchive bool `json:"confirm_mass_archive,omitempty"`
}

func (w writeIn) archives() (n int) {
	for _, o := range w.Operations {
		if o.Op == "update" && o.Archived != nil && *o.Archived {
			n++
		}
	}
	return n
}

type opIn struct {
	Op          string         `json:"op" jsonschema:"create, update or move"`
	Key         string         `json:"key,omitempty" jsonschema:"item key (update, move)"`
	Version     int64          `json:"version,omitempty" jsonschema:"current item version (update, move)"`
	Project     string         `json:"project,omitempty" jsonschema:"project key (create)"`
	Type        string         `json:"type,omitempty" jsonschema:"epic, story, task or bug (create)"`
	Title       string         `json:"title,omitempty"`
	Description *string        `json:"description,omitempty"`
	Parent      string         `json:"parent,omitempty" jsonschema:"parent item key"`
	Status      string         `json:"status,omitempty" jsonschema:"status name; for move, the target column"`
	Priority    string         `json:"priority,omitempty" jsonschema:"low, normal, high or urgent"`
	Assignee    *int64         `json:"assignee,omitempty" jsonschema:"user id"`
	DueAt       *int64         `json:"due_at,omitempty" jsonschema:"unix milliseconds"`
	Labels      []string       `json:"labels,omitempty" jsonschema:"replaces the item's labels"`
	CustomData  map[string]any `json:"custom_data,omitempty" jsonschema:"custom field values (update merges; null removes a key)"`
	Archived    *bool          `json:"archived,omitempty" jsonschema:"update: archive or restore"`
	Milestone   string         `json:"milestone,omitempty" jsonschema:"milestone name (see workflow_get)"`
	Template    string         `json:"template,omitempty" jsonschema:"create: name of an item template of the project (see workflow_get); fills type, description, priority, labels, status and checklist you leave out"`
	Clear       []string       `json:"clear,omitempty" jsonschema:"update: fields to set to null: assignee, due_at, parent, milestone"`
	After       string         `json:"after,omitempty" jsonschema:"move: place after this item key"`
	Before      string         `json:"before,omitempty" jsonschema:"move: place before this item key"`
}

func (o opIn) toCore() core.BatchOp {
	op := core.BatchOp{Op: o.Op, Key: o.Key, Version: o.Version,
		Move: core.MoveSpec{Status: o.Status, After: o.After, Before: o.Before}}
	switch o.Op {
	case "create":
		desc := ""
		if o.Description != nil {
			desc = *o.Description
		}
		op.Create = core.CreateItem{Project: o.Project, Type: o.Type, Title: o.Title, Description: desc, Parent: o.Parent,
			Status: o.Status, Priority: o.Priority, Assignee: o.Assignee, DueAt: o.DueAt, Labels: o.Labels, CustomData: o.CustomData,
			Milestone: o.Milestone, Template: o.Template}
	case "update":
		p := map[string]any{}
		if o.Title != "" {
			p["title"] = o.Title
		}
		if o.Description != nil {
			p["description"] = *o.Description
		}
		if o.Status != "" {
			p["status"] = o.Status
		}
		if o.Priority != "" {
			p["priority"] = o.Priority
		}
		if o.Assignee != nil {
			p["assignee"] = *o.Assignee
		}
		if o.DueAt != nil {
			p["due_at"] = *o.DueAt
		}
		if o.Labels != nil {
			p["labels"] = o.Labels
		}
		if o.Parent != "" {
			p["parent"] = o.Parent
		}
		if o.CustomData != nil {
			p["custom_data"] = o.CustomData
		}
		if o.Milestone != "" {
			p["milestone"] = o.Milestone
		}
		if o.Archived != nil {
			p["archived"] = *o.Archived
		}
		for _, f := range o.Clear {
			p[f] = nil
		}
		// UpdateItem expects decoded JSON (float64, []any); normalise typed values the same way REST gets them.
		raw, _ := json.Marshal(p)
		_ = json.Unmarshal(raw, &p)
		op.Patch = p
	}
	return op
}

// workflowView is the compact workflow shape shared by workflow_get and the workflow resource.
func workflowView(w core.Workflow) map[string]any {
	statuses := make([]map[string]any, 0, len(w.Statuses))
	for _, st := range w.Statuses {
		m := map[string]any{"name": st.Name, "category": st.Category}
		if st.WIPLimit != nil {
			m["wip_limit"] = *st.WIPLimit
		}
		if !st.AllowCreate {
			m["allow_create"] = false // items cannot be created here, only moved in
		}
		statuses = append(statuses, m)
	}
	return map[string]any{
		"project": w.Project, "types": w.Types, "parents": w.Hierarchy, "statuses": statuses,
		"transitions": w.Transitions, "labels": w.Labels, "item_templates": w.ItemTemplates, "milestones": w.Milestones, "custom_fields": w.CustomFields,
	}
}

var errTooLarge = errors.New("resource is larger than " + strconv.Itoa(maxResponse) + " bytes")

// logCalls writes one compact line per tool call: what agents use, how big the answers are and how long they take.
func logCalls(a auth.Actor, m *Metrics) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			start := time.Now()
			res, err := next(ctx, method, req)
			call, ok := req.(*sdk.CallToolRequest)
			if !ok || method != "tools/call" {
				return res, err
			}
			text, isErr := "", err != nil
			if r, ok := res.(*sdk.CallToolResult); ok && r != nil {
				isErr = isErr || r.IsError
				for _, c := range r.Content {
					if t, ok := c.(*sdk.TextContent); ok {
						text += t.Text
					}
				}
			}
			ms := time.Since(start).Milliseconds()
			m.record(call.Params.Name, ms, text, isErr)
			slog.Info("mcp call", "tool", call.Params.Name, "actor", a.String(), "ms", ms, "bytes", len(text), "error", isErr)
			return res, err
		}
	}
}
