package app

// W11.28 (pf 20261014073846) — BÁO CÁO SỞ NS01–NS06 (BRD HCTH v3.0 FN-12) và DASHBOARD NHÂN SỰ (FN-06). Xem
// domain/reports.go cho bố cục và quy tắc kiểm tra.
//
// Dữ liệu: phép chiếu as-of của hồ sơ (W11.22) tại NGÀY CHỐT + trường của kho định danh đọc THEO LÔ (client.ProfileBatch,
// tối đa 200 chủ thể/lần, một lượt hạn mức; kho cũ → đọc từng chủ thể). Lần xuất ghi sổ report_log (lịch sử xuất + tên
// trường nhạy cảm đã đưa vào tệp) và record_access 'export' (nhật ký của từng hồ sơ) TRƯỚC khi trả tệp, rồi qua điểm
// kiểm soát xuất pf/metering (kênh report_export, nội bộ → observe). Tệp KHÔNG lưu: tải lại = dựng lại tại (ngày chốt,
// thời điểm biết của lần xuất) và so SHA-256 với bản gốc (drift khi định danh ở kho đã đổi).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// ReportsConfig là cấu hình báo cáo Sở của Service (UseEgress, tiêu đề cơ quan).
type ReportsConfig struct {
	Egress     meter.Checkpoint
	EgressMode meter.Mode
	Log        *slog.Logger
	// Agency, Org: tiêu đề cơ quan chủ quản / đơn vị trên biểu (rỗng → mặc định An Giang).
	Agency, Org string
}

// ProductReportPrefix là tiền tố tên sản phẩm của lần xuất báo cáo Sở trong sổ egress_event (pf/metering).
const ProductReportPrefix = "hrm_report_"

// --- Quyền. ---

// reportCaller: biểu báo cáo Sở là biểu TOÀN TRUNG TÂM — cần quyền xuất phạm vi "toàn Trung tâm" (FN-12: Cấp Tỉnh – HCTH,
// Admin) và, với biểu có trường thẻ Thống kê BC Sở, quyền sensitive.view (BR-04).
func (s *Service) reportCaller(ctx context.Context, meta domain.ReportMeta) (rc, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return rc{}, err
	}
	if c.m[domain.PermExport] != domain.ScopeAll {
		return rc{}, problem.Forbidden.WithDetail("báo cáo Sở: cần quyền xuất phạm vi toàn Trung tâm (FN-12, FN-20)")
	}
	if len(meta.Sensitive) > 0 && c.m[domain.PermSensitive] != domain.ScopeAll {
		return rc{}, problem.Forbidden.WithDetail("biểu " + string(meta.Code) + " có trường thẻ Thống kê BC Sở: cần quyền sensitive.view (BR-04)")
	}
	return c, nil
}

func reportMeta(code string) (domain.ReportMeta, error) {
	m, ok := domain.ReportOf(code)
	if !ok {
		return m, port.ErrReportUnknown
	}
	return m, nil
}

// --- Nạp dân số tại ngày chốt (theo thời điểm biết). ---

type population struct {
	heads  []store.StaffRow
	events map[uuid.UUID][]domain.Event
	cat    domain.Catalog
	rank   func(string) int
	ret    domain.RetirementAges
	known  time.Time // thời điểm biết: known đã cho, hoặc đồng hồ DB lúc nạp (khớp recorded_at của sổ)
}

