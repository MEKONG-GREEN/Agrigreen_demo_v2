package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"
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

type AppraisalExclusion struct {
	StaffID uuid.UUID `json:"staff_id"`
	Reason  string    `json:"reason"`
}
type AppraisalPeriodInput struct {
	Year                int                  `json:"year"`
	Quarter             int                  `json:"quarter"` // 0 = đánh giá năm; 1..4 = quý
	CutoffOn            string               `json:"cutoff_on"`
	SelfDueAt           string               `json:"self_due_at"`
	ManagerDueAt        string               `json:"manager_due_at"`
	SummaryDueAt        string               `json:"summary_due_at"`
	Forms               map[string]uuid.UUID `json:"forms"`
	Summaries           map[string]uuid.UUID `json:"summaries"`
	Excluded            []AppraisalExclusion `json:"excluded"`
	AcknowledgeWarnings bool                 `json:"acknowledge_warnings"`
}
type AppraisalPeriod struct {
	ID              uuid.UUID                    `json:"id"`
	Version         int64                        `json:"version"`
	Year            int                          `json:"year"`
	Quarter         int                          `json:"quarter"`
	Label           string                       `json:"label"`
	CutoffOn        string                       `json:"cutoff_on"`
	KnownAt         time.Time                    `json:"known_at"`
	SelfDueAt       time.Time                    `json:"self_due_at"`
	ManagerDueAt    time.Time                    `json:"manager_due_at"`
	SummaryDueAt    time.Time                    `json:"summary_due_at"`
	Forms           map[string]AppraisalTemplate `json:"forms"`
	Summaries       map[string]AppraisalTemplate `json:"summaries"`
	SelfLocked      bool                         `json:"self_locked"`
	ManagerLocked   bool                         `json:"manager_locked"`
	Closed          bool                         `json:"closed"`
	SignedDocuments map[string]uuid.UUID         `json:"signed_documents"`
	Warnings        []string                     `json:"warnings"`
	Counts          map[string]int               `json:"counts"`
	RecordedAt      time.Time                    `json:"recorded_at"`
	Actor           uuid.UUID                    `json:"actor_ref"`
}

func appraisalPeriodKey(year, quarter int) string {
	return strconv.Itoa(year) + "-q" + strconv.Itoa(quarter)
}
func appraisalPeriod(row store.AppraisalEvent) (AppraisalPeriod, error) {
	p, err := appraisalsReadObject[AppraisalPeriod](row)
	p.Version, p.RecordedAt, p.Actor = row.Seq, row.RecordedAt, row.Actor
	return p, err
}
func (s *Service) appraisalRecords() error {
	if s.Rec == nil || !s.Rec.Enabled {
		return port.ErrRecordsDisabled
	}
	if s.Rec.Vault == nil || s.Rec.Profiles == nil {
		return port.ErrAppraisalUnavailable.WithDetail("kho định danh nhân sự chưa nối")
	}
	return nil
}
func appraisalDates(in AppraisalPeriodInput) (time.Time, time.Time, time.Time, time.Time, error) {
	if in.Year < 2020 || in.Year > 2100 || in.Quarter < 0 || in.Quarter > 4 {
		return time.Time{}, time.Time{}, time.Time{}, time.Time{}, problem.Validation.WithField("year", "năm 2020–2100, quý 1–4 hoặc 0 cho năm")
	}
	cutoff, err := time.Parse(time.DateOnly, in.CutoffOn)
	if err != nil || cutoff.Year() != in.Year {
		return cutoff, time.Time{}, time.Time{}, time.Time{}, problem.Validation.WithField("cutoff_on", "ngày chốt YYYY-MM-DD thuộc năm đánh giá")
	}
	dates := []time.Time{}
	for _, f := range []struct{ key, value string }{{"self_due_at", in.SelfDueAt}, {"manager_due_at", in.ManagerDueAt}, {"summary_due_at", in.SummaryDueAt}} {
		if len(f.value) > 40 {
			return cutoff, time.Time{}, time.Time{}, time.Time{}, problem.Validation.WithField(f.key, "hạn RFC3339 có múi giờ")
		}
		d, e := time.Parse(time.RFC3339, f.value)
		if e != nil {
			return cutoff, time.Time{}, time.Time{}, time.Time{}, problem.Validation.WithField(f.key, "hạn RFC3339 có múi giờ")
		}
		dates = append(dates, d.UTC())
	}
	if !dates[0].After(cutoff) || !dates[1].After(dates[0]) || !dates[2].After(dates[1]) {
		return cutoff, dates[0], dates[1], dates[2], problem.Validation.WithField("summary_due_at", "ngày chốt < hạn tự chấm < hạn cấp quản lý < hạn tổng hợp")
	}
	return cutoff, dates[0], dates[1], dates[2], nil
}
func appraisalAuthority(c appraisalActor) domain.AppraisalAuthority {
	return domain.AppraisalAuthority{Actor: c.ref, Group: c.group, Unit: c.unit, Manager: c.manager, ReadOnly: c.readOnly}
}

