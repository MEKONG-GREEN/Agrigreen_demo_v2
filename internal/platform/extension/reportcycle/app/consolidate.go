package app

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

func zone() *time.Location { return clock.Zone() }

// SnapRow là dòng xã trong ảnh chụp (kèm tên xã/khu vực lúc chạy).
type SnapRow struct {
	Key         string            `json:"key"`
	Commune     string            `json:"commune"`
	CommuneName string            `json:"commune_name"`
	Region      string            `json:"region"`
	RegionName  string            `json:"region_name"`
	Indicator   string            `json:"indicator,omitempty"`
	Cells       map[string]string `json:"cells"`
}

// SnapTotal là dòng Cộng khu vực / Toàn tỉnh.
type SnapTotal struct {
	Key        string            `json:"key"`
	Kind       string            `json:"kind"`
	Region     string            `json:"region,omitempty"`
	RegionName string            `json:"region_name,omitempty"`
	Indicator  string            `json:"indicator,omitempty"`
	Members    int               `json:"members"`
	Cells      map[string]string `json:"cells"`
}

type consolidateOut struct {
	runs     []store.Run
	upToDate []string
}

func (s *Service) afterConsolidate(c consolidateOut) {
	for _, r := range c.runs {
		s.log().Info("tổng hợp các xã", "event", "extension.consolidation.completed", "run_id", r.ID, "form", r.FormCode, "trigger", r.Trigger,
			"cells_changed", r.CellsChanged, "outcome", "ok")
	}
}

// consolidateTx dựng lần tổng hợp cho các biểu của kỳ (templates nil = mọi biểu): lấy bản "Đã gửi" mới nhất từng
// xã, tính lại công thức và dòng Khu vực/Toàn tỉnh (FormEngine.Totals), so khác ô với lần trước, ghi lần chạy
// bất biến + ảnh chụp + chỉ số dashboard. Không có thay đổi → "Dữ liệu đã mới nhất" (không ghi).
func (s *Service) consolidateTx(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p store.Period, b store.Batch, templates []uuid.UUID, trigger string,
	actor uuid.UUID, obo *uuid.UUID, idem string, now time.Time) (consolidateOut, error) {
	var out consolidateOut
	if s.Forms == nil {
		return out, ErrDependency.WithDetail("chưa nối engine mẫu báo cáo (pf/reports)")
	}
	nb, err := s.regionNames(ctx, p.ProvinceCode)
	if err != nil {
		return out, err
	}
	latest, err := store.LatestOfPeriod(ctx, tx, p.ID)
	if err != nil {
		return out, err
	}
	fc := s.forms(tid)
	var codes []string
	cells := 0
	for _, f := range b.Forms {
		if templates != nil && !slices.Contains(templates, f.TemplateID) {
			continue
		}
		fi, err := fc.get(ctx, f.VersionID)
		if err != nil {
			return out, err
		}
		run, changed, err := s.consolidateForm(ctx, tx, tid, p, b, f, fi, latest, nb, trigger, actor, obo, idem, now)
		if err != nil {
			return out, err
		}
		if !changed {
			out.upToDate = append(out.upToDate, f.Code)
			continue
		}
		out.runs = append(out.runs, run)
		codes = append(codes, f.Code)
		cells += run.CellsChanged
	}
	if len(out.runs) == 0 {
		return out, nil
	}
	runIDs := make([]uuid.UUID, 0, len(out.runs))
	for _, r := range out.runs {
		runIDs = append(runIDs, r.ID)
	}
	return out, outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventConsolidationCompleted, SchemaVersion: 1, OccurredAt: now,
		Producer: port.EventProducerName, Payload: port.ConsolidationCompletedV1{RunIDs: refStrings(runIDs), PeriodID: p.ID.String(),
			BatchID: p.BatchID.String(), FormCodes: codes, Trigger: trigger, DepartmentCode: p.DepartmentCode, ProvinceCode: p.ProvinceCode,
			CellsChanged: cells, EventTime: ts(now)}})
}

