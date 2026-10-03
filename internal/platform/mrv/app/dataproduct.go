package app

// Sản phẩm dữ liệu mrv_package_status v1 cho đơn vị thẩm định (G5, ADR-0022; g5-conventions §6;
// KN US-MRV-05 — chuyên gia thẩm định chỉ xem, mọi lần truy cập có nhật ký ở api-public).
//
// pf/apiclient đã kiểm cấp quyền dp_mrv_package_status TRƯỚC khi gọi; ở đây đọc job xuất của tenant
// bên cấp (pool vai mrv_app, tenant.WithTenantTx — R-05, không BYPASSRLS) và chỉ điền trường của DTO
// MRVPackageStatusV1: trạng thái, mốc, băm gói, băm ảnh chụp, số bằng chứng tổng hợp, kiểm lại.
// Không ảnh, tọa độ, mã nông hộ/thửa, người thu, người duyệt, người yêu cầu, khóa đối tượng, URL.
//
//   - catalog_version = phiên bản quy tắc gói của module (domain.RuleVersion): job không ghim một
//     phiên bản danh mục duy nhất — danh mục cụ thể theo từng loại minh chứng nằm trong gói
//     (snapshot.catalogs) mà đơn vị thẩm định nhận khi bật tải gói (chờ C-1).
//   - season_code / province_code: từ bộ lọc bất biến của job (tỉnh chỉ khi job lọc đúng một mã tỉnh
//     2 chữ số); khác → null.
//   - last_verify_match: Package (một gói) dựng lại ảnh chụp tại as_of từ sổ và so băm như
//     VerifyExport (kiểm lại mỗi lần đọc — không lưu); Packages (danh sách) → null để không dựng lại
//     cả trang.

import (
	"context"
	"encoding/base64"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ apiclientport.MRVPackageSource = (*Service)(nil)

// Giới hạn trang của mrv_package_status.
const (
	DefaultPackageLimit = 50
	MaxPackageLimit     = 200
)

var (
	productSeasonRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)
	productProvinceRe = regexp.MustCompile(`^[0-9]{2}$`)
)

// packageProduct dựng phần tử mrv_package_status v1 — CHỈ trường của hợp đồng.
func packageProduct(j domain.ExportJob, verify *bool) apiclientport.MRVPackageStatusV1 {
	out := apiclientport.MRVPackageStatusV1{
		ExportID: j.ID.String(), Status: string(j.Status), RequestedAt: domain.FormatTime(j.RequestedAt),
		CatalogVersion: domain.RuleVersion, PackageSHA256: j.PackageSHA256, SnapshotSHA256: j.SnapshotSHA256, LastVerifyMatch: verify,
	}
	if j.CompletedAt != nil {
		c := domain.FormatTime(*j.CompletedAt)
		out.CompletedAt = &c
	}
	if j.EvidenceCount != nil {
		out.EvidenceCount = int64(*j.EvidenceCount)
	}
	if sc := j.Filters.SeasonCode; sc != "" && productSeasonRe.MatchString(sc) {
		out.SeasonCode = &sc
	}
	if u := j.Filters.AdminUnitCodes; !j.Filters.AllUnits && len(u) == 1 && productProvinceRe.MatchString(u[0]) {
		p := u[0]
		out.ProvinceCode = &p
	}
	return out
}

// Packages cài đặt apiclientport.MRVPackageSource.
func (s *Service) Packages(ctx context.Context, dataTenant uuid.UUID, status, after string, limit int) ([]apiclientport.MRVPackageStatusV1, string, error) {
	if dataTenant == uuid.Nil {
		return nil, "", tenant.ErrMissing
	}
	switch domain.ExportStatus(status) {
	case "", domain.ExportQueued, domain.ExportCompleted, domain.ExportFailed:
	default:
		return nil, "", problem.Validation.WithField("status", "queued | completed | failed")
	}
	cur := uuid.Nil
	if after != "" {
		b, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil || len(b) != 16 {
			return nil, "", problem.Validation.WithField("cursor", "con trỏ không hợp lệ")
		}
		cur = uuid.UUID(b)
	}
	if limit <= 0 {
		limit = DefaultPackageLimit
	}
	limit = min(limit, MaxPackageLimit)
	var jobs []domain.ExportJob
	if err := s.tx(ctx, dataTenant, func(tx pgx.Tx) error {
		var e error
		jobs, e = store.ProductJobs(ctx, tx, status, cur, limit+1)
		return e
	}); err != nil {
		return nil, "", err
	}
	next := ""
	if len(jobs) > limit {
		jobs = jobs[:limit]
		id := jobs[limit-1].ID
		next = base64.RawURLEncoding.EncodeToString(id[:])
	}
	out := make([]apiclientport.MRVPackageStatusV1, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, packageProduct(j, nil))
	}
	return out, next, nil
}

// Package cài đặt apiclientport.MRVPackageSource: không có (hoặc thuộc tenant khác — RLS) → 404
// mrv.export_not_found. Gói đã xong được kiểm lại (dựng ảnh chụp, so băm).
func (s *Service) Package(ctx context.Context, dataTenant, exportID uuid.UUID) (apiclientport.MRVPackageStatusV1, error) {
	if dataTenant == uuid.Nil {
		return apiclientport.MRVPackageStatusV1{}, tenant.ErrMissing
	}
	var out apiclientport.MRVPackageStatusV1
	err := s.tx(ctx, dataTenant, func(tx pgx.Tx) error {
		j, err := store.JobByID(ctx, tx, exportID, false)
		if err != nil {
			return err
		}
		if j == nil {
			return ErrExportNotFound
		}
		var verify *bool
		if j.Status == domain.ExportCompleted && j.SnapshotSHA256 != nil {
			snap, err := buildSnapshot(ctx, tx, *j)
			if err != nil {
				return err
			}
			_, sum, err := snap.Canonical()
			if err != nil {
				return err
			}
			m := sum == *j.SnapshotSHA256
			verify = &m
		}
		out = packageProduct(*j, verify)
		return nil
	})
	return out, err
}
