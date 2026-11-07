package app_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// CNTS-V4-HOUSEHOLD-LIST (#251, ADR-0056 mục 4, BRD CNTS v4.0 FN-10 HCN-09): Amend với extension.contact ghi bản sửa và trả
// revision_ref; Revisions chỉ trả nhãn che bớt cũ → mới; mục đích, vai, tenant; sổ revision_read; bản mã không có giá trị
// rõ; xóa định danh xóa bản sửa (RB-3).
func TestIdentityRevisions_251(t *testing.T) {
	s, env := newService(t)
	tid := ids.New()
	ctx := as(tid, true, client.RoleRegistrar, client.RoleEraser)
	c, err := s.Create(ctx, client.PurposeContact, client.Identity{Kind: client.SubjectHousehold, FullName: "Nguyễn Văn An",
		Phone: "0912000123", AreaCode: "30001"})
	if err != nil {
		t.Fatal(err)
	}
	// Mục đích: extension.contact được sửa (mới), registry.admin không.
	if _, err := s.AmendRevision(ctx, client.PurposeRegistryAdmin, c.Ref, client.Identity{FullName: "X"}); code(err) != "validation_failed" {
		t.Fatalf("amend registry.admin: %v", err)
	}
	r1, err := s.AmendRevision(ctx, client.PurposeContact, c.Ref, client.Identity{FullName: "Nguyễn Văn Bình", Phone: "0913000456"})
	if err != nil || r1 == uuid.Nil || r1.Version() != 7 {
		t.Fatalf("amend extension.contact: %v %v", r1, err)
	}
	// Amend (chữ ký cũ) cũng ghi bản sửa.
	if err := s.Amend(ctx, client.PurposeContact, c.Ref, client.Identity{FullName: "Nguyễn Văn Bình"}); err != nil {
		t.Fatal(err)
	}
	admin := env.AdminPool(t, migrations.Vault)
	var n int
	if err := admin.QueryRow(context.Background(), "SELECT count(*) FROM vault.identity_revision WHERE subject_ref = $1", c.Ref).Scan(&n); err != nil || n != 2 {
		t.Fatalf("số bản sửa: %d %v", n, err)
	}
	rows, err := admin.Query(context.Background(), `SELECT coalesce(prev_name_ct, ''), coalesce(prev_phone_ct, ''), coalesce(next_name_ct, ''),
	    coalesce(next_phone_ct, '') FROM vault.identity_revision WHERE subject_ref = $1`, c.Ref)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a, b, x, y []byte
		if err := rows.Scan(&a, &b, &x, &y); err != nil {
			t.Fatal(err)
		}
		for _, ct := range [][]byte{a, b, x, y} {
			if bytes.Contains(ct, []byte("Nguy")) || bytes.Contains(ct, []byte("912000123")) || bytes.Contains(ct, []byte("913000456")) {
				t.Fatal("bản sửa lưu giá trị rõ")
			}
		}
	}
	rows.Close()

	// Nhãn che bớt cũ → mới; mã lạ bị bỏ qua.
	got, err := s.Revisions(ctx, client.PurposeContact, []uuid.UUID{r1, ids.New()})
	if err != nil || len(got) != 1 {
		t.Fatalf("revisions: %+v %v", got, err)
	}
	if g := got[0]; g.Ref != r1 || g.SubjectRef != c.Ref || g.PrevLabel != "Nguyễn V. A." || g.NextLabel != "Nguyễn V. B." ||
		g.PrevPhone != "0912•••123" || g.NextPhone != "0913•••456" {
		t.Fatalf("nhãn bản sửa: %+v", g)
	}
	// Mục đích ngoài danh mục; vai: display đọc được, directory không; quá 200 mã; tenant khác không thấy.
	if _, err := s.Revisions(ctx, client.PurposeFieldSupport, []uuid.UUID{r1}); code(err) != "validation_failed" {
		t.Fatalf("revisions field_support: %v", err)
	}
	if got, err := s.Revisions(as(tid, false, client.RoleDisplay), client.PurposeContact, []uuid.UUID{r1}); err != nil || len(got) != 1 {
		t.Fatalf("vai display: %v %v", got, err)
	}
	if _, err := s.Revisions(as(tid, true, client.RoleDirectory), client.PurposeContact, []uuid.UUID{r1}); code(err) != "forbidden" {
		t.Fatalf("vai directory: %v", err)
	}
	if _, err := s.Revisions(ctx, client.PurposeContact, make([]uuid.UUID, 201)); code(err) != "validation_failed" {
		t.Fatalf("201 mã: %v", err)
	}
	if got, err := s.Revisions(as(ids.New(), true, client.RoleRegistrar), client.PurposeContact, []uuid.UUID{r1}); err != nil || len(got) != 0 {
		t.Fatalf("tenant khác: %v %v", got, err)
	}

	// Sổ truy cập có revision_read.
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
	if !slices.Contains(ops, "revision_read/extension.contact/ok") || !slices.Contains(ops, "amend/extension.contact/ok") {
		t.Fatalf("sổ truy cập: %v", ops)
	}

	// Xóa định danh xóa bản sửa (RB-3).
	if err := s.Erase(ctx, c.Ref, "Chủ hộ yêu cầu xóa dữ liệu cá nhân"); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(context.Background(), "SELECT count(*) FROM vault.identity_revision WHERE subject_ref = $1", c.Ref).Scan(&n); err != nil || n != 0 {
		t.Fatalf("bản sửa sau xóa: %d %v", n, err)
	}
	if got, err := s.Revisions(ctx, client.PurposeContact, []uuid.UUID{r1}); err != nil || len(got) != 0 {
		t.Fatalf("revisions sau xóa: %v %v", got, err)
	}
	if _, err := s.AmendRevision(ctx, client.PurposeContact, c.Ref, client.Identity{FullName: "Y"}); !errors.Is(err, client.ErrErased) {
		t.Fatalf("amend sau xóa: %v", err)
	}
}
