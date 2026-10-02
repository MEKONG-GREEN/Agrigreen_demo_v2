package app

// Cổng của pf/reports cho P2 (KN v7.7, nối ở gốc lắp ráp — gói P4): menu "Báo cáo đột xuất" chỉ hiện khi phòng ban có
// mẫu của loại báo cáo (party/port.FormAvailability, FN-AD-04.06) và cột "biểu đang dùng" của Danh mục nông nghiệp
// (masterdata/port.FormUsage, FN-AD-04.05).

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// reportTypeOf ánh xạ mã danh mục report_type của pf/masterdata (tuan, thang, hang_vu, nam, dot_xuat, gui_chi_cuc)
// sang loại báo cáo của mẫu; mã đã là loại của mẫu thì giữ nguyên.
func reportTypeOf(code string) string {
	switch code {
	case "tuan":
		return port.ReportWeekly
	case "thang":
		return port.ReportMonthly
	case "hang_vu":
		return port.ReportSeasonal
	case "nam":
		return port.ReportAnnual
	case "dot_xuat":
		return port.ReportAdhoc
	case "gui_chi_cuc":
		return port.ReportDispatch
	}
	return code
}

// FormAvailability hiện thực party/port.FormAvailability.
type FormAvailability struct{ Svc *Service }

// HasForms cho biết phòng ban có mẫu ĐÃ PHÁT HÀNH đang gán cho loại báo cáo ở cấp nhập liệu (tenant từ Principal).
func (f FormAvailability) HasForms(ctx context.Context, department, level, reportType string) (bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return false, nil
	}
	fs, err := Forms(f).AssignedForms(ctx, p.TenantID, department, level, reportTypeOf(reportType), f.Svc.now())
	return len(fs) > 0, err
}

// FormUsage hiện thực masterdata/port.FormUsage: đếm mẫu (đang dùng) có dòng chỉ tiêu hoặc cột tham chiếu mục danh mục.
type FormUsage struct{ Svc *Service }

// FormsUsing trả số mẫu đang dùng mỗi mã mục của danh mục catalog: dòng chỉ tiêu (catalog, code) hoặc cột danh mục
// catalog có khóa ổn định = code. Xét phiên bản đã phát hành áp dụng hiện tại, nếu chưa có thì bản đang soạn.
func (f FormUsage) FormsUsing(ctx context.Context, catalog string, codes []string) (map[string]int, error) {
	out := map[string]int{}
	p, ok := authz.FromContext(ctx)
	if !ok || len(codes) == 0 {
		return out, nil
	}
	want := map[string]bool{}
	for _, c := range codes {
		want[c] = true
	}
	err := f.Svc.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		tpls, err := store.ListFormTemplates(ctx, tx, store.FormTemplateFilter{Status: "active"})
		if err != nil {
			return err
		}
		now := f.Svc.now()
		for _, t := range tpls {
			v, err := store.ApplicableFormVersion(ctx, tx, t.ID, now)
			if err != nil {
				return err
			}
			if v == nil {
				if v, err = store.OpenFormVersion(ctx, tx, t.ID); err != nil {
					return err
				}
			}
			if v == nil {
				continue
			}
			seen := map[string]bool{}
			for _, ind := range v.Def.Rows.Indicators {
				if ind.Catalog == catalog && want[ind.Code] {
					seen[ind.Code] = true
				}
			}
			for _, c := range v.Def.Columns {
				if c.Catalog == catalog && want[c.Key] {
					seen[c.Key] = true
				}
			}
			for c := range seen {
				out[c]++
			}
		}
		return nil
	})
	return out, err
}
