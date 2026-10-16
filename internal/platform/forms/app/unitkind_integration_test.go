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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W11.05: đơn vị lá tổng quát. cnts: xã/cơ sở/hộ, chỉ phòng chan_nuoi; khtc: đơn vị = phòng ban, kn_tinh của
// ke_hoach là cấp phát hành, kn_tinh phòng khác là cấp lá, tối đa 3 lần gửi.
var (
	cntsProfile = port.Profile{Name: "cnts", ResolveActor: fixtureActorResolver, Hierarchy: []string{"TW", "TINH", "XA"}, Roles: map[string]string{"tinh": "TINH", "xa": "XA"},
		PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}, AllowFixtures: true,
		UnitKinds: []string{port.UnitCommune, port.UnitFacility, port.UnitHousehold}, Departments: []string{"chan_nuoi"}}
	khtcProfile = port.Profile{Name: "khtc", ResolveActor: fixtureActorResolver, Hierarchy: []string{"TINH", "PHONG"}, Roles: map[string]string{"tinh@ke_hoach": "TINH", "tinh": "PHONG"},
		PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}, AllowFixtures: true, UnitKinds: []string{port.UnitDepartment}, MaxSubmissions: 3}
)

func newUnitFx(t *testing.T) *fx {
	f := newFx(t)
	own := db.ForModule(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), DBModule)
	svc, err := NewService(own, f.clk, sqlApprovals{}, []port.Profile{fxProfile, cntsProfile, khtcProfile})
	if err != nil {
		t.Fatal(err)
	}
	f.svc, f.own = svc, own
	return f
}

// asDept như as nhưng gắn phòng ban của phiên.
func (f *fx) asDept(actor uuid.UUID, role, dept string, node uuid.UUID, units ...string) context.Context {
	if actor == uuid.Nil {
		actor = ids.New()
	}
	return fixtureContext(f.ctx, authz.Principal{ActorRef: actor, TenantID: f.tid}, port.ResolvedActor{Role: role, Department: dept, Scope: authz.Scope{Units: units}, Node: node})
}

func unitOf(t *testing.T, v PeriodView, u string) UnitView {
	t.Helper()
	for _, x := range v.Units {
		if x.Unit == u {
			return x
		}
	}
	t.Fatalf("không có đơn vị %s trong %+v", u, v.Units)
	return UnitView{}
}

var fx02 = map[string]string{"pest": "rầy nâu", "area_infected": "3.5"}

