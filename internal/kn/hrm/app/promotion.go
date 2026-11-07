package app

// FN-09 HCTH v4.0: hồ sơ hiện hành, chỉ thường xuyên, ba trạng thái.

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// PromotionConfig là phần cấu hình nâng bậc của Service.
type PromotionConfig struct {
	Writer port.PromotionWriter // W10.17 — Word; nil → chỉ Excel
}

// promoCaller: xem/xuất cần export = all (FN-09 "Cấp Tỉnh – HCTH, Admin"); ghi cần thêm profile.edit = all.
func (s *Service) promoCaller(ctx context.Context, write bool) (rc, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return rc{}, err
	}
	if c.m[domain.PermExport] != domain.ScopeAll {
		return rc{}, problem.Forbidden.WithDetail("nâng bậc: chỉ Cấp Tỉnh – HCTH, Admin (FN-09, FN-10 — quyền xuất toàn Trung tâm)")
	}
	if write {
		if err := c.need(domain.PermEdit, true); err != nil {
			return rc{}, err
		}
		if c.m[domain.PermEdit] != domain.ScopeAll {
			return rc{}, problem.Forbidden.WithDetail("nâng bậc: cần quyền sửa hồ sơ toàn Trung tâm")
		}
	}
	return c, nil
}

// caseState là trạng thái xử lý hiện hành của một kỳ (dòng promotion_event mới nhất).
type caseState struct {
	Kind     domain.PromoKind
	Staff    uuid.UUID
	Status   domain.PromoStatus
	Gone     bool // NLTH đã gỡ
	Action   string
	Reason   string // lý do đưa vào danh sách / đề xuất
	Note     string // ghi chú trả về (không có tên trong quyết định)
	Award    *uuid.UUID
	ExportID *uuid.UUID
	Events   []store.PromoEvent
}

// decisionState là một quyết định đã gộp các dòng sổ.
type decisionState struct {
	Issue  store.DecisionRow
	DocID  *uuid.UUID
	Cancel *store.DecisionRow
	Rows   []store.DecisionRow
}

func (d decisionState) active() bool { return d.Cancel == nil }

type promoWorld struct {
	day       time.Time
	heads     map[uuid.UUID]store.StaffRow
	order     []store.StaffRow
	events    map[uuid.UUID][]domain.Event
	snaps     map[uuid.UUID]domain.Snapshot
	scales    map[string]store.ScaleRow
	cat       domain.Catalog
	cases     map[string]*caseState
	decisions map[uuid.UUID]*decisionState
	decOrder  []uuid.UUID
	cands     []domain.Candidate
	byKey     map[string]domain.Candidate
	missing   []uuid.UUID // viên chức đang công tác chưa tính được kỳ (thiếu ngạch, bậc, bảng hệ số)
	activeVC  int
}

