package port

// KN v7.7 gói P2 (docs/brd/kn-v7.7-plan.md §3 P2): phòng ban theo đơn vị (mỗi phòng ban là một module),
// khu vực theo tỉnh (thuộc tính của Nút xã), cây tổ chức 3 cấp, menu theo cấu hình (UR-KT-02).
//
// Module khác (P1 accounts, P3 reports, P4/P5/P7 extension…) chỉ dùng các kiểu và giao diện ở đây.

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Mã phòng ban đã biết (seed TTKN An Giang, tạm — chờ khách Q-02). Mã là chuỗi [a-z][a-z0-9_]{1,39};
// mọi bản ghi nghiệp vụ KN mới mang department_code là một trong các mã của đơn vị.
const (
	DeptTrongTrot = "trong_trot" // Phòng Trồng trọt — "Đang áp dụng" (FN-AD-04.03)
)

// Trạng thái phòng ban (FN-AD-04.03).
const (
	DeptActive    = "active"    // Đang áp dụng — chọn được ở bước 2 đăng nhập
	DeptNotOpen   = "not_open"  // Chưa mở — hiện mờ, nhãn "Chưa mở"
	DeptSuspended = "suspended" // Ngưng hoạt động — dữ liệu cũ giữ nguyên
)

// Bộ menu dựng sẵn (bảng tĩnh có phiên bản trong mã, party/domain/menu.go).
const (
	MenuProfileTrongTrot = "trong_trot"
	MenuProfileGeneric   = "generic" // phòng ban mới bật bằng cấu hình (UR-KT-02)
)

// Đối tượng (vai KN v7.7) của menu. KHÔNG phải vai Keycloak: vai KN nằm ở ngữ cảnh phiên (P1,
// gap §4.5 phương án A). Tên khớp gap §4.5: kn_tw (Cấp Quốc gia), kn_tinh, kn_xa, admin.
const (
	AudienceTW    = "kn_tw"
	AudienceTinh  = "kn_tinh"
	AudienceXa    = "kn_xa"
	AudienceAdmin = "admin"
)

// Audiences là mọi đối tượng menu hợp lệ.
var Audiences = []string{AudienceTW, AudienceTinh, AudienceXa, AudienceAdmin}

// Department là một phòng ban (module) của một đơn vị.
type Department struct {
	ID          uuid.UUID
	OrgRef      uuid.UUID // đơn vị: Nút cấp 1 (ttkn_national) hoặc cấp 2 (ttkn_province)
	Code        string
	Name        string
	Description string
	SortOrder   int
	Status      string // DeptActive | DeptNotOpen | DeptSuspended
	MenuProfile string
	Retired     bool
	UpdatedBy   uuid.UUID
	UpdatedAt   time.Time
}

// Selectable cho biết phòng ban chọn được khi đăng nhập (chỉ "Đang áp dụng", chưa "xóa").
func (d Department) Selectable() bool { return d.Status == DeptActive && !d.Retired }

// Departments là cổng đọc phòng ban cho module khác (P1: dựng ngữ cảnh phiên, kiểm phòng ban của
// gán vai trò; P3: mẫu báo cáo thuộc phòng ban). Tenant lấy từ context. Đọc trực tiếp bảng hiện hành
// nên đổi trạng thái có hiệu lực ở request kế tiếp — không cần phát hành lại ứng dụng (FN-CH-00.01 AC).
type Departments interface {
	// DepartmentUnit trả đơn vị (Nút cấp 1/2) sở hữu phòng ban của một Nút bất kỳ: chính nó nếu là
	// ttkn_national/ttkn_province, ngược lại tổ tiên gần nhất thuộc hai loại đó. ErrOrgNotFound nếu không có.
	DepartmentUnit(ctx context.Context, orgRef uuid.UUID) (uuid.UUID, error)
	// UnitDepartments trả phòng ban (chưa "xóa") của đơn vị, theo thứ tự hiển thị.
	UnitDepartments(ctx context.Context, unitRef uuid.UUID) ([]Department, error)
	// DepartmentByCode trả một phòng ban của đơn vị; ErrDepartmentNotFound nếu không có.
	DepartmentByCode(ctx context.Context, unitRef uuid.UUID, code string) (Department, error)
}

// ErrDepartmentNotFound: đơn vị không có phòng ban với mã này.
var ErrDepartmentNotFound = errors.New("party: không có phòng ban")

// DepartmentUsage (tùy chọn, do module khác cung cấp qua gốc lắp ráp) đếm tài khoản và bản ghi nghiệp
// vụ gắn với một phòng ban — dùng cho cột "số tài khoản", cảnh báo khi ngưng và chặn "xóa"
// (FN-AD-04.03: "không xóa phòng ban đã có tài khoản/dữ liệu"). P1 đếm gán vai trò; P3/P4/P5/P7 đếm bản
// ghi có department_code. Tenant lấy từ context.
type DepartmentUsage interface {
	DepartmentUsage(ctx context.Context, unitRef uuid.UUID, code string) (Usage, error)
}

// Usage là số tài khoản và số bản ghi nghiệp vụ.
type Usage struct {
	Accounts int
	Records  int
}

