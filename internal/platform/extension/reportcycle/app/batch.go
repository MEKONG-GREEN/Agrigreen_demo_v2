package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Phạm vi xã nhận (FN-TI-02.02 B2).
const (
	ScopeProvince = "province" // Toàn tỉnh
	ScopeRegions  = "regions"  // Theo Khu vực
	ScopeCommunes = "communes" // Chọn từng xã
)

// Nguồn giá trị Tỉnh giao.
const (
	PVManual   = "manual"
	PVExcel    = "excel"
	PVPrevious = "previous_batch"
)

// BatchInput là yêu cầu phát hành (và xem trước).
type BatchInput struct {
	ReportType     string
	TemplateIDs    []uuid.UUID
	ScopeMode      string
	Regions        []string
	Communes       []string
	PeriodType     string // rỗng = theo loại báo cáo
	From, To       string
	SeasonLabel    string
	Deadline       *domain.Deadline
	CloseAfterDays *int
	Note           string
	// ProvincialValues: template_id → mã xã → chữ cái cột Tỉnh giao → giá trị.
	ProvincialValues map[uuid.UUID]map[string]map[string]string
	ProvincialSource string // manual | excel
	// CopyProvincial: các mẫu lấy giá trị Tỉnh giao từ đợt trước (xã chưa nhập trực tiếp).
	CopyProvincial []uuid.UUID
}

// ProvincialColumn là một cột "Tỉnh giao" của mẫu (Bước 4).
type ProvincialColumn struct {
	TemplateID uuid.UUID
	FormCode   string
	Letter     string
	Title      string
	Unit       string
}

// PreviewForm là một mẫu trong khung xem trước.
type PreviewForm struct {
	Version     repport.FormVersion
	PeriodBlock bool
	Slots       int // số khe kỳ của mẫu (0 = không có khối kỳ)
}

// Preview là kết quả "Xem trước" (FN-TI-02.02 B5).
type Preview struct {
	ReportType           string
	PeriodType           string
	Rule                 domain.PeriodRule
	Deadline             domain.Deadline
	CloseAfterDays       int
	Forms                []PreviewForm
	Periods              []domain.Period
	Communes             []partyport.CommuneRegion
	CommunesNoOfficer    []string
	Officers             map[string][]uuid.UUID
	TaskCount            int
	ProvincialColumns    []ProvincialColumn
	Warnings             []string
	province, department string
	from, to             time.Time
}

func (s *Service) deps() error {
	switch {
	case s.Forms == nil:
		return ErrDependency.WithDetail("chưa nối engine mẫu báo cáo (pf/reports)")
	case s.Regions == nil:
		return ErrDependency.WithDetail("chưa nối khu vực (pf/party)")
	case s.Assignments == nil:
		return ErrDependency.WithDetail("chưa nối gán vai trò (pf/accounts)")
	}
	return nil
}

func hasStructure(def repport.FormDefinition) bool {
	return slices.ContainsFunc(def.Columns, func(c repport.FormColumn) bool {
		return c.Kind == repport.ColumnInput || c.Kind == repport.ColumnPerPeriod
	})
}

func slotsOf(def repport.FormDefinition) int {
	n := 0
	for _, b := range def.PeriodBlocks {
		if n == 0 || b.Slots < n {
			n = b.Slots
		}
	}
	return n
}

// PreviewBatch dựng khung xem trước (không ghi gì).
func (s *Service) PreviewBatch(ctx context.Context, in BatchInput) (Preview, error) {
	a, err := s.writer(ctx, authz.KNTinh)
	if err != nil {
		return Preview{}, err
	}
	return s.preview(ctx, a, in, false)
}

