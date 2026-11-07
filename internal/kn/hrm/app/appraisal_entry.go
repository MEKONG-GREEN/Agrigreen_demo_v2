package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

type AppraisalItem struct {
	LegacyReadOnly bool `json:"legacy_read_only"`
	domain.AppraisalEntry
	ID             uuid.UUID                `json:"id"`
	PeriodID       uuid.UUID                `json:"period_id"`
	Version        int64                    `json:"version"`
	RecordedAt     time.Time                `json:"recorded_at"`
	Actor          uuid.UUID                `json:"actor_ref"`
	Action         string                   `json:"action"`
	ChangeReason   string                   `json:"change_reason,omitempty"`
	QuarterResults []AppraisalQuarterResult `json:"quarter_results,omitempty"`
	Name           string                   `json:"name,omitempty"` // chỉ được ghép trong bộ nhớ, sau audit và ngoài TX
	CanManage      bool                     `json:"can_manage"`
	CanCenter      bool                     `json:"can_center"`
	SelfLate       bool                     `json:"self_late"`
	ManagerLate    bool                     `json:"manager_late"`
}

func appraisalReadOnly(item *AppraisalItem, legacy bool) {
	item.LegacyReadOnly = legacy
	if legacy {
		item.CanManage, item.CanCenter = false, false
	}
}

func appraisalEntryKey(period, staff uuid.UUID) string { return period.String() + ":" + staff.String() }

func appraisalSubmissionTimes(e *domain.AppraisalEntry, action string, now time.Time) {
	at := now.UTC()
	switch action {
	case "manager_submit":
		e.ManagerSubmittedAt = &at
	case "reopen", "return":
		e.ManagerSubmittedAt = nil
	}
}

func appraisalLate(item *AppraisalItem, p AppraisalPeriod, now time.Time) {
	if item.Member.ExcludedReason != "" {
		return
	}
	if item.ManagerSubmittedAt != nil {
		item.ManagerLate = item.ManagerSubmittedAt.After(p.ManagerDueAt)
	} else if item.Status == domain.AppraisalNotStarted || item.Status == domain.AppraisalChangeRequested {
		item.ManagerLate = now.After(p.ManagerDueAt)
	}
}

func appraisalItem(row store.AppraisalEvent, c appraisalActor) (AppraisalItem, error) {
	e, err := appraisalsReadObject[domain.AppraisalEntry](row)
	if err != nil {
		return AppraisalItem{}, err
	}
	v := AppraisalItem{AppraisalEntry: e, ID: row.ID, Version: row.Seq, Actor: row.Actor, RecordedAt: row.RecordedAt, Action: row.Action, ChangeReason: row.Reason}
	if row.PeriodRef != nil {
		v.PeriodID = *row.PeriodRef
	}
	// Chỉ trả tham chiếu quý có phân công đóng băng mà caller hiện tại được đọc.
	for _, q := range e.QuarterResults {
		if c.canRead(q.Member) {
			v.QuarterResults = append(v.QuarterResults, AppraisalQuarterResult{Quarter: q.Quarter, PeriodID: q.PeriodID, EntryVersion: q.EntryVersion, Version: q.Version, Status: q.Status, Rating: q.Rating})
		}
	}
	a := appraisalAuthority(c)
	v.CanManage = c.canRead(e.Member) && domain.CanManageAppraisal(a, e.Member)
	v.CanCenter = c.center() && c.scope == domain.ScopeAll && !c.readOnly
	return v, nil
}

func loadAppraisalPeriod(ctx context.Context, tx pgx.Tx, id uuid.UUID) (AppraisalPeriod, error) {
	r, ok, err := store.AppraisalPeriodByRef(ctx, tx, id)
	if err != nil {
		return AppraisalPeriod{}, err
	}
	if !ok {
		return AppraisalPeriod{}, port.ErrAppraisalNotFound
	}
	return appraisalPeriod(r)
}