// loadPromo nạp thế giới tại ngày day trong giao dịch tx. units: danh mục đơn vị nội bộ (tên đơn vị) — nil được.
func loadPromo(ctx context.Context, tx pgx.Tx, day time.Time, units []domain.UnitRef) (*promoWorld, error) {
	w := &promoWorld{day: day, heads: map[uuid.UUID]store.StaffRow{}, events: map[uuid.UUID][]domain.Event{},
		snaps: map[uuid.UUID]domain.Snapshot{}, scales: map[string]store.ScaleRow{}, cases: map[string]*caseState{},
		decisions: map[uuid.UUID]*decisionState{}, byKey: map[string]domain.Candidate{},
		cat: domain.Catalog{Items: map[domain.Kind]map[string]domain.CatItem{}, Units: units}}
	heads, err := store.CurrentStaff(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, h := range sortedHeads(heads) {
		w.heads[h.StaffID] = h
		w.order = append(w.order, h)
	}
	evs, err := store.Events(ctx, tx, uuid.Nil)
	if err != nil {
		return nil, err
	}
	for _, e := range evs {
		w.events[e.StaffID] = append(w.events[e.StaffID], e.Event)
	}
	items, err := store.CatalogKnown(ctx, tx, nil)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		k := domain.Kind(it.Kind)
		if w.cat.Items[k] == nil {
			w.cat.Items[k] = map[string]domain.CatItem{}
		}
		var a domain.Attrs
		_ = jsonUnmarshal(it.Attrs, &a)
		w.cat.Items[k][it.Code] = domain.CatItem{Name: it.Name, SortOrder: it.SortOrder, Attrs: a}
	}
	sc, err := store.ScalesAt(ctx, tx, "", day)
	if err != nil {
		return nil, err
	}
	for _, x := range sc {
		w.scales[x.GradeCode] = x
	}
	pes, err := store.PromoEvents(ctx, tx, uuid.Nil)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(pes, func(i, j int) bool { return pes[i].RecordedAt.Before(pes[j].RecordedAt) })
	for _, e := range pes {
		cs := w.cases[e.CaseKey]
		if cs == nil {
			cs = &caseState{Kind: domain.PromoKind(e.Kind), Staff: e.StaffID}
			w.cases[e.CaseKey] = cs
		}
		cs.Events = append(cs.Events, e)
		cs.Action = e.Action
		st, alive := domain.StatusAfter(cs.Kind, e.Action)
		cs.Status, cs.Gone = st, !alive
		if (e.Action == domain.PromoActReturn || e.Action == domain.PromoActUndecide) && cs.ExportID == nil {
			cs.Status = domain.PromoPending
		}
		switch e.Action {
		case domain.PromoActInclude, domain.PromoActPropose:
			cs.Reason, cs.Note = e.Reason, ""
			if e.AwardEvent != nil {
				cs.Award = e.AwardEvent
			}
		case domain.PromoActReturn:
			cs.Note = e.Reason
		case domain.PromoActExport:
			cs.ExportID = e.ExportID
		}
	}
	decs, err := store.Decisions(ctx, tx, uuid.Nil)
	if err != nil {
		return nil, err
	}
	for _, d := range decs {
		st := w.decisions[d.DecisionID]
		if st == nil {
			st = &decisionState{}
			w.decisions[d.DecisionID] = st
			w.decOrder = append(w.decOrder, d.DecisionID)
		}
		st.Rows = append(st.Rows, d)
		switch d.Action {
		case "issue":
			st.Issue = d
		case "attach":
			id := *d.DocID
			st.DocID = &id
		case "cancel":
			x := d
			st.Cancel = &x
		}
	}
	w.compute()
	return w, nil
}

func (w *promoWorld) rank(level string) int {
	if level == "" {
		return -1
	}
	return w.cat.Items[domain.KindEducationLevel][level].Attrs.Rank
}

// tnvkOf: lần hưởng TNVK gần nhất từ quyết định TNVK còn hiệu lực (ngày hưởng ≤ ngày xét).
func (w *promoWorld) tnvkOf(staff uuid.UUID, at time.Time) *domain.TNVKState {
	var best *domain.TNVKState
	for _, id := range w.decOrder {
		d := w.decisions[id]
		if !d.active() || d.Issue.Kind != string(domain.PromoTNVK) {
			continue
		}
		for _, p := range d.Issue.People {
			eff, err := time.Parse(time.DateOnly, p.EffectiveOn)
			pct, err2 := decimal.NewFromString(p.PctTo)
			if p.StaffID != staff || err != nil || err2 != nil || eff.After(at) {
				continue
			}
			if best == nil || eff.After(best.Since) {
				best = &domain.TNVKState{Pct: pct, Since: eff}
			}
		}
	}
	return best
}

func (w *promoWorld) subject(h store.StaffRow) (domain.PromoSubject, bool) {
	snap := domain.Project(w.events[h.StaffID], w.day, w.rank)
	w.snaps[h.StaffID] = snap
	sub := domain.PromoSubject{StaffID: h.StaffID, Snap: snap}
	if h.Type != domain.StaffVienChuc || snap.Status != domain.StatusActive {
		return sub, false
	}
	if sc, ok := w.scales[snap.Grade]; ok {
		sub.Coefficients = sc.Coefficients
	}
	return sub, true
}

func (w *promoWorld) compute() {
	for _, h := range w.order {
		sub, ok := w.subject(h)
		if !ok {
			continue
		}
		w.activeVC++
		// Bậc cuối không thuộc FN-09, kể cả khi hồ sơ còn thiếu mốc/NB07.
		if len(sub.Coefficients) > 0 && sub.Snap.Step >= len(sub.Coefficients) {
			continue
		}
		cs := domain.Compute(sub, domain.PromotionRules{}, 0)
		if len(cs) == 0 {
			w.missing = append(w.missing, h.StaffID)
		}
		for _, c := range cs {
			w.cands = append(w.cands, c)
			w.byKey[c.Key] = c
		}
	}
}