// Cơ sở GM (đơn vị cha = xã): phạm vi theo xã cha; phòng ban khác không dùng được profile; khóa kỳ được lưu.
func TestFormsFacilityUnits_W1105(t *testing.T) {
	f := newUnitFx(t)
	node := ids.New()
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	tinh := f.asDept(uuid.Nil, "tinh", "chan_nuoi", node, "X1", "X2")
	xa1 := f.asDept(uuid.Nil, "xa", "chan_nuoi", ids.New(), "X1")
	tpls, err := f.svc.InstallFixtures(admin, "cnts", "")
	f.must(err, "fixture")
	tpl := tpls[1]

	_, err = f.svc.Periods(f.asDept(uuid.Nil, "tinh", "trong_trot", node, "X1"), "cnts", 10)
	f.want(err, "forbidden", "phòng ban khác dùng profile cnts")

	from, to := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	pin := PublishInput{Profile: "cnts", TemplateID: tpl.ID, Label: "BM-01 Tuần 41/2026", DueAt: f.clk.Now().Add(48 * time.Hour),
		Units: []string{"CS-A", "CS-B"}, UnitKind: port.UnitFacility, UnitParents: []string{"X1", "X2"},
		Period: &PeriodKey{Kind: "week", Key: "2026-W41", From: &from, To: &to, Rule: "kn.period_rule/v2;week_start=wed;week=year;month=week_end"}}
	bad := pin
	bad.UnitParents = []string{"X1"}
	_, err = f.svc.Publish(tinh, bad, "")
	f.want(err, "validation_failed", "thiếu xã cha")
	bad = pin
	bad.UnitKind = port.UnitDepartment
	bad.UnitParents = nil
	_, err = f.svc.Publish(tinh, bad, "")
	f.want(err, "validation_failed", "loại đơn vị ngoài profile")
	bad = pin
	bad.UnitParents = []string{"X1", "X9"}
	_, err = f.svc.Publish(tinh, bad, "")
	f.want(err, "forbidden", "xã cha ngoài phạm vi")
	pv, err := f.svc.Publish(tinh, pin, "")
	f.must(err, "phát hành cơ sở GM")
	if pv.Spec.UnitKind != port.UnitFacility || pv.Spec.PeriodKey != "2026-W41" || pv.Spec.From == nil || !pv.Spec.From.Equal(from) ||
		unitOf(t, pv, "CS-B").Parent != "X2" {
		t.Fatalf("đặc tả kỳ: %+v %+v", pv.Spec, pv.Units)
	}
	pid := pv.Period.ID

	// Xã X1 chỉ thấy, nộp cơ sở của xã mình.
	v, err := f.svc.Period(xa1, "cnts", pid)
	if err != nil || len(v.Units) != 1 || v.Units[0].Unit != "CS-A" {
		t.Fatalf("xã 1 xem kỳ cơ sở: %+v %v", v.Units, err)
	}
	_, err = f.svc.Submit(xa1, "cnts", pid, "CS-B", fx02, "")
	f.want(err, "forbidden", "cơ sở của xã khác")
	_, err = f.svc.Submit(xa1, "cnts", pid, "CS-A", fx02, "")
	f.must(err, "nộp cơ sở A")

	// Đặc tả chỉ ghi cùng giao dịch phát hành; mỗi kỳ một dòng.
	err = tenant.WithTenantTx(f.ctx, f.own, f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(f.ctx, `INSERT INTO pf_forms.period_spec (id, tenant_id, stream_key, period_id, profile, unit_kind, unit_parents, review_flow,
		    event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, 'spec:' || $3::uuid::text, $3::uuid, '', 'commune', '{}', 'single', now(), $4, $5, 't')`,
			ids.New(), f.tid, pid, ids.New(), "direct:"+ids.New().String())
		return e
	})
	if c, _ := pgCode(err); c != "AGF01" {
		t.Fatalf("đặc tả ghi sau phát hành: %s %v", c, err)
	}
	if n := f.scalar(`SELECT count(*) FROM pf_forms.period_spec WHERE tenant_id = $1 AND period_id = $2 AND period_key = '2026-W41'`, f.tid, pid); n != 1 {
		t.Fatalf("khóa kỳ lưu ở period_spec: %d", n)
	}
}

