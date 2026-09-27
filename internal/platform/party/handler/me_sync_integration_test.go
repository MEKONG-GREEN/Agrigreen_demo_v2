package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func scopeOf(t *testing.T, r resp) (all bool, level string, units []string, names map[string]any) {
	t.Helper()
	sc, ok := r.body["scope"].(map[string]any)
	if !ok {
		t.Fatalf("thiếu scope: %v", r.body)
	}
	all, _ = sc["all"].(bool)
	level, _ = sc["level"].(string)
	raw, ok := sc["units"].([]any)
	if !ok {
		t.Fatalf("scope.units phải là mảng (kể cả rỗng): %v", sc)
	}
	for _, u := range raw {
		units = append(units, u.(string))
	}
	names, ok = sc["unit_names"].(map[string]any)
	if !ok {
		t.Fatalf("scope.unit_names phải là đối tượng: %v", sc)
	}
	return all, level, units, names
}

// ADR-0009: GET /v1/me trả người gọi, tổ chức và phạm vi đã giải theo cây TW → Tỉnh → Xã.
func TestMeScopeByOrgLevel(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())

	// TTKN tỉnh: tỉnh + mọi xã của tỉnh, có tên đơn vị từ masterdata.
	prov := f.as(f.tenant, tr.p89, port.RoleKNProvinceAdmin)
	r := prov.appDo(http.MethodGet, "/v1/me", nil)
	if r.status != http.StatusOK || r.hdr.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("GET /v1/me: %d %v %v", r.status, r.hdr, r.body)
	}
	if str(r.body, "actor_ref") != prov.actor.String() || str(r.body, "tenant_id") != f.tenant.String() || r.body["mfa"] != true {
		t.Fatalf("danh tính người gọi: %v", r.body)
	}
	if roles, _ := r.body["roles"].([]any); len(roles) != 1 || roles[0] != port.RoleKNProvinceAdmin {
		t.Fatalf("roles: %v", r.body["roles"])
	}
	if str(r.body, "org", "ref") != tr.p89.String() || str(r.body, "org", "kind") != "ttkn_province" ||
		str(r.body, "org", "name") != "TTKN An Giang" || str(r.body, "org", "admin_unit_code") != "89" {
		t.Fatalf("org: %v", r.body["org"])
	}
	all, level, units, names := scopeOf(t, r)
	slices.Sort(units)
	if all || level != "province" || !slices.Equal(units, []string{"30001", "30002", "89"}) {
		t.Fatalf("phạm vi tỉnh: all=%v level=%s units=%v", all, level, units)
	}
	if names["89"] != "An Giang" || names["30001"] != "Xã A" || names["30002"] != "Xã B" || len(names) != 3 {
		t.Fatalf("unit_names: %v", names)
	}

	// Tổ KNCĐ xã → commune; quản lý HTX → cooperative.
	team := f.as(f.tenant, tr.t30001, port.RoleKNOfficer)
	if _, level, units, names := scopeOf(t, team.appDo(http.MethodGet, "/v1/me", nil)); level != "commune" || !slices.Equal(units, []string{"30001"}) || names["30001"] != "Xã A" {
		t.Fatalf("phạm vi xã: %s %v %v", level, units, names)
	}
	coop := f.as(f.tenant, tr.c2, port.RoleCoopManager)
	cr := coop.appDo(http.MethodGet, "/v1/me", nil)
	if _, level, units, _ := scopeOf(t, cr); level != "cooperative" || !slices.Equal(units, []string{"31001"}) || str(cr.body, "org", "kind") != "cooperative" {
		t.Fatalf("phạm vi HTX: %s %v %v", level, units, cr.body)
	}

	// TTKN QG → toàn quốc, không liệt kê đơn vị.
	national := f.as(f.tenant, tr.national, port.RoleKNNationalAdmin)
	nr := national.appDo(http.MethodGet, "/v1/me", nil)
	if all, level, units, _ := scopeOf(t, nr); !all || level != "national" || len(units) != 0 || nr.body["org"].(map[string]any)["admin_unit_code"] != nil {
		t.Fatalf("toàn quốc: %v", nr.body)
	}
	// Quản trị nền tảng không có org_ref → toàn quốc, org = null.
	rr := f.root().appDo(http.MethodGet, "/v1/me", nil)
	if all, level, _, _ := scopeOf(t, rr); !all || level != "national" || rr.body["org"] != nil {
		t.Fatalf("platform_admin: %v", rr.body)
	}

	// Không có org_ref → none; org_ref không tồn tại → org = null, none.
	for _, org := range []uuid.UUID{uuid.Nil, ids.New()} {
		c := f.as(f.tenant, org, port.RoleKNOfficer)
		r := c.appDo(http.MethodGet, "/v1/me", nil)
		if all, level, units, _ := scopeOf(t, r); r.status != http.StatusOK || all || level != "none" || len(units) != 0 || r.body["org"] != nil {
			t.Fatalf("không có đơn vị công tác phải none: %v", r.body)
		}
	}
}

