package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/domain"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
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