func (s *Service) loadPopulation(ctx context.Context, c rc, asOf time.Time, known *time.Time) (population, error) {
	pop := population{events: map[uuid.UUID][]domain.Event{}, cat: domain.Catalog{Items: map[domain.Kind]map[string]domain.CatItem{}}}
	err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if known != nil {
			pop.known = *known
		} else if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&pop.known); err != nil {
			return err
		}
		known := &pop.known
		var err error
		if pop.heads, err = store.CurrentStaffKnown(ctx, tx, known); err != nil {
			return err
		}
		evs, err := store.Events(ctx, tx, uuid.Nil)
		if err != nil {
			return err
		}
		for _, e := range evs {
			if !e.RecordedAt.After(*known) {
				pop.events[e.StaffID] = append(pop.events[e.StaffID], e.Event)
			}
		}
		items, err := store.CatalogKnown(ctx, tx, known)
		if err != nil {
			return err
		}
		for _, it := range items {
			k := domain.Kind(it.Kind)
			if pop.cat.Items[k] == nil {
				pop.cat.Items[k] = map[string]domain.CatItem{}
			}
			var a domain.Attrs
			_ = jsonUnmarshal(it.Attrs, &a)
			pop.cat.Items[k][it.Code] = domain.CatItem{Name: it.Name, SortOrder: it.SortOrder, Attrs: a}
		}
		r, ok, err := store.RuleAtKnown(ctx, tx, string(domain.RulePromotion), asOf, known)
		if err != nil || !ok {
			return err
		}
		var p domain.PromotionRules
		if jsonUnmarshal(r.Payload, &p) == nil {
			pop.ret = domain.RetirementAges{MaleMonths: p.RetirementAgeMaleMonths, FemaleMonths: p.RetirementAgeFemaleMonths}
			for _, m := range p.RemindMonths {
				pop.ret.RemindMonths = max(pop.ret.RemindMonths, m)
			}
		}
		return nil
	})
	if err != nil {
		return pop, err
	}
	pop.rank = func(level string) int {
		if level == "" {
			return -1
		}
		return pop.cat.Items[domain.KindEducationLevel][level].Attrs.Rank
	}
	if s.Rec.Units != nil && c.unitRef != uuid.Nil {
		us, err := s.Rec.Units.InternalUnits(ctx, c.unitRef)
		if err != nil {
			return pop, err
		}
		for _, u := range us {
			pop.cat.Units = append(pop.cat.Units, domain.UnitRef{Code: u.Code, Name: u.Name})
		}
	}
	return pop, nil
}

// --- Đọc kho theo lô. ---

