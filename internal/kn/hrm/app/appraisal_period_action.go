package app

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type AppraisalPeriodChange struct {
	Action              string               `json:"action"`
	Version             int64                `json:"version"`
	Reason              string               `json:"reason,omitempty"`
	Unit                string               `json:"unit,omitempty"`
	Step                string               `json:"step,omitempty"`
	SelfDueAt           string               `json:"self_due_at,omitempty"`
	ManagerDueAt        string               `json:"manager_due_at,omitempty"`
	SummaryDueAt        string               `json:"summary_due_at,omitempty"`
	SignedDocuments     map[string]uuid.UUID `json:"signed_documents,omitempty"`
	AcknowledgeUnsigned bool                 `json:"acknowledge_unsigned"`
}

func redactedAppraisalPeriod(c appraisalActor, p AppraisalPeriod) AppraisalPeriod {
	if !c.center() || c.scope != domain.ScopeAll {
		p.Warnings, p.Counts = []string{}, map[string]int{}
		p.Forms, p.Summaries = map[string]AppraisalTemplate{}, map[string]AppraisalTemplate{}
		p.SignedDocuments = map[string]uuid.UUID{}
	}
	return p
}

func appraisalPeriodPermission(c appraisalActor, action string) error {
	if action == "send" {
		if c.readOnly || !(c.center() || (c.group == domain.GroupManager && c.manager)) {
			return problem.Forbidden.WithDetail("chỉ cấp quản lý được gửi đề xuất thuộc phân công")
		}
		return nil
	}
	return c.needCenter(true)
}

func extendAppraisalPeriod(p *AppraisalPeriod, in AppraisalPeriodChange) error {
	nextPeriod := *p
	changed := false
	for _, v := range []struct {
		field, raw string
		old        *time.Time
	}{{"self_due_at", in.SelfDueAt, &nextPeriod.SelfDueAt}, {"manager_due_at", in.ManagerDueAt, &nextPeriod.ManagerDueAt}, {"summary_due_at", in.SummaryDueAt, &nextPeriod.SummaryDueAt}} {
		if v.raw == "" {
			continue
		}
		if len(v.raw) > 40 {
			return problem.Validation.WithField(v.field, "hạn RFC3339 có múi giờ")
		}
		next, err := time.Parse(time.RFC3339, v.raw)
		if err != nil || next.Before(*v.old) {
			return problem.Validation.WithField(v.field, "gia hạn không được rút ngắn hạn hiện có")
		}
		changed = changed || next.After(*v.old)
		*v.old = next.UTC()
	}
	if !changed || !nextPeriod.ManagerDueAt.After(nextPeriod.SelfDueAt) || !nextPeriod.SummaryDueAt.After(nextPeriod.ManagerDueAt) {
		return problem.Validation.WithField("summary_due_at", "cần hạn mới và giữ tự đánh giá < cấp quản lý < tổng hợp")
	}
	*p = nextPeriod
	return nil
}

