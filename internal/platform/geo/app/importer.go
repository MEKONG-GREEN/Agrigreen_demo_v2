package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Trạng thái kết quả từng đối tượng nhập.
const (
	FeatureAccepted  = "accepted"  // đã tạo thửa
	FeatureFlagged   = "flagged"   // đã tạo, chồng lấn cùng HTX/thửa nháp đã xác nhận — chờ xem xét
	FeatureDuplicate = "duplicate" // lần nhập lặp cùng Idempotency-Key
	FeatureRejected  = "rejected"
)

// FeatureResult là kết quả một đối tượng. Reason không chứa dữ liệu cá nhân.
type FeatureResult struct {
	Index    int
	Name     string
	Status   string
	Code     string // mã lỗi khi rejected
	Reason   string
	PlotRef  uuid.UUID
	PlotCode string
	AreaHa   *decimal.Decimal
	Overlaps []domain.Overlap
}

// ImportResult là kết quả cả tệp.
type ImportResult struct {
	Format                                 string
	Accepted, Flagged, Duplicate, Rejected int
	Features                               []FeatureResult
}

// ImportInput là tệp ranh giới thửa (KN US-PLOT-02, GIS FN-05).
type ImportInput struct {
	Format         string // geojson | kml | shapefile; rỗng = tự nhận
	FileName       string
	Data           []byte
	CommuneCode    string    // mặc định khi đối tượng không có thuộc tính commune_code
	CooperativeRef uuid.UUID // mặc định khi đối tượng không có cooperative_ref
	Source         string    // import_file (mặc định) | cadastral
	// ConfirmOverlap: nhận cả đối tượng chồng lấn cùng HTX/thửa nháp (mở xung đột xem xét).
	ConfirmOverlap bool
	IdemKey        string
}

func propUUID(p map[string]string, keys ...string) (uuid.UUID, bool, error) {
	v := firstOf(p, keys...)
	if v == "" {
		return uuid.Nil, false, nil
	}
	u, err := uuid.Parse(v)
	return u, true, err
}

func firstOf(p map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(p[k]); v != "" {
			return v
		}
	}
	return ""
}

func rejectReason(err error) (string, string) {
	var pr *problem.Problem
	if !errors.As(err, &pr) {
		return problem.Internal.Code, "lỗi hệ thống"
	}
	switch {
	case len(pr.Errors) > 0:
		return pr.Code, pr.Errors[0].Field + ": " + pr.Errors[0].Reason
	case pr.Detail != "":
		return pr.Code, pr.Detail
	}
	return pr.Code, pr.Title
}

