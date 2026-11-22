package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// VersionView là phần đầu một phiên bản kế hoạch + người thao tác.
type VersionView struct {
	store.VersionRow
	Actor Actor
}

// EntryView là một dòng kế hoạch + số La Mã + mã/tên CT/DA chủ.
type EntryView struct {
	domain.Entry
	Roman       string
	ProgramCode string
	ProgramName string
}

// PlanView là một phiên bản kế hoạch năm đầy đủ.
type PlanView struct {
	Version  VersionView
	Latest   int // số phiên bản mới nhất của năm
	Entries  []EntryView
	Programs map[uuid.UUID]store.ProgramRow
}

func validYear(y int) error {
	if y < 2000 || y > 2100 {
		return problem.Validation.WithField("year", "năm 2000–2100")
	}
	return nil
}

func (s *Service) verViews(ctx context.Context, rs []store.VersionRow) []VersionView {
	refs := make([]uuid.UUID, 0, len(rs))
	for _, r := range rs {
		refs = append(refs, r.ActorRef)
	}
	act := s.actors(ctx, refs)
	out := make([]VersionView, 0, len(rs))
	for _, r := range rs {
		out = append(out, VersionView{VersionRow: r, Actor: act(r.ActorRef)})
	}
	return out
}

func heads(vs []store.VersionRow) []domain.VersionHead {
	out := make([]domain.VersionHead, 0, len(vs))
	for _, v := range vs {
		out = append(out, domain.VersionHead{Version: int(v.Seq), EffectiveFrom: v.EffectiveFrom})
	}
	return out
}

func programMap(rs []store.ProgramRow) map[uuid.UUID]store.ProgramRow {
	m := make(map[uuid.UUID]store.ProgramRow, len(rs))
	for _, r := range rs {
		m[r.Ref] = r
	}
	return m
}

func entryViews(es []domain.Entry, progs map[uuid.UUID]store.ProgramRow, at *time.Time) []EntryView {
	roman := domain.RomanNumbers(es, at)
	out := make([]EntryView, 0, len(es))
	for _, e := range es {
		v := EntryView{Entry: e, Roman: roman[e.Ref]}
		if e.ProgramRef != nil {
			if p, ok := progs[*e.ProgramRef]; ok {
				v.ProgramCode, v.ProgramName = p.Code, p.Name
			}
		}
		out = append(out, v)
	}
	return out
}

// loadPlan đọc phiên bản (version > 0: đúng số; at != nil: theo ngày hiệu lực; còn lại: mới nhất) trong tx.
func loadPlan(ctx context.Context, tx pgx.Tx, year, version int, at *time.Time) (store.VersionRow, int, []domain.Entry, error) {
	vs, err := store.Versions(ctx, tx, year)
	if err != nil {
		return store.VersionRow{}, 0, nil, err
	}
	n := len(vs)
	pick := n
	switch {
	case version > 0:
		pick = version
	case at != nil:
		pick = domain.PickAt(heads(vs), *at)
	}
	if pick < 1 || pick > n {
		return store.VersionRow{}, n, nil, ErrPlanNotFound
	}
	v := vs[pick-1]
	es, err := store.Entries(ctx, tx, v.ID)
	return v, n, es, err
}

// GetPlan trả một phiên bản kế hoạch năm: ?version=n, ?at=YYYY-MM-DD (phiên bản hiệu lực, BR-DL-13), mặc định mới nhất.
func (s *Service) GetPlan(ctx context.Context, year, version int, at string) (PlanView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return PlanView{}, err
	}
	if err := validYear(year); err != nil {
		return PlanView{}, err
	}
	var atp *time.Time
	if strings.TrimSpace(at) != "" {
		t, err := domain.ParseDate(at)
		if err != nil {
			return PlanView{}, problem.Validation.WithField("at", "phải là ngày YYYY-MM-DD")
		}
		atp = &t
	}
	var pv PlanView
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		v, n, es, err := loadPlan(ctx, tx, year, version, atp)
		if err != nil {
			return err
		}
		progs, err := store.ProgramsCurrent(ctx, tx)
		if err != nil {
			return err
		}
		pv = PlanView{Version: VersionView{VersionRow: v}, Latest: n, Programs: programMap(progs)}
		pv.Entries = entryViews(es, pv.Programs, atp)
		return nil
	})
	if err != nil {
		return PlanView{}, err
	}
	pv.Version = s.verViews(ctx, []store.VersionRow{pv.Version.VersionRow})[0]
	return pv, nil
}

