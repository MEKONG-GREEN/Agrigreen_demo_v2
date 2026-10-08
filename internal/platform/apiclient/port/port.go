// Package port là cửa công khai DUY NHẤT của module pf/apiclient (R-02): khách hàng API của
// Public API (partner.<domain>, cmd/api-public), khóa API lưu băm, scope, đăng ký sản phẩm dữ
// liệu, gói hạn mức, việc xuất bất đồng bộ và handler của sản phẩm dữ liệu (ADR-0022; tài liệu
// kiến trúc mục 11; docs/dev/g5-conventions.md).
//
// Mô hình tenant (ADR-0022 mục 3): khách hàng API, khóa, đăng ký, gói hạn mức thuộc tenant ĐƠN VỊ
// VẬN HÀNH (AG_PUBLIC_OPERATOR_TENANT); bên thứ ba là tenant riêng (ClientTenantID); dữ liệu phục
// vụ thuộc tenant chủ dữ liệu (bên cấp) và chỉ đọc được khi có cấp quyền chia sẻ loại dp_<sản phẩm>
// (ADR-0020, party/port.GrantLookup).
//
// Khóa API (ADR-0022 mục 4): "agk_<prefix>_<secret>", header X-API-Key; chỉ lưu prefix +
// HMAC-SHA-256(pepper theo phiên bản, khóa đầy đủ); bí mật hiện MỘT lần lúc tạo, không bao giờ log,
// lưu, đưa vào lỗi/URL/sự kiện. Vòng đời khóa là sổ chỉ ghi thêm (RB-1).
//
// ĐÓNG BĂNG ở G5-0: đổi chữ ký / ngữ nghĩa qua người tích hợp; thêm phương thức vào interface MỚI;
// thêm trường DTO chỉ khi tương thích ngược.
package port

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// DBRole là vai DB của module ở cụm pf (schema pf_apiclient, pf 000080/000081).
const DBRole = "apiclient_app"

// PublicDBRole là vai vận hành của tiến trình cmd/api-public: chỉ bảng vận hành schema shared
// (idempotency, outbox) như platform_api — không quyền nghiệp vụ (pf 000080).
const PublicDBRole = "public_api"

// Producer là giá trị producer của phong bì sự kiện.
const Producer = "pf/apiclient"

// Vai nghiệp vụ (realm Keycloak; tài khoản tenant đơn vị vận hành, bề mặt Admin API — MFA).
const (
	// RoleAPIClientAdmin: đăng ký khách hàng API, đề xuất đăng ký sản phẩm + scope + gói hạn mức,
	// phát hành / xoay / thu hồi khóa (bí mật hiện một lần).
	RoleAPIClientAdmin = "api_client_admin"
	// RoleAPIClientApprover: duyệt khách hàng, đăng ký sản phẩm, gói hạn mức — KHÔNG duyệt việc do
	// chính mình đề xuất (R-56, app/ + CHECK DB).
	RoleAPIClientApprover = "api_client_approver"
)

// ---------------------------------------------------------------------------
// Khóa API, phương thức xác thực, scope, header
// ---------------------------------------------------------------------------

// Header và tiền tố khóa.
const (
	KeyHeader       = "X-API-Key"
	KeyTokenPrefix  = "agk_"
	KeyPrefixLength = 12 // ký tự base32 thường (a-z, 2-7)
	// MaxActiveKeys: số khóa hiệu lực tối đa mỗi khách hàng (chồng lấn khi xoay).
	MaxActiveKeys = 2
)

var keyRe = regexp.MustCompile(`^agk_([a-z2-7]{12})_([A-Za-z0-9_-]{43})$`)

