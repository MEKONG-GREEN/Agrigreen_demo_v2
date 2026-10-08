// Package port là giao diện công khai DUY NHẤT của module pf/cooperative (dom/cooperative:
// HTX, nông hộ, thửa, vụ, nhật ký canh tác) mà module khác được import (R-02). Thiết kế:
// docs/design/cooperative.md mục 4 (W3.1); hiện thực W3.2.
//
// W3.2: module là MẶT TIỀN — dữ liệu vẫn ở pf_party/pf_geo/pf_cropcycle, gốc lắp ráp nối
// nguồn cũ qua bộ chuyển cmd/internal/coopwire. Gói này KHÔNG import port của party/geo/
// cropcycle: pf/cooperative phải là đỉnh lá của đồ thị module để bên tiêu thụ đang nằm trong
// chu trình pf (mechanization, extension, cropcycle) đổi sang được mà không sinh chu trình mới
// (agmodgraph, docs/design/cooperative.md mục 3, ghi chú W3.2). W3.3 chuyển store vào module.
//
// DTO giữ nguyên trường của DTO cũ (partyport, geoport, ccport) để đổi bên tiêu thụ chỉ là
// đổi import. Không có dữ liệu cá nhân: nông hộ chỉ là household_ref (R-41), thửa không trả
// hình học qua port.
package port

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Vai nghiệp vụ (coop_manager, farmer, plot_editor, plot_viewer, sop_admin): W3.2 chưa khai hằng Role*
// ở đây — module chưa kiểm vai nào, mà modgen sinh permissions từ Role* và modreg.RoleBlocked coi module
// per_tenant đang bật cùng khai là "không chặn" (tắt pf/geo sẽ không còn chặn plot_editor). Vai vẫn do
// pf/party, pf/geo, pf/cropcycle khai; W3.3 chuyển về đây cùng handler (docs/design/cooperative.md mục 7.1, 16.4).

// Trạng thái thửa (geoport.Status*).
const (
	PlotDraft   = "draft"   // KN vẽ trước, chưa gán HTX
	PlotActive  = "active"  // đã gán HTX
	PlotRetired = "retired" // ngừng dùng (xóa mềm, GIS BR-17)
)

// Trạng thái vụ (ccport.Season*).
const (
	SeasonOpen      = "open"
	SeasonCompleted = "completed"
	SeasonCancelled = "cancelled"
)

// Trạng thái duyệt nhật ký (ccport.Log*).
const (
	LogPending         = "pending"
	LogApproved        = "approved"
	LogNeedsSupplement = "needs_supplement"
	LogSuperseded      = "superseded"
)

// Entity đồng bộ ngoại tuyến (docs/dev/sync-contract.md) — tên giữ nguyên khi chuyển chủ (mục 5,
// dòng pf/sync) để field-app/htx-app không phải đổi.
const (
	EntityPlot         = "plot"
	EntityPlotBoundary = "plot_boundary"
	EntitySeason       = "season"
	EntityFarmLog      = "farm_log"
	EntitySeasonOpen   = "season_open"
	EntityLogBatch     = "farm_log_batch"
	EntityHarvest      = "harvest"
)

var (
	// ErrHouseholdNotFound: không có nông hộ trong tenant.
	ErrHouseholdNotFound = errors.New("cooperative: không có nông hộ")
	// ErrPlotNotFound: không có thửa trong tenant.
	ErrPlotNotFound = errors.New("cooperative: không có thửa")
	// ErrDiaryEntryNotFound: không có nhật ký canh tác.
	ErrDiaryEntryNotFound = errors.New("cooperative: không có nhật ký canh tác")
	// ErrSubjectNotFound: Resolve không thấy chủ thể (hoặc chủ thể ngoài tenant).
	ErrSubjectNotFound = errors.New("cooperative: không có chủ thể")
	// ErrSubjectTypeUnsupported: loại chủ thể không hợp lệ hoặc chưa phân giải được (W3.2: season,
	// harvest chờ store riêng ở W3.3).
	ErrSubjectTypeUnsupported = errors.New("cooperative: loại chủ thể không hỗ trợ")
	// ErrUnavailable: nguồn dữ liệu chưa nối (gốc lắp ráp không cấp). Bên gọi coi như phụ thuộc lỗi (503).
	ErrUnavailable = errors.New("cooperative: nguồn dữ liệu chưa nối")
)