// Versions trả lịch sử phiên bản của năm (cũ trước).
func (s *Service) Versions(ctx context.Context, year int) ([]VersionView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if err := validYear(year); err != nil {
		return nil, err
	}
	var vs []store.VersionRow
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		vs, e = store.Versions(ctx, tx, year)
		return e
	}); err != nil {
		return nil, err
	}
	return s.verViews(ctx, vs), nil
}

// SaveInput là một lần "Lưu" kế hoạch năm: phiên bản 1 (lập) hoặc "Điều chỉnh theo QĐ" (phiên bản ≥ 2).
type SaveInput struct {
	Year           int
	BaseVersion    int // số phiên bản mới nhất người dùng đang sửa (0 = chưa có) — khóa lạc quan
	DecisionNo     string
	DecisionDate   string
	EffectiveFrom  string
	CopiedFromYear int
	Note           string
	Entries        []domain.Entry
	IdemKey        string
}

// SaveVersion ghi phiên bản mới của kế hoạch năm (cả cây). Phiên bản cũ giữ nguyên, xem lại được.
func (s *Service) SaveVersion(ctx context.Context, in SaveInput) (PlanView, bool, error) {
	ctx = txdeps.With(ctx) // P0-POOL-KN-2: ngày chốt kỳ KHTC, dòng đã có số liệu ở phiếu đọc ngoài giao dịch (service.go tx)
	c, err := s.planWriter(ctx)
	if err != nil {
		return PlanView{}, false, err
	}
	if in.IdemKey == "" {
		return PlanView{}, false, problem.IdempotencyRequired
	}
	if err := validYear(in.Year); err != nil {
		return PlanView{}, false, err
	}
	replay := func() (*PlanView, error) {
		var v *store.VersionRow
		if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			var e error
			v, e = store.VersionByIdem(ctx, tx, in.IdemKey)
			return e
		}); err != nil || v == nil {
			return nil, err
		}
		pv, err := s.GetPlan(ctx, v.Year, int(v.Seq), "")
		return &pv, err
	}
	if pv, err := replay(); err != nil || pv != nil {
		if err != nil {
			return PlanView{}, false, err
		}
		return *pv, true, nil
	}
	ver := store.VersionRow{Year: in.Year, DecisionNo: strings.TrimSpace(in.DecisionNo), Note: strings.TrimSpace(in.Note),
		CopiedFromYear: in.CopiedFromYear}
	if len([]rune(ver.DecisionNo)) > 100 {
		return PlanView{}, false, problem.Validation.WithField("decision_no", "tối đa 100 ký tự")
	}
	if len([]rune(ver.Note)) > 1000 {
		return PlanView{}, false, problem.Validation.WithField("note", "tối đa 1000 ký tự")
	}
	if in.CopiedFromYear != 0 && in.CopiedFromYear != in.Year-1 {
		return PlanView{}, false, problem.Validation.WithField("copied_from_year", "chỉ sao chép từ năm liền trước")
	}
	if strings.TrimSpace(in.DecisionDate) != "" {
		d, err := domain.ParseDate(in.DecisionDate)
		if err != nil {
			return PlanView{}, false, problem.Validation.WithField("decision_date", "phải là ngày YYYY-MM-DD")
		}
		if d.After(s.today()) {
			return PlanView{}, false, problem.Validation.WithField("decision_date", "không ở tương lai")
		}
		ver.DecisionDate = &d
	}
	start := time.Date(in.Year, 1, 1, 0, 0, 0, 0, time.UTC)
	if strings.TrimSpace(in.EffectiveFrom) == "" {
		ver.EffectiveFrom = start
	} else {
		d, err := domain.ParseDate(in.EffectiveFrom)
		if err != nil {
			return PlanView{}, false, ErrEffectiveDate.WithField("effective_from", "phải là ngày YYYY-MM-DD")
		}
		ver.EffectiveFrom = d
	}
	if ver.EffectiveFrom.Year() != in.Year {
		return PlanView{}, false, ErrEffectiveDate.WithField("effective_from", fmt.Sprintf("trong năm kế hoạch %d", in.Year))
	}
	es := append([]domain.Entry(nil), in.Entries...)
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, "plan:"+strconv.Itoa(in.Year)); err != nil {
			return err
		}
		vs, err := store.Versions(ctx, tx, in.Year)
		if err != nil {
			return err
		}
		if in.BaseVersion != len(vs) {
			return ErrVersionConflict.With("latest_version", len(vs))
		}
		ver.Basis = domain.BasisInitial
		var prev []domain.Entry
		if len(vs) > 0 {
			ver.Basis = domain.BasisAdjust
			// "Điều chỉnh theo QĐ": Số quyết định, Ngày quyết định, Ngày hiệu lực bắt buộc (FN-TI-02.02 B5).
			p := ErrEffectiveDate
			bad := false
			if ver.DecisionNo == "" {
				p, bad = p.WithField("decision_no", "điều chỉnh theo QĐ bắt buộc số quyết định"), true
			}
			if ver.DecisionDate == nil {
				p, bad = p.WithField("decision_date", "điều chỉnh theo QĐ bắt buộc ngày quyết định"), true
			}
			if strings.TrimSpace(in.EffectiveFrom) == "" {
				p, bad = p.WithField("effective_from", "điều chỉnh theo QĐ bắt buộc ngày hiệu lực"), true
			}
			if bad {
				return p
			}
			if prev, err = store.Entries(ctx, tx, vs[len(vs)-1].ID); err != nil {
				return err
			}
		}
		if s.ClosedCutoff != nil {
			cut, err := txdeps.Get(ctx, "closed_cutoff:"+strconv.Itoa(in.Year), func(ctx context.Context) (*time.Time, error) {
				return s.ClosedCutoff(ctx, in.Year)
			})
			if err != nil {
				return err
			}
			if cut != nil && !ver.EffectiveFrom.After(*cut) {
				return ErrEffectiveDate.WithField("effective_from", "trước ngày chốt của kỳ đã đóng gần nhất ("+cut.Format("02/01/2006")+")")
			}
		}
		cat, err := store.CatalogCurrent(ctx, tx, "")
		if err != nil {
			return err
		}
		progs, err := store.ProgramsCurrent(ctx, tx)
		if err != nil {
			return err
		}
		pm := programMap(progs)
		usable := map[uuid.UUID]bool{}
		for _, p := range progs {
			if p.Status == domain.StatusActive {
				usable[p.Ref] = true
			}
		}
		for _, e := range prev {
			if e.ProgramRef != nil {
				usable[*e.ProgramRef] = true // CT/DA đã ngừng ở danh sách chủ vẫn giữ được trong kế hoạch đang có
			}
		}
		for i := range es {
			if es[i].Kind == domain.EntryProgram && strings.TrimSpace(es[i].Name) == "" && es[i].ProgramRef != nil {
				if p, ok := pm[*es[i].ProgramRef]; ok {
					es[i].Name = p.Name
				}
			}
		}
		codes := activeCodes(cat)
		if errs := domain.NormalizePlan(in.Year, es, domain.Refs{Units: codes[domain.KindUnit], Funding: codes[domain.KindFundingSource],
			Fields: codes[domain.KindField], Programs: usable}); len(errs) > 0 {
			return fieldsProblem(ErrPlanInvalid, errs)
		}
		if err := s.checkRemoved(ctx, in.Year, prev, es); err != nil {
			return err
		}
		ver.Meta = c.meta(newID(), s.now(), in.IdemKey)
		ver.EntriesCount, ver.EntriesHash = len(es), domain.EntriesHash(es)
		if err := store.InsertVersion(ctx, tx, c.p.TenantID, &ver); err != nil {
			return err
		}
		return store.InsertEntries(ctx, tx, c.p.TenantID, ver.ID, in.Year, es)
	})
	if dup(err) {
		if pv, rerr := replay(); rerr == nil && pv != nil {
			return *pv, true, nil
		}
	}
	if err != nil {
		return PlanView{}, false, err
	}
	pv, err := s.GetPlan(ctx, in.Year, int(ver.Seq), "")
	return pv, false, err
}

