package app

import (
	"context"
	"encoding/json"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func cntsBatch(sr CNTSSeries) formsport.BatchInput {
	b := formsport.BatchInput{Profile: domain.ProfileCNTS, TemplateID: sr.Template.ID, UnitKind: sr.Form.UnitKind, Units: sr.Units}
	for _, p := range sr.Periods {
		b.Periods = append(b.Periods, formsport.PeriodDef{Label: p.Label, DueAt: p.DueAt, Kind: p.Kind, Key: p.Key, From: p.From, To: p.To, Rule: p.Rule})
	}
	return b
}

func (s *Service) publication275(ctx context.Context, in CNTSSeriesInput, key string) (CNTSSeries, formsport.BatchInput, store.CNTSPublishIntent, error) {
	empty := store.CNTSPublishIntent{}
	if err := s.AuthorizeCNTSPublish(ctx); err != nil {
		return CNTSSeries{}, formsport.BatchInput{}, empty, err
	}
	if len(key) < 8 || len(key) > 200 {
		return CNTSSeries{}, formsport.BatchInput{}, empty, problem.IdempotencyRequired
	}
	if in.TemplateID == uuid.Nil || in.TemplateVersion < 1 {
		return CNTSSeries{}, formsport.BatchInput{}, empty, problem.Validation.WithField("template_id", "chọn ID và phiên bản biểu bất biến trước khi phát hành")
	}
	sr, err := s.PreviewCNTSSeries(ctx, in)
	if err != nil {
		return sr, formsport.BatchInput{}, empty, err
	}
	p, k, err := port.RequireSession(ctx, port.SessionRoleProvince)
	if err != nil {
		return sr, formsport.BatchInput{}, empty, err
	}
	// Current commune scope is checked before an intent or cache can be used.
	// The same public facility directory used by native Forms is checked below.
	if sr.Form.UnitKind == domain.UnitCommune {
		for _, u := range sr.Units {
			if !k.Scope.Allows(u) {
				return sr, formsport.BatchInput{}, empty, problem.Forbidden
			}
		}
	}
	if sr.Form.UnitKind == domain.UnitFacility {
		if s.CNTSDirectory == nil {
			return sr, formsport.BatchInput{}, empty, ErrDependency.WithDetail("chưa nối danh mục cơ sở để kiểm trước phát hành")
		}
		refs := make([]uuid.UUID, len(sr.Units))
		for i, u := range sr.Units {
			var err error
			refs[i], err = uuid.Parse(u)
			if err != nil {
				return sr, formsport.BatchInput{}, empty, formsport.ErrFacilityInvalid
			}
		}
		parents, err := s.CNTSDirectory.FacilityCommunes(ctx, refs, sr.Periods[0].From)
		if err != nil {
			return sr, formsport.BatchInput{}, empty, err
		}
		for _, ref := range refs {
			parent, ok := parents[ref]
			if !ok || parent == "" {
				return sr, formsport.BatchInput{}, empty, formsport.ErrFacilityInvalid
			}
			if !k.Scope.Allows(parent) {
				return sr, formsport.BatchInput{}, empty, problem.Forbidden
			}
		}
	}
	b := cntsBatch(sr)
	hash, err := formsport.OperationHash(in)
	if err != nil {
		return sr, b, empty, err
	}
	batchHash, err := formsport.OperationHash(b)
	if err != nil {
		return sr, b, empty, err
	}
	authority, err := formsport.OperationHash(struct {
		Session port.SessionContext
		MFA     bool
		Org     uuid.UUID
	}{k, p.MFA, p.OrgRef})
	if err != nil {
		return sr, b, empty, err
	}
	rawRequest, err := json.Marshal(in)
	if err != nil {
		return sr, b, empty, err
	}
	rawBatch, err := json.Marshal(b)
	if err != nil {
		return sr, b, empty, err
	}
	rawSeries, err := json.Marshal(sr)
	if err != nil {
		return sr, b, empty, err
	}
	return sr, b, store.CNTSPublishIntent{Key: key, ActorRef: p.ActorRef, SessionRef: k.ID, AuthorityHash: authority, RequestHash: hash, BatchHash: batchHash, Request: rawRequest, Batch: rawBatch, Series: rawSeries, EventTime: s.now()}, nil
}

func sameCNTSIntent(a, b store.CNTSPublishIntent) bool {
	return a.Key == b.Key && a.ActorRef == b.ActorRef && a.SessionRef == b.SessionRef && a.AuthorityHash == b.AuthorityHash && a.RequestHash == b.RequestHash && a.BatchHash == b.BatchHash
}

// CheckCNTSPublication is read-only and executes before HTTP cache replay.
func (s *Service) CheckCNTSPublication(ctx context.Context, in CNTSSeriesInput, key string) error {
	_, _, candidate, err := s.publication275(ctx, in, key)
	if err != nil {
		return err
	}
	p, _, err := port.RequireSession(ctx)
	if err != nil {
		return err
	}
	return s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		existing, err := store.CNTSPublishIntentByKey(ctx, tx, key)
		if err != nil {
			return err
		}
		if existing != nil && !sameCNTSIntent(*existing, candidate) {
			return problem.IdempotencyConflict
		}
		return nil
	})
}

