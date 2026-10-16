// Package app: use case bộ đọc nguồn module của Kỳ KHTC (W11.64 — BRD KHTC v2.2 BR-DL-02…04, BR-DL-09, BR-DL-17; D4 KH4;
// ADR-0053 §8). Ba việc trên màn kỳ KHTC:
//
//   - "Cập nhật số liệu" (Refresh): chỉ cán bộ KHTC PHỤ TRÁCH kỳ (mặc định ⚑3/Q11: người mở kỳ) — chụp BẤT BIẾN kết quả
//     port KHTCSources của kỳ (mỗi kỳ nguồn một dòng, kèm nguồn gốc và băm), tính số của các hạng mục Kế hoạch năm có
//     nguồn module (lũy kế của nguồn — D18; giá min/max toàn tỉnh — D22), ghi lịch sử cũ → mới so với ảnh chụp trước.
//   - "Mốc dữ liệu" và "Nguồn đã thay đổi" (Status): mốc từng nguồn của ảnh chụp; so figures_hash hiện tại của port với
//     ảnh chụp — chỉ cảnh báo, không ghi đè; đổi status_hash (nhận HTX — D2) không cảnh báo.
//   - Lịch sử (History): các lần cập nhật, người, thời điểm, giá trị cũ → mới.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	pstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcsource/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors/pf-extension-khtc-source.md). Không tìm thấy / đã đóng / phụ thuộc dùng chung mã của Kỳ KHTC.
var (
	ErrNotFound   = problem.New(http.StatusNotFound, "extension.khtc_period_not_found", "Không tìm thấy kỳ KHTC")
	ErrTerminal   = problem.New(http.StatusConflict, "extension.khtc_period_terminal", "Kỳ đã đóng hoặc đã hủy")
	ErrDependency = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	// ErrNotOwner: chỉ cán bộ KHTC phụ trách kỳ được "Cập nhật số liệu" (⚑3, KH-QD-08).
	ErrNotOwner = problem.New(http.StatusForbidden, "extension.khtc_refresh_not_owner",
		"Chỉ cán bộ KHTC phụ trách kỳ được \"Cập nhật số liệu\"")
	errNotKHTC       = problem.Forbidden.WithDetail("Kỳ KHTC chỉ dành cho Phòng Kế hoạch – Tài chính (Cấp Tỉnh) và Admin xem")
	errNotKHTCWriter = problem.Forbidden.WithDetail("chỉ Cấp Tỉnh Phòng Kế hoạch – Tài chính \"Cập nhật số liệu\"")
)

// Service là use case bộ đọc nguồn module.
type Service struct {
	Finals port.KHTCReportStates // W11.66: final reports freeze refreshes
	DB     tenant.Beginner       // pool của pf/extension (r_extension)
	Clock  clock.Clock
	// Sources: port KHTCSources (ADR-0053 §7) — thiếu → 503.
	Sources port.KHTCSources
	// Plans: Kế hoạch năm (W11.61) — tùy chọn; thiếu → ảnh chụp chỉ có dòng nguồn, không có số hạng mục.
	Plans port.KHTCPlans
	// Names: tên người bấm cho lịch sử (tùy chọn).
	Names func() partyport.ActorNames
}

type caller struct {
	p authz.Principal
	k authz.KNContext
}

func (s *Service) reader(ctx context.Context) (caller, error) {
	if s.DB == nil || s.Sources == nil {
		return caller{}, ErrDependency.WithDetail("chưa nối port KHTCSources")
	}
	p, k, err := authz.RequireKN(ctx)
	if err != nil {
		return caller{}, err
	}
	if k.Role == authz.KNAdmin || (k.Department == partyport.DeptKeHoachTaiChinh && (k.Role == authz.KNTinh || k.Role == authz.KNTW)) {
		return caller{p: p, k: k}, nil
	}
	return caller{}, errNotKHTC
}

