package httpapi

import (
	"context"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

type ctxKey int

const userCtxKey ctxKey = iota

func withUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// userFromContext returns the authenticated user, or nil if unauthenticated.
func userFromContext(ctx context.Context) *store.User {
	u, _ := ctx.Value(userCtxKey).(*store.User)
	return u
}