func sortedHeads(xs []store.StaffRow) []store.StaffRow {
	out := append([]store.StaffRow(nil), xs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// status trả trạng thái xử lý của kỳ c (mặc định: NLTX/TNVK "Chưa xử lý", NLTH "Đã đưa vào danh sách").
func (w *promoWorld) status(c domain.Candidate) (domain.PromoStatus, *caseState) {
	// A future independent decision exists immediately, before its step event takes effect.
	for _, id := range w.decOrder {
		d := w.decisions[id]
		if !d.active() || d.Issue.Kind != string(domain.PromoNLTX) {
			continue
		}
		for _, p := range d.Issue.People {
			if p.StaffID != c.StaffID {
				continue
			}
			key := p.CaseKey
			if key == "" {
				eff, err := time.Parse(time.DateOnly, p.EffectiveOn)
				if err != nil {
					continue
				}
				key = domain.CaseKey(p.StaffID, domain.PromoNLTX, p.ToStep, eff, uuid.Nil)
			}
			if key == c.Key {
				return domain.PromoDecided, w.cases[c.Key]
			}
		}
	}
	if cs, ok := w.cases[c.Key]; ok {
		return cs.Status, cs
	}
	if c.Kind == domain.PromoNLTH {
		return domain.PromoListed, nil
	}
	return domain.PromoPending, nil
}

// --- Dữ liệu ra. ---

// CaseView là một kỳ nâng bậc trên màn danh sách / chi tiết.
type CaseView struct {
	domain.Candidate
	Code          string
	Name          string
	Unit          string
	UnitName      string
	GradeName     string
	Status        domain.PromoStatus
	Reason        string
	Note          string
	MonthsEnjoyed string
	DecisionID    *uuid.UUID
	DecisionNo    string
}

func (w *promoWorld) view(c domain.Candidate, names map[uuid.UUID]string) CaseView {
	h := w.heads[c.StaffID]
	sn := w.snaps[c.StaffID]
	st, cs := w.status(c)
	v := CaseView{Candidate: c, Code: h.Code, Name: names[c.StaffID], Unit: sn.Unit, UnitName: w.cat.UnitName(sn.Unit),
		GradeName: w.cat.Name(domain.KindGrade, c.Grade), Status: st, MonthsEnjoyed: domain.MonthsEnjoyed(c.Due)}
	if cs != nil {
		v.Reason, v.Note = cs.Reason, cs.Note
	}
	return v
}

// decidedViews: kỳ đã có quyết định (quyết định còn hiệu lực) có ngày hưởng trong [from, to].
func (w *promoWorld) decidedViews(from, to time.Time, names map[uuid.UUID]string) []CaseView {
	var out []CaseView
	for _, id := range w.decOrder {
		d := w.decisions[id]
		if !d.active() || d.Issue.Kind != string(domain.PromoNLTX) {
			continue
		}
		for _, p := range d.Issue.People {
			eff, err := time.Parse(time.DateOnly, p.EffectiveOn)
			if err != nil || eff.Before(from) || eff.After(to) {
				continue
			}
			k := domain.PromoKind(d.Issue.Kind)
			key := p.CaseKey
			if key == "" {
				key = domain.CaseKey(p.StaffID, k, p.ToStep, eff, uuid.Nil)
				if k == domain.PromoNLTH && p.AwardEventID != nil {
					key = domain.CaseKey(p.StaffID, k, 0, eff, *p.AwardEventID)
				}
			}
			c := domain.Candidate{Key: key, StaffID: p.StaffID, Kind: k, Due: eff, Grade: p.Grade, FromStep: p.FromStep, ToStep: p.ToStep,
				Cond: domain.CondOK}
			previous := domain.Project(w.events[p.StaffID], eff.AddDate(0, 0, -1), nil)
			c.StepSince, _, _ = domain.ParseDate(previous.StepSince)
			c.PromotionMonths = previous.PromotionMonths
			c.NextDue = eff
			c.FromCoeff, _ = decimal.NewFromString(p.FromCoeff)
			c.ToCoeff, _ = decimal.NewFromString(p.ToCoeff)
			if p.PctTo != "" {
				c.PctTo, _ = decimal.NewFromString(p.PctTo)
			}
			if p.PctFrom != "" {
				x, _ := decimal.NewFromString(p.PctFrom)
				c.PctFrom = &x
			}
			v := w.view(c, names)
			v.Status = domain.PromoDecided
			did := d.Issue.DecisionID
			v.DecisionID, v.DecisionNo = &did, d.Issue.No
			out = append(out, v)
		}
	}
	return out
}

// PromotionList has no indicator, quota or achievement/rating block in FN-09 v4.
type PromotionList struct {
	Day     time.Time
	Items   []CaseView
	Missing []PersonBrief
}

func (s *Service) unitRefs(ctx context.Context, c rc) ([]domain.UnitRef, error) {
	if s.Rec.Units == nil || c.unitRef == uuid.Nil {
		return nil, nil
	}
	us, err := s.Rec.Units.InternalUnits(ctx, c.unitRef)
	if err != nil {
		return nil, err
	}
	out := make([]domain.UnitRef, 0, len(us))
	for _, u := range us {
		out = append(out, domain.UnitRef{Code: u.Code, Name: u.Name})
	}
	return out, nil
}

// world loads current profile data in one read transaction, with no promotion-rule dependency.
func (s *Service) world(ctx context.Context, c rc, day time.Time) (*promoWorld, error) {
	units, err := s.unitRefs(ctx, c)
	if err != nil {
		return nil, err
	}
	var w *promoWorld
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error { var e error; w, e = loadPromo(ctx, tx, day, units); return e })
	return w, err
}

// names đọc họ tên (kho định danh, theo lô) của các hồ sơ ids; người xem không có vai kho → nhãn rỗng.
func (s *Service) names(ctx context.Context, w *promoWorld, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	seen := map[uuid.UUID]bool{}
	var refs []uuid.UUID
	bySubject := map[uuid.UUID]uuid.UUID{}
	for _, id := range ids {
		h, ok := w.heads[id]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		refs = append(refs, h.SubjectRef)
		bySubject[h.SubjectRef] = id
	}
	got, err := s.readPII(ctx, refs, client.PurposeHRMProfile, true, "")
	if forbiddenErr(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for ref, it := range got {
		if it.Erased {
			out[bySubject[ref]] = client.ErasedLabel
			continue
		}
		out[bySubject[ref]] = it.FullName
	}
	return out, nil
}

// Promotion always projects the current profile. Year/month only select due dates.
func (s *Service) Promotion(ctx context.Context, year, month int, status, unit, query string) (PromotionList, error) {
	c, err := s.promoCaller(ctx, false)
	if err != nil {
		return PromotionList{}, err
	}
	day := s.day(time.Time{})
	w, err := s.world(ctx, c, day)
	if err != nil {
		return PromotionList{}, err
	}
	var ids []uuid.UUID
	for _, x := range w.cands {
		ids = append(ids, x.StaffID)
	}
	for _, id := range w.decOrder {
		ids = append(ids, w.decisions[id].Issue.StaffIDs...)
	}
	ids = append(ids, w.missing...)
	names, err := s.names(ctx, w, ids)
	if err != nil {
		return PromotionList{}, err
	}
	out := PromotionList{Day: day}
	for _, x := range w.cands {
		if st, _ := w.status(x); st != domain.PromoDecided {
			out.Items = append(out.Items, w.view(x, names))
		}
	}
	out.Items = append(out.Items, w.decidedViews(time.Time{}, time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), names)...)
	selected := make([]CaseView, 0, len(out.Items))
	q := strings.ToLower(strings.TrimSpace(query))
	for _, v := range out.Items {
		if year > 0 && v.Due.Year() != year || month > 0 && int(v.Due.Month()) != month || status != "" && string(v.Status) != status || unit != "" && v.Unit != unit {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(v.Name), q) {
			continue
		}
		selected = append(selected, v)
	}
	out.Items = selected
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if !a.Due.Equal(b.Due) {
			return a.Due.Before(b.Due)
		}
		return a.Code < b.Code
	})
	for _, id := range w.missing {
		out.Missing = append(out.Missing, PersonBrief{ID: id, Code: w.heads[id].Code, Type: w.heads[id].Type, Name: names[id], Unit: w.snaps[id].Unit, UnitName: w.cat.UnitName(w.snaps[id].Unit)})
	}
	return out, nil
}

