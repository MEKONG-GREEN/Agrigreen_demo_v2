package handler_test

// BF-CN-WEEKLY (PRD UP-06): kế hoạch tuần BM-05 Phần II đã đăng ký → Lịch công tác chờ tỉnh duyệt
// ("/v1/extension/cnts/weekly-plan/schedule"). Bài nộp cap/forms đọc qua formsport.SubmissionFeed (giả ở đây).

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// planFeed là SubmissionFeed giả: bài nộp cuối theo (kỳ, đơn vị).
type planFeed struct {
	mu   sync.Mutex
	subs []formsport.RecordedSubmission
}

func (f *planFeed) put(s formsport.RecordedSubmission) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.subs {
		if f.subs[i].PeriodID == s.PeriodID && f.subs[i].Unit == s.Unit {
			f.subs[i] = s
			return
		}
	}
	f.subs = append(f.subs, s)
}

func (f *planFeed) Recorded(context.Context, uuid.UUID, uuid.UUID) (*formsport.RecordedSubmission, error) {
	return nil, nil
}

func (f *planFeed) Latest(_ context.Context, _ uuid.UUID, q formsport.SubmissionQuery) ([]formsport.RecordedSubmission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []formsport.RecordedSubmission
	for _, s := range f.subs {
		if s.Profile == q.Profile && s.TemplateCode == q.TemplateCode && (len(q.Units) == 0 || s.Unit == q.Units[0]) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *planFeed) DuePeriods(context.Context, uuid.UUID, string, time.Time, time.Time) ([]formsport.PeriodProgress, error) {
	return nil, nil
}

func planSub(period uuid.UUID, unit string, by uuid.UUID, from string, values map[string]string) formsport.RecordedSubmission {
	d, _ := time.Parse(time.DateOnly, from)
	to := d.AddDate(0, 0, 6)
	return formsport.RecordedSubmission{ID: ids.New(), PeriodID: period, Profile: "kn_cnts", TemplateCode: "CNTS-BM05-II", PeriodKind: "week",
		From: &d, To: &to, UnitKind: "commune", Unit: unit, Parent: unit, Version: 1, Values: values, Submitter: by, EventTime: t0}
}

func planBody(period uuid.UUID, unit string) map[string]any {
	return map[string]any{"period_id": period.String(), "unit": unit}
}

func TestBFCNWeekly_PlanBecomesPendingSchedule(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	off, other, tt := p.off30001a, p.off30001b, p.off30002
	head := f.as(f.org.p89, port.RoleKNProvinceAdmin)
	const cnts = "chan_nuoi_thuy_san"
	f.mod.UseKNContext(fakeKNContext{byActor: map[uuid.UUID]accountsport.KNContext{
		off.actor:   {Role: port.KNRoleXa, Department: cnts, ProvinceCode: "89", Communes: []string{"30001"}},
		other.actor: {Role: port.KNRoleXa, Department: cnts, ProvinceCode: "89", Communes: []string{"30001"}},
		head.actor:  {Role: port.KNRoleTinh, Department: cnts, ProvinceCode: "89", MgmtLevel: port.MgmtLevelHead},
	}}, nil)
	feed := &planFeed{}
	f.mod.UseCNTSAlerts(extension.CNTSAlertDeps{Feed: feed})
	const path = "/v1/extension/cnts/weekly-plan/schedule"
	period := ids.New()

	// Chưa đăng ký kế hoạch → 409.
	if r := off.post(path, planBody(period, "30001")); r.status != http.StatusConflict || code(r) != "extension.cnts_plan_not_registered" {
		t.Fatalf("chưa có kế hoạch → 409: %d %v", r.status, r.body)
	}
	// Xã ngoài phạm vi, phòng khác (Trồng trọt) → 403.
	if r := off.post(path, planBody(period, "30002")); r.status != http.StatusForbidden {
		t.Fatalf("xã ngoài phạm vi → 403: %d %v", r.status, r.body)
	}
	if r := tt.post(path, planBody(period, "30002")); r.status != http.StatusForbidden {
		t.Fatalf("cán bộ Trồng trọt → 403: %d %v", r.status, r.body)
	}
	if r := off.call(http.MethodPost, path, "", planBody(period, "30001")); r.status < 400 {
		t.Fatalf("thiếu Idempotency-Key phải lỗi: %d", r.status)
	}

	// Lịch nháp có sẵn một mục tự thêm (Thứ Tư) — giữ nguyên khi đăng ký kế hoạch.
	manual := sItem("2026-09-23", "afternoon", "14:00", "16:00", "Họp tổ khuyến nông")
	off.must(off.saveSched(nextWeek, "draft", nil, manual), http.StatusCreated, "nháp tự thêm")

	plan := map[string]string{
		"t2_loai": "Làm việc chuyên môn thường xuyên tại xã", "t2_ke_hoach": "Kiểm tra đàn heo ấp 1", "t2_ket_qua_du_kien": "Nắm số đàn",
		"t3_loai": "Công tác khác", "t3_cong_tac_khac": "Họp giao ban tỉnh", "t3_ke_hoach": "Dự họp giao ban", "t3_ket_qua_du_kien": "Nắm chỉ đạo",
		"t3_dia_diem": "Sở Nông nghiệp và Môi trường",
		"t7_loai":     "Làm việc chuyên môn thường xuyên tại xã", "t7_ke_hoach": "Trực tiêm phòng dại", "t7_ket_qua_du_kien": "Tiêm 50 liều",
	}
	// Kế hoạch do cán bộ khác đăng ký → 403.
	feed.put(planSub(period, "30001", other.actor, nextWeek, plan))
	if r := off.post(path, planBody(period, "30001")); r.status != http.StatusForbidden || code(r) != "extension.cnts_plan_not_owner" {
		t.Fatalf("kế hoạch của người khác → 403: %d %v", r.status, r.body)
	}
	feed.put(planSub(period, "30001", off.actor, nextWeek, plan))

	k := key()
	d := off.must(off.call(http.MethodPost, path, k, planBody(period, "30001")), http.StatusCreated, "tạo lịch từ kế hoạch")
	if schedStatus(d) != "pending" || schedVersion(d) != 2 {
		t.Fatalf("lịch chờ duyệt, phiên bản 2: %v", d.body["schedule"])
	}
	its := items(d, "items")
	byDay := map[string]map[string]any{}
	for _, it := range its {
		byDay[str(it, "day")] = it
	}
	if len(its) != 4 || byDay["2026-09-23"] == nil || str(byDay["2026-09-23"], "content") != "Họp tổ khuyến nông" {
		t.Fatalf("giữ mục tự thêm + 3 ngày kế hoạch: %v", its)
	}
	mon, tue, sat := byDay["2026-09-21"], byDay["2026-09-22"], byDay["2026-09-26"]
	if mon == nil || tue == nil || sat == nil {
		t.Fatalf("ngày kế hoạch T2, T3, T7: %v", its)
	}
	if str(mon, "content") != "Kiểm tra đàn heo ấp 1" || str(mon, "location") != "Làm việc tại xã" || str(mon, "commune_code") != "30001" ||
		str(mon, "note") != "Kết quả dự kiến: Nắm số đàn" || str(mon, "start_time") != "07:00" || str(mon, "end_time") != "17:00" {
		t.Fatalf("mục Thứ Hai: %v", mon)
	}
	if str(tue, "location") != "Sở Nông nghiệp và Môi trường" || str(tue, "note") != "Công tác khác: Họp giao ban tỉnh · Kết quả dự kiến: Nắm chỉ đạo" {
		t.Fatalf("mục Thứ Ba (đi công tác): %v", tue)
	}
	if str(mon, "id") != app.PlanItemID(off.actor, mustDay(nextWeek), "30001", 0).String() {
		t.Fatalf("mã mục ổn định theo xã và ngày: %v", mon["id"])
	}
	// Gọi lặp cùng khóa → trả kết quả lần đầu (middleware Idempotency-Key phát lại), không thêm phiên bản.
	again := off.call(http.MethodPost, path, k, planBody(period, "30001"))
	if (again.status != http.StatusCreated && again.status != http.StatusOK) || schedID(again) != schedID(d) || schedVersion(again) != 2 {
		t.Fatalf("gọi lặp trả kết quả lần đầu: %v", again.body["schedule"])
	}
	if ev := f.scheduleOutbox("pf.extension.work_schedule.submitted", schedID(d)); len(ev) != 1 {
		t.Fatalf("gửi duyệt phát một sự kiện (chuông R-B2): %v", ev)
	}

	// Tỉnh duyệt; sửa kế hoạch (đổi Thứ Hai, bỏ Thứ Bảy) → lịch điều chỉnh, chờ duyệt lại.
	head.must(head.review(schedID(d), "approve", 2, ""), http.StatusOK, "duyệt")
	plan["t2_ke_hoach"] = "Kiểm tra đàn heo ấp 2"
	delete(plan, "t7_loai")
	delete(plan, "t7_ke_hoach")
	delete(plan, "t7_ket_qua_du_kien")
	amended := planSub(period, "30001", off.actor, nextWeek, plan)
	amended.Version = 2
	feed.put(amended)
	d2 := off.must(off.post(path, planBody(period, "30001")), http.StatusCreated, "đăng ký lại")
	if schedStatus(d2) != "pending" || schedVersion(d2) != 3 || d2.body["schedule"].(map[string]any)["amendment"] != true {
		t.Fatalf("điều chỉnh lịch đã duyệt → chờ duyệt lại: %v", d2.body["schedule"])
	}
	its = items(d2, "items")
	byDay = map[string]map[string]any{}
	for _, it := range its {
		byDay[str(it, "day")] = it
	}
	if len(its) != 3 || byDay["2026-09-26"] != nil || str(byDay["2026-09-21"], "content") != "Kiểm tra đàn heo ấp 2" ||
		byDay["2026-09-21"]["changed"] != true || byDay["2026-09-23"] == nil {
		t.Fatalf("thay mục kế hoạch, bỏ Thứ Bảy, giữ mục tự thêm: %v", its)
	}
}

// #110 (review Codex P2): cán bộ phụ trách hai xã nộp kế hoạch BM-05 Phần II cho mỗi xã cùng tuần. Chuyển kế hoạch xã B
// không được xóa mục của kế hoạch xã A; mỗi xã là một nguồn riêng (mã mục theo người × tuần × xã × ngày). Hai kế hoạch
// cùng ngày (T2–T6 bắt buộc ở mọi xã) cho mục trùng giờ — giữ cả hai, đánh dấu "trùng giờ" (cảnh báo FN-XA-03.08).
// Đăng ký lại kế hoạch xã A chỉ thay mục của A; mục tự thêm luôn giữ.
func TestBFCNWeekly_PlansFromTwoCommunesKeptSeparately(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	off := p.off30001a
	head := f.as(f.org.p89, port.RoleKNProvinceAdmin)
	const cnts = "chan_nuoi_thuy_san"
	f.mod.UseKNContext(fakeKNContext{byActor: map[uuid.UUID]accountsport.KNContext{
		off.actor:  {Role: port.KNRoleXa, Department: cnts, ProvinceCode: "89", Communes: []string{"30001", "30002"}},
		head.actor: {Role: port.KNRoleTinh, Department: cnts, ProvinceCode: "89", MgmtLevel: port.MgmtLevelHead},
	}}, nil)
	feed := &planFeed{}
	f.mod.UseCNTSAlerts(extension.CNTSAlertDeps{Feed: feed})
	const path = "/v1/extension/cnts/weekly-plan/schedule"
	week := mustDay(nextWeek)

	// Kế hoạch đầy đủ T2–T6 (hợp lệ theo ValidateCNTS của production).
	vals := func(label string) map[string]string {
		v := map[string]string{}
		for _, d := range rcdomain.WorkDays[:rcdomain.WorkdayCount] {
			v[d[0]+"_loai"] = rcdomain.WorkTypes[0]
			v[d[0]+"_ke_hoach"] = label + " " + d[1]
			v[d[0]+"_ket_qua_du_kien"] = "Hoàn thành"
		}
		if es := rcdomain.ValidateCNTS(rcdomain.CNTSBM05II, v); len(es) > 0 {
			t.Fatalf("kế hoạch thử không hợp lệ: %v", es)
		}
		return v
	}
	count := func(r resp) (a, b, manual, conflicts int) {
		for _, it := range items(r, "items") {
			switch {
			case str(it, "content") == "Họp tổ khuyến nông":
				manual++
				if it["conflict"] == true {
					t.Fatalf("mục tự thêm Thứ Bảy không trùng giờ: %v", it)
				}
			case str(it, "commune_code") == "30001":
				a++
			case str(it, "commune_code") == "30002":
				b++
			}
			if it["conflict"] == true {
				conflicts++
			}
		}
		return
	}

	// Mục tự thêm (Thứ Bảy, không trùng ngày kế hoạch) — giữ qua mọi lần chuyển kế hoạch.
	off.must(off.saveSched(nextWeek, "draft", nil, sItem("2026-09-26", "afternoon", "14:00", "16:00", "Họp tổ khuyến nông")), http.StatusCreated, "nháp tự thêm")

	pA, pB := ids.New(), ids.New()
	feed.put(planSub(pA, "30001", off.actor, nextWeek, vals("Kế hoạch A")))
	feed.put(planSub(pB, "30002", off.actor, nextWeek, vals("Kế hoạch B")))

	dA := off.must(off.post(path, planBody(pA, "30001")), http.StatusCreated, "chuyển kế hoạch xã A")
	if a, b, m, c := count(dA); a != 5 || b != 0 || m != 1 || c != 0 {
		t.Fatalf("sau A: A=%d B=%d tự thêm=%d trùng=%d", a, b, m, c)
	}
	// Chuyển kế hoạch xã B: giữ cả 5 mục A, thêm 5 mục B, cùng ngày → trùng giờ (cảnh báo, không chặn).
	dB := off.must(off.post(path, planBody(pB, "30002")), http.StatusCreated, "chuyển kế hoạch xã B")
	if a, b, m, c := count(dB); a != 5 || b != 5 || m != 1 || c != 10 {
		t.Fatalf("sau B phải giữ A: A=%d B=%d tự thêm=%d trùng=%d — %v", a, b, m, c, items(dB, "items"))
	}
	if cc := dB.body["schedule"].(map[string]any)["conflict_count"]; cc != float64(10) {
		t.Fatalf("conflict_count = 10: %v", cc)
	}
	seen := map[string]bool{}
	for _, it := range items(dB, "items") {
		seen[str(it, "id")] = true
	}
	for i := 0; i < rcdomain.WorkdayCount; i++ {
		if !seen[app.PlanItemID(off.actor, week, "30001", i).String()] || !seen[app.PlanItemID(off.actor, week, "30002", i).String()] {
			t.Fatalf("mã mục theo xã × ngày (ngày %d): %v", i, seen)
		}
	}

	// Tỉnh duyệt; sửa kế hoạch xã A (đổi nội dung Thứ Hai) → chỉ mục Thứ Hai của A đổi, mục B và mục tự thêm giữ
	// nguyên; lịch đã duyệt → điều chỉnh, chờ duyệt lại.
	head.must(head.review(schedID(dB), "approve", schedVersion(dB), ""), http.StatusOK, "duyệt")
	va := vals("Kế hoạch A")
	va["t2_ke_hoach"] = "Kế hoạch A sửa Thứ Hai"
	amended := planSub(pA, "30001", off.actor, nextWeek, va)
	amended.Version = 2
	feed.put(amended)
	dA2 := off.must(off.post(path, planBody(pA, "30001")), http.StatusCreated, "đăng ký lại kế hoạch xã A")
	if schedStatus(dA2) != "pending" || dA2.body["schedule"].(map[string]any)["amendment"] != true {
		t.Fatalf("điều chỉnh lịch đã duyệt → chờ duyệt lại: %v", dA2.body["schedule"])
	}
	if a, b, m, _ := count(dA2); a != 5 || b != 5 || m != 1 {
		t.Fatalf("đăng ký lại A chỉ thay mục A: A=%d B=%d tự thêm=%d", a, b, m)
	}
	changed := 0
	for _, it := range items(dA2, "items") {
		if it["changed"] == true {
			changed++
			if str(it, "id") != app.PlanItemID(off.actor, week, "30001", 0).String() || str(it, "content") != "Kế hoạch A sửa Thứ Hai" {
				t.Fatalf("chỉ mục Thứ Hai của xã A đổi: %v", it)
			}
		}
	}
	if changed != 1 {
		t.Fatalf("đúng một mục điều chỉnh (Thứ Hai xã A), có %d: %v", changed, items(dA2, "items"))
	}
}

func mustDay(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}
