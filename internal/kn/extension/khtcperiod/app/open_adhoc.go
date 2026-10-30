package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
)

var ErrAdhocOpenConflict = problem.New(http.StatusConflict, "extension.khtc_adhoc_open_idempotency", "Thao tác mở kỳ đã dùng cho nội dung, người gọi hoặc phiên khác")

func sameOpenCaller(c caller, x *store.AdhocOpenIntent, hash string) bool {
	return x.ActorRef == c.p.ActorRef && x.EffectiveSessionID == c.k.ID && x.ContextHash == openContextHash(c) && x.RequestHash == hash
}
func (s *Service) openAdhoc(ctx context.Context, c caller, in OpenInput, idem string) (Detail, error) {
	if !c.k.ExpiresAt.IsZero() && !c.k.ExpiresAt.After(s.now()) {
		return Detail{}, problem.Forbidden.WithDetail("Ngữ cảnh phiên đã hết hạn")
	}
	if len(idem) > 200 || len(in.Departments) > 64 {
		return Detail{}, problem.Validation.WithDetail("Thao tác hoặc danh sách phòng quá lớn")
	}
	in.Departments = append([]string{}, in.Departments...)
	in.Request.Name = strings.TrimSpace(in.Request.Name)
	// Validate raw range/name before hashing or reading a durable intent.
	if _, err := domain.PlanOf(rc.DefaultRule, in.Request, clock.Zone()); err != nil {
		return Detail{}, inputErr(err)
	}
	for i, d := range in.Departments {
		if len(d) > 64 {
			return Detail{}, problem.Validation
		}
		in.Departments[i] = strings.TrimSpace(d)
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return Detail{}, err
	}
	hash, err := forms.OperationHash(in)
	if err != nil {
		return Detail{}, err
	}
	nativeKey := "khtc:" + idem
	var intent *store.AdhocOpenIntent
	if err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; intent, e = store.AdhocOpenByKey(ctx, tx, nativeKey); return e }); err != nil {
		return Detail{}, err
	}
	if intent != nil && !sameOpenCaller(c, intent, hash) {
		return Detail{}, ErrAdhocOpenConflict
	}
	if intent == nil {
		draft, err := s.draft(ctx, c, in, true)
		if err != nil {
			return Detail{}, err
		}
		ref, err := s.Forms.LatestTemplate(ctx, domain.ProfileKHTC, domain.TemplateCode)
		if err != nil {
			return Detail{}, err
		}
		plan := draft.Plan
		depts := make([]string, 0, len(draft.Departments))
		for _, d := range draft.Departments {
			depts = append(depts, d.Code)
		}
		if len(draft.Sources) > 10000 {
			return Detail{}, ErrDependency.WithDetail("Quá nhiều kỳ nguồn")
		}
		max := 3
		batch := forms.BatchInput{Profile: domain.ProfileKHTC, TemplateID: ref.ID, UnitKind: forms.UnitDepartment, Units: depts, MaxSubmissions: &max, Periods: []forms.PeriodDef{{Label: plan.Name, DueAt: plan.DueAt, Kind: plan.FormsKind, Key: plan.FormsKey, From: plan.From, To: plan.To, Rule: plan.Rule}}}
		batchHash, err := forms.OperationHash(batch)
		if err != nil {
			return Detail{}, err
		}
		candidate := store.AdhocOpenIntent{ID: ids.New(), PlannedPeriodID: ids.New(), ActorRef: c.p.ActorRef, SessionID: c.k.ID, EffectiveSessionID: c.k.ID, ContextHash: openContextHash(c), SessionExpiresAt: c.k.ExpiresAt, EffectiveExpiresAt: c.k.ExpiresAt, NativeKey: nativeKey, RequestHash: hash, BatchHash: batchHash, Input: raw, Plan: plan, Batch: batch, Departments: depts, Sources: draft.Sources, At: s.now()}
		if candidate.Sources == nil {
			candidate.Sources = []ext.KHTCSourceRef{}
		}
		if err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			if e := store.LockAdhocOpen(ctx, tx, c.p.TenantID, nativeKey); e != nil {
				return e
			}
			var e error
			intent, e = store.AdhocOpenByKey(ctx, tx, nativeKey)
			if e != nil {
				return e
			}
			if intent != nil {
				if !sameOpenCaller(c, intent, hash) {
					return ErrAdhocOpenConflict
				}
				return nil
			}
			if e = store.InsertAdhocOpen(ctx, tx, c.p.TenantID, candidate); e != nil {
				return e
			}
			intent = &candidate
			return nil
		}); err != nil {
			return Detail{}, err
		}
	}
	if !validOpenIntent(*intent) {
		return Detail{}, ErrDependency.WithDetail("Ý định mở kỳ không hợp lệ")
	}
	var existing *store.Period
	if err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		existing, e = store.PeriodByID(ctx, tx, intent.PlannedPeriodID)
		return e
	}); err != nil {
		return Detail{}, err
	}
	if existing != nil {
		return s.Get(ctx, existing.ID)
	}
	op := forms.Operation{Action: forms.OperationPublishBatch, Profile: domain.ProfileKHTC, PayloadHash: intent.BatchHash, IdemKey: intent.NativeKey}
	nativeCtx := ext.WithKHTCOperation(ctx, OperationAuthority{DB: s.DB}, ext.KHTCOperationProof{IntentID: intent.ID, SessionID: c.k.ID, LeaseGeneration: intent.LeaseGeneration, Expected: op})
	// Native publish owns its transaction and idempotency. On an unknown outcome we
	// retain the intent and retry this exact batch/key; never blindly cancel success.
	durable, ok := s.Forms.(forms.DurablePublisher)
	if !ok {
		return Detail{}, ErrDependency.WithDetail("Chưa nối cổng phát hành có biên nhận bền")
	}
	published, err := durable.PublishBatchOnce(nativeCtx, intent.Batch, intent.NativeKey)
	if err != nil {
		return Detail{}, err
	}
	if len(published) != 1 {
		return Detail{}, problem.Internal.WithDetail("cap/forms trả số kỳ khác1")
	}
	plan := intent.Plan
	row := store.Period{ID: intent.PlannedPeriodID, Type: domain.TypeAdhoc, Year: plan.Year, Name: plan.Name, From: plan.From, To: plan.To, Cutoff: plan.Cutoff, DueAt: plan.DueAt, ExportDueAt: plan.ExportDueAt, MaxSubmissions: 3, Departments: intent.Departments, FormsPeriodRef: published[0].ID, TemplateCode: domain.TemplateCode, Rule: plan.Rule, ActorRef: c.p.ActorRef, EventTime: intent.At}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if e := store.LockAdhocOpen(ctx, tx, c.p.TenantID, nativeKey); e != nil {
			return e
		}
		current, e := store.AdhocOpenByID(ctx, tx, intent.ID)
		if e != nil {
			return e
		}
		if current == nil || !sameOpenCaller(c, current, hash) || current.LeaseGeneration != intent.LeaseGeneration {
			return ErrAdhocOpenConflict
		}
		previous, e := store.PeriodByID(ctx, tx, row.ID)
		if e != nil {
			return e
		}
		if previous != nil {
			return nil
		}
		if e = store.InsertPeriod(ctx, tx, c.p.TenantID, &row, openIdem(idem)); e != nil {
			return e
		}
		for i, source := range intent.Sources {
			sr := store.Source{ID: ids.New(), PeriodID: row.ID, Department: source.Department, Source: source.Source, CalendarID: source.CalendarID, CalendarVersion: source.CalendarVersion, NativeKey: source.NativeKey}
			if source.Label != "" {
				label := source.Label
				sr.Label = &label
			}
			if e = store.InsertSource(ctx, tx, c.p.TenantID, &sr, c.p.ActorRef, intent.At, openIdem(idem)+":src:"+fmt.Sprint(i)); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return Detail{}, classify(err)
	}
	return s.Get(ctx, row.ID)
}
