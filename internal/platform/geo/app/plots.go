package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// mode quyết định cách xử lý chồng lấn cùng HTX và sửa trên phiên bản cũ.
type mode int

const (
	// modeOnline: người vẽ thấy bản đồ hiện hành (KN US-PLOT-03, HTX US-PLOT-04):
	// chồng lấn HTX khác → chặn 409 geo.overlap_cross_coop; chồng lấn cùng HTX/thửa
	// nháp hoặc lệch diện tích khai báo > 5 % → 409 cần xác nhận, trừ khi Confirm;
	// sửa trên phiên bản cũ → 409 geo.version_conflict.
	modeOnline mode = iota
	// modeOffline: bản ghi từ thiết bị ngoại tuyến — không chặn được máy, nên giữ
	// cả hai và mở xung đột chờ xử lý (mục 15) thay vì từ chối.
	modeOffline
)

// BoundaryInput là một ranh giới gửi lên.
type BoundaryInput struct {
	ID           uuid.UUID       // Nil → server sinh; ngoại tuyến do máy sinh (R-11)
	Geometry     json.RawMessage // GeoJSON Polygon/MultiPolygon, WGS84
	Parsed       domain.MultiPolygon
	Points       int
	GPSAccuracyM *decimal.Decimal
	Source       string
	EventTime    time.Time
	DeviceID     *string
	// DeclaredAreaHa: diện tích khai báo (tùy chọn) — lệch > geo.area_deviation_max_pct cần xác nhận (HTX US-PLOT-02 AC-3).
	DeclaredAreaHa *decimal.Decimal
	// Confirm (confirm_overlap): người vẽ xác nhận giữ nguyên dù chồng lấn cùng HTX/thửa nháp
	// hoặc lệch diện tích khai báo (HTX US-PLOT-04 AC-2). Không bỏ qua được chồng lấn HTX khác.
	Confirm bool
}

// CreateInput tạo thửa kèm phiên bản ranh giới đầu tiên.
type CreateInput struct {
	PlotRef        uuid.UUID // Nil → server sinh
	Name           string
	CommuneCode    string
	CooperativeRef uuid.UUID // Nil: KN vẽ trước khi gán HTX
	HouseholdRef   uuid.UUID
	Boundary       BoundaryInput
	IdemKey        string
	// KN (KN v7.7 FN-XA-03.05): thuộc tính v7.7 + chế độ hỗ trợ; khác nil → trực tuyến chặn MỌI chồng lấn.
	KN *KNInput
}

// CorrectInput ghi phiên bản ranh giới mới thay phiên bản SupersedesID.
type CorrectInput struct {
	PlotRef      uuid.UUID
	SupersedesID uuid.UUID
	Reason       string
	Boundary     BoundaryInput
	IdemKey      string
	KN           *KNInput // KN v7.7: cách vẽ, chế độ hỗ trợ; trực tuyến chặn mọi chồng lấn
}

// WriteResult là kết quả ghi ranh giới.
type WriteResult struct {
	Plot      store.Plot
	Boundary  store.Boundary
	Conflicts []store.Conflict
	// Flagged: chồng lấn vượt ngưỡng đã nhận nhưng chờ xem xét (cùng HTX đã xác nhận; khác HTX khi ngoại tuyến).
	Flagged  []domain.Overlap
	Replayed bool
}

// prepare đọc và kiểm hình phía Go (≥ 4 đỉnh, WGS84), chuẩn hóa nguồn và thời điểm.
func (s *Service) prepare(b *BoundaryInput, defaultSource string) error {
	if b.Parsed == nil {
		mp, n, err := domain.ParseGeometry(b.Geometry)
		if err != nil {
			return violationProblem(err)
		}
		b.Parsed, b.Points = mp, n
	}
	if b.Source == "" {
		b.Source = defaultSource
	}
	if !domain.KnownSource(b.Source) {
		return problem.Validation.WithField("source", "phải là kn_web, kn_field_app, htx_app, import_file hoặc cadastral")
	}
	if b.DeclaredAreaHa != nil && !b.DeclaredAreaHa.IsPositive() {
		return problem.Validation.WithField("declared_area_ha", "phải dương")
	}
	if b.GPSAccuracyM != nil && b.GPSAccuracyM.IsNegative() {
		return problem.Validation.WithField("gps_accuracy_m", "không được âm")
	}
	t, err := s.eventTime(b.EventTime)
	if err != nil {
		return err
	}
	b.EventTime = t
	if b.ID == uuid.Nil {
		b.ID = ids.New()
	} else if b.ID.Version() != 7 {
		return problem.Validation.WithField("boundary_id", "phải là UUIDv7")
	}
	return nil
}

// checked là kết quả kiểm hình trong PostGIS.
type checked struct {
	geojson string
	areaHa  decimal.Decimal
	same    []domain.Overlap // cùng HTX / thửa nháp — cần xác nhận
	cross   []domain.Overlap // HTX khác — bị chặn khi trực tuyến
	devPct  *decimal.Decimal // lệch so với diện tích khai báo (nếu có)
}