// R-16/R-33: org_ref trỏ sang tổ chức của tenant khác không được lộ tên hay phạm vi.
func TestMeTenantIsolation(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	other := f.as(ids.New(), tr.p89, port.RoleKNProvinceAdmin)
	r := other.appDo(http.MethodGet, "/v1/me", nil)
	if all, level, units, _ := scopeOf(t, r); r.status != http.StatusOK || all || level != "none" || len(units) != 0 || r.body["org"] != nil {
		t.Fatalf("tenant khác không được thấy tổ chức: %v", r.body)
	}
}

// R-14, R-04: /v1/me không có trường dữ liệu cá nhân.
func TestMeNoPII(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	r := f.as(f.tenant, tr.c1, port.RoleCoopManager).appDo(http.MethodGet, "/v1/me", nil)
	raw, _ := json.Marshal(r.body)
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"actor_ref", "mfa", "org", "roles", "scope", "tenant_id"}) {
		t.Fatalf("trường cấp đầu của /v1/me: %v", keys)
	}
	for _, k := range []string{"full_name", "phone", "national_id", "address", "email", "display_name", "name"} {
		if _, ok := top[k]; ok {
			t.Fatalf("/v1/me không được có %q", k)
		}
	}
	if _, ok := r.body["org"].(map[string]any)["representative_ref"]; ok {
		t.Fatalf("org trong /v1/me không kèm người đại diện: %v", r.body["org"])
	}
}

func TestMeUnauthenticated(t *testing.T) {
	f := newFixture(t)
	c := f.as(f.tenant, uuid.Nil)
	c.token = ""
	r := c.appDo(http.MethodGet, "/v1/me", nil)
	if r.status != http.StatusUnauthorized {
		t.Fatalf("không token phải 401: %d", r.status)
	}
}

// ---------------------------------------------------------------------------
// Sink đồng bộ entity consent (docs/dev/sync-contract.md).

func syncCtx(tid uuid.UUID, roles ...string) context.Context {
	p := authz.Principal{ActorRef: ids.New(), TenantID: tid, Roles: roles}
	return authz.WithPrincipal(tenant.WithID(context.Background(), tid), p)
}

func consentRecord(device uuid.UUID, seq int, subject uuid.UUID, status string) syncport.Record {
	payload, _ := json.Marshal(map[string]any{"subject_ref": subject.String(), "purpose": "mrv.evidence", "text_version": "2026-11", "status": status})
	return syncport.Record{
		ID: ids.New(), IdemKey: "dev:" + device.String() + ":" + strconv.Itoa(seq), Entity: "consent", Op: syncport.OpCreate,
		EventTime: time.Now().Add(-time.Hour), DeviceID: device, Payload: payload,
	}
}

