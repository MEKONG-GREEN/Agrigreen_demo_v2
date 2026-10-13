package app_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
)

// W11.23 (vault 20261013165158): thẻ danh bạ — chỉ trường được hỏi, phép chiếu năm sinh/email từ ngăn hrm.profile, ngăn
// contact.card của liên hệ ngoài tỉnh, vai vault_directory chỉ đọc thẻ, mục đích gắn cứng, RLS, sổ op "card".
func TestContactCards_W1123(t *testing.T) {
	s, _ := newService(t)
	tid := ids.New()
	reg := as(tid, true, client.RoleRegistrar)
	staff, err := s.Create(reg, client.PurposeHRMProfile, client.Identity{Kind: client.SubjectPerson, FullName: "Trần Thị Mai",
		Phone: "0912000111", Address: "Long Xuyên", AreaCode: "hrm"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutSection(reg, client.PurposeHRMProfile, staff.Ref, client.SectionHRMProfile,
		client.Fields{"birth_date": "1990-03-15", "birth_precision": "day", "gender": "nu", "email": "mai@ttkn.vn", "note": "ghi chú riêng"}); err != nil {
		t.Fatal(err)
	}
	ext, err := s.Create(reg, client.PurposeContactDirectory, client.Identity{Kind: client.SubjectPerson, FullName: "Nguyễn Văn TW",
		Phone: "0243000999", AreaCode: "contact"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutSection(reg, client.PurposeContactDirectory, ext.Ref, client.SectionContactCard,
		client.Fields{"email": "tw@khuyennong.gov.vn", "birth_year": "1975"}); err != nil {
		t.Fatal(err)
	}
	// Ngăn contact.card gắn cứng mục đích danh bạ; trường lạ bị từ chối.
	if err := s.PutSection(reg, client.PurposeHRMProfile, ext.Ref, client.SectionContactCard, client.Fields{"email": "x@y.vn"}); code(err) != client.CodeSectionInvalid {
		t.Fatalf("ngăn danh bạ với hrm.profile: %v", err)
	}
	if err := s.PutSection(reg, client.PurposeContactDirectory, ext.Ref, client.SectionContactCard, client.Fields{"address": "x"}); code(err) != client.CodeSectionInvalid {
		t.Fatalf("trường lạ: %v", err)
	}

	dir := as(tid, false, client.RoleDirectory) // vai của mọi cán bộ KN (qua kn_user)
	basic := []string{client.CardFullName, client.CardBirthYear, client.CardPhone}
	got, err := s.Cards(dir, client.PurposeContactDirectory, []uuid.UUID{staff.Ref, ext.Ref, ids.New()}, basic)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ref lạ phải bị bỏ qua: %+v", got)
	}
	byRef := map[uuid.UUID]client.Card{got[0].Ref: got[0], got[1].Ref: got[1]}
	if c := byRef[staff.Ref]; c.FullName != "Trần Thị Mai" || c.BirthYear != "1990" || c.Phone == "" || c.Email != "" {
		t.Fatalf("thẻ cán bộ (AC-04 năm sinh 1990, không email khi không hỏi): %+v", c)
	}
	if c := byRef[ext.Ref]; c.FullName != "Nguyễn Văn TW" || c.BirthYear != "1975" || c.Email != "" {
		t.Fatalf("thẻ liên hệ ngoài tỉnh: %+v", c)
	}
	full, err := s.Cards(dir, client.PurposeContactDirectory, []uuid.UUID{staff.Ref, ext.Ref}, append(basic, client.CardEmail))
	if err != nil || len(full) != 2 {
		t.Fatal(full, err)
	}
	for _, c := range full {
		if c.Email == "" {
			t.Fatalf("email phải có khi hỏi: %+v", c)
		}
	}

	// Vai vault_directory KHÔNG đọc được định danh đầy đủ, ngăn hồ sơ; mục đích khác bị từ chối; trường lạ bị từ chối.
	if _, err := s.Resolve(dir, client.PurposeHRMProfile, staff.Ref); code(err) != "forbidden" {
		t.Fatalf("resolve bằng vai danh bạ: %v", err)
	}
	if _, err := s.GetSection(dir, client.PurposeContactDirectory, ext.Ref, client.SectionContactCard); code(err) != "forbidden" {
		t.Fatalf("đọc ngăn bằng vai danh bạ: %v", err)
	}
	if _, err := s.Cards(dir, client.PurposeHRMProfile, []uuid.UUID{staff.Ref}, basic); code(err) != "validation_failed" {
		t.Fatalf("thẻ với mục đích hrm.profile: %v", err)
	}
	if _, err := s.Cards(dir, client.PurposeContactDirectory, []uuid.UUID{staff.Ref}, []string{"address"}); code(err) != "validation_failed" {
		t.Fatalf("trường địa chỉ: %v", err)
	}
	if _, err := s.Cards(as(tid, false, "kn_officer"), client.PurposeContactDirectory, []uuid.UUID{staff.Ref}, basic); code(err) != "forbidden" {
		t.Fatalf("vai không có quyền kho: %v", err)
	}
	// Tenant khác: không thấy chủ thể (RLS) — danh sách rỗng, không lỗi.
	if other, err := s.Cards(as(ids.New(), false, client.RoleDirectory), client.PurposeContactDirectory, []uuid.UUID{staff.Ref}, basic); err != nil || len(other) != 0 {
		t.Fatalf("tenant khác: %+v %v", other, err)
	}

	// Sổ truy cập: op card với mục đích danh bạ.
	var ops []string
	if err := tenant.WithTenantTx(context.Background(), s.DB, tid, func(tx pgx.Tx) error {
		acc, e := store.AccessBySubject(context.Background(), tx, staff.Ref)
		for _, a := range acc {
			ops = append(ops, a.Op+"/"+a.Purpose+"/"+a.Outcome)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, o := range ops {
		if o == "card/contact.directory/ok" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("sổ card: %v", ops)
	}

	// Xóa định danh: thẻ đánh dấu erased, không trường nào.
	if err := s.Erase(as(tid, true, client.RoleEraser), ext.Ref, "Liên hệ yêu cầu xóa dữ liệu khỏi danh bạ"); err != nil {
		t.Fatal(err)
	}
	after, err := s.Cards(dir, client.PurposeContactDirectory, []uuid.UUID{ext.Ref}, basic)
	if err != nil || len(after) != 1 || !after[0].Erased || after[0].FullName != "" {
		t.Fatalf("thẻ sau xóa: %+v %v", after, err)
	}
}
