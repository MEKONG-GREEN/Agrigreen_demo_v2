package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// fakeFacilities: danh mục cơ sở giết mổ giả (ref → mã xã).
type fakeFacilities map[uuid.UUID]string

func (f fakeFacilities) FacilityCommunes(_ context.Context, refs []uuid.UUID, _ time.Time) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	for _, r := range refs {
		if c, ok := f[r]; ok {
			out[r] = c
		}
	}
	return out, nil
}

// noNegativeArea: quy tắc nghiệp vụ giả — area_infected ≤ 100.
type capArea struct{}

func (capArea) Validate(_ context.Context, code string, values map[string]string) []port.Issue {
	if code == "FX-02" && len(values["area_infected"]) > 3 {
		return []port.Issue{{Field: "area_infected", Reason: "quá lớn"}}
	}
	return nil
}

// W11.42: lô kỳ (Publisher), cơ sở thuộc xã, khóa lạc quan (app + DB AGF04), báo không phát sinh, lý do trễ, Trễ hạn,
// sao chép kỳ trước, công việc, đồng bộ ngoại tuyến (forms_task / forms_submission, Chờ mở kỳ).
func TestFormsCNTSPeriods_W1142(t *testing.T) {
	f := newUnitFx(t)
	p := cntsProfile
	p.RequireLateReason = true
	svc, err := NewService(f.own, f.clk, sqlApprovals{}, []port.Profile{p})
	f.must(err, "service")
	facA, facB, facX := ids.New(), ids.New(), ids.New()
	svc.Facilities = fakeFacilities{facA: "X1", facB: "X2"}
	svc.Validators = map[string]port.Validator{"cnts": capArea{}}
	f.svc = svc
	pub := Publisher{Svc: svc}

	node := ids.New()
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	tinh := f.asDept(uuid.Nil, "tinh", "chan_nuoi", node, "X1", "X2")
	xaRef := ids.New()
	xa1 := authz.WithPrincipal(f.ctx, authz.Principal{ActorRef: xaRef, TenantID: f.tid,
		KN: &authz.KNContext{Role: "xa", Department: "chan_nuoi", Scope: authz.Scope{Units: []string{"X1"}}, OrgRef: ids.New()}})
	xa1b := f.asDept(uuid.Nil, "xa", "chan_nuoi", ids.New(), "X1")
	tpls, err := svc.InstallFixtures(admin, "cnts", "")
	f.must(err, "fixture")
	ref, err := pub.LatestTemplate(admin, "cnts", "FX-02")
	f.must(err, "biểu mới nhất")
	if ref.ID != tpls[1].ID {
		t.Fatalf("LatestTemplate: %+v", ref)
	}

	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	periods := []port.PeriodDef{
		{Label: "Ngày 04/10/2026", DueAt: day(5).Add(2 * time.Hour), Kind: "day", Key: "2026-10-04", From: day(4), To: day(4), Rule: "r"},
		{Label: "Ngày 05/10/2026", DueAt: day(6).Add(2 * time.Hour), Kind: "day", Key: "2026-10-05", From: day(5), To: day(5), Rule: "r"},
	}
	_, err = pub.PublishBatch(tinh, port.BatchInput{Profile: "cnts", TemplateID: ref.ID, UnitKind: port.UnitFacility,
		Units: []string{facA.String(), facX.String()}, Periods: periods}, "")
	f.want(err, "forms.facility_invalid", "cơ sở không có trong danh mục")
	_, err = pub.PublishBatch(tinh, port.BatchInput{Profile: "cnts", TemplateID: ref.ID, UnitKind: port.UnitFacility,
		Units: []string{facA.String(), facB.String()}, UnitParents: []string{"X1", "X1"}, Periods: periods}, "")
	f.want(err, "forms.facility_invalid", "xã cha khác xã của cơ sở")
	_, err = pub.PublishBatch(xa1, port.BatchInput{Profile: "cnts", TemplateID: ref.ID, UnitKind: port.UnitFacility,
		Units: []string{facA.String()}, Periods: periods}, "")
	f.want(err, "forbidden", "Cấp Xã không phát hành")
	out, err := pub.PublishBatch(tinh, port.BatchInput{Profile: "cnts", TemplateID: ref.ID, UnitKind: port.UnitFacility,
		Units: []string{facA.String(), facB.String()}, Periods: periods}, "")
	f.must(err, "phát hành lô")
	if len(out) != 2 || out[0].Key != "2026-10-04" {
		t.Fatalf("lô: %+v", out)
	}
	d4, d5 := out[0].ID, out[1].ID
	v, err := svc.Period(tinh, "cnts", d4)
	f.must(err, "đọc kỳ")
	if v.Spec.PeriodKind != "day" || unitOf(t, v, facB.String()).Parent != "X2" {
		t.Fatalf("đặc tả lô: %+v", v.Spec)
	}
	// Đồng hồ 05/10 02:00 UTC: kỳ 04/10 hạn 05/10 02:00 → chưa quá hạn; đẩy 1 giờ → Trễ hạn.
	f.clk.Advance(time.Hour)
	ua := facA.String()
	v, _ = svc.Period(xa1, "cnts", d4)
	if len(v.Units) != 1 || v.Units[0].Status != domain.SubOverdue {
		t.Fatalf("Trễ hạn: %+v", v.Units)
	}
	zero := int64(0)
	_, err = svc.SubmitWith(xa1, "cnts", d4, SubmitInput{Unit: ua, Values: fx02, BaseVersion: &zero}, "")
	f.want(err, "forms.late_reason_required", "gửi trễ không lý do")
	_, err = svc.SubmitWith(xa1, "cnts", d4, SubmitInput{Unit: ua, Values: map[string]string{"pest": "x", "area_infected": "1000"}, BaseVersion: &zero,
		Explanation: "mất điện"}, "")
	f.want(err, "forms.values_invalid", "quy tắc nghiệp vụ")
	s1, err := svc.SubmitWith(xa1, "cnts", d4, SubmitInput{Unit: ua, Values: fx02, BaseVersion: &zero, Explanation: "mất điện", Note: "ghi chú"}, "")
	f.must(err, "gửi trễ có lý do")
	if s1.Meta == nil || !s1.Meta.Late || s1.Submission.Version != 1 {
		t.Fatalf("meta: %+v", s1.Meta)
	}
	// Khóa lạc quan: người thứ hai dựa trên phiên bản 0 → xung đột; dựa trên 1 → được.
	_, err = svc.SubmitWith(xa1b, "cnts", d4, SubmitInput{Unit: ua, Values: fx02, BaseVersion: &zero, Explanation: "trễ"}, "")
	f.want(err, "forms.submission_conflict", "khóa lạc quan")
	one := int64(1)
	_, err = svc.SubmitWith(xa1b, "cnts", d4, SubmitInput{Unit: ua, Values: map[string]string{"pest": "y", "area_infected": "5"}, NilReport: true,
		BaseVersion: &one, Explanation: "trễ"}, "")
	f.want(err, "forms.values_invalid", "không phát sinh mà có số")
	// D8: bản offline ghi trên máy TRƯỚC hạn (captured_at) nhưng máy chủ nhận SAU hạn → vẫn trễ: đòi lý do trễ; late
	// tính theo giờ máy chủ, captured_at giữ để kiểm toán.
	before := v.Period.DueAt.Add(-30 * time.Minute)
	_, err = svc.SubmitWith(xa1b, "cnts", d4, SubmitInput{Unit: ua, Values: map[string]string{"pest": "không"}, NilReport: true,
		BaseVersion: &one, CapturedAt: &before}, "")
	f.want(err, "forms.late_reason_required", "bản offline ghi trước hạn, tới sau hạn")
	s2, err := svc.SubmitWith(xa1b, "cnts", d4, SubmitInput{Unit: ua, Values: map[string]string{"pest": "không"}, NilReport: true,
		BaseVersion: &one, Explanation: "trễ", CapturedAt: &before}, "")
	f.must(err, "báo không phát sinh")
	if s2.Submission.Values["area_infected"] != "0" || s2.Submission.Values["area_treated"] != "0" || !s2.Meta.NilReport {
		t.Fatalf("không phát sinh: %+v", s2.Submission.Values)
	}
	if !s2.Meta.Late || s2.Meta.CapturedAt == nil || !s2.Meta.CapturedAt.Equal(before) {
		t.Fatalf("late theo giờ máy chủ, captured_at giữ: %+v", s2.Meta)
	}
	// DB: base_version sai → AGF04 (kể cả khi bỏ qua app).
	err = tenant.WithTenantTx(f.ctx, f.own, f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(f.ctx, `INSERT INTO pf_forms.submission_meta (id, tenant_id, stream_key, submission_id, period_id, unit_code, profile,
		    base_version, nil_report, late, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'meta:' || $3::uuid::text, $3::uuid, $3::uuid, '', '', 0, false, false, now(), $4, $5, 't')`,
			ids.New(), f.tid, s2.Submission.ID, ids.New(), "direct:"+ids.New().String())
		return e
	})
	if c, _ := pgCode(err); c != "AGF01" {
		t.Fatalf("meta ghi sau giao dịch nộp: %s %v", c, err)
	}

	// Sao chép kỳ trước: kỳ 05/10 của cơ sở A → bài cuối của kỳ 04/10.
	prev, err := svc.Previous(xa1, "cnts", d5, ua)
	f.must(err, "kỳ trước")
	if prev == nil || prev.PeriodID != d4 || prev.Submission.Version != 2 || prev.Meta == nil || !prev.Meta.NilReport {
		t.Fatalf("kỳ trước: %+v", prev)
	}
	if p0, err := svc.Previous(xa1, "cnts", d4, ua); err != nil || p0 != nil {
		t.Fatalf("kỳ đầu không có kỳ trước: %+v %v", p0, err)
	}
	_, err = svc.Previous(xa1, "cnts", d5, facB.String())
	f.want(err, "forbidden", "kỳ trước của cơ sở xã khác")

	// Công việc: Xã X1 thấy 2 kỳ × cơ sở A; Tỉnh thấy 4.
	ts, err := svc.Tasks(xa1, "cnts", TaskFilter{})
	f.must(err, "công việc xã")
	if len(ts) != 2 || ts[0].Template == nil {
		t.Fatalf("công việc xã: %d", len(ts))
	}
	if ts, _ := svc.Tasks(tinh, "cnts", TaskFilter{Code: "FX-02"}); len(ts) != 4 {
		t.Fatalf("công việc tỉnh: %d", len(ts))
	}

	// Đồng bộ: chiều xuống forms_task cho Cấp Xã.
	src := SyncSource{Svc: svc}
	chs, last, err := src.Changes(xa1, authz.Scope{Units: []string{"X1"}}, port.EntityFormsTask, 0, 100)
	f.must(err, "đồng bộ xuống")
	if len(chs) != 2 || last == 0 {
		t.Fatalf("forms_task: %d %d", len(chs), last)
	}
	var td TaskData
	for _, c := range chs {
		if c.ID == TaskID(d4, ua) {
			if err := json.Unmarshal(c.Data, &td); err != nil {
				t.Fatal(err)
			}
		}
	}
	if td.Version != 2 || !td.NilReport || len(td.Fields) == 0 || td.Status != domain.SubSubmitted {
		t.Fatalf("dữ liệu công việc: %+v", td)
	}
	if _, _, err := src.Changes(tinh, authz.Scope{All: true}, port.EntityFormsTask, 0, 100); err == nil {
		t.Fatal("Cấp Tỉnh không nhận forms_task")
	}
	// Chiều lên: gửi kỳ 05/10 (phiên bản 0), gửi lặp → duplicate; base cũ → rejected conflict.
	snk := SyncSink{Svc: svc}
	rec := func(pid uuid.UUID, base int64) syncport.Record {
		raw, _ := json.Marshal(SubmissionPayload{Profile: "cnts", PeriodID: pid, Unit: ua, Values: fx02, BaseVersion: &base})
		return syncport.Record{ID: ids.New(), IdemKey: "dev:" + ids.New().String(), Entity: port.EntityFormsSubmission, Op: syncport.OpCreate,
			EventTime: f.clk.Now(), DeviceID: ids.New(), Payload: raw}
	}
	r1 := rec(d5, 0)
	if res := snk.Accept(xa1, authz.Scope{}, r1); res.Status != syncport.StatusAccepted {
		t.Fatalf("sink: %+v", res)
	}
	if res := snk.Accept(xa1, authz.Scope{}, r1); res.Status != syncport.StatusDuplicate {
		t.Fatalf("sink gửi lặp: %+v", res)
	}
	if res := snk.Accept(xa1, authz.Scope{}, rec(d5, 0)); res.Status != syncport.StatusRejected || res.Code != "forms.submission_conflict" {
		t.Fatalf("sink xung đột: %+v", res)
	}
	chs, last2, _ := src.Changes(xa1, authz.Scope{Units: []string{"X1"}}, port.EntityFormsTask, last, 100)
	if len(chs) != 1 || chs[0].ID != TaskID(d5, ua) || last2 <= last {
		t.Fatalf("con trỏ sau nộp: %d", len(chs))
	}
	// Chốt kỳ 05/10 rồi bản offline đến → Chờ mở kỳ (accepted + forms.awaiting_reopen).
	_, err = svc.Close(tinh, "cnts", d5, "")
	f.must(err, "chốt")
	f.clk.Advance(time.Minute)
	if res := snk.Accept(xa1, authz.Scope{}, rec(d5, 1)); res.Status != syncport.StatusAccepted || res.Code != CodeAwaitingReopen {
		t.Fatalf("sink sau chốt: %+v", res)
	}
	v, _ = svc.Period(xa1, "cnts", d5)
	if v.Units[0].Status != domain.SubAwaitingReopen {
		t.Fatalf("Chờ mở kỳ: %+v", v.Units)
	}
	// Ảnh chụp: kỳ 04/10 chốt khi cơ sở B chưa nộp → Không nộp (kể cả đang Trễ hạn).
	cv, err := svc.Close(tinh, "cnts", d4, "")
	f.must(err, "chốt 04/10")
	snap, err := svc.Snapshot(tinh, "cnts", cv.Snapshots[0].ID)
	f.must(err, "ảnh chụp")
	var content domain.Snapshot
	_ = json.Unmarshal(snap.Content, &content)
	for _, u := range content.Units {
		if (u.Unit == ua && (!u.NilReport || !u.Late)) || (u.Unit == facB.String() && u.ClosedStatus != domain.SubMissed) {
			t.Fatalf("ảnh chụp: %+v", u)
		}
	}
}