func (s *Service) consolidateForm(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p store.Period, b store.Batch, f store.BatchForm, fi formInfo,
	latest map[uuid.UUID]store.Latest, nb *nameBook, trigger string, actor uuid.UUID, obo *uuid.UUID, idem string, now time.Time) (store.Run, bool, error) {
	tpl := f.TemplateID
	all, err := store.ListTasks(ctx, tx, store.TaskFilter{BatchID: &b.ID, TemplateID: &tpl})
	if err != nil {
		return store.Run{}, false, err
	}
	// Bản "Đã gửi" mới nhất của mọi công việc cùng đợt × biểu (khe kỳ ≤ kỳ này) — một lần đọc.
	var subIDs []uuid.UUID
	for _, t := range all {
		if t.SubmittedSubmissionID != nil && t.Slot <= p.Slot {
			subIDs = append(subIDs, *t.SubmittedSubmissionID)
		}
	}
	subs, err := store.SubmissionsByIDs(ctx, tx, subIDs)
	if err != nil {
		return store.Run{}, false, err
	}
	// Tham chiếu liên biểu cùng kỳ (T03 ↔ T01): bản gửi của biểu được tham chiếu.
	xrefRows := map[string]map[string]map[string]string{} // mã mẫu → xã → ô
	for _, code := range fi.xrefCodes {
		ts, err := store.ListTasks(ctx, tx, store.TaskFilter{PeriodID: &p.ID, FormCode: code})
		if err != nil {
			return store.Run{}, false, err
		}
		var ids_ []uuid.UUID
		for _, t := range ts {
			if t.SubmittedSubmissionID != nil {
				ids_ = append(ids_, *t.SubmittedSubmissionID)
			}
		}
		xs, err := store.SubmissionsByIDs(ctx, tx, ids_)
		if err != nil {
			return store.Run{}, false, err
		}
		xrefRows[code] = map[string]map[string]string{}
		for _, t := range ts {
			if t.SubmittedSubmissionID != nil {
				if sub, ok := xs[*t.SubmittedSubmissionID]; ok && len(sub.Rows) > 0 {
					xrefRows[code][t.CommuneCode] = sub.Rows[0].Cells
				}
			}
		}
	}
	pvs, err := store.ProvincialValues(ctx, tx, b.ID, &tpl)
	if err != nil {
		return store.Run{}, false, err
	}
	// Giá trị khe kỳ khác theo xã.
	slotVals := map[string]map[string]map[string]string{} // xã → khóa dòng → chữ cái → giá trị
	if fi.block {
		for _, t := range all {
			if t.PeriodID == p.ID || t.SubmittedSubmissionID == nil || t.Slot > p.Slot {
				continue
			}
			sub, ok := subs[*t.SubmittedSubmissionID]
			if !ok {
				continue
			}
			for _, r := range sub.Rows {
				k := rowKey(t.CommuneCode, r.Indicator)
				for _, l := range fi.slotLetter[t.Slot] {
					if v, ok := r.Input[l]; ok && v != "" {
						if slotVals[t.CommuneCode] == nil {
							slotVals[t.CommuneCode] = map[string]map[string]string{}
						}
						if slotVals[t.CommuneCode][k] == nil {
							slotVals[t.CommuneCode][k] = map[string]string{}
						}
						slotVals[t.CommuneCode][k][l] = v
					}
				}
			}
		}
	}
	var evalRows []repport.EvalRow
	sources := map[string]domain.Source{}
	var missing []string
	var regions []store.RegionRef
	communesTotal := 0
	for _, t := range all {
		if t.PeriodID != p.ID {
			continue
		}
		communesTotal++
		cname, region := nb.commune(ctx, s, t.CommuneCode), nb.regionOf(t.CommuneCode)
		regions = append(regions, store.RegionRef{Commune: t.CommuneCode, CommuneName: cname, Region: region, RegionName: nb.region(region),
			SortOrder: nb.byCommune[t.CommuneCode].SortOrder})
		if t.SubmittedSubmissionID == nil {
			missing = append(missing, t.CommuneCode)
			continue
		}
		sub, ok := subs[*t.SubmittedSubmissionID]
		if !ok {
			missing = append(missing, t.CommuneCode)
			continue
		}
		sources[t.CommuneCode] = domain.Source{SubmissionID: sub.ID.String(), VersionNo: sub.VersionNo, SubmittedAt: ts(sub.EventTime),
			ActorRef: sub.ActorRef.String(), OnBehalfOf: ptrString(sub.OnBehalfOf), AdjustmentResponse: sub.AdjustmentResponse}
		system := systemCells(fi, cname, nb.region(region), pvs[tpl][t.CommuneCode])
		var xr map[string]map[string]string
		for code, m := range xrefRows {
			if c, ok := m[t.CommuneCode]; ok {
				if xr == nil {
					xr = map[string]map[string]string{}
				}
				xr[code] = c
			}
		}
		for _, r := range sub.Rows {
			k := rowKey(t.CommuneCode, r.Indicator)
			in := map[string]string{}
			for l, v := range r.Input {
				if slot, ok := fi.letterSlot[l]; ok && slot != t.Slot {
					continue
				}
				in[l] = v
			}
			for l, v := range slotVals[t.CommuneCode][k] {
				in[l] = v
			}
			evalRows = append(evalRows, repport.EvalRow{Key: k, Commune: t.CommuneCode, Region: region, Indicator: r.Indicator, Input: in, System: system, XRefs: xr})
		}
	}
	sort.Slice(regions, func(i, j int) bool {
		if regions[i].SortOrder != regions[j].SortOrder {
			return regions[i].SortOrder < regions[j].SortOrder
		}
		return regions[i].Commune < regions[j].Commune
	})
	res := s.Forms.Evaluate(fi.def, repport.EvalInput{Mode: repport.EvalDraft, Rows: evalRows})
	totals := s.Forms.Totals(fi.def, res.Rows)
	rows := make([]domain.Row, 0, len(res.Rows))
	for _, r := range res.Rows {
		rows = append(rows, domain.Row{Key: r.Key, Commune: r.Commune, Region: r.Region, Indicator: r.Indicator, Cells: r.Cells})
	}
	// Lần trước.
	var prev *store.Run
	var prevRows []domain.Row
	prevSrc := map[string]domain.Source{}
	if l, ok := latest[tpl]; ok {
		if prev, err = store.RunByID(ctx, tx, l.RunID); err != nil {
			return store.Run{}, false, err
		}
		snap, err := store.SnapshotOf(ctx, tx, l.RunID)
		if err != nil {
			return store.Run{}, false, err
		}
		if snap != nil {
			if err := json.Unmarshal(snap.Rows, &prevRows); err != nil {
				return store.Run{}, false, err
			}
			if err := json.Unmarshal(snap.Sources, &prevSrc); err != nil {
				return store.Run{}, false, err
			}
		}
	}
	changes, communesChanged := domain.Diff(prevRows, rows, prevSrc, sources, fi.formula)
	sort.Strings(missing)
	provisional := p.Status != "closed" || len(missing) > 0
	if prev != nil && len(changes) == 0 && prev.Provisional == provisional && sameSources(prevSrc, sources) {
		return store.Run{}, false, nil
	}
	run := store.Run{ID: ids.New(), RunNo: 1, PeriodID: p.ID, BatchID: b.ID, TemplateID: tpl, VersionID: f.VersionID, FormCode: f.Code,
		DepartmentCode: p.DepartmentCode, ProvinceCode: p.ProvinceCode, Trigger: trigger, CommunesTotal: communesTotal, CommunesWithData: len(sources),
		CommunesChanged: communesChanged, CellsChanged: len(changes), Provisional: provisional, MissingCommunes: missing, Regions: regions, OnBehalfOf: obo,
		EventTime: now, ActorRef: actor, IdemKey: idem + ":" + tpl.String()}
	if prev != nil {
		run.RunNo, run.PreviousRunID = prev.RunNo+1, &prev.ID
	}
	if err := store.InsertRun(ctx, tx, tid, &run); err != nil {
		return store.Run{}, false, err
	}
	snapRows := make([]SnapRow, 0, len(rows))
	for _, r := range rows {
		snapRows = append(snapRows, SnapRow{Key: r.Key, Commune: r.Commune, CommuneName: nb.commune(ctx, s, r.Commune), Region: r.Region,
			RegionName: nb.region(r.Region), Indicator: r.Indicator, Cells: r.Cells})
	}
	snapTotals := make([]SnapTotal, 0, len(totals))
	for _, t := range totals {
		st := SnapTotal{Key: t.Key, Kind: t.Kind, Region: t.Region, Indicator: t.Indicator, Members: t.Members, Cells: t.Cells}
		if t.Kind == repport.TotalRegion {
			st.RegionName = nb.region(t.Region)
		}
		snapTotals = append(snapTotals, st)
	}
	rj, _ := json.Marshal(snapRows)
	tj, _ := json.Marshal(snapTotals)
	sj, _ := json.Marshal(sources)
	if err := store.InsertSnapshot(ctx, tx, tid, store.Snapshot{RunID: run.ID, PeriodID: p.ID, TemplateID: tpl, Rows: rj, Totals: tj, Sources: sj, CreatedAt: now}); err != nil {
		return store.Run{}, false, err
	}
	ccs := make([]store.CellChange, 0, len(changes))
	for _, c := range changes {
		cc := store.CellChange{ID: ids.New(), RunID: run.ID, PeriodID: p.ID, TemplateID: tpl, FormCode: f.Code, Commune: c.Commune, RowKey: c.RowKey,
			Letter: c.Letter, Old: c.Old, New: c.New, Kind: c.Kind, CreatedAt: now}
		if c.Source != nil {
			if id, err := uuid.Parse(c.Source.SubmissionID); err == nil {
				cc.Submission = &id
			}
			if by, err := uuid.Parse(c.Source.ActorRef); err == nil {
				cc.ChangedBy = &by
			}
			if c.Source.OnBehalfOf != nil {
				if o, err := uuid.Parse(*c.Source.OnBehalfOf); err == nil {
					cc.OnBehalfOf = &o
				}
			}
			if at, err := time.Parse(time.RFC3339, c.Source.SubmittedAt); err == nil {
				cc.SubmittedAt = &at
			}
		}
		ccs = append(ccs, cc)
	}
	if err := store.InsertCellChanges(ctx, tx, tid, ccs); err != nil {
		return store.Run{}, false, err
	}
	assign := map[string]partyport.CommuneRegion{}
	for _, r := range regions {
		assign[r.Commune] = partyport.CommuneRegion{CommuneCode: r.Commune, RegionCode: r.Region}
	}
	mv := domain.ExtractMetrics(f.Code, p.ProvinceCode, fi.def, rows, assign)
	ms := make([]store.Metric, 0, len(mv))
	for _, m := range mv {
		ms = append(ms, store.Metric{Metric: m.Metric, Scope: m.Scope, Unit: m.Unit, Value: m.Value})
	}
	if err := store.InsertMetrics(ctx, tx, tid, run.ID, p.ID, b.ID, f.Code, p.DepartmentCode, p.ProvinceCode, ms); err != nil {
		return store.Run{}, false, err
	}
	if err := store.UpsertLatest(ctx, tx, tid, run); err != nil {
		return store.Run{}, false, err
	}
	return run, true, nil
}

