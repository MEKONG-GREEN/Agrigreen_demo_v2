// Package port là hợp đồng của pf/evidence (`cap/evidence`) cho module khác (R-02): tệp minh chứng đã mã hóa
// phong bì (schema pf_files), bộ ba chủ thể (subject_module, subject_type, subject_id) của R-DB-07 và cổng mở
// tệp có mục đích, có nhật ký (Open(purpose), kiến trúc v4.1 §18, §26).
package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
)

// Giới hạn tệp minh chứng.
const (
	MaxFileBytes = 15 << 20 // 15 MiB bản mã mỗi tệp (docs/dev/sync-contract.md)
	MinFileBytes = envelope.TagSize
	// MaxUploadBody là giới hạn thân multipart của POST /v1/files (bản mã + metadata + biên).
	MaxUploadBody = MaxFileBytes + 256<<10
)

// Subject là bộ ba tham chiếu chéo module (R-DB-07): module chủ, loại thực thể, mã thực thể. Không có khóa
// ngoại xuyên schema: module chủ phân giải qua Resolver của nó.
type Subject struct {
	Module string    // tên module.yaml của module chủ, vd pf/cooperative
	Type   string    // loại thực thể module chủ cho tham chiếu, vd plot
	ID     uuid.UUID // mã thực thể
}

// File là siêu dữ liệu một tệp minh chứng đã mã hóa phong bì. Nội dung chỉ nằm ở kho đối tượng dưới dạng
// bản mã (R-40).
type File struct {
	TenantID         uuid.UUID
	Ref              uuid.UUID
	SubjectRef       uuid.UUID // chủ thể có khóa công khai bọc DEK (hộ ở kho định danh)
	V                int
	Alg              string
	Nonce            []byte
	WrappedDEK       []byte
	SHA256Ciphertext string
	Size             int64
	ContentType      string
	// CapturedAt và GPS là dữ liệu cá nhân khi gắn với hộ: không log, không đưa vào sự kiện.
	CapturedAt   *time.Time `pii:"true"`
	GPSLat       *float64   `pii:"true"`
	GPSLon       *float64   `pii:"true"`
	GPSAccuracyM *float64
	ObjectKey    string
	UploadedBy   uuid.UUID
	DeviceID     *uuid.UUID
	// Subject: bộ ba R-DB-07 (pf 000123); nil ở tệp cũ và tệp tải trước khi biết thực thể.
	Subject   *Subject
	CreatedAt time.Time
}

// AttachInput là một tệp máy gửi lên: metadata phong bì và BẢN MÃ.
type AttachInput struct {
	FileRef  *string // tùy chọn: UUIDv7 máy sinh để bản ghi ngoại tuyến tham chiếu trước khi tải
	Meta     envelope.Metadata
	Blob     []byte
	DeviceID *uuid.UUID // thiết bị đã kiểm (pf/sync)
	Subject  *Subject   // tùy chọn: phân giải qua Resolver của module chủ trước khi ghi
}

// Opened là siêu dữ liệu kèm URL tải bản mã có hạn.
type Opened struct {
	File      File
	URL       string
	ExpiresAt time.Time
}

// Files là cổng của pf/evidence. Người gọi lấy từ context (authz.Principal); tenant của người gọi.
type Files interface {
	// Attach lưu tệp đã mã hóa (kiểm phong bì, băm bản mã, chủ thể qua Resolver). Gửi lại cùng file_ref,
	// cùng bản mã → bản cũ, created = false.
	Attach(ctx context.Context, in AttachInput) (f File, created bool, err error)
	// Open cấp siêu dữ liệu và URL tải bản mã: bắt buộc mục đích, vai xem minh chứng, người gọi thấy được
	// chủ thể (Resolver của module chủ + phạm vi), đồng ý của hộ. Mọi lần quyết định ghi sổ pf_files.access_log.
	Open(ctx context.Context, ref uuid.UUID, purpose string) (Opened, error)
	// OpenContent như Open, rồi mở DEK qua kho định danh và trả bản rõ (chỉ trong bộ nhớ).
	OpenContent(ctx context.Context, ref uuid.UUID, purpose string) (contentType string, plaintext []byte, err error)
}

// ---------------------------------------------------------------------------
// Interface do pf/evidence CẦN, module chủ hiện thực (R-DB-07) — nối ở gốc lắp ráp (module năng lực không
// import module domain, mẫu pf/workflow ↔ pf/party).
// ---------------------------------------------------------------------------

// Resolver phân giải chủ thể của MỘT module chủ. Tenant lấy từ context (tenant.WithID).
type Resolver interface {
	// Resolve trả chủ thể; không có hoặc ngoài tenant → ErrSubjectNotFound; loại không hỗ trợ →
	// ErrSubjectTypeUnsupported; nguồn chưa nối → ErrResolverUnavailable.
	Resolve(ctx context.Context, subjectType string, id uuid.UUID) (Resolved, error)
}

// Resolved là chủ thể đã phân giải — đủ để kiểm phạm vi người xem. Không có dữ liệu cá nhân.
type Resolved struct {
	CommuneCode   string    // đơn vị hành chính của thực thể; rỗng → chỉ phạm vi toàn quốc thấy
	NodeRef       uuid.UUID // Nút phạm vi (HTX); uuid.Nil nếu không có
	ViaShareGrant bool      // thấy qua cấp phép chia sẻ (W4.2)
}

// Lỗi của Resolver.
var (
	ErrSubjectNotFound        = errors.New("evidence: không có chủ thể")
	ErrSubjectTypeUnsupported = errors.New("evidence: loại chủ thể không hỗ trợ")
	ErrResolverUnavailable    = errors.New("evidence: nguồn phân giải chủ thể chưa nối")
)
