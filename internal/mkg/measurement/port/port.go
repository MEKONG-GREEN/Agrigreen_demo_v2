// Package port là cửa công khai DUY NHẤT của module mkg/measurement (R-02):
// ba điểm cân, phiếu cân dẫn xuất, đối soát, sổ thiết bị cân (tài liệu kiến trúc
// mục 13, 14; docs/design/mkg.md §5.2; ADR-0019).
//
// ĐÓNG BĂNG ở G3-0 để các đợt G3-1 làm song song: đổi chữ ký hoặc ngữ nghĩa của
// một interface dưới đây phải qua người tích hợp (docs/dev/g3-conventions.md §6).
// Thêm phương thức mới vào interface MỚI, không vào interface đã đóng băng.
//
// Nguyên tắc ngữ nghĩa chung:
//   - Mọi phương thức nhận tenantID tường minh và chạy trong giao dịch tenant
//     (tenant.WithTenantTx) — RLS là lớp chặn cuối (RB-2).
//   - Khối lượng là mass.Kg (numeric(14,3), R-10); không float.
//   - Thứ tự số đo là BỘ ĐẾM của thiết bị, không phải đồng hồ thiết bị (ADR-0019).
//   - Không có gì trong package này được qua van công bố: phiếu cân, số đo thô,
//     khối lượng chính xác của lô, dung sai, tranh chấp đều ở lại vùng mkg
//     (docs/design/mkg.md §3.1). Chỉ mrv_collection_evidence (khối lượng làm tròn
//     theo bậc) đi qua van.
package port