// CaseDetail là màn chi tiết một kỳ (FN-09 B3): kỳ, căn cứ điều kiện, lịch sử xử lý, xếp loại, thành tích.
type CaseDetail struct {
	CaseView
	History []store.PromoEvent
}

// PromotionCase trả chi tiết kỳ key tại ngày at.
func (s *Service) PromotionCase(ctx context.Context, key string) (CaseDetail, error) {
	c, err := s.promoCaller(ctx, false)
	if err != nil {
		return CaseDetail{}, err
	}
	staff, kind, ok := domain.ParseCaseKey(key)
	if !ok || kind != domain.PromoNLTX {
		return CaseDetail{}, port.ErrCaseNotFound
	}
	day := s.day(time.Time{})
	w, err := s.world(ctx, c, day)
	if err != nil {
		return CaseDetail{}, err
	}
	names, err := s.names(ctx, w, []uuid.UUID{staff})
	if err != nil {
		return CaseDetail{}, err
	}
	var d CaseDetail
	if x, ok := w.byKey[key]; ok {
		d.CaseView = w.view(x, names)
	} else {
		found := false
		for _, v := range w.decidedViews(time.Time{}, time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), names) {
			if v.Key == key {
				d.CaseView, found = v, true
			}
		}
		if !found {
			return CaseDetail{}, port.ErrCaseNotFound
		}
	}
	if cs, ok := w.cases[key]; ok {
		d.History = cs.Events
	}
	return d, nil
}