// ImportPlots nhập tệp GeoJSON/KML/Shapefile (.zip): mỗi đối tượng vùng thành một thửa, cùng quy
// tắc như vẽ tay (≥ 3 điểm, hợp lệ, trong xã, chồng lấn). Đối tượng lỗi bị loại
// kèm lý do; đối tượng hợp lệ vẫn được nạp. Lỗi hệ thống dừng cả lượt (client
// thử lại được nhờ idem_key theo từng đối tượng).
func (s *Service) ImportPlots(ctx context.Context, in ImportInput) (ImportResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return ImportResult{}, err
	}
	if in.IdemKey == "" {
		return ImportResult{}, problem.IdempotencyRequired
	}
	if in.Source == "" {
		in.Source = domain.SourceImportFile
	}
	if in.Source != domain.SourceImportFile && in.Source != domain.SourceCadastral {
		return ImportResult{}, problem.Validation.WithField("source", "phải là import_file hoặc cadastral")
	}
	format := domain.DetectFormat(in.Format, in.FileName, in.Data)
	feats, err := domain.ParseFeatures(format, in.Data)
	if err != nil {
		return ImportResult{}, violationProblem(err)
	}
	res := ImportResult{Format: format}
	coops := map[string]uuid.UUID{} // Mã HTX → ref (tra một lần mỗi mã)
	for _, f := range feats {
		r := FeatureResult{Index: f.Index, Name: f.Name}
		if f.Err != nil {
			r.Status = FeatureRejected
			r.Code, r.Reason = rejectReason(violationProblem(f.Err))
			res.add(r)
			continue
		}
		cin := CreateInput{
			Name:           f.Name,
			CommuneCode:    firstOf(f.Props, "commune_code", "ma_xa"),
			CooperativeRef: in.CooperativeRef,
			Boundary:       BoundaryInput{Parsed: f.Geom, Points: f.Points, Source: in.Source, Confirm: in.ConfirmOverlap},
			IdemKey:        in.IdemKey + ":" + strconv.Itoa(f.Index),
		}
		if cin.CommuneCode == "" {
			cin.CommuneCode = in.CommuneCode
		}
		var bad error
		if u, ok, err := propUUID(f.Props, "cooperative_ref"); err != nil {
			bad = problem.Validation.WithField("cooperative_ref", "phải là UUID")
		} else if ok {
			cin.CooperativeRef = u
		} else if code := firstOf(f.Props, "cooperative_code", "ma_htx"); code != "" {
			ref, err := s.coopByCode(ctx, code, coops)
			if err != nil {
				var pr *problem.Problem
				if !errors.As(err, &pr) {
					return ImportResult{}, err
				}
				bad = err
			}
			cin.CooperativeRef = ref
		}
		if u, ok, err := propUUID(f.Props, "household_ref"); err != nil {
			bad = problem.Validation.WithField("household_ref", "phải là UUID")
		} else if ok {
			cin.HouseholdRef = u
		}
		var out WriteResult
		if bad == nil {
			out, bad = s.create(ctx, a, cin, modeOnline, in.Source)
		}
		if bad != nil {
			var pr *problem.Problem
			if !errors.As(bad, &pr) || pr.Status >= 500 {
				return ImportResult{}, bad
			}
			r.Status = FeatureRejected
			r.Code, r.Reason = rejectReason(bad) // chồng lấn: Reason nêu mã các thửa bị đè
			res.add(r)
			continue
		}
		r.PlotRef, r.PlotCode, r.Overlaps = out.Plot.Ref, out.Plot.Code, out.Flagged
		area := out.Plot.AreaHa
		r.AreaHa = &area
		switch {
		case out.Replayed:
			r.Status = FeatureDuplicate
		case len(out.Flagged) > 0:
			r.Status = FeatureFlagged
		default:
			r.Status = FeatureAccepted
		}
		res.add(r)
	}
	return res, nil
}

// coopByCode tra HTX theo Mã HTX (thuộc tính cooperative_code/ma_htx của đối
// tượng, vd trường MA_HTX của .dbf) qua party (port.OrgCodes).
func (s *Service) coopByCode(ctx context.Context, code string, cache map[string]uuid.UUID) (uuid.UUID, error) {
	if ref, ok := cache[code]; ok {
		if ref == uuid.Nil {
			return uuid.Nil, ErrCooperativeInvalid.WithDetail("Không có HTX mã " + code)
		}
		return ref, nil
	}
	oc, ok := s.Orgs.(partyport.OrgCodes)
	if s.Orgs == nil || !ok {
		return uuid.Nil, ErrDependency.WithDetail("chưa nối tra cứu Mã HTX (pf/party)")
	}
	o, err := oc.ByCode(ctx, code)
	switch {
	case errors.Is(err, partyport.ErrOrgNotFound) || (err == nil && o.Kind != partyport.OrgCooperative):
		cache[code] = uuid.Nil
		return uuid.Nil, ErrCooperativeInvalid.WithDetail("Không có HTX mã " + code)
	case err != nil:
		return uuid.Nil, ErrDependency.WithDetail("không tra được Mã HTX").WithCause(err)
	}
	cache[code] = o.Ref
	return o.Ref, nil
}

func (r *ImportResult) add(f FeatureResult) {
	switch f.Status {
	case FeatureAccepted:
		r.Accepted++
	case FeatureFlagged:
		r.Flagged++
	case FeatureDuplicate:
		r.Duplicate++
	default:
		r.Rejected++
	}
	r.Features = append(r.Features, f)
}

// ---------------------------------------------------------------------------
// Ranh giới hành chính (GIS FN-07 BR-26/27)
// ---------------------------------------------------------------------------

// AdminImportInput là tệp GeoJSON ranh giới tỉnh/xã.
type AdminImportInput struct {
	Data   []byte
	Source string // vd "CloudMap 2026-09"; bắt buộc
	DryRun bool   // BR-27: kiểm và xem trước, chưa áp dụng
}

// AdminFeatureResult là kết quả một đơn vị.
type AdminFeatureResult struct {
	Index    int
	UnitCode string
	Name     string
	Level    string
	Status   string // accepted | rejected
	Replaced bool   // đã có hình trước đó (được đóng, không xóa)
	Code     string
	Reason   string
	AreaHa   *decimal.Decimal
}