func (s *Service) preview(ctx context.Context, a actor, in BatchInput, strict bool) (Preview, error) {
	if err := s.deps(); err != nil {
		return Preview{}, err
	}
	pv := Preview{ReportType: in.ReportType, province: a.province, department: a.k.Department, Officers: map[string][]uuid.UUID{}}
	if a.province == "" || a.k.Department == "" {
		return pv, ErrOutOfScope.WithDetail("ngữ cảnh phiên không gắn tỉnh/phòng ban")
	}
	// W11.05b: loại kỳ chọn được trong ReportPeriodTypes của loại báo cáo (ngày / tuần KSGM / quý — quy tắc v2).
	pt, err := domain.ResolvePeriodType(in.ReportType, in.PeriodType)
	if err != nil {
		var pe domain.ErrPeriod
		errors.As(err, &pe)
		return pv, problem.Validation.WithField(pe.Field, pe.Msg)
	}
	pv.PeriodType = pt
	from, err := domain.ParseDate(in.From)
	if err != nil {
		return pv, problem.Validation.WithField("from_date", err.Error())
	}
	to, err := domain.ParseDate(in.To)
	if err != nil {
		return pv, problem.Validation.WithField("to_date", err.Error())
	}
	pv.from, pv.to = from, to
	in.SeasonLabel = strings.TrimSpace(in.SeasonLabel)
	if n := len([]rune(in.SeasonLabel)); n < 1 || n > 120 {
		return pv, problem.Validation.WithField("season_label", "bắt buộc, tối đa 120 ký tự (vd Thu Đông 2026)")
	}
	pv.Rule = s.periodRule(ctx, from)
	pv.Deadline = pv.Rule.DefaultDeadline(pt)
	if in.Deadline != nil {
		pv.Deadline = *in.Deadline
	}
	pv.CloseAfterDays = domain.DefaultCloseAfterDays
	if in.CloseAfterDays != nil {
		pv.CloseAfterDays = *in.CloseAfterDays
	}
	pv.Periods, err = domain.Split(domain.SplitInput{PeriodType: pt, From: from, To: to, SeasonLabel: in.SeasonLabel, Rule: pv.Rule,
		Deadline: pv.Deadline, CloseAfterDays: pv.CloseAfterDays, Loc: clock.Zone(), KSGM: s.ksgmStarts(ctx)})
	if err != nil {
		var pe domain.ErrPeriod
		if errors.As(err, &pe) {
			return pv, problem.Validation.WithField(pe.Field, pe.Msg)
		}
		return pv, err
	}
	// Bước 1: mẫu đã gán cho loại báo cáo của phòng ban (FN-AD-04.06), phiên bản áp dụng tại kỳ đầu.
	if len(in.TemplateIDs) == 0 {
		return pv, problem.Validation.WithField("template_ids", "chọn ít nhất một mẫu")
	}
	assigned, err := s.Forms.AssignedForms(ctx, a.p.TenantID, a.k.Department, repport.LevelCommune, in.ReportType, from)
	if err != nil {
		return pv, err
	}
	byTpl := map[uuid.UUID]repport.FormVersion{}
	for _, f := range assigned {
		byTpl[f.TemplateID] = f
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range in.TemplateIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		f, ok := byTpl[id]
		if !ok {
			return pv, ErrFormNotAssigned.WithField("template_ids", id.String()+": mẫu chưa gán cho loại báo cáo "+in.ReportType+" của phòng ban hoặc chưa có phiên bản phát hành")
		}
		if !hasStructure(f.Definition) {
			return pv, ErrFormNoStructure.WithField("template_ids", f.Code+": mẫu chưa có cấu trúc cột")
		}
		slots := slotsOf(f.Definition)
		if slots > 0 && len(pv.Periods) > slots {
			msg := fmt.Sprintf("%s chỉ có %d khe kỳ trên một dòng, đợt có %d kỳ (tạm theo mẫu seed, chờ khách Q-01)", f.Code, slots, len(pv.Periods))
			if strict {
				return pv, ErrSlotsExceeded.WithField("to_date", msg)
			}
			pv.Warnings = append(pv.Warnings, msg)
		}
		pv.Forms = append(pv.Forms, PreviewForm{Version: f, PeriodBlock: len(f.Definition.PeriodBlocks) > 0, Slots: slots})
		for _, c := range f.Definition.Columns {
			if c.Kind == repport.ColumnProvincial {
				pv.ProvincialColumns = append(pv.ProvincialColumns, ProvincialColumn{TemplateID: f.TemplateID, FormCode: f.Code, Letter: c.Letter, Title: c.Title, Unit: c.Unit})
			}
		}
	}
	// Bước 2: phạm vi xã.
	all, err := s.Regions.CommuneRegions(ctx, a.province)
	if err != nil {
		return pv, err
	}
	switch in.ScopeMode {
	case ScopeProvince, "":
		for _, c := range all {
			pv.Communes = append(pv.Communes, c)
		}
	case ScopeRegions:
		if len(in.Regions) == 0 {
			return pv, problem.Validation.WithField("regions", "chọn ít nhất một khu vực")
		}
		for _, c := range all {
			if slices.Contains(in.Regions, c.RegionCode) {
				pv.Communes = append(pv.Communes, c)
			}
		}
	case ScopeCommunes:
		for _, code := range in.Communes {
			c, ok := all[code]
			if !ok {
				return pv, ErrNoCommunes.WithField("communes", code+": không phải xã đang hoạt động của tỉnh")
			}
			if !slices.ContainsFunc(pv.Communes, func(x partyport.CommuneRegion) bool { return x.CommuneCode == code }) {
				pv.Communes = append(pv.Communes, c)
			}
		}
	default:
		return pv, problem.Validation.WithField("scope.mode", "province | regions | communes")
	}
	if len(pv.Communes) == 0 {
		return pv, ErrNoCommunes
	}
	sort.Slice(pv.Communes, func(i, j int) bool {
		x, y := pv.Communes[i], pv.Communes[j]
		if x.SortOrder != y.SortOrder {
			return x.SortOrder < y.SortOrder
		}
		return x.CommuneCode < y.CommuneCode
	})
	// Cán bộ xã phụ trách CÙNG phòng ban (FN-TI-02.02 quy tắc); xã chưa có → cảnh báo.
	asg, err := s.Assignments.ActiveAssignments(ctx, authz.KNXa, a.k.Department)
	if err != nil {
		return pv, err
	}
	now := s.now()
	for _, x := range asg {
		if !x.EffectiveAt(now) {
			continue
		}
		for _, c := range x.Communes {
			if !slices.Contains(pv.Officers[c], x.UserRef) {
				pv.Officers[c] = append(pv.Officers[c], x.UserRef)
			}
		}
	}
	for _, c := range pv.Communes {
		if len(pv.Officers[c.CommuneCode]) == 0 {
			pv.CommunesNoOfficer = append(pv.CommunesNoOfficer, c.CommuneCode)
		}
	}
	if n := len(pv.CommunesNoOfficer); n > 0 {
		pv.Warnings = append(pv.Warnings, fmt.Sprintf("%d xã/phường chưa có cán bộ phụ trách cùng phòng ban", n))
	}
	pv.TaskCount = len(pv.Forms) * len(pv.Communes) * len(pv.Periods)
	return pv, nil
}