// OrgImpactSource (tùy chọn) đếm đối tượng chịu ảnh hưởng khi di chuyển/vô hiệu hóa một Nút
// (FN-AD-04.02, bảng "Ảnh hưởng"): tài khoản (P1) và kỳ báo cáo chưa chốt (P4). HTX đếm ngay trong
// pf/party, thửa qua geo/port.PlotStats. communes là các mã xã của nhánh Nút.
type OrgImpactSource interface {
	// ImpactKind là tên dòng trong bảng ảnh hưởng: "accounts" hoặc "open_periods".
	ImpactKind() string
	OrgImpact(ctx context.Context, orgRef uuid.UUID, communes []string) (int, error)
}

// Tên dòng của bảng ảnh hưởng.
const (
	ImpactAccounts     = "accounts"
	ImpactCooperatives = "cooperatives"
	ImpactPlots        = "plots"
	ImpactOpenPeriods  = "open_periods"
)

// FormAvailability (tùy chọn, P3) cho menu biết phòng ban đã có mẫu báo cáo được gán cho loại báo cáo
// ở cấp nhập liệu hay chưa — menu "Báo cáo đột xuất" chỉ hiện khi đã có (FN-AD-04.06). level là
// "province" hoặc "commune"; reportType là mã danh mục report_type của pf/masterdata (vd "dot_xuat").
type FormAvailability interface {
	HasForms(ctx context.Context, department, level, reportType string) (bool, error)
}

// ActorNames (tùy chọn) trả tên hiển thị của người thao tác cho khung Log (RB-3: tên tra lúc đọc từ
// actor_ref, không chép vào sổ). Cùng chữ ký với extension/port.StaffNames; P1 có thể thay bằng danh
// bạ tài khoản.
type ActorNames interface {
	DisplayNames(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error)
}

// SessionContext (tùy chọn, P1) trả đối tượng menu và phòng ban của ngữ cảnh phiên hiện tại
// (X-AG-Context). Khi có, menu mặc định theo ngữ cảnh và chỉ trả menu của ngữ cảnh đó.
type SessionContext interface {
	Current(ctx context.Context) (audience, department string, unitRef uuid.UUID, ok bool)
}

// ---------------------------------------------------------------------------
// Khu vực
// ---------------------------------------------------------------------------

// Region là một khu vực của tỉnh.
type Region struct {
	ID           uuid.UUID
	ProvinceCode string
	Code         string
	Name         string
	SortOrder    int
	Active       bool
}

// CommuneRegion là khu vực của một xã (Nút extension_team).
type CommuneRegion struct {
	OrgRef      uuid.UUID
	CommuneCode string
	RegionCode  string // rỗng = chưa gán
	RegionName  string
	SortOrder   int
}

// Regions là cổng đọc khu vực cho P4 (nhóm dòng xã và dòng tổng Khu vực/Toàn tỉnh của bảng tổng
// hợp; ảnh chụp cấu trúc khu vực lúc chốt). Tenant lấy từ context.
type Regions interface {
	// ProvinceRegions trả khu vực của tỉnh (kể cả ngừng dùng nếu includeInactive), theo thứ tự hiển thị.
	ProvinceRegions(ctx context.Context, provinceCode string, includeInactive bool) ([]Region, error)
	// CommuneRegions trả khu vực hiện hành của mọi Nút xã đang hoạt động thuộc tỉnh, khóa theo mã xã.
	CommuneRegions(ctx context.Context, provinceCode string) (map[string]CommuneRegion, error)
}

// Unassigned là mã nhóm của xã chưa gán khu vực trong cây và trong tổng theo khu vực.
const Unassigned = ""

// RegionTotals là tổng theo khu vực và toàn tỉnh của một chỉ tiêu.
type RegionTotals struct {
	ByRegion map[string]decimal.Decimal // khóa = mã khu vực; Unassigned cho xã chưa gán
	Province decimal.Decimal            // Toàn tỉnh = Σ mọi xã = Σ mọi khu vực
	Communes map[string]int             // số xã có số liệu theo khu vực
}

// Regions trả mã khu vực có trong tổng, sắp theo mã (Unassigned cuối).
func (t RegionTotals) Regions() []string {
	out := make([]string, 0, len(t.ByRegion))
	for k := range t.ByRegion {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i] == Unassigned) != (out[j] == Unassigned) {
			return out[j] == Unassigned
		}
		return out[i] < out[j]
	})
	return out
}

// SumByRegion cộng giá trị theo xã thành dòng tổng Khu vực và Toàn tỉnh (decimal, R-10). Xã không có
// trong assign (chưa gán) cộng vào Unassigned — Toàn tỉnh luôn bằng Σ các khu vực.
func SumByRegion(values map[string]decimal.Decimal, assign map[string]CommuneRegion) RegionTotals {
	t := RegionTotals{ByRegion: map[string]decimal.Decimal{}, Province: decimal.Zero, Communes: map[string]int{}}
	for commune, v := range values {
		r := assign[commune].RegionCode
		t.ByRegion[r] = t.ByRegion[r].Add(v)
		t.Communes[r]++
		t.Province = t.Province.Add(v)
	}
	return t
}
