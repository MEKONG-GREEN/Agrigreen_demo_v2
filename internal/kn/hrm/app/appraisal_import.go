package app

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AppraisalImportResult struct {
	Count   int   `json:"count"`
	Version int64 `json:"version"`
}
type appraisalImportMarker struct {
	AppraisalPeriod
	ImportStaff []uuid.UUID `json:"imported_staff"`
}

func appraisalImportResult(row store.AppraisalEvent) (AppraisalImportResult, error) {
	marker, err := appraisalsReadObject[appraisalImportMarker](row)
	return AppraisalImportResult{Count: len(marker.ImportStaff), Version: row.Seq}, err
}
func (s *Service) appraisalImportCaller(ctx context.Context) (appraisalActor, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return c, err
	}
	if err = s.appraisalRecords(); err != nil {
		return c, err
	}
	if c.readOnly || (!c.center() && (c.group != domain.GroupManager || !c.manager)) {
		return c, problem.Forbidden
	}
	return c, nil
}
func appraisalImportHeaders(p AppraisalPeriod, header []string) (map[string]int, error) {
	for _, t := range p.Summaries {
		if t.Summary == nil || len(header) != len(t.Summary.Columns) {
			continue
		}
		indexes := map[string]int{}
		valid := true
		for i, column := range t.Summary.Columns {
			if tabular.NormalizeHeader(header[i]) != tabular.NormalizeHeader(column.Title) {
				valid = false
				break
			}
			indexes[column.Key] = i
		}
		if valid {
			return indexes, nil
		}
	}
	return nil, problem.Validation.WithField("file", "dòng đầu cần đúng tám tiêu đề theo mẫu nhập của đợt")
}
func appraisalImportName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
func appraisalImportRating(raw string, t domain.AppraisalSummaryTemplate) string {
	raw = strings.TrimSpace(raw)
	for _, rating := range domain.Ratings {
		if strings.EqualFold(raw, rating) || strings.EqualFold(raw, t.Ratings[rating]) {
			return rating
		}
	}
	return raw
}