// BatchView là đợt kèm kỳ.
type BatchView struct {
	Batch   store.Batch
	Periods []store.Period
}

// IssueResult là kết quả phát hành.
type IssueResult struct {
	BatchView
	Warnings          []string
	CommunesNoOfficer []string
}

// IssueBatch phát hành đợt: sinh công việc = mẫu × xã × kỳ, giá trị Tỉnh giao, thông báo (FN-TI-02.02).
func (s *Service) IssueBatch(ctx context.Context, in BatchInput, idem string) (IssueResult, error) {
	a, err := s.writer(ctx, authz.KNTinh)
	if err != nil {
		return IssueResult{}, err
	}
	pv, err := s.preview(ctx, a, in, true)
	if err != nil {
		return IssueResult{}, err
	}
	now := s.now()
	note := strings.TrimSpace(in.Note)
	if len([]rune(note)) > 2000 {
		return IssueResult{}, problem.Validation.WithField("note", "tối đa 2000 ký tự")
	}
	pvs, err := provincialRows(pv, in)
	if err != nil {
		return IssueResult{}, err
	}
	b := store.Batch{ID: ids.New(), DepartmentCode: pv.department, ProvinceCode: pv.province, ReportType: pv.ReportType, PeriodType: pv.PeriodType,
		SeasonLabel: strings.TrimSpace(in.SeasonLabel), From: pv.from, To: pv.to, PeriodRule: pv.Rule.Tag(), DeadlineOffsetDays: pv.Deadline.OffsetDays,
		DeadlineTime: pv.Deadline.Time, CloseAfterDays: pv.CloseAfterDays, Status: "issued", PeriodCount: len(pv.Periods), TaskCount: pv.TaskCount,
		IssuedBy: a.p.ActorRef, IssuedAt: now, LastEventID: ids.New()}
	if note != "" {
		b.Note = &note
	}
	for _, f := range pv.Forms {
		b.Forms = append(b.Forms, store.BatchForm{TemplateID: f.Version.TemplateID, VersionID: f.Version.VersionID, Code: f.Version.Code,
			Name: f.Version.Name, Version: f.Version.Version, PeriodBlock: f.PeriodBlock})
	}
	for _, c := range pv.Communes {
		b.Communes = append(b.Communes, c.CommuneCode)
	}
	var periods []store.Period
	for _, p := range pv.Periods {
		sp := store.Period{ID: ids.New(), BatchID: b.ID, Slot: p.Slot, Label: p.Label, RangeLabel: p.RangeLabel, From: p.From, To: p.To, DueAt: p.DueAt,
			AutoCloseAt: p.AutoCloseAt, Status: "open", DepartmentCode: b.DepartmentCode, ProvinceCode: b.ProvinceCode}
		if p.ISOWeek > 0 {
			w := p.ISOWeek
			sp.ISOWeek = &w
		}
		if p.Key != "" {
			k := p.Key
			sp.PeriodKey = &k
			if p.WeekYear > 0 {
				y := p.WeekYear
				sp.WeekYear = &y
			}
		}
		periods = append(periods, sp)
	}
	var tasks []store.NewTask
	assignees := map[string]port.TaskAssigneesV1{}
	for _, c := range pv.Communes {
		csv := "{" + strings.Join(refStrings(pv.Officers[c.CommuneCode]), ",") + "}"
		for _, f := range b.Forms {
			for _, p := range periods {
				tasks = append(tasks, store.NewTask{ID: ids.New(), PeriodID: p.ID, Form: f, Commune: c.CommuneCode, Region: c.RegionCode, Slot: p.Slot,
					DueAt: p.DueAt, AssigneeCSV: csv})
			}
		}
		assignees[c.CommuneCode] = port.TaskAssigneesV1{CommuneCode: c.CommuneCode, OfficerRefs: refStrings(pv.Officers[c.CommuneCode]),
			TaskCount: len(b.Forms) * len(periods)}
	}
	payload, err := json.Marshal(map[string]any{
		"report_type": b.ReportType, "period_type": b.PeriodType, "season_label": b.SeasonLabel, "from_date": in.From, "to_date": in.To,
		"forms": b.Forms, "scope": map[string]any{"mode": in.ScopeMode, "regions": in.Regions, "communes": b.Communes},
		"deadline": map[string]any{"offset_days": b.DeadlineOffsetDays, "time": b.DeadlineTime}, "close_after_days": b.CloseAfterDays,
		"period_rule": b.PeriodRule, "task_count": b.TaskCount, "communes_no_officer": pv.CommunesNoOfficer, "note": b.Note,
	})
	if err != nil {
		return IssueResult{}, err
	}
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		ev := store.BatchEvent{ID: b.LastEventID, BatchID: b.ID, Action: "issued", DepartmentCode: b.DepartmentCode, ProvinceCode: b.ProvinceCode,
			Payload: payload, OnBehalfOf: a.onBehalf(), EventTime: now, ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "batch")}
		if err := store.InsertBatchEvent(ctx, tx, a.p.TenantID, &ev); err != nil {
			return err
		}
		if err := store.InsertBatch(ctx, tx, a.p.TenantID, b); err != nil {
			return err
		}
		for _, p := range periods {
			if err := store.InsertPeriod(ctx, tx, a.p.TenantID, p); err != nil {
				return err
			}
			// W11.05b: khóa kỳ theo lịch kỳ hệ thống LƯU lúc phát hành (sổ report_period_key), không tính lại về sau.
			if p.PeriodKey != nil {
				k := store.PeriodKey{ID: ids.New(), PeriodID: p.ID, BatchID: b.ID, PeriodType: b.PeriodType, Key: *p.PeriodKey, WeekYear: p.WeekYear,
					PeriodRule: b.PeriodRule, EventTime: now, ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "key:"+strconv.Itoa(p.Slot))}
				if err := store.InsertPeriodKey(ctx, tx, a.p.TenantID, k); err != nil {
					return err
				}
			}
		}
		if err := store.InsertTasks(ctx, tx, a.p.TenantID, b.ID, b.DepartmentCode, b.ProvinceCode, tasks, now); err != nil {
			return err
		}
		// Giá trị Tỉnh giao: nhập trực tiếp / Excel, rồi lấy từ đợt trước cho xã còn trống.
		for _, tpl := range in.CopyProvincial {
			prev, err := store.PreviousBatch(ctx, tx, b.ProvinceCode, b.DepartmentCode, tpl, b.ID)
			if err != nil {
				return err
			}
			if prev == nil {
				continue
			}
			vals, err := store.ProvincialValues(ctx, tx, prev.ID, &tpl)
			if err != nil {
				return err
			}
			for commune, cells := range vals[tpl] {
				if !slices.Contains(b.Communes, commune) || hasPV(pvs, tpl, commune) {
					continue
				}
				src := prev.ID
				pvs = append(pvs, store.ProvincialValue{TemplateID: tpl, Commune: commune, Cells: cells, Source: PVPrevious, SourceBatchID: &src})
			}
		}
		for i := range pvs {
			pvs[i].ID, pvs[i].BatchID, pvs[i].Department, pvs[i].EventTime, pvs[i].ActorRef = ids.New(), b.ID, b.DepartmentCode, now, a.p.ActorRef
			pvs[i].IdemKey = idemOr(idem, "pv:"+pvs[i].TemplateID.String()+":"+pvs[i].Commune)
		}
		if err := store.InsertProvincialValues(ctx, tx, a.p.TenantID, pvs); err != nil {
			return err
		}
		list := make([]port.TaskAssigneesV1, 0, len(assignees))
		for _, c := range b.Communes {
			list = append(list, assignees[c])
		}
		codes := make([]string, 0, len(b.Forms))
		for _, f := range b.Forms {
			codes = append(codes, f.Code)
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: a.p.TenantID, Type: port.EventReportBatchIssued, SchemaVersion: 1, OccurredAt: now,
			Producer: port.EventProducerName, Payload: port.ReportBatchIssuedV1{BatchID: b.ID.String(), DepartmentCode: b.DepartmentCode,
				ProvinceCode: b.ProvinceCode, ReportType: b.ReportType, SeasonLabel: b.SeasonLabel, FormCodes: codes, PeriodCount: len(periods),
				TaskCount: len(tasks), FirstDueAt: ts(periods[0].DueAt), Assignees: list, IssuedBy: a.p.ActorRef.String(), EventTime: ts(now)}})
	})
	if err != nil {
		return IssueResult{}, err
	}
	s.log().Info("phát hành biểu", "event", "extension.report_batch.issued", "batch_id", b.ID, "tasks", len(tasks), "outcome", "ok")
	return IssueResult{BatchView: BatchView{Batch: b, Periods: periods}, Warnings: pv.Warnings, CommunesNoOfficer: pv.CommunesNoOfficer}, nil
}

