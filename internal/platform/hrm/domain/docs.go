package domain

// W11.24 — KHO TÀI LIỆU NHÂN SỰ (BRD HCTH v3.0 FN-14, UR-HC-25, UR-ADM-10): cây thư mục, phân loại tài liệu (loại quyết
// định, năm, phòng ban, nhân viên), quyền theo thư mục. Quyền dùng CÙNG ma trận FN-20 của W11.22 (perm.go) với hai nhóm
// quyền tài liệu; thư mục có thể ghi đè (acl) cho nhóm vai hoặc người dùng — thư mục con kế thừa, người dùng ưu tiên hơn
// nhóm, Admin cố định toàn quyền.

import (
	"bytes"
	"encoding/json"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Nhóm quyền tài liệu (FN-20 "quản lý tài liệu"; FN-14 §VI.2: HCTH, Admin CRUD; lãnh đạo phòng R phòng mình).
const (
	PermDocView   Perm = "docs.view"   // xem danh sách, xem trước, tải xuống
	PermDocManage Perm = "docs.manage" // tạo/đổi tên thư mục con, tải lên, sửa phân loại, ẩn tài liệu
)

// DocPerms là các nhóm quyền chỉnh được ở quyền thư mục.
var DocPerms = []Perm{PermDocView, PermDocManage}

// Loại tài liệu (FN-14: quyết định nâng bậc, điều động, bổ nhiệm, khen thưởng, kỷ luật, hồ sơ đánh giá, tài liệu khác).
const (
	DocNangBac    = "nang_bac"
	DocDieuDong   = "dieu_dong"
	DocBoNhiem    = "bo_nhiem"
	DocKhenThuong = "khen_thuong"
	DocKyLuat     = "ky_luat"
	DocDanhGia    = "danh_gia"
	DocKhac       = "khac"
)

// DocTypes theo thứ tự hiển thị.
var DocTypes = []string{DocNangBac, DocDieuDong, DocBoNhiem, DocKhenThuong, DocKyLuat, DocDanhGia, DocKhac}

// DefaultFolder là một thư mục gốc mặc định (Admin cài, đổi tên được). Key là system_key cho nghiệp vụ tự lưu tệp.
type DefaultFolder struct {
	Key, Name, DocType string
}

// DefaultFolders: cấu trúc thư mục gốc mặc định theo FN-14 (mô tả) — "Quyết định nâng bậc" nhận QĐ của FN-10 (W11.25),
// "Hồ sơ đánh giá" nhận bản ký MS05/06 (W11.26).
var DefaultFolders = []DefaultFolder{
	{"nang_bac", "Quyết định nâng bậc", DocNangBac},
	{"dieu_dong", "Quyết định điều động", DocDieuDong},
	{"bo_nhiem", "Quyết định bổ nhiệm", DocBoNhiem},
	{"khen_thuong", "Quyết định khen thưởng", DocKhenThuong},
	{"ky_luat", "Quyết định kỷ luật", DocKyLuat},
	{"danh_gia", "Hồ sơ đánh giá", DocDanhGia},
	{"khac", "Tài liệu nhân sự khác", DocKhac},
}

// MaxFolderDepth: số cấp thư mục tối đa (gốc = 1).
const MaxFolderDepth = 6

// MaxDocBytes: dung lượng tối đa của một tệp (bản rõ) — bản mã phải ≤ 15 MiB của pf/evidence.
const MaxDocBytes = 15<<20 - 1024

// NormalizeName chuẩn hóa tên thư mục/tài liệu: bỏ khoảng trắng thừa; cấm ký tự điều khiển và '/' '\' ở tên thư mục.
func NormalizeName(s string, max int, folder bool) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	n := utf8.RuneCountInString(s)
	if n == 0 || n > max {
		return "", invalid("tên bắt buộc, tối đa %d ký tự", max)
	}
	for _, r := range s {
		if unicode.IsControl(r) || (folder && (r == '/' || r == '\\')) {
			return "", invalid("tên có ký tự không cho phép")
		}
	}
	return s, nil
}

// ValidDocType kiểm loại tài liệu.
func ValidDocType(t string) bool { return contains(DocTypes, t) }

// docFormats: đuôi tệp được nhận → kiểu nội dung và chữ ký đầu tệp (FN-14 luồng ngoại lệ: định dạng không cho phép → chặn).
var docFormats = map[string]struct {
	ct    string
	magic []byte
}{
	".pdf":  {"application/pdf", []byte("%PDF-")},
	".jpg":  {"image/jpeg", []byte{0xFF, 0xD8, 0xFF}},
	".jpeg": {"image/jpeg", []byte{0xFF, 0xD8, 0xFF}},
	".png":  {"image/png", []byte{0x89, 'P', 'N', 'G'}},
	".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("PK\x03\x04")},
	".xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("PK\x03\x04")},
	".doc":  {"application/msword", []byte{0xD0, 0xCF, 0x11, 0xE0}},
	".xls":  {"application/vnd.ms-excel", []byte{0xD0, 0xCF, 0x11, 0xE0}},
}