func sameSources(a, b map[string]domain.Source) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k].SubmissionID != v.SubmissionID {
			return false
		}
	}
	return true
}

// ConsolidateResult là kết quả "Cập nhật".
type ConsolidateResult struct {
	Runs     []store.Run
	UpToDate []string // mã mẫu "Dữ liệu đã mới nhất"
}

// Consolidate là nút "Cập nhật" của Cấp Tỉnh (FN-TI-02.05 B3).
func (s *Service) Consolidate(ctx context.Context, periodID uuid.UUID, templateIDs []uuid.UUID, idem string) (ConsolidateResult, error) {
	a, err := s.writer(ctx, authz.KNTinh)
	if err != nil {
		return ConsolidateResult{}, err
	}
	now := s.now()
	var c consolidateOut
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		p, b, err := s.periodInScope(ctx, tx, a, periodID, true)
		if err != nil {
			return err
		}
		var tpls []uuid.UUID
		if len(templateIDs) > 0 {
			tpls = templateIDs
		}
		c, err = s.consolidateTx(ctx, tx, a.p.TenantID, *p, *b, tpls, domain.TriggerManual, a.p.ActorRef, a.onBehalf(), idemOr(idem, "cons"), now)
		return err
	})
	if err != nil {
		return ConsolidateResult{}, err
	}
	s.afterConsolidate(c)
	return ConsolidateResult{Runs: c.runs, UpToDate: c.upToDate}, nil
}

