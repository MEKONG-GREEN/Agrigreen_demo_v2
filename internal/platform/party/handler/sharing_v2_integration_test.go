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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// intraProposal là đề xuất cấp phép intra_platform (ADR-0030 mục 3) trên bảng household của pf/cooperative.
func intraProposal(grantee, node uuid.UUID) map[string]any {
	return map[string]any{"grantee_tenant_id": grantee.String(), "grant_class": "intra_platform",
		"module_table": "pf/cooperative:household", "scope_node_ref": node.String(), "purpose_code": "khuyen-nong",
		"basis_ref": "TTCS-2026-001", "purpose": "Cán bộ khuyến nông đọc hộ HTX trong tỉnh (ADR-0030)",
		"valid_to": time.Now().Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339)}
}

func with(m map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == nil {
			delete(out, kv[i].(string))
			continue
		}
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func (f *fixture) outboxPayloads(tid uuid.UUID, typ, grant string) []map[string]any {
	f.t.Helper()
	pub := f.env.Pool(f.t, migrations.PF, dbadmin.OutboxPublisher)
	rows, err := pub.Query(context.Background(), `SELECT payload FROM shared.outbox
	    WHERE tenant_id = $1 AND event_type = $2 AND payload->>'grant_id' = $3 ORDER BY occurred_at`, tid, typ, grant)
	if err != nil {
		f.t.Fatal(err)
	}
	raws, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		f.t.Fatal(err)
	}
	out := make([]map[string]any, 0, len(raws))
	for _, r := range raws {
		var m map[string]any
		if err := json.Unmarshal(r, &m); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func (f *fixture) verifyStream(tid uuid.UUID, table, stream string, rows int64) {
	f.t.Helper()
	if err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		res, err := ledger.Verify(context.Background(), tx, table, tid, stream)
		if err != nil {
			return err
		}
		if !res.OK || res.RowsChecked != rows {
			f.t.Fatalf("chuỗi băm %s %s: %+v", table, stream, res)
		}
		return nil
	}); err != nil {
		f.t.Fatal(err)
	}
}