func (s *Service) ChangeAppraisalPeriod(ctx context.Context, period uuid.UUID, idem string, in AppraisalPeriodChange) (AppraisalPeriod, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return AppraisalPeriod{}, err
	}
	if err = s.appraisalRecords(); err != nil {
		return AppraisalPeriod{}, err
	}
	if err = appraisalPeriodPermission(c, in.Action); err != nil {
		return AppraisalPeriod{}, err
	}
	if err = appraisalKey(idem); err != nil {
		return AppraisalPeriod{}, err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(in.Reason) > 2000 || len(in.SignedDocuments) > 2 {
		return AppraisalPeriod{}, problem.Validation.WithField("reason", "lý do tối đa 2.000 ký tự, tối đa hai bản ký MS05/MS06")
	}
	hash, err := appraisalHash(c.ref, "period:"+period.String(), in)
	if err != nil {
		return AppraisalPeriod{}, err
	}
	var pre AppraisalPeriod
	var replay *store.AppraisalEvent
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		old, ok, e := appraisalReplay(ctx, tx, c, idem, hash)
		if e != nil {
			return e
		}
		pre, e = loadAppraisalPeriod(ctx, tx, period)
		if e != nil {
			return e
		}
		if ok {
			replay = &old
		} else if in.Version != pre.Version {
			return port.ErrAppraisalConflict.WithDetail("đợt đã đổi; tải lại trước khi kiểm tài liệu/thực hiện")
		}
		return nil
	})
	if err != nil {
		return AppraisalPeriod{}, classify(err)
	}
	if replay != nil {
		p, e := appraisalPeriod(*replay)
		return redactedAppraisalPeriod(c, p), e
	}
	proofs := map[string]store.DocRow{}
	var documents map[string]uuid.UUID
	if in.Action == "attach_signed" {
		documents = in.SignedDocuments
		if len(documents) == 0 {
			return AppraisalPeriod{}, problem.Validation.WithField("signed_documents", "chọn bản ký MS05/MS06")
		}
	} else if in.Action == "close" {
		documents = pre.SignedDocuments
	}
	// Quyền/tệp chứng từ được đọc ngoài giao dịch ghi; stale version và replay đã được kiểm phía trên.
	for kind, doc := range documents {
		if _, exists := pre.Summaries[kind]; !exists {
			return AppraisalPeriod{}, problem.Validation.WithField("signed_documents", "mẫu không có trong đợt")
		}
		if doc == uuid.Nil {
			if in.Action != "attach_signed" || utf8.RuneCountInString(in.Reason) < 10 {
				return AppraisalPeriod{}, problem.Validation.WithField("reason", "gỡ liên kết bản ký cần lý do ít nhất 10 ký tự")
			}
			continue
		}
		p, e := s.verifiedAppraisalDocument(ctx, c, doc, uuid.Nil, true)
		if e != nil {
			return AppraisalPeriod{}, e
		}
		if p.Year != nil && *p.Year != pre.Year {
			return AppraisalPeriod{}, problem.Validation.WithField("signed_documents", "năm phân loại tài liệu khác năm đánh giá")
		}
		proofs[kind] = p
	}
	var saved store.AppraisalEvent
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		old, ok, e := appraisalReplay(ctx, tx, c, idem, hash)
		if e != nil {
			return e
		}
		if ok {
			saved = old
			return nil
		}
		if e = store.LockAppraisal(ctx, tx, c.tenant, "period-id:"+period.String()); e != nil {
			return e
		}
		p, e := loadAppraisalPeriod(ctx, tx, period)
		if e != nil {
			return e
		}
		if p.Version != in.Version {
			return port.ErrAppraisalConflict.WithDetail("đợt đã đổi; tải lại trước khi ghi")
		}
		for _, proof := range proofs {
			if e = checkAppraisalDocument(ctx, tx, proof); e != nil {
				return e
			}
		}
		if p.Closed && in.Action != "close" && in.Action != "send" && in.Action != "attach_signed" && in.Action != "remind" {
			return port.ErrAppraisalBlocked.WithDetail("đợt đã chốt; chỉ mở lại từng người có lý do")
		}
		switch in.Action {
		case "extend":
			e = extendAppraisalPeriod(&p, in)
		case "lock_self", "unlock_self", "lock_manager", "unlock_manager":
			unlock := strings.HasPrefix(in.Action, "unlock")
			if unlock && utf8.RuneCountInString(in.Reason) < 10 {
				return problem.Validation.WithField("reason", "mở khóa bước cần lý do ít nhất 10 ký tự")
			}
			field := &p.SelfLocked
			if strings.HasSuffix(in.Action, "manager") {
				field = &p.ManagerLocked
			}
			if *field == !unlock {
				return port.ErrAppraisalConflict.WithDetail("trạng thái khóa không thay đổi")
			}
			*field = !unlock
		case "attach_signed":
			if p.SignedDocuments == nil {
				p.SignedDocuments = map[string]uuid.UUID{}
			}
			for kind, doc := range in.SignedDocuments {
				if doc == uuid.Nil {
					delete(p.SignedDocuments, kind)
				} else {
					p.SignedDocuments[kind] = doc
				}
			}
		case "send":
			e = s.sendAppraisalUnit(ctx, tx, c, p, idem, hash, in.Unit)
		case "close":
			if !in.AcknowledgeUnsigned {
				for kind := range p.Summaries {
					if p.SignedDocuments[kind] == uuid.Nil {
						return port.ErrAppraisalBlocked.WithDetail("chưa có đủ bản ký; xác nhận cảnh báo trước khi chốt")
					}
				}
			}
			e = s.closeAppraisalEntries(ctx, tx, c, p, idem, hash, in.Reason)
			p.Closed, p.SelfLocked, p.ManagerLocked = true, true, true
		case "remind":
			e = s.remindAppraisal(ctx, tx, c, p, in.Step, idem)
		default:
			return problem.Validation.WithField("action", "thao tác đợt không hợp lệ")
		}
		if e != nil {
			return e
		}
		p.Counts = map[string]int{} // tiến độ tính từ dòng hiện hành khi đọc, không trả số cũ của snapshot
		raw, e := appraisalJSON(p)
		if e != nil {
			return e
		}
		saved, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: "period", Key: appraisalPeriodKey(p.Year, p.Quarter),
			PeriodRef: &period, Action: in.Action, Payload: raw, Reason: in.Reason, RequestHash: hash, IdemKey: idem, Actor: c.ref}, s.Clock.Now())
		if e == nil && in.Action == "send" {
			n := appraisalNotice(p, c, "send", "summary", saved.ID)
			n.Center = true
			e = s.enqueueAppraisalNotice(ctx, tx, c, n)
		}
		return e
	})
	if err != nil {
		return AppraisalPeriod{}, classify(err)
	}
	p, err := appraisalPeriod(saved)
	return redactedAppraisalPeriod(c, p), err
}