// AdminImportResult là kết quả cả tệp.
type AdminImportResult struct {
	DryRun             bool
	Accepted, Rejected int
	Features           []AdminFeatureResult
}

// ImportAdminBoundaries nhập hình ranh giới tỉnh/xã. Mỗi đối tượng phải có thuộc
// tính code (mã đơn vị còn hiệu lực trong masterdata). DryRun: chỉ kiểm, không ghi.
func (s *Service) ImportAdminBoundaries(ctx context.Context, in AdminImportInput) (AdminImportResult, error) {
	a, err := s.access(ctx, adminRoles...)
	if err != nil {
		return AdminImportResult{}, err
	}
	in.Source = strings.TrimSpace(in.Source)
	if in.Source == "" || len([]rune(in.Source)) > 300 {
		return AdminImportResult{}, problem.Validation.WithField("source", "bắt buộc (nguồn dữ liệu, vd CloudMap), tối đa 300 ký tự")
	}
	if s.Units == nil {
		return AdminImportResult{}, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	feats, err := domain.ParseFeatures(domain.FormatGeoJSON, in.Data)
	if err != nil {
		return AdminImportResult{}, violationProblem(err)
	}
	res := AdminImportResult{DryRun: in.DryRun}
	now := s.now()
	for _, f := range feats {
		r := AdminFeatureResult{Index: f.Index, UnitCode: firstOf(f.Props, "code", "unit_code", "ma")}
		fail := func(err error) {
			r.Status = FeatureRejected
			r.Code, r.Reason = rejectReason(err)
			res.Rejected++
			res.Features = append(res.Features, r)
		}
		if f.Err != nil {
			fail(violationProblem(f.Err))
			continue
		}
		if r.UnitCode == "" {
			fail(problem.Validation.WithField("properties.code", "bắt buộc: mã đơn vị hành chính"))
			continue
		}
		u, err := s.Units.Get(ctx, r.UnitCode, now)
		if errors.Is(err, mdport.ErrUnitNotFound) {
			fail(problem.Validation.WithField("properties.code", "không có đơn vị hành chính còn hiệu lực với mã này"))
			continue
		}
		if err != nil {
			return AdminImportResult{}, ErrDependency.WithDetail("không tra được đơn vị hành chính").WithCause(err)
		}
		if u.Level != mdport.LevelProvince && u.Level != mdport.LevelCommune {
			fail(problem.Validation.WithField("properties.code", "chỉ nhận ranh giới tỉnh hoặc xã"))
			continue
		}
		if !a.sc.Allows(u.Code) {
			fail(ErrOutOfScope)
			continue
		}
		r.Name, r.Level = u.Name, string(u.Level)
		if n := firstOf(f.Props, "name", "ten"); n != "" {
			r.Name = n
		}
		gj := f.Geom.GeoJSON()
		err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
			an, err := store.Analyze(ctx, tx, gj)
			if err != nil {
				return err
			}
			if !an.Valid {
				return ErrInvalidGeometry.WithDetail("Ranh giới hành chính không hợp lệ (" + an.Reason + ")")
			}
			r.AreaHa = &an.AreaHa
			if in.DryRun {
				cur, err := store.AdminBoundaryByCode(ctx, tx, u.Code, 0)
				r.Replaced = cur != nil
				return err
			}
			r.Replaced, err = store.ReplaceAdminBoundary(ctx, tx, a.p.TenantID, store.AdminBoundary{
				ID: ids.New(), UnitCode: u.Code, Level: string(u.Level), Name: r.Name, Source: in.Source, ImportedBy: a.p.ActorRef,
			}, gj)
			return err
		})
		if err != nil {
			var pr *problem.Problem
			if !errors.As(err, &pr) {
				return AdminImportResult{}, err
			}
			fail(err)
			continue
		}
		r.Status = FeatureAccepted
		res.Accepted++
		res.Features = append(res.Features, r)
	}
	return res, nil
}

// GetAdminBoundary đọc hình ranh giới hiện hành của một đơn vị (dùng chung, BR-21).
func (s *Service) GetAdminBoundary(ctx context.Context, code string, zoom int) (store.AdminBoundary, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return store.AdminBoundary{}, err
	}
	var b *store.AdminBoundary
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		b, e = store.AdminBoundaryByCode(ctx, tx, code, SimplifyTolerance(zoom))
		return e
	})
	if err != nil {
		return store.AdminBoundary{}, err
	}
	if b == nil {
		return store.AdminBoundary{}, ErrAdminBoundaryNotFound
	}
	return *b, nil
}