// Phòng ban (KHTC): vai theo phòng ban, giới hạn 3 lần gửi (app + DB AGF03), yêu cầu bổ sung chặn ở lần cuối, hủy kỳ.
func TestFormsDepartmentLimitCancel_W1105(t *testing.T) {
	f := newUnitFx(t)
	node := ids.New()
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	khtc := f.asDept(uuid.Nil, "tinh", "ke_hoach", node)
	ttRef := ids.New()
	tt := f.asDept(ttRef, "tinh", "trong_trot", ids.New())
	cn := f.asDept(uuid.Nil, "tinh", "chan_nuoi", ids.New())
	tpls, err := f.svc.InstallFixtures(admin, "khtc", "")
	f.must(err, "fixture")
	pin := PublishInput{Profile: "khtc", TemplateID: tpls[1].ID, Label: "Tuần 41/2026", DueAt: f.clk.Now().Add(48 * time.Hour),
		Units: []string{"trong_trot", "chan_nuoi"}}
	_, err = f.svc.Publish(tt, pin, "")
	f.want(err, "forbidden", "phòng khác mở kỳ KHTC")
	bad := pin
	bad.Units = []string{"Trong-Trot"}
	_, err = f.svc.Publish(khtc, bad, "")
	f.want(err, "validation_failed", "mã phòng ban sai dạng")
	pv, err := f.svc.Publish(khtc, pin, "")
	f.must(err, "mở kỳ KHTC")
	if pv.Spec.UnitKind != port.UnitDepartment || pv.Spec.MaxSubmissions != 3 {
		t.Fatalf("đặc tả KHTC: %+v", pv.Spec)
	}
	pid := pv.Period.ID

	// Phòng chỉ thấy và nộp phiếu của phòng mình.
	if v, err := f.svc.Period(tt, "khtc", pid); err != nil || len(v.Units) != 1 || v.Units[0].Unit != "trong_trot" {
		t.Fatalf("phòng TT xem kỳ: %+v %v", v.Units, err)
	}
	_, err = f.svc.Submit(tt, "khtc", pid, "chan_nuoi", fx02, "")
	f.want(err, "forbidden", "nộp thay phòng khác")
	var last SubmissionView
	for i := 1; i <= 3; i++ {
		last, err = f.svc.Submit(tt, "khtc", pid, "trong_trot", fx02, "")
		f.must(err, "gửi lần")
		if i < 3 {
			_, err = f.svc.Review(khtc, "khtc", last.Submission.ID, "returned", "Bổ sung số liệu", "")
			f.must(err, "yêu cầu bổ sung")
		}
	}
	_, err = f.svc.Review(khtc, "khtc", last.Submission.ID, "returned", "Bổ sung lần nữa", "")
	f.want(err, "forms.submission_limit", "yêu cầu bổ sung sau lần gửi thứ 3")
	_, err = f.svc.Submit(tt, "khtc", pid, "trong_trot", fx02, "")
	f.want(err, "forms.submission_limit", "gửi lần 4")
	// DB: phiên bản 4 ghi thẳng → AGF03.
	err = tenant.WithTenantTx(f.ctx, f.own, f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(f.ctx, `INSERT INTO pf_forms.submission (id, tenant_id, stream_key, period_id, unit_code, profile, template_id, "values", submitter_ref,
		    event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, 'sub:' || $3::uuid::text || ':trong_trot', $3::uuid, 'trong_trot', '', $3::uuid, '{}',
		    $4, now(), $4, $5, 't')`, ids.New(), f.tid, pid, ttRef, "direct:"+ids.New().String())
		return e
	})
	if c, _ := pgCode(err); c != "AGF03" {
		t.Fatalf("giới hạn lần gửi ở DB: %s %v", c, err)
	}

	// Hủy kỳ: chỉ cấp close_by của Nút phát hành, lý do bắt buộc; sau hủy không nộp/chốt/mở lại.
	_, err = f.svc.Cancel(tt, "khtc", pid, "Kỳ mở nhầm tuần", "")
	f.want(err, "forbidden", "phòng khác hủy kỳ")
	_, err = f.svc.Cancel(khtc, "khtc", pid, "ngắn", "")
	f.want(err, "validation_failed", "lý do ngắn")
	cv, err := f.svc.Cancel(khtc, "khtc", pid, "Kỳ mở nhầm tuần", "")
	f.must(err, "hủy kỳ")
	if cv.State.Status != domain.PeriodCancelled || cv.Events[len(cv.Events)-1].Action != port.ActionCancelled {
		t.Fatalf("kỳ sau hủy: %+v", cv.State)
	}
	_, err = f.svc.Submit(cn, "khtc", pid, "chan_nuoi", fx02, "")
	f.want(err, "forms.period_cancelled", "nộp vào kỳ đã hủy")
	_, err = f.svc.Close(khtc, "khtc", pid, "")
	f.want(err, "forms.period_cancelled", "chốt kỳ đã hủy")
	_, err = f.svc.Cancel(khtc, "khtc", pid, "Hủy lần hai cho chắc", "")
	f.want(err, "forms.period_cancelled", "hủy hai lần")
	// DB: sau cancelled không có sự kiện native nào (AGF01).
	err = tenant.WithTenantTx(f.ctx, f.own, f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(f.ctx, `INSERT INTO pf_forms.period_event (id, tenant_id, stream_key, period_ref, profile, origin, action, mode, event_time, actor_ref,
		    idem_key, rule_version) VALUES ($1, $2, 'period:' || $3::uuid::text, $3::uuid, 'khtc', 'native', 'closed', 'manual', now(), $4, $5, 't')`,
			ids.New(), f.tid, pid, ttRef, "direct:"+ids.New().String())
		return e
	})
	if c, _ := pgCode(err); c != "AGF01" {
		t.Fatalf("chốt sau hủy ở DB: %s %v", c, err)
	}
}

