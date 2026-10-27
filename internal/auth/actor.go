package auth

import (
	"context"
	"fmt"
	"slices"
)

const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)

type ActorKind string

const (
	KindHuman ActorKind = "human"
	KindToken ActorKind = "token"
)

// Actor is who performs an operation. Every mutation carries one.
type Actor struct {
	Kind    ActorKind
	UserID  int64
	TokenID int64
	IsAdmin bool
	// Scopes and Projects only apply to tokens; humans (sessions) have the user's full rights.
	Scopes []string
	// Projects limits a token to these project keys; nil means all the owner can access.
	Projects []string
	// CSRF is the session's CSRF token (humans only).
	CSRF string
}

// String gives the audit form: human:user-4, token:pat-17.
func (a Actor) String() string {
	if a.Kind == KindToken {
		return fmt.Sprintf("token:pat-%d", a.TokenID)
	}
	return fmt.Sprintf("human:user-%d", a.UserID)
}

func (a Actor) Can(scope string) bool {
	return a.Kind == KindHuman || slices.Contains(a.Scopes, scope)
}

// CanProject reports whether the actor may touch the project key.
func (a Actor) CanProject(key string) bool {
	return a.Projects == nil || slices.Contains(a.Projects, key)
}

type ctxKey int

const (
	actorKey ctxKey = iota
	holderKey
)

func WithActor(ctx context.Context, a Actor) context.Context {
	if h, ok := ctx.Value(holderKey).(*Holder); ok {
		h.Actor = &a
	}
	return context.WithValue(ctx, actorKey, a)
}

func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey).(Actor)
	return a, ok
}

// Holder lets outer middleware (logging) read the actor that inner middleware resolved.
type Holder struct{ Actor *Actor }

func WithHolder(ctx context.Context, h *Holder) context.Context {
	return context.WithValue(ctx, holderKey, h)
}