func loadAppraisalItem(ctx context.Context, tx pgx.Tx, c appraisalActor, period, staff uuid.UUID) (store.AppraisalEvent, AppraisalItem, error) {
	p, err := loadAppraisalPeriod(ctx, tx, period)
	if err != nil {
		return store.AppraisalEvent{}, AppraisalItem{}, err
	}
	if !c.onPeriod(p) {
		return store.AppraisalEvent{}, AppraisalItem{}, port.ErrAppraisalNotFound
	}
	r, ok, err := store.AppraisalLatest(ctx, tx, "entry", appraisalEntryKey(period, staff))
	if err != nil {
		return r, AppraisalItem{}, err
	}
	if !ok {
		return r, AppraisalItem{}, port.ErrAppraisalNotFound
	}
	e, err := appraisalItem(r, c)
	if err != nil {
		return r, e, err
	}
	appraisalReadOnly(&e, p.WorkflowVersion != AppraisalProposalV4)
	if !c.canRead(e.Member) {
		return r, e, port.ErrAppraisalNotFound
	}
	return r, e, nil
}

func (s *Service) AppraisalEntries(ctx context.Context, period uuid.UUID) ([]AppraisalItem, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.appraisalRecords(); err != nil {
		return nil, err
	}
	out := []AppraisalItem{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		p, e := loadAppraisalPeriod(ctx, tx, period)
		if e != nil {
			return e
		}
		if !c.onPeriod(p) {
			return port.ErrAppraisalNotFound
		}
		rows, e := store.AppraisalEntrySummaries(ctx, tx, period, true)
		if e != nil {
			return e
		}
		staff := []uuid.UUID{}
		for _, row := range rows {
			v, e := appraisalItem(row, c)
			if e != nil {
				return e
			}
			appraisalReadOnly(&v, p.WorkflowVersion != AppraisalProposalV4)
			if c.canRead(v.Member) {
				appraisalLate(&v, p, s.Clock.Now())
				out = append(out, v)
				staff = append(staff, v.Member.StaffID)
			}
		}
		if len(out) == 0 && (!c.center() || c.scope != domain.ScopeAll) {
			return port.ErrAppraisalNotFound
		}
		return store.InsertAppraisalAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "view", StaffIDs: staff,
			ItemCount: len(staff), Scope: string(c.scope), Actor: c.ref, Query: map[string]string{"appraisal_period": period.String()}}, s.Clock.Now())
	})
	if err != nil {
		return nil, classify(err)
	}
	slices.SortFunc(out, func(a, b AppraisalItem) int {
		if n := strings.Compare(a.Member.UnitName, b.Member.UnitName); n != 0 {
			return n
		}
		if a.Member.PositionOrder != b.Member.PositionOrder {
			return a.Member.PositionOrder - b.Member.PositionOrder
		}
		return strings.Compare(a.Member.Code, b.Member.Code)
	})
	return s.appraisalNames(ctx, out)
}

func (s *Service) appraisalNames(ctx context.Context, items []AppraisalItem) ([]AppraisalItem, error) {
	refs := make([]uuid.UUID, 0, len(items))
	for _, v := range items {
		refs = append(refs, v.Member.SubjectRef)
	}
	names, err := s.readPII(ctx, refs, client.PurposeHRMProfile, true, "")
	if forbiddenErr(err) {
		return items, nil // chỉ trả mã hồ sơ; quyền đánh giá không nới quyền của kho định danh
	}
	if err != nil {
		return nil, err
	}
	for i := range items {
		n := names[items[i].Member.SubjectRef]
		items[i].Name = n.FullName
		if n.Erased {
			items[i].Name = client.ErasedLabel
		}
	}
	return items, nil
}

type AppraisalDetail struct {
	Item        AppraisalItem     `json:"item"`
	Period      AppraisalPeriod   `json:"period"`
	Form        AppraisalTemplate `json:"form"`
	History     []AppraisalItem   `json:"history"`
	HistoryMore bool              `json:"history_more"`
}