// ParseKey tách prefix của khóa trình ra (header X-API-Key). ok = false khi sai dạng — trả 401
// apiclient.unauthenticated, không nói lý do. Prefix dùng để tra cứu và ghi log; KHÔNG BAO GIỜ log
// cả khóa.
func ParseKey(presented string) (prefix string, ok bool) {
	m := keyRe.FindStringSubmatch(presented)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// AuthMethod là phương thức xác thực của khách hàng API (ADR-0022 mục 5).
type AuthMethod string

// Phương thức xác thực.
const (
	AuthAPIKey                 AuthMethod = "api_key"                  // bắt buộc có
	AuthOAuthClientCredentials AuthMethod = "oauth_client_credentials" //nolint:gosec // tên phương thức xác thực, không phải bí mật — tùy chọn cho đơn vị thẩm định
	AuthMTLS                   AuthMethod = "mtls"                     // để sau (cần quyết định ingress)
)

// Scope.
const (
	ActionRead     = "read"
	ActionExport   = "export"
	ScopeUsageRead = "usage:read"
)

// ProductScope trả scope của sản phẩm dữ liệu: "dp:<product>:<action>".
func ProductScope(product, action string) string { return "dp:" + product + ":" + action }

// Header phản hồi của Public API (ADR-0022 mục 7–8).
const (
	HeaderDataContract       = "AG-Data-Contract" // "<sản phẩm>.v<N>"
	HeaderRateLimitLimit     = "RateLimit-Limit"
	HeaderRateLimitRemaining = "RateLimit-Remaining"
	HeaderRateLimitReset     = "RateLimit-Reset" // giây tới khi cửa sổ đặt lại
	HeaderQuotaRemaining     = "AG-Quota-Remaining"
	HeaderRequestID          = "AG-Request-Id"
)

// Biến môi trường của cmd/api-public (ADR-0022 mục 1).
const (
	EnvEnabled          = "AG_PUBLIC_API_ENABLED" // "1" mới nghe; khác → thoát lỗi khi khởi động
	EnvHTTPAddr         = "AG_PUBLIC_HTTP_ADDR"   // mặc định DefaultHTTPAddr
	EnvOperatorTenant   = "AG_PUBLIC_OPERATOR_TENANT"
	EnvPepperPrefix     = "AG_APIKEY_PEPPER_V" // + số phiên bản; ≥ 32 byte
	EnvPepperCurrent    = "AG_APIKEY_PEPPER_CURRENT"
	EnvOIDCIssuer       = "AG_PUBLIC_OIDC_ISSUER"
	EnvOIDCAudience     = "AG_PUBLIC_OIDC_AUDIENCE"
	EnvValkeyURL        = "AG_VALKEY_URL"
	DefaultHTTPAddr     = ":8085"
	MinPepperBytes      = 32
	DefaultKeyLifetime  = 365 * 24 * time.Hour
	DefaultRotateGrace  = 7 * 24 * time.Hour
	IdempotencyTTLHours = 48 // R-51
)

// JobKeyExpiry là job cmd/worker (-jobs, chạy ở leader) ghi sự kiện expired vào sổ api_key_event cho
// mọi khóa đã quá hạn (hoặc quá hạn chồng lấn khi xoay) của tenant đơn vị vận hành (RB-1: trạng thái
// khóa đổi bằng bản ghi sổ mới, không UPDATE cột). Xác thực đã từ chối khóa quá hạn theo thời điểm;
// job chỉ làm sổ và bảng chiếu khớp thực tế (danh sách khóa ở Admin API, tối đa 2 khóa hiệu lực).
const JobKeyExpiry = "apiclient_key_expiry"

// Khóa tham số (pf/masterdata, có phiên bản, setter ≠ approver).
const (
	// ParamKMin: ngưỡng gom nhóm k của sản phẩm tổng hợp (C-3, Q-MKG-11) — tạm 10, chờ khách.
	ParamKMin = "publicdata.k_min"
)

// ---------------------------------------------------------------------------
// Khách hàng, khóa, gói hạn mức
// ---------------------------------------------------------------------------

// ClientStatus: proposed → active → suspended ↔ active → revoked (cuối). Đổi trạng thái là bản ghi sổ.
type ClientStatus string

// Trạng thái khách hàng API.
const (
	ClientProposed  ClientStatus = "proposed"
	ClientActive    ClientStatus = "active"
	ClientSuspended ClientStatus = "suspended"
	ClientRevoked   ClientStatus = "revoked"
)

// KeyEventKind là loại bản ghi sổ khóa api_key_event (RB-1: không UPDATE trạng thái).
type KeyEventKind string

// Loại sự kiện khóa.
const (
	KeyIssued  KeyEventKind = "issued"
	KeyRotated KeyEventKind = "rotated" // khóa cũ còn hiệu lực tới grace_until
	KeyRevoked KeyEventKind = "revoked" // bắt buộc lý do; hiệu lực ngay ở lời gọi kế tiếp
	KeyExpired KeyEventKind = "expired"
)

// SubscriptionStatus: đăng ký sản phẩm của khách hàng (scope cấp qua đây).
type SubscriptionStatus string

// Trạng thái đăng ký sản phẩm.
const (
	SubscriptionProposed SubscriptionStatus = "proposed"
	SubscriptionActive   SubscriptionStatus = "active"
	SubscriptionRevoked  SubscriptionStatus = "revoked"
)

// Client là khách hàng API đang hiệu lực (bảng chiếu, tenant đơn vị vận hành).
type Client struct {
	ID             uuid.UUID
	OperatorTenant uuid.UUID // tenant_id của bản ghi (đơn vị vận hành)
	ClientTenantID uuid.UUID // tenant bên thứ ba (bên nhận cấp quyền dp_*)
	DisplayName    string    // tên tổ chức — không phải người
	Status         ClientStatus
	AuthMethods    []AuthMethod
	ContactRef     uuid.UUID // đầu mối liên hệ: chỉ mã tham chiếu (tên/điện thoại ở vault hoặc pf/accounts)
	Scopes         []string  // từ đăng ký sản phẩm đang active
}

// HasScope cho biết khách hàng có scope s.
func (c Client) HasScope(s string) bool {
	for _, x := range c.Scopes {
		if x == s {
			return true
		}
	}
	return false
}

// Caller là bên gọi Public API đã xác thực — "principal" của cmd/api-public (thay authz.Principal).
// Tenant của bên gọi = Client.ClientTenantID, lấy từ bản ghi khóa, không bao giờ từ path/query/body.
type Caller struct {
	Client     Client
	KeyID      uuid.UUID // khóa đã dùng (Nil với oauth_client_credentials)
	KeyPrefix  string    // an toàn để log
	AuthMethod AuthMethod
	RequestID  uuid.UUID // UUIDv7 sinh ở api-public = idem_key của dòng sổ sử dụng
	ReceivedAt time.Time
}

type callerKey struct{}

// WithCaller gắn bên gọi vào context, kèm dạng tối thiểu authz.APIClient cho module khác đọc (không import
// pf/apiclient — R-MOD-05).
func WithCaller(ctx context.Context, c Caller) context.Context {
	ctx = authz.WithAPIClient(ctx, authz.APIClient{ClientID: c.Client.ID, TenantID: c.Client.ClientTenantID, RequestID: c.RequestID, ReceivedAt: c.ReceivedAt})
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom đọc bên gọi từ context.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// Authenticator xác thực khóa API trình ra (header X-API-Key): tra theo prefix, so HMAC thời gian
// hằng theo pepper_version của khóa, kiểm khóa chưa thu hồi / chưa hết hạn (kể cả hạn chồng lấn khi
// xoay) và khách hàng active. Mọi thất bại → ErrUnauthenticated (không phân biệt lý do).
type Authenticator interface {
	Authenticate(ctx context.Context, presented string, receivedAt time.Time) (Caller, error)
}

// TokenAuthenticator (tùy chọn, AuthOAuthClientCredentials): kiểm JWT client credentials (issuer,
// audience, chữ ký — R-54), ánh xạ azp → khách hàng có AuthOAuthClientCredentials.
type TokenAuthenticator interface {
	AuthenticateToken(ctx context.Context, bearer string, receivedAt time.Time) (Caller, error)
}

// IssuedKey là kết quả phát hành / xoay khóa. Secret chỉ có trong phản hồi tạo khóa (Cache-Control:
// no-store) — tag pii:"true" để agpiilog cấm đưa vào log/lỗi (R-14, R-92).
type IssuedKey struct {
	KeyID      uuid.UUID  `json:"key_id"`
	ClientID   uuid.UUID  `json:"client_id"`
	Prefix     string     `json:"key_prefix"`
	Secret     string     `json:"api_key" pii:"true"` // "agk_<prefix>_<secret>" — HIỆN MỘT LẦN
	ExpiresAt  time.Time  `json:"expires_at"`
	GraceUntil *time.Time `json:"previous_key_grace_until,omitempty"` // khi xoay: hạn chồng lấn của khóa cũ
}

// Plan là gói hạn mức có phiên bản của một khách hàng (người đặt api_client_admin ≠ người duyệt
// api_client_approver). Giá trị mặc định "tạm, chờ khách" (g5-conventions §1.2).
type Plan struct {
	ClientID          uuid.UUID
	Version           int
	RequestsPerMinute int64
	DailyUnits        int64
	MonthlyUnits      int64
	MaxExportRows     int64
	EffectiveFrom     time.Time
}

// Mặc định gói hạn mức (tạm, chờ khách).
const (
	DefaultRequestsPerMinute = 60
	DefaultDailyUnits        = 10_000
	DefaultMonthlyUnits      = 200_000
	DefaultMaxExportRows     = 100_000
)

// Plans cho pf/metering đọc gói hạn mức hiệu lực tại at (tenant đơn vị vận hành truyền tường minh).
type Plans interface {
	PlanFor(ctx context.Context, operatorTenant, clientID uuid.UUID, at time.Time) (Plan, error)
}

// ---------------------------------------------------------------------------
// Việc xuất bất đồng bộ (R-52, mẫu mục 12)
// ---------------------------------------------------------------------------

// ExportStatus là trạng thái việc xuất của Public API.
type ExportStatus string

// Trạng thái việc xuất.
const (
	ExportQueued    ExportStatus = "queued"
	ExportCompleted ExportStatus = "completed"
	ExportFailed    ExportStatus = "failed"
)

// Sự kiện và consumer của pf/apiclient.
const (
	// EventExportRequested: yêu cầu xuất đã ghi job + ảnh chụp đầu vào (tenant đơn vị vận hành).
	EventExportRequested = "pf.apiclient.export.requested"
	// ExportConsumer: worker dựng tệp (CSV/JSON tất định, SHA-256, kho đối tượng trong lãnh thổ).
	ExportConsumer = "pf.apiclient.export"
)

// ExportRequestedV1 là payload yêu cầu xuất. Worker nạp bộ lọc + bên cấp từ bảng job, không từ sự
// kiện. Chỉ mã tham chiếu.
//
//ag:event pf.apiclient.export.requested v1
type ExportRequestedV1 struct {
	JobID           string `json:"job_id"`
	ClientID        string `json:"client_id"`
	Product         string `json:"product"`
	ContractVersion int    `json:"contract_version"`
	DataTenantID    string `json:"data_tenant_id"`
	RequestedAt     string `json:"requested_at"`
}

// ---------------------------------------------------------------------------
// Sản phẩm dữ liệu — DTO một phần tử (contracts/dataproduct/*.json, contractcheck kiểm)
// ---------------------------------------------------------------------------

// Tên sản phẩm dữ liệu G5-0 (dự thảo, chờ khách chốt bên thứ ba đầu tiên).
const (
	ProductCollectionVolume = "collection_volume"
	ProductHubCapacity      = "hub_capacity"
	ProductMRVPackageStatus = "mrv_package_status"
)

// CollectionVolumeV1 là một ô tổng hợp (xã × vụ × tháng), chỉ khi ≥ k nông hộ phân biệt.
//
//ag:dataproduct collection_volume
type CollectionVolumeV1 struct {
	ProvinceCode    string `json:"province_code"`
	CommuneCode     string `json:"commune_code"`
	SeasonCode      string `json:"season_code"`
	PeriodMonth     string `json:"period_month"`
	RollsCollected  int64  `json:"rolls_collected"`
	AreaHaCollected string `json:"area_ha_collected"` // decimal "0.00"
	KApplied        int64  `json:"k_applied"`
	AsOf            string `json:"as_of"`
}

// LatLng là tọa độ WGS84 (json.Number — không float trong DTO).
type LatLng struct {
	Lat json.Number `json:"lat"`
	Lng json.Number `json:"lng"`
}

// HubCapacityV1 là một hub MKG đã công bố kèm bậc sử dụng (không tấn tồn).
//
//ag:dataproduct hub_capacity
type HubCapacityV1 struct {
	HubRef          string  `json:"hub_ref"`
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	Kind            string  `json:"kind"`
	ProvinceCode    string  `json:"province_code"`
	CommuneCode     *string `json:"commune_code"`
	Location        LatLng  `json:"location"`
	Status          string  `json:"status"`
	CapacityTons    *string `json:"capacity_tons"`
	UtilizationBand string  `json:"utilization_band"`
	AsOf            string  `json:"as_of"`
}

// MRVPackageStatusV1 là một gói xuất MRV của bên cấp (cho đơn vị thẩm định — chỉ xem).
//
//ag:dataproduct mrv_package_status
type MRVPackageStatusV1 struct {
	ExportID        string  `json:"export_id"`
	Status          string  `json:"status"`
	RequestedAt     string  `json:"requested_at"`
	CompletedAt     *string `json:"completed_at"`
	SeasonCode      *string `json:"season_code"`
	ProvinceCode    *string `json:"province_code"`
	EvidenceCount   int64   `json:"evidence_count"`
	CatalogVersion  string  `json:"catalog_version"`
	PackageSHA256   *string `json:"package_sha256"`
	SnapshotSHA256  *string `json:"snapshot_sha256"`
	LastVerifyMatch *bool   `json:"last_verify_match"`
}

// ---------------------------------------------------------------------------
// Nguồn dữ liệu của sản phẩm — module CHỦ DỮ LIỆU hiện thực (đọc bằng pool vai của chính nó với
// tenant.WithTenantTx(ctx, pool, DataTenant, …) — R-05, không BYPASSRLS), gốc lắp ráp nối vào
// pf/apiclient. pf/apiclient đã kiểm cấp quyền dp_<sản phẩm> TRƯỚC khi gọi; nguồn chỉ trả trường
// của DTO (không dữ liệu cá nhân). Con trỏ (After/Next) là chuỗi mờ do nguồn tự mã hóa.
// ---------------------------------------------------------------------------

// CollectionQuery là bộ lọc của collection_volume.
type CollectionQuery struct {
	DataTenant   uuid.UUID
	SeasonCode   string // rỗng = mọi vụ
	ProvinceCode string // rỗng = mọi tỉnh
	FromMonth    string // YYYY-MM, rỗng = không chặn
	ToMonth      string
	K            int64 // ngưỡng gom nhóm đã đọc từ ParamKMin (≥ 2)
	AsOf         time.Time
	After        string
	Limit        int
}

// CollectionPage là một trang ô tổng hợp. Suppressed = số ô bị bỏ vì dưới K (cả bộ lọc, không chỉ trang).
type CollectionPage struct {
	Items      []CollectionVolumeV1
	Next       string
	Suppressed int64
}

// CollectionVolumeSource: pf/cropcycle hiện thực (bảng kê HTX nhận qua van collection, revision cao
// nhất; số nông hộ phân biệt qua pf/geo — chủ quan hệ thửa ↔ nông hộ). Ô có < K nông hộ phân biệt
// KHÔNG được trả (kiểm thử bắt buộc).
type CollectionVolumeSource interface {
	CollectionCells(ctx context.Context, q CollectionQuery) (CollectionPage, error)
}

// HubCapacitySource: pf/geo hiện thực (mô hình đọc hub nhận qua van hub_facility/hub_capacity; bậc sử
// dụng theo geo.hub_capacity_warn_pct / geo.hub_capacity_full_pct — không trả tấn tồn).
type HubCapacitySource interface {
	HubCapacities(ctx context.Context, dataTenant uuid.UUID, provinceCode, after string, limit int) (items []HubCapacityV1, next string, err error)
}

// MRVPackageSource: pf/mrv hiện thực (pf_mrv.export_job + kết quả kiểm lại gần nhất). Không ảnh,
// tọa độ, mã nông hộ, người thu/duyệt.
type MRVPackageSource interface {
	Packages(ctx context.Context, dataTenant uuid.UUID, status, after string, limit int) (items []MRVPackageStatusV1, next string, err error)
	// Package: không có (hoặc thuộc tenant khác) → 404 mrv.export_not_found.
	Package(ctx context.Context, dataTenant, exportID uuid.UUID) (MRVPackageStatusV1, error)
}

// ---------------------------------------------------------------------------
// Lỗi (docs/errors.md, mục pf/apiclient)
// ---------------------------------------------------------------------------

var (
	// ErrUnauthenticated: khóa thiếu, sai dạng, sai, thu hồi, hết hạn hoặc khách hàng không active —
	// KHÔNG nói lý do. Không ghi sổ sử dụng (không quy được về khách hàng), chỉ log prefix + metric.
	ErrUnauthenticated = problem.New(http.StatusUnauthorized, "apiclient.unauthenticated", "Khóa API không hợp lệ")
	// ErrForbidden: thiếu scope, thiếu cấp quyền dp_* đang hiệu lực, hoặc sản phẩm không phục vụ ở
	// môi trường này — không nói thiếu điều kiện nào.
	ErrForbidden = problem.New(http.StatusForbidden, "apiclient.forbidden", "Không có quyền truy cập sản phẩm dữ liệu này")
	// ErrUnknownProduct: không có sản phẩm dữ liệu với tên này.
	ErrUnknownProduct = problem.New(http.StatusNotFound, "apiclient.unknown_product", "Không có sản phẩm dữ liệu này")
	// ErrRateLimited: vượt giới hạn tần suất mỗi phút (Retry-After).
	ErrRateLimited = problem.New(http.StatusTooManyRequests, "apiclient.rate_limited", "Vượt giới hạn tần suất gọi")
	// ErrContractViolation: phần tử sắp trả có trường ngoài hợp đồng dữ liệu — không trả gì, cảnh báo.
	ErrContractViolation = problem.New(http.StatusInternalServerError, "apiclient.contract_violation", "Dữ liệu không khớp hợp đồng sản phẩm — không trả")
	// ErrSelfApproval: người duyệt khách hàng / đăng ký / gói hạn mức là người đề xuất (R-56).
	ErrSelfApproval = problem.New(http.StatusForbidden, "apiclient.self_approval", "Không được duyệt việc do chính mình đề xuất")
	// ErrTooManyKeys: đã có MaxActiveKeys khóa hiệu lực — thu hồi hoặc chờ hết hạn chồng lấn.
	ErrTooManyKeys = problem.New(http.StatusConflict, "apiclient.too_many_keys", "Khách hàng đã có tối đa số khóa hiệu lực")
	// ErrExportTooLarge: yêu cầu xuất vượt MaxExportRows của gói hạn mức.
	ErrExportTooLarge = problem.New(http.StatusUnprocessableEntity, "apiclient.export_too_large", "Yêu cầu xuất vượt số dòng tối đa của gói")
)
