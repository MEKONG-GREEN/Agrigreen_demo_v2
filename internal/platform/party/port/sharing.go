package port

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// Cấp quyền chia sẻ dữ liệu giữa hai tenant (ADR-0020; RB-2, Q-MKG-12).
//
// Chủ sản phẩm chốt 29–30/09/2026: MKG (ERP, TMS) là tenant RIÊNG, không dùng chung tenant
// với nền tảng. Nền tảng → MKG chỉ các dữ liệu cần thiết, qua bề mặt đối tác /v1/partner/*
// của Application API, và chỉ khi có một cấp quyền tường minh: bên cấp (tenant chương trình)
// → bên nhận (tenant MKG), phạm vi = danh sách loại dữ liệu, có hạn, người duyệt ≠ người đề
// xuất (R-56), thu hồi được, mọi lần đọc ghi sổ. MKG → nền tảng vẫn chỉ qua van công bố; consumer
// van của nền tảng ghi vào tenant bên cấp đã cấp loại valve_* cho tenant phong bì.
// ---------------------------------------------------------------------------

// Loại dữ liệu của một cấp quyền (cột kinds của pf_party.share_grant_event).
const (
	// Nền tảng → bên nhận (đọc qua /v1/partner/*).
	KindCooperativeDirectory   = "cooperative_directory"    // HTX: mã, tên, xã, trạng thái, vị trí hồ sơ
	KindPlotDirectory          = "plot_directory"           // thửa: hình học, diện tích, xã, HTX, trạng thái — không nông hộ
	KindSeasonHarvestDates     = "season_harvest_dates"     // vụ: ngày gieo, ngày thu hoạch dự kiến/mở (proto FM-01/02)
	KindHTXCollectionResponses = "htx_collection_responses" // phản hồi của HTX với bảng kê thu gom (RB-6 chiều mua)
	// KindWaterwayRoutes (G4-0, pf 000046): định tuyến trên mạng đường thủy đã phát hành của bên cấp
	// (pf/waterway) cho chuyến vận tải thủy của mkg/logistics (proto FM-05) — hình học, km, phiên bản
	// mạng, ràng buộc tải trọng; không dữ liệu cá nhân.
	KindWaterwayRoutes = "waterway_routes"
	// Bên nhận → nền tảng (van công bố ghi vào tenant bên cấp).
	KindValveHub         = "valve_hub"          // hub_facility, hub_capacity → pf/geo hub_view
	KindValveCollection  = "valve_collection"   // collection_schedule, htx_collection_statement → pf/cropcycle
	KindValveMRVEvidence = "valve_mrv_evidence" // mrv_collection_evidence → pf/mrv
	// Sản phẩm dữ liệu Public API (G5-0, pf 000080; ADR-0022 mục 3): bên cấp (chủ dữ liệu) → tenant
	// BÊN THỨ BA. Loại = "dp_" + tên hợp đồng contracts/dataproduct/<tên>.json (DataProductKind).
	KindDPCollectionVolume = "dp_collection_volume"  // số cuộn + diện tích đã gom theo xã × vụ × tháng, ngưỡng k
	KindDPHubCapacity      = "dp_hub_capacity"       // hub MKG đã công bố, sức chứa danh định, bậc sử dụng
	KindDPMRVPackageStatus = "dp_mrv_package_status" // trạng thái gói xuất MRV cho đơn vị thẩm định
)

// ShareKinds là mọi loại hợp lệ (khớp CHECK của migration pf 000042, mở rộng ở pf 000046, pf 000080).
var ShareKinds = []string{KindCooperativeDirectory, KindPlotDirectory, KindSeasonHarvestDates, KindHTXCollectionResponses,
	KindWaterwayRoutes,
	KindValveHub, KindValveCollection, KindValveMRVEvidence,
	KindDPCollectionVolume, KindDPHubCapacity, KindDPMRVPackageStatus}