// --- Xếp loại năm. ---

// RatingInput là một xếp loại năm cần ghi (rating rỗng = hủy xếp loại của năm).
type RatingInput struct {
	Year   int
	Rating string
	Source string
	Reason string
}

// SetRating ghi xếp loại năm của một viên chức (nguồn chuyển đổi ban đầu hoặc nhập tay; FN-18 W11.26 ghi nguồn
// 'evaluation'). Đã có xếp loại của năm → dòng mới THAY dòng cũ, cần lý do ≥ 10 ký tự.
func (s *Service) SetRating(ctx context.Context, id uuid.UUID, in RatingInput, idem string) (store.RatingRow, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return store.RatingRow{}, err
	}
	if err := c.need(domain.PermEdit, true); err != nil {
		return store.RatingRow{}, err
	}
	if in.Year < 1990 || in.Year > 2100 {
		return store.RatingRow{}, problem.Validation.WithField("year", "năm 1990–2100")
	}
	if in.Rating != "" && !contains(domain.Ratings, in.Rating) {
		return store.RatingRow{}, problem.Validation.WithField("rating", "HTXSNV | HTTNV | HTNV | KHTNV")
	}
	if in.Source == "" {
		in.Source = "manual"
	}
	if in.Source != "manual" && in.Source != "conversion" {
		return store.RatingRow{}, problem.Validation.WithField("source", "manual | conversion")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	row := store.RatingRow{ID: ids.New(), StaffID: id, Year: in.Year, Rating: in.Rating, Source: in.Source, Void: in.Rating == "", Actor: c.ref}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if _, e := s.editable(ctx, tx, c, id, domain.PermEdit); e != nil {
			return e
		}
		if e := store.LockRating(ctx, tx, c.tenant, id); e != nil {
			return e
		}
		rows, e := store.Ratings(ctx, tx, id)
		if e != nil {
			return e
		}
		if cur, ok := store.EffectiveRatings(rows)[id][in.Year]; ok {
			if len([]rune(in.Reason)) < 10 || len([]rune(in.Reason)) > 500 {
				return problem.Validation.WithField("reason", "sửa/hủy xếp loại đã có cần lý do 10–500 ký tự")
			}
			row.Supersedes, row.Reason = &cur.ID, in.Reason
		} else if row.Void {
			return problem.Validation.WithField("rating", "năm này chưa có xếp loại để hủy")
		}
		var e2 error
		row.Seq, e2 = store.InsertRating(ctx, tx, c.tenant, row, s.Clock.Now(), stableKey(idem, "rating:"+id.String()))
		return classify(e2)
	})
	return row, err
}

// Ratings trả xếp loại hiệu lực + lịch sử của một hồ sơ (cần quyền xem hồ sơ đó).
func (s *Service) Ratings(ctx context.Context, id uuid.UUID) ([]store.RatingRow, error) {
	c, err := s.recCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.need(domain.PermView, false); err != nil {
		return nil, err
	}
	var out []store.RatingRow
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		f, e := loadFile(ctx, tx, id)
		if e != nil {
			return e
		}
		if !c.on(domain.PermView, f.head, domain.Project(f.events, s.day(time.Time{}), nil)) {
			return port.ErrStaffNotFound
		}
		out, e = store.Ratings(ctx, tx, id)
		return e
	})
	return out, err
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
