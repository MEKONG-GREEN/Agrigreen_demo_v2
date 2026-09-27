package clienttest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

func TestFakeSemantics(t *testing.T) {
	f := New()
	ctxA := authz.WithPrincipal(context.Background(), authz.Principal{TenantID: uuid.New(), ActorRef: uuid.New()})
	ctxB := authz.WithPrincipal(context.Background(), authz.Principal{TenantID: uuid.New(), ActorRef: uuid.New()})
	id := client.Identity{Kind: client.SubjectHousehold, FullName: "Trần Thị Bé", Phone: "0912 345 678", AreaCode: "xa-01"}

	if _, err := f.Create(ctxA, "", id); !errors.Is(err, client.ErrNoPurpose) {
		t.Fatalf("thiếu mục đích: %v", err)
	}
	c, err := f.Create(ctxA, client.PurposeRegistryAdmin, id)
	if err != nil {
		t.Fatal(err)
	}
	dup, err := f.Create(ctxA, client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectPerson, FullName: "Khác", Phone: "+84912345678", AreaCode: "xa-01"})
	if !errors.Is(err, client.ErrDuplicate) || dup.Ref != c.Ref {
		t.Fatalf("trùng SĐT cùng tenant: %v %v", dup.Ref, err)
	}
	if _, err := f.Create(ctxB, client.PurposeRegistryAdmin, id); err != nil {
		t.Fatalf("cùng SĐT tenant khác phải được: %v", err)
	}
	if _, err := f.Resolve(ctxB, client.PurposeContact, c.Ref); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("tenant khác phải không thấy: %v", err)
	}
	d, err := f.Display(ctxA, client.PurposeContact, c.Ref)
	if err != nil || d.Label != "Trần T. B." || d.MaskedPhone != "09xx xxx 678" {
		t.Fatalf("display: %+v %v", d, err)
	}
	pub, _ := envelope.Key32(c.PublicKey)
	s, _ := envelope.Seal([]byte("ảnh"), pub)
	dek, err := f.Unwrap(ctxA, client.PurposeMRVEvidence, c.Ref, s.WrappedDEK)
	if err != nil || len(dek) != 32 {
		t.Fatalf("unwrap: %v", err)
	}
	pt, err := envelope.DecryptWithDEK(dek, s.Nonce, s.Ciphertext)
	if err != nil || !bytes.Equal(pt, []byte("ảnh")) {
		t.Fatal("giải mã bằng DEK thất bại")
	}
	if err := f.Erase(ctxA, c.Ref, "yêu cầu xóa của chủ hộ"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Unwrap(ctxA, client.PurposeMRVEvidence, c.Ref, s.WrappedDEK); !errors.Is(err, client.ErrErased) {
		t.Fatalf("sau xóa unwrap phải ErrErased: %v", err)
	}
	if _, err := f.Lookup(ctxA, client.PurposeDedup, "0912345678"); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("sau xóa lookup phải không thấy: %v", err)
	}
	if len(f.Accesses()) == 0 {
		t.Fatal("phải ghi nhật ký truy cập")
	}
}