// Trạng thái lúc chốt: Không nộp, Đã chốt – chưa điều chỉnh (ảnh chụp và phép chiếu), Chờ mở kỳ (bản offline sau chốt).
func TestFormsClosedStatuses_W1105(t *testing.T) {
	f := newUnitFx(t)
	node := ids.New()
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	tinh := f.as(f.tid, uuid.Nil, "tinh", node, "U1", "U2", "U3")
	xa1 := f.as(f.tid, uuid.Nil, "xa", ids.New(), "U1")
	xa2 := f.as(f.tid, uuid.Nil, "xa", ids.New(), "U2")
	xa3 := f.as(f.tid, uuid.Nil, "xa", ids.New(), "U3")
	tpls, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "fixture")
	pv, err := f.svc.Publish(tinh, PublishInput{Profile: "fx", TemplateID: tpls[1].ID, Label: "Kỳ", DueAt: f.clk.Now().Add(time.Hour),
		Units: []string{"U1", "U2", "U3"}}, "")
	f.must(err, "phát hành")
	pid := pv.Period.ID
	if pv.Spec.UnitKind != port.UnitCommune || pv.Spec.MaxSubmissions != 0 {
		t.Fatalf("mặc định profile cũ: %+v", pv.Spec)
	}
	s1, err := f.svc.Submit(xa1, "fx", pid, "U1", fx02, "")
	f.must(err, "U1 nộp")
	_, err = f.svc.Review(tinh, "fx", s1.Submission.ID, "accepted", "", "")
	f.must(err, "chấp nhận U1")
	s2, err := f.svc.Submit(xa2, "fx", pid, "U2", fx02, "")
	f.must(err, "U2 nộp")
	_, err = f.svc.Review(tinh, "fx", s2.Submission.ID, "returned", "Cần đối chiếu", "")
	f.must(err, "trả lại U2")
	// Kỳ đang mở: bản offline nộp bình thường.
	f.clk.Advance(time.Minute)
	cv, err := f.svc.Close(tinh, "fx", pid, "")
	f.must(err, "chốt")
	if unitOf(t, cv, "U1").Status != domain.SubAccepted || unitOf(t, cv, "U2").Status != domain.SubClosedUnadjusted ||
		unitOf(t, cv, "U3").Status != domain.SubMissed {
		t.Fatalf("trạng thái sau chốt: %+v", cv.Units)
	}
	snap, err := f.svc.Snapshot(tinh, "fx", cv.Snapshots[0].ID)
	f.must(err, "ảnh chụp")
	var content domain.Snapshot
	f.must(json.Unmarshal(snap.Content, &content), "nội dung ảnh chụp")
	got := map[string]string{}
	for _, u := range content.Units {
		got[u.Unit] = u.ClosedStatus
	}
	if got["U2"] != domain.SubClosedUnadjusted || got["U3"] != domain.SubMissed || content.RuleVersion != domain.RuleVersion {
		t.Fatalf("ảnh chụp ghi trạng thái lúc chốt: %+v", got)
	}

	// Bản offline đến sau chốt → giữ "Chờ mở kỳ" (202), không vào số liệu kỳ.
	_, err = f.svc.Submit(xa3, "fx", pid, "U3", fx02, "")
	f.want(err, "forms.period_closed", "nộp trực tuyến sau chốt")
	f.clk.Advance(time.Minute)
	hv, err := f.svc.SubmitOffline(xa3, "fx", pid, "U3", fx02, f.clk.Now().Add(-2*time.Hour), "")
	f.must(err, "bản offline sau chốt")
	if hv.Status != domain.SubAwaitingReopen {
		t.Fatalf("bản offline: %+v", hv)
	}
	v, err := f.svc.Period(tinh, "fx", pid)
	f.must(err, "đọc kỳ")
	if u := unitOf(t, v, "U3"); u.Status != domain.SubAwaitingReopen || u.Held == nil || u.Submission != nil {
		t.Fatalf("U3 chờ mở kỳ: %+v", u)
	}
	// Mở lại một phần cho U3: hết "Chờ mở kỳ", nộp lại bình thường; U2 ngoài phạm vi mở lại vẫn "chưa điều chỉnh".
	f.clk.Advance(time.Minute)
	rv, err := f.svc.Reopen(tinh, "fx", pid, ReopenInput{Units: []string{"U3"}, Reason: "Nhận bản offline của xã U3"}, "")
	f.must(err, "mở lại U3")
	if unitOf(t, rv, "U3").Status != domain.SubNone || unitOf(t, rv, "U2").Status != domain.SubClosedUnadjusted {
		t.Fatalf("sau mở lại một phần: %+v", rv.Units)
	}
	// Kỳ đang nhận bài của U3: SubmitOffline = nộp bình thường.
	ov, err := f.svc.SubmitOffline(xa3, "fx", pid, "U3", fx02, f.clk.Now(), "")
	f.must(err, "offline khi kỳ mở")
	if ov.Status != domain.SubSubmitted || ov.Submission.Version != 1 {
		t.Fatalf("offline khi kỳ mở: %+v", ov)
	}
	// U2 (ngoài phạm vi mở lại) gửi offline → giữ lại.
	if hv, err := f.svc.SubmitOffline(xa2, "fx", pid, "U2", fx02, f.clk.Now(), ""); err != nil || hv.Status != domain.SubAwaitingReopen {
		t.Fatalf("U2 offline ngoài phạm vi mở lại: %+v %v", hv, err)
	}
	// DB: giữ bài khi kỳ đang nhận bài của đơn vị → AGF02.
	err = tenant.WithTenantTx(f.ctx, f.own, f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(f.ctx, `INSERT INTO pf_forms.held_submission (id, tenant_id, stream_key, period_id, unit_code, profile, template_id, "values",
		    submitter_ref, captured_at, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'held:' || $3::uuid::text || ':U3', $3::uuid, 'U3', '', $3::uuid, '{}', $4, now(), now(), $4, $5, 't')`,
			ids.New(), f.tid, pid, ids.New(), "direct:"+ids.New().String())
		return e
	})
	if c, _ := pgCode(err); c != "AGF02" {
		t.Fatalf("giữ bài khi kỳ mở ở DB: %s %v", c, err)
	}
	if n := f.scalar(`SELECT count(*) FROM shared.v_missing_immutability WHERE schema_name = 'pf_forms'`); n != 0 {
		t.Fatalf("pf_forms thiếu chặn sửa/xóa: %d", n)
	}
	if n := f.scalar(`SELECT count(*) FROM shared.v_missing_rls WHERE schema_name = 'pf_forms'`); n != 0 {
		t.Fatalf("pf_forms thiếu RLS: %d", n)
	}
}