// ConsolidationQuery chọn bảng tổng hợp để xem.
type ConsolidationQuery struct {
	PeriodID   uuid.UUID
	TemplateID *uuid.UUID
	FormCode   string
	RunID      *uuid.UUID // xem một lần cũ (mặc định lần gần nhất)
	Region     string     // lọc Khu vực
	Province   string     // Cấp Quốc gia chọn tỉnh
}

// CellMark là ô đã thay đổi trong lần chạy (viền vàng + tooltip giá trị cũ, người sửa, thời điểm).
type CellMark struct {
	RowKey      string
	Letter      string
	Old         *string
	Kind        string
	ChangedBy   *uuid.UUID
	SubmittedAt *time.Time
}

// ConsolidationView là bảng "Báo cáo tổng hợp các xã" của một lần chạy.
type ConsolidationView struct {
	Period          store.Period
	Batch           store.Batch
	Run             store.Run
	Rows            []SnapRow
	Totals          []SnapTotal
	Missing         []store.RegionRef
	Marks           []CellMark
	Pending         PendingSummary
	Names           map[uuid.UUID]string
	FormName        string
	Compare         *SnapTotal // "So sánh cùng kỳ năm trước" (dòng Toàn tỉnh)
	CompareLabel    string
}

// GetConsolidation trả bảng tổng hợp theo mẫu (Cấp Tỉnh; Cấp Quốc gia chỉ xem — FN-TW-01.02).
func (s *Service) GetConsolidation(ctx context.Context, q ConsolidationQuery, compare bool) (ConsolidationView, error) {
	a, err := s.knActor(ctx, authz.KNTinh, authz.KNTW, authz.KNAdmin)
	if err != nil {
		return ConsolidationView{}, err
	}
	var v ConsolidationView
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		p, b, err := s.periodInScope(ctx, tx, a, q.PeriodID, false)
		if err != nil {
			return err
		}
		v.Period, v.Batch = *p, *b
		var tpl uuid.UUID
		for _, f := range b.Forms {
			if (q.TemplateID != nil && f.TemplateID == *q.TemplateID) || (q.FormCode != "" && f.Code == q.FormCode) {
				tpl, v.FormName = f.TemplateID, f.Name
			}
		}
		if tpl == uuid.Nil && q.TemplateID == nil && q.FormCode == "" && len(b.Forms) > 0 {
			tpl, v.FormName = b.Forms[0].TemplateID, b.Forms[0].Name
		}
		if tpl == uuid.Nil {
			return ErrRunNotFound.WithDetail("biểu không thuộc đợt")
		}
		var run *store.Run
		if q.RunID != nil {
			if run, err = store.RunByID(ctx, tx, *q.RunID); err != nil {
				return err
			}
			if run != nil && (run.PeriodID != p.ID || run.TemplateID != tpl) {
				run = nil
			}
		} else {
			latest, err := store.LatestOfPeriod(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			if l, ok := latest[tpl]; ok {
				if run, err = store.RunByID(ctx, tx, l.RunID); err != nil {
					return err
				}
			}
		}
		if run == nil {
			return ErrRunNotFound
		}
		v.Run = *run
		snap, err := store.SnapshotOf(ctx, tx, run.ID)
		if err != nil {
			return err
		}
		var rows []SnapRow
		var totals []SnapTotal
		if err := json.Unmarshal(snap.Rows, &rows); err != nil {
			return err
		}
		if err := json.Unmarshal(snap.Totals, &totals); err != nil {
			return err
		}
		for _, r := range rows {
			if q.Region == "" || r.Region == q.Region {
				v.Rows = append(v.Rows, r)
			}
		}
		for _, t := range totals {
			if t.Kind == repport.TotalProvince || q.Region == "" || t.Region == q.Region {
				v.Totals = append(v.Totals, t)
			}
		}
		for _, r := range run.Regions {
			if slices.Contains(run.MissingCommunes, r.Commune) && (q.Region == "" || r.Region == q.Region) {
				v.Missing = append(v.Missing, r)
			}
		}
		chs, err := store.CellChanges(ctx, tx, run.ID, nil, 5000)
		if err != nil {
			return err
		}
		for _, c := range chs {
			v.Marks = append(v.Marks, CellMark{RowKey: c.RowKey, Letter: c.Letter, Old: c.Old, Kind: c.Kind, ChangedBy: c.ChangedBy, SubmittedAt: c.SubmittedAt})
		}
		if v.Pending, err = pendingOf(ctx, tx, *p, tpl, snap); err != nil {
			return err
		}
		if compare {
			v.Compare, v.CompareLabel, err = compareLastYear(ctx, tx, *p, b.ReportType, run.FormCode)
		}
		return err
	})
	if err != nil {
		return v, err
	}
	refs := []uuid.UUID{v.Run.ActorRef}
	for _, m := range v.Marks {
		if m.ChangedBy != nil {
			refs = append(refs, *m.ChangedBy)
		}
	}
	v.Names = s.names(ctx, refs)
	return v, nil
}

