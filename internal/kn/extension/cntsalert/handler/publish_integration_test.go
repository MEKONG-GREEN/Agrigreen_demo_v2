package handler_test

// BF-CN-PUBLISH (#108, CN-07) trên PostgreSQL thật: nút Nhắc của màn theo dõi biểu kỳ (POST /v1/extension/cnts/
// period-reminders — chỉ đơn vị chưa gửi / bị trả lại, kênh + nội dung mẫu của biểu, tối đa một lượt mỗi đơn vị mỗi giờ,
// Cấp Xã không nhắc) và cảnh báo trễ liên tiếp gửi đúng cán bộ tỉnh phụ trách đã chọn khi phát hành (sổ
// cnts_publication) thay cho mọi người có cờ phụ trách biểu.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// control giả cap/forms: tiến độ kỳ từ feed.due (người gọi thấy mọi kỳ — phạm vi đã kiểm ở cap/forms thật).
type control struct{ f *feed }

func (c control) Progress(_ context.Context, _ string, id uuid.UUID) (formsport.PeriodProgress, error) {
	for _, p := range c.f.due {
		if p.ID == id {
			return p, nil
		}
	}
	return formsport.PeriodProgress{}, errors.New("forms: không thấy kỳ")
}
func (control) ClosePeriod(context.Context, string, uuid.UUID, string) error          { return nil }
func (control) CancelPeriod(context.Context, string, uuid.UUID, string, string) error { return nil }
func (control) ChangeDue(context.Context, string, uuid.UUID, time.Time, string, string) error {
	return nil
}

