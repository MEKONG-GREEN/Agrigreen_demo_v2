// Package domain: quy tắc thuần của Trang chủ phòng + kho văn bản phòng (W11.54 — D4 CN14; BRD CNTS v3.7 FN-17
// "Trang chủ & dashboard phòng" UR-TINH-06, FN-20 "Quy trình kỹ thuật, văn bản" UR-TINH-25; trang chủ tương ứng của các
// phòng Trồng trọt, KHTC, HCTH, Thông tin – Truyền thông). Không gọi time.Now (R-12), không phụ thuộc hạ tầng.
//
//   - Giới thiệu, chức năng nhiệm vụ của phòng: chuỗi phiên bản chỉ ghi thêm 'dept_profile:<phòng>' — Lãnh đạo phòng sửa
//     (FN-17 luồng phụ), Ban Giám đốc chỉ xem (BR-02).
//   - Kho văn bản: thư mục (chuỗi 'dept_folder:<ref>') và văn bản (chuỗi 'dept_doc:<ref>') có phiên bản; tệp ở kho đối
//     tượng (khóa theo băm, ghi một lần — KHÔNG lưu nội dung trong DB); xóa = ẩn (bản ghi bù), văn bản hết hiệu lực vẫn
//     lưu, tìm được, có nhãn "Hết hiệu lực" (BR-01, AC-03); tệp > 20 MB bị chặn (AC-02).
package domain