func (s *Service) AppraisalEntryDetail(ctx context.Context, period, staff uuid.UUID) (AppraisalDetail, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return AppraisalDetail{}, err
	}
	if err = s.appraisalRecords(); err != nil {
		return AppraisalDetail{}, err
	}
	v := AppraisalDetail{History: []AppraisalItem{}}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		v.Period, e = loadAppraisalPeriod(ctx, tx, period)
		if e != nil {
			return e
		}
		_, v.Item, e = loadAppraisalItem(ctx, tx, c, period, staff)
		if e != nil {
			return e
		}
		v.Form = v.Period.Forms[v.Item.Member.Group] // phiên bản bất biến của đợt, không lấy mẫu mới
		appraisalLate(&v.Item, v.Period, s.Clock.Now())
		rows, e := store.AppraisalEntryHistory(ctx, tx, appraisalEntryKey(period, staff), 0)
		if e != nil {
			return e
		}
		v.HistoryMore = len(rows) > 50
		rows = rows[:min(len(rows), 50)]
		for _, row := range rows {
			it, e := appraisalItem(row, c)
			if e != nil {
				return e
			}
			appraisalReadOnly(&it, v.Item.LegacyReadOnly)
			v.History = append(v.History, it)
		}
		if !c.center() || c.scope != domain.ScopeAll {
			v.Period.Warnings, v.Period.Counts = []string{}, map[string]int{}
			v.Period.SignedDocuments = map[string]uuid.UUID{}
			v.Period.Forms, v.Period.Summaries = map[string]AppraisalTemplate{}, map[string]AppraisalTemplate{}
		}
		return store.InsertAppraisalAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "view", StaffID: &staff,
			ItemCount: 1, Scope: string(c.scope), Actor: c.ref, Query: map[string]string{"appraisal_period": period.String()}}, s.Clock.Now())
	})
	if err != nil {
		return v, classify(err)
	}
	named, err := s.appraisalNames(ctx, []AppraisalItem{v.Item})
	if err == nil {
		v.Item = named[0]
	}
	return v, err
}

// verifiedAppraisalSignedDocument kiểm quyền và chủ thể tệp ký ngoài TX ghi.
func (s *Service) verifiedAppraisalSignedDocument(ctx context.Context, c appraisalActor, id uuid.UUID) (store.DocRow, error) {
	if !s.docsReady() {
		return store.DocRow{}, port.ErrDocsUnavailable
	}
	doc, err := s.GetDoc(ctx, id)
	if err != nil {
		return store.DocRow{}, err
	}
	if doc.Status != "active" || doc.DocType != domain.DocDanhGia || doc.FileRef == uuid.Nil ||
		(doc.ContentType != "application/pdf" && !strings.HasPrefix(doc.ContentType, "image/")) ||
		!doc.CanManage {
		return store.DocRow{}, problem.Validation.WithField("document_ref", "cần bản scan đánh giá đang dùng, đúng hồ sơ và quyền tài liệu")
	}
	opened, err := s.Docs.Files.Open(ctx, doc.FileRef, string(client.PurposeHRRecords))
	if err != nil {
		return store.DocRow{}, err
	}
	file := opened.File
	if file.TenantID != c.tenant || file.Ref != doc.FileRef || file.Subject == nil || file.Subject.Module != port.SubjectModule ||
		file.Subject.Type != port.SubjectTypeFolder || file.Subject.ID != doc.FolderID {
		return store.DocRow{}, port.ErrAppraisalBlocked.WithDetail("tệp scan không gắn đúng thư mục hồ sơ đánh giá")
	}
	return doc.DocRow, nil
}

func appraisalActionPermission(c appraisalActor, item AppraisalItem, in domain.AppraisalEntryChange) error {
	a := appraisalAuthority(c)
	allowed := false
	switch in.Action {
	case "manager_submit", "send":
		allowed = domain.CanManageAppraisal(a, item.Member)
	case "return":
		allowed = c.center() && c.scope == domain.ScopeAll && !c.readOnly
	case "official", "reopen":
		allowed = c.center() && c.scope == domain.ScopeAll && !c.readOnly
	default:
		return problem.Validation.WithField("action", "thao tác đánh giá không hợp lệ")
	}
	if !allowed {
		return problem.Forbidden.WithDetail("không có quyền thực hiện bước đánh giá này")
	}
	return nil
}

func checkAppraisalDocument(ctx context.Context, tx pgx.Tx, proof store.DocRow) error {
	_, current, err := docHead(ctx, tx, proof.DocID)
	if err != nil {
		return err
	}
	if current.Seq != proof.Seq || current.Status != "active" || current.FileRef != proof.FileRef {
		return port.ErrAppraisalConflict.WithDetail("bản scan đã đổi hoặc bị ẩn; kiểm lại trước khi ghi")
	}
	return nil
}