// compareLastYear tìm lần tổng hợp của kỳ cùng loại báo cáo có ngày bắt đầu gần nhất với cùng ngày năm trước (±10
// ngày) — "So sánh cùng kỳ năm trước" (FN-TW-01.02 phụ; dữ liệu lịch sử từ Google Sheet chờ khách Q-20).
func compareLastYear(ctx context.Context, tx pgx.Tx, p store.Period, reportType, formCode string) (*SnapTotal, string, error) {
	ps, err := store.ListPeriods(ctx, tx, store.PeriodFilter{Province: p.ProvinceCode, Department: p.DepartmentCode, ReportType: reportType, Limit: 400})
	if err != nil {
		return nil, "", err
	}
	target := p.From.AddDate(-1, 0, 0)
	var best *store.Period
	for i := range ps {
		d := ps[i].From.Sub(target)
		if d < 0 {
			d = -d
		}
		if d <= 10*24*time.Hour && (best == nil || absDur(ps[i].From.Sub(target)) < absDur(best.From.Sub(target))) {
			best = &ps[i]
		}
	}
	if best == nil {
		return nil, "", nil
	}
	ls, err := store.LatestForForm(ctx, tx, p.ProvinceCode, p.DepartmentCode, formCode, []uuid.UUID{best.ID})
	if err != nil || len(ls) == 0 {
		return nil, "", err
	}
	snap, err := store.SnapshotOf(ctx, tx, ls[0].RunID)
	if err != nil || snap == nil {
		return nil, "", err
	}
	var totals []SnapTotal
	if err := json.Unmarshal(snap.Totals, &totals); err != nil {
		return nil, "", err
	}
	for i := range totals {
		if totals[i].Kind == repport.TotalProvince && totals[i].Indicator == "" {
			return &totals[i], best.Label + " (" + best.RangeLabel + ")", nil
		}
	}
	return nil, "", nil
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// PendingItem là một bản gửi sau lần tổng hợp gần nhất ("Thay đổi chờ cập nhật").
type PendingItem struct {
	Commune      string
	FormCode     string
	TaskID       uuid.UUID
	SubmissionID uuid.UUID
	SubmittedAt  time.Time
	Kind         string // new | resubmit | adjusted
}

// PendingSummary là "Có n thay đổi mới từ m xã".
type PendingSummary struct {
	Changes  int
	Communes int
	Items    []PendingItem
}

func pendingOf(ctx context.Context, tx pgx.Tx, p store.Period, tpl uuid.UUID, snap *store.Snapshot) (PendingSummary, error) {
	var out PendingSummary
	src := map[string]domain.Source{}
	if snap != nil {
		if err := json.Unmarshal(snap.Sources, &src); err != nil {
			return out, err
		}
	}
	tasks, err := store.TasksOfPeriod(ctx, tx, p.ID, &tpl, false)
	if err != nil {
		return out, err
	}
	communes := map[string]bool{}
	for _, t := range tasks {
		if t.SubmittedSubmissionID == nil {
			continue
		}
		prev, ok := src[t.CommuneCode]
		if ok && prev.SubmissionID == t.SubmittedSubmissionID.String() {
			continue
		}
		kind := domain.ChangeNew
		if ok {
			kind = domain.ChangeResubmit
		}
		sub, err := store.SubmissionByID(ctx, tx, *t.SubmittedSubmissionID)
		if err != nil {
			return out, err
		}
		if sub != nil && sub.AdjustmentResponse && ok {
			kind = domain.ChangeAdjusted
		}
		item := PendingItem{Commune: t.CommuneCode, FormCode: t.FormCode, TaskID: t.ID, SubmissionID: *t.SubmittedSubmissionID, Kind: kind}
		if t.SubmittedAt != nil {
			item.SubmittedAt = *t.SubmittedAt
		}
		out.Items = append(out.Items, item)
		communes[t.CommuneCode] = true
	}
	out.Changes, out.Communes = len(out.Items), len(communes)
	return out, nil
}

// PendingChanges trả "Thay đổi chờ cập nhật" của mọi biểu trong kỳ.
func (s *Service) PendingChanges(ctx context.Context, periodID uuid.UUID) (PendingSummary, error) {
	a, err := s.knActor(ctx, authz.KNTinh, authz.KNTW, authz.KNAdmin)
	if err != nil {
		return PendingSummary{}, err
	}
	var out PendingSummary
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		p, b, err := s.periodInScope(ctx, tx, a, periodID, false)
		if err != nil {
			return err
		}
		latest, err := store.LatestOfPeriod(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		communes := map[string]bool{}
		for _, f := range b.Forms {
			var snap *store.Snapshot
			if l, ok := latest[f.TemplateID]; ok {
				if snap, err = store.SnapshotOf(ctx, tx, l.RunID); err != nil {
					return err
				}
			}
			ps, err := pendingOf(ctx, tx, *p, f.TemplateID, snap)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, ps.Items...)
			for _, it := range ps.Items {
				communes[it.Commune] = true
			}
		}
		out.Changes, out.Communes = len(out.Items), len(communes)
		return nil
	})
	return out, err
}