import (
	"bytes"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// RuleVersion ghi vào rule_version của ba sổ.
const RuleVersion = "kn.dept-home/v1"

// Sự việc của sổ giới thiệu phòng.
const EventProfileSaved = "saved"

// Sự việc của sổ thư mục.
const (
	FolderCreated  = "created"
	FolderRenamed  = "renamed"
	FolderHidden   = "hidden"
	FolderRestored = "restored"
)

// Sự việc của sổ văn bản.
const (
	DocCreated      = "created"       // phiên bản 1: thuộc tính + tệp
	DocUpdated      = "updated"       // sửa thuộc tính (giữ tệp)
	DocFileReplaced = "file_replaced" // tải tệp mới (phiên bản tệp + 1); tệp cũ vẫn tải được theo phiên bản
	DocHidden       = "hidden"        // "xóa" = ẩn, có lý do
	DocRestored     = "restored"
)

// Loại văn bản (UR-TINH-25: Nghị định, chính sách, kế hoạch, quy trình kỹ thuật, mẫu biểu dự án; prototype A
// resources.documents: Quy trình kỹ thuật, Văn bản chỉ đạo, Tài liệu tập huấn).
const (
	KindQuyTrinh  = "quy_trinh_ky_thuat"
	KindVanBan    = "van_ban_phap_quy" // Nghị định, thông tư, quyết định, chính sách
	KindChiDao    = "van_ban_chi_dao"
	KindKeHoach   = "ke_hoach"
	KindMauBieu   = "mau_bieu"
	KindTapHuan   = "tai_lieu_tap_huan"
	KindKhac      = "khac"
	MaxFolderDeep = 5
)

// Kinds theo thứ tự hiển thị.
var Kinds = []string{KindQuyTrinh, KindVanBan, KindChiDao, KindKeHoach, KindMauBieu, KindTapHuan, KindKhac}

// KindLabels là nhãn tiếng Việt của loại văn bản.
var KindLabels = map[string]string{
	KindQuyTrinh: "Quy trình kỹ thuật", KindVanBan: "Văn bản pháp quy, chính sách", KindChiDao: "Văn bản chỉ đạo",
	KindKeHoach: "Kế hoạch", KindMauBieu: "Mẫu biểu", KindTapHuan: "Tài liệu tập huấn", KindKhac: "Khác",
}

// Đối tượng được xem (FN-20 ma trận: Cấp Tỉnh CRUD, Cấp Xã R, Nông hộ R). Cấp Tỉnh của phòng luôn xem được.
const (
	AudienceXa     = "xa"      // Cấp Xã của phòng xem, tải
	AudienceNongHo = "nong_ho" // Nông hộ xem trên Mobile (GĐ2 — W11.55); lưu sẵn cờ
)

// Audiences là đối tượng chọn được.
var Audiences = []string{AudienceXa, AudienceNongHo}

// Giới hạn.
const (
	MaxIntro    = 4000
	MaxDuties   = 8000
	MaxName     = 200
	MaxTitle    = 300
	MaxSummary  = 2000
	MaxNumber   = 100
	MaxIssuer   = 200
	MaxField    = 100
	MaxReason   = 1000
	MinReason   = 5
	MaxFileName = 200
	// MaxFileBytes: UR-TINH-25 "Tệp PDF, DOCX ≤ 20 MB"; FN-20 AC-02 "Tệp trên 20 MB bị chặn".
	MaxFileBytes = 20 << 20
)

// Errors là danh sách lỗi theo trường.
type Errors []FieldError

// FieldError là một lỗi trường.
type FieldError struct{ Field, Reason string }

func (e *Errors) add(field, reason string) { *e = append(*e, FieldError{field, reason}) }

// CleanText bỏ khoảng trắng thừa ở hai đầu, chặn ký tự điều khiển (trừ xuống dòng, tab ở văn bản dài).
func CleanText(s string, multiline bool) (string, bool) {
	s = strings.TrimSpace(s)
	for _, r := range s {
		if unicode.IsControl(r) && (!multiline || (r != '\n' && r != '\r' && r != '\t')) {
			return s, false
		}
	}
	return s, true
}

// OneLine gộp khoảng trắng của tên/tiêu đề.
func OneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Profile là giới thiệu và chức năng nhiệm vụ của phòng.
type Profile struct {
	Intro  string
	Duties string
}

// Validate kiểm giới thiệu phòng.
func (p *Profile) Validate() Errors {
	var es Errors
	var ok bool
	if p.Intro, ok = CleanText(p.Intro, true); !ok || utf8.RuneCountInString(p.Intro) > MaxIntro {
		es.add("intro", "tối đa 4000 ký tự, không có ký tự điều khiển")
	}
	if p.Duties, ok = CleanText(p.Duties, true); !ok || utf8.RuneCountInString(p.Duties) > MaxDuties {
		es.add("duties", "tối đa 8000 ký tự, không có ký tự điều khiển")
	}
	if p.Intro == "" && p.Duties == "" {
		es.add("intro", "nhập giới thiệu hoặc chức năng nhiệm vụ")
	}
	return es
}

// FolderName chuẩn hóa tên thư mục.
func FolderName(s string) (string, bool) {
	s = OneLine(s)
	n := utf8.RuneCountInString(s)
	if n == 0 || n > MaxName {
		return s, false
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return s, false
		}
	}
	return s, true
}

// DocMeta là thuộc tính của văn bản (UR-TINH-25: loại, lĩnh vực, đối tượng, ngày ban hành, hiệu lực).
type DocMeta struct {
	Title         string
	Kind          string
	Field         string // lĩnh vực (gia súc, gia cầm, thủy sản, trồng trọt…) — chữ tự do
	Number        string // số hiệu
	Issuer        string // cơ quan ban hành
	Summary       string
	IssuedOn      *time.Time
	EffectiveFrom *time.Time
	EffectiveTo   *time.Time // nil = còn hiệu lực vô thời hạn
	Audience      []string
}