// checkShape kiểm hợp lệ, diện tích, nằm trong xã, chồng lấn (trong giao dịch đang mở).
func checkShape(ctx context.Context, tx pgx.Tx, mp domain.MultiPolygon, commune string, coop, exclude uuid.UUID, tol decimal.Decimal) (checked, error) {
	c := checked{geojson: mp.GeoJSON()}
	an, err := store.Analyze(ctx, tx, c.geojson)
	if err != nil {
		return c, err
	}
	if !an.Valid {
		return c, ErrInvalidGeometry.WithDetail("Ranh giới tự cắt hoặc sai cấu trúc (" + an.Reason + ") — hãy chấm các điểm theo đúng thứ tự đi vòng quanh thửa")
	}
	if an.AreaHa.LessThan(domain.MinAreaHa) || an.AreaHa.GreaterThan(domain.MaxAreaHa) {
		return c, ErrAreaOutOfRange.WithDetail("Diện tích "+an.AreaHa.String()+" ha nằm ngoài khoảng "+
			domain.MinAreaHa.String()+"–"+domain.MaxAreaHa.String()+" ha").With("area_ha", an.AreaHa.String())
	}
	c.areaHa = an.AreaHa
	out, err := store.OutsideCommunePct(ctx, tx, c.geojson, commune)
	if err != nil {
		return c, err
	}
	if out != nil && out.GreaterThan(tol) {
		return c, ErrOutsideCommune.WithDetail(out.StringFixed(2)+"% diện tích nằm ngoài địa giới xã "+commune).
			With("outside_pct", out.StringFixed(4))
	}
	c.same, c.cross, err = classifiedOverlaps(ctx, tx, c.geojson, an.AreaHa, coop, exclude, tol)
	return c, err
}

// classifiedOverlaps tìm và phân loại chồng lấn của hình geojson (diện tích areaHa) nếu thuộc HTX coop.
func classifiedOverlaps(ctx context.Context, tx pgx.Tx, geojson string, areaHa decimal.Decimal, coop, exclude uuid.UUID, tol decimal.Decimal) (same, cross []domain.Overlap, err error) {
	hits, err := store.FindOverlaps(ctx, tx, geojson, exclude)
	if err != nil {
		return nil, nil, err
	}
	overlaps := make([]domain.Overlap, 0, len(hits))
	for _, h := range hits {
		o := h.Overlap
		o.Pct = domain.OverlapPct(h.IntersectHa, h.AreaHa, areaHa)
		overlaps = append(overlaps, o)
	}
	same, cross = domain.ClassifyOverlaps(coop, overlaps, tol)
	return same, cross, nil
}

// deviation tính lệch diện tích so với khai báo.
func (c *checked) deviation(declared *decimal.Decimal) {
	if declared != nil {
		d := domain.AreaDeviationPct(c.areaHa, *declared)
		c.devPct = &d
	}
}

// onlineGate áp quy tắc chặn/cần xác nhận khi ghi trực tuyến.
func onlineGate(same, cross []domain.Overlap, devPct *decimal.Decimal, maxDev decimal.Decimal, confirm bool) error {
	if len(cross) > 0 {
		return overlapProblem(ErrOverlapCrossCoop, cross, "của HTX/nông hộ khác, vui lòng kiểm tra lại.")
	}
	if confirm {
		return nil
	}
	if len(same) > 0 {
		return overlapProblem(ErrOverlapNeedsConfirm, same, "— vui lòng kiểm tra lại hoặc xác nhận giữ nguyên (confirm_overlap).").
			With("needs_confirm", true)
	}
	if devPct != nil && devPct.GreaterThan(maxDev) {
		return ErrAreaDeviation.WithDetail("Diện tích đo lệch "+devPct.StringFixed(2)+"% so với diện tích khai báo (ngưỡng "+
			maxDev.String()+"%) — kiểm tra lại hoặc xác nhận giữ nguyên (confirm_overlap).").
			With("deviation_pct", devPct.StringFixed(4)).With("needs_confirm", true)
	}
	return nil
}

func overlapList(os []domain.Overlap) []map[string]any {
	out := make([]map[string]any, 0, len(os))
	for _, o := range os {
		m := map[string]any{"plot_ref": o.PlotRef.String(), "code": o.Code, "overlap_pct": o.Pct.StringFixed(4),
			"intersect_ha": o.IntersectHa.StringFixed(4), "cooperative_ref": nil}
		if o.CooperativeRef != uuid.Nil {
			m["cooperative_ref"] = o.CooperativeRef.String()
		}
		out = append(out, m)
	}
	return out
}

func refsOf(os []domain.Overlap) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(os))
	for _, o := range os {
		out = append(out, o.PlotRef)
	}
	return out
}

func overlapProblem(base *problem.Problem, os []domain.Overlap, tail string) *problem.Problem {
	codes := make([]string, 0, len(os))
	for _, o := range os {
		codes = append(codes, o.Code)
	}
	return base.WithDetail("Ranh giới bị chồng lấn với lô đã đăng ký ("+strings.Join(codes, ", ")+") "+tail).
		With("overlaps", overlapList(os))
}