// RunsOf trả "Nhật ký cập nhật" (mọi lần chạy, không xóa).
func (s *Service) RunsOf(ctx context.Context, periodID uuid.UUID, templateID *uuid.UUID) ([]store.Run, map[uuid.UUID]string, error) {
	a, err := s.knActor(ctx, authz.KNTinh, authz.KNTW, authz.KNAdmin)
	if err != nil {
		return nil, nil, err
	}
	var runs []store.Run
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, _, err := s.periodInScope(ctx, tx, a, periodID, false); err != nil {
			return err
		}
		var err error
		runs, err = store.Runs(ctx, tx, periodID, templateID, 500)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	refs := []uuid.UUID{}
	for _, r := range runs {
		refs = append(refs, r.ActorRef)
	}
	return runs, s.names(ctx, refs), nil
}

// RunChanges trả chi tiết ô thay đổi của một lần chạy (cursor theo id).
func (s *Service) RunChanges(ctx context.Context, runID uuid.UUID, after *uuid.UUID, limit int) (store.Run, []store.CellChange, map[uuid.UUID]string, error) {
	a, err := s.knActor(ctx, authz.KNTinh, authz.KNTW, authz.KNAdmin)
	if err != nil {
		return store.Run{}, nil, nil, err
	}
	var run *store.Run
	var chs []store.CellChange
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var err error
		if run, err = store.RunByID(ctx, tx, runID); err != nil {
			return err
		}
		if run == nil || !a.allowsProvince(run.ProvinceCode, run.DepartmentCode) {
			return ErrRunNotFound
		}
		chs, err = store.CellChanges(ctx, tx, runID, after, limit)
		return err
	})
	if err != nil {
		return store.Run{}, nil, nil, err
	}
	refs := []uuid.UUID{run.ActorRef}
	for _, c := range chs {
		if c.ChangedBy != nil {
			refs = append(refs, *c.ChangedBy)
		}
	}
	return *run, chs, s.names(ctx, refs), nil
}

