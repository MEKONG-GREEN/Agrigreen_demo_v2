package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func code(r resp) string { return str(r.body, "code") }

func wantStatus(t *testing.T, r resp, status int, what string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: muốn %d, nhận %d %v", what, status, r.status, r.body)
	}
}

func wantCode(t *testing.T, r resp, status int, c, what string) {
	t.Helper()
	if r.status != status || code(r) != c {
		t.Fatalf("%s: muốn %d %s, nhận %d %v", what, status, c, r.status, r.body)
	}
}

func proposal(grantee uuid.UUID, kinds ...string) map[string]any {
	return map[string]any{"grantee_tenant_id": grantee.String(), "kinds": kinds,
		"purpose":  "MKG lập lệnh gom rơm theo HTX/thửa của chương trình (ADR-0020)",
		"valid_to": time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)}
}

func (f *fixture) countIn(tid uuid.UUID, table string) int {
	f.t.Helper()
	var n int
	if err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n)
	}); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// RB-2 / Q-MKG-12 (ADR-0020, chủ sản phẩm chốt 29–30/09/2026): cấp quyền chia sẻ tenant chương
// trình → tenant MKG riêng. Đề xuất ≠ duyệt (R-56, ở app/ và CHECK DB); chưa duyệt → đọc 403; sau
// duyệt, tài khoản dịch vụ mkg_sync của tenant MKG đọc danh bạ HTX của tenant chương trình (trường
// tối thiểu, không dữ liệu cá nhân), mỗi lời gọi một dòng sổ đọc (bên cấp xem được); loại không
// được cấp → 403; thu hồi → 403 ngay; sổ cấp quyền và sổ đọc chỉ ghi thêm, chuỗi băm liền.
func TestShareGrantLifecycle_SoD_Audit_ADR0020(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	mkg := ids.New()
	admA := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	admB := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	sa := f.as(mkg, uuid.Nil, port.RolePartnerSync)

	// Kiểm dữ liệu đề xuất.
	wantStatus(t, admA.adminDo(http.MethodPost, "/v1/share-grants", proposal(f.tenant, port.KindCooperativeDirectory)), http.StatusUnprocessableEntity, "tự cấp cho chính mình")
	wantStatus(t, admA.adminDo(http.MethodPost, "/v1/share-grants", proposal(mkg, "household_pii")), http.StatusUnprocessableEntity, "loại lạ")
	bad := proposal(mkg, port.KindCooperativeDirectory)
	bad["valid_to"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	wantStatus(t, admA.adminDo(http.MethodPost, "/v1/share-grants", bad), http.StatusUnprocessableEntity, "hết hạn từ đầu")
	wantStatus(t, f.as(f.tenant, uuid.Nil, port.RoleRegistryAdmin).adminDo(http.MethodPost, "/v1/share-grants", proposal(mkg, port.KindCooperativeDirectory)),
		http.StatusForbidden, "không phải platform_admin")

	r := admA.adminDo(http.MethodPost, "/v1/share-grants", proposal(mkg, port.KindHTXCollectionResponses, port.KindCooperativeDirectory))
	wantStatus(t, r, http.StatusCreated, "đề xuất")
	id := str(r.body, "id")
	if str(r.body, "status") != "proposed" || str(r.body, "grantor_tenant_id") != f.tenant.String() || str(r.body, "grantee_tenant_id") != mkg.String() {
		t.Fatalf("đề xuất: %v", r.body)
	}
	// Chưa duyệt → không đọc được.
	wantCode(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusForbidden, "party.share_grant_missing", "chưa duyệt")

	// R-56: người đề xuất không tự duyệt; vai khác không duyệt; quản trị tenant khác không thấy.
	wantCode(t, admA.adminDo(http.MethodPost, "/v1/share-grants/"+id+"/approve", nil), http.StatusForbidden, "self_approval_forbidden", "tự duyệt")
	wantStatus(t, f.as(f.tenant, uuid.Nil, port.RoleRegistryAdmin).adminDo(http.MethodPost, "/v1/share-grants/"+id+"/approve", nil), http.StatusForbidden, "vai khác")
	wantStatus(t, f.as(mkg, uuid.Nil, authz.RolePlatformAdmin).adminDo(http.MethodPost, "/v1/share-grants/"+id+"/approve", nil), http.StatusNotFound, "quản trị tenant MKG")
	// R-56 ở DB: dòng 'approved' do chính người đề xuất ghi bị CHECK chặn.
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), `INSERT INTO pf_party.share_grant_event (id, tenant_id, stream_key, grant_id, action, grantee_tenant_id,
		    kinds, purpose, valid_from, valid_to, proposed_by, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'grant:x', $3, 'approved', $4, ARRAY['cooperative_directory'], 'Thử tự duyệt ở DB (R-56)', now(), now() + interval '1 day',
		    $5, now(), $5, $6, 'test')`, ids.New(), f.tenant, ids.New(), mkg, admA.actor, "test:"+ids.New().String())
		return e
	})
	if err == nil {
		t.Fatal("CHECK DB phải chặn tự duyệt (R-56)")
	}

	r = admB.adminDo(http.MethodPost, "/v1/share-grants/"+id+"/approve", nil)
	wantStatus(t, r, http.StatusCreated, "duyệt")
	if str(r.body, "status") != "active" || str(r.body, "approved_by") != admB.actor.String() {
		t.Fatalf("duyệt: %v", r.body)
	}
	// Bảng chiếu ở tenant bên nhận (không vai BYPASSRLS).
	if n := f.countIn(mkg, "pf_party.share_grant_inbound WHERE status = 'active'"); n != 1 {
		t.Fatalf("bảng chiếu bên nhận: %d", n)
	}
	if n := f.countIn(f.tenant, "pf_party.share_grant_inbound"); n != 0 {
		t.Fatalf("bảng chiếu bên nhận không được nằm ở bên cấp: %d", n)
	}

	// Bên nhận thấy cấp quyền của mình.
	r = sa.appDo(http.MethodGet, "/v1/partner/grants", nil)
	if it := items(r); len(it) != 1 || it[0]["grantor_tenant_id"] != f.tenant.String() {
		t.Fatalf("cấp quyền của bên nhận: %v", r.body)
	}
	// Danh bạ HTX: đúng HTX của bên cấp, không dữ liệu cá nhân.
	r = sa.appDo(http.MethodGet, "/v1/partner/cooperatives", nil)
	wantStatus(t, r, http.StatusOK, "danh bạ HTX")
	got := refs(r, "ref")
	if len(got) != 2 || !slices.Contains(got, tr.c1.String()) || !slices.Contains(got, tr.c2.String()) {
		t.Fatalf("danh bạ HTX của bên cấp: %v", r.body)
	}
	raw, _ := json.Marshal(r.body)
	for _, k := range []string{"representative_ref", "tax_code", "phone", "households", "head_office"} {
		if strings.Contains(string(raw), `"`+k+`"`) {
			t.Fatalf("danh bạ không được mang %s: %s", k, raw)
		}
	}
	r = sa.appDo(http.MethodGet, "/v1/partner/cooperatives/"+tr.c1.String()+"?grantor="+f.tenant.String(), nil)
	if r.status != http.StatusOK || str(r.body, "commune_code") != "30001" || str(r.body, "lat") == "" || r.body["active"] != true {
		t.Fatalf("HTX c1: %d %v", r.status, r.body)
	}
	wantCode(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives/"+tr.c1.String()+"?grantor="+ids.New().String(), nil),
		http.StatusForbidden, "party.share_grant_missing", "bên cấp khác")
	wantStatus(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives/"+ids.New().String(), nil), http.StatusNotFound, "HTX không có")
	// Tenant khác, vai khác → 403.
	wantStatus(t, f.as(ids.New(), uuid.Nil, port.RolePartnerSync).appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusForbidden, "tenant khác")
	wantStatus(t, f.as(mkg, uuid.Nil, port.RoleKNNationalAdmin).appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusForbidden, "vai khác")

	// Phạm vi: loại không được cấp → 403; cổng van chỉ trả bên cấp của loại đã cấp.
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: sa.actor, TenantID: mkg, Roles: []string{port.RolePartnerSync}})
	pa := f.mod.PartnerAccess()
	if _, err := pa.Authorize(ctx, port.KindPlotDirectory, uuid.Nil); !errors.Is(err, port.ErrShareGrantMissing) {
		t.Fatalf("loại không được cấp → share_grant_missing: %v", err)
	}
	vg := f.mod.ValveGrants()
	if gs, err := vg.ValveGrantors(context.Background(), mkg, port.KindValveCollection, time.Now()); err != nil || len(gs) != 0 {
		t.Fatalf("valve_collection chưa cấp: %v %v", gs, err)
	}
	if gs, err := vg.ValveGrantors(context.Background(), mkg, port.KindHTXCollectionResponses, time.Now()); err != nil || len(gs) != 1 || gs[0].Tenant() != f.tenant || gs[0].From() != mkg {
		t.Fatalf("bên cấp của loại đã cấp: %v %v", gs, err)
	}
	// Hết hạn (đồng hồ sau valid_to) → 403.
	later := party.New(f.pool, clock.NewFixed(time.Now().Add(40*24*time.Hour)))
	if _, err := later.PartnerAccess().Authorize(ctx, port.KindCooperativeDirectory, uuid.Nil); !errors.Is(err, port.ErrShareGrantMissing) {
		t.Fatalf("hết hạn → share_grant_missing: %v", err)
	}

	// Sổ đọc: 3 lời gọi thành công (danh sách, c1, HTX không có) — bên cấp xem được, bên nhận thì không.
	r = admA.adminDo(http.MethodGet, "/v1/share-grants/"+id+"/reads", nil)
	reads := items(r)
	if len(reads) != 3 || reads[0]["actor_ref"] != sa.actor.String() || reads[0]["kind"] != port.KindCooperativeDirectory ||
		reads[0]["endpoint"] != "GET /v1/partner/cooperatives" || reads[0]["item_count"] != float64(2) {
		t.Fatalf("sổ đọc: %v", r.body)
	}
	wantStatus(t, f.as(mkg, uuid.Nil, authz.RolePlatformAdmin).adminDo(http.MethodGet, "/v1/share-grants/"+id+"/reads", nil), http.StatusNotFound, "bên nhận không xem sổ bên cấp")
	gid := uuid.MustParse(id)
	for table, rows := range map[string]int64{"pf_party.share_grant_event": 2, "pf_party.partner_read_log": 3} {
		if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
			res, err := ledger.Verify(context.Background(), tx, table, f.tenant, "grant:"+gid.String())
			if err != nil {
				return err
			}
			if !res.OK || res.RowsChecked != rows {
				t.Fatalf("chuỗi băm %s: %+v", table, res)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	// RB-1: sổ chỉ ghi thêm.
	for _, stmt := range []string{"UPDATE pf_party.share_grant_event SET purpose = purpose", "DELETE FROM pf_party.partner_read_log"} {
		if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
			_, e := tx.Exec(context.Background(), stmt)
			return e
		}); err == nil {
			t.Fatalf("RB-1: %s phải bị chặn", stmt)
		}
	}

	// Thu hồi: cần lý do; sau thu hồi đọc 403 ngay, cổng van rỗng, không duyệt lại được.
	wantStatus(t, admA.adminDo(http.MethodPost, "/v1/share-grants/"+id+"/revoke", map[string]any{"reason": "ngắn"}), http.StatusUnprocessableEntity, "thiếu lý do")
	r = admA.adminDo(http.MethodPost, "/v1/share-grants/"+id+"/revoke", map[string]any{"reason": "Kết thúc hợp đồng thu gom vụ Đông Xuân"})
	wantStatus(t, r, http.StatusCreated, "thu hồi")
	if str(r.body, "status") != "revoked" {
		t.Fatalf("thu hồi: %v", r.body)
	}
	wantCode(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusForbidden, "party.share_grant_missing", "sau thu hồi")
	if gs, _ := vg.ValveGrantors(context.Background(), mkg, port.KindHTXCollectionResponses, time.Now()); len(gs) != 0 {
		t.Fatalf("sau thu hồi cổng van phải rỗng: %v", gs)
	}
	wantCode(t, admB.adminDo(http.MethodPost, "/v1/share-grants/"+id+"/approve", nil), http.StatusConflict, "party.share_grant_state", "duyệt lại bản đã thu hồi")
	r = admB.adminDo(http.MethodGet, "/v1/share-grants?status=revoked", nil)
	if it := items(r); len(it) != 1 || it[0]["revoke_reason"] == nil {
		t.Fatalf("danh sách đã thu hồi: %v", r.body)
	}
}