// OpenAppraisalPeriod chụp dân số bằng truy vấn as-of/known ở TX đọc riêng, đóng TX rồi mới đọc port đơn vị.
// TX ghi chỉ đọc SQL nội bộ/mẫu bất biến và ghi sổ + snapshot; không gọi vault, đơn vị hay cap/export trong TX.
func (s *Service) OpenAppraisalPeriod(ctx context.Context, idem string, in AppraisalPeriodInput) (AppraisalPeriod, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return AppraisalPeriod{}, err
	}
	if err = c.needCenter(true); err != nil {
		return AppraisalPeriod{}, err
	}
	if err = s.appraisalRecords(); err != nil {
		return AppraisalPeriod{}, err
	}
	if err = appraisalKey(idem); err != nil {
		return AppraisalPeriod{}, err
	}
	cutoff, selfDue, managerDue, summaryDue, err := appraisalDates(in)
	if err != nil {
		return AppraisalPeriod{}, err
	}
	if len(in.Forms) == 0 || len(in.Forms) > 3 || len(in.Summaries) > 2 || len(in.Excluded) > 5000 {
		return AppraisalPeriod{}, problem.Validation.WithField("forms", "chọn 1–3 nhóm đối tượng/phiếu và tối đa 2 mẫu tổng hợp")
	}
	for group, id := range in.Forms {
		if !slices.Contains([]string{domain.AppraisalCivil, domain.AppraisalLeader, domain.AppraisalCommune}, group) || id == uuid.Nil {
			return AppraisalPeriod{}, problem.Validation.WithField("forms", "nhóm/phiên bản phiếu không hợp lệ")
		}
	}
	excluded := map[uuid.UUID]string{}
	for _, e := range in.Excluded {
		if e.StaffID == uuid.Nil || utf8.RuneCountInString(strings.TrimSpace(e.Reason)) < 10 || utf8.RuneCountInString(e.Reason) > 2000 || excluded[e.StaffID] != "" {
			return AppraisalPeriod{}, problem.Validation.WithField("excluded", "loại trừ cần đúng mã người, không trùng và lý do 10–2.000 ký tự")
		}
		excluded[e.StaffID] = strings.TrimSpace(e.Reason)
	}
	hash, err := appraisalHash(c.ref, "period:open", in)
	if err != nil {
		return AppraisalPeriod{}, err
	}
	var replay *store.AppraisalEvent
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		old, ok, e := appraisalReplay(ctx, tx, c, idem, hash)
		if e != nil {
			return e
		}
		if ok {
			replay = &old
		}
		return nil
	})
	if err != nil {
		return AppraisalPeriod{}, err
	}
	if replay != nil {
		return appraisalPeriod(*replay)
	}
	pop, err := s.loadPopulation(ctx, rc{ref: c.ref, tenant: c.tenant, unitRef: c.unitRef, group: c.group, unit: c.unit}, cutoff, nil)
	if err != nil {
		return AppraisalPeriod{}, err
	}
	label := fmt.Sprintf("Năm %d", in.Year)
	if in.Quarter > 0 {
		label = fmt.Sprintf("Quý %s/%d", []string{"", "I", "II", "III", "IV"}[in.Quarter], in.Year)
	}
	p := AppraisalPeriod{ID: ids.New(), Year: in.Year, Quarter: in.Quarter, Label: label, CutoffOn: in.CutoffOn, KnownAt: pop.known,
		SelfDueAt: selfDue, ManagerDueAt: managerDue, SummaryDueAt: summaryDue, Forms: map[string]AppraisalTemplate{}, Summaries: map[string]AppraisalTemplate{}, SignedDocuments: map[string]uuid.UUID{}, Warnings: []string{}, Counts: map[string]int{}}
	unitNames := map[string]string{}
	for _, u := range pop.cat.Units {
		unitNames[u.Code] = u.Name
	}
	members := []domain.AppraisalEntry{}
	seenExcluded := map[uuid.UUID]bool{}
	for _, h := range pop.heads {
		if h.Type != domain.StaffVienChuc && h.Type != domain.StaffCanBoXa {
			continue
		}
		snap := domain.Project(pop.events[h.StaffID], cutoff, pop.rank)
		if snap.Status != domain.StatusActive {
			continue
		}
		position := pop.cat.Items[domain.KindPosition][snap.Position]
		group := domain.AppraisalCivil
		if h.Type == domain.StaffCanBoXa {
			group = domain.AppraisalCommune
		} else if position.Attrs.Leader {
			group = domain.AppraisalLeader
		}
		if _, selected := in.Forms[group]; !selected {
			continue
		}
		management := snap.Unit
		if h.Type == domain.StaffCanBoXa {
			management = snap.MgmtUnit
		}
		member := domain.AppraisalMember{StaffID: h.StaffID, SubjectRef: h.SubjectRef, AccountRef: h.AccountRef, Code: h.Code, Type: h.Type, Group: group, Unit: snap.Unit,
			ManagementUnit: management, Position: snap.Position, PositionName: position.Name, PositionOrder: position.SortOrder, UnitName: unitNames[management], SummaryKind: domain.AppraisalSummaryCode(h.Type, position.Attrs.Leader), ExcludedReason: excluded[h.StaffID]}
		if member.ExcludedReason != "" {
			seenExcluded[h.StaffID] = true
		} else {
			if member.Position == "" || member.ManagementUnit == "" {
				p.Warnings = append(p.Warnings, member.Code+": thiếu chức vụ/đơn vị quản lý; cần kiểm phân công")
			}
			if h.AccountRef == nil {
				p.Warnings = append(p.Warnings, member.Code+": chưa có tài khoản; guest OTP chưa nối (#184), nhập thay cần bản scan")
			}
		}
		members = append(members, domain.AppraisalEntry{Member: member, Status: domain.AppraisalNotStarted})
	}
	if len(members) == 0 || len(members) > 5000 {
		return AppraisalPeriod{}, port.ErrAppraisalBlocked.WithDetail("cần 1–5.000 người đang công tác trong nhóm đã chọn")
	}
	if len(seenExcluded) != len(excluded) {
		return AppraisalPeriod{}, problem.Validation.WithField("excluded", "người loại trừ không thuộc danh sách tại ngày chốt/nhóm đã chọn")
	}
	if len(p.Warnings) > 0 && !in.AcknowledgeWarnings {
		return AppraisalPeriod{}, port.ErrAppraisalConflict.WithDetail(strings.Join(p.Warnings, "; ") + "; xác nhận cảnh báo hoặc sửa hồ sơ trước khi mở")
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
		key := appraisalPeriodKey(in.Year, in.Quarter)
		if e = store.LockAppraisal(ctx, tx, c.tenant, "period:"+key); e != nil {
			return e
		}
		if _, exists, e := store.AppraisalLatest(ctx, tx, "period", key); e != nil {
			return e
		} else if exists {
			return port.ErrAppraisalConflict.WithDetail("đã có đợt của kỳ; dùng mở lại có lý do cho người đã chốt")
		}
		load := func(kind string, id uuid.UUID) (AppraisalTemplate, error) {
			row, exists, e := store.AppraisalVersion(ctx, tx, id)
			if e != nil {
				return AppraisalTemplate{}, e
			}
			if !exists || row.Kind != kind || row.Action != "publish" {
				return AppraisalTemplate{}, port.ErrAppraisalBlocked.WithDetail("phiên bản mẫu/phiếu chưa phát hành")
			}
			latest, ok, e := store.AppraisalLatest(ctx, tx, kind, row.Key)
			if e != nil {
				return AppraisalTemplate{}, e
			}
			if !ok || latest.ID != row.ID {
				return AppraisalTemplate{}, port.ErrAppraisalConflict.WithDetail("phiên bản mẫu/phiếu đã thay đổi; tải lại trước khi mở đợt")
			}
			return appraisalTemplate(row)
		}
		for group, id := range in.Forms {
			v, e := load("form", id)
			if e != nil {
				return e
			}
			if v.Form == nil || v.Form.Group != group {
				return problem.Validation.WithField("forms", "phiếu không đúng nhóm đối tượng")
			}
			p.Forms[group] = v
		}
		for _, entry := range members {
			kind := entry.Member.SummaryKind
			if _, ok := p.Summaries[kind]; ok {
				continue
			}
			id := in.Summaries[kind]
			if id == uuid.Nil {
				return problem.Validation.WithField("summaries", "chọn đủ họ mẫu MS05/MS06 cho nhóm đối tượng")
			}
			v, e := load("summary", id)
			if e != nil {
				return e
			}
			if v.Summary == nil || v.Summary.Kind != kind {
				return problem.Validation.WithField("summaries", "họ mẫu không đúng chức vụ/đối tượng")
			}
			p.Summaries[kind] = v
		}
		for _, entry := range members {
			raw, e := appraisalJSON(entry)
			if e != nil {
				return e
			}
			staff := entry.Member.StaffID
			_, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: "entry", Key: p.ID.String() + ":" + staff.String(), PeriodRef: &p.ID, StaffRef: &staff, Action: "open", Payload: raw, RequestHash: hash, IdemKey: idem + ":" + staff.String(), Actor: c.ref}, s.Clock.Now())
			if e != nil {
				return e
			}
		}
		raw, e := appraisalJSON(p)
		if e != nil {
			return e
		}
		saved, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: "period", Key: key, PeriodRef: &p.ID, Action: "open", Payload: raw, RequestHash: hash, IdemKey: idem, Actor: c.ref}, s.Clock.Now())
		return e
	})
	if err != nil {
		return AppraisalPeriod{}, classify(err)
	}
	return appraisalPeriod(saved)
}

