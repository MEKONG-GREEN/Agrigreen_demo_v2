// Package port là hợp đồng của pf/export (`cap/export`, W10.17, ADR-0051) cho module khác và gốc lắp ráp (R-02: module
// khác chỉ import gói này và thư viện internal/shared/doctpl).
//
// Bên dùng (KHTC Mẫu 1–5 ở W10.17; CNTS BM-11, HCTH MS05/06, Mẫu KNQG ở gói sau):
//  1. Gốc lắp ráp đăng ký mẫu của bên dùng (TemplateRegistration) khi dựng pf/export.
//  2. Bên dùng tự kiểm quyền nghiệp vụ, dựng doctpl.Data rồi gọi Exporter.Export — pf/export ghi sổ phiên bản mẫu (lần
//     đầu dùng), cấp số văn bản (nếu yêu cầu và ô số để trống), điền dòng địa danh/ngày (nếu để trống), dựng .docx tất
//     định và ghi sổ lần xuất (byte + SHA-256) trong MỘT giao dịch.
//  3. Tải lại: Exporter.Run theo subject + mã lần xuất (kiểm băm byte trước khi trả).
package port

import (
	"context"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// TemplateRegistration là các mẫu một module đăng ký (gốc lắp ráp). Mỗi khóa có thể có nhiều phiên bản; Export không
// ghi phiên bản thì dùng phiên bản lớn nhất.
type TemplateRegistration struct {
	Owner     string // module, vd pf/extension
	Templates []doctpl.Template
}

// Numbering yêu cầu tự cấp số văn bản "{n}/<Series>" khi Data.Vars[Var] rỗng (sau khi bỏ khoảng trắng). n đánh lại từ 1
// mỗi năm (năm theo giờ Việt Nam lúc xuất); mỗi Subject nhận một số cho mỗi Series, các lần xuất sau dùng lại.
type Numbering struct {
	Series string // vd BC-TTKN
	Var    string // biến nhận số, vd so_van_ban
}

// DateLine yêu cầu điền dòng "<Place>, ngày dd tháng mm năm yyyy" (giờ Việt Nam lúc xuất) khi Data.Vars[Var] rỗng.
type DateLine struct {
	Var   string
	Place string
}

// Request là một yêu cầu xuất .docx.
type Request struct {
	Template string // khóa mẫu
	Version  int    // 0 = phiên bản lớn nhất đã đăng ký
	// Subject: "<bên dùng>:<mã đối tượng>" (vd "khtc_report:<mã kỳ>") — gom lịch sử lần xuất và số văn bản.
	Subject  string
	FileName string // tên tệp .docx, không đường dẫn
	Data     doctpl.Data
	Number   *Numbering
	Date     *DateLine
	IdemKey  string // khóa Idempotency-Key của request; cùng Subject + khóa → trả lần xuất cũ
}

// Run là một lần xuất đã ghi sổ. Body chỉ có ở Export và Run (không có ở danh sách).
type Run struct {
	ID              uuid.UUID
	Seq             int64 // lần xuất thứ n của Subject
	Subject         string
	Template        string
	TemplateVersion int
	TemplateHash    string
	InputHash       string
	DocNumber       string // số văn bản đã dùng (rỗng nếu không có)
	DocNumberAuto   bool
	FileName        string
	ContentType     string
	Body            []byte
	BodyHash        string
	Size            int64
	ActorRef        uuid.UUID
	At              time.Time
}

// Exporter là năng lực xuất văn bản. Người gọi đã xác thực (tenant, actor lấy từ ngữ cảnh); quyền nghiệp vụ do bên dùng
// kiểm trước khi gọi.
type Exporter interface {
	Export(ctx context.Context, req Request) (Run, error)
	// Run trả lần xuất (kèm byte, đã kiểm băm) thuộc subject.
	Run(ctx context.Context, subject string, id uuid.UUID) (Run, error)
	// Runs trả lịch sử lần xuất của subject (không byte), mới nhất trước.
	Runs(ctx context.Context, subject string) ([]Run, error)
}

// Access quyết định ai xem màn quản trị mẫu và lịch sử xuất của cả tenant (gốc lắp ráp cấu hình theo ứng dụng).
type Access interface {
	CanAdmin(ctx context.Context) bool
}

// AccessFunc chuyển hàm thành Access.
type AccessFunc func(ctx context.Context) bool

// CanAdmin thực hiện Access.
func (f AccessFunc) CanAdmin(ctx context.Context) bool { return f(ctx) }

var (
	subjectRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*:[A-Za-z0-9._:-]{1,150}$`)
	seriesRe   = regexp.MustCompile(`^[A-Z0-9Đ][A-Z0-9Đ-]{0,29}$`)
	fileNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,150}\.docx$`)
)

// ValidSubject, ValidSeries, ValidFileName kiểm định dạng (cùng CHECK của schema pf_export).
func ValidSubject(s string) bool  { return subjectRe.MatchString(s) }
func ValidSeries(s string) bool   { return seriesRe.MatchString(s) }
func ValidFileName(s string) bool { return fileNameRe.MatchString(s) }

// Lỗi của module (docs/errors/pf-export.md).
var (
	ErrTemplateNotFound = problem.New(http.StatusNotFound, "export.template_not_found", "Không có mẫu văn bản hoặc phiên bản này")
	ErrTemplateDrift    = problem.New(http.StatusConflict, "export.template_drift",
		"Nội dung mẫu đã đổi nhưng chưa tăng phiên bản — phiên bản đã dùng là bất biến")
	ErrInvalid     = problem.New(http.StatusUnprocessableEntity, "export.invalid", "Yêu cầu xuất văn bản không hợp lệ")
	ErrRunNotFound = problem.New(http.StatusNotFound, "export.run_not_found", "Không có lần xuất này")
	ErrCorrupt     = problem.New(http.StatusServiceUnavailable, "export.file_corrupt", "Tệp đã lưu không khớp băm")
	ErrUnavailable = problem.New(http.StatusServiceUnavailable, "export.dependency_unavailable", "Dịch vụ xuất văn bản chưa sẵn sàng")
)