// canRefresh: Cấp Tỉnh Phòng KHTC, không chỉ xem, là người phụ trách kỳ (người mở kỳ — ⚑3 mặc định).
func canRefresh(c caller, p pstore.Period) error {
	if c.k.Role != authz.KNTinh || c.k.Department != partyport.DeptKeHoachTaiChinh || c.k.ReadOnly() {
		return errNotKHTCWriter
	}
	if c.p.ActorRef != p.ActorRef {
		return ErrNotOwner
	}
	return nil
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

// Owner trả người phụ trách kỳ: ⚑3 mặc định = người mở kỳ (actor_ref của sổ khtc_period).
func Owner(p pstore.Period) uuid.UUID { return p.ActorRef }

func terminalOf(evs []pstore.Event) string {
	for _, e := range evs {
		if e.Action == pstore.ActionClosed || e.Action == pstore.ActionCancelled {
			return e.Action
		}
	}
	return ""
}

func (s *Service) loadPeriod(ctx context.Context, tx pgx.Tx, id uuid.UUID) (pstore.Period, string, error) {
	p, err := pstore.PeriodByID(ctx, tx, id)
	if err != nil {
		return pstore.Period{}, "", err
	}
	if p == nil {
		return pstore.Period{}, "", ErrNotFound
	}
	evs, err := pstore.Events(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return pstore.Period{}, "", err
	}
	return *p, terminalOf(evs[id]), nil
}

// calendarOf: lịch KHTC của kỳ = thẻ quy tắc đã lưu lúc mở kỳ (W11.62), lịch (ke_hoach_tai_chinh, phiên bản D8).
func calendarOf(p pstore.Period) rc.Calendar {
	c, err := rc.CalendarOf(partyport.DeptKeHoachTaiChinh, p.Rule)
	if err != nil || c.Rule.Version == rc.RuleVersionV1 {
		c, _ = rc.CalendarOf(partyport.DeptKeHoachTaiChinh, rc.KHTCRule.Tag())
	}
	return c
}

// windowFrom: tuần đầu đọc = tuần trước tuần 1 của năm KHTC chứa cuối kỳ (gốc của tuần 1, lũy kế năm), sớm hơn nếu kỳ
// (Mẫu 5, phát sinh) bắt đầu trước đó.
func windowFrom(r rc.PeriodRule, from, to time.Time) (time.Time, error) {
	w1, err := r.PeriodOf(rc.PeriodWeek, rc.Date(to.Year(), 1, 1), nil)
	if err != nil {
		return time.Time{}, err
	}
	start := w1.From.AddDate(0, 0, -1)
	if f := from.AddDate(0, 0, -1); f.Before(start) {
		start = f
	}
	w0, err := r.PeriodOf(rc.PeriodWeek, start, nil)
	if err != nil {
		return time.Time{}, err
	}
	return w0.From, nil
}

// planKeys trả khóa nguồn module của Kế hoạch năm hiệu lực tại ngày chốt của kỳ → hạng mục dùng khóa (BR-DL-10).
func (s *Service) planKeys(ctx context.Context, p pstore.Period) (*uuid.UUID, map[string][]uuid.UUID, map[uuid.UUID]port.KHTCPlanEntry, error) {
	keys := map[string][]uuid.UUID{}
	entries := map[uuid.UUID]port.KHTCPlanEntry{}
	if s.Plans == nil {
		return nil, keys, entries, nil
	}
	plan, err := s.Plans.PlanAt(ctx, p.Year, p.Cutoff) // tenant của ctx (middleware xác thực)
	if errors.Is(err, port.ErrKHTCPlanNotFound) {
		return nil, keys, entries, nil
	}
	if err != nil {
		return nil, nil, nil, ErrDependency.WithDetail("Kế hoạch năm lỗi").WithCause(err)
	}
	for _, e := range plan.Entries {
		entries[e.Ref] = e
		if e.DataSource != "module" || e.SourceKey == "" {
			continue
		}
		if e.Tracking == "stopped" && e.StoppedFrom != nil && !e.StoppedFrom.After(p.From) {
			continue // ngừng theo dõi trước kỳ
		}
		keys[e.SourceKey] = append(keys[e.SourceKey], e.Ref)
	}
	vid := plan.VersionID
	return &vid, keys, entries, nil
}

func hashRows(rows []domain.Row, status bool) string {
	ids := make([]string, 0, len(rows))
	byID := map[string]domain.Row{}
	for _, r := range rows {
		ids = append(ids, r.Identity())
		byID[r.Identity()] = r
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		r := byID[id]
		v := r.FiguresHash
		if status {
			v = r.StatusHash
		}
		x := "-"
		if v != nil {
			x = *v
		}
		_, _ = fmt.Fprintf(h, "%s=%s;", id, x)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Refresh "Cập nhật số liệu" (B5 của FN-TI-02.05): ảnh chụp mới của kỳ. Gọi lặp cùng Idempotency-Key → ảnh chụp đã ghi.
func (s *Service) Refresh(ctx context.Context, id uuid.UUID, idem string) (Status, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Status{}, err
	}
	if idem == "" {
		return Status{}, problem.IdempotencyRequired
	}
	key := "khtc:refresh:" + idem
	var (
		p        pstore.Period
		term     string
		done     *store.Snapshot
		prev     *store.Snapshot
		prevVals []store.Value
	)
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if p, term, e = s.loadPeriod(ctx, tx, id); e != nil {
			return e
		}
		if done, e = store.SnapshotByIdem(ctx, tx, key); e != nil || done != nil {
			return e
		}
		if prev, e = store.LatestSnapshot(ctx, tx, id); e != nil || prev == nil {
			return e
		}
		prevVals, e = store.Values(ctx, tx, prev.ID)
		return e
	})
	if err != nil {
		return Status{}, err
	}
	if done != nil {
		if done.PeriodID != id {
			return Status{}, problem.Conflict.WithDetail("Idempotency-Key đã dùng cho kỳ khác")
		}
		return s.Status(ctx, id)
	}
	if err := canRefresh(c, p); err != nil {
		return Status{}, err
	}
	if s.Finals != nil {
		final, e := s.Finals.HasFinal(ctx, id.String())
		if e != nil {
			return Status{}, ErrDependency.WithCause(e)
		}
		if final {
			return Status{}, ErrTerminal.WithDetail("Báo cáo đã có Bản cuối")
		}
	}
	if term != "" {
		return Status{}, ErrTerminal.WithDetail("kỳ đã đóng giữ số đã chốt — phần chênh vào kỳ mở kế tiếp")
	}
	cal := calendarOf(p)
	wFrom, err := windowFrom(cal.Rule, p.From, p.To)
	if err != nil {
		return Status{}, err
	}
	target := port.CrosswalkTarget{CalendarID: cal.ID, RuleTag: cal.Rule.Tag(), PeriodType: rc.PeriodWeek}
	ps, err := s.Sources.Periods(ctx, c.p.TenantID, port.KHTCQuery{ConfirmationAt: cutoffEnd(p.Cutoff), From: wFrom, To: p.To, Target: target})
	if err != nil {
		return Status{}, ErrDependency.WithDetail("đọc số liệu nguồn lỗi").WithCause(err)
	}
	planVer, keys, _, err := s.planKeys(ctx, p)
	if err != nil {
		return Status{}, err
	}
	for _, key := range domain.ReportKeys(ps) {
		if _, exists := keys[key]; !exists {
			keys[key] = []uuid.UUID{}
		}
	}
	var weekKeys []string
	for _, x := range ps {
		weekKeys = append(weekKeys, x.TargetKey)
	}
	var frozen map[string]map[string]domain.Frozen
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		frozen, e = store.FrozenWeeks(ctx, tx, weekKeys)
		return e
	}); err != nil {
		return Status{}, err
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	vals := make([]store.Value, 0, len(names))
	cur := map[string]domain.Value{}
	for _, raw := range names {
		v := store.Value{SourceKey: raw, EntryRefs: keys[raw]}
		k, err := domain.ParseSourceKey(raw)
		if err != nil {
			v.Invalid, v.V = true, domain.Value{SourceKey: raw}
			vals = append(vals, v)
			continue
		}
		fz := map[string]domain.Frozen{}
		for w, m := range frozen {
			if f, ok := m[raw]; ok {
				fz[w] = f
			}
		}
		v.V = domain.Evaluate(k, ps, fz, p.From, p.To)
		cur[raw] = v.V
		vals = append(vals, v)
	}
	var changes []store.Change
	if prev != nil {
		pv := map[string]domain.Value{}
		refs := map[string][]uuid.UUID{}
		for _, x := range prevVals {
			if !x.Invalid {
				pv[x.SourceKey] = x.V
			}
			refs[x.SourceKey] = x.EntryRefs
		}
		for k, r := range keys {
			refs[k] = r
		}
		for _, ch := range domain.Diff(pv, cur) {
			changes = append(changes, store.Change{PrevSnapshotID: prev.ID, SourceKey: ch.SourceKey, EntryRefs: refs[ch.SourceKey], Field: ch.Field,
				Old: ch.Old, New: ch.New})
		}
	}
	rows := domain.Rows(ps, p.From, p.To)
	closed := true
	for _, x := range ps {
		if !dayOf(x.TargetTo).Before(dayOf(p.From)) && !dayOf(x.TargetTo).After(dayOf(p.To)) {
			closed = closed && x.Closed
		}
	}
	snap := store.Snapshot{ID: ids.New(), PeriodID: id, PeriodFrom: p.From, PeriodTo: p.To, WindowFrom: wFrom, TargetCalendarID: cal.ID,
		TargetCalendarVersion: cal.Version, TargetRule: cal.Rule.Tag(), CrosswalkVersion: port.CrosswalkV1, PlanVersionID: planVer, Closed: closed,
		FiguresHash: hashRows(rows, false), StatusHash: hashRows(rows, true), RowCount: len(rows), ValueCount: len(vals), ChangeCount: len(changes),
		ActorRef: c.p.ActorRef, EventTime: s.Clock.Now().UTC()}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.InsertSnapshot(ctx, tx, c.p.TenantID, &snap, key); err != nil {
			return err
		}
		if err := store.InsertRows(ctx, tx, c.p.TenantID, &snap, rows, ids.New, key); err != nil {
			return err
		}
		if err := store.InsertValues(ctx, tx, c.p.TenantID, &snap, vals, ids.New, key); err != nil {
			return err
		}
		return store.InsertChanges(ctx, tx, c.p.TenantID, &snap, changes, ids.New, key)
	})
	if err != nil {
		return Status{}, classify(err)
	}
	return s.Status(ctx, id)
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func classify(err error) error {
	var finalError *pgconn.PgError
	if errors.As(err, &finalError) && finalError.ConstraintName == "khtc_report_already_final" {
		return ErrTerminal.WithDetail("Báo cáo đã có Bản cuối")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "khtc_snapshot_terminal":
		return ErrTerminal.WithCause(err)
	case "khtc_snapshot_period_fkey":
		return ErrNotFound.WithCause(err)
	}
	if pg.Code == "23505" {
		return problem.Conflict.WithCause(err)
	}
	return err
}

