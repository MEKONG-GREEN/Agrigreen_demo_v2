package port

import "github.com/google/uuid"

// ---------------------------------------------------------------------------
// G3-2: phía HTX của thu gom rơm MKG (g3-conventions §4, §5).
//
// MKG → nền tảng chỉ qua van công bố (contracts/publication/collection_schedule.json,
// htx_collection_statement.json — người nhận pf/cropcycle); tên sự kiện và hợp đồng
// khai lại ở đây vì vùng nền tảng không import mkg (R-03). Nền tảng → MKG chỉ qua
// Application API công khai: MKG (tenant riêng, ADR-0020) kéo phản hồi của HTX bằng tài khoản
// dịch vụ vai RoleMKGSync qua bề mặt đối tác GET /v1/partner/htx-collection/responses (bí danh
// /v1/cropcycle/htx-collection/responses) sau khi kiểm cấp quyền chia sẻ htx_collection_responses
// — không có hàng đợi pf → mkg.
// ---------------------------------------------------------------------------

// RoleMKGSync là vai của tài khoản dịch vụ MKG (client agrigreen-mkg-sync, client
// credentials) đọc bề mặt đối tác /v1/partner/* (ADR-0020). Luồng phản hồi chỉ gồm phản hồi thuộc
// bảng kê do chính tenant của tài khoản gửi qua van; không đọc lịch gom, bảng kê hay dữ liệu khác.
const RoleMKGSync = "mkg_sync"

// Sự kiện van MKG (tên chốt ở contracts/publication/*.json).
const (
	EventMKGCollectionSchedulePublished     = "mkg.fieldops.collection_schedule.published"
	EventMKGHTXCollectionStatementPublished = "mkg.fieldops.htx_collection_statement.published"
	ContractCollectionSchedule              = "collection_schedule"
	ContractHTXCollectionStatement          = "htx_collection_statement"
)

// CollectionValveConsumer là consumer của worker nhận hai sự kiện van, kiểm payload
// bằng bộ kiểm hợp đồng lúc chạy (contracts/publication), ghi sổ nhận + bảng chiếu
// (idempotent theo ref + revision, bản revision lớn nhất là hiện hành).
const CollectionValveConsumer = "pf.cropcycle.htx_collection_valve"

// CollectionValveEvents là các sự kiện consumer CollectionValveConsumer đăng ký.
var CollectionValveEvents = []string{EventMKGCollectionSchedulePublished, EventMKGHTXCollectionStatementPublished}

// Entity đồng bộ ngoại tuyến của App HTX (quản lý HTX).
const (
	EntityCollectionSchedule  = "htx_collection_schedule"  // chiều xuống: lịch gom của HTX
	EntityCollectionStatement = "htx_collection_statement" // chiều xuống: bảng kê số cuộn + phản hồi hiện hành
	EntityCollectionResponse  = "htx_collection_response"  // chiều lên: xác nhận / tranh chấp (op create)
)

// Kết quả phản hồi của HTX với một bảng kê.
const (
	ResponseConfirmed = "confirmed"
	ResponseDisputed  = "disputed"
)

// Sự kiện nền tảng phát khi nhận bản mới của van (pf/notify báo quản lý HTX).
const (
	EventCollectionScheduleReceived  = "pf.cropcycle.collection_schedule.received"
	EventCollectionStatementReceived = "pf.cropcycle.collection_statement.received"
)

// CollectionScheduleReceivedV1 là payload khi lịch gom của HTX mới có hoặc đổi
// (revision mới trở thành hiện hành). Chỉ khung thời gian và trạng thái.
//
//ag:event pf.cropcycle.collection_schedule.received v1
type CollectionScheduleReceivedV1 struct {
	ScheduleRef    uuid.UUID `json:"schedule_ref"`
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	CommuneCode    string    `json:"commune_code"`
	WindowFrom     string    `json:"window_from"`
	WindowTo       string    `json:"window_to"`
	Status         string    `json:"status"`
	Revision       int       `json:"revision"`
	First          bool      `json:"first"` // lần đầu nhận lịch này
}

// CollectionStatementReceivedV1 là payload khi bảng kê số cuộn của một thửa mới có
// hoặc có bản sửa — HTX cần xác nhận hoặc tranh chấp theo revision này.
//
//ag:event pf.cropcycle.collection_statement.received v1
type CollectionStatementReceivedV1 struct {
	StatementRef   uuid.UUID `json:"statement_ref"`
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	PlotRef        uuid.UUID `json:"plot_ref"`
	CollectedOn    string    `json:"collected_on"`
	Rolls          int       `json:"rolls"`
	Revision       int       `json:"revision"`
	First          bool      `json:"first"`
}
