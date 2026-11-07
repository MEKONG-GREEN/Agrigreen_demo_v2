package kn77_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// S-4 exercises real module routes with real PostgreSQL and the existing KN
// session projection. A hidden copied menu cannot still be called through API.
func TestStandaloneDepartmentAPI(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	for _, department := range []string{"chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"} {
		for _, role := range []string{authz.KNTW, authz.KNTinh, authz.KNXa} {
			c := f.kn(f.tenant, role, department, f.p89, []string{f.communes89[0]})
			for _, path := range []string{"/v1/extension/report-forms?report_type=weekly", "/v1/extension/report-batches", "/v1/extension/report-tasks", "/v1/extension/kn-periods", "/v1/extension/kn-dashboard", "/v1/extension/consolidations?period_id=invalid"} {
				c.fail(c.get(path), http.StatusForbidden, "forbidden", role+"/"+department+path)
			}
			c.fail(c.post("/v1/extension/report-batches/preview", map[string]any{}), http.StatusForbidden, "forbidden", "direct publish preview outside TT")
			c.fail(c.post("/v1/extension/report-batches", map[string]any{}), http.StatusForbidden, "forbidden", "direct publish outside TT")
			if department != "chan_nuoi_thuy_san" {
				c.fail(c.get("/v1/extension/dept-docs"), http.StatusForbidden, "forbidden", "FN20 outside CNTS")
				c.fail(c.post("/v1/extension/dept-docs/folders", map[string]any{"name": "Scope"}), http.StatusForbidden, "forbidden", "FN20 write outside CNTS")
			}
		}
	}
	tt := f.kn(f.tenant, authz.KNXa, dept, f.p89, []string{f.communes89[0]})
	tt.must(tt.get("/v1/extension/report-tasks"), http.StatusOK, "TT FN-XA-03.01 remains available")
	cn := f.kn(f.tenant, authz.KNTinh, "chan_nuoi_thuy_san", f.p89, nil)
	cn.must(cn.get("/v1/extension/dept-docs"), http.StatusOK, "CNTS FN20 remains available")
	admin := f.kn(f.tenant, authz.KNAdmin, "", f.p89, nil)
	admin.fail(admin.get("/v1/extension/dept-docs?department=trong_trot"), http.StatusForbidden, "forbidden", "Admin cannot restore removed TT FN20")
	admin.must(admin.get("/v1/extension/dept-docs?department=chan_nuoi_thuy_san"), http.StatusOK, "Admin CNTS FN20 remains available")
	tinh := f.kn(f.tenant, authz.KNTinh, dept, f.p89, nil)
	tinh.fail(tinh.post("/v1/extension/report-batches/preview", map[string]any{"report_type": "adhoc"}), http.StatusForbidden, "forbidden", "TT Q4 ad hoc cannot be recreated")
}

// Historical records are inserted as pre-S-4 fixture state with INSERT only;
// the current API must keep reads, reject every write, and preserve exact rows.
func TestStandaloneTTAdhocHistoryReadOnly(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	tinh := f.kn(f.tenant, authz.KNTinh, dept, f.p89, nil)
	xa := f.kn(f.tenant, authz.KNXa, dept, f.p89, []string{f.communes89[0]})
	record := seedStandaloneRecord(t, f, dept, "adhoc", tinh.actor, xa.actor, repport.FormVersion{TemplateID: ids.New(), VersionID: ids.New(), Code: "OLD-ADHOC", Name: "Historical", Version: 1})
	batch, period, task := record.batch, record.period, record.task
	snapshot := func() string { return standaloneSnapshot(t, f, batch, period, task) }
	before := snapshot()
	tinh.must(tinh.get("/v1/extension/report-batches/"+batch.String()), http.StatusOK, "historical batch remains readable")
	tinh.must(tinh.get("/v1/extension/report-periods/"+period.String()), http.StatusOK, "historical period remains readable")
	xa.must(xa.get("/v1/extension/report-tasks/"+task.String()+"/history"), http.StatusOK, "historical task remains readable within commune")
	for _, write := range []struct {
		who  caller
		path string
		body any
	}{
		{tinh, "/v1/extension/report-batches/" + batch.String() + "/withdraw", map[string]any{"reason": "Historical feature was removed"}},
		{tinh, "/v1/extension/report-periods/" + period.String() + "/close", nil},
		{tinh, "/v1/extension/consolidations", map[string]any{"period_id": period.String()}},
		{tinh, "/v1/extension/report-batches/" + batch.String() + "/reminders", map[string]any{"period_id": period.String(), "message": "Historical reminder", "all_pending": true}},
		{xa, "/v1/extension/report-tasks/" + task.String() + "/submissions", map[string]any{"kind": "draft", "rows": []any{map[string]any{"cells": map[string]string{"A": "1"}}}}},
	} {
		write.who.fail(write.who.post(write.path, write.body), http.StatusForbidden, "forbidden", write.path)
	}
	if snapshot() != before {
		t.Fatal("removed ad hoc writes changed historical records or ledgers")
	}
	result, err := f.ext.ReportCycleJobRun(context.Background(), f.tenant, t0.Add(3*time.Hour))
	if err != nil || result.Closed != 0 || result.Reclosed != 0 || result.Reminders != 0 {
		t.Fatalf("removed ad hoc job: %+v %v", result, err)
	}
	if snapshot() != before {
		t.Fatal("background jobs changed historical ad hoc records")
	}

	other := f.kn(f.tenant, authz.KNXa, dept, f.p89, []string{f.communes89[1]})
	other.fail(other.get("/v1/extension/report-tasks/"+task.String()+"/history"), http.StatusNotFound, "extension.report_task_not_found", "historical read does not expand commune scope")
}

