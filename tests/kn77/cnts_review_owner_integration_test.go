package kn77_test

// BF-CN-XA-DATA (#175 review [P2]): pf/forms THẬT + pf/extension THẬT (ReviewGuard kn_cnts nối như assembly). Kỳ thuộc lần
// phát hành có người phụ trách biểu: chỉ người đó xác thực / yêu cầu điều chỉnh bài nộp qua POST
// /v1/forms/submissions/{id}/review — cán bộ Tỉnh CNTS khác (cùng phạm vi) nhận 403, bài không đổi trạng thái. Kỳ không có
// người phụ trách giữ quy tắc cũ (Cấp Tỉnh trong phạm vi, bốn mắt).

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func TestCNTSReviewOwnerOnly_175(t *testing.T) {
	facA, facB := ids.New(), ids.New()
	f := newGrantFx(t, facilityDir{facA: "X1", facB: "X1"})
	admin, publisher, owner, other, officer := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	if c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", admin, authz.KNAdmin, nil); c >= 300 {
		t.Fatalf("cài biểu: %d %s", c, b)
	}
	publish := func(day string) string {
		t.Helper()
		c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-series", publisher, authz.KNTinh, map[string]any{"code": "CNTS-BM01",
			"from_date": day, "to_date": day, "units": []string{facA.String(), facB.String()}})
		var s series
		if c >= 300 || json.Unmarshal(b, &s) != nil || len(s.Periods) != 1 {
			t.Fatalf("phát hành %s: %d %s", day, c, b)
		}
		return s.Periods[0].ID
	}
	submit := func(pid string, unit uuid.UUID) string {
		t.Helper()
		c, b := f.call(http.MethodPost, "/v1/forms/periods/"+pid+"/submissions?profile=kn_cnts", officer, authz.KNXa, map[string]any{"unit": unit.String(),
			"values": map[string]string{"trau_bo": "1", "heo": "2", "gia_cam": "3", "de": "0"}, "explanation": "Gửi bổ sung sau hạn do mất mạng"})
		var r struct {
			ID string `json:"id"`
		}
		if c >= 300 || json.Unmarshal(b, &r) != nil || r.ID == "" {
			t.Fatalf("nộp: %d %s", c, b)
		}
		return r.ID
	}
	review := func(actor uuid.UUID, sub, decision string) (int, string) {
		t.Helper()
		body := map[string]any{"decision": decision}
		if decision == "returned" {
			body["note"] = "Số heo chưa khớp sổ tiêm phòng"
		}
		c, b := f.call(http.MethodPost, "/v1/forms/submissions/"+sub+"/review?profile=kn_cnts", actor, authz.KNTinh, body)
		return c, string(b)
	}
	reviews := func(sub string) int {
		return f.adm(`SELECT count(*) FROM pf_forms.review WHERE submission_id = $1::uuid`, sub)
	}

	owned, free := publish("2026-10-01"), publish("2026-10-02")
	// Lần phát hành mới nhất chứa kỳ owned chỉ định người phụ trách (như POST period-series có owner_ref — HTTP chọn người
	// phụ trách kiểm ở reportcycle TestCNTSPublish*).
	if err := tenant.WithTenantTx(context.Background(), f.ext, f.tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_extension.cnts_publication (id, tenant_id, stream_key, department_code, template_code,
		    owner_ref, target_mode, unit_kind, units, period_from, period_to, deadline_offset, deadline_time, period_refs, task_count, event_time,
		    actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'cnts_publication:CNTS-BM01', $3, 'CNTS-BM01', $4, 'facilities', 'facility', $5::text[], '2026-10-01', '2026-10-01', 1,
		    '12:00', ARRAY[$6::uuid], 2, now() + interval '1 minute', $7, $8, 'test')`, ids.New(), f.tid, aggDept, owner,
			[]string{facA.String(), facB.String()}, owned, publisher, "seed-"+ids.New().String())
		return err
	}); err != nil {
		t.Fatal(err)
	}

	sA, sB := submit(owned, facA), submit(owned, facB)
	// Người không phụ trách (cùng phạm vi, cùng phòng) — cả xác thực lẫn yêu cầu điều chỉnh → 403, không ghi rà soát.
	for _, d := range []string{"accepted", "returned"} {
		if c, b := review(other, sA, d); c != http.StatusForbidden {
			t.Fatalf("người không phụ trách %s: %d %s", d, c, b)
		}
	}
	if c, b := review(other, sB, "accepted"); c != http.StatusForbidden {
		t.Fatalf("người không phụ trách, đơn vị khác cùng kỳ: %d %s", c, b)
	}
	if reviews(sA) != 0 || reviews(sB) != 0 {
		t.Fatalf("bị chặn mà vẫn ghi rà soát")
	}
	// Người phụ trách: xác thực X1/facA, yêu cầu điều chỉnh facB.
	if c, b := review(owner, sA, "accepted"); c != http.StatusOK {
		t.Fatalf("người phụ trách xác thực: %d %s", c, b)
	}
	if c, b := review(owner, sB, "returned"); c != http.StatusOK {
		t.Fatalf("người phụ trách yêu cầu điều chỉnh: %d %s", c, b)
	}
	// Control: kỳ không chỉ định người phụ trách — cán bộ Tỉnh khác vẫn rà soát được (quy tắc cũ).
	sF := submit(free, facA)
	if c, b := review(other, sF, "accepted"); c != http.StatusOK {
		t.Fatalf("kỳ không có người phụ trách: %d %s", c, b)
	}
}
