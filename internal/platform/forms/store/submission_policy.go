package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
)

func RegisterSubmissionPolicy(ctx context.Context, tx pgx.Tx, profile string, maximum int, autoLock bool) error {
	if maximum > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO pf_forms.profile_submission_policy(profile,max_submissions,auto_lock)
		 VALUES($1,$2,$3) ON CONFLICT(profile) DO NOTHING`, profile, maximum, autoLock); err != nil {
			return err
		}
	}
	var stored int
	var locked bool
	err := tx.QueryRow(ctx, `SELECT max_submissions,auto_lock FROM pf_forms.profile_submission_policy WHERE profile=$1`, profile).Scan(&stored, &locked)
	if errors.Is(err, pgx.ErrNoRows) && maximum == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	if stored != maximum || locked != autoLock {
		return fmt.Errorf("immutable policy conflicts with startup configuration")
	}
	return nil
}

// EffectiveSpecOf leaves historical period_spec bytes intact. Only canonical
// period.profile selects the immutable owner rule, never caller metadata/GUCs.
func EffectiveSpecOf(ctx context.Context, tx pgx.Tx, periodID uuid.UUID) (domain.Spec, error) {
	spec, err := SpecOf(ctx, tx, periodID)
	if err != nil {
		return spec, err
	}
	var maximum int
	var autoLock bool
	err = tx.QueryRow(ctx, `SELECT r.max_submissions,r.auto_lock FROM pf_forms.period p
	 JOIN pf_forms.profile_submission_policy r ON r.profile=p.profile WHERE p.id=$1`, periodID).Scan(&maximum, &autoLock)
	if errors.Is(err, pgx.ErrNoRows) {
		return spec, nil
	}
	if err != nil {
		return spec, err
	}
	spec.MaxSubmissions, spec.AutoLockAtLimit = maximum, autoLock
	return spec, nil
}
