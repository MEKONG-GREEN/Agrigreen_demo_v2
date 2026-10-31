package app

import (
	"context"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"slices"
	"strings"
)

type OperationAuthority struct{ DB tenant.Beginner }

var _ ext.KHTCOperationSource = OperationAuthority{}

func (a OperationAuthority) VerifyKHTCOperation(ctx context.Context, p authz.Principal, k ext.SessionContext, proof ext.KHTCOperationProof, op forms.Operation) (bool, error) {
	if a.DB == nil || proof.Expected != op || proof.SessionID != k.ID {
		return false, nil
	}
	allowed := false
	err := tenant.WithTenantTx(ctx, a.DB, p.TenantID, func(tx pgx.Tx) error {
		if op.Action == forms.OperationPublishBatch {
			x, err := store.AdhocOpenByID(ctx, tx, proof.IntentID)
			if err != nil {
				return err
			}
			if x == nil || x.ActorRef != p.ActorRef || x.EffectiveSessionID != k.ID || x.LeaseGeneration != proof.LeaseGeneration || x.ContextHash != openContextHash(caller{p: p, k: k}) || x.NativeKey != op.IdemKey || x.BatchHash != op.PayloadHash || !validOpenIntent(*x) {
				return nil
			}
			hash, err := forms.OperationHash(x.Batch)
			if err != nil {
				return err
			}
			if hash != x.BatchHash {
				return nil
			}
			// A fixed period can never be authorized through an adhoc intent.
			existing, err := store.PeriodByID(ctx, tx, x.PlannedPeriodID)
			if err != nil {
				return err
			}
			if existing != nil && existing.Type != domain.TypeAdhoc {
				return nil
			}
			allowed = true
			return nil
		}
		actual, err := store.PeriodByFormsID(ctx, tx, op.PeriodID)
		if err != nil {
			return err
		}
		if actual == nil || actual.ID != proof.KNPeriodID || actual.Type != domain.TypeAdhoc {
			return nil
		}
		events, err := store.Events(ctx, tx, []uuid.UUID{actual.ID})
		if err != nil {
			return err
		}
		if terminalOf(events[actual.ID]) != "" {
			return nil
		}
		allowed = true
		return nil
	})
	return allowed, err
}
func validOpenIntent(x store.AdhocOpenIntent) bool {
	p, b := x.Plan, x.Batch
	if len(x.Input) > 65536 || x.ID == uuid.Nil || x.PlannedPeriodID == uuid.Nil || x.SessionID == uuid.Nil || x.ActorRef == uuid.Nil || b.TemplateID == uuid.Nil || len(x.Departments) < 1 || len(x.Departments) > 64 {
		return false
	}
	var input OpenInput
	if json.Unmarshal(x.Input, &input) != nil || input.Request.Type != domain.TypeAdhoc {
		return false
	}
	inputHash, err := forms.OperationHash(input)
	if err != nil || inputHash != x.RequestHash {
		return false
	}
	expected, err := domain.PlanOf(rc.DefaultRule, input.Request, clock.Zone())
	if err != nil {
		return false
	}
	expected, err = expected.Apply(input.Overrides, clock.Zone())
	if err != nil {
		return false
	}
	expectedHash, err := forms.OperationHash(expected)
	if err != nil {
		return false
	}
	planHash, err := forms.OperationHash(p)
	if err != nil || planHash != expectedHash {
		return false
	}
	if len(input.Departments) > 0 {
		expectedDepartments := []string{}
		for _, d := range input.Departments {
			d = strings.TrimSpace(d)
			if !slices.Contains(expectedDepartments, d) {
				expectedDepartments = append(expectedDepartments, d)
			}
		}
		if !slices.Equal(expectedDepartments, x.Departments) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, d := range x.Departments {
		if !deptRe.MatchString(d) || seen[d] {
			return false
		}
		seen[d] = true
	}
	for _, source := range x.Sources {
		if !seen[source.Department] {
			return false
		}
	}
	if p.Type != domain.TypeAdhoc || p.FormsKind != "adhoc" || b.Profile != domain.ProfileKHTC || b.UnitKind != forms.UnitDepartment || b.MaxSubmissions == nil || *b.MaxSubmissions != 3 || len(b.Periods) != 1 || !slices.Equal(b.Units, x.Departments) || len(b.UnitParents) != 0 {
		return false
	}
	v := b.Periods[0]
	return v.Kind == p.FormsKind && v.Key == p.FormsKey && v.Label == p.Name && v.From.Equal(p.From) && v.To.Equal(p.To) && v.DueAt.Equal(p.DueAt) && v.Rule == p.Rule
}
func BindAdhocOperation(ctx context.Context, db tenant.Beginner, k ext.SessionContext, period uuid.UUID, op forms.Operation) context.Context {
	return ext.WithKHTCOperation(ctx, OperationAuthority{DB: db}, ext.KHTCOperationProof{KNPeriodID: period, SessionID: k.ID, Expected: op})
}

func cancelNativeContext(ctx context.Context, db tenant.Beginner, k ext.SessionContext, p store.Period, reason, key string) context.Context {
	if p.Type != domain.TypeAdhoc {
		return ctx
	}
	hash, _ := forms.OperationHash(struct{ Reason string }{strings.TrimSpace(reason)})
	return BindAdhocOperation(ctx, db, k, p.ID, forms.Operation{Action: forms.OperationCancel, Profile: domain.ProfileKHTC, PeriodID: p.FormsPeriodRef, PayloadHash: hash, IdemKey: key})
}