import (
	"context"
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Vai DB (docs/dev/g3-conventions.md §3).
const (
	// DBRole: vai của module ở cụm mkg (schema mkg_measurement).
	DBRole = "measurement_app"
	// IngestRole: vai của cmd/ingest ở cụm mkg — đọc sổ thiết bị, ghi phiếu cân dẫn xuất (R-70).
	IngestRole = "ingest_mkg"
	// IngestTSRole: vai của cmd/ingest ở cụm ts — INSERT + SELECT ts_weigh.
	IngestTSRole = "ingest_ts"
	// TSReaderRole: đọc chuỗi thời gian (tích phân lại, bằng chứng tranh chấp).
	TSReaderRole = "ts_reader"
)

// Vai nghiệp vụ (khai trong realm Keycloak; tài khoản tenant MKG).
const (
	// RoleScaleSupervisor: giám sát cân — đề xuất đăng ký/thu hồi thiết bị cân,
	// nhập phiếu CUSTOMER_IN tay có bằng chứng, xem số đo thô.
	RoleScaleSupervisor = "scale_supervisor"
	// RoleDeviceApprover: duyệt đăng ký thiết bị cân (khóa nguồn + chứng thư mTLS) —
	// không duyệt đề xuất của chính mình (R-56).
	RoleDeviceApprover = "device_approver"
	// RoleReconciliationOfficer: chạy / xác nhận đối soát HUB_IN → HUB_OUT → CUSTOMER_IN.
	RoleReconciliationOfficer = "reconciliation_officer"
	// RoleDisputeOfficer: mở, bổ sung bằng chứng, đóng hồ sơ tranh chấp thương mại.
	RoleDisputeOfficer = "dispute_officer"
	// RoleToleranceApprover: duyệt phiên bản bảng dung sai / định mức hao hụt sấy
	// (người đặt ≠ người duyệt, R-56).
	RoleToleranceApprover = "tolerance_approver"
)

// WeighPoint là điểm cân (mục 13.1).
type WeighPoint string

// Ba điểm cân.
const (
	PointHubIn      WeighPoint = "HUB_IN"      // MKG cân vào hub — agent biên
	PointHubOut     WeighPoint = "HUB_OUT"     // MKG cân ra hub — agent biên
	PointCustomerIn WeighPoint = "CUSTOMER_IN" // khách cân nhận — tích hợp hệ cân của khách HOẶC nhập tay có bằng chứng (C-5)
)

// Route là tuyến của lô.
type Route string

// Tuyến.
const (
	RouteHub    Route = "HUB"    // qua hub: đủ ba điểm, tách hao hụt nội bộ và chênh thương mại
	RouteDirect Route = "DIRECT" // đường 2 thẳng nhà máy: chỉ CUSTOMER_IN, KHÔNG tính hao hụt tách lớp; chỉ số sản lượng quy đổi đánh dấu gộp
)

// TicketSource là nguồn của phiếu cân.
type TicketSource string

// Nguồn phiếu cân.
const (
	SourceEdge                TicketSource = "edge"                 // tích phân từ ts_weigh.raw_reading bởi ingest (R-70) — HUB_IN, HUB_OUT
	SourceCustomerIntegration TicketSource = "customer_integration" // hệ cân của khách — CUSTOMER_IN (C-5)
	SourceManualEvidence      TicketSource = "manual_evidence"      // nhập tay kèm bằng chứng (ảnh phiếu cân) — khi cân hỏng hoặc CUSTOMER_IN chưa tích hợp; bắt buộc lý do
)

// Window là cửa sổ số đo dùng để tích phân một phiếu cân: một đoạn bộ đếm LIÊN
// TIẾP của một thiết bị trong một kỳ đăng ký. Cửa sổ theo bộ đếm, không theo đồng
// hồ thiết bị (ADR-0019); mốc mở/đóng cửa sổ lô do fieldops/hub cung cấp.
type Window struct {
	DeviceID    string
	Epoch       int
	CounterFrom int64
	CounterTo   int64 // ≥ CounterFrom
}

// Integration là kết quả tích phân một cửa sổ.
type Integration struct {
	Window Window
	// NetKg: khối lượng tịnh của cửa sổ theo thuật toán RuleVersion (≥ 0).
	NetKg mass.Kg
	// ReadingsUsed: số số đo thực sự dùng (vd chỉ số đo ổn định); ReadingsTotal: số số đo trong cửa sổ.
	ReadingsUsed  int
	ReadingsTotal int
	// BatchIDs: các ts_weigh.reading_batch chứa cửa sổ — khoảng raw_reading đã dùng, ghi vào phiếu (R-70).
	BatchIDs []uuid.UUID
	// RuleVersion: phiên bản thuật toán tích phân, vd "weigh.integrate.v1" — ghim vào phiếu.
	RuleVersion string
	// Gaps: bộ đếm bị thiếu trong cửa sổ (lô mất). Có Gaps → Integrate trả ErrWindowIncomplete.
	Gaps []int64
	// ReadingsSHA256: hex thường của SHA-256 trên dạng chuẩn của các số đo trong
	// cửa sổ (bổ sung G3-1, tương thích ngược; xem internal/mkg/measurement/ingest
	// WindowDigest) — ghi vào phiếu để xác minh lại khoảng raw_reading đã dùng (R-70).
	ReadingsSHA256 string
}

// Integrator tích phân luồng số đo thô thành khối lượng (R-70). CHỈ ingest gọi
// Integrator; không module nào khác tự tính khối lượng từ luồng thô.
//
// Integrate đọc ts_weigh.raw_reading của đúng cửa sổ (vai ts_reader hoặc
// ingest_ts), tất định: cùng số đo + cùng RuleVersion → cùng kết quả byte-cho-byte
// (tính lại được khi tranh chấp). Cửa sổ thiếu số đo → ErrWindowIncomplete kèm
// Gaps (không bao giờ nội suy). Thuật toán cụ thể phụ thuộc mẫu cân (C-6).
type Integrator interface {
	RuleVersion() string
	Integrate(ctx context.Context, tenantID uuid.UUID, w Window) (Integration, error)
}

// TicketDraft là yêu cầu ghi một phiếu cân.
type TicketDraft struct {
	// ID: UUIDv7 do bên gọi sinh (R-11); IdemKey: khóa idempotency trên sổ.
	ID      uuid.UUID
	IdemKey string
	LotRef  uuid.UUID // lô (mkg/hub) — UUID thuần (R-06)
	Point   WeighPoint
	Route   Route
	Source  TicketSource
	MassKg  mass.Kg // ≥ 0
	// MoisturePct: độ ẩm tại cân dạng decimal chuỗi "12.50" (numeric(5,2)); rỗng = không đo. Trạng thái ẩm khi cân chờ C-4.
	MoisturePct string
	// MoistureState (thêm ở G3-A2, tùy chọn — C-4 tạm): MoistureNotMeasured |
	// MoistureAsReceived | MoistureAfterDrying. Rỗng → suy ra từ MoisturePct.
	MoistureState string
	// SessionRef (thêm ở G3-A2, tùy chọn): phiên cân (weighing_session) mà cửa sổ thuộc về.
	SessionRef *uuid.UUID
	// Reason (thêm ở G3-A2): lý do nhập tay — bắt buộc (≥ 10 ký tự) khi Source = SourceManualEvidence.
	Reason string
	// EventTime: lúc cân tại hiện trường (R-13) — với SourceEdge là received_at của lô cuối cửa sổ.
	EventTime time.Time
	// Integration: bắt buộc khi Source = SourceEdge (R-70), nil với nguồn khác.
	Integration *Integration
	// EvidenceRef: tệp bằng chứng (ảnh phiếu, biên bản) — bắt buộc khi Source = SourceManualEvidence.
	EvidenceRef *uuid.UUID
	// CustomerRef: khách (chỉ CUSTOMER_IN). Danh tính khách không bao giờ qua van.
	CustomerRef *uuid.UUID
	ActorRef    uuid.UUID
	// Supersedes + CorrectionReason: phiếu bù sửa sai (RB-1) — cả hai cùng có hoặc cùng không.
	Supersedes       *uuid.UUID
	CorrectionReason string
}

// Ticket là phiếu cân đã ghi (sổ mkg_measurement.weigh_ticket, chuỗi băm `lot:<lot_ref>`).
type Ticket struct {
	TicketDraft
	Seq        int64
	RecordedAt time.Time
}

// TicketWriter ghi phiếu cân vào sổ (event sourcing mức B: sổ là nguồn).
//
// Record: một giao dịch ghi phiếu + cập nhật bảng chiếu lot_mass_current (R-21)
// + outbox sự kiện NỘI BỘ mkg.measurement.weigh_ticket.recorded (KindInternal,
// ADR-0018). Idempotent theo IdemKey: gọi lặp trả phiếu đã có, không ghi thêm.
// Từ chối (ErrInvalidTicket): SourceEdge thiếu Integration hoặc RuleVersion;
// SourceManualEvidence thiếu EvidenceRef hoặc lý do; CUSTOMER_IN từ SourceEdge;
// HUB_IN/HUB_OUT trên lô RouteDirect; Supersedes trỏ phiếu khác lô / khác điểm.
// Một lô có tối đa một phiếu HIỆU LỰC mỗi điểm cân; phiếu mới cùng điểm phải là
// phiếu bù (Supersedes).
type TicketWriter interface {
	Record(ctx context.Context, tenantID uuid.UUID, d TicketDraft) (Ticket, error)
}

// LotMassView là khối lượng chuẩn của lô theo phiếu cân hiệu lực (RB-6, Q-MKG-04).
type LotMassView struct {
	LotRef       uuid.UUID
	Route        Route
	HubInKg      *mass.Kg // nil = chưa có phiếu
	HubOutKg     *mass.Kg
	CustomerInKg *mass.Kg
	// TicketIDs theo điểm cân — phiếu hiệu lực (đã tính phiếu bù).
	TicketIDs map[WeighPoint]uuid.UUID
	// AsOf: recorded_at của phiếu mới nhất đã tính.
	AsOf time.Time
}

// LotMass cho hub / costing đọc khối lượng chuẩn của lô (đồng bộ, cùng tiến
// trình). ErrNotFound khi lô chưa có phiếu nào. Khối lượng chính xác của lô KHÔNG
// BAO GIỜ rời vùng mkg (van chỉ nhận khối lượng làm tròn theo bậc).
type LotMass interface {
	LotMass(ctx context.Context, tenantID, lotRef uuid.UUID) (LotMassView, error)
}

// DeviceStatus là trạng thái đăng ký thiết bị.
type DeviceStatus string

// Trạng thái thiết bị.
const (
	DeviceActive     DeviceStatus = "active"
	DeviceRevoked    DeviceStatus = "revoked"    // thu hồi: lô mới bị từ chối
	DeviceSuperseded DeviceStatus = "superseded" // có kỳ đăng ký mới hơn
)

// Device là một kỳ đăng ký thiết bị cân ĐÃ DUYỆT (sổ thiết bị, người đề xuất ≠
// người duyệt — R-56). Mỗi (DeviceID, Epoch) một bản ghi; khóa và chứng thư của
// một kỳ không đổi — thay khóa = kỳ mới.
type Device struct {
	TenantID    uuid.UUID
	DeviceID    string // trùng CN/SAN chứng thư mTLS client
	Epoch       int
	FacilityRef uuid.UUID // hub/kho (mkg/hub)
	Point       WeighPoint
	// PublicKey: khóa nguồn Ed25519 dùng edgewire.Verify; KeyID = edgewire.KeyID(PublicKey).
	PublicKey ed25519.PublicKey
	KeyID     string
	// CertSHA256: SHA-256 (hex thường) của chứng thư mTLS client DER đã duyệt.
	CertSHA256 string
	// ScaleModel: mẫu cân / bộ điều hợp (C-6).
	ScaleModel string
	Status     DeviceStatus
	ApprovedAt time.Time
}

// DeviceRegistry là sổ thiết bị đã duyệt — nguồn tin duy nhất của ingest.
//
// Resolve trả kỳ đăng ký (deviceID, epoch) nếu và chỉ nếu nó ĐANG hiệu lực VÀ
// certSHA256 trùng chứng thư đã duyệt của kỳ đó: ErrUnknownDevice (không có),
// ErrDeviceRevoked (thu hồi), ErrEpochSuperseded (có kỳ mới hơn — bộ đếm của kỳ
// cũ không bao giờ nhận thêm), ErrCertMismatch. Không cần tenant: ingest suy ra
// tenant từ thiết bị (tenant không bao giờ lấy từ lô, R-16).
type DeviceRegistry interface {
	Resolve(ctx context.Context, deviceID string, epoch int, certSHA256 string) (Device, error)
}

// Lỗi ổn định của port (ánh xạ problem.* mã measurement.* — docs/errors.md).
var (
	ErrNotFound         = errors.New("measurement: không tìm thấy")
	ErrInvalidTicket    = errors.New("measurement: phiếu cân không hợp lệ")
	ErrWindowIncomplete = errors.New("measurement: cửa sổ số đo thiếu bộ đếm")
	ErrUnknownDevice    = errors.New("measurement: thiết bị chưa đăng ký hoặc chưa duyệt")
	ErrDeviceRevoked    = errors.New("measurement: thiết bị đã thu hồi")
	ErrEpochSuperseded  = errors.New("measurement: kỳ đăng ký đã được thay")
	ErrCertMismatch     = errors.New("measurement: chứng thư mTLS không khớp đăng ký")
)