// checkRemoved: dòng có ở phiên bản trước mà không còn ở phiên bản mới — chỉ được bỏ khi chưa có số liệu (EntryUsage).
func (s *Service) checkRemoved(ctx context.Context, year int, prev, next []domain.Entry) error {
	if s.EntryUsage == nil || len(prev) == 0 {
		return nil
	}
	keep := map[uuid.UUID]bool{}
	for _, e := range next {
		keep[e.Ref] = true
	}
	var gone []uuid.UUID
	names := map[uuid.UUID]string{}
	for _, e := range prev {
		if !keep[e.Ref] {
			gone = append(gone, e.Ref)
			names[e.Ref] = e.Name
		}
	}
	if len(gone) == 0 {
		return nil
	}
	keys := make([]string, len(gone))
	for i, r := range gone {
		keys[i] = r.String()
	}
	slices.Sort(keys)
	used, err := txdeps.Get(ctx, "entry_usage:"+strconv.Itoa(year)+":"+strings.Join(keys, ","), func(ctx context.Context) (map[uuid.UUID]bool, error) {
		return s.EntryUsage(ctx, year, gone)
	})
	if err != nil {
		return err
	}
	p, bad := ErrEntryInUse, false
	for _, r := range gone {
		if used[r] {
			p, bad = p.WithField("entries", names[r]+" — đã có số liệu, chỉ ngừng theo dõi"), true
		}
	}
	if bad {
		return p
	}
	return nil
}