// ADR-0020: hai bên cấp cùng cấp một loại cho một bên nhận → thiếu grantor là 422 (không đoán);
// có grantor thì đọc đúng dữ liệu của bên cấp đó (cách ly giữa hai tenant chương trình).
func TestShareGrantorAmbiguous_ADR0020(t *testing.T) {
	f := newFixture(t)
	mkg := ids.New()
	other := ids.New()
	for _, tid := range []uuid.UUID{f.tenant, other} {
		a, b := f.as(tid, uuid.Nil, authz.RolePlatformAdmin, port.RoleRegistryAdmin), f.as(tid, uuid.Nil, authz.RolePlatformAdmin)
		r := a.adminDo(http.MethodPost, "/v1/share-grants", proposal(mkg, port.KindCooperativeDirectory))
		wantStatus(t, r, http.StatusCreated, "đề xuất")
		wantStatus(t, b.adminDo(http.MethodPost, "/v1/share-grants/"+str(r.body, "id")+"/approve", nil), http.StatusCreated, "duyệt")
	}
	c := f.as(other, uuid.Nil, port.RoleRegistryAdmin, authz.RolePlatformAdmin).createOrg(map[string]any{"kind": "cooperative", "name": "HTX tenant khác",
		"admin_unit_code": "30001"})
	sa := f.as(mkg, uuid.Nil, port.RolePartnerSync)
	wantCode(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusUnprocessableEntity, "party.share_grantor_ambiguous", "hai bên cấp")
	r := sa.appDo(http.MethodGet, "/v1/partner/cooperatives?grantor="+other.String(), nil)
	if got := refs(r, "ref"); len(got) != 1 || got[0] != c.String() {
		t.Fatalf("chỉ HTX của bên cấp được chọn: %v", r.body)
	}
	r = sa.appDo(http.MethodGet, "/v1/partner/cooperatives?grantor="+f.tenant.String(), nil)
	if got := refs(r, "ref"); len(got) != 0 {
		t.Fatalf("bên cấp thứ nhất chưa có HTX: %v", r.body)
	}
}

