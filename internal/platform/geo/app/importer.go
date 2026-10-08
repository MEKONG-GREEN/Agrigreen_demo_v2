package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
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
		if errors.Is(err, partyport.ErrUnitNotFound) {
			fail(problem.Validation.WithField("properties.code", "không có đơn vị hành chính còn hiệu lực với mã này"))
			continue
		}
		if err != nil {
			return AdminImportResult{}, ErrDependency.WithDetail("không tra được đơn vị hành chính").WithCause(err)
		}
		if u.Level != partyport.LevelProvince && u.Level != partyport.LevelCommune {
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
