package app_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W11.22 (vault 000004): ngăn hồ sơ có khóa riêng, gắn cứng mục đích; sửa định danh; sổ truy cập; xóa định danh xóa ngăn;
// MFA cho ngăn nhạy cảm; RLS theo tenant.
func TestHRMProfileSections_W1122(t *testing.T) {
	s, env := newService(t)
	s.RequireMFASensitive = true
	tid := ids.New()
	ctx := as(tid, true, client.RoleRegistrar, client.RoleEraser)
	staff := client.Identity{Kind: client.SubjectPerson, FullName: "Trần Thị Mai", Phone: "0912000111", Address: "Long Xuyên", AreaCode: "hrm"}
	c, err := s.Create(ctx, client.PurposeHRMProfile, staff)
	if err != nil {
		t.Fatal(err)
	}
	prof := client.Fields{"birth_date": "1988-05-01", "birth_precision": "day", "gender": "nu", "email": "mai@ttkn.vn"}
	sens := client.Fields{"ethnicity": "kinh", "religion": "khong", "party_date": "2015-02-03", "hometown": "Xã Mỹ Hòa Hưng"}
	if err := s.PutSection(ctx, client.PurposeHRMProfile, c.Ref, client.SectionHRMProfile, prof); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSection(ctx, client.PurposeHRMSensitive, c.Ref, client.SectionHRMSensitive, sens); err != nil {
		t.Fatal(err)
	}

	// Ngăn gắn cứng mục đích: đọc/ghi ngăn nhạy cảm bằng hrm.profile bị từ chối; mục đích ngoài danh mục bị từ chối;
	// hrm.sensitive không đọc được định danh.
	if _, err := s.GetSection(ctx, client.PurposeHRMProfile, c.Ref, client.SectionHRMSensitive); code(err) != client.CodeSectionInvalid {
		t.Fatalf("ngăn nhạy cảm với mục đích hrm.profile: %v", err)
	}
	if err := s.PutSection(ctx, client.PurposeHRMSensitive, c.Ref, client.SectionHRMProfile, prof); code(err) != client.CodeSectionInvalid {
		t.Fatalf("ghi ngăn hồ sơ với hrm.sensitive: %v", err)
	}
	if _, err := s.GetSection(ctx, client.PurposeRegistryAdmin, c.Ref, client.SectionHRMProfile); code(err) != "validation_failed" {
		t.Fatalf("mục đích ngoài danh mục: %v", err)
	}
	if _, err := s.Resolve(ctx, client.PurposeHRMSensitive, c.Ref); code(err) != "validation_failed" {
		t.Fatalf("resolve bằng hrm.sensitive: %v", err)
	}
	if err := s.PutSection(ctx, client.PurposeHRMProfile, c.Ref, client.SectionHRMProfile, client.Fields{"salary": "1"}); code(err) != client.CodeSectionInvalid {
		t.Fatalf("trường lạ: %v", err)
	}
	if _, err := s.GetSection(ctx, client.PurposeHRMProfile, c.Ref, "hrm.salary"); code(err) != client.CodeSectionInvalid {
		t.Fatalf("ngăn lạ: %v", err)
	}
	// Vai: chỉ vault_registrar; ngăn nhạy cảm cần MFA, ngăn hồ sơ không.
	if _, err := s.GetSection(as(tid, true, client.RoleDisplay), client.PurposeHRMProfile, c.Ref, client.SectionHRMProfile); code(err) != "forbidden" {
		t.Fatalf("vai display: %v", err)
	}
	noMFA := as(tid, false, client.RoleRegistrar)
	if _, err := s.GetSection(noMFA, client.PurposeHRMSensitive, c.Ref, client.SectionHRMSensitive); code(err) != "mfa_required" {
		t.Fatalf("ngăn nhạy cảm không MFA: %v", err)
	}
	if got, err := s.GetSection(noMFA, client.PurposeHRMProfile, c.Ref, client.SectionHRMProfile); err != nil || got["email"] != "mai@ttkn.vn" {
		t.Fatalf("ngăn hồ sơ: %v %v", got, err)
	}
	if got, err := s.GetSection(ctx, client.PurposeHRMSensitive, c.Ref, client.SectionHRMSensitive); err != nil || got["hometown"] != "Xã Mỹ Hòa Hưng" || len(got) != 4 {
		t.Fatalf("ngăn nhạy cảm: %v %v", got, err)
	}
	// Tenant khác không thấy chủ thể (RLS).
	if _, err := s.GetSection(as(ids.New(), true, client.RoleRegistrar), client.PurposeHRMProfile, c.Ref, client.SectionHRMProfile); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("tenant khác: %v", err)
	}

	// Bản mã: không chứa giá trị rõ; mỗi ngăn một khóa bọc riêng.
	admin := env.AdminPool(t, migrations.Vault)
	rows, err := admin.Query(context.Background(), "SELECT section, payload_ct, wrapped_key FROM vault.section WHERE tenant_id = $1 ORDER BY section", tid)
	if err != nil {
		t.Fatal(err)
	}
	var keys [][]byte
	for rows.Next() {
		var sec string
		var ct, wk []byte
		if err := rows.Scan(&sec, &ct, &wk); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(ct, []byte("kinh")) || bytes.Contains(ct, []byte("mai@ttkn.vn")) {
			t.Fatalf("ngăn %s lưu giá trị rõ", sec)
		}
		keys = append(keys, wk)
	}
	rows.Close()
	if len(keys) != 2 || bytes.Equal(keys[0], keys[1]) {
		t.Fatalf("hai ngăn phải có hai khóa bọc riêng: %d", len(keys))
	}

	// Amend: đổi họ tên/SĐT; SĐT của chủ thể khác → trùng; tra theo SĐT mới ra chủ thể.
	other, err := s.Create(ctx, client.PurposeHRMProfile, client.Identity{Kind: client.SubjectPerson, FullName: "Lê Văn Bình",
		Phone: "0912000222", AreaCode: "hrm"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Amend(ctx, client.PurposeHRMProfile, c.Ref, client.Identity{FullName: "Trần Thị Mai", Phone: "0912000222"}); !errors.Is(err, client.ErrDuplicate) {
		t.Fatalf("SĐT trùng: %v", err)
	}
	if err := s.Amend(ctx, client.PurposeRegistryAdmin, c.Ref, client.Identity{FullName: "X"}); code(err) != "validation_failed" {
		t.Fatalf("amend mục đích khác: %v", err)
	}
	if err := s.Amend(ctx, client.PurposeHRMProfile, c.Ref, client.Identity{FullName: "Trần  Thị Mai ", Phone: "0912000333", Address: "Châu Đốc"}); err != nil {
		t.Fatal(err)
	}
	if ref, err := s.Lookup(ctx, client.PurposeHRMProfile, "0912000333"); err != nil || ref != c.Ref {
		t.Fatalf("lookup SĐT mới: %v %v", ref, err)
	}
	if _, err := s.Lookup(ctx, client.PurposeHRMProfile, "0912000111"); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("SĐT cũ phải hết chỉ mục: %v", err)
	}
	if id, err := s.Resolve(ctx, client.PurposeHRMProfile, c.Ref); err != nil || id.FullName != "Trần Thị Mai" || id.Address != "Châu Đốc" || id.Phone != "84912000333" {
		t.Fatalf("resolve sau sửa: %+v %v", id, err)
	}
	_ = other

	// Sổ truy cập ghi mọi thao tác ngăn có mục đích.
	var ops []string
	if err := tenant.WithTenantTx(context.Background(), s.DB, tid, func(tx pgx.Tx) error {
		acc, e := store.AccessBySubject(context.Background(), tx, c.Ref)
		for _, a := range acc {
			ops = append(ops, a.Op+"/"+a.Purpose+"/"+a.Outcome)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"section_write/hrm.profile/ok", "section_write/hrm.sensitive/ok", "section_read/hrm.sensitive/ok",
		"section_read/hrm.profile/ok", "amend/hrm.profile/duplicate", "amend/hrm.profile/ok"} {
		found := false
		for _, o := range ops {
			found = found || o == w
		}
		if !found {
			t.Errorf("thiếu dòng sổ %s trong %v", w, ops)
		}
	}

	// Xóa định danh xóa luôn các ngăn (RB-3).
	if err := s.Erase(ctx, c.Ref, "Viên chức nghỉ việc yêu cầu xóa dữ liệu"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := admin.QueryRow(context.Background(), "SELECT count(*) FROM vault.section WHERE subject_ref = $1", c.Ref).Scan(&n); err != nil || n != 0 {
		t.Fatalf("ngăn sau xóa: %d %v", n, err)
	}
	if _, err := s.GetSection(ctx, client.PurposeHRMProfile, c.Ref, client.SectionHRMProfile); !errors.Is(err, client.ErrErased) {
		t.Fatalf("đọc sau xóa: %v", err)
	}
}
