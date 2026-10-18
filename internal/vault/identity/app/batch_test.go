package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W11.28: đọc theo lô — một lượt hạn mức cho cả lô, mục đích/ngăn gắn cứng, MFA, ref lạ bỏ qua, sổ ghi từng chủ thể.
func TestReadBatch_W1128(t *testing.T) {
	s, env := newService(t)
	s.RequireMFASensitive = true
	s.Quota = app.NewQuota(clock.NewFixed(time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)), nil, 60, 2)
	tid := ids.New()
	ctx := as(tid, true, client.RoleRegistrar, client.RoleEraser)
	var refs []uuid.UUID
	for i, ph := range []string{"0912000201", "0912000202", "0912000203"} {
		c, err := s.Create(ctx, client.PurposeHRMProfile, client.Identity{Kind: client.SubjectPerson, FullName: "Nguyễn Văn " + string(rune('A'+i)),
			Phone: ph, AreaCode: "hrm"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.PutSection(ctx, client.PurposeHRMProfile, c.Ref, client.SectionHRMProfile, client.Fields{"gender": "nam", "birth_date": "1980"}); err != nil {
			t.Fatal(err)
		}
		if err := s.PutSection(ctx, client.PurposeHRMSensitive, c.Ref, client.SectionHRMSensitive, client.Fields{"ethnicity": "khmer"}); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, c.Ref)
	}
	if err := s.Erase(ctx, refs[2], "kiểm thử xóa định danh lô"); err != nil {
		t.Fatal(err)
	}
	lot := append([]uuid.UUID{ids.New()}, refs...) // ref lạ đứng đầu: bỏ qua
	got, err := s.ReadBatch(ctx, client.BatchRequest{Purpose: client.PurposeHRMProfile, Refs: lot, Identity: true, Section: client.SectionHRMProfile})
	if err != nil || len(got) != 3 || got[0].FullName != "Nguyễn Văn A" || got[0].Fields["gender"] != "nam" || !got[2].Erased || got[2].FullName != "" {
		t.Fatalf("lô hồ sơ: %+v %v", got, err)
	}
	// Ngăn nhạy cảm chỉ với hrm.sensitive, không kèm định danh; cần MFA; vai vault_registrar.
	if _, err := s.ReadBatch(ctx, client.BatchRequest{Purpose: client.PurposeHRMProfile, Refs: refs, Section: client.SectionHRMSensitive}); code(err) != client.CodeSectionInvalid {
		t.Fatalf("ngăn nhạy cảm bằng hrm.profile: %v", err)
	}
	if _, err := s.ReadBatch(ctx, client.BatchRequest{Purpose: client.PurposeHRMSensitive, Refs: refs, Identity: true, Section: client.SectionHRMSensitive}); code(err) != "validation_failed" {
		t.Fatalf("định danh bằng hrm.sensitive: %v", err)
	}
	if _, err := s.ReadBatch(as(tid, false, client.RoleRegistrar), client.BatchRequest{Purpose: client.PurposeHRMSensitive, Refs: refs,
		Section: client.SectionHRMSensitive}); code(err) != "mfa_required" {
		t.Fatalf("không MFA: %v", err)
	}
	if _, err := s.ReadBatch(as(tid, true, client.RoleDisplay), client.BatchRequest{Purpose: client.PurposeHRMProfile, Refs: refs,
		Section: client.SectionHRMProfile}); code(err) != "forbidden" {
		t.Fatalf("vai display: %v", err)
	}
	// Hạn mức dồn 2: lô hợp lệ thứ hai còn được, thứ ba bị chặn (một lượt mỗi lô).
	if got, err := s.ReadBatch(ctx, client.BatchRequest{Purpose: client.PurposeHRMSensitive, Refs: refs[:2], Section: client.SectionHRMSensitive}); err != nil ||
		len(got) != 2 || got[1].Fields["ethnicity"] != "khmer" {
		t.Fatalf("lô nhạy cảm: %+v %v", got, err)
	}
	if _, err := s.ReadBatch(ctx, client.BatchRequest{Purpose: client.PurposeHRMProfile, Refs: refs, Section: client.SectionHRMProfile}); code(err) != client.CodeQuotaExceeded {
		t.Fatalf("hạn mức: %v", err)
	}
	// Sổ: lô đầu ghi resolve + section_read cho 3 chủ thể (kể cả chủ thể đã xóa); lô nhạy cảm 2 section_read.
	admin := env.AdminPool(t, migrations.Vault)
	var n int
	if err := admin.QueryRow(context.Background(), `SELECT count(*) FROM vault.access_log WHERE tenant_id = $1 AND op IN ('resolve', 'section_read')`,
		tid).Scan(&n); err != nil || n != 8 {
		t.Fatalf("sổ truy cập: %d %v", n, err)
	}
}