// readPII đọc kho cho refs theo lô (≤ client.MaxBatchRefs/lần); kho không có phần mở rộng lô → đọc từng chủ thể.
func (s *Service) readPII(ctx context.Context, refs []uuid.UUID, purpose client.Purpose, identity bool, section string) (map[uuid.UUID]client.BatchItem, error) {
	out := make(map[uuid.UUID]client.BatchItem, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	if b, ok := client.AsProfileBatch(s.Rec.Vault); ok {
		for i := 0; i < len(refs); i += client.MaxBatchRefs {
			items, err := b.ReadBatch(ctx, client.BatchRequest{Purpose: purpose, Refs: refs[i:min(i+client.MaxBatchRefs, len(refs))],
				Identity: identity, Section: section})
			if err != nil {
				return nil, err
			}
			for _, it := range items {
				out[it.Ref] = it
			}
		}
		return out, nil
	}
	for _, ref := range refs {
		it := client.BatchItem{Ref: ref, Fields: client.Fields{}}
		if identity {
			id, err := s.Rec.Vault.Resolve(ctx, purpose, ref)
			if errors.Is(err, client.ErrErased) {
				it.Erased = true
				out[ref] = it
				continue
			}
			if err != nil {
				return nil, err
			}
			it.FullName, it.Phone, it.Address = id.FullName, localPhone(id.Phone), id.Address
		}
		if section != "" {
			f, err := s.Rec.Profiles.GetSection(ctx, purpose, ref, section)
			if err != nil && !errors.Is(err, client.ErrErased) {
				return nil, err
			}
			it.Fields = f
		}
		out[ref] = it
	}
	return out, nil
}

func forbiddenErr(err error) bool {
	var p *problem.Problem
	return errors.As(err, &p) && p.Status == http.StatusForbidden
}

// person ghép phép chiếu + trường kho thành domain.Person.
func person(h store.StaffRow, snap domain.Snapshot, evs []domain.Event, at time.Time, prof, sens *client.BatchItem) domain.Person {
	p := domain.Person{StaffID: h.StaffID, Code: h.Code, Type: h.Type, Snap: snap}
	p.Degrees, p.Trainings = domain.CollectQualifications(evs, at)
	if prof != nil && !prof.Erased {
		p.FullName, p.Phone, p.Address = prof.FullName, localPhone(prof.Phone), prof.Address
		p.Gender = prof.Fields["gender"]
		if b, prec, err := domain.ParseDate(prof.Fields["birth_date"]); err == nil {
			p.Birth, p.BirthPrec = &b, prec
		}
	} else if prof != nil {
		p.FullName = client.ErasedLabel
	}
	if sens != nil {
		f := sens.Fields
		p.Sensitive = true
		p.Ethnicity, p.Religion, p.PartyDate, p.Hometown, p.Birthplace, p.RevolutionDate = f["ethnicity"], f["religion"], f["party_date"],
			f["hometown"], f["birthplace"], f["revolution_date"]
	}
	return p
}

// --- Dựng biểu. ---

type builtReport struct {
	meta    domain.ReportMeta
	table   domain.Table
	people  map[uuid.UUID]domain.Person
	heads   map[uuid.UUID]store.StaffRow
	snaps   map[uuid.UUID]domain.Snapshot
	cat     domain.Catalog
	byActor map[uuid.UUID]uuid.UUID // tài khoản → hồ sơ (tên người xuất ở chân trang)
	known   time.Time
}

// buildReport dựng biểu tại ngày chốt asOf theo thời điểm biết known (nil = hiện tại). extra: hồ sơ cần thêm họ tên
// (người lập, người ký, người xuất).
func (s *Service) buildReport(ctx context.Context, c rc, meta domain.ReportMeta, asOf time.Time, known *time.Time, extra []uuid.UUID,
	actor uuid.UUID) (builtReport, error) {
	pop, err := s.loadPopulation(ctx, c, asOf, known)
	if err != nil {
		return builtReport{}, err
	}
	b := builtReport{meta: meta, people: map[uuid.UUID]domain.Person{}, heads: map[uuid.UUID]store.StaffRow{},
		snaps: map[uuid.UUID]domain.Snapshot{}, cat: pop.cat, byActor: map[uuid.UUID]uuid.UUID{}, known: pop.known}
	prevAsOf := domain.PeriodStart(domain.PeriodMonth, asOf).AddDate(0, 0, -1)
	from := domain.PeriodStart(domain.PeriodMonth, asOf)
	in := domain.BuildInput{AsOf: asOf, Period: meta.Period, Cat: pop.cat, PrevAsOf: prevAsOf, Retirement: pop.ret}
	want := map[uuid.UUID]bool{}
	for _, x := range extra {
		want[x] = true
	}
	var refs []uuid.UUID
	var cand []store.StaffRow
	for _, h := range pop.heads {
		b.heads[h.StaffID] = h
		if h.AccountRef != nil {
			b.byActor[*h.AccountRef] = h.StaffID
			if *h.AccountRef == actor {
				want[h.StaffID] = true
			}
		}
		snap := domain.Project(pop.events[h.StaffID], asOf, pop.rank)
		b.snaps[h.StaffID] = snap
		if h.Type == domain.StaffVienChuc {
			if domain.Project(pop.events[h.StaffID], prevAsOf, nil).Status == domain.StatusActive {
				in.Prev = append(in.Prev, h.StaffID)
			}
			if snap.StartedOn != nil && snap.StartedOn.After(prevAsOf) {
				in.Joined = append(in.Joined, h.StaffID)
			}
			if snap.Status == domain.StatusLeft && snap.EndedOn != nil && snap.EndedOn.After(prevAsOf) {
				in.LeftIDs = append(in.LeftIDs, h.StaffID)
			}
		}
		relevant := h.Type != domain.StaffCanBoXa && (snap.Status == domain.StatusActive ||
			snap.Status == domain.StatusLeft && snap.EndedOn != nil && !snap.EndedOn.Before(from))
		if relevant {
			cand = append(cand, h)
		}
		if relevant || want[h.StaffID] {
			refs = append(refs, h.SubjectRef)
		}
	}
	prof, err := s.readPII(ctx, refs, client.PurposeHRMProfile, true, client.SectionHRMProfile)
	if err != nil {
		return builtReport{}, err
	}
	var sens map[uuid.UUID]client.BatchItem
	if len(meta.Sensitive) > 0 {
		var sr []uuid.UUID
		for _, h := range cand {
			sr = append(sr, h.SubjectRef)
		}
		if sens, err = s.readPII(ctx, sr, client.PurposeHRMSensitive, false, client.SectionHRMSensitive); err != nil {
			return builtReport{}, err
		}
	}
	for _, h := range pop.heads {
		pr, okP := prof[h.SubjectRef]
		if !okP && !want[h.StaffID] {
			continue
		}
		var sp *client.BatchItem
		if x, ok := sens[h.SubjectRef]; ok {
			sp = &x
		}
		p := person(h, b.snaps[h.StaffID], pop.events[h.StaffID], asOf, &pr, sp)
		b.people[h.StaffID] = p
		if h.Type != domain.StaffCanBoXa {
			in.People = append(in.People, p)
		}
	}
	b.table, err = domain.Build(meta.Code, in)
	return b, err
}

// PersonBrief là một người trong danh sách (kiểm tra, nhấn ô, dashboard) — họ tên chỉ khi đọc được.
type PersonBrief struct {
	ID       uuid.UUID
	Code     string
	Type     domain.StaffType
	Name     string
	Unit     string
	UnitName string
	Position string
}

func (b builtReport) brief(id uuid.UUID) PersonBrief {
	p := b.people[id]
	h := b.heads[id]
	sn := b.snaps[id]
	return PersonBrief{ID: id, Code: h.Code, Type: h.Type, Name: p.FullName, Unit: sn.Unit, UnitName: b.cat.UnitName(sn.Unit),
		Position: b.cat.Name(domain.KindPosition, sn.Position)}
}

// CheckOut là kết quả quy tắc kèm danh sách người liên quan.
type CheckOut struct {
	domain.Check
	People []PersonBrief
}

// ReportView là biểu đã dựng để xem trước (FN-12 B3).
type ReportView struct {
	Meta   domain.ReportMeta
	Table  domain.Table
	Status string
	Checks []CheckOut
}

func (b builtReport) view() ReportView {
	v := ReportView{Meta: b.meta, Table: b.table, Status: b.table.Status()}
	for _, c := range b.table.Checks {
		co := CheckOut{Check: c}
		for _, id := range c.StaffIDs {
			co.People = append(co.People, b.brief(id))
		}
		v.Checks = append(v.Checks, co)
	}
	return v
}

func checkSummaries(t domain.Table) []store.CheckSummary {
	out := make([]store.CheckSummary, 0, len(t.Checks))
	for _, c := range t.Checks {
		out = append(out, store.CheckSummary{Rule: c.Rule, Level: c.Level, OK: c.OK, Count: len(c.StaffIDs)})
	}
	return out
}

// logReport ghi sổ report_log (+ record_access 'export' cho tệp) trong một giao dịch.
func (s *Service) logReport(ctx context.Context, c rc, l store.ReportLog, idem string, fileAccess bool) error {
	l.Actor = c.ref
	return s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if _, err := store.InsertReportLog(ctx, tx, c.tenant, l, s.Clock.Now(), stableKey(idem, "report:"+l.Action)); err != nil {
			return err
		}
		if !fileAccess || len(l.StaffIDs) == 0 {
			return nil
		}
		return store.InsertAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "export", StaffIDs: l.StaffIDs, ItemCount: len(l.StaffIDs),
			Scope: string(domain.ScopeAll), Actor: c.ref, Query: map[string]string{"report": l.Report, "as_of": l.AsOf.Format(time.DateOnly),
				"report_action": l.Action, "sensitive": strings.Join(l.Sensitive, ",")}}, s.Clock.Now())
	})
}