// G4-0 (pf 000046, proto FM-05): loại waterway_routes đề xuất, duyệt được qua sổ thật (CHECK DB mở rộng)
// và bên nhận thấy loại này trong /v1/partner/grants; loại lạ vẫn bị CHECK DB chặn.
func TestShareGrantWaterwayRoutesKind_G4(t *testing.T) {
	f := newFixture(t)
	mkg := ids.New()
	a, b := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin), f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	r := a.adminDo(http.MethodPost, "/v1/share-grants", proposal(mkg, port.KindWaterwayRoutes))
	wantStatus(t, r, http.StatusCreated, "đề xuất waterway_routes")
	wantStatus(t, b.adminDo(http.MethodPost, "/v1/share-grants/"+str(r.body, "id")+"/approve", nil), http.StatusCreated, "duyệt")
	g := f.as(mkg, uuid.Nil, port.RolePartnerSync).appDo(http.MethodGet, "/v1/partner/grants", nil)
	it := items(g)
	if len(it) != 1 {
		t.Fatalf("cấp quyền bên nhận: %v", g.body)
	}
	if k, _ := it[0]["kinds"].([]any); len(k) != 1 || k[0] != port.KindWaterwayRoutes {
		t.Fatalf("loại: %v", it[0])
	}
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), `INSERT INTO pf_party.share_grant_event (id, tenant_id, stream_key, grant_id, action, grantee_tenant_id,
		    kinds, purpose, valid_from, valid_to, proposed_by, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'grant:y', $3, 'proposed', $4, ARRAY['road_routes'], 'Loại lạ phải bị CHECK chặn', now(), now() + interval '1 day',
		    $5, now(), $5, $6, 'test')`, ids.New(), f.tenant, ids.New(), mkg, a.actor, "test:"+ids.New().String())
		return e
	})
	if err == nil {
		t.Fatal("CHECK DB phải chặn loại dữ liệu lạ")
	}
}

