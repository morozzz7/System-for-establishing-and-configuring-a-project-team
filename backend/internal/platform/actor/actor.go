// Package actor carries the server-derived acting principal through request contexts.
//
// This MVP has no auth provider: the default actor is the server-side `system`
// principal. A client-supplied `decided_by` (or any other asserted identity)
// must be ignored by callers. This package never reads a client header; it only
// accepts values injected by trusted server-side code.
package actor

import (
	"context"
	"regexp"
)

// System is the server-side principal used when no actor is present in the context.
const System = "system"

var validActor = regexp.MustCompile(`^(emp_[a-z0-9]{4,12}|system)$`)

type contextKey struct{}

// WithContext returns a context carrying actor when it matches the contract
// pattern (`emp_[a-z0-9]{4,12}` or `system`). An invalid actor is ignored: the
// original context is returned unchanged, so it resolves to System (or to a
// valid actor stored earlier).
func WithContext(ctx context.Context, actor string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if !validActor.MatchString(actor) {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, actor)
}

// FromContext returns the actor stored in ctx, or System when the context is
// nil, holds no actor, or holds an invalid value.
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return System
	}
	actor, ok := ctx.Value(contextKey{}).(string)
	if !ok || !validActor.MatchString(actor) {
		return System
	}
	return actor
}
