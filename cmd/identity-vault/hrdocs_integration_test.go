package main

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// HRM-DOC-CRYPTO-001: mục đích tài liệu phải hoạt động qua HTTP thật,
// nhưng không cho người thiếu vai hay tenant khác mở khóa tệp.
func TestHRRecordsUnwrapHTTP_W1124(t *testing.T) {
	f := newFixture(t)
	tid := ids.New()
	registrar := f.user(tid, true, client.RoleRegistrar)
	viewer := f.user(tid, true, client.RoleEvidenceViewer)
	vc := client.NewHTTP(f.srv.URL, f.srv.Client())
	owner, err := vc.Create(registrar.ctx(), client.PurposeHRMProfile, client.Identity{
		Kind: client.SubjectPerson, FullName: "Kho tài liệu thử nghiệm", AreaCode: client.AreaHRDocs,
	})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := envelope.Key32(owner.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("%PDF-1.7\nNội dung hồ sơ thử nghiệm riêng tư\n%%EOF")
	sealed, err := envelope.Seal(plain, pub)
	if err != nil {
		t.Fatal(err)
	}
	dek, err := vc.Unwrap(viewer.ctx(), client.PurposeHRRecords, owner.Ref, sealed.WrappedDEK)
	if err != nil {
		t.Fatal(err)
	}
	got, err := envelope.DecryptWithDEK(dek, sealed.Nonce, sealed.Ciphertext)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("tệp mở qua HTTP không khớp bản gốc")
	}
	if _, err := vc.Unwrap(registrar.ctx(), client.PurposeHRRecords, owner.Ref, sealed.WrappedDEK); err == nil {
		t.Fatal("vai đăng ký không được mở khóa tài liệu")
	} else {
		var p *problem.Problem
		if !errors.As(err, &p) || p.Status != http.StatusForbidden {
			t.Fatalf("sai vai: %v", err)
		}
	}
	foreign := f.user(ids.New(), true, client.RoleEvidenceViewer)
	if _, err := vc.Unwrap(foreign.ctx(), client.PurposeHRRecords, owner.Ref, sealed.WrappedDEK); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("tenant khác phải không thấy khóa: %v", err)
	}
	if _, err := vc.Unwrap(viewer.ctx(), client.PurposeHRMProfile, owner.Ref, sealed.WrappedDEK); err == nil {
		t.Fatal("mục đích hồ sơ không được thay thế mục đích mở tệp")
	}
	for _, secret := range []string{string(plain), registrar.token, viewer.token, foreign.token} {
		if strings.Contains(f.log.String(), secret) {
			t.Fatal("log chứa bản rõ hoặc token")
		}
	}
}