// conflictsFor dựng xung đột cho chồng lấn đã được nhận (xác nhận hoặc ngoại tuyến).
func conflictsFor(plot store.Plot, boundaryID uuid.UUID, same, cross []domain.Overlap, now time.Time) []store.Conflict {
	var out []store.Conflict
	if len(same) > 0 {
		out = append(out, newConflict(domain.ConflictSameCoop, plot, boundaryID, same, now))
	}
	if len(cross) > 0 {
		out = append(out, newConflict(domain.ConflictCrossCoop, plot, boundaryID, cross, now))
	}
	return out
}

// newConflict dựng xung đột với người phụ trách và SLA (C-8, tạm 72 giờ).
func newConflict(kind string, plot store.Plot, boundaryID uuid.UUID, others []domain.Overlap, now time.Time) store.Conflict {
	c := store.Conflict{
		ID: ids.New(), Kind: kind, PlotRef: plot.Ref, BoundaryID: boundaryID, OtherPlotRefs: refsOf(others),
		CommuneCode: plot.CommuneCode, OwnerRef: plot.CooperativeRef, DueAt: now.Add(domain.ConflictSLA), Status: "open", DetectedAt: now,
	}
	if len(others) > 0 {
		m := domain.MaxPct(others)
		c.MaxOverlapPct = &m
	}
	if kind == domain.ConflictCrossCoop {
		c.OwnerRef = uuid.Nil // chồng lấn giữa HTX: cán bộ KN của xã phân xử
	}
	return c
}

// replayBoundary đọc lại kết quả của lần ghi trước cùng idem_key.
func (s *Service) replayBoundary(ctx context.Context, tx pgx.Tx, key string) (*WriteResult, error) {
	b, err := store.BoundaryByIdemKey(ctx, tx, key)
	if err != nil || b == nil {
		return nil, err
	}
	p, err := store.PlotByRef(ctx, tx, b.PlotRef, false)
	if err != nil || p == nil {
		return nil, err
	}
	cs, err := store.ListConflicts(ctx, tx, store.ConflictFilter{All: true, PlotRef: p.Ref, Limit: 50})
	if err != nil {
		return nil, err
	}
	res := &WriteResult{Plot: *p, Boundary: *b, Replayed: true}
	for _, c := range cs {
		if c.BoundaryID == b.ID {
			res.Conflicts = append(res.Conflicts, c)
		}
	}
	return res, nil
}

// writeBoundary chạy một lần ghi có chống lặp theo idem_key; hai request cùng khóa
// chạy song song: bên thua đọc lại bản ghi của bên thắng.
func (s *Service) writeBoundary(ctx context.Context, tid uuid.UUID, key string, fn func(pgx.Tx) (WriteResult, error)) (WriteResult, error) {
	var res WriteResult
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		if prev, err := s.replayBoundary(ctx, tx, key); err != nil {
			return err
		} else if prev != nil {
			res = *prev
			return nil
		}
		var err error
		res, err = fn(tx)
		return err
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *WriteResult
		if rerr := s.inTx(ctx, tid, func(tx pgx.Tx) error {
			var e error
			prev, e = s.replayBoundary(ctx, tx, key)
			return e
		}); rerr == nil && prev != nil {
			return *prev, nil
		}
	}
	return res, err
}

// CreatePlot tạo thửa với ranh giới đầu tiên (KN US-PLOT-01, HTX US-PLOT-02/03).
func (s *Service) CreatePlot(ctx context.Context, in CreateInput) (WriteResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return WriteResult{}, err
	}
	return s.create(ctx, a, in, modeOnline, domain.SourceKNWeb)
}