func hasPV(vs []store.ProvincialValue, tpl uuid.UUID, commune string) bool {
	return slices.ContainsFunc(vs, func(v store.ProvincialValue) bool { return v.TemplateID == tpl && v.Commune == commune })
}

// provincialRows kiểm giá trị Tỉnh giao: chỉ cột loại "Tỉnh giao" của mẫu trong đợt, xã trong phạm vi, số decimal.
func provincialRows(pv Preview, in BatchInput) ([]store.ProvincialValue, error) {
	src := in.ProvincialSource
	if src == "" {
		src = PVManual
	}
	if src != PVManual && src != PVExcel {
		return nil, problem.Validation.WithField("provincial_source", "manual | excel")
	}
	cols := map[uuid.UUID]map[string]repport.FormColumn{}
	for _, f := range pv.Forms {
		m := map[string]repport.FormColumn{}
		for _, c := range f.Version.Definition.Columns {
			if c.Kind == repport.ColumnProvincial {
				m[c.Letter] = c
			}
		}
		cols[f.Version.TemplateID] = m
	}
	communes := map[string]bool{}
	for _, c := range pv.Communes {
		communes[c.CommuneCode] = true
	}
	var out []store.ProvincialValue
	tpls := make([]uuid.UUID, 0, len(in.ProvincialValues))
	for t := range in.ProvincialValues {
		tpls = append(tpls, t)
	}
	slices.SortFunc(tpls, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, tpl := range tpls {
		m, ok := cols[tpl]
		if !ok {
			return nil, problem.Validation.WithField("provincial_values", tpl.String()+": mẫu không thuộc đợt")
		}
		byCommune := in.ProvincialValues[tpl]
		cs := make([]string, 0, len(byCommune))
		for c := range byCommune {
			cs = append(cs, c)
		}
		sort.Strings(cs)
		for _, commune := range cs {
			if !communes[commune] {
				return nil, problem.Validation.WithField("provincial_values", commune+": xã không nhận đợt này")
			}
			cells := map[string]string{}
			for letter, v := range byCommune[commune] {
				l := strings.ToUpper(strings.TrimSpace(letter))
				col, ok := m[l]
				if !ok {
					return nil, problem.Validation.WithField("provincial_values", l+": không phải cột Tỉnh giao của mẫu")
				}
				v = strings.TrimSpace(v)
				if v == "" {
					continue
				}
				if col.DType == repport.DTypeNumber || col.DType == repport.DTypeInteger {
					d, err := decimal.NewFromString(v)
					if err != nil || d.IsNegative() {
						return nil, problem.Validation.WithField("provincial_values", commune+"/"+l+": số không âm (decimal)")
					}
					v = d.String()
				}
				cells[l] = v
			}
			if len(cells) > 0 {
				out = append(out, store.ProvincialValue{TemplateID: tpl, Commune: commune, Cells: cells, Source: src})
			}
		}
	}
	return out, nil
}

// ListBatches trả đợt đã phát hành của phạm vi (Cấp Tỉnh, TW, Admin).
func (s *Service) ListBatches(ctx context.Context, province, reportType string, before *uuid.UUID, limit int) ([]store.Batch, *uuid.UUID, error) {
	a, err := s.knActor(ctx, authz.KNTinh, authz.KNTW, authz.KNAdmin)
	if err != nil {
		return nil, nil, err
	}
	f := store.BatchFilter{Department: a.dept(), ReportType: reportType, Before: before, Limit: clampLimit(limit) + 1}
	switch {
	case a.province != "":
		f.Province = a.province
	case province != "":
		f.Province = province
	default:
		f.AllProvinces = true
	}
	var out []store.Batch
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = store.ListBatches(ctx, tx, f)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	var next *uuid.UUID
	if len(out) > clampLimit(limit) {
		out = out[:clampLimit(limit)]
		id := out[len(out)-1].ID
		next = &id
	}
	return out, next, nil
}

func clampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

// GetBatch trả đợt kèm kỳ.
func (s *Service) GetBatch(ctx context.Context, id uuid.UUID) (BatchView, error) {
	a, err := s.knActor(ctx, authz.KNTinh, authz.KNTW, authz.KNAdmin)
	if err != nil {
		return BatchView{}, err
	}
	var v BatchView
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		b, err := s.batchInScope(ctx, tx, a, id, false)
		if err != nil {
			return err
		}
		v.Batch = *b
		v.Periods, err = store.PeriodsOfBatch(ctx, tx, id)
		return err
	})
	return v, err
}

