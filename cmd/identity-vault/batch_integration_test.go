package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// W11.28: qua router và HTTP client thật, dữ liệu lô vẫn bị giới hạn bởi tenant,
// vai và mục đích; phản hồi không được cache hay đưa PII vào log.
func TestBatchReadHTTP_W1128(t *testing.T) {
	f := newFixture(t)
	u := f.user(ids.New(), true, client.RoleRegistrar)
	vc := client.NewHTTP(f.srv.URL, f.srv.Client())
	batch, ok := client.AsProfileBatch(vc)
	if !ok {
		t.Fatal("HTTP client thiếu ProfileBatch")
	}
	profiles, ok := client.AsProfiles(vc)
	if !ok {
		t.Fatal("HTTP client thiếu Profiles")
	}
	own, err := vc.Create(u.ctx(), client.PurposeHRMProfile, client.Identity{
		Kind: client.SubjectPerson, FullName: "Nguyễn Thị Kiểm Lô", Phone: "0912000711", AreaCode: "hrm",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := profiles.PutSection(u.ctx(), client.PurposeHRMProfile, own.Ref, client.SectionHRMProfile,
		client.Fields{"gender": "nu", "birth_date": "1990-05-01"}); err != nil {
		t.Fatal(err)
	}
	other := f.user(ids.New(), true, client.RoleRegistrar)
	foreign, err := vc.Create(other.ctx(), client.PurposeHRMProfile, client.Identity{
		Kind: client.SubjectPerson, FullName: "Người Tenant Khác", Phone: "0912000712", AreaCode: "hrm",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := client.BatchRequest{Purpose: client.PurposeHRMProfile, Identity: true, Section: client.SectionHRMProfile,
		Refs: []uuid.UUID{foreign.Ref, own.Ref, own.Ref, ids.New()}}
	got, err := batch.ReadBatch(u.ctx(), req)
	if err != nil || len(got) != 1 || got[0].Ref != own.Ref || got[0].FullName != "Nguyễn Thị Kiểm Lô" ||
		got[0].Fields["gender"] != "nu" {
		t.Fatalf("đọc lô qua HTTP sai dữ liệu hoặc lọt tenant: %v", err)
	}
	if _, err := batch.ReadBatch(context.Background(), req); !errors.Is(err, client.ErrNoToken) {
		t.Fatalf("thiếu token: %v", err)
	}
	headers := map[string]string{"X-Purpose": string(client.PurposeHRMProfile)}
	body := client.BatchReadRequest{Refs: []string{own.Ref.String()}, Identity: true, Section: client.SectionHRMProfile}
	resp, result := u.do(http.MethodPost, "/v1/subjects/batch-read", headers, body)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" || result["items"] == nil {
		t.Fatalf("phản hồi lô: HTTP %d, cache=%q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	display := f.user(u.tid, true, client.RoleDisplay)
	if resp, result := display.do(http.MethodPost, "/v1/subjects/batch-read", headers, body); resp.StatusCode != http.StatusForbidden || result["code"] != "forbidden" {
		t.Fatalf("vai chỉ hiển thị đọc được lô: HTTP %d", resp.StatusCode)
	}
	for _, refs := range [][]string{nil, {"khong-phai-uuid"}, make([]string, client.MaxBatchRefs+1)} {
		if resp, result := u.do(http.MethodPost, "/v1/subjects/batch-read", headers,
			client.BatchReadRequest{Refs: refs, Identity: true}); resp.StatusCode != http.StatusUnprocessableEntity || result["code"] != "validation_failed" {
			t.Fatalf("refs không hợp lệ: HTTP %d, code=%v", resp.StatusCode, result["code"])
		}
	}
	for _, pii := range []string{"Nguyễn Thị Kiểm Lô", "Người Tenant Khác", "0912000711", "1990-05-01"} {
		if strings.Contains(f.log.String(), pii) {
			t.Fatal("log HTTP chứa dữ liệu cá nhân")
		}
	}
}