func (s *Service) create(ctx context.Context, a access, in CreateInput, m mode, defaultSource string) (WriteResult, error) {
	if in.IdemKey == "" {
		return WriteResult{}, problem.IdempotencyRequired
	}
	in.CommuneCode, in.Name = strings.TrimSpace(in.CommuneCode), strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) > 200 {
		return WriteResult{}, problem.Validation.WithField("name", "tối đa 200 ký tự")
	}
	if a.coopOnly && in.CooperativeRef == uuid.Nil {
		in.CooperativeRef = a.p.OrgRef // quản lý HTX vẽ thửa cho chính HTX mình
	}
	if in.HouseholdRef != uuid.Nil && in.CooperativeRef == uuid.Nil {
		return WriteResult{}, problem.Validation.WithField("household_ref", "phải gán HTX trước khi gán nông hộ")
	}
	if in.PlotRef != uuid.Nil && in.PlotRef.Version() != 7 {
		return WriteResult{}, problem.Validation.WithField("plot_ref", "phải là UUIDv7")
	}
	if err := s.prepare(&in.Boundary, defaultSource); err != nil {
		return WriteResult{}, err
	}
	if err := s.checkCommune(ctx, in.CommuneCode); err != nil {
		return WriteResult{}, err
	}
	if err := a.canWrite(in.CommuneCode, in.CooperativeRef); err != nil {
		return WriteResult{}, err
	}
	if err := s.checkCooperative(ctx, a, in.CooperativeRef); err != nil {
		return WriteResult{}, err
	}
	if err := s.checkHousehold(ctx, in.HouseholdRef, in.CooperativeRef); err != nil {
		return WriteResult{}, err
	}
	tol, maxDev, err := s.thresholds(ctx, in.Boundary.EventTime)
	if err != nil {
		return WriteResult{}, err
	}
	now := s.now()
	return s.writeBoundary(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (WriteResult, error) {
		ref := in.PlotRef
		if ref == uuid.Nil {
			ref = ids.New()
		} else if exist, err := store.PlotByRef(ctx, tx, ref, false); err != nil {
			return WriteResult{}, err
		} else if exist != nil {
			return WriteResult{}, ErrPlotExists
		}
		c, err := checkShape(ctx, tx, in.Boundary.Parsed, in.CommuneCode, in.CooperativeRef, ref, tol)
		if err != nil {
			return WriteResult{}, err
		}
		c.deviation(in.Boundary.DeclaredAreaHa)
		if err := gate(ctx, tx, m, in.KN, c, maxDev, in.Boundary.Confirm); err != nil {
			return WriteResult{}, err
		}
		code, err := store.NextPlotCode(ctx, tx)
		if err != nil {
			return WriteResult{}, err
		}
		b := store.Boundary{
			ID: in.Boundary.ID, TenantID: a.p.TenantID, PlotRef: ref, PointCount: in.Boundary.Points,
			GPSAccuracyM: in.Boundary.GPSAccuracyM, Source: in.Boundary.Source, CommuneCode: in.CommuneCode,
			CooperativeRef: in.CooperativeRef, BecameCurrent: true, EventTime: in.Boundary.EventTime, ActorRef: a.p.ActorRef,
			DeviceID: in.Boundary.DeviceID, IdemKey: in.IdemKey,
		}
		in.KN.boundary(&b, in.Boundary.DeclaredAreaHa)
		if err := store.InsertBoundary(ctx, tx, &b, c.geojson); err != nil {
			return WriteResult{}, err
		}
		flagged := append(append([]domain.Overlap{}, c.same...), c.cross...)
		plot := store.Plot{
			Ref: ref, Code: code, Name: in.Name, CommuneCode: in.CommuneCode, CooperativeRef: in.CooperativeRef,
			HouseholdRef: in.HouseholdRef, Status: domain.StatusFor(in.CooperativeRef), CurrentVersion: b.Version,
			CurrentBoundaryID: b.ID, AreaHa: b.AreaHa, Source: b.Source, NeedsReview: len(flagged) > 0, CreatedBy: a.p.ActorRef,
		}
		if in.KN != nil {
			plot.KNAttrs = in.KN.Attrs
			plot.DeclaredAreaHa = in.Boundary.DeclaredAreaHa
		}
		ev := store.PlotEvent{
			ID: ids.New(), TenantID: a.p.TenantID, PlotRef: ref, Action: domain.ActionCreated, Code: code, Name: in.Name,
			CommuneCode: in.CommuneCode, CooperativeRef: in.CooperativeRef, HouseholdRef: in.HouseholdRef, Status: plot.Status,
			EventTime: in.Boundary.EventTime, ActorRef: a.p.ActorRef, DeviceID: in.Boundary.DeviceID, IdemKey: in.IdemKey,
			KNAttrs: plot.KNAttrs,
		}
		in.KN.event(&ev)
		if err := store.InsertPlotEvent(ctx, tx, &ev); err != nil {
			return WriteResult{}, err
		}
		if err := store.InsertPlot(ctx, tx, a.p.TenantID, plot, c.geojson); err != nil {
			return WriteResult{}, err
		}
		res := WriteResult{Boundary: b, Flagged: flagged, Conflicts: conflictsFor(plot, b.ID, c.same, c.cross, now)}
		for _, cf := range res.Conflicts {
			if err := store.InsertConflict(ctx, tx, a.p.TenantID, cf); err != nil {
				return WriteResult{}, err
			}
		}
		if err := enqueueCreated(ctx, tx, plot, b, a.p.ActorRef); err != nil {
			return WriteResult{}, err
		}
		if err := enqueueBoundary(ctx, tx, b, in.Boundary.DeclaredAreaHa, c.devPct); err != nil {
			return WriteResult{}, err
		}
		cur, err := store.PlotByRef(ctx, tx, ref, false)
		if err != nil {
			return WriteResult{}, err
		}
		res.Plot = *cur
		return res, nil
	})
}

// CorrectBoundary ghi phiên bản ranh giới mới (bản ghi bù): phiên bản cũ giữ
// nguyên trong sổ, phiên bản mới trỏ supersedes_id về nó (HTX US-PLOT-01 AC-2).
func (s *Service) CorrectBoundary(ctx context.Context, in CorrectInput) (WriteResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return WriteResult{}, err
	}
	return s.correct(ctx, a, in, modeOnline, domain.SourceKNWeb)
}