func (s *Service) ChangeAppraisalEntry(ctx context.Context, period, staff uuid.UUID, idem string, in domain.AppraisalEntryChange) (AppraisalItem, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return AppraisalItem{}, err
	}
	if err = s.appraisalRecords(); err != nil {
		return AppraisalItem{}, err
	}
	if c.readOnly {
		return AppraisalItem{}, problem.Forbidden
	}
	if err = appraisalKey(idem); err != nil {
		return AppraisalItem{}, err
	}
	hash, err := appraisalHash(c.ref, "entry:"+appraisalEntryKey(period, staff), in)
	if err != nil {
		return AppraisalItem{}, err
	}
	var replay *store.AppraisalEvent
	var resultPeriod AppraisalPeriod
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		old, ok, e := appraisalReplay(ctx, tx, c, idem, hash)
		if e != nil {
			return e
		}
		_, item, e := loadAppraisalItem(ctx, tx, c, period, staff)
		if e != nil {
			return e
		}
		if e = appraisalActionPermission(c, item, in); e != nil {
			return e
		}
		resultPeriod, e = loadAppraisalPeriod(ctx, tx, period)
		if e != nil {
			return e
		}
		if resultPeriod.WorkflowVersion != AppraisalProposalV4 {
			return port.ErrAppraisalBlocked.WithDetail("đợt và phiếu cũ chỉ được đọc")
		}
		if !ok && in.Version != item.Version {
			return port.ErrAppraisalConflict.WithDetail("phiếu đã đổi; tải lại trước khi kiểm bản scan")
		}
		if ok {
			replay = &old
		}
		return nil
	})
	if err != nil {
		return AppraisalItem{}, err
	}
	if replay != nil {
		item, e := appraisalItem(*replay, c)
		appraisalLate(&item, resultPeriod, s.Clock.Now())
		return item, e
	}
	var saved store.AppraisalEvent
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		old, ok, e := appraisalReplay(ctx, tx, c, idem, hash)
		if e != nil {
			return e
		}
		if ok {
			_, item, loadErr := loadAppraisalItem(ctx, tx, c, period, staff)
			if loadErr != nil {
				return loadErr
			}
			e = appraisalActionPermission(c, item, in)
			saved = old
			if e == nil {
				resultPeriod, e = loadAppraisalPeriod(ctx, tx, period)
			}
			return e
		}
		if e = store.LockAppraisal(ctx, tx, c.tenant, "period-id:"+period.String()); e != nil {
			return e
		}
		p, e := loadAppraisalPeriod(ctx, tx, period)
		if e != nil {
			return e
		}
		resultPeriod = p
		row, current, e := loadAppraisalItem(ctx, tx, c, period, staff)
		if e != nil {
			return e
		}
		if in.Version != row.Seq {
			return port.ErrAppraisalConflict.WithDetail("phiếu đã đổi; tải lại trước khi ghi")
		}
		if e = appraisalActionPermission(c, current, in); e != nil {
			return e
		}
		a := appraisalAuthority(c)
		if p.WorkflowVersion != AppraisalProposalV4 {
			return port.ErrAppraisalBlocked.WithDetail("đợt và phiếu cũ chỉ được đọc")
		}
		next, e := domain.ChangeAppraisalEntry(current.AppraisalEntry,
			domain.AppraisalStepLocks{Closed: p.Closed}, a, in)
		if e != nil {
			return appraisalValidation(e)
		}
		appraisalSubmissionTimes(&next, in.Action, s.Clock.Now())
		raw, e := appraisalJSON(next)
		if e != nil {
			return e
		}
		saved, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: "entry", Key: row.Key,
			PeriodRef: &period, StaffRef: &staff, Action: in.Action, Payload: raw, Reason: in.Reason, RequestHash: hash, IdemKey: idem, Actor: c.ref}, s.Clock.Now())
		if e != nil {
			return e
		}
		return s.appraisalEntryNotice(ctx, tx, c, p, saved, next)
	})
	if err != nil {
		return AppraisalItem{}, classify(err)
	}
	item, err := appraisalItem(saved, c)
	appraisalLate(&item, resultPeriod, s.Clock.Now())
	return item, err
}