// Validate kiểm thuộc tính và chuẩn hóa (bỏ trùng, sắp đối tượng).
func (m *DocMeta) Validate() Errors {
	var es Errors
	m.Title = OneLine(m.Title)
	if n := utf8.RuneCountInString(m.Title); n == 0 || n > MaxTitle {
		es.add("title", "bắt buộc, tối đa 300 ký tự")
	}
	if !slices.Contains(Kinds, m.Kind) {
		es.add("kind", "một trong "+strings.Join(Kinds, ", "))
	}
	for _, f := range []struct {
		name string
		v    *string
		max  int
	}{{"field", &m.Field, MaxField}, {"number", &m.Number, MaxNumber}, {"issuer", &m.Issuer, MaxIssuer}} {
		*f.v = OneLine(*f.v)
		if utf8.RuneCountInString(*f.v) > f.max {
			es.add(f.name, "quá dài")
		}
	}
	var ok bool
	if m.Summary, ok = CleanText(m.Summary, true); !ok || utf8.RuneCountInString(m.Summary) > MaxSummary {
		es.add("summary", "tối đa 2000 ký tự")
	}
	if m.EffectiveFrom != nil && m.EffectiveTo != nil && m.EffectiveTo.Before(*m.EffectiveFrom) {
		es.add("effective_to", "ngày hết hiệu lực trước ngày có hiệu lực")
	}
	if m.IssuedOn != nil && m.EffectiveTo != nil && m.EffectiveTo.Before(*m.IssuedOn) {
		es.add("effective_to", "ngày hết hiệu lực trước ngày ban hành")
	}
	aud := []string{}
	for _, a := range m.Audience {
		if !slices.Contains(Audiences, a) {
			es.add("audience", "chỉ nhận "+strings.Join(Audiences, ", "))
			continue
		}
		if !slices.Contains(aud, a) {
			aud = append(aud, a)
		}
	}
	slices.Sort(aud)
	m.Audience = aud
	return es
}

// Expired: văn bản hết hiệu lực khi ngày hết hiệu lực trước hôm nay (BR-01: vẫn lưu, gắn nhãn).
func Expired(effectiveTo *time.Time, today time.Time) bool {
	return effectiveTo != nil && effectiveTo.Before(today)
}

// Trạng thái hiệu lực hiển thị.
const (
	ValidityActive  = "active"  // Còn hiệu lực
	ValidityPending = "pending" // Chưa có hiệu lực
	ValidityExpired = "expired" // Hết hiệu lực
)

// Validity suy trạng thái hiệu lực tại ngày today.
func Validity(from, to *time.Time, today time.Time) string {
	switch {
	case Expired(to, today):
		return ValidityExpired
	case from != nil && from.After(today):
		return ValidityPending
	}
	return ValidityActive
}

// fileFormats: đuôi tệp nhận được → kiểu nội dung và chữ ký đầu tệp (UR-TINH-25: PDF, DOCX; thêm XLSX cho mẫu biểu —
// giả định gia-dinh/W11.54.md).
var fileFormats = map[string]struct {
	ct    string
	magic []byte
}{
	".pdf":  {"application/pdf", []byte("%PDF-")},
	".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("PK\x03\x04")},
	".xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("PK\x03\x04")},
}

// FileExtensions là các đuôi tệp nhận được.
var FileExtensions = []string{".pdf", ".docx", ".xlsx"}

// FileType kiểm tên tệp và nội dung (không tin Content-Type của máy khách): đuôi trong danh sách, chữ ký đầu tệp khớp.
// Trả kiểu nội dung chuẩn và tên tệp đã làm sạch (bỏ thư mục, ký tự điều khiển, dấu nháy).
func FileType(fileName string, head []byte) (ct, name string, ok bool) {
	name = path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, OneLine(name))
	if name == "." || name == "/" || name == "" {
		return "", "", false
	}
	if utf8.RuneCountInString(name) > MaxFileName {
		ext := path.Ext(name)
		if utf8.RuneCountInString(ext) > 10 {
			ext = ""
		}
		name = string([]rune(strings.TrimSuffix(name, ext))[:MaxFileName-utf8.RuneCountInString(ext)]) + ext
	}
	f, found := fileFormats[strings.ToLower(path.Ext(name))]
	if !found || !bytes.HasPrefix(head, f.magic) {
		return "", name, false
	}
	return f.ct, name, true
}
