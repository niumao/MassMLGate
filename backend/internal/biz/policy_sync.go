package biz

import "context"

// PolicySync keeps Casbin's grouping policy (user -> role -> domain) in sync
// with the users table as users are created/updated/deleted, so a new or
// changed user doesn't need a server restart to pick up their permissions.
// Implemented by internal/pkg/casbin.
type PolicySync interface {
	SyncUser(ctx context.Context, u *User) error
	RemoveUser(ctx context.Context, userID string) error
}