// ---------------------------------------------------------------------------
// Trạng thái nguồn: mốc dữ liệu, "Nguồn đã thay đổi"
// ---------------------------------------------------------------------------

// AsOf là mốc dữ liệu của một nguồn (phòng × biểu): lần chốt gần nhất của các kỳ nguồn quy vào kỳ.
type AsOf struct {
	Department   string
	SourceModule string
	FormCode     string // rỗng: phòng không có số (Missing) hoặc nguồn chưa công bố (Unavailable)
	NativeKeys   []string
	ClosedAt     *time.Time
	SnapshotNo   *int64
	Closed       bool
	Missing      bool
	Unavailable  bool
	Fallback     bool
	Confirmation string // unconfirmed nếu có kỳ nguồn chưa xác nhận (D2)
}

// asOf gộp dòng ảnh chụp theo phòng × biểu.
func asOf(rows []domain.Row) []AsOf {
	var out []AsOf
	idx := map[string]int{}
	for _, r := range rows {
		k := r.Department + "|" + r.FormCode
		i, ok := idx[k]
		if !ok {
			idx[k] = len(out)
			i = len(out)
			out = append(out, AsOf{Department: r.Department, SourceModule: r.SourceModule, FormCode: r.FormCode, Closed: true,
				Confirmation: port.ConfirmationConfirmed})
		}
		a := &out[i]
		a.Missing, a.Unavailable = a.Missing || r.Missing, a.Unavailable || r.Unavailable
		a.Fallback = a.Fallback || r.Fallback
		a.Closed = a.Closed && r.Closed && !r.Unavailable
		if r.Confirmation == port.ConfirmationUnconfirmed {
			a.Confirmation = port.ConfirmationUnconfirmed
		}
		if r.NativeKey != "" && !slices.Contains(a.NativeKeys, r.NativeKey) {
			a.NativeKeys = append(a.NativeKeys, r.NativeKey)
		}
		if r.ClosedAt != nil && (a.ClosedAt == nil || r.ClosedAt.After(*a.ClosedAt)) {
			t := *r.ClosedAt
			a.ClosedAt = &t
		}
		if r.SnapshotNo != nil && (a.SnapshotNo == nil || *r.SnapshotNo > *a.SnapshotNo) {
			n := *r.SnapshotNo
			a.SnapshotNo = &n
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Department != out[j].Department {
			return out[i].Department < out[j].Department
		}
		return out[i].FormCode < out[j].FormCode
	})
	return out
}