func (s *Service) reportEgress(ctx context.Context, c rc, code domain.ReportCode, records, size int) {
	cfg := s.Rep
	if cfg.Egress == nil {
		return
	}
	_ = meter.Guard(ctx, cfg.Egress, cfg.EgressMode, meter.Egress{SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: c.tenant,
		CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: c.ref, Product: ProductReportPrefix + strings.ToLower(string(code)),
		Records: int64(records), Bytes: int64(size), OccurredAt: s.Clock.Now(), ActorRef: c.ref, TraceID: telemetry.TraceID(ctx), Internal: true}, cfg.Log)
}

// --- API của app. ---

// ReportSummary là một biểu trong danh sách (FN-12 B1): mô tả + lần xuất gần nhất.
type ReportSummary struct {
	Meta    domain.ReportMeta
	Last    *store.ReportLog
	Exports int
}

// Reports trả sáu biểu kèm lần xuất gần nhất (không đọc kho — trạng thái kiểm tra hiện hành chạy khi xem trước).
func (s *Service) Reports(ctx context.Context) ([]ReportSummary, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return nil, err
	}
	if c.m[domain.PermExport] != domain.ScopeAll {
		return nil, problem.Forbidden.WithDetail("báo cáo Sở: cần quyền xuất phạm vi toàn Trung tâm (FN-12, FN-20)")
	}
	var logs []store.ReportLog
	if err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		logs, e = store.ReportLogs(ctx, tx, "", []string{"export"}, 1000)
		return e
	}); err != nil {
		return nil, err
	}
	out := make([]ReportSummary, 0, 6)
	for _, m := range domain.Reports() {
		r := ReportSummary{Meta: m}
		for i := range logs {
			if logs[i].Report == string(m.Code) {
				if r.Last == nil {
					r.Last = &logs[i]
				}
				r.Exports++
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// PreviewReport dựng biểu tại ngày chốt (zero → hôm nay), chạy quy tắc kiểm tra; GHI SỔ 'preview' (họ tên, trường nhạy
// cảm đã hiển thị) trước khi trả.
func (s *Service) PreviewReport(ctx context.Context, code string, asOf time.Time) (ReportView, error) {
	meta, err := reportMeta(code)
	if err != nil {
		return ReportView{}, err
	}
	c, err := s.reportCaller(ctx, meta)
	if err != nil {
		return ReportView{}, err
	}
	day := s.day(asOf)
	b, err := s.buildReport(ctx, c, meta, day, nil, nil, c.ref)
	if err != nil {
		return ReportView{}, err
	}
	if err := s.logReport(ctx, c, store.ReportLog{ID: ids.New(), Action: "preview", Report: code, AsOf: day, Period: b.table.Period,
		StaffIDs: b.table.People, Sensitive: meta.Sensitive, ItemCount: len(b.table.People), Checks: checkSummaries(b.table)}, "", false); err != nil {
		return ReportView{}, err
	}
	return b.view(), nil
}

// ExportInput là tham số xuất (FN-12 B2): ngày chốt, người lập, người ký (mã hồ sơ — tên đọc từ kho khi dựng tệp).
type ExportInput struct {
	AsOf     time.Time
	Preparer *uuid.UUID
	Signer   *uuid.UUID
}

// ExportResult là kết quả một lần xuất (không chứa tệp — tải bằng DownloadExport).
type ExportResult struct {
	ID       uuid.UUID
	FileName string
	SHA256   string
	Size     int
	View     ReportView
}

func exportParams(in ExportInput) map[string]string {
	p := map[string]string{}
	if in.Preparer != nil {
		p["preparer"] = in.Preparer.String()
	}
	if in.Signer != nil {
		p["signer"] = in.Signer.String()
	}
	return p
}

func paramsInput(p map[string]string) ExportInput {
	var in ExportInput
	if u, err := uuid.Parse(p["preparer"]); err == nil {
		in.Preparer = &u
	}
	if u, err := uuid.Parse(p["signer"]); err == nil {
		in.Signer = &u
	}
	return in
}

func (in ExportInput) extra() []uuid.UUID {
	var out []uuid.UUID
	for _, x := range []*uuid.UUID{in.Preparer, in.Signer} {
		if x != nil {
			out = append(out, *x)
		}
	}
	return out
}

// render dựng tệp Excel của biểu (chân trang: người xuất, thời điểm — BR-03).
func (s *Service) render(b builtReport, in ExportInput, actor uuid.UUID, at time.Time) ([]byte, string, error) {
	name := func(id *uuid.UUID) string {
		if id == nil {
			return ""
		}
		return b.people[*id].FullName
	}
	exporter := "tài khoản " + actor.String()[:8]
	if sid, ok := b.byActor[actor]; ok && b.people[sid].FullName != "" {
		exporter = b.people[sid].FullName
	}
	data, err := renderXLSX(b.table, xlsxMeta{Agency: s.Rep.Agency, Org: s.Rep.Org, Preparer: name(in.Preparer), Signer: name(in.Signer),
		Exporter: exporter, ExportedAt: at})
	file := fmt.Sprintf("%s_%s.xlsx", b.table.Code, b.table.AsOf.Format("2006-01-02"))
	return data, file, err
}

// ExportReport chạy quy tắc kiểm tra; lỗi mức CHẶN → hrm.report_blocked (FN-12 luồng ngoại lệ). Đạt: dựng tệp, ghi sổ
// 'export' (tham số, mã hồ sơ, tên trường nhạy cảm, SHA-256, thời điểm biết) + record_access, qua điểm kiểm soát xuất.
func (s *Service) ExportReport(ctx context.Context, code string, in ExportInput, idem string) (ExportResult, error) {
	meta, err := reportMeta(code)
	if err != nil {
		return ExportResult{}, err
	}
	c, err := s.reportCaller(ctx, meta)
	if err != nil {
		return ExportResult{}, err
	}
	day := s.day(in.AsOf)
	if day.After(s.day(time.Time{})) {
		return ExportResult{}, problem.Validation.WithField("as_of", "ngày chốt sau hôm nay")
	}
	b, err := s.buildReport(ctx, c, meta, day, nil, in.extra(), c.ref)
	if err != nil {
		return ExportResult{}, err
	}
	for _, x := range in.extra() {
		if _, ok := b.heads[x]; !ok {
			return ExportResult{}, problem.Validation.WithField("preparer", "người lập/người ký phải là mã hồ sơ nhân sự")
		}
	}
	v := b.view()
	if b.table.Blocked() {
		return ExportResult{View: v}, port.ErrReportBlocked.WithDetail(blockedDetail(b.table))
	}
	known := b.known.UTC()
	data, file, err := s.render(b, in, c.ref, known)
	if err != nil {
		return ExportResult{}, err
	}
	sum := sha256.Sum256(data)
	res := ExportResult{ID: ids.New(), FileName: file, SHA256: hex.EncodeToString(sum[:]), Size: len(data), View: v}
	if err := s.logReport(ctx, c, store.ReportLog{ID: res.ID, Action: "export", Report: code, AsOf: day, Period: b.table.Period, KnownAt: &known,
		Params: exportParams(in), StaffIDs: b.table.People, Sensitive: meta.Sensitive, ItemCount: len(b.table.People), FileName: file,
		SHA256: res.SHA256, ByteSize: int64(len(data)), CheckStatus: b.table.Status(), Checks: checkSummaries(b.table)}, idem, true); err != nil {
		return ExportResult{}, err
	}
	return res, nil
}

func blockedDetail(t domain.Table) string {
	var msg []string
	for _, c := range t.Checks {
		if !c.OK && c.Level == domain.LevelBlock {
			msg = append(msg, c.Rule+": "+c.Message)
		}
	}
	return strings.Join(msg, " · ")
}

// ExportFile là tệp tải lại.
type ExportFile struct {
	Data     []byte
	FileName string
	Drift    bool // nội dung khác bản gốc (định danh ở kho đã sửa/xóa sau lần xuất)
	Log      store.ReportLog
}

// DownloadExport tải lại một lần xuất (FN-12 luồng phụ, AC-04 FN-09 tương tự): dựng lại tại (ngày chốt, thời điểm biết),
// so SHA-256 với bản gốc; ghi sổ 'download' + record_access; qua điểm kiểm soát xuất.
func (s *Service) DownloadExport(ctx context.Context, id uuid.UUID) (ExportFile, error) {
	c0, err := s.recCaller(ctx)
	if err != nil {
		return ExportFile{}, err
	}
	var l store.ReportLog
	if err := s.tx(ctx, c0.tenant, func(tx pgx.Tx) error {
		x, ok, e := store.ReportLogByID(ctx, tx, id)
		if e == nil && (!ok || x.Action != "export") {
			e = port.ErrExportNotFound
		}
		l = x
		return e
	}); err != nil {
		return ExportFile{}, err
	}
	meta, err := reportMeta(l.Report)
	if err != nil {
		return ExportFile{}, err
	}
	c, err := s.reportCaller(ctx, meta)
	if err != nil {
		return ExportFile{}, err
	}
	in := paramsInput(l.Params)
	in.AsOf = l.AsOf
	b, err := s.buildReport(ctx, c, meta, l.AsOf, l.KnownAt, in.extra(), l.Actor)
	if err != nil {
		return ExportFile{}, err
	}
	data, file, err := s.render(b, in, l.Actor, *l.KnownAt)
	if err != nil {
		return ExportFile{}, err
	}
	sum := sha256.Sum256(data)
	drift := hex.EncodeToString(sum[:]) != l.SHA256
	if err := s.logReport(ctx, c, store.ReportLog{ID: ids.New(), Action: "download", Report: l.Report, AsOf: l.AsOf, Period: l.Period, ExportID: &id,
		StaffIDs: b.table.People, Sensitive: meta.Sensitive, ItemCount: len(b.table.People), FileName: file, SHA256: hex.EncodeToString(sum[:]),
		ByteSize: int64(len(data)), Drift: &drift}, "", true); err != nil {
		return ExportFile{}, err
	}
	s.reportEgress(ctx, c, meta.Code, len(b.table.People), len(data))
	return ExportFile{Data: data, FileName: file, Drift: drift, Log: l}, nil
}

// ReportHistory trả lịch sử xuất/tải của biểu (không PII).
func (s *Service) ReportHistory(ctx context.Context, code string, limit int) ([]store.ReportLog, error) {
	meta, err := reportMeta(code)
	if err != nil {
		return nil, err
	}
	c, err := s.reportCaller(ctx, meta)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.ReportLog
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		out, e = store.ReportLogs(ctx, tx, code, []string{"export", "download"}, limit)
		return e
	})
	return out, err
}

// CellPeople trả người được đếm ở một ô của biểu thống kê (NS05, NS06 — FN-12 luồng phụ), lọc theo phạm vi xem của ma
// trận FN-20 (người ngoài phạm vi chỉ được đếm ở Hidden); ghi sổ 'drill'.
func (s *Service) CellPeople(ctx context.Context, code string, asOf time.Time, row, col string) ([]PersonBrief, int, error) {
	meta, err := reportMeta(code)
	if err != nil {
		return nil, 0, err
	}
	if !meta.Aggregate {
		return nil, 0, problem.Validation.WithField("code", "chỉ biểu thống kê NS05, NS06 có ô số")
	}
	c, err := s.reportCaller(ctx, meta)
	if err != nil {
		return nil, 0, err
	}
	if c.m[domain.PermView] == domain.ScopeNone {
		return nil, 0, problem.Forbidden.WithDetail("xem người được đếm cần quyền profile.view (FN-20)")
	}
	day := s.day(asOf)
	b, err := s.buildReport(ctx, c, meta, day, nil, nil, c.ref)
	if err != nil {
		return nil, 0, err
	}
	found := false
	for _, col0 := range b.table.Columns {
		found = found || col0.Key == col && col0.Numeric
	}
	if !found {
		return nil, 0, problem.Validation.WithField("col", "không phải cột số của biểu")
	}
	var out []PersonBrief
	hidden := 0
	var shown []uuid.UUID
	for _, id := range b.table.Cells[domain.CellKey(row, col)] {
		if !c.on(domain.PermView, b.heads[id], b.snaps[id]) {
			hidden++
			continue
		}
		out = append(out, b.brief(id))
		shown = append(shown, id)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	sens := []string{}
	for _, c0 := range b.table.Columns {
		if c0.Key == col && c0.Sensitive {
			sens = meta.Sensitive
		}
	}
	if err := s.logReport(ctx, c, store.ReportLog{ID: ids.New(), Action: "drill", Report: code, AsOf: day, Period: b.table.Period,
		Params: map[string]string{"row": row, "col": col}, StaffIDs: shown, Sensitive: sens, ItemCount: len(shown)}, "", false); err != nil {
		return nil, 0, err
	}
	return out, hidden, nil
}

// --- FN-06 Dashboard nhân sự. ---

// DashboardView là dashboard trong phạm vi người xem.
type DashboardView struct {
	domain.Dashboard
	Scope   domain.Scope
	Elapsed time.Duration
}

type dashData struct {
	d     domain.Dashboard
	b     builtReport
	scope domain.Scope
}

func (s *Service) dashboard(ctx context.Context, at time.Time) (dashData, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return dashData{}, err
	}
	if err := c.need(domain.PermView, false); err != nil {
		return dashData{}, err
	}
	day := s.day(at)
	pop, err := s.loadPopulation(ctx, c, day, nil)
	if err != nil {
		return dashData{}, err
	}
	b := builtReport{people: map[uuid.UUID]domain.Person{}, heads: map[uuid.UUID]store.StaffRow{}, snaps: map[uuid.UUID]domain.Snapshot{},
		cat: pop.cat}
	var refs []uuid.UUID
	var scope []store.StaffRow
	for _, h := range pop.heads {
		snap := domain.Project(pop.events[h.StaffID], day, pop.rank)
		if !c.on(domain.PermView, h, snap) {
			continue
		}
		b.heads[h.StaffID], b.snaps[h.StaffID] = h, snap
		scope = append(scope, h)
		if snap.Status == domain.StatusActive && h.Type != domain.StaffCanBoXa {
			refs = append(refs, h.SubjectRef)
		}
	}
	// Chỉ ngăn hrm.profile (giới tính, ngày sinh) — không đọc định danh, không thẻ nhạy cảm. Người xem không có vai kho →
	// dashboard vẫn chạy, giới tính/độ tuổi "chưa rõ".
	pii := true
	prof, err := s.readPII(ctx, refs, client.PurposeHRMProfile, false, client.SectionHRMProfile)
	if forbiddenErr(err) {
		pii, prof, err = false, nil, nil
	}
	if err != nil {
		return dashData{}, err
	}
	var ps []domain.Person
	for _, h := range scope {
		var pp *client.BatchItem
		if x, ok := prof[h.SubjectRef]; ok {
			pp = &x
		}
		p := person(h, b.snaps[h.StaffID], pop.events[h.StaffID], day, pp, nil)
		b.people[h.StaffID] = p
		ps = append(ps, p)
	}
	return dashData{d: domain.BuildDashboard(ps, pop.cat, day, pop.ret, pii), b: b, scope: c.m[domain.PermView]}, nil
}

// Dashboard trả số liệu tổng quan tại ngày chốt (FN-06: HCTH/Admin toàn Trung tâm, lãnh đạo phòng — phòng mình theo
// phạm vi profile.view của ma trận FN-20). Không ghi nhật ký (không có họ tên).
func (s *Service) Dashboard(ctx context.Context, at time.Time) (DashboardView, error) {
	start := time.Now()
	d, err := s.dashboard(ctx, at)
	if err != nil {
		return DashboardView{}, err
	}
	return DashboardView{Dashboard: d.d, Scope: d.scope, Elapsed: time.Since(start)}, nil
}

// DashboardMembers trả danh sách người của một chỉ số (FN-06 B3) — họ tên đọc theo lô cho trang được hỏi (kho ghi sổ).
func (s *Service) DashboardMembers(ctx context.Context, at time.Time, metric string, offset, limit int) ([]PersonBrief, int, error) {
	d, err := s.dashboard(ctx, at)
	if err != nil {
		return nil, 0, err
	}
	members, ok := d.d.Members[metric]
	if !ok {
		if !strings.Contains(metric, ":") && !map[string]bool{"total": true, "staff": true, "joined": true, "left": true, "retire_soon": true,
			"over_retire": true}[metric] {
			return nil, 0, problem.Validation.WithField("metric", "chỉ số không có")
		}
		return []PersonBrief{}, 0, nil
	}
	sort.SliceStable(members, func(i, j int) bool { return d.b.heads[members[i]].Code < d.b.heads[members[j]].Code })
	if limit <= 0 || limit > client.MaxBatchRefs {
		limit = 50
	}
	if offset < 0 || offset >= len(members) {
		return []PersonBrief{}, len(members), nil
	}
	page := members[offset:min(offset+limit, len(members))]
	refs := make([]uuid.UUID, 0, len(page))
	for _, id := range page {
		refs = append(refs, d.b.heads[id].SubjectRef)
	}
	names, err := s.readPII(ctx, refs, client.PurposeHRMProfile, true, "")
	if forbiddenErr(err) {
		names, err = nil, nil
	}
	if err != nil {
		return nil, 0, err
	}
	out := make([]PersonBrief, 0, len(page))
	for _, id := range page {
		br := d.b.brief(id)
		br.Name = names[d.b.heads[id].SubjectRef].FullName
		if names[d.b.heads[id].SubjectRef].Erased {
			br.Name = client.ErasedLabel
		}
		out = append(out, br)
	}
	return out, len(members), nil
}