// DataProductKind trả loại cấp quyền của một sản phẩm dữ liệu Public API: "dp_<product>".
func DataProductKind(product string) string { return "dp_" + product }

// GrantLookup (G5-0, ADR-0022 mục 3) cho cmd/api-public kiểm cấp quyền của bên thứ ba MÀ KHÔNG cần
// vai OIDC (bên gọi là khách hàng API đã xác thực bằng khóa, không phải người dùng Keycloak):
// trả cấp quyền active, trong hạn tại at, bên cấp grantor → bên nhận grantee (tenant bên thứ ba lấy
// từ bản ghi khóa — không bao giờ từ request), có loại kind, ở CẢ bảng chiếu bên nhận lẫn bảng hiện
// hành bên cấp. grantor = uuid.Nil → chỉ nhận khi đúng một bên cấp. Lỗi như PartnerAccess.Authorize
// (ErrShareGrantMissing, ErrShareGrantorAmbiguous). Chỉ đọc; ghi sổ đọc vẫn bằng
// PartnerAccess.RecordRead trước khi trả dữ liệu.
type GrantLookup interface {
	ActiveGrant(ctx context.Context, grantee, grantor uuid.UUID, kind string, at time.Time) (ShareGrant, error)
}

// ValidKind cho biết k có là loại dữ liệu của cấp quyền.
func ValidKind(k string) bool { return slices.Contains(ShareKinds, k) }

// RolePartnerSync là vai của tài khoản dịch vụ bên nhận (client agrigreen-mkg-sync) đọc bề mặt
// /v1/partner/*. Cùng tên vai với cropcycle/port.RoleMKGSync.
const RolePartnerSync = "mkg_sync"

// ShareGrant là một cấp quyền đang hiệu lực mà app/ của pf/party đã kiểm.
type ShareGrant struct {
	ID        uuid.UUID
	Grantor   uuid.UUID // tenant bên cấp — chủ dữ liệu
	Grantee   uuid.UUID // tenant bên nhận (tenant của người gọi / phong bì van)
	Kinds     []string
	ValidFrom time.Time
	ValidTo   time.Time
}

// Covers cho biết cấp quyền có loại k.
func (g ShareGrant) Covers(k string) bool { return slices.Contains(g.Kinds, k) }

// PartnerRead là một lần đọc của bên nhận cần ghi sổ partner_read_log.
type PartnerRead struct {
	Kind     string
	Endpoint string // vd "GET /v1/partner/plots"
	Query    string // tham số lọc/con trỏ (không có dữ liệu cá nhân)
	Items    int
}

// PartnerAccess là cổng kiểm cấp quyền cho bề mặt đối tác (module chủ dữ liệu gọi):
//
//   - Authorize: người gọi (context) phải có RolePartnerSync; bên nhận = tenant của người gọi;
//     trả cấp quyền đang hiệu lực (trạng thái active, trong hạn, ở CẢ bảng chiếu bên nhận lẫn
//     bảng hiện hành bên cấp) có loại kind. grantor = uuid.Nil → chỉ nhận khi đúng một bên cấp có
//     loại này. Lỗi là *problem.Problem (403 ErrShareGrantMissing, 422 ErrShareGrantorAmbiguous).
//   - RecordRead: ghi một dòng sổ partner_read_log ở tenant bên cấp. Module gọi PHẢI ghi sổ
//     thành công trước khi trả dữ liệu (lỗi → 503, không trả dữ liệu chưa ghi sổ).
//
// Module gọi đọc dữ liệu của g.Grantor bằng tenant.WithTenantTx(ctx, pool, g.Grantor, …) trên
// pool vai của chính nó (không có vai BYPASSRLS) và chỉ trả trường tối thiểu, không dữ liệu cá nhân.
type PartnerAccess interface {
	Authorize(ctx context.Context, kind string, grantor uuid.UUID) (ShareGrant, error)
	RecordRead(ctx context.Context, g ShareGrant, r PartnerRead) error
}