// Status là trạng thái nguồn của kỳ trên màn kỳ KHTC.
type Status struct {
	PeriodID   uuid.UUID
	OwnerRef   uuid.UUID
	OwnerName  string
	Terminal   string // closed | cancelled | ""
	CanRefresh bool   // người xem là người phụ trách, kỳ chưa đóng/hủy
	Snapshot   *store.Snapshot
	ActorName  string
	Values     []store.Value
	Entries    map[uuid.UUID]port.KHTCPlanEntry
	AsOf       []AsOf
	Live       bool // AsOf lấy từ port (chưa có ảnh chụp)
	// Changes: "Nguồn đã thay đổi" — kỳ nguồn khác ảnh chụp (figures_hash, tập kỳ nguồn). Chỉ cảnh báo.
	Changes   []domain.SourceChange
	LiveError string // đọc port lỗi: không xác định được nguồn có đổi
}

// SourceChanged: có nguồn khác ảnh chụp (BR-DL-04).
func (s Status) SourceChanged() bool { return len(s.Changes) > 0 }

func (s *Service) names(ctx context.Context, refs ...uuid.UUID) map[uuid.UUID]string {
	if s.Names == nil {
		return nil
	}
	n := s.Names()
	if n == nil {
		return nil
	}
	m, err := n.DisplayNames(ctx, refs)
	if err != nil {
		return nil
	}
	return m
}