type standaloneRecord struct{ batch, period, task uuid.UUID }

// Pre-S-4 state is seeded directly so no removed feature must be re-enabled.
func seedStandaloneRecord(t *testing.T, f *fixture, department, reportType string, issuer, assignee uuid.UUID, form repport.FormVersion, states ...standaloneRecordState) standaloneRecord {
	t.Helper()
	periodType := reportType
	if reportType == "weekly" {
		periodType = "week"
	}
	state := standaloneRecordState{status: "open"}
	if len(states) > 0 {
		state = states[0]
	}
	batch, period, task := ids.New(), ids.New(), ids.New()
	event := ids.New()
	forms, err := json.Marshal([]map[string]any{{"template_id": form.TemplateID, "version_id": form.VersionID, "code": form.Code, "name": form.Name, "version": form.Version}})
	if err != nil {
		t.Fatal(err)
	}
	err = tenant.WithTenantTx(context.Background(), f.extPool, f.tenant, func(tx pgx.Tx) error {
		// Pre-S-4 fixture state uses INSERT only; current public writes are disabled.
		if _, err := tx.Exec(context.Background(), `INSERT INTO pf_extension.report_batch_event
    (id,tenant_id,stream_key,batch_id,action,department_code,province_code,payload,event_time,actor_ref,idem_key,rule_version)
    VALUES ($1,$2,$3,$4,'issued',$5,'89','{}',$6,$7,$8,'extension.reportcycle.v1')`, event, f.tenant, "batch:"+batch.String(), batch, department, t0, issuer, key()); err != nil {
			return err
		}
		if _, err := tx.Exec(context.Background(), `INSERT INTO pf_extension.report_batch
    (tenant_id,id,department_code,province_code,report_type,period_type,season_label,from_date,to_date,period_rule,deadline_offset_days,deadline_time,close_after_days,forms,communes,status,period_count,task_count,issued_by,issued_at,last_event_id)
    VALUES ($1,$2,$3,'89',$10,$11,'Historical',$4::date,$4::date,'kn.period_rule/v1-tam',0,'17:00',0,$5::jsonb,ARRAY[$6]::text[],'issued',1,1,$7,$9,$8)`, f.tenant, batch, department, t0.Format(time.DateOnly), string(forms), f.communes89[0], issuer, event, t0, reportType, periodType); err != nil {
			return err
		}
		if _, err := tx.Exec(context.Background(), `INSERT INTO pf_extension.report_period
    (tenant_id,id,batch_id,slot,label,range_label,from_date,to_date,due_at,auto_close_at,status,department_code,province_code,closed_at,closed_by,close_mode)
    VALUES ($1,$2,$3,1,'Historical','Historical',$4::date,$4::date,$5,$6,$8,$7,'89',CASE WHEN $8='closed' THEN $9::timestamptz END,CASE WHEN $8='closed' THEN $10::uuid END,CASE WHEN $8='closed' THEN 'manual' END)`, f.tenant, period, batch, t0.Format(time.DateOnly), t0.Add(time.Hour), t0.Add(2*time.Hour), department, state.status, t0, issuer); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_extension.report_task
    (tenant_id,id,batch_id,period_id,template_id,version_id,form_code,department_code,province_code,commune_code,region_code,slot,period_block,due_at,status,assignees,created_at,updated_at,reopened_until)
    VALUES ($1,$2,$3,$4,$5,$6,$13,$7,'89',$8,'KV1',1,false,$9,'not_started',ARRAY[$10]::uuid[],$11,$11,$12)`, f.tenant, task, batch, period, form.TemplateID, form.VersionID, department, f.communes89[0], t0.Add(time.Hour), assignee, t0, state.reopenedUntil, form.Code)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return standaloneRecord{batch: batch, period: period, task: task}
}

func standaloneSnapshot(t *testing.T, f *fixture, batch, period, task uuid.UUID) string {
	t.Helper()
	var value string
	err := tenant.WithTenantTx(context.Background(), f.extPool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT jsonb_build_object(
     'batch',(SELECT to_jsonb(r) FROM pf_extension.report_batch r WHERE id=$1),
     'period',(SELECT to_jsonb(r) FROM pf_extension.report_period r WHERE id=$2),
     'task',(SELECT to_jsonb(r) FROM pf_extension.report_task r WHERE id=$3),
     'submissions',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM pf_extension.submission_version r),
     'batch_events',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM pf_extension.report_batch_event r),
     'period_events',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM pf_extension.period_event r),
     'task_events',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM pf_extension.report_task_event r),
 'runs',(SELECT count(*) FROM pf_extension.consolidation_run),
 'reminders',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM pf_extension.reminder_event r))::text`, batch, period, task).Scan(&value)
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestStandaloneAdminForeignHistoryReadOnly(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	templates := f.publishForms("T01")
	form, err := f.rep.Forms().ApplicableVersion(context.Background(), f.tenant, uuid.MustParse(templates["T01"]), t0)
	if err != nil {
		t.Fatal(err)
	}
	admin := f.kn(f.tenant, authz.KNAdmin, "", f.p89, nil)
	for _, department := range []string{"chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"} {
		t.Run(department, func(t *testing.T) {
			record := seedStandaloneRecord(t, f, department, "weekly", admin.actor, admin.actor, form)
			batch, period, task := record.batch.String(), record.period.String(), record.task.String()
			before := standaloneSnapshot(t, f, record.batch, record.period, record.task)
			outboxBefore := f.count(`SELECT count(*) FROM shared.outbox WHERE tenant_id=$1`, f.tenant)
			admin.must(admin.get("/v1/extension/report-batches/"+batch), http.StatusOK, "Admin reads historical foreign batch")
			admin.must(admin.get("/v1/extension/report-periods/"+period), http.StatusOK, "Admin reads historical foreign period")
			admin.must(admin.get("/v1/extension/report-tasks/"+task+"/history"), http.StatusOK, "Admin reads historical foreign task")
			// Existing Admin authority writes through a selected support context only.
			// A foreign department cannot borrow TT eligibility in either support role.
			callers := []caller{admin}
			for _, role := range []string{authz.KNTinh, authz.KNXa} {
				support := f.knFor(f.tenant, admin.actor, role, department, f.p89, []string{f.communes89[0]}, &authz.KNSupport{SessionID: ids.New()})
				support.reason = "Hỗ trợ kiểm tra quyền chức năng của phòng"
				callers = append(callers, support)
			}
			for _, who := range callers {
				for _, write := range []struct {
					path string
					body any
				}{
					{"/v1/extension/report-batches/preview", map[string]any{"report_type": "weekly"}},
					{"/v1/extension/report-batches", map[string]any{"report_type": "weekly"}},
					{"/v1/extension/report-batches/" + batch + "/withdraw", map[string]any{"reason": "Historical feature was removed"}},
					{"/v1/extension/report-batches/" + batch + "/deadline", map[string]any{"offset_days": 1, "time": "17:00", "reason": "Historical feature was removed"}},
					{"/v1/extension/report-periods/" + period + "/close", nil},
					{"/v1/extension/report-periods/" + period + "/reopen", map[string]any{"reason": "Historical feature was removed", "until": t0.Add(time.Hour).Format(time.RFC3339)}},
					{"/v1/extension/consolidations", map[string]any{"period_id": period}},
					{"/v1/extension/report-batches/" + batch + "/reminders", map[string]any{"period_id": period, "message": "Historical reminder", "all_pending": true}},
					{"/v1/extension/report-tasks/" + task + "/submissions", map[string]any{"kind": "draft", "rows": []any{map[string]any{"input": map[string]string{"F": "1"}}}}},
					{"/v1/extension/report-tasks/" + task + "/adjustment-request", map[string]any{"reason": "Historical feature was removed"}},
					{"/v1/extension/report-tasks/" + task + "/confirm", map[string]any{}},
				} {
					who.fail(who.post(write.path, write.body), http.StatusForbidden, "forbidden", write.path)
				}
			}
			if standaloneSnapshot(t, f, record.batch, record.period, record.task) != before || f.count(`SELECT count(*) FROM shared.outbox WHERE tenant_id=$1`, f.tenant) != outboxBefore {
				t.Fatal("403 changed historical foreign rows, append-only ledgers or outbox")
			}
		})
	}
	// Configure the same server-owned custom TT profile contract covered against
	// real accounts/party by TestKNSPLIT2MenuParityConfiguredMatchesCompatibility.
	f.departments.profiles["pb_trong_trot"] = "trong_trot"
	f.departments.jobDepartments = append(f.departments.jobDepartments, partyport.ProvinceDepartment{ProvinceCode: "89", Department: partyport.Department{Code: "pb_trong_trot", MenuProfile: "trong_trot", Status: partyport.DeptActive}})
	for _, department := range []string{dept, "pb_trong_trot"} {
		officer := ids.New()
		who := f.knFor(f.tenant, admin.actor, authz.KNXa, department, f.p89, []string{f.communes89[0]}, &authz.KNSupport{SessionID: ids.New(), OnBehalfOf: officer})
		who.reason = "Hỗ trợ cán bộ xã lưu nháp đúng chức năng TT"
		record := seedStandaloneRecord(t, f, department, "weekly", admin.actor, officer, form)
		who.must(submit(who, record.task.String(), "draft", map[string]string{"F": "1"}), http.StatusCreated, "valid TT profile still writes")
		if f.count(`SELECT count(*) FROM pf_extension.submission_version WHERE tenant_id=$1 AND task_id=$2 AND department_code=$3`, f.tenant, record.task, department) != 1 {
			t.Fatal("valid TT draft did not append a version")
		}
	}
}

type standaloneRecordState struct {
	status        string
	reopenedUntil *time.Time
}

func TestStandaloneWorkerDepartmentScope(t *testing.T) {
	for _, mode := range []string{"remind", "close", "reclose"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, fixtureOpts{})
			templates := f.publishForms("T01")
			form, err := f.rep.Forms().ApplicableVersion(context.Background(), f.tenant, uuid.MustParse(templates["T01"]), t0)
			if err != nil {
				t.Fatal(err)
			}
			actor := ids.New()
			state := standaloneRecordState{status: "open"}
			now := t0.Add(30 * time.Minute)
			if mode == "close" {
				now = t0.Add(3 * time.Hour)
			}
			if mode == "reclose" {
				until := t0.Add(time.Minute)
				state = standaloneRecordState{status: "closed", reopenedUntil: &until}
			}
			for _, d := range []partyport.Department{
				{Code: "retired_tt", MenuProfile: "trong_trot", Status: partyport.DeptActive, Retired: true},
				{Code: "inactive_tt", MenuProfile: "trong_trot", Status: partyport.DeptSuspended},
			} {
				f.departments.jobDepartments = append(f.departments.jobDepartments, partyport.ProvinceDepartment{ProvinceCode: "89", Department: d})
			}
			records := []standaloneRecord{}
			for _, department := range []string{"chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop", "orphan_tt", "retired_tt", "inactive_tt"} {
				records = append(records, seedStandaloneRecord(t, f, department, "weekly", actor, actor, form, state))
			}
			records = append(records, seedStandaloneRecord(t, f, dept, "adhoc", actor, actor, form, state))
			snapshots := []string{}
			for _, record := range records {
				snapshots = append(snapshots, standaloneSnapshot(t, f, record.batch, record.period, record.task))
			}
			outboxBefore := f.count(`SELECT count(*) FROM shared.outbox WHERE tenant_id=$1`, f.tenant)
			result, err := f.ext.ReportCycleJobRun(context.Background(), f.tenant, now)
			if err != nil || result.Closed != 0 || result.Reclosed != 0 || result.Reminders != 0 {
				t.Fatalf("removed department job: %+v %v", result, err)
			}
			for i, record := range records {
				if standaloneSnapshot(t, f, record.batch, record.period, record.task) != snapshots[i] {
					t.Fatal("worker changed removed department rows/ledgers")
				}
			}
			if f.count(`SELECT count(*) FROM shared.outbox WHERE tenant_id=$1`, f.tenant) != outboxBefore {
				t.Fatal("worker emitted an event for a removed function")
			}
			// The very same run still performs the selected operation for TT and a
			// custom code using TT's current menu profile; it does not disable the job.
			f.departments.jobDepartments = append(f.departments.jobDepartments, partyport.ProvinceDepartment{ProvinceCode: "89", Department: partyport.Department{Code: "pb_trong_trot", MenuProfile: "trong_trot", Status: partyport.DeptActive}})
			for _, department := range []string{dept, "pb_trong_trot"} {
				seedStandaloneRecord(t, f, department, "weekly", actor, actor, form, state)
			}
			result, err = f.ext.ReportCycleJobRun(context.Background(), f.tenant, now)
			if err != nil {
				t.Fatal(err)
			}
			count := result.Reminders
			if mode == "close" {
				count = result.Closed
			}
			if mode == "reclose" {
				count = result.Reclosed
			}
			if count != 2 {
				t.Fatalf("valid TT/customTT %s job: %+v", mode, result)
			}
		})
	}
}