func (s *Service) batchInScope(ctx context.Context, tx pgx.Tx, a actor, id uuid.UUID, lock bool) (*store.Batch, error) {
	b, err := store.BatchByID(ctx, tx, id, lock)
	if err != nil {
		return nil, err
	}
	if b == nil || !a.allowsProvince(b.ProvinceCode, b.DepartmentCode) {
		return nil, ErrBatchNotFound
	}
	return b, nil
}

// WithdrawBatch thu hồi đợt chưa có xã nào nộp (sự kiện bù, lý do bắt buộc).
func (s *Service) WithdrawBatch(ctx context.Context, id uuid.UUID, reason, idem string) (store.Batch, error) {
	a, err := s.writer(ctx, authz.KNTinh)
	if err != nil {
		return store.Batch{}, err
	}
	why, ok := domain.Reason(reason, 500)
	if !ok {
		return store.Batch{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	now := s.now()
	var out store.Batch
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		b, err := s.batchInScope(ctx, tx, a, id, true)
		if err != nil {
			return err
		}
		if b.Status == "withdrawn" {
			return ErrBatchWithdrawn
		}
		n, err := store.CountSubmittedInBatch(ctx, tx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrBatchHasSubmissions.WithDetail(fmt.Sprintf("%d công việc đã có bản gửi", n))
		}
		ev := store.BatchEvent{ID: ids.New(), BatchID: id, Action: "withdrawn", DepartmentCode: b.DepartmentCode, ProvinceCode: b.ProvinceCode,
			Payload: []byte(`{}`), Reason: &why, OnBehalfOf: a.onBehalf(), EventTime: now, ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "withdraw")}
		if err := store.InsertBatchEvent(ctx, tx, a.p.TenantID, &ev); err != nil {
			return err
		}
		if err := store.WithdrawBatch(ctx, tx, id, a.p.ActorRef, ev.ID, why, now); err != nil {
			return err
		}
		if err := store.WithdrawTasks(ctx, tx, id, now); err != nil {
			return err
		}
		if err := s.mirrorWithdrawn(ctx, tx, id, why, a.p.ActorRef, ev.IdemKey, now); err != nil { // W11.05: hủy kỳ ở cap/forms
			return err
		}
		nb, err := store.BatchByID(ctx, tx, id, false)
		if err == nil && nb != nil {
			out = *nb
		}
		return err
	})
	return out, err
}