// Status đọc mốc dữ liệu, số của ảnh chụp mới nhất và so nguồn hiện tại với ảnh chụp.
func (s *Service) Status(ctx context.Context, id uuid.UUID) (Status, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Status{}, err
	}
	var (
		p    pstore.Period
		term string
		snap *store.Snapshot
		rows []domain.Row
		vals []store.Value
	)
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if p, term, e = s.loadPeriod(ctx, tx, id); e != nil {
			return e
		}
		if snap, e = store.LatestSnapshot(ctx, tx, id); e != nil || snap == nil {
			return e
		}
		if rows, e = store.Rows(ctx, tx, snap.ID); e != nil {
			return e
		}
		vals, e = store.Values(ctx, tx, snap.ID)
		return e
	})
	if err != nil {
		return Status{}, err
	}
	st := Status{PeriodID: id, OwnerRef: Owner(p), Terminal: term, Snapshot: snap, Values: vals, CanRefresh: term == "" && canRefresh(c, p) == nil}
	if s.Finals != nil {
		final, e := s.Finals.HasFinal(ctx, id.String())
		if e != nil {
			return Status{}, ErrDependency.WithCause(e)
		}
		st.CanRefresh = st.CanRefresh && !final
	}
	refs := []uuid.UUID{st.OwnerRef}
	if snap != nil {
		refs = append(refs, snap.ActorRef)
	}
	nm := s.names(ctx, refs...)
	st.OwnerName = nm[st.OwnerRef]
	if snap != nil {
		st.ActorName = nm[snap.ActorRef]
	}
	if _, _, entries, err := s.planKeys(ctx, p); err == nil {
		st.Entries = entries
	}
	cal := calendarOf(p)
	live, lerr := s.Sources.Periods(ctx, c.p.TenantID, port.KHTCQuery{ConfirmationAt: cutoffEnd(p.Cutoff), From: p.From, To: p.To,
		Target: port.CrosswalkTarget{CalendarID: cal.ID, RuleTag: cal.Rule.Tag(), PeriodType: rc.PeriodWeek}})
	liveRows := domain.Rows(live, p.From, p.To)
	switch {
	case lerr != nil:
		st.LiveError = "không đọc được số liệu nguồn hiện tại"
		st.AsOf = asOf(rows)
	case snap == nil:
		st.AsOf, st.Live = asOf(liveRows), true
	default:
		st.AsOf = asOf(rows)
		st.Changes = domain.Changes(rows, liveRows)
	}
	return st, nil
}