// Mục 10.5, mục 15: đồng ý thu ngoại tuyến đi qua pf/sync vào cùng sổ; gửi lại → duplicate.
func TestConsentSyncSink(t *testing.T) {
	f := newFixture(t)
	sink := f.mod.SyncSink()
	if !slices.Equal(sink.Entities(), []string{"consent"}) {
		t.Fatalf("Entities: %v", sink.Entities())
	}
	ctx := syncCtx(f.tenant, port.RoleConsentWriter)
	device, subject := ids.New(), ids.New()
	rec := consentRecord(device, 1, subject, "granted")

	if res := sink.Accept(ctx, authz.Scope{}, rec); res.Status != syncport.StatusAccepted || res.ID != rec.ID {
		t.Fatalf("lần đầu phải accepted: %+v", res)
	}
	// Gửi lại (lô khác, cùng idem_key) → duplicate, không ghi thêm.
	if res := sink.Accept(ctx, authz.Scope{}, rec); res.Status != syncport.StatusDuplicate {
		t.Fatalf("gửi lại phải duplicate: %+v", res)
	}
	ok, err := f.mod.ConsentChecker().HasConsent(ctx, subject, "mrv.evidence")
	if err != nil || !ok {
		t.Fatalf("đồng ý qua đồng bộ phải có hiệu lực: %v %v", ok, err)
	}
	// Bản ghi đọc lại qua API có id máy sinh và device_id.
	reader := f.as(f.tenant, uuid.Nil, port.RoleConsentReader)
	got := reader.appDo(http.MethodGet, "/v1/consents/"+rec.ID.String(), nil)
	if got.status != http.StatusOK || str(got.body, "channel") != "field_app" || str(got.body, "device_id") != device.String() {
		t.Fatalf("đọc lại bản ghi: %d %v", got.status, got.body)
	}
	var n int
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM pf_party.consent WHERE subject_ref = $1", subject).Scan(&n)
	}); err != nil || n != 1 {
		t.Fatalf("sổ phải có đúng 1 bản ghi, có %d (%v)", n, err)
	}

	// Rút lại = bản ghi mới status withdrawn (op create).
	later := consentRecord(device, 2, subject, "withdrawn")
	later.EventTime = time.Now().Add(-time.Minute)
	if res := sink.Accept(ctx, authz.Scope{}, later); res.Status != syncport.StatusAccepted {
		t.Fatalf("rút lại: %+v", res)
	}
	if ok, _ := f.mod.ConsentChecker().HasConsent(ctx, subject, "mrv.evidence"); ok {
		t.Fatal("sau khi rút lại không còn đồng ý")
	}
}

func TestConsentSyncSinkRejects(t *testing.T) {
	f := newFixture(t)
	sink := f.mod.SyncSink()
	ctx := syncCtx(f.tenant, port.RoleConsentWriter)
	device := ids.New()

	correct := consentRecord(device, 1, ids.New(), "granted")
	correct.Op = syncport.OpCorrect
	if res := sink.Accept(ctx, authz.Scope{}, correct); res.Status != syncport.StatusRejected || res.Code != "validation_failed" || res.Errors[0].Field != "op" {
		t.Fatalf("op correct phải rejected: %+v", res)
	}

	bad := consentRecord(device, 2, ids.New(), "granted")
	bad.Payload = json.RawMessage(`{"subject_ref":"x","purpose":"mrv.evidence","text_version":"1","status":"granted"}`)
	if res := sink.Accept(ctx, authz.Scope{}, bad); res.Status != syncport.StatusRejected || res.Errors[0].Field != "payload.subject_ref" {
		t.Fatalf("subject_ref sai phải rejected: %+v", res)
	}

	wrongPurpose := consentRecord(device, 3, ids.New(), "granted")
	wrongPurpose.Payload = json.RawMessage(`{"subject_ref":"` + ids.New().String() + `","purpose":"Sai","text_version":"1","status":"granted"}`)
	if res := sink.Accept(ctx, authz.Scope{}, wrongPurpose); res.Status != syncport.StatusRejected || res.Code != "validation_failed" || res.Errors[0].Field != "purpose" {
		t.Fatalf("purpose sai phải rejected theo trường: %+v", res)
	}

	notJSON := consentRecord(device, 4, ids.New(), "granted")
	notJSON.Payload = json.RawMessage(`[1,2]`)
	if res := sink.Accept(ctx, authz.Scope{}, notJSON); res.Status != syncport.StatusRejected || res.Errors[0].Field != "payload" {
		t.Fatalf("payload không phải đối tượng: %+v", res)
	}

	// Thiếu vai consent_writer → rejected forbidden (kiểm vai như API thường).
	noRole := syncCtx(f.tenant, port.RoleKNOfficer)
	if res := sink.Accept(noRole, authz.Scope{}, consentRecord(device, 5, ids.New(), "granted")); res.Status != syncport.StatusRejected || res.Code != "forbidden" {
		t.Fatalf("thiếu vai phải rejected forbidden: %+v", res)
	}
}