// DeadlineInput là yêu cầu sửa hạn nộp.
type DeadlineInput struct {
	PeriodIDs      []uuid.UUID // rỗng = mọi kỳ chưa đến hạn
	DueAt          *time.Time  // hạn tuyệt đối (một kỳ) hoặc
	Deadline       *domain.Deadline
	CloseAfterDays *int
	Reason         string
}

// DeadlineChange là thay đổi hạn của một kỳ.
type DeadlineChange struct {
	PeriodID           uuid.UUID `json:"period_id"`
	Label              string    `json:"label"`
	OldDue, NewDue     time.Time `json:"-"`
	OldClose, NewClose time.Time `json:"-"`
}

// ChangeDeadline sửa hạn nộp — CHỈ áp cho kỳ chưa đến hạn (FN-TI-02.02 luồng phụ); sự kiện deadline_changed.
func (s *Service) ChangeDeadline(ctx context.Context, id uuid.UUID, in DeadlineInput, idem string) ([]DeadlineChange, error) {
	a, err := s.writer(ctx, authz.KNTinh)
	if err != nil {
		return nil, err
	}
	why, ok := domain.Reason(in.Reason, 500)
	if !ok {
		return nil, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	if (in.DueAt == nil) == (in.Deadline == nil) {
		return nil, problem.Validation.WithField("due_at", "chọn một: due_at (hạn mới) hoặc deadline (quy tắc hạn)")
	}
	now := s.now()
	var changes []DeadlineChange
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		b, err := s.batchInScope(ctx, tx, a, id, true)
		if err != nil {
			return err
		}
		if b.Status == "withdrawn" {
			return ErrBatchWithdrawn
		}
		ps, err := store.PeriodsOfBatch(ctx, tx, id)
		if err != nil {
			return err
		}
		closeAfter := b.CloseAfterDays
		if in.CloseAfterDays != nil {
			if *in.CloseAfterDays < 0 || *in.CloseAfterDays > 60 {
				return problem.Validation.WithField("close_after_days", "0–60 ngày")
			}
			closeAfter = *in.CloseAfterDays
		}
		for _, p := range ps {
			if len(in.PeriodIDs) > 0 && !slices.Contains(in.PeriodIDs, p.ID) {
				continue
			}
			if p.Status != "open" || !now.Before(p.DueAt) {
				continue // kỳ đã đến hạn/đã chốt giữ hạn cũ
			}
			var due time.Time
			if in.DueAt != nil {
				due = in.DueAt.UTC()
			} else if due, err = in.Deadline.DueAt(p.To, clock.Zone()); err != nil {
				return problem.Validation.WithField("deadline.time", err.Error())
			}
			if !due.After(now) {
				return problem.Validation.WithField("due_at", "hạn mới phải sau thời điểm hiện tại")
			}
			ch := DeadlineChange{PeriodID: p.ID, Label: p.Label, OldDue: p.DueAt, NewDue: due, OldClose: p.AutoCloseAt, NewClose: due.AddDate(0, 0, closeAfter)}
			if err := store.SetPeriodDue(ctx, tx, p.ID, ch.NewDue, ch.NewClose, now); err != nil {
				return err
			}
			changes = append(changes, ch)
		}
		if len(changes) == 0 {
			return ErrNoFuturePeriod
		}
		items := make([]map[string]string, 0, len(changes))
		for _, c := range changes {
			items = append(items, map[string]string{"period_id": c.PeriodID.String(), "label": c.Label, "old_due_at": ts(c.OldDue), "new_due_at": ts(c.NewDue),
				"old_auto_close_at": ts(c.OldClose), "new_auto_close_at": ts(c.NewClose)})
		}
		payload, _ := json.Marshal(map[string]any{"changes": items})
		ev := store.BatchEvent{ID: ids.New(), BatchID: id, Action: "deadline_changed", DepartmentCode: b.DepartmentCode, ProvinceCode: b.ProvinceCode,
			Payload: payload, Reason: &why, OnBehalfOf: a.onBehalf(), EventTime: now, ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "deadline")}
		if err := store.InsertBatchEvent(ctx, tx, a.p.TenantID, &ev); err != nil {
			return err
		}
		return store.TouchBatch(ctx, tx, id, ev.ID, now)
	})
	return changes, err
}