// G5-0 (pf 000080, ADR-0022 mục 3): ba loại sản phẩm dữ liệu dp_* đề xuất + duyệt được qua sổ thật
// (CHECK DB mở rộng) cho tenant BÊN THỨ BA; bên nhận thấy đủ ba loại.
func TestShareGrantDataProductKinds_G5(t *testing.T) {
	f := newFixture(t)
	third := ids.New()
	a, b := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin), f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	r := a.adminDo(http.MethodPost, "/v1/share-grants", proposal(third, port.KindDPCollectionVolume, port.KindDPHubCapacity, port.KindDPMRVPackageStatus))
	wantStatus(t, r, http.StatusCreated, "đề xuất dp_*")
	wantStatus(t, b.adminDo(http.MethodPost, "/v1/share-grants/"+str(r.body, "id")+"/approve", nil), http.StatusCreated, "duyệt")
	g := f.as(third, uuid.Nil, port.RolePartnerSync).appDo(http.MethodGet, "/v1/partner/grants", nil)
	it := items(g)
	if len(it) != 1 {
		t.Fatalf("cấp quyền bên nhận: %v", g.body)
	}
	if k, _ := it[0]["kinds"].([]any); len(k) != 3 {
		t.Fatalf("loại: %v", it[0])
	}
	if port.DataProductKind("hub_capacity") != port.KindDPHubCapacity {
		t.Fatal("DataProductKind phải là dp_<sản phẩm>")
	}
}
