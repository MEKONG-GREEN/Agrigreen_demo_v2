// Package tenant là bản giả của internal/shared/tenant cho kiểm thử archlint.
package tenant

import "context"

type ctxKey struct{}

type Grant struct{ from, to [16]byte }

func WithID(ctx context.Context, id [16]byte) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func MintGrant(from, to [16]byte) Grant { return Grant{from: from, to: to} }

func WithGrant(ctx context.Context, g Grant) (context.Context, error) { return WithID(ctx, g.to), nil }

// Trong chính gói tenant: không kiểm.
func self(ctx context.Context) context.Context {
	return WithID(ctx, MintGrant([16]byte{}, [16]byte{}).to)
}

var _ = self