// publishCNTS275 keeps the dispatch command before external I/O. Native
// DurablePublisher returns its original immutable creation receipts after lost
// ACK; no current period/event head is manufactured as a creation receipt.
func (s *Service) publishCNTS275(ctx context.Context, in CNTSSeriesInput, key string) (CNTSSeries, error) {
	sr, b, candidate, err := s.publication275(ctx, in, key)
	if err != nil {
		return sr, err
	}
	durable, ok := s.FormsNative.(formsport.DurablePublisher)
	if !ok {
		return sr, ErrDependency.WithDetail("chưa nối phát hành kỳ có biên nhận bền")
	}
	p, _, err := port.RequireSession(ctx)
	if err != nil {
		return sr, err
	}
	candidate.ID = ids.New()
	var intent *store.CNTSPublishIntent
	var receipt json.RawMessage
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockCNTSPublish(ctx, tx, p.TenantID, key); err != nil {
			return err
		}
		var err error
		intent, err = store.CNTSPublishIntentByKey(ctx, tx, key)
		if err != nil {
			return err
		}
		if intent == nil {
			if err := store.InsertCNTSPublishIntent(ctx, tx, p.TenantID, candidate); err != nil {
				return err
			}
			intent = &candidate
		} else if !sameCNTSIntent(*intent, candidate) {
			return problem.IdempotencyConflict
		}
		receipt, err = store.CNTSPublishReceipt(ctx, tx, intent.ID)
		return err
	})
	if err != nil {
		return sr, err
	}
	if receipt != nil {
		if err := json.Unmarshal(receipt, &sr); err != nil {
			return CNTSSeries{}, ErrDependency.WithCause(err)
		}
		return sr, nil
	}
	// The durable, stored command is the only dispatch input on retry.
	if json.Unmarshal(intent.Batch, &b) != nil || json.Unmarshal(intent.Series, &sr) != nil {
		return CNTSSeries{}, ErrDependency
	}
	if sr.Published, err = durable.PublishBatchOnce(ctx, b, "cnts:publish275:"+intent.ID.String()); err != nil {
		return sr, err
	}
	publication, err := s.cntsPublication(ctx, in, &sr, "cnts275:"+intent.ID.String())
	if err != nil {
		return sr, err
	}
	publication.EventTime = intent.EventTime
	sr.PublicationID = &publication.ID
	receipt, err = json.Marshal(sr)
	if err != nil {
		return sr, err
	}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockCNTSPublish(ctx, tx, p.TenantID, key); err != nil {
			return err
		}
		existing, err := store.CNTSPublishReceipt(ctx, tx, intent.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			receipt = existing
			return nil
		}
		if err := store.InsertCNTSPublication(ctx, tx, p.TenantID, publication); err != nil {
			return err
		}
		return store.InsertCNTSPublishReceipt(ctx, tx, p.TenantID, *intent, receipt)
	})
	if err == nil && json.Unmarshal(receipt, &sr) != nil {
		return CNTSSeries{}, ErrDependency
	}
	return sr, err
}