// APIReadLog (G5, K-05 — ADR-0022 mục 3, KN US-MRV-05) ghi sổ đọc partner_read_log ở tenant bên cấp
// cho MỘT lần đọc của khách hàng Public API (apiclient/port.Caller trong context, không có
// authz.Principal): tenant bên nhận = Caller.Client.ClientTenantID (phải = g.Grantee), actor_ref = mã
// khách hàng API; chỉ loại dp_* mà g có. Không qua kênh partner_share (egress public_api đã ghi cùng sổ
// sử dụng — ADR-0023). Lỗi → ErrPartnerAuditUnavailable: module gọi KHÔNG trả dữ liệu. Hiện thực không
// kéo kho định danh: party/port/grantlookup (cmd/api-public, RB-3).
type APIReadLog interface {
	RecordRead(ctx context.Context, g ShareGrant, r PartnerRead) error
}

// ValveGrants cho consumer van của nền tảng tìm tenant bên cấp của một phong bì van: mọi bên cấp
// có cấp quyền đang hiệu lực loại kind cho bên nhận grantee (= tenant phong bì) tại thời điểm at =
// lúc consumer NHẬN (đồng hồ của module) — không phải occurred_at: sự kiện hiện trường ghi lùi giờ.
type ValveGrants interface {
	ValveGrantors(ctx context.Context, grantee uuid.UUID, kind string, at time.Time) ([]uuid.UUID, error)
}

// Lỗi của cấp quyền chia sẻ (docs/errors.md, mục pf/party).
var (
	ErrShareGrantMissing = problem.New(http.StatusForbidden, "party.share_grant_missing",
		"Không có cấp quyền chia sẻ đang hiệu lực cho loại dữ liệu này (đã thu hồi, hết hạn hoặc chưa cấp)")
	ErrShareGrantorAmbiguous = problem.New(http.StatusUnprocessableEntity, "party.share_grantor_ambiguous",
		"Có nhiều bên cấp cho loại dữ liệu này — truyền tham số grantor")
	ErrPartnerAuditUnavailable = problem.New(http.StatusServiceUnavailable, "party.partner_audit_unavailable",
		"Không ghi được sổ đọc của bên nhận — không trả dữ liệu")
	// ErrValveGrantMissing: phong bì van của một tenant chưa được bên cấp nào nhận (không có cấp
	// quyền valve_* đang hiệu lực). Consumer trả lỗi → giao lại → DLQ (R-62); duyệt cấp quyền rồi
	// chuyển lại từ DLQ là đủ (xử lý idempotent).
	ErrValveGrantMissing = errors.New("party.valve_share_grant_missing: tenant phong bì van chưa có cấp quyền chia sẻ valve_* đang hiệu lực (ADR-0020)")
)

// ParseGrantor đọc tham số truy vấn grantor (tenant bên cấp) của bề mặt đối tác. Đây KHÔNG phải
// tenant của người gọi (R-16 — tenant người gọi luôn lấy từ token): chỉ chọn bên cấp, và Authorize
// luôn kiểm cấp quyền đang hiệu lực grantor → tenant người gọi trước khi đọc. Rỗng → uuid.Nil (chỉ
// nhận khi đúng một bên cấp có loại đó). Sai dạng → lỗi validation (problem).
func ParseGrantor(s string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, nil
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, problem.Validation.WithField("grantor", "phải là UUID tenant bên cấp")
	}
	return u, nil
}

// ValveTargets trả tenant bên cấp cho phong bì van của grantee: lỗi ErrValveGrantMissing khi
// không có (hoặc chưa nối cổng ValveGrants — mặc định từ chối).
func ValveTargets(ctx context.Context, g ValveGrants, grantee uuid.UUID, kind string, at time.Time) ([]uuid.UUID, error) {
	if g == nil {
		return nil, ErrValveGrantMissing
	}
	out, err := g.ValveGrantors(ctx, grantee, kind, at)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w (tenant phong bì %s, loại %s)", ErrValveGrantMissing, grantee, kind)
	}
	return out, nil
}
