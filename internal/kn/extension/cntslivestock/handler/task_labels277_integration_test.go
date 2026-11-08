package handler_test

// #277: hợp đồng FN-10 HCN-01…03 mà consumer công việc FN-21 đọc theo ref + xã của công việc.
// Không sửa FormsTask/snapshot; dùng endpoint owner và ngữ cảnh đã chọn trên PostgreSQL thật.
import (
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

func TestCampaignHouseholdReadContract_277(t *testing.T) {
	f := newFix(t)
	officer := f.user("Cán bộ thử nhãn hộ", xa("30001"))
	viewer := f.user("Viên chức chỉ xem", tinh("vien_chuc"))
	created := want(t, officer.post(base, household("Hộ tổng hợp ban đầu", "30001", "Ấp thử")), http.StatusCreated, "", "tạo hộ tổng hợp")
	ref, code := str(created, "id"), str(created, "code")
	path := base + "/" + ref
	initial := want(t, officer.get(path), http.StatusOK, "", "đọc nhãn bằng ref công việc")
	if str(initial, "id") != ref || str(initial, "code") != code || str(initial, "commune_code") != "30001" || str(initial, "owner_name") != "Hộ tổng hợp ban đầu" {
		t.Fatalf("nhãn được phép: %v", initial)
	}
	last := f.vault.Accesses()
	found := false
	for _, access := range last {
		if access.Op == "resolve" && access.Purpose == client.PurposeFieldSupport && access.Actor == officer.ref {
			found = true
		}
	}
	if !found {
		t.Fatal("đọc tên phải qua Vault Resolve có mục đích và actor hiện tại")
	}
	readOnly := want(t, viewer.get(path), http.StatusOK, "", "đọc nhãn trong quyền chỉ xem")
	if readOnly["owner_name"] != nil || str(readOnly, "owner_label") == "" || str(readOnly, "owner_masked_phone") != "0912•••123" {
		t.Fatalf("nhãn chỉ xem/SĐT che: %v", readOnly)
	}
	want(t, officer.post(path, map[string]any{"base_version": 1, "owner_name": "Hộ tổng hợp hiện hành", "commune_code": "30001", "hamlet": "Ấp thử", "livestock_type": "nong_ho", "animals": []any{map[string]any{"species_code": "GA", "head_count": 120}}}), http.StatusOK, "", "sửa tên tại owner")
	current := want(t, officer.get(path), http.StatusOK, "", "đọc lại tên hiện hành")
	if str(current, "code") != code || str(current, "owner_name") != "Hộ tổng hợp hiện hành" || strings.Contains(str(current, "owner_masked_phone"), "0912000123") {
		t.Fatalf("mã bất biến/tên hiện hành/SĐT che: %v", current)
	}
	beforeDenied := len(f.vault.Accesses())
	f.mu.Lock()
	f.ctxs[officer.ctxID].Scope = authz.Scope{Units: []string{"30002"}}
	f.ctxs[officer.ctxID].Communes = []string{"30002"}
	f.mu.Unlock()
	want(t, officer.get(path), http.StatusNotFound, "extension.cnts_livestock_household_not_found", "đổi phạm vi không đọc được hộ cũ")
	f.mu.Lock()
	delete(f.ctxs, officer.ctxID)
	f.mu.Unlock()
	want(t, officer.get(path), http.StatusForbidden, "", "thu hồi ngữ cảnh hiện tại")
	if len(f.vault.Accesses()) != beforeDenied {
		t.Fatal("ngoài phạm vi/thu hồi vẫn đọc Vault")
	}
	other := newFix(t)
	foreign := other.user("Cán bộ tenant khác", xa("30001"))
	want(t, foreign.get(path), http.StatusNotFound, "extension.cnts_livestock_household_not_found", "cùng xã khác tenant")
	if len(other.vault.Accesses()) != 0 {
		t.Fatal("chéo tenant vẫn gọi Vault")
	}
}