func (s *Service) correct(ctx context.Context, a access, in CorrectInput, m mode, defaultSource string) (WriteResult, error) {
	if in.IdemKey == "" {
		return WriteResult{}, problem.IdempotencyRequired
	}
	if in.SupersedesID == uuid.Nil {
		return WriteResult{}, problem.Validation.WithField("supersedes_id", "bắt buộc: phiên bản ranh giới đang được thay")
	}
	if !domain.ValidReason(in.Reason) {
		return WriteResult{}, problem.Validation.WithField("correction_reason", "bắt buộc, ít nhất 10 ký tự")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if err := s.prepare(&in.Boundary, defaultSource); err != nil {
		return WriteResult{}, err
	}
	tol, maxDev, err := s.thresholds(ctx, in.Boundary.EventTime)
	if err != nil {
		return WriteResult{}, err
	}
	now := s.now()
	return s.writeBoundary(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (WriteResult, error) {
		plot, err := store.PlotByRef(ctx, tx, in.PlotRef, true)
		if err != nil {
			return WriteResult{}, err
		}
		if plot == nil || !a.sees(plot.CommuneCode, plot.CooperativeRef) {
			return WriteResult{}, ErrPlotNotFound
		}
		if err := a.canWrite(plot.CommuneCode, plot.CooperativeRef); err != nil {
			return WriteResult{}, err
		}
		if plot.Status == port.StatusRetired {
			return WriteResult{}, ErrPlotState.WithDetail("thửa đã ngừng dùng")
		}
		stale := in.SupersedesID != plot.CurrentBoundaryID
		if stale {
			if m == modeOnline {
				return WriteResult{}, ErrVersionConflict.With("current_boundary_id", plot.CurrentBoundaryID.String()).
					With("current_version", plot.CurrentVersion)
			}
			old, err := store.BoundaryByID(ctx, tx, in.SupersedesID)
			if err != nil {
				return WriteResult{}, err
			}
			if old == nil || old.PlotRef != plot.Ref {
				return WriteResult{}, problem.Validation.WithField("supersedes_id", "không phải phiên bản ranh giới của thửa này")
			}
		}
		c, err := checkShape(ctx, tx, in.Boundary.Parsed, plot.CommuneCode, plot.CooperativeRef, plot.Ref, tol)
		if err != nil {
			return WriteResult{}, err
		}
		c.deviation(in.Boundary.DeclaredAreaHa)
		if err := gate(ctx, tx, m, in.KN, c, maxDev, in.Boundary.Confirm); err != nil {
			return WriteResult{}, err
		}
		sup, reason := in.SupersedesID, in.Reason
		b := store.Boundary{
			ID: in.Boundary.ID, TenantID: a.p.TenantID, PlotRef: plot.Ref, PointCount: in.Boundary.Points,
			GPSAccuracyM: in.Boundary.GPSAccuracyM, Source: in.Boundary.Source, CommuneCode: plot.CommuneCode,
			CooperativeRef: plot.CooperativeRef, BecameCurrent: !stale, EventTime: in.Boundary.EventTime,
			SupersedesID: &sup, CorrectionReason: &reason, ActorRef: a.p.ActorRef, DeviceID: in.Boundary.DeviceID, IdemKey: in.IdemKey,
		}
		in.KN.boundary(&b, in.Boundary.DeclaredAreaHa)
		if err := store.InsertBoundary(ctx, tx, &b, c.geojson); err != nil {
			return WriteResult{}, err
		}
		res := WriteResult{Boundary: b}
		var conflicts []store.Conflict
		if stale {
			conflicts = append(conflicts, newConflict(domain.ConflictConcurrent, *plot, b.ID, nil, now))
		} else {
			res.Flagged = append(append(res.Flagged, c.same...), c.cross...)
			conflicts = conflictsFor(*plot, b.ID, c.same, c.cross, now)
			if err := store.SetCurrentBoundary(ctx, tx, plot.Ref, b, c.geojson, len(conflicts) > 0); err != nil {
				return WriteResult{}, err
			}
			if in.KN != nil {
				if err := store.SetShape(ctx, tx, plot.Ref, b.Shape, b.DeclaredAreaHa); err != nil {
					return WriteResult{}, err
				}
			}
		}
		for _, cf := range conflicts {
			if err := store.InsertConflict(ctx, tx, a.p.TenantID, cf); err != nil {
				return WriteResult{}, err
			}
		}
		if stale {
			if err := store.SetNeedsReview(ctx, tx, plot.Ref, true); err != nil {
				return WriteResult{}, err
			}
		}
		res.Conflicts = conflicts
		if err := enqueueBoundary(ctx, tx, b, in.Boundary.DeclaredAreaHa, c.devPct); err != nil {
			return WriteResult{}, err
		}
		cur, err := store.PlotByRef(ctx, tx, plot.Ref, false)
		if err != nil {
			return WriteResult{}, err
		}
		res.Plot = *cur
		return res, nil
	})
}

// AssignInput gán thửa cho HTX / nông hộ (KN vẽ trước, HTX nhận sau — ADR-0009).
type AssignInput struct {
	PlotRef        uuid.UUID
	CooperativeRef uuid.UUID // Nil = bỏ gán (thửa về nháp)
	HouseholdRef   uuid.UUID
	Reason         string
	// ConfirmOverlap: xác nhận giữ nguyên khi thửa chồng lấn thửa cùng HTX mới/thửa nháp.
	ConfirmOverlap bool
	IdemKey        string
}

// AttrResult là kết quả gán/ngừng dùng.
type AttrResult struct {
	Plot      store.Plot
	Event     store.PlotEvent
	Conflicts []store.Conflict // chồng lấn đã xác nhận khi gán HTX mới
	Replayed  bool
}

func (s *Service) writeAttrs(ctx context.Context, tid uuid.UUID, key string, fn func(pgx.Tx) (AttrResult, error)) (AttrResult, error) {
	replay := func(tx pgx.Tx) (*AttrResult, error) {
		ev, err := store.PlotEventByIdemKey(ctx, tx, key)
		if err != nil || ev == nil {
			return nil, err
		}
		p, err := store.PlotByRef(ctx, tx, ev.PlotRef, false)
		if err != nil || p == nil {
			return nil, err
		}
		return &AttrResult{Plot: *p, Event: *ev, Replayed: true}, nil
	}
	var res AttrResult
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		if prev, err := replay(tx); err != nil {
			return err
		} else if prev != nil {
			res = *prev
			return nil
		}
		var err error
		res, err = fn(tx)
		return err
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *AttrResult
		if rerr := s.inTx(ctx, tid, func(tx pgx.Tx) error {
			var e error
			prev, e = replay(tx)
			return e
		}); rerr == nil && prev != nil {
			return *prev, nil
		}
	}
	return res, err
}

