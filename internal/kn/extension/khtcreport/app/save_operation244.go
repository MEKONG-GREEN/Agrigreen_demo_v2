package app

import (
	"context"
	"fmt"
	"slices"
	"time"

	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	party "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Reject a wrong/currently read-only owner before DB or receipt lookup. The
// period-specific owner check still runs inside each transaction before data.
func (s *Service) saveCaller244(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return caller{}, err
	}
	if c.kn.ReadOnly() {
		return caller{}, problem.Forbidden
	}
	if c.kn.Role == port.SessionRoleAdmin {
		if !c.principal.MFA {
			return caller{}, problem.MFARequired
		}
	} else if c.kn.Role != port.SessionRoleProvince || c.kn.Department != party.DeptKeHoachTaiChinh {
		return caller{}, problem.Forbidden
	}
	return c, nil
}
func saveAuthority244(c caller) (string, error) {
	owner := c.kn
	// These fields do not grant authority and may change on a read/reload.
	owner.DefaultRoute = ""
	owner.ExpiresAt = time.Time{}
	if owner.Support != nil {
		support := *owner.Support
		support.Reason = ""
		owner.Support = &support
	}
	roles := slices.Clone(c.principal.Roles)
	slices.Sort(roles)
	return forms.OperationHash(struct {
		Tenant, Actor, Org uuid.UUID
		Profile            string
		MFA                bool
		Roles              []string
		Owner              port.SessionContext
	}{c.principal.TenantID, c.principal.ActorRef, c.principal.OrgRef, c.principal.SelectedSession.Profile(), c.principal.MFA, roles, owner})
}
func savePeriod244(ctx context.Context, tx pgx.Tx, c caller, id uuid.UUID) (ps.Period, string, error) {
	p, term, err := loadPeriod(ctx, tx, id)
	if err != nil {
		return ps.Period{}, "", err
	}
	if err = writer(c, p); err != nil {
		return ps.Period{}, "", err
	}
	return p, term, nil
}
func existingSave244(ctx context.Context, tx pgx.Tx, c caller, id, operation uuid.UUID, key, digest, authority string) (*store.SaveOperation, error) {
	var v *store.SaveOperation
	var err error
	if operation == uuid.Nil {
		v, err = store.SaveOperationByKey(ctx, tx, key)
	} else {
		v, err = store.SaveOperationByID(ctx, tx, operation)
	}
	if err != nil {
		return nil, err
	}
	if v != nil {
		if v.PeriodID != id || v.RequestKey != key || v.Applied.RequestHash != digest {
			return nil, problem.Conflict.WithDetail("Thao tác lưu đã gắn với yêu cầu khác")
		}
		if v.Applied.ActorRef != c.principal.ActorRef || v.AuthorityHash != authority {
			return nil, problem.Forbidden.WithDetail("Quyền của thao tác lưu đã thay đổi")
		}
		return v, nil
	}
	if operation == uuid.Nil {
		return nil, nil
	}
	other, err := store.SaveOperationByKey(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	if other != nil {
		return nil, problem.Conflict.WithDetail("Khóa lưu đã gắn với thao tác khác")
	}
	return nil, nil
}

func (s *Service) saveWithReceipt244(ctx context.Context, id uuid.UUID, in SaveInput, idem string) (Detail, error) {
	c, err := s.saveCaller244(ctx)
	if err != nil {
		return Detail{}, err
	}
	if idem == "" {
		return Detail{}, problem.IdempotencyRequired
	}
	if in.BaseVersion < 0 {
		return Detail{}, problem.Validation.WithField("base_version", "không âm")
	}
	if len(in.Admin) > len(domain.AdminKeys) || len(in.Texts) > 200 {
		return Detail{}, ErrInvalid.WithDetail("Quá nhiều ô nhập")
	}
	key := "khtc:report:save:" + id.String() + ":" + idem
	operationID := in.OperationID
	if operationID == uuid.Nil {
		operationID = ids.New()
	}
	keyHash, err := forms.OperationHash(key)
	if err != nil {
		return Detail{}, err
	}
	digest, err := forms.OperationHash(in)
	if err != nil {
		return Detail{}, err
	}
	authority, err := saveAuthority244(c)
	if err != nil {
		return Detail{}, err
	}
	var p ps.Period
	var term string
	var prior *store.Draft
	var operation *store.SaveOperation
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		var e error
		p, term, e = savePeriod244(ctx, tx, c, id)
		if e != nil {
			return e
		}
		operation, e = existingSave244(ctx, tx, c, id, in.OperationID, keyHash, digest, authority)
		if e != nil || operation != nil {
			return e
		}
		if term == pd.PeriodCancelled {
			return ErrCancelled
		}
		final, e := store.GetFinal(ctx, tx, id)
		if e != nil {
			return e
		}
		if final != nil {
			return ErrFinal
		}
		prior, e = store.LatestDraft(ctx, tx, id)
		if e != nil {
			return e
		}
		if !sameVersion(prior, in.BaseVersion) {
			return ErrVersion
		}
		return nil
	})
	if err != nil {
		return Detail{}, err
	}
	if operation != nil {
		return s.savedDetail244(ctx, id, *operation)
	}
	var before domain.Draft
	if prior == nil {
		data, defaults, e := s.capture(ctx, c, p)
		if e != nil {
			return Detail{}, e
		}
		before, e = s.initialDraft(ctx, c, data, defaults)
		if e != nil {
			return Detail{}, e
		}
		if e = s.pinNewTemplate(ctx, &before, term); e != nil {
			return Detail{}, e
		}
	} else {
		before = prior.Content
	}
	content, err := editFixedTemplate(before.TemplateSnapshot, before, domain.Edits{Admin: in.Admin, Texts: in.Texts, Adhoc: in.Adhoc}, c.principal.ActorRef, s.now())
	if err != nil {
		return Detail{}, ErrInvalid.WithCause(err)
	}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if e := store.LockSaveOperation(ctx, tx, c.principal.TenantID, operationID, keyHash); e != nil {
			return e
		}
		if e := store.Lock(ctx, tx, c.principal.TenantID, id); e != nil {
			return e
		}
		_, term, e := savePeriod244(ctx, tx, c, id)
		if e != nil {
			return e
		}
		operation, e = existingSave244(ctx, tx, c, id, in.OperationID, keyHash, digest, authority)
		if e != nil || operation != nil {
			return e
		}
		if term == pd.PeriodCancelled {
			return ErrCancelled
		}
		final, e := store.GetFinal(ctx, tx, id)
		if e != nil {
			return e
		}
		if final != nil {
			return ErrFinal
		}
		latest, e := store.LatestDraft(ctx, tx, id)
		if e != nil {
			return e
		}
		if !sameVersion(latest, in.BaseVersion) {
			return ErrVersion
		}
		if latest == nil {
			if e = checkNewFixedSelection(ctx, tx, c.principal.TenantID, content.TemplateSnapshot, content); e != nil {
				return e
			}
		}
		if e = captureTemplateTerminal(&content, term); e != nil {
			return e
		}
		row := store.Draft{ID: ids.New(), PeriodID: id, Action: "save", Content: content, InputHash: content.Data.InputHash, ActorRef: c.principal.ActorRef, At: s.now().Truncate(time.Microsecond)}
		if e = store.InsertDraft(ctx, tx, c.principal.TenantID, &row, key); e != nil {
			return fmt.Errorf("insert report draft: %w", e)
		}
		v := store.SaveOperation{OperationID: operationID, PeriodID: id, RequestKey: keyHash, AuthorityHash: authority, Applied: domain.SavedDraft{DraftID: row.ID, BaseVersion: in.BaseVersion, Version: row.Version, RequestHash: digest, ActorRef: row.ActorRef, At: row.At, Changes: domain.SaveChanges(before, content)}}
		if latest != nil {
			base := latest.ID
			v.Applied.BaseDraftID = &base
		}
		if e = store.InsertSaveOperation(ctx, tx, c.principal.TenantID, v); e != nil {
			return fmt.Errorf("insert report save audit/receipt: %w", e)
		}
		operation = &v
		return nil
	})
	if err != nil {
		return Detail{}, classify(err)
	}
	return s.savedDetail244(ctx, id, *operation)
}
func (s *Service) savedDetail244(ctx context.Context, id uuid.UUID, v store.SaveOperation) (Detail, error) {
	out, err := s.Get(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	receipt := v.Receipt()
	out.SaveReceipt = &receipt
	return out, nil
}

// ReadSaveOperation never executes or retries a mutation. No row remains unknown,
// including when the original transaction is still running or rolled back.
func (s *Service) ReadSaveOperation(ctx context.Context, id, operation uuid.UUID) (domain.SaveReceipt, error) {
	c, err := s.saveCaller244(ctx)
	if err != nil {
		return domain.SaveReceipt{}, err
	}
	authority, err := saveAuthority244(c)
	if err != nil {
		return domain.SaveReceipt{}, err
	}
	out := domain.SaveReceipt{OperationID: operation, PeriodID: id, Outcome: "unknown"}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if _, _, e := savePeriod244(ctx, tx, c, id); e != nil {
			return e
		}
		v, e := store.SaveOperationByID(ctx, tx, operation)
		if e != nil || v == nil {
			return e
		}
		if v.PeriodID != id {
			return ErrNotFound
		}
		if v.Applied.ActorRef != c.principal.ActorRef || v.AuthorityHash != authority {
			return problem.Forbidden.WithDetail("Quyền của thao tác lưu đã thay đổi")
		}
		out = v.Receipt()
		return nil
	})
	return out, err
}

// SaveAudit lists at most 50 actual new save receipts; it does not fabricate
// historical audit events for drafts saved before this capability existed.
func (s *Service) SaveAudit(ctx context.Context, id uuid.UUID) ([]domain.SaveReceipt, error) {
	c, err := s.saveCaller244(ctx)
	if err != nil {
		return nil, err
	}
	out := []domain.SaveReceipt{}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		if _, _, e := savePeriod244(ctx, tx, c, id); e != nil {
			return e
		}
		rows, e := store.SaveOperations(ctx, tx, id)
		if e != nil {
			return e
		}
		for _, v := range rows {
			out = append(out, v.Receipt())
		}
		return nil
	})
	return out, err
}
