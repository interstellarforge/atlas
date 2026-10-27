package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kees/atlas/internal/auth"
	"github.com/kees/atlas/internal/core"
)

var (
	itemURI = regexp.MustCompile(`^atlas://items/([A-Z][A-Z0-9]{1,9}-[0-9]+)$`)
)

// addResources offers the read-only views a client UI can attach to a conversation. They cost tokens
// when shown to a model, so they use the same compact shapes as the tools.
func addResources(s *sdk.Server, svc *core.Service, a auth.Actor) {
	add := func(uri, name, desc string, h func(ctx context.Context, m []string) (any, error), re *regexp.Regexp) {
		s.AddResourceTemplate(&sdk.ResourceTemplate{URITemplate: uri, Name: name, Description: desc, MIMEType: "application/json"},
			func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
				m := re.FindStringSubmatch(req.Params.URI)
				if m == nil {
					return nil, sdk.ResourceNotFoundError(req.Params.URI)
				}
				v, err := h(ctx, m)
				if err != nil {
					return nil, resourceError(err)
				}
				b, err := json.Marshal(v)
				if err != nil {
					return nil, err
				}
				if len(b) > maxResponse {
					return nil, errTooLarge
				}
				return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, MIMEType: "application/json", Text: string(b)}}}, nil
			})
	}
	add("atlas://projects/{key}/board", "board", "Compact board of a project: columns with counts and first cards.",
		func(ctx context.Context, m []string) (any, error) {
			return svc.Board(ctx, a, m[1], core.BoardFilter{Limit: boardLimit, Fields: boardFields})
		}, regexp.MustCompile(`^atlas://projects/([A-Z][A-Z0-9]{1,9})/board$`))
	add("atlas://projects/{key}/workflow", "workflow", "Item types, statuses, transitions, labels and custom fields of a project.",
		func(ctx context.Context, m []string) (any, error) {
			w, err := svc.GetWorkflow(ctx, a, m[1])
			if err != nil {
				return nil, err
			}
			return workflowView(w), nil
		}, regexp.MustCompile(`^atlas://projects/([A-Z][A-Z0-9]{1,9})/workflow$`))
	add("atlas://projects/{key}/context", "context", "Project knowledge for agents: pinned context entries in compact form (same as context_get bootstrap).",
		func(ctx context.Context, m []string) (any, error) {
			return svc.ContextBootstrapFor(ctx, a, m[1], nil, 0, "", 0)
		}, regexp.MustCompile(`^atlas://projects/([A-Z][A-Z0-9]{1,9})/context$`))
	add("atlas://items/{key}", "item", "One work item in detail, with its links.",
		func(ctx context.Context, m []string) (any, error) {
			it, err := svc.GetItem(ctx, a, m[1])
			if err != nil {
				return nil, err
			}
			links, err := svc.ListLinks(ctx, a, m[1])
			if err != nil {
				return nil, err
			}
			return map[string]any{"item": it, "links": links}, nil
		}, itemURI)
}

// resourceError keeps core messages and hides everything else behind a generic error (logged here).
func resourceError(err error) error {
	if info := core.ErrorInfo(err); info.Code != "internal" {
		return errors.New(info.Message)
	}
	slog.Error("mcp resource failed", "err", err)
	return errors.New("internal error")
}