func (s *Service) AppraisalPeriods(ctx context.Context) ([]AppraisalPeriod, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.appraisalRecords(); err != nil {
		return nil, err
	}
	out := []AppraisalPeriod{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		periods, e := store.AppraisalCurrent(ctx, tx, "period", nil)
		if e != nil {
			return e
		}
		for _, row := range periods {
			p, e := appraisalPeriod(row)
			if e != nil {
				return e
			}
			entries, e := store.AppraisalEntrySummaries(ctx, tx, p.ID, false)
			if e != nil {
				return e
			}
			p.Counts = map[string]int{}
			visible := 0
			for _, r := range entries {
				entry, e := appraisalsReadObject[domain.AppraisalEntry](r)
				if e != nil {
					return e
				}
				if !c.canRead(entry.Member) {
					continue
				}
				visible++
				if entry.Member.ExcludedReason != "" {
					p.Counts["excluded"]++
				} else {
					p.Counts[entry.Status]++
					p.Counts[entry.Member.SummaryKind]++
				}
			}
			if (c.center() && c.scope == domain.ScopeAll) || visible > 0 {
				if !c.center() || c.scope != domain.ScopeAll {
					p.Warnings = []string{}
					p.SignedDocuments = map[string]uuid.UUID{}
					p.Forms = map[string]AppraisalTemplate{}
					p.Summaries = map[string]AppraisalTemplate{}
				}
				out = append(out, p)
			}
		}
		return nil
	})
	return out, classify(err)
}