// ---------------------------------------------------------------------------
// Cổng port.Consolidations cho pf/reports (báo cáo Chi cục)
// ---------------------------------------------------------------------------

// Consolidations hiện thực port.Consolidations (tenant tường minh — worker/pf/reports).
type Consolidations struct{ Svc *Service }

var _ port.Consolidations = Consolidations{}

// Period trả kỳ.
func (c Consolidations) Period(ctx context.Context, tid, periodID uuid.UUID) (port.PeriodInfo, error) {
	var out port.PeriodInfo
	err := c.Svc.tx(ctx, tid, func(tx pgx.Tx) error {
		p, err := store.PeriodByID(ctx, tx, periodID, false)
		if err != nil {
			return err
		}
		if p == nil {
			return port.ErrConsolidationNotFound
		}
		b, err := store.BatchByID(ctx, tx, p.BatchID, false)
		if err != nil {
			return err
		}
		out = port.PeriodInfo{ID: p.ID, BatchID: p.BatchID, Label: p.Label, RangeLabel: p.RangeLabel, SeasonLabel: b.SeasonLabel, From: p.From, To: p.To,
			Closed: p.Status == "closed", ProvinceCode: p.ProvinceCode, DepartmentCode: p.DepartmentCode, ReportType: b.ReportType, Slot: p.Slot}
		ps, err := store.PeriodsOfBatch(ctx, tx, p.BatchID)
		for _, x := range ps {
			out.SlotLabels = append(out.SlotLabels, x.Label)
		}
		return err
	})
	return out, err
}

