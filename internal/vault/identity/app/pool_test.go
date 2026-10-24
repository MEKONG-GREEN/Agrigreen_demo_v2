package app_test

// P0-POOL-SHARED (#126): KMS không chạy khi đang giữ giao dịch DB — pool CỠ 1 vẫn phục vụ giao dịch khác trong lúc
// KMS treo; KMS lỗi → không ghi sổ OK; kết quả vẫn đúng.

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/kms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// gateKMS bọc KMS Local: khi armed, Unwrap báo entered rồi chờ release (hoặc trả lỗi khi fail).
type gateKMS struct {
	kms.KMS
	armed   atomic.Bool
	fail    atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (g *gateKMS) Unwrap(ctx context.Context, keyID string, wrapped, aad []byte) ([]byte, error) {
	if g.fail.Load() {
		return nil, errors.New("fixture: KMS lỗi")
	}
	if g.armed.Load() {
		g.entered <- struct{}{}
		select {
		case <-g.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return g.KMS.Unwrap(ctx, keyID, wrapped, aad)
}

func singleConnService(t *testing.T) (*app.Service, *gateKMS) {
	t.Helper()
	s, env := newService(t)
	g := &gateKMS{KMS: s.KMS, entered: make(chan struct{}, 8), release: make(chan struct{})}
	s.KMS = g
	s.DB = env.SizedPool(t, migrations.Vault, role, 1)
	return s, g
}

// poolFree: trong lúc KMS treo, giao dịch khác trên pool cỡ 1 chạy được ngay.
func poolFree(t *testing.T, s *app.Service, g *gateKMS, tid uuid.UUID) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("KMS.Unwrap không được gọi")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT 1")
		return err
	}); err != nil {
		t.Fatalf("pool cỡ 1 bị giữ trong lúc gọi KMS: %v", err)
	}
	g.armed.Store(false)
	g.release <- struct{}{}
}