// ProvincialValuesOf trả giá trị Tỉnh giao của đợt (Cấp Xã chỉ thấy xã mình).
func (s *Service) ProvincialValuesOf(ctx context.Context, batchID uuid.UUID, templateID *uuid.UUID) (map[uuid.UUID]map[string]map[string]string, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return nil, err
	}
	var out map[uuid.UUID]map[string]map[string]string
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		b, err := s.batchInScope(ctx, tx, a, batchID, false)
		if err != nil {
			return err
		}
		out, err = store.ProvincialValues(ctx, tx, b.ID, templateID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if a.k.Role == authz.KNXa {
		for tpl, m := range out {
			for c := range m {
				if !slices.Contains(a.k.Communes, c) {
					delete(out[tpl], c)
				}
			}
		}
	}
	return out, nil
}

// BatchLog trả nhật ký đợt (Detail Log).
func (s *Service) BatchLog(ctx context.Context, id uuid.UUID) ([]LogEntry, error) {
	a, err := s.knActor(ctx, authz.KNTinh, authz.KNTW, authz.KNAdmin)
	if err != nil {
		return nil, err
	}
	var evs []store.BatchEvent
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, err := s.batchInScope(ctx, tx, a, id, false); err != nil {
			return err
		}
		var err error
		evs, err = store.BatchEvents(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	refs := make([]uuid.UUID, 0, len(evs))
	for _, e := range evs {
		refs = append(refs, e.ActorRef)
	}
	names := s.names(ctx, refs)
	out := make([]LogEntry, 0, len(evs))
	for _, e := range evs {
		out = append(out, LogEntry{Event: "batch." + e.Action, ActorRef: e.ActorRef, ActorDisplay: names[e.ActorRef], At: e.EventTime, Reason: e.Reason,
			OnBehalfOf: e.OnBehalfOf, Detail: e.Payload})
	}
	return out, nil
}

// LogEntry là một dòng khung Log chuẩn (kế hoạch §1.5).
type LogEntry struct {
	Event        string
	ActorRef     uuid.UUID
	ActorDisplay string
	At           time.Time
	Reason       *string
	OnBehalfOf   *uuid.UUID
	Changes      []Change
	Detail       json.RawMessage
}

// Change là thay đổi trường (cũ → mới).
type Change struct {
	Field string  `json:"field"`
	Old   *string `json:"old"`
	New   *string `json:"new"`
}