// Assign gán HTX/nông hộ cho thửa (HTX US-PLOT-01 AC-3: mỗi thửa đúng một HTX).
func (s *Service) Assign(ctx context.Context, in AssignInput) (AttrResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return AttrResult{}, err
	}
	if in.IdemKey == "" {
		return AttrResult{}, problem.IdempotencyRequired
	}
	if in.HouseholdRef != uuid.Nil && in.CooperativeRef == uuid.Nil {
		return AttrResult{}, problem.Validation.WithField("household_ref", "phải gán HTX trước khi gán nông hộ")
	}
	if a.coopOnly && in.CooperativeRef != a.p.OrgRef {
		return AttrResult{}, ErrOutOfScope.WithDetail("quản lý HTX chỉ gán thửa cho chính HTX của mình")
	}
	if err := s.checkCooperative(ctx, a, in.CooperativeRef); err != nil {
		return AttrResult{}, err
	}
	if err := s.checkHousehold(ctx, in.HouseholdRef, in.CooperativeRef); err != nil {
		return AttrResult{}, err
	}
	now := s.now()
	tol, err := s.tolerance(ctx, now)
	if err != nil {
		return AttrResult{}, err
	}
	return s.writeAttrs(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (AttrResult, error) {
		plot, err := store.PlotByRef(ctx, tx, in.PlotRef, true)
		if err != nil {
			return AttrResult{}, err
		}
		if plot == nil || !a.sees(plot.CommuneCode, plot.CooperativeRef) {
			return AttrResult{}, ErrPlotNotFound
		}
		if err := a.canWrite(plot.CommuneCode, plot.CooperativeRef); err != nil {
			return AttrResult{}, err
		}
		if plot.Status == port.StatusRetired {
			return AttrResult{}, ErrPlotState.WithDetail("thửa đã ngừng dùng")
		}
		if plot.CooperativeRef == in.CooperativeRef && plot.HouseholdRef == in.HouseholdRef {
			return AttrResult{}, ErrPlotState.WithDetail("thửa đã được gán như vậy")
		}
		p := *plot
		p.CooperativeRef, p.HouseholdRef, p.Status = in.CooperativeRef, in.HouseholdRef, domain.StatusFor(in.CooperativeRef)
		// Đổi HTX làm đổi phân loại chồng lấn: kiểm lại với HTX mới (khác HTX → chặn,
		// cùng HTX/thửa nháp → cần xác nhận). Bỏ gán về nháp không cần kiểm lại.
		var conflicts []store.Conflict
		if in.CooperativeRef != uuid.Nil && in.CooperativeRef != plot.CooperativeRef {
			same, cross, err := classifiedOverlaps(ctx, tx, plot.GeoJSON, plot.AreaHa, in.CooperativeRef, plot.Ref, tol)
			if err != nil {
				return AttrResult{}, err
			}
			if err := onlineGate(same, cross, nil, decimal.Zero, in.ConfirmOverlap); err != nil {
				return AttrResult{}, err
			}
			conflicts = conflictsFor(p, plot.CurrentBoundaryID, same, nil, now)
		}
		ev := store.PlotEvent{
			ID: ids.New(), TenantID: a.p.TenantID, PlotRef: p.Ref, Action: domain.ActionAssigned, Code: p.Code, Name: p.Name,
			CommuneCode: p.CommuneCode, CooperativeRef: p.CooperativeRef, HouseholdRef: p.HouseholdRef, Status: p.Status,
			Reason: strings.TrimSpace(in.Reason), EventTime: now, ActorRef: a.p.ActorRef, IdemKey: in.IdemKey, KNAttrs: p.KNAttrs,
		}
		if err := store.InsertPlotEvent(ctx, tx, &ev); err != nil {
			return AttrResult{}, err
		}
		if err := store.SetAttrs(ctx, tx, p); err != nil {
			return AttrResult{}, err
		}
		for _, cf := range conflicts {
			if err := store.InsertConflict(ctx, tx, a.p.TenantID, cf); err != nil {
				return AttrResult{}, err
			}
		}
		if len(conflicts) > 0 {
			if err := store.SetNeedsReview(ctx, tx, p.Ref, true); err != nil {
				return AttrResult{}, err
			}
		}
		cur, err := store.PlotByRef(ctx, tx, p.Ref, false)
		if err != nil {
			return AttrResult{}, err
		}
		return AttrResult{Plot: *cur, Event: ev, Conflicts: conflicts}, nil
	})
}