// HistoryEntry là một lần "Cập nhật số liệu" và các thay đổi của nó.
type HistoryEntry struct {
	Snapshot  store.Snapshot
	ActorName string
	Changes   []store.Change
}

// History liệt kê các lần cập nhật của kỳ (mới trước) với lịch sử cũ → mới (KH-QD-08).
func (s *Service) History(ctx context.Context, id uuid.UUID) ([]HistoryEntry, map[uuid.UUID]port.KHTCPlanEntry, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, nil, err
	}
	var (
		p     pstore.Period
		snaps []store.Snapshot
		chs   []store.Change
	)
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if p, _, e = s.loadPeriod(ctx, tx, id); e != nil {
			return e
		}
		if snaps, e = store.Snapshots(ctx, tx, id, 200); e != nil {
			return e
		}
		chs, e = store.Changes(ctx, tx, id)
		return e
	})
	if err != nil {
		return nil, nil, err
	}
	var refs []uuid.UUID
	for _, sn := range snaps {
		refs = append(refs, sn.ActorRef)
	}
	nm := s.names(ctx, refs...)
	by := map[uuid.UUID][]store.Change{}
	for _, ch := range chs {
		by[ch.SnapshotID] = append(by[ch.SnapshotID], ch)
	}
	out := make([]HistoryEntry, 0, len(snaps))
	for _, sn := range snaps {
		out = append(out, HistoryEntry{Snapshot: sn, ActorName: nm[sn.ActorRef], Changes: by[sn.ID]})
	}
	_, _, entries, _ := s.planKeys(ctx, p)
	return out, entries, nil
}

// Cutoff is a calendar date stored at UTC midnight. Resolve its END in Vietnam (M6).
func cutoffEnd(day time.Time) *time.Time {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, clock.Zone())
	end := start.AddDate(0, 0, 1).Add(-time.Microsecond).UTC()
	return &end
}