// Draft là bản nháp cây kế hoạch (chưa lưu): sao chép từ năm trước hoặc đọc từ Excel.
type Draft struct {
	FromYear    int
	FromVersion int
	Entries     []EntryView
	Problems    []RowProblem
}

// RowProblem là lỗi của một dòng Excel (row = số dòng trên trang tính, tính cả dòng tiêu đề).
type RowProblem struct {
	Row    int
	Field  string
	Reason string
}

// CopyDraft dựng bản nháp năm year từ phiên bản mới nhất của năm year−1 — chỉ nội dung còn theo dõi (FN-TI-02.02
// "Sao chép từ năm trước"). Không ghi; người dùng sửa rồi "Lưu" thành phiên bản 1 (copied_from_year = year−1).
func (s *Service) CopyDraft(ctx context.Context, year int) (Draft, error) {
	c, err := s.planWriter(ctx)
	if err != nil {
		return Draft{}, err
	}
	if err := validYear(year); err != nil {
		return Draft{}, err
	}
	var d Draft
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		v, _, es, err := loadPlan(ctx, tx, year-1, 0, nil)
		if err != nil {
			return err
		}
		progs, err := store.ProgramsCurrent(ctx, tx)
		if err != nil {
			return err
		}
		d = Draft{FromYear: year - 1, FromVersion: int(v.Seq), Entries: entryViews(domain.CopyForYear(es, newID), programMap(progs), nil)}
		return nil
	})
	if errors.Is(err, ErrPlanNotFound) {
		return Draft{}, ErrPlanNotFound.WithDetail(fmt.Sprintf("năm %d chưa có kế hoạch để sao chép", year-1))
	}
	return d, err
}

// ---------------------------------------------------------------------------
// Cổng PlanAt (W11.62, W11.63, W11.65)
// ---------------------------------------------------------------------------

// PlanAt trả phiên bản dùng cho ngày at (BR-DL-13).
func (p Ports) PlanAt(ctx context.Context, year int, at time.Time) (port.KHTCPlan, error) {
	return p.planVersion(ctx, year, uuid.Nil, at)
}

func (p Ports) PlanVersion(ctx context.Context, year int, versionID uuid.UUID, at time.Time) (port.KHTCPlan, error) {
	return p.planVersion(ctx, year, versionID, at)
}

func (p Ports) planVersion(ctx context.Context, year int, versionID uuid.UUID, at time.Time) (port.KHTCPlan, error) {
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	var out port.KHTCPlan
	err := p.tx(ctx, func(tx pgx.Tx) error {
		version := 0
		if versionID != uuid.Nil {
			versions, err := store.Versions(ctx, tx, year)
			if err != nil {
				return err
			}
			for _, v := range versions {
				if v.ID == versionID {
					version = int(v.Seq)
					break
				}
			}
			if version == 0 {
				return ErrPlanNotFound
			}
		}
		v, _, es, err := loadPlan(ctx, tx, year, version, &day)
		if err != nil {
			return err
		}
		out = port.KHTCPlan{VersionID: v.ID, Year: v.Year, Version: int(v.Seq), DecisionNo: v.DecisionNo, DecisionDate: v.DecisionDate,
			EffectiveFrom: v.EffectiveFrom}
		programs, err := store.ProgramsCurrent(ctx, tx)
		if err != nil {
			return err
		}
		names := programMap(programs)
		roman := domain.RomanNumbers(es, &day)
		for _, e := range es {
			if e.Kind == domain.EntryProgram && e.Name == "" && e.ProgramRef != nil {
				e.Name = names[*e.ProgramRef].Name
			}
			out.Entries = append(out.Entries, port.KHTCPlanEntry{Ref: e.Ref, Tab: e.Tab, Kind: e.Kind, ParentRef: e.ParentRef,
				ProgramRef: e.ProgramRef, Roman: roman[e.Ref], SortOrder: e.SortOrder, Code: e.Code, Name: e.Name, UnitCode: e.UnitCode,
				PlannedQty: e.PlannedQty, ScaleNote: e.ScaleNote, Department: e.Department, DataSource: e.DataSource,
				SourceKey: e.SourceKey, Tracking: e.Tracking, StoppedFrom: e.StoppedFrom, Communes: append([]string(nil), e.Communes...),
				PointsCount: e.PointsCount, Budget: e.Budget, FundingCode: e.FundingCode, FieldCode: e.FieldCode, StopDecision: e.StopDecision})
		}
		return nil
	})
	if errors.Is(err, ErrPlanNotFound) {
		return port.KHTCPlan{}, port.ErrKHTCPlanNotFound
	}
	return out, err
}