func accessOps(t *testing.T, s *app.Service, tid, ref uuid.UUID) []string {
	t.Helper()
	var out []string
	if err := tenant.WithTenantTx(context.Background(), s.DB, tid, func(tx pgx.Tx) error {
		as, err := store.AccessBySubject(context.Background(), tx, ref)
		for _, a := range as {
			out = append(out, a.Op+"/"+a.Outcome)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestResolveAndAmendKMSOutsideTxOnSingleConnPool(t *testing.T) {
	s, g := singleConnService(t)
	tid := uuid.New()
	ctx := as(tid, true, allRoles...)
	c, err := s.Create(ctx, client.PurposeHRMProfile, sample("0901777001"))
	if err != nil {
		t.Fatal(err)
	}

	g.armed.Store(true)
	type res struct {
		id  client.Identity
		err error
	}
	done := make(chan res, 1)
	go func() {
		id, err := s.Resolve(ctx, client.PurposeHRMProfile, c.Ref)
		done <- res{id, err}
	}()
	poolFree(t, s, g, tid)
	r := <-done
	if r.err != nil || r.id.FullName != "Nguyễn Văn Thành" || r.id.Phone != "84901777001" {
		t.Fatalf("Resolve: %+v %v", r.id, r.err)
	}

	g.armed.Store(true)
	amended := make(chan error, 1)
	go func() {
		amended <- s.Amend(ctx, client.PurposeHRMProfile, c.Ref, client.Identity{FullName: "Trần Thị Bích", Phone: "0901777002", Address: "Ấp 5"})
	}()
	poolFree(t, s, g, tid)
	if err := <-amended; err != nil {
		t.Fatal(err)
	}
	got, err := s.Resolve(ctx, client.PurposeHRMProfile, c.Ref)
	if err != nil || got.FullName != "Trần Thị Bích" || got.Phone != "84901777002" || got.Address != "Ấp 5" {
		t.Fatalf("sau Amend: %+v %v", got, err)
	}
	if ref, err := s.Lookup(ctx, client.PurposeHRMProfile, "0901777002"); err != nil || ref != c.Ref {
		t.Fatalf("chỉ mục mù SĐT mới: %v %v", ref, err)
	}

	// Ngăn: ghi rồi đọc khi KMS treo.
	if err := s.PutSection(ctx, client.PurposeHRMProfile, c.Ref, domain.SectionHRMProfile, client.Fields{"email": "b@example.vn"}); err != nil {
		t.Fatal(err)
	}
	g.armed.Store(true)
	sec := make(chan client.Fields, 1)
	go func() {
		f, err := s.GetSection(ctx, client.PurposeHRMProfile, c.Ref, domain.SectionHRMProfile)
		if err != nil {
			t.Error(err)
		}
		sec <- f
	}()
	poolFree(t, s, g, tid)
	if f := <-sec; f["email"] != "b@example.vn" {
		t.Fatalf("GetSection: %v", f)
	}
	ops := accessOps(t, s, tid, c.Ref)
	want := []string{"create/ok", "resolve/ok", "amend/ok", "resolve/ok", "lookup/ok", "section_write/ok", "section_read/ok"}
	if len(ops) != len(want) {
		t.Fatalf("sổ truy cập: %v", ops)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Fatalf("sổ truy cập: %v; muốn %v", ops, want)
		}
	}
}

// KMS lỗi ngoài giao dịch → thao tác lỗi, KHÔNG có dòng sổ OK; Amend không đổi định danh.
func TestKMSFailureLogsNothingAndKeepsIdentity(t *testing.T) {
	s, g := singleConnService(t)
	tid := uuid.New()
	ctx := as(tid, true, allRoles...)
	c, err := s.Create(ctx, client.PurposeHRMProfile, sample("0901777011"))
	if err != nil {
		t.Fatal(err)
	}
	g.fail.Store(true)
	if _, err := s.Resolve(ctx, client.PurposeHRMProfile, c.Ref); err == nil {
		t.Fatal("Resolve: lỗi KMS bị nuốt")
	}
	if _, err := s.Display(ctx, client.PurposeHRMProfile, c.Ref); err == nil {
		t.Fatal("Display: lỗi KMS bị nuốt")
	}
	if err := s.Amend(ctx, client.PurposeHRMProfile, c.Ref, client.Identity{FullName: "Tên Khác"}); err == nil {
		t.Fatal("Amend: lỗi KMS bị nuốt")
	}
	if _, err := s.Cards(ctx, client.PurposeContactDirectory, []uuid.UUID{c.Ref}, []string{domain.CardFullName}); err == nil {
		t.Fatal("Cards: lỗi KMS bị nuốt")
	}
	if _, err := s.Unwrap(ctx, client.PurposeMRVEvidence, c.Ref, bytes.Repeat([]byte{1}, 80)); err == nil {
		t.Fatal("Unwrap: lỗi KMS bị nuốt")
	}
	if ops := accessOps(t, s, tid, c.Ref); len(ops) != 1 || ops[0] != "create/ok" {
		t.Fatalf("lỗi KMS vẫn ghi sổ: %v", ops)
	}
	g.fail.Store(false)
	got, err := s.Resolve(ctx, client.PurposeHRMProfile, c.Ref)
	if err != nil || got.FullName != "Nguyễn Văn Thành" {
		t.Fatalf("định danh đổi dù Amend lỗi: %+v %v", got, err)
	}
	// DEK sai → rejected được ghi sổ, trả unwrap_failed (như trước).
	if _, err := s.Unwrap(ctx, client.PurposeMRVEvidence, c.Ref, bytes.Repeat([]byte{1}, 80)); code(err) != code(client.ProblemUnwrapFailed) {
		t.Fatalf("Unwrap DEK sai: %v", err)
	}
	if ops := accessOps(t, s, tid, c.Ref); ops[len(ops)-1] != "unwrap/rejected" {
		t.Fatalf("Unwrap DEK sai phải ghi rejected: %v", ops)
	}
}