// seedPublication ghi một lần phát hành (sổ cnts_publication) gồm các kỳ với người phụ trách owner.
func (f *fix) seedPublication(owner uuid.UUID, periods ...uuid.UUID) {
	f.t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.tenant.String()); err != nil {
		f.t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_publication (id, tenant_id, stream_key, department_code, template_code, owner_ref,
	    target_mode, unit_kind, units, period_from, period_to, deadline_offset, deadline_time, period_refs, task_count, event_time, actor_ref,
	    idem_key, rule_version)
	  VALUES ($1, $2, 'cnts_publication:CNTS-BM04', $3, 'CNTS-BM04', $4, 'province', 'commune', ARRAY['30001','30002'], '2027-10-05', '2027-10-18',
	    1, '12:00', $5::uuid[], 4, now(), $6, $7, 'test')`, ids.New(), f.tenant, cnts, owner, refStrs(periods), ids.New(), "seed-"+ids.New().String()); err != nil {
		f.t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		f.t.Fatal(err)
	}
}

func TestCNTSPublishRemindAndOwner_BF_CN_PUBLISH(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	tinh := f.user(f.dir.tinh, authz.KNContext{Role: authz.KNTinh, Department: cnts, Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}})
	xa1 := f.user(f.dir.xa1, authz.KNContext{Role: authz.KNXa, Department: cnts, Communes: []string{"30001"}, Scope: authz.Scope{Units: []string{"30001"}}})

	// Chưa nối tiến độ kỳ native → 503.
	want(t, tinh.post("/v1/extension/cnts/period-reminders", map[string]any{"period_id": ids.New().String()}), 503,
		"extension.dependency_unavailable", "thiếu cap/forms")
	f.mod.UseCNTSCampaigns(extension.CNTSCampaignDeps{Control: control{f.feed}})

	// Kênh và nội dung mẫu của biểu: chỉ hộp thư (bỏ push).
	want(t, tinh.put("/v1/extension/cnts/alert-config/forms/CNTS-BM04", map[string]any{"before_due_hours": 24, "on_overdue": true,
		"repeat_daily": false, "repeat_at": "", "late_streak": 2, "channels": []string{"in_app"}, "message": "Tỉnh nhắc nộp {bieu} kỳ {ky}"}), 200, "", "cấu hình")

	due := t0.Add(48 * time.Hour) // chưa tới hạn → lượt "before"
	w := ids.New()
	f.feed.due = []formsport.PeriodProgress{{ID: w, Profile: "kn_cnts", Label: "Tuần 42", TemplateCode: "CNTS-BM04", DueAt: due, Status: "open",
		PeriodKey: "2027-W42", Units: []formsport.UnitProgress{
			{Unit: "30001", Parent: "30001", Status: "not_submitted"},
			{Unit: "30002", Parent: "30002", Status: "submitted", Versions: 1},
		}}}

	want(t, xa1.post("/v1/extension/cnts/period-reminders", map[string]any{"period_id": w.String()}), 403, "", "Cấp Xã không nhắc")
	want(t, tinh.post("/v1/extension/cnts/period-reminders", map[string]any{"period_id": w.String(), "units": []string{"30009"}}), 422,
		"validation_failed", "đơn vị ngoài kỳ")
	r := want(t, tinh.post("/v1/extension/cnts/period-reminders", map[string]any{"period_id": w.String()}), 200, "", "nhắc")
	if num(r, "reminded").String() != "1" || len(list(r, "skipped")) != 0 {
		t.Fatalf("nhắc đơn vị chưa gửi: %v", r)
	}
	rem := f.outbox(port.EventCNTSFormReminded)
	if len(rem) != 1 || rem[0]["unit"] != "30001" || rem[0]["slot"] != "before" || rem[0]["message"] != "Tỉnh nhắc nộp BM-04 Nhập liệu tuần chăn nuôi kỳ Tuần 42" {
		t.Fatalf("sự kiện nhắc tay: %v", rem)
	}
	if ch, _ := rem[0]["channels"].([]any); len(ch) != 1 || ch[0] != "in_app" {
		t.Fatalf("kênh của biểu: %v", rem[0]["channels"])
	}
	if rs, _ := rem[0]["recipient_refs"].([]any); len(rs) != 1 || rs[0] != f.dir.xa1.String() {
		t.Fatalf("người nhận là cán bộ xã: %v", rem[0]["recipient_refs"])
	}
	r = want(t, tinh.post("/v1/extension/cnts/period-reminders", map[string]any{"period_id": w.String()}), 200, "", "nhắc lại trong giờ")
	if num(r, "reminded").String() != "0" || len(list(r, "skipped")) != 1 || len(f.outbox(port.EventCNTSFormReminded)) != 1 {
		t.Fatalf("tối đa một lượt mỗi giờ: %v", r)
	}

	// Trả lại (yêu cầu điều chỉnh) cũng được nhắc; đơn vị đã nộp xong thì không còn gì để nhắc → 422.
	f.feed.due[0].Units = []formsport.UnitProgress{{Unit: "30002", Parent: "30002", Status: "returned", Versions: 1}}
	r = want(t, tinh.post("/v1/extension/cnts/period-reminders", map[string]any{"period_id": w.String()}), 200, "", "nhắc đơn vị bị trả lại")
	if num(r, "reminded").String() != "1" {
		t.Fatalf("nhắc trả lại: %v", r)
	}
	f.feed.due[0].Units = []formsport.UnitProgress{{Unit: "30002", Parent: "30002", Status: "accepted", Versions: 2}}
	want(t, tinh.post("/v1/extension/cnts/period-reminders", map[string]any{"period_id": w.String()}), 422, "validation_failed", "không còn đơn vị")
	f.feed.due[0].Status = "closed"
	want(t, tinh.post("/v1/extension/cnts/period-reminders", map[string]any{"period_id": w.String()}), 409, "conflict", "kỳ đã chốt")

	// Trễ liên tiếp: kỳ phát hành có người phụ trách → chỉ người đó (+ Lãnh đạo phòng), không phải mọi người có cờ.
	owner := ids.New()
	w41, w42 := ids.New(), ids.New()
	d42 := time.Date(2027, 10, 19, 5, 0, 0, 0, time.UTC)
	f.feed.due = []formsport.PeriodProgress{
		{ID: w42, Profile: "kn_cnts", Label: "Tuần 42", TemplateCode: "CNTS-BM04", DueAt: d42, Status: "open", PeriodKey: "2027-W42",
			Units: []formsport.UnitProgress{{Unit: "30001", Parent: "30001", Status: "overdue"}}},
		{ID: w41, Profile: "kn_cnts", Label: "Tuần 41", TemplateCode: "CNTS-BM04", DueAt: d42.AddDate(0, 0, -7), Status: "closed", PeriodKey: "2027-W41",
			Units: []formsport.UnitProgress{{Unit: "30001", Parent: "30001", Status: "submitted", Versions: 1, Late: true}}},
	}
	f.seedPublication(owner, w41, w42)
	if r, err := f.mod.CNTSAlertJobRun(ctx, f.tenant, d42.Add(time.Hour)); err != nil || r.Streaks != 1 {
		t.Fatalf("trễ liên tiếp: %+v %v", r, err)
	}
	ls := lastKind(f, "late_streak")
	rs, _ := ls["recipient_refs"].([]any)
	got := map[string]bool{}
	for _, x := range rs {
		got[x.(string)] = true
	}
	if len(rs) != 2 || !got[owner.String()] || !got[f.dir.lead.String()] || got[f.dir.tinh.String()] {
		t.Fatalf("người phụ trách của kỳ + Lãnh đạo phòng: %v", ls)
	}
}

func refStrs(xs []uuid.UUID) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.String())
	}
	return out
}