// W4.1 (ADR-0030 mục 3, R-SEC-04): cấp phép v2 intra_platform — chỉ quản trị đơn vị chủ tạo; module_table phải có
// trong shares của module chủ (danh mục modreg); Nút phạm vi là Nút đang hoạt động của chính đơn vị chủ; đủ mục
// đích + căn cứ; không kinds. Duyệt bốn mắt như cũ; không ghi bảng chiếu bên nhận (đường partner); sự kiện approved
// mang điều khoản v2. Đề xuất partner không grant_class vẫn chạy như trước.
func TestShareGrantV2IntraPlatform_W41(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	kn := ids.New() // đơn vị xem (tenant khác)
	admA := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	admB := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	base := intraProposal(kn, tr.p89)
	post := func(c caller, body map[string]any) resp { return c.adminDo(http.MethodPost, "/v1/share-grants", body) }

	// Chỉ quản trị đơn vị chủ (platform_admin của tenant bên cấp).
	wantStatus(t, post(f.as(f.tenant, uuid.Nil, port.RoleRegistryAdmin), base), http.StatusForbidden, "không phải quản trị đơn vị chủ")
	// module_table phải nằm trong shares của module chủ.
	wantCode(t, post(admA, with(base, "module_table", "pf/cooperative:cooperative")), http.StatusUnprocessableEntity,
		"party.share_table_not_shared", "bảng không khai shares")
	wantCode(t, post(admA, with(base, "module_table", "pf/party:org")), http.StatusUnprocessableEntity,
		"party.share_table_not_shared", "module không khai shares")
	wantCode(t, post(admA, with(base, "module_table", "pf/khong_co:household")), http.StatusUnprocessableEntity,
		"party.share_table_not_shared", "module không tồn tại")
	wantStatus(t, post(admA, with(base, "module_table", "pf_coop.household")), http.StatusUnprocessableEntity, "sai dạng module_table")
	// intra_platform: không kinds; đủ Nút, mục đích, căn cứ.
	wantStatus(t, post(admA, with(base, "kinds", []string{port.KindCooperativeDirectory})), http.StatusUnprocessableEntity, "intra có kinds")
	wantStatus(t, post(admA, with(base, "basis_ref", nil)), http.StatusUnprocessableEntity, "thiếu căn cứ")
	wantStatus(t, post(admA, with(base, "purpose_code", nil)), http.StatusUnprocessableEntity, "thiếu mã mục đích")
	wantStatus(t, post(admA, with(base, "purpose_code", "Khuyen Nong")), http.StatusUnprocessableEntity, "mã mục đích sai dạng")
	wantStatus(t, post(admA, with(base, "scope_node_ref", nil)), http.StatusUnprocessableEntity, "thiếu Nút")
	wantStatus(t, post(admA, with(base, "scope_node_ref", ids.New().String())), http.StatusUnprocessableEntity, "Nút không có")
	other := ids.New()
	otherNode := f.as(other, uuid.Nil, port.RoleRegistryAdmin, authz.RolePlatformAdmin).createOrg(map[string]any{"kind": "cooperative",
		"name": "HTX tenant khác", "admin_unit_code": "30001"})
	wantStatus(t, post(admA, with(base, "scope_node_ref", otherNode.String())), http.StatusUnprocessableEntity, "Nút của đơn vị khác")
	wantStatus(t, post(admA, with(base, "grant_class", "noi_bo")), http.StatusUnprocessableEntity, "lớp lạ")
	// partner không được mang bảng/Nút.
	wantStatus(t, post(admA, with(proposal(kn, port.KindCooperativeDirectory), "module_table", "pf/cooperative:household")),
		http.StatusUnprocessableEntity, "partner có module_table")

	r := post(admA, base)
	wantStatus(t, r, http.StatusCreated, "đề xuất intra_platform")
	id := str(r.body, "id")
	if str(r.body, "grant_class") != "intra_platform" || str(r.body, "module_table") != "pf/cooperative:household" ||
		str(r.body, "scope_node_ref") != tr.p89.String() || str(r.body, "purpose_code") != "khuyen-nong" || str(r.body, "basis_ref") != "TTCS-2026-001" ||
		r.body["legacy_terms"] != false {
		t.Fatalf("đề xuất intra: %v", r.body)
	}
	if k, _ := r.body["kinds"].([]any); len(k) != 0 {
		t.Fatalf("intra không có kinds: %v", r.body)
	}
	wantCode(t, admA.adminDo(http.MethodPost, "/v1/share-grants/"+id+"/approve", nil), http.StatusForbidden, "self_approval_forbidden", "tự duyệt")
	r = admB.adminDo(http.MethodPost, "/v1/share-grants/"+id+"/approve", nil)
	wantStatus(t, r, http.StatusCreated, "duyệt")
	if str(r.body, "status") != "active" || str(r.body, "review_due_at") == "" {
		t.Fatalf("duyệt: %v", r.body)
	}
	// Không đi đường partner: không bảng chiếu bên nhận, mkg_sync của đơn vị xem không thấy.
	if n := f.countIn(kn, "pf_party.share_grant_inbound"); n != 0 {
		t.Fatalf("intra_platform không ghi bảng chiếu bên nhận: %d", n)
	}
	if it := items(f.as(kn, uuid.Nil, port.RolePartnerSync).appDo(http.MethodGet, "/v1/partner/grants", nil)); len(it) != 0 {
		t.Fatalf("bề mặt đối tác không thấy intra_platform: %v", it)
	}
	// Sổ phụ điều khoản: một dòng, chuỗi băm liền, chỉ ghi thêm; sổ cấp phép không đổi cấu trúc.
	f.verifyStream(f.tenant, "pf_party.share_grant_terms", "terms:"+id, 1)
	f.verifyStream(f.tenant, "pf_party.share_grant_event", "grant:"+id, 2)
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "UPDATE pf_party.share_grant_terms SET basis_ref = 'khac' WHERE grant_id = $1", uuid.MustParse(id))
		return e
	}); err == nil {
		t.Fatal("UPDATE sổ share_grant_terms phải bị từ chối")
	}
	// Sự kiện approved mang điều khoản v2 (trường tùy chọn).
	ev := f.outboxPayloads(f.tenant, port.EventShareGrantApproved, id)
	if len(ev) != 1 || ev[0]["grant_class"] != "intra_platform" || ev[0]["module_table"] != "pf/cooperative:household" ||
		ev[0]["scope_node_ref"] != tr.p89.String() || ev[0]["purpose_code"] != "khuyen-nong" || ev[0]["basis_ref"] != "TTCS-2026-001" {
		t.Fatalf("sự kiện approved: %v", ev)
	}

	// Partner mặc định (không grant_class) như cũ; điều khoản v2 = partner, mục đích/căn cứ tùy chọn.
	r = post(admA, with(proposal(kn, port.KindCooperativeDirectory), "basis_ref", "HD-MKG-01"))
	wantStatus(t, r, http.StatusCreated, "đề xuất partner")
	if str(r.body, "grant_class") != "partner" || r.body["legacy_terms"] != false || str(r.body, "basis_ref") != "HD-MKG-01" || r.body["module_table"] != nil {
		t.Fatalf("đề xuất partner: %v", r.body)
	}
	pid := str(r.body, "id")
	wantStatus(t, admB.adminDo(http.MethodPost, "/v1/share-grants/"+pid+"/approve", nil), http.StatusCreated, "duyệt partner")
	if n := f.countIn(kn, "pf_party.share_grant_inbound WHERE status = 'active'"); n != 1 {
		t.Fatalf("partner ghi bảng chiếu bên nhận: %d", n)
	}
	if ev := f.outboxPayloads(f.tenant, port.EventShareGrantApproved, pid); len(ev) != 1 || ev[0]["grant_class"] != "partner" || ev[0]["module_table"] != nil {
		t.Fatalf("sự kiện approved partner: %v", ev)
	}

	// Cấp phép trước v2 (dòng chiếu không có điều khoản) = partner kế thừa.
	legacy := ids.New()
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), `INSERT INTO pf_party.share_grant (tenant_id, grant_id, grantee_tenant_id, kinds, purpose, valid_from, valid_to,
		    status, proposed_by, proposed_at, last_event_id, last_seq) VALUES ($1, $2, $3, ARRAY['plot_directory'], 'Cấp phép trước v2', now(),
		    now() + interval '30 days', 'proposed', $4, now(), $5, 1)`, f.tenant, legacy, kn, admA.actor, ids.New())
		return e
	}); err != nil {
		t.Fatal(err)
	}
	r = admA.adminDo(http.MethodGet, "/v1/share-grants/"+legacy.String(), nil)
	if str(r.body, "grant_class") != "partner" || r.body["legacy_terms"] != true || r.body["module_table"] != nil {
		t.Fatalf("cấp phép trước v2: %v", r.body)
	}
	// CHECK bảng chiếu: intra_platform không được có kinds.
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), `INSERT INTO pf_party.share_grant (tenant_id, grant_id, grantee_tenant_id, kinds, purpose, valid_from, valid_to,
		    status, proposed_by, proposed_at, last_event_id, last_seq, grant_class, module_table, scope_node_ref, purpose_code, basis_ref)
		  VALUES ($1, $2, $3, ARRAY['plot_directory'], 'x', now(), now() + interval '1 day', 'proposed', $4, now(), $5, 1,
		    'intra_platform', 'pf/cooperative:plot', $6, 'khuyen-nong', 'TT-1')`, f.tenant, ids.New(), kn, admA.actor, ids.New(), tr.p89)
		return e
	}); err == nil {
		t.Fatal("CHECK share_grant_class_kinds phải chặn intra_platform có kinds")
	}
}