// Ghi song song: 'cancelled' cần lý do; kỳ mirror vẫn giữ máy trạng thái của bên dùng.
func TestFormsMirrorCancelled_W1105(t *testing.T) {
	f := newUnitFx(t)
	ref := ids.New()
	err := tenant.WithTenantTx(f.ctx, f.own, f.tid, func(tx pgx.Tx) error {
		ctx := db.JoinTx(f.ctx, tx)
		in := port.MirrorInput{Profile: "fx", PeriodRef: ref, Label: "Tuần 41", Action: port.ActionCancelled, Actor: ids.New(), At: f.clk.Now(), IdemKey: "c1"}
		if _, err := f.svc.Record(ctx, in); err == nil {
			t.Fatal("hủy không lý do được ghi")
		}
		in.Reason = "Thu hồi đợt phát hành nhầm"
		r, err := f.svc.Record(ctx, in)
		if err != nil || r.SnapshotID != nil {
			t.Fatalf("mirror cancelled: %+v %v", r, err)
		}
		return nil
	})
	f.must(err, "giao dịch bên dùng")
	if n := f.scalar(`SELECT count(*) FROM pf_forms.period_event WHERE tenant_id = $1 AND period_ref = $2 AND action = 'cancelled' AND origin = 'mirror'`,
		f.tid, ref); n != 1 {
		t.Fatalf("sự kiện cancelled mirror: %d", n)
	}
}