// LatestRuns trả lần tổng hợp gần nhất của các biểu trong kỳ.
func (c Consolidations) LatestRuns(ctx context.Context, tid, periodID uuid.UUID, formCodes []string) ([]port.ConsolidationView, error) {
	var out []port.ConsolidationView
	err := c.Svc.tx(ctx, tid, func(tx pgx.Tx) error {
		latest, err := store.LatestOfPeriod(ctx, tx, periodID)
		if err != nil {
			return err
		}
		keys := make([]store.Latest, 0, len(latest))
		for _, l := range latest {
			if len(formCodes) == 0 || slices.Contains(formCodes, l.FormCode) {
				keys = append(keys, l)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].FormCode < keys[j].FormCode })
		for _, l := range keys {
			run, err := store.RunByID(ctx, tx, l.RunID)
			if err != nil {
				return err
			}
			snap, err := store.SnapshotOf(ctx, tx, l.RunID)
			if err != nil {
				return err
			}
			var rows []SnapRow
			var totals []SnapTotal
			if err := json.Unmarshal(snap.Rows, &rows); err != nil {
				return err
			}
			if err := json.Unmarshal(snap.Totals, &totals); err != nil {
				return err
			}
			v := port.ConsolidationView{RunID: run.ID, RunNo: run.RunNo, TemplateID: run.TemplateID, VersionID: run.VersionID, FormCode: run.FormCode,
				At: run.EventTime, Trigger: run.Trigger, ActorRef: run.ActorRef, Provisional: run.Provisional, MissingCommunes: run.MissingCommunes}
			for _, m := range run.MissingCommunes {
				name := m
				for _, r := range run.Regions {
					if r.Commune == m {
						name = r.CommuneName
					}
				}
				v.MissingNames = append(v.MissingNames, name)
			}
			for _, r := range rows {
				v.Rows = append(v.Rows, port.ConsolidationRow{Key: r.Key, Commune: r.Commune, CommuneName: r.CommuneName, Region: r.Region,
					RegionName: r.RegionName, Indicator: r.Indicator, Cells: r.Cells})
			}
			for _, t := range totals {
				v.Totals = append(v.Totals, port.ConsolidationTotal{Key: t.Key, Kind: t.Kind, Region: t.Region, RegionName: t.RegionName,
					Indicator: t.Indicator, Members: t.Members, Cells: t.Cells})
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}