// HTTP sync has its own transport; it must not revive a removed TT write by
// calling the same use case without the reportcycle handler's menu middleware.
func TestStandaloneSyncWriteScope(t *testing.T) {
	s := setupP7Sync(t)
	f := s.f
	form, err := f.rep.Forms().ApplicableVersion(context.Background(), f.tenant, uuid.MustParse(s.forms["T01"]), t0)
	if err != nil {
		t.Fatal(err)
	}
	f.departments.profiles["pb_trong_trot"] = "trong_trot"
	f.departments.jobDepartments = append(f.departments.jobDepartments, partyport.ProvinceDepartment{ProvinceCode: "89", Department: partyport.Department{Code: "pb_trong_trot", MenuProfile: "trong_trot", Status: partyport.DeptActive}})
	for _, department := range []string{"chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop", dept, "pb_trong_trot"} {
		t.Run(department, func(t *testing.T) {
			who := f.kn(f.tenant, authz.KNXa, department, f.p89, []string{f.communes89[0]})
			record := seedStandaloneRecord(t, f, department, "weekly", s.tinh.actor, who.actor, form)
			device := s.register(t, who)
			before := standaloneSnapshot(t, f, record.batch, record.period, record.task)
			outboxBefore := f.count(`SELECT count(*) FROM shared.outbox WHERE tenant_id=$1`, f.tenant)
			results := device.push(t, device.record(ids.New().String(), record.task.String(), "draft", 0, map[string]string{"F": "1"}))
			if len(results) != 1 {
				t.Fatalf("sync results: %+v", results)
			}
			if department == dept || department == "pb_trong_trot" {
				if str(results[0], "status") != "accepted" {
					t.Fatalf("valid TT/customTT sync write: %+v", results)
				}
				if f.count(`SELECT count(*) FROM pf_extension.submission_version WHERE tenant_id=$1 AND task_id=$2 AND department_code=$3`, f.tenant, record.task, department) != 1 {
					t.Fatal("valid sync draft lost its department scope")
				}
			} else {
				if str(results[0], "status") != "rejected" || str(results[0], "code") != "forbidden" {
					t.Fatalf("removed department sync write: %+v", results)
				}
				if standaloneSnapshot(t, f, record.batch, record.period, record.task) != before || f.count(`SELECT count(*) FROM shared.outbox WHERE tenant_id=$1`, f.tenant) != outboxBefore {
					t.Fatal("rejected sync changed extension rows/ledgers/outbox")
				}
			}
		})
	}
}