// ---------------------------------------------------------------------------
// Cổng đọc. Tenant lấy từ context (tenant.WithID). Không kiểm phạm vi người gọi — module gọi
// tự giới hạn danh sách xã/HTX theo phạm vi của mình (như các port cũ).
// ---------------------------------------------------------------------------

// Cooperatives: hồ sơ HTX (Nút kind=cooperative của pf/party + hồ sơ + vùng HTX).
type Cooperatives interface {
	// ByUnit trả số HTX đang hoạt động và hộ thành viên đang mở theo mã đơn vị; units nil =
	// mọi đơn vị. Đơn vị không có HTX không có trong kết quả (thay partyport.CooperativeStats).
	ByUnit(ctx context.Context, units []string) ([]UnitCooperatives, error)
	// Active liệt kê HTX đang hoạt động (thay partyport.CooperativeSites).
	Active(ctx context.Context) ([]CooperativeSite, error)
	// Centroids trả tâm vùng HTX đang hiệu lực; coops nil = mọi HTX; HTX chưa có vùng không có
	// trong kết quả (thay geoport.CoopCentroids).
	Centroids(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]CoopCentroid, error)
}

// Households: nông hộ (chỉ household_ref của kho định danh, R-41) và xã viên theo thời gian.
type Households interface {
	// Membership trả HTX hiện tại của nông hộ; không có → ErrHouseholdNotFound
	// (thay partyport.Households).
	Membership(ctx context.Context, ref uuid.UUID) (HouseholdMembership, error)
	// InCommunes trả nông hộ có thửa đang dùng (nháp hoặc đã gán HTX) trong các xã; communes
	// nil = mọi xã (thay geoport.PlotHouseholds).
	InCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error)
}

// Plots: thửa (không trả hình học — dữ liệu cá nhân gián tiếp, v4.1 §28).
type Plots interface {
	// Get trả thửa; không có → ErrPlotNotFound (thay geoport.Plots.Get).
	Get(ctx context.Context, ref uuid.UUID) (Plot, error)
	// DistanceM trả khoảng cách trắc địa (mét, làm tròn) từ điểm WGS84 tới ranh giới hiện hành
	// của thửa; 0 nếu điểm nằm trong thửa (thay geoport.Plots.DistanceM).
	DistanceM(ctx context.Context, ref uuid.UUID, lon, lat decimal.Decimal) (decimal.Decimal, error)
	// Facts tra nhiều thửa một lần; thửa không có trong tenant không có trong kết quả. Kết quả chỉ
	// để TỔNG HỢP (thay geoport.PlotFacts).
	Facts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]Plot, error)
	// DigitizedByCommune: số thửa đã số hoá và diện tích theo xã; communes nil = mọi xã
	// (thay geoport.PlotStats).
	DigitizedByCommune(ctx context.Context, communes []string) ([]PlotStat, error)
	// DigitizedByCooperative: như trên theo HTX (chỉ HTX có thửa).
	DigitizedByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]PlotStat, error)
}

