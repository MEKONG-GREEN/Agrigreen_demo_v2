package acceptance

import (
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// Chống gửi trùng khi tạo (R-51, shared/idempotency) — thử trên POST /v1/extension/tasks.

func taskBody(desc string) map[string]any {
	return map[string]any{"commune_code": communeNK, "category": "ky_thuat", "description": desc}
}

func (w *world) taskCount() int {
	w.t.Helper()
	return len(w.want(w.provAdmin.Do(harness.App, http.MethodGet, "/v1/extension/tasks?limit=200", "", nil), 200, "liệt kê nhiệm vụ").Items())
}

// Nguồn: mekong-green/tests/idempotency.test.ts — "Cùng khoá, gửi hai lần → một đội, phản hồi thứ hai là bản trả lại".
func TestPrototypeCungKhoaGuiHaiLan(t *testing.T) {
	nguon(t, "idempotency.test.ts", "Cùng khoá, gửi hai lần → một đội, phản hồi thứ hai là bản trả lại")
	w := newWorld(t)
	key := harness.NewIdem()
	first := w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", key, taskBody("Hướng dẫn chống trùng lần một")), 201, "lần 1")
	second := w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", key, taskBody("Hướng dẫn chống trùng lần một")), 201, "lần 2")
	if first.Header.Get("Idempotent-Replayed") != "" || second.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatal("chỉ phản hồi thứ hai mang Idempotent-Replayed: true")
	}
	if str(first.Body, "task_id") != str(second.Body, "task_id") || w.taskCount() != 1 {
		t.Fatal("nghiệp vụ chạy đúng một lần, trả lại đúng kết quả cũ")
	}
}

// Nguồn: mekong-green/tests/idempotency.test.ts — "Không có khoá → mỗi lần gửi là một bản ghi (hành vi cũ giữ nguyên)"
// và "Khoá quá ngắn bị bỏ qua (không phải khoá), khoá quá hạn bị dọn".
// Khác prototype: mọi POST BẮT BUỘC Idempotency-Key (400 idempotency_key_required); khóa sai dạng
// (< 8 ký tự) bị từ chối 400 chứ không bị bỏ qua. Dọn khóa hết hạn (48 giờ) là việc nội bộ, không kiểm qua HTTP.
func TestPrototypeKhongCoKhoaBiTuChoi(t *testing.T) {
	nguon(t, "idempotency.test.ts", "Không có khoá → mỗi lần gửi là một bản ghi (hành vi cũ giữ nguyên)")
	nguon(t, "idempotency.test.ts", "Khoá quá ngắn bị bỏ qua (không phải khoá), khoá quá hạn bị dọn")
	w := newWorld(t)
	raw := func(key string) harness.Resp {
		h := http.Header{"Content-Type": {"application/json"}}
		if key != "" {
			h.Set("Idempotency-Key", key)
		}
		return w.officer.DoRaw(harness.App, http.MethodPost, "/v1/extension/tasks", h, strings.NewReader(`{"commune_code":"`+communeNK+`","category":"khac","description":"Không có khóa chống trùng"}`))
	}
	wantProblem(t, raw(""), 400, "idempotency_key_required", "POST thiếu khóa")
	wantProblem(t, raw("abc"), 400, "bad_request", "khóa quá ngắn")
	if w.taskCount() != 0 {
		t.Fatal("request bị từ chối không được ghi gì")
	}
}

// Nguồn: mekong-green/tests/idempotency.test.ts — "Yêu cầu lỗi không được lưu — sửa dữ liệu rồi gửi lại cùng khoá thì chạy thật".
// Khác prototype: phản hồi 4xx CŨNG được lưu và phát lại (R-51: gọi lặp trả đúng phản hồi lần đầu);
// sửa dữ liệu rồi gửi cùng khóa là tái dùng khóa (422 idempotency_key_reused) — phải dùng khóa mới.
// Chỉ lỗi hệ thống (5xx) mới xóa khóa để thử lại.
func TestPrototypeYeuCauLoiVaKhoaCu(t *testing.T) {
	nguon(t, "idempotency.test.ts", "Yêu cầu lỗi không được lưu — sửa dữ liệu rồi gửi lại cùng khoá thì chạy thật")
	w := newWorld(t)
	key := harness.NewIdem()
	bad := wantProblem(t, w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", key, taskBody("")), 422, "validation_failed", "thiếu mô tả")
	again := w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", key, taskBody(""))
	if again.Status != 422 || again.Header.Get("Idempotent-Replayed") != "true" || string(again.Raw) != string(bad.Raw) {
		t.Fatalf("gửi lại y hệt phải phát lại đúng phản hồi lỗi cũ: %d %s", again.Status, trim(again.Raw))
	}
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", key, taskBody("Đã sửa mô tả đủ dài")), 422, "idempotency_key_reused", "sửa dữ liệu rồi gửi cùng khóa")
	w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", "", taskBody("Đã sửa mô tả đủ dài")), 201, "gửi với khóa mới")
}

// Nguồn: mekong-green/tests/idempotency.test.ts — "Cùng khoá nhưng đường dẫn khác → từ chối, vì đó là lỗi tái dùng khoá".
func TestPrototypeCungKhoaKhacDuongDan(t *testing.T) {
	nguon(t, "idempotency.test.ts", "Cùng khoá nhưng đường dẫn khác → từ chối, vì đó là lỗi tái dùng khoá")
	w := newWorld(t)
	key := harness.NewIdem()
	w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", key, taskBody("Nhiệm vụ đầu tiên của khóa")), 201, "lần 1")
	wantProblem(t, w.officer.Do(harness.App, http.MethodPost, "/v1/extension/staff", key, map[string]any{
		"actor_ref": w.officer.Actor.String(), "unit_code": communeNK, "role_title": "KNV", "display_name": "KNV",
	}), 422, "idempotency_key_reused", "cùng khóa, route khác")
}

// Nguồn: mekong-green/tests/idempotency.test.ts — "Khoá thuộc về người dùng: người khác dùng trùng khoá vẫn chạy bình thường".
// Khác prototype: khóa thuộc về TENANT (bảng shared.idempotency_key theo tenant, RLS): người khác
// cùng tenant dùng lại khóa → 422 idempotency_key_reused; tenant khác dùng cùng khóa chạy bình thường.
func TestPrototypeKhoaTheoTenant(t *testing.T) {
	nguon(t, "idempotency.test.ts", "Khoá thuộc về người dùng: người khác dùng trùng khoá vẫn chạy bình thường")
	w := newWorld(t)
	key := harness.NewIdem()
	w.want(w.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", key, taskBody("Nhiệm vụ của cán bộ A")), 201, "cán bộ A")
	peer := w.user(w.teamNK, false, "kn_officer")
	wantProblem(t, peer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", key, taskBody("Nhiệm vụ của cán bộ A")), 422, "idempotency_key_reused", "người khác cùng tenant, cùng khóa")

	other := newWorld(t) // tenant khác
	r := other.want(other.officer.Do(harness.App, http.MethodPost, "/v1/extension/tasks", key, taskBody("Nhiệm vụ của cán bộ A")), 201, "tenant khác, cùng khóa")
	if r.Header.Get("Idempotent-Replayed") != "" || other.taskCount() != 1 {
		t.Fatal("tenant khác dùng cùng khóa phải chạy thật")
	}
}
