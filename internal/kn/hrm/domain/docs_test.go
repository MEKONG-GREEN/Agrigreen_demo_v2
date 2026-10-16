package domain

import (
	"strings"
	"testing"
)

// W11.24: định dạng tệp (đuôi + chữ ký đầu tệp), quyền thư mục (ghi đè ma trận FN-20, kế thừa, người dùng ưu tiên, Admin
// cố định), mặc định lãnh đạo phòng xem tài liệu đơn vị mình.
func TestDocsDomain_W1124(t *testing.T) {
	ct, name, err := DocContentType(`C:\fakepath\QĐ 12-2026.pdf`, []byte("%PDF-1.7 ..."))
	if err != nil || ct != "application/pdf" || name != "QĐ 12-2026.pdf" {
		t.Fatalf("pdf: %q %q %v", ct, name, err)
	}
	if ct, _, err := DocContentType("bang.XLSX", []byte("PK\x03\x04....")); err != nil || !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("xlsx: %q %v", ct, err)
	}
	for _, c := range []struct {
		name string
		head string
	}{{"virus.exe", "MZ"}, {"gia.pdf", "PK\x03\x04"}, {"khong-duoi", "%PDF-"}, {"a\x01.pdf", "%PDF-"}} {
		if _, _, err := DocContentType(c.name, []byte(c.head)); err == nil {
			t.Errorf("%q phải bị chặn", c.name)
		}
	}
	if len(DocContentTypes()) != 7 {
		t.Fatalf("kiểu nội dung: %v", DocContentTypes())
	}

	if m := DefaultMatrix(GroupManager); m[PermDocView] != ScopeUnit || m[PermDocManage] != ScopeNone {
		t.Fatalf("lãnh đạo phòng: %v", m)
	}
	if m := DefaultMatrix(GroupHCTH); m[PermDocView] != ScopeAll || m[PermDocManage] != ScopeAll {
		t.Fatalf("HCTH: %v", m)
	}
	if m := DefaultMatrix(GroupTinh); m[PermDocView] != ScopeNone {
		t.Fatalf("Cấp Tỉnh khác: %v", m)
	}
	if _, _, err := ParseMatrix(GranteeRole, GroupTinh, []byte(`{"docs.view":"unit"}`)); err != nil {
		t.Fatalf("ma trận FN-20 nhận nhóm quyền tài liệu: %v", err)
	}

	user := "0190a0a0-0000-7000-8000-000000000001"
	acl, canon, err := ParseFolderACL([]byte(`{"role:manager":{"docs.view":"none"},"user:` + user + `":{"docs.view":"all"}}`))
	if err != nil || len(acl) != 2 || !strings.Contains(string(canon), "role:manager") {
		t.Fatalf("acl: %v %s %v", acl, canon, err)
	}
	for _, bad := range []string{`{"role:admin":{"docs.view":"none"}}`, `{"role:x":{}}`, `{"user:ABC":{}}`, `{"manager":{}}`,
		`{"role:hcth":{"profile.view":"all"}}`, `{"role:hcth":{"docs.view":"most"}}`, `[]`} {
		if _, _, err := ParseFolderACL([]byte(bad)); err == nil {
			t.Errorf("acl %s phải bị từ chối", bad)
		}
	}
	if _, canon, err := ParseFolderACL(nil); err != nil || string(canon) != "{}" {
		t.Fatalf("acl rỗng: %s %v", canon, err)
	}

	base := DefaultMatrix(GroupManager)
	// Gốc chặn lãnh đạo phòng; thư mục con mở lại cho một người.
	chain := []FolderACL{{"role:manager": {PermDocView: ScopeNone}}, {"user:" + user: {PermDocView: ScopeAll}}}
	if m := FolderMatrix(base, GroupManager, "khac", chain); m[PermDocView] != ScopeNone {
		t.Fatalf("thư mục con kế thừa chặn: %v", m)
	}
	if m := FolderMatrix(base, GroupManager, user, chain); m[PermDocView] != ScopeAll {
		t.Fatalf("người dùng ưu tiên: %v", m)
	}
	if m := FolderMatrix(DefaultMatrix(GroupAdmin), GroupAdmin, user, []FolderACL{{"role:admin": {PermDocView: ScopeNone}}}); m[PermDocView] != ScopeAll {
		t.Fatalf("Admin cố định: %v", m)
	}
	if self, unit := DocRelation([]string{"a", "b"}, map[string]bool{"b": true}, "trong_trot", "chan_nuoi"); !self || unit {
		t.Fatal("quan hệ bản thân")
	}
	if _, err := NormalizeName("  Quyết   định  ", 120, true); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeName("a/b", 120, true); err == nil {
		t.Fatal("tên thư mục có '/' phải bị chặn")
	}
}
