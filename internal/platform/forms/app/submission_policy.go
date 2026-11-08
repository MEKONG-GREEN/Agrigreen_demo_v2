package app

import (
	"context"
	"fmt"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/jackc/pgx/v5"
)

// Registration runs once before serving, from genuine owner configuration. The
// registry has no tenant business data and is immutable, including on restart.
func (s *Service) registerSubmissionPolicies() error {
	if s.DB == nil {
		return nil // Freezer-only construction has no native write capability.
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, name := range s.order {
		p := s.profiles[name]
		if err := store.RegisterSubmissionPolicy(ctx, tx, name, p.FixedMaxSubmissions, p.AutoLockAtLimit); err != nil {
			return fmt.Errorf("forms submission policy %s: %w", name, err)
		}
	}
	return tx.Commit(ctx)
}