// RetireInput ngừng dùng thửa (xóa mềm, GIS BR-17): ẩn khỏi bản đồ, giữ lịch sử.
type RetireInput struct {
	PlotRef uuid.UUID
	Reason  string
	IdemKey string
}

// Retire ngừng dùng thửa kèm lý do.
func (s *Service) Retire(ctx context.Context, in RetireInput) (AttrResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return AttrResult{}, err
	}
	if in.IdemKey == "" {
		return AttrResult{}, problem.IdempotencyRequired
	}
	if !domain.ValidReason(in.Reason) {
		return AttrResult{}, problem.Validation.WithField("reason", "bắt buộc, ít nhất 10 ký tự")
	}
	now := s.now()
	return s.writeAttrs(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (AttrResult, error) {
		plot, err := store.PlotByRef(ctx, tx, in.PlotRef, true)
		if err != nil {
			return AttrResult{}, err
		}
		if plot == nil || !a.sees(plot.CommuneCode, plot.CooperativeRef) {
			return AttrResult{}, ErrPlotNotFound
		}
		if err := a.canWrite(plot.CommuneCode, plot.CooperativeRef); err != nil {
			return AttrResult{}, err
		}
		if plot.Status == port.StatusRetired {
			return AttrResult{}, ErrPlotState.WithDetail("thửa đã ngừng dùng")
		}
		p := *plot
		p.Status, p.StatusReason = port.StatusRetired, strings.TrimSpace(in.Reason)
		ev := store.PlotEvent{
			ID: ids.New(), TenantID: a.p.TenantID, PlotRef: p.Ref, Action: domain.ActionRetired, Code: p.Code, Name: p.Name,
			CommuneCode: p.CommuneCode, CooperativeRef: p.CooperativeRef, HouseholdRef: p.HouseholdRef, Status: p.Status,
			Reason: p.StatusReason, EventTime: now, ActorRef: a.p.ActorRef, IdemKey: in.IdemKey, KNAttrs: p.KNAttrs,
		}
		if err := store.InsertPlotEvent(ctx, tx, &ev); err != nil {
			return AttrResult{}, err
		}
		if err := store.SetAttrs(ctx, tx, p); err != nil {
			return AttrResult{}, err
		}
		// GIS US-EVL-01: cùng giao dịch với dòng sổ (outbox).
		if err := enqueue(ctx, tx, ev.TenantID, port.EventPlotRetired, ev.EventTime, port.PlotRetiredV1{
			PlotRef: p.Ref, PlotEventID: ev.ID, Code: p.Code, CommuneCode: p.CommuneCode, CooperativeRef: uuidPtr(p.CooperativeRef),
			StatusBefore: plot.Status, StatusAfter: p.Status, Reason: ev.Reason, ActorRef: ev.ActorRef,
			EventTime: ev.EventTime.Format(time.RFC3339),
		}); err != nil {
			return AttrResult{}, err
		}
		cur, err := store.PlotByRef(ctx, tx, p.Ref, false)
		if err != nil {
			return AttrResult{}, err
		}
		return AttrResult{Plot: *cur, Event: ev}, nil
	})
}

// RestoreInput khôi phục thửa đã ngừng dùng (GIS BR-17: xóa mềm khôi phục được).
type RestoreInput struct {
	PlotRef uuid.UUID
	Reason  string
	// ConfirmOverlap: xác nhận giữ nguyên khi thửa chồng lấn thửa cùng HTX/thửa nháp vẽ sau lúc ngừng dùng.
	ConfirmOverlap bool
	IdemKey        string
}