// DocExtensions là các đuôi tệp được nhận (sắp theo bảng).
var DocExtensions = []string{".pdf", ".jpg", ".jpeg", ".png", ".docx", ".xlsx", ".doc", ".xls"}

// DocContentType kiểm tên tệp và nội dung: đuôi trong danh sách, chữ ký đầu tệp khớp đuôi. Trả kiểu nội dung chuẩn và tên
// tệp đã làm sạch (bỏ thư mục, ký tự điều khiển).
func DocContentType(fileName string, head []byte) (ct, name string, err error) {
	name = path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	name, err = NormalizeName(name, 200, true)
	if err != nil {
		return "", "", invalid("tên tệp: %v", err)
	}
	f, ok := docFormats[strings.ToLower(path.Ext(name))]
	if !ok {
		return "", "", invalid("định dạng không cho phép — chỉ nhận %s", strings.Join(DocExtensions, ", "))
	}
	if !bytes.HasPrefix(head, f.magic) {
		return "", "", invalid("nội dung tệp không đúng định dạng %s", path.Ext(name))
	}
	return f.ct, name, nil
}

// DocContentTypes là các kiểu nội dung có thể có của tài liệu (cho pf/evidence).
func DocContentTypes() []string {
	seen, out := map[string]bool{}, []string{}
	for _, e := range DocExtensions {
		if ct := docFormats[e].ct; !seen[ct] {
			seen[ct] = true
			out = append(out, ct)
		}
	}
	return out
}

// FolderACL là quyền riêng của một thư mục: "role:<nhóm vai>" | "user:<uuid>" → nhóm quyền tài liệu → phạm vi.
type FolderACL map[string]Matrix

// ParseFolderACL kiểm quyền thư mục: khóa là nhóm vai (không phải Admin) hoặc người dùng; chỉ nhóm quyền tài liệu.
func ParseFolderACL(raw []byte) (FolderACL, []byte, error) {
	var in map[string]map[string]string
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	if err := decodeStrict(raw, &in); err != nil {
		return nil, nil, invalid("quyền thư mục: %v", err)
	}
	if len(in) > 100 {
		return nil, nil, invalid("quyền thư mục: tối đa 100 dòng")
	}
	out := FolderACL{}
	for key, perms := range in {
		kind, grantee, ok := strings.Cut(key, ":")
		if !ok {
			return nil, nil, invalid("khóa %q phải là role:<nhóm> hoặc user:<uuid>", key)
		}
		switch kind {
		case GranteeRole:
			if !contains(Groups, grantee) || grantee == GroupAdmin {
				return nil, nil, invalid("nhóm vai %q (quyền của Admin cố định)", grantee)
			}
		case GranteeUser:
			if !reUserRef.MatchString(grantee) {
				return nil, nil, invalid("người dùng phải là UUID chữ thường")
			}
		default:
			return nil, nil, invalid("khóa %q phải là role:<nhóm> hoặc user:<uuid>", key)
		}
		m := Matrix{}
		for k, v := range perms {
			p, s := Perm(k), Scope(v)
			if p != PermDocView && p != PermDocManage {
				return nil, nil, invalid("quyền thư mục chỉ nhận docs.view, docs.manage (không %q)", k)
			}
			if !containsScope(Scopes, s) {
				return nil, nil, invalid("phạm vi %q", v)
			}
			m[p] = s
		}
		out[key] = m
	}
	canon, err := json.Marshal(out)
	return out, canon, err
}

// FolderMatrix là quyền tài liệu hiệu lực ở một thư mục: ma trận FN-20 của người gọi (base) ← quyền của từng thư mục từ
// gốc xuống (chain; nhóm vai rồi người dùng — người dùng ưu tiên). Admin luôn toàn quyền.
func FolderMatrix(base Matrix, group, user string, chain []FolderACL) Matrix {
	m := Matrix{}
	for _, p := range DocPerms {
		m[p] = base[p]
		if m[p] == "" {
			m[p] = ScopeNone
		}
	}
	if group == GroupAdmin {
		return m
	}
	for _, acl := range chain {
		for _, key := range []string{GranteeRole + ":" + group, GranteeUser + ":" + user} {
			for p, s := range acl[key] {
				m[p] = s
			}
		}
	}
	return m
}

// DocRelation là quan hệ của tài liệu với người gọi để xét phạm vi: bản thân (tài liệu gắn hồ sơ của người gọi), đơn vị
// (phòng ban của tài liệu là đơn vị nội bộ của người gọi).
func DocRelation(staffIDs []string, mine map[string]bool, unitCode, callerUnit string) (self, unit bool) {
	for _, id := range staffIDs {
		if mine[id] {
			self = true
			break
		}
	}
	return self, callerUnit != "" && unitCode == callerUnit
}