func (s *Service) sendAppraisalUnit(ctx context.Context, tx pgx.Tx, c appraisalActor, p AppraisalPeriod, idem, hash, unit string) error {
	if !c.center() && unit != "" && unit != c.unit {
		return problem.Forbidden.WithDetail("không gửi đề xuất của đơn vị khác")
	}
	rows, err := store.AppraisalEntrySummaries(ctx, tx, p.ID, false)
	if err != nil {
		return err
	}
	n := 0
	for _, row := range rows {
		x, e := appraisalItem(row, c)
		if e != nil {
			return e
		}
		if x.Member.ExcludedReason != "" || !c.canRead(x.Member) || !domain.CanManageAppraisal(appraisalAuthority(c), x.Member) ||
			(unit != "" && unit != x.Member.ManagementUnit) || x.Status == domain.AppraisalSent || x.Status == domain.AppraisalClosed {
			continue
		}
		if x.Status != domain.AppraisalManagerSubmitted {
			return port.ErrAppraisalBlocked.WithDetail("đơn vị còn người chưa hoàn tất đề xuất; chưa gửi dòng nào")
		}
		_, full, e := loadAppraisalItem(ctx, tx, c, p.ID, x.Member.StaffID)
		if e != nil {
			return e
		}
		form := p.Forms[x.Member.Group]
		if form.Form == nil {
			return port.ErrAppraisalBlocked
		}
		next, e := domain.ChangeAppraisalEntry(full.AppraisalEntry, *form.Form,
			domain.AppraisalStepLocks{Self: p.SelfLocked, Manager: p.ManagerLocked, Closed: p.Closed}, appraisalAuthority(c), domain.AppraisalEntryChange{Action: "send"})
		if e != nil {
			return appraisalValidation(e)
		}
		raw, e := appraisalJSON(next)
		if e != nil {
			return e
		}
		staff := x.Member.StaffID
		if _, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: "entry", Key: row.Key, PeriodRef: &p.ID,
			StaffRef: &staff, Action: "send", Payload: raw, RequestHash: hash, IdemKey: idem + ":" + staff.String(), Actor: c.ref}, s.Clock.Now()); e != nil {
			return e
		}
		n++
	}
	if n == 0 {
		return port.ErrAppraisalBlocked.WithDetail("không còn đề xuất được giao để gửi")
	}
	return nil
}

func (s *Service) closeAppraisalEntries(ctx context.Context, tx pgx.Tx, c appraisalActor, p AppraisalPeriod, idem, hash, reason string) error {
	rows, err := store.AppraisalEntrySummaries(ctx, tx, p.ID, false)
	if err != nil {
		return err
	}
	n := 0
	for _, row := range rows { // SQL sắp theo khóa staff trong cùng đợt, giữ thứ tự khóa rating ổn định
		x, e := appraisalItem(row, c)
		if e != nil {
			return e
		}
		if x.Member.ExcludedReason != "" || x.Status == domain.AppraisalClosed {
			continue
		}
		_, full, e := loadAppraisalItem(ctx, tx, c, p.ID, x.Member.StaffID)
		if e != nil {
			return e
		}
		next, e := domain.CloseAppraisalEntry(full.AppraisalEntry)
		if e != nil {
			return appraisalValidation(e)
		}
		staff := x.Member.StaffID
		if p.Quarter == 0 {
			if e = store.LockRating(ctx, tx, c.tenant, staff); e != nil {
				return e
			}
			old, e := store.Ratings(ctx, tx, staff)
			if e != nil {
				return e
			}
			rating := store.RatingRow{ID: ids.New(), StaffID: staff, Year: p.Year, Rating: next.ClosedRating, Source: "evaluation", Actor: c.ref}
			if previous, ok := store.EffectiveRatings(old)[staff][p.Year]; ok {
				why := reason
				if why == "" {
					why = full.CorrectionReason
				}
				if utf8.RuneCountInString(why) < 10 {
					return problem.Validation.WithField("reason", "đã có kết quả năm; thay bằng kết luận đợt cần lý do ít nhất 10 ký tự")
				}
				rating.Supersedes, rating.Reason = &previous.ID, why
			}
			if _, e = store.InsertRating(ctx, tx, c.tenant, rating, s.Clock.Now(), idem+":rating:"+staff.String()); e != nil {
				return e
			}
		}
		raw, e := appraisalJSON(next)
		if e != nil {
			return e
		}
		if _, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: "entry", Key: row.Key, PeriodRef: &p.ID,
			StaffRef: &staff, Action: "close", Payload: raw, Reason: reason, RequestHash: hash, IdemKey: idem + ":" + staff.String(), Actor: c.ref}, s.Clock.Now()); e != nil {
			return e
		}
		n++
	}
	if n == 0 {
		return port.ErrAppraisalBlocked.WithDetail("không có người đang mở để chốt")
	}
	return nil
}