// Restore đưa thửa đã ngừng dùng về trạng thái theo HTX (nháp/hoạt động) bằng
// sự kiện 'restored' kèm lý do. Trong thời gian ngừng dùng có thể đã có thửa
// khác vẽ đè: kiểm lại chồng lấn như khi gán (khác HTX → chặn, cùng HTX → cần xác nhận).
func (s *Service) Restore(ctx context.Context, in RestoreInput) (AttrResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return AttrResult{}, err
	}
	if in.IdemKey == "" {
		return AttrResult{}, problem.IdempotencyRequired
	}
	if !domain.ValidReason(in.Reason) {
		return AttrResult{}, problem.Validation.WithField("reason", "bắt buộc, ít nhất 10 ký tự")
	}
	now := s.now()
	tol, err := s.tolerance(ctx, now)
	if err != nil {
		return AttrResult{}, err
	}
	return s.writeAttrs(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (AttrResult, error) {
		plot, err := store.PlotByRef(ctx, tx, in.PlotRef, true)
		if err != nil {
			return AttrResult{}, err
		}
		if plot == nil || !a.sees(plot.CommuneCode, plot.CooperativeRef) {
			return AttrResult{}, ErrPlotNotFound
		}
		if err := a.canWrite(plot.CommuneCode, plot.CooperativeRef); err != nil {
			return AttrResult{}, err
		}
		if plot.Status != port.StatusRetired {
			return AttrResult{}, ErrPlotState.WithDetail("thửa chưa ngừng dùng")
		}
		same, cross, err := classifiedOverlaps(ctx, tx, plot.GeoJSON, plot.AreaHa, plot.CooperativeRef, plot.Ref, tol)
		if err != nil {
			return AttrResult{}, err
		}
		if err := onlineGate(same, cross, nil, decimal.Zero, in.ConfirmOverlap); err != nil {
			return AttrResult{}, err
		}
		p := *plot
		p.Status, p.StatusReason = domain.StatusFor(p.CooperativeRef), ""
		conflicts := conflictsFor(p, plot.CurrentBoundaryID, same, nil, now)
		ev := store.PlotEvent{
			ID: ids.New(), TenantID: a.p.TenantID, PlotRef: p.Ref, Action: domain.ActionRestored, Code: p.Code, Name: p.Name,
			CommuneCode: p.CommuneCode, CooperativeRef: p.CooperativeRef, HouseholdRef: p.HouseholdRef, Status: p.Status,
			Reason: strings.TrimSpace(in.Reason), EventTime: now, ActorRef: a.p.ActorRef, IdemKey: in.IdemKey, KNAttrs: p.KNAttrs,
		}
		if err := store.InsertPlotEvent(ctx, tx, &ev); err != nil {
			return AttrResult{}, err
		}
		if err := store.SetAttrs(ctx, tx, p); err != nil {
			return AttrResult{}, err
		}
		for _, cf := range conflicts {
			if err := store.InsertConflict(ctx, tx, a.p.TenantID, cf); err != nil {
				return AttrResult{}, err
			}
		}
		if len(conflicts) > 0 {
			if err := store.SetNeedsReview(ctx, tx, p.Ref, true); err != nil {
				return AttrResult{}, err
			}
		}
		// GIS US-EVL-01: cùng giao dịch với dòng sổ (outbox).
		if err := enqueue(ctx, tx, ev.TenantID, port.EventPlotRestored, ev.EventTime, port.PlotRestoredV1{
			PlotRef: p.Ref, PlotEventID: ev.ID, Code: p.Code, CommuneCode: p.CommuneCode, CooperativeRef: uuidPtr(p.CooperativeRef),
			StatusBefore: plot.Status, StatusAfter: p.Status, Reason: ev.Reason, NeedsReview: len(conflicts) > 0,
			ActorRef: ev.ActorRef, EventTime: ev.EventTime.Format(time.RFC3339),
		}); err != nil {
			return AttrResult{}, err
		}
		cur, err := store.PlotByRef(ctx, tx, p.Ref, false)
		if err != nil {
			return AttrResult{}, err
		}
		return AttrResult{Plot: *cur, Event: ev, Conflicts: conflicts}, nil
	})
}

// ---------------------------------------------------------------------------
// Sự kiện
// ---------------------------------------------------------------------------

func uuidPtr(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func enqueue(ctx context.Context, tx pgx.Tx, tid uuid.UUID, typ string, at time.Time, payload any) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: typ, SchemaVersion: 1, OccurredAt: at,
		TraceID: telemetry.TraceID(ctx), Producer: "pf/geo", Payload: payload,
	})
}

func enqueueCreated(ctx context.Context, tx pgx.Tx, p store.Plot, b store.Boundary, actor uuid.UUID) error {
	return enqueue(ctx, tx, b.TenantID, port.EventPlotCreated, b.EventTime, port.PlotCreatedV1{
		PlotRef: p.Ref, Code: p.Code, CommuneCode: p.CommuneCode, CooperativeRef: uuidPtr(p.CooperativeRef),
		HouseholdRef: uuidPtr(p.HouseholdRef), Status: p.Status, BoundaryID: b.ID, AreaHa: b.AreaHa.StringFixed(4),
		Source: b.Source, ActorRef: actor, EventTime: b.EventTime.Format(time.RFC3339),
	})
}

func decStr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.StringFixed(4)
	return &s
}

func enqueueBoundary(ctx context.Context, tx pgx.Tx, b store.Boundary, declared, devPct *decimal.Decimal) error {
	return enqueue(ctx, tx, b.TenantID, port.EventPlotBoundaryRecorded, b.EventTime, port.PlotBoundaryRecordedV1{
		PlotRef: b.PlotRef, BoundaryID: b.ID, Version: b.Version, BecameCurrent: b.BecameCurrent, AreaHa: b.AreaHa.StringFixed(4),
		PointCount: b.PointCount, Source: b.Source, CommuneCode: b.CommuneCode, CooperativeRef: uuidPtr(b.CooperativeRef),
		SupersedesID: b.SupersedesID, CorrectionReason: b.CorrectionReason, ActorRef: b.ActorRef,
		DeclaredAreaHa: decStr(declared), AreaDeviationPct: decStr(devPct),
		EventTime: b.EventTime.Format(time.RFC3339),
	})
}