// W4.1 (R-SEC-04, kiến trúc v4.1 §17.1): rà soát định kỳ — danh sách đến hạn, quyết định giữ/thu hồi ghi sổ chỉ ghi
// thêm (gọi lặp idempotent), giữ đặt lại hạn; thu hồi (khi rà soát hoặc thường) tăng epoch ngữ cảnh của đơn vị xem
// cùng giao dịch và phát pf.party.context_epoch.bumped ở tenant đơn vị xem.
func TestShareGrantReviewAndEpoch_W41(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	kn := ids.New()
	admA := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	admB := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	newActive := func(body map[string]any) string {
		t.Helper()
		r := admA.adminDo(http.MethodPost, "/v1/share-grants", body)
		wantStatus(t, r, http.StatusCreated, "đề xuất")
		id := str(r.body, "id")
		wantStatus(t, admB.adminDo(http.MethodPost, "/v1/share-grants/"+id+"/approve", nil), http.StatusCreated, "duyệt")
		return id
	}
	g1 := newActive(intraProposal(kn, tr.p89))
	g2 := newActive(proposal(kn, port.KindCooperativeDirectory))
	pending := str(admA.adminDo(http.MethodPost, "/v1/share-grants", intraProposal(kn, tr.p92)).body, "id")

	dueIDs := func(days int) []string {
		t.Helper()
		r := admA.adminDo(http.MethodGet, "/v1/share-grant-reviews/due?within_days="+strconv.Itoa(days), nil)
		wantStatus(t, r, http.StatusOK, "danh sách đến hạn")
		return refs(r, "id")
	}
	if got := dueIDs(0); len(got) != 0 {
		t.Fatalf("vừa duyệt chưa đến hạn: %v", got)
	}
	if got := dueIDs(90); len(got) != 2 || !slices.Contains(got, g1) || !slices.Contains(got, g2) || slices.Contains(got, pending) {
		t.Fatalf("đến hạn trong 90 ngày (chỉ cấp phép đang hiệu lực): %v", got)
	}
	wantStatus(t, f.as(f.tenant, uuid.Nil, port.RoleRegistryAdmin).adminDo(http.MethodGet, "/v1/share-grant-reviews/due", nil),
		http.StatusForbidden, "không phải quản trị đơn vị chủ")
	wantStatus(t, admA.adminDo(http.MethodGet, "/v1/share-grant-reviews/due?within_days=999", nil), http.StatusUnprocessableEntity, "within_days quá lớn")

	// Giữ: ghi sổ, đặt lại hạn; gọi lặp cùng khóa → 200, không thêm dòng.
	path := "/v1/share-grants/" + g1 + "/reviews"
	k := key()
	r := admA.call(f.admin.URL, http.MethodPost, path, k, "application/json", []byte(`{"decision":"retain","note":"Còn nhu cầu theo thỏa thuận 2026"}`))
	wantStatus(t, r, http.StatusCreated, "rà soát giữ")
	if str(r.body, "review", "decision") != "retain" || str(r.body, "grant", "last_reviewed_by") != admA.actor.String() {
		t.Fatalf("rà soát giữ: %v", r.body)
	}
	first := str(r.body, "review", "id")
	r = admA.call(f.admin.URL, http.MethodPost, path, k, "application/json", []byte(`{"decision":"retain","note":"Còn nhu cầu theo thỏa thuận 2026"}`))
	if r.status != http.StatusCreated && r.status != http.StatusOK || str(r.body, "review", "id") != first {
		t.Fatalf("gọi lặp cùng khóa phải trả lại đúng dòng rà soát: %d %v", r.status, r.body)
	}
	if got := dueIDs(89); len(got) != 0 {
		t.Fatalf("sau rà soát giữ, hạn mới sau 90 ngày từ hôm nay: %v", got)
	}
	if got := dueIDs(90); len(got) != 2 {
		t.Fatalf("g2 vẫn đến hạn, g1 đến hạn lại sau chu kỳ: %v", got)
	}
	wantStatus(t, admA.adminDo(http.MethodPost, path, map[string]any{"decision": "bo_qua"}), http.StatusUnprocessableEntity, "quyết định lạ")
	wantStatus(t, admA.adminDo(http.MethodPost, path, map[string]any{"decision": "revoke"}), http.StatusUnprocessableEntity, "thu hồi thiếu lý do")
	wantCode(t, admA.adminDo(http.MethodPost, "/v1/share-grants/"+pending+"/reviews", map[string]any{"decision": "retain"}), http.StatusConflict,
		"party.share_grant_state", "rà soát đề xuất chưa duyệt")
	wantStatus(t, f.as(f.tenant, uuid.Nil, port.RoleRegistryAdmin).adminDo(http.MethodPost, path, map[string]any{"decision": "retain"}),
		http.StatusForbidden, "vai khác rà soát")

	epoch := func() int64 {
		t.Helper()
		e, err := f.mod.ContextEpochs().ContextEpoch(context.Background(), kn)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	if e := epoch(); e != 0 {
		t.Fatalf("epoch ban đầu: %d", e)
	}
	// Thu hồi khi rà soát: cùng giao dịch, đơn vị xem tăng epoch.
	r = admA.adminDo(http.MethodPost, path, map[string]any{"decision": "revoke", "note": "Hết nhu cầu khi rà soát quý"})
	wantStatus(t, r, http.StatusCreated, "rà soát thu hồi")
	if str(r.body, "grant", "status") != "revoked" || str(r.body, "grant", "revoke_reason") != "Hết nhu cầu khi rà soát quý" {
		t.Fatalf("rà soát thu hồi: %v", r.body)
	}
	if e := epoch(); e != 1 {
		t.Fatalf("epoch sau thu hồi khi rà soát: %d", e)
	}
	// Thu hồi thường cấp phép đang hiệu lực khác của cùng đơn vị xem: epoch 2; thu hồi đề xuất chưa duyệt: không tăng.
	wantStatus(t, admA.adminDo(http.MethodPost, "/v1/share-grants/"+g2+"/revoke", map[string]any{"reason": "Kết thúc hợp đồng thu gom"}),
		http.StatusCreated, "thu hồi partner")
	wantStatus(t, admA.adminDo(http.MethodPost, "/v1/share-grants/"+pending+"/revoke", map[string]any{"reason": "Rút đề xuất chưa duyệt"}),
		http.StatusCreated, "thu hồi đề xuất")
	if e := epoch(); e != 2 {
		t.Fatalf("epoch sau hai thu hồi cấp phép đang hiệu lực: %d", e)
	}
	if e, _ := f.mod.ContextEpochs().ContextEpoch(context.Background(), f.tenant); e != 0 {
		t.Fatalf("epoch của đơn vị chủ không đổi: %d", e)
	}
	for _, g := range []string{g1, g2} {
		ev := f.outboxPayloads(kn, port.EventContextEpochBumped, g)
		if len(ev) != 1 || ev[0]["grantor_tenant_id"] != f.tenant.String() || ev[0]["cause"] != "share_grant_revoked" {
			t.Fatalf("sự kiện epoch %s: %v", g, ev)
		}
	}
	if ev := f.outboxPayloads(kn, port.EventContextEpochBumped, pending); len(ev) != 0 {
		t.Fatalf("thu hồi đề xuất không tăng epoch: %v", ev)
	}
	if ev := f.outboxPayloads(f.tenant, port.EventShareGrantRevoked, g1); len(ev) != 1 || ev[0]["grant_class"] != "intra_platform" {
		t.Fatalf("sự kiện revoked intra: %v", ev)
	}

	// Sổ rà soát: hai dòng theo thứ tự, chuỗi băm liền, chỉ ghi thêm.
	r = admA.adminDo(http.MethodGet, path, nil)
	if it := items(r); len(it) != 2 || it[0]["decision"] != "retain" || it[1]["decision"] != "revoke" {
		t.Fatalf("sổ rà soát: %v", r.body)
	}
	f.verifyStream(f.tenant, "pf_party.share_grant_review", "review:"+g1, 2)
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "DELETE FROM pf_party.share_grant_review WHERE grant_id = $1", uuid.MustParse(g1))
		return e
	}); err == nil {
		t.Fatal("DELETE sổ share_grant_review phải bị từ chối")
	}
	wantCode(t, admA.adminDo(http.MethodPost, path, map[string]any{"decision": "retain"}), http.StatusConflict, "party.share_grant_state", "rà soát bản đã thu hồi")
}