// Seasons: vụ, khai báo sản lượng, diện tích vụ theo HTX, số liệu báo cáo.
type Seasons interface {
	// ActivityByCooperative: vụ đang mở và nhật ký chờ duyệt; coops nil = mọi HTX có hoạt động
	// (thay ccport.Stats.ActivityByCooperative).
	ActivityByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]CoopActivity, error)
	// Areas: diện tích vụ của HTX; seasonCode rỗng / year 0 = mọi vụ / năm (thay ccport.Stats.SeasonAreas).
	Areas(ctx context.Context, coop uuid.UUID, seasonCode string, year int) ([]CoopSeasonArea, error)
	// Supply: sản lượng lúa theo HTX × vụ của một năm (thay ccport.SupplyStats).
	Supply(ctx context.Context, year int) ([]CoopSeasonSupply, error)
	// Production: số liệu báo cáo sản lượng HTX (thay ccport.ReportData). Không kiểm vai —
	// bên gọi đã giải phạm vi vào q.
	Production(ctx context.Context, q ProductionQuery) (ProductionData, error)
	// HarvestDates: ngày gieo / thu hoạch của vụ trên các thửa (thay đọc chéo của van collection).
	// W3.2: nguồn cũ không có cổng → ErrUnavailable; có ở W3.3.
	HarvestDates(ctx context.Context, plotRefs []uuid.UUID) ([]SeasonHarvestDate, error)
}

// Diary: nhật ký canh tác (sổ farm_log + bảng chiếu farm_log_current), bằng chứng MRV
// (ADR-0011). Phục vụ cap/evidence (W6.5) và đọc chia sẻ (W4.2). W3.2: chưa có nguồn → ErrUnavailable.
type Diary interface {
	// Get trả phiên bản hiện hành của log_ref; không có → ErrDiaryEntryNotFound.
	Get(ctx context.Context, ref uuid.UUID) (DiaryEntry, error)
	// BySeason trả nhật ký của vụ theo log_ref tăng dần, sau after (uuid.Nil = từ đầu), tối đa limit
	// (≤ 0 → 100; > 500 → 500).
	BySeason(ctx context.Context, season uuid.UUID, after uuid.UUID, limit int) ([]DiaryEntry, error)
}

// Resolve phân giải bộ ba (subject_module, subject_type, subject_id) của R-DB-07 / cap/evidence (W6.5).
type Resolve interface {
	// Resolve trả chủ thể; không có hoặc ngoài tenant → ErrSubjectNotFound; loại không hỗ trợ →
	// ErrSubjectTypeUnsupported.
	Resolve(ctx context.Context, t SubjectType, id uuid.UUID) (Subject, error)
}

// SubjectType là loại chủ thể của Resolve.
type SubjectType string

// Loại chủ thể.
const (
	SubjectCooperative SubjectType = "cooperative"
	SubjectHousehold   SubjectType = "household"
	SubjectPlot        SubjectType = "plot"
	SubjectSeason      SubjectType = "season"
	SubjectDiaryEntry  SubjectType = "diary_entry"
	SubjectHarvest     SubjectType = "harvest"
)

// SubjectModule là subject_module của bộ ba R-DB-07 cho mọi chủ thể của module.
const SubjectModule = "pf/cooperative"

// Subject là chủ thể đã phân giải. Không có dữ liệu cá nhân.
type Subject struct {
	Type          SubjectType
	ID            uuid.UUID
	TenantID      uuid.UUID // đơn vị chủ (H-1)
	NodeRef       uuid.UUID // Nút phạm vi = cooperative_ref; uuid.Nil khi thửa nháp chưa gán HTX
	CommuneCode   string
	HouseholdRef  uuid.UUID // uuid.Nil nếu không gắn hộ
	Status        string
	ViaShareGrant bool // W4.2
}

// ---------------------------------------------------------------------------
// Interface do pf/cooperative CẦN, module khác hiện thực (nối ở gốc lắp ráp).
// ---------------------------------------------------------------------------

// MachineCounts: tổng số máy khai báo của HTX (pf/mechanization hiện thực; KN FN-XA-03.03/03.04).
// Thay partyport.CoopMachineCounts (cùng chữ ký).
type MachineCounts interface {
	CoopMachineTotal(ctx context.Context, cooperativeRef uuid.UUID) (int, error)
}

// FarmerHouseholds ánh xạ tài khoản nông dân (actor_ref) sang nông hộ liên kết (pf/accounts,
// qua gốc lắp ráp). Thay geoport/ccport.FarmerHouseholds (cùng chữ ký).
type FarmerHouseholds interface {
	HouseholdsOf(ctx context.Context, actorRef uuid.UUID) ([]uuid.UUID, error)
}