// AppraisalImportTemplate đọc tên ngoài TX, tạo tệp trong bộ nhớ. Không ghi tệp lên đĩa hoặc gọi port trong TX.
func (s *Service) AppraisalImportTemplate(ctx context.Context, period uuid.UUID, kind string) ([]byte, error) {
	c, err := s.appraisalImportCaller(ctx)
	if err != nil {
		return nil, err
	}
	if kind != "MS05" && kind != "MS06" {
		return nil, port.ErrAppraisalNotFound
	}
	var p AppraisalPeriod
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		p, e = loadAppraisalPeriod(ctx, tx, period)
		if e != nil {
			return e
		}
		if !c.onPeriod(p) {
			return port.ErrAppraisalNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if p.WorkflowVersion != AppraisalProposalV4 {
		return nil, port.ErrAppraisalBlocked.WithDetail("đợt cũ chỉ đọc")
	}
	template := p.Summaries[kind].Summary
	if template == nil {
		return nil, port.ErrAppraisalNotFound
	}
	items, err := s.AppraisalEntries(ctx, period)
	if err != nil {
		return nil, err
	}
	rows := [][]string{}
	for _, item := range items {
		if item.CanManage && item.Member.SummaryKind == kind && item.Member.ExcludedReason == "" && (item.Status == domain.AppraisalNotStarted || item.Status == domain.AppraisalEntered || item.Status == domain.AppraisalChangeRequested) {
			rows = append(rows, appraisalPaperCells(item, len(rows)+1, *template))
		}
	}
	if len(rows) == 0 {
		return nil, port.ErrAppraisalNotFound
	}
	header := []string{}
	for _, column := range template.Columns {
		header = append(header, column.Title)
	}
	var out bytes.Buffer
	if err = tabular.WriteXLSX(&out, kind, header, rows); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// ImportAppraisalProposals kiểm toàn bộ dòng và snapshot trước khi ghi nguyên tử.
// Kho định danh được đọc ngoài TX; trong TX chỉ có query/ledger của HRM.
func (s *Service) ImportAppraisalProposals(ctx context.Context, period uuid.UUID, idem string, version int64, table *tabular.Table) (AppraisalImportResult, error) {
	c, err := s.appraisalImportCaller(ctx)
	if err != nil {
		return AppraisalImportResult{}, err
	}
	if err = appraisalKey(idem); err != nil {
		return AppraisalImportResult{}, err
	}
	if table == nil || len(table.Rows) == 0 || len(table.Rows) > 5000 || len(table.Header) != 8 || version < 1 {
		return AppraisalImportResult{}, problem.Validation.WithField("file", "cần 1–5.000 dòng, tám cột và phiên bản đợt")
	}
	hash, err := appraisalHash(c.ref, "import:"+period.String(), struct {
		Version int64
		Table   *tabular.Table
	}{version, table})
	if err != nil {
		return AppraisalImportResult{}, err
	}
	var p AppraisalPeriod
	var replay *store.AppraisalEvent
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		old, ok, e := appraisalReplay(ctx, tx, c, idem, hash)
		if e != nil {
			return e
		}
		p, e = loadAppraisalPeriod(ctx, tx, period)
		if e != nil {
			return e
		}
		if !c.onPeriod(p) {
			return port.ErrAppraisalNotFound
		}
		if p.WorkflowVersion != AppraisalProposalV4 {
			return port.ErrAppraisalBlocked.WithDetail("đợt cũ chỉ đọc")
		}
		if ok {
			marker, e := appraisalsReadObject[appraisalImportMarker](old)
			if e != nil {
				return e
			}
			for _, staff := range marker.ImportStaff {
				_, item, e := loadAppraisalItem(ctx, tx, c, period, staff)
				if e != nil {
					return e
				}
				if !domain.CanManageAppraisal(appraisalAuthority(c), item.Member) {
					return problem.Forbidden
				}
			}
			replay = &old
			return nil
		}
		if p.Version != version {
			return port.ErrAppraisalConflict.WithDetail("đợt đã đổi; tải lại trước khi nhập")
		}
		return nil
	})
	if err != nil {
		return AppraisalImportResult{}, classify(err)
	}
	if replay != nil {
		return appraisalImportResult(*replay)
	}
	indexes, err := appraisalImportHeaders(p, table.Header)
	if err != nil {
		return AppraisalImportResult{}, err
	}
	visible, err := s.AppraisalEntries(ctx, period)
	if err != nil {
		return AppraisalImportResult{}, err
	}
	byName := map[string][]AppraisalItem{}
	for _, item := range visible {
		if !item.CanManage || item.Member.ExcludedReason != "" {
			continue
		}
		for _, name := range []string{item.Name, item.Member.Code} {
			if name != "" {
				key := appraisalImportName(name)
				byName[key] = append(byName[key], item)
			}
		}
	}
	type change struct {
		staff   uuid.UUID
		version int64
		input   domain.AppraisalEntryChange
	}
	changes := []change{}
	seen := map[uuid.UUID]bool{}
	for _, row := range table.Rows {
		field := "rows." + strconv.Itoa(row.Line)
		if len(row.Cells) > 8 {
			return AppraisalImportResult{}, problem.Validation.WithField(field, "dòng vượt tám cột")
		}
		for _, value := range row.Cells {
			if utf8.RuneCountInString(value) > 2000 {
				return AppraisalImportResult{}, problem.Validation.WithField(field, "mỗi ô tối đa 2.000 ký tự")
			}
		}
		cell := func(key string) string {
			index := indexes[key]
			if index >= len(row.Cells) {
				return ""
			}
			return strings.TrimSpace(row.Cells[index])
		}
		matches := byName[appraisalImportName(cell("staff"))]
		if len(matches) != 1 {
			return AppraisalImportResult{}, problem.Validation.WithField(field, "tên không khớp duy nhất trong phân công; nếu trùng tên, nhập mã hồ sơ vào cột Họ và tên")
		}
		item := matches[0]
		if seen[item.Member.StaffID] {
			return AppraisalImportResult{}, problem.Validation.WithField(field, "mỗi người chỉ được có một dòng")
		}
		seen[item.Member.StaffID] = true
		template := p.Summaries[item.Member.SummaryKind].Summary
		if template == nil {
			return AppraisalImportResult{}, port.ErrAppraisalBlocked
		}
		input := domain.AppraisalEntryChange{Action: "manager_submit", Version: item.Version, SelfRating: appraisalImportRating(cell("self_rating"), *template), CollectiveRating: appraisalImportRating(cell("collective_rating"), *template), Reason: cell("reason"), PersonnelProposal: cell("personnel_proposal"), Note: cell("note")}
		if _, e := domain.ChangeAppraisalEntry(item.AppraisalEntry, domain.AppraisalStepLocks{Closed: p.Closed}, appraisalAuthority(c), input); e != nil {
			return AppraisalImportResult{}, problem.Validation.WithField(field, fmt.Sprint(e))
		}
		changes = append(changes, change{item.Member.StaffID, item.Version, input})
	}
	slices.SortFunc(changes, func(a, b change) int { return strings.Compare(a.staff.String(), b.staff.String()) })
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
		current, e := loadAppraisalPeriod(ctx, tx, period)
		if e != nil {
			return e
		}
		if current.Version != version {
			return port.ErrAppraisalConflict
		}
		staffIDs := []uuid.UUID{}
		for _, change := range changes {
			row, item, e := loadAppraisalItem(ctx, tx, c, period, change.staff)
			if e != nil {
				return e
			}
			if row.Seq != change.version {
				return port.ErrAppraisalConflict.WithDetail("đề xuất đã đổi trong lúc đọc tệp; tải lại trước khi nhập")
			}
			next, e := domain.ChangeAppraisalEntry(item.AppraisalEntry, domain.AppraisalStepLocks{Closed: current.Closed}, appraisalAuthority(c), change.input)
			if e != nil {
				return appraisalValidation(e)
			}
			appraisalSubmissionTimes(&next, "manager_submit", s.Clock.Now())
			raw, e := appraisalJSON(next)
			if e != nil {
				return e
			}
			staff := change.staff
			if _, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: "entry", Key: row.Key, PeriodRef: &period, StaffRef: &staff, Action: "manager_submit", Payload: raw, RequestHash: hash, IdemKey: idem + ":" + staff.String(), Actor: c.ref}, s.Clock.Now()); e != nil {
				return e
			}
			staffIDs = append(staffIDs, staff)
		}
		current.Counts = map[string]int{}
		raw, e := appraisalJSON(appraisalImportMarker{current, staffIDs})
		if e != nil {
			return e
		}
		saved, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: "period", Key: appraisalPeriodKey(current.Year, current.Quarter) + ":" + period.String(), PeriodRef: &period, Action: "manager_submit", Payload: raw, RequestHash: hash, IdemKey: idem, Actor: c.ref}, s.Clock.Now())
		return e
	})
	if err != nil {
		return AppraisalImportResult{}, classify(err)
	}
	return appraisalImportResult(saved)
}
