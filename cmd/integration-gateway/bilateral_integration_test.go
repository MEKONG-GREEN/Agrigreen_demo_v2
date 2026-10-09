package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// bilateralEnv: cổng THẬT với port thật của hợp đồng song phương (sổ pf_coop của pf/cooperative) + phía HTX (module
// pf/cooperative như api-platform, AG_BILATERAL_ALLOW_PROPOSED) + job dựng feed cặp trên module pf/apiclient của cổng.
type bilateralEnv struct {
	*ingestEnv
	coop  *cooperative.Module
	owner uuid.UUID // tenant chủ của các HTX
}

func newBilateralEnv(t *testing.T) *bilateralEnv {
	t.Helper()
	e := newIngestEnv(t, true, 0, realOwners)
	if e.deps.Bilateral == nil {
		t.Fatal("gốc lắp ráp phải nối sổ song phương (Ports.Bilateral)")
	}
	coop := cooperative.New(e.pg.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, cooperative.Deps{})
	coop.UseBilateral(false, true)
	e.mod.UseBilateral(e.deps.Bilateral)
	return &bilateralEnv{ingestEnv: e, coop: coop, owner: e.grantor}
}

// seedHTX ghi một Nút HTX (pf_party.org kind cooperative) + hồ sơ (pf_coop.cooperative) ở tenant chủ.
func (b *bilateralEnv) seedHTX() uuid.UUID {
	t := b.t
	htx := ids.New()
	owner := b.pg.Pool(t, migrations.PF, dbadmin.Migrator)
	if err := tenant.WithTenantTx(context.Background(), owner, b.owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), `INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, active, last_event_id, last_seq)
		  VALUES ($1, $2, 'cooperative', $3, 'HTX song phương (thử)', '89123', true, $4, 1)`, b.owner, htx, "W56-"+htx.String()[24:], ids.New()); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_coop.cooperative (tenant_id, org_ref) VALUES ($1, $2)`, b.owner, htx)
		return err
	}); err != nil {
		t.Fatalf("seed HTX: %v", err)
	}
	return htx
}

// as trả ngữ cảnh người dùng App HTX (coop_manager, đơn vị công tác = node) ở tenant tid.
func as(tid, node uuid.UUID, roles ...string) context.Context {
	if len(roles) == 0 {
		roles = []string{partyport.RoleCoopManager}
	}
	return authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: tid, OrgRef: node, Roles: roles})
}

// onboardBilateral: khách hàng API của tenant đối tác với scope ingest:collection_schedule:write,
// ingest:roll_count_statement:write, feed:htx_roll_confirmation:read (bốn mắt) + gói + khóa.
func (b *bilateralEnv) onboardBilateral(partner uuid.UUID) string {
	t := b.t
	ad, ap := b.adminAs(b.operator, apiclientport.RoleAPIClientAdmin), b.adminAs(b.operator, apiclientport.RoleAPIClientApprover)
	r := ad(http.MethodPost, "/v1/api-clients", map[string]any{"client_tenant_id": partner.String(), "display_name": "MKG song phương (thử)",
		"purpose": "Lịch gom, bảng kê số cuộn và xác nhận của HTX", "contact_ref": ids.New().String()})
	want(t, r, http.StatusCreated, "đề xuất khách hàng API")
	clientID := r.body["id"].(string)
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/approve", nil), http.StatusOK, "duyệt khách hàng")
	for product, scope := range map[string]string{"collection_schedule": apiclientport.IngestScope("collection_schedule"),
		"roll_count_statement": apiclientport.IngestScope("roll_count_statement"), "htx_roll_confirmation": apiclientport.FeedScope("htx_roll_confirmation")} {
		r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions", map[string]any{"product": product, "scopes": []string{scope}})
		want(t, r, http.StatusCreated, "đề xuất đăng ký "+product)
		want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions/"+r.body["id"].(string)+"/approve", nil), http.StatusOK, "duyệt "+product)
	}
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/plans", map[string]any{"requests_per_minute": 600, "daily_units": 1000,
		"monthly_units": 10000, "max_export_rows": 1000})
	want(t, r, http.StatusCreated, "đề xuất gói")
	want(t, ap(http.MethodPost, fmt.Sprintf("/v1/api-clients/%s/plans/%v/approve", clientID, r.body["version"]), nil), http.StatusOK, "duyệt gói")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/keys", nil)
	want(t, r, http.StatusCreated, "phát hành khóa")
	return r.body["api_key"].(string)
}

func stmt(ref, htx uuid.UUID, revision, rolls int) map[string]any {
	return map[string]any{"statement_ref": ref.String(), "schedule_ref": nil, "htx_ref": htx.String(), "plot_ref": "01989f00-aa11-7b22-8c33-d44e55f66a77",
		"season_ref": nil, "collected_on": "2026-10-09", "rolls": rolls, "revision": revision}
}

func schedule(ref, htx uuid.UUID) map[string]any {
	return map[string]any{"schedule_ref": ref.String(), "htx_ref": htx.String(), "commune_code": "89123", "window_from": "2026-10-10",
		"window_to": "2026-10-12", "status": "planned", "revision": 1}
}

func confirmation(st, htx uuid.UUID, revision, mkgRolls int, outcome string, htxRolls *int, reason *string) []byte {
	b := fmt.Sprintf(`{"confirmation_ref":%q,"statement_ref":%q,"statement_revision":%d,"htx_ref":%q,"plot_ref":"01989f00-aa11-7b22-8c33-d44e55f66a77",`+
		`"mkg_rolls":%d,"outcome":%q,"htx_rolls":%s,"reason":%s,"responded_at":"2026-10-09T17:00:00+07:00","supersedes_confirmation_ref":null}`,
		ids.New(), st, revision, htx, mkgRolls, outcome, jsonInt(htxRolls), jsonStr(reason))
	return []byte(b)
}

func jsonInt(p *int) string {
	if p == nil {
		return "null"
	}
	return strconv.Itoa(*p)
}

func jsonStr(p *string) string {
	if p == nil {
		return "null"
	}
	return strconv.Quote(*p)
}

// submit gửi một lô, xử lý hàng đợi, trả kết quả từng bản ghi (outcome, problem_code).
func (b *bilateralEnv) submit(key, contract string, items ...map[string]any) [][2]string {
	b.t.Helper()
	r := b.post(key, contract, batchBody(contract, ids.New().String(), items...))
	want(b.t, r, http.StatusAccepted, "lô "+contract)
	b.drain()
	g := b.call(http.MethodGet, "/v1/batches/"+r.body["id"].(string), key, nil)
	want(b.t, g, http.StatusOK, "kết quả lô")
	var out [][2]string
	for _, x := range g.body["results"].([]any) {
		m := x.(map[string]any)
		code, _ := m["problem_code"].(string)
		out = append(out, [2]string{m["outcome"].(string), code})
	}
	return out
}

// countRows đếm dòng sổ song phương thấy được dưới vai r_cooperative ở (tenant, Nút) — chính sách hai bên.
func (b *bilateralEnv) countRows(tid, node uuid.UUID) int {
	b.t.Helper()
	var n int
	pool := db.ForModule(b.pg.Pool(b.t, migrations.PF, dbadmin.AppPlatform), db.Module("pf/cooperative"))
	ctx := context.Background()
	if node != uuid.Nil {
		ctx = as(tid, node)
	}
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_coop.bilateral_record`).Scan(&n)
	}); err != nil {
		b.t.Fatal(err)
	}
	return n
}

func isCode(err error, code string) bool {
	var p *problem.Problem
	return errors.As(err, &p) && p.Code == code
}

// W5.6 (ADR-0034, kiến trúc v4.1 §38.2) trên cổng THẬT + DB thật, đầu–cuối trong tiến trình:
//   - MKG gửi lịch gom + bảng kê số cuộn qua nạp theo lô → port THẬT (sổ song phương pf_coop); chưa lập cặp → rejected
//     integration.pair_missing; khóa nghiệp vụ idempotent (cùng nội dung accepted, khác nội dung business_key_conflict);
//   - chỉ hai bên thấy: HTX của cặp thấy (App HTX), HTX khác cùng tenant, cán bộ KN khác Nút cùng tenant, đối tác khác
//     không thấy (chính sách p_pair ở DB + kiểm cặp ở cổng); bên MKG không ghi được bản ghi bên b;
//   - HTX xác nhận / phản đối → job dựng feed cặp → MKG đọc GET /v1/feeds/bilateral?pair= (cả hai phía, mỗi mục có
//     contract); đối tác khác / HTX khác → 403;
//   - tổng kiểm ngày bilateral_day.v1 của cổng (sổ thật) = tổng kiểm MKG tính lại trên trang feed cặp;
//   - chấm dứt cặp → ghi + đọc feed bị từ chối, lịch sử còn (HTX đọc được, tổng kiểm còn); lập lại → mở lại.
func TestBilateralPairFlow_W56Integration(t *testing.T) {
	b := newBilateralEnv(t)
	ctx := context.Background()
	mkg, other := ids.New(), ids.New()
	key, otherKey := b.onboardBilateral(mkg), b.onboardBilateral(other)
	htx1, htx2 := b.seedHTX(), b.seedHTX()
	st1, st2, sch := ids.New(), ids.New(), ids.New()

	// 1. Chưa lập cặp → mặc định từ chối.
	if res := b.submit(key, "roll_count_statement", item(st1.String(), stmt(st1, htx1, 1, 46))); res[0] != [2]string{"rejected", "integration.pair_missing"} {
		t.Fatalf("chưa có cặp → rejected integration.pair_missing: %v", res)
	}
	// htx_roll_confirmation (bên b) không nối qua cổng — mặc định từ chối.
	if r := b.post(key, "htx_roll_confirmation", batchBody("htx_roll_confirmation", ids.New().String(), item(ids.New().String(), map[string]any{}))); r.status != http.StatusForbidden {
		t.Fatalf("bên b qua cổng → 403: %d %s", r.status, r.raw)
	}

	// 2. HTX1 lập cặp với MKG (chỉ bên HTX, vai coop_manager, Nút có hồ sơ).
	if _, err := b.coop.Bilateral().EstablishPair(as(b.owner, ids.New()), mkg); !isCode(err, "cooperative.bilateral_not_htx") {
		t.Fatalf("Nút không phải HTX → cooperative.bilateral_not_htx: %v", err)
	}
	pair, err := b.coop.Bilateral().EstablishPair(as(b.owner, htx1), mkg)
	if err != nil || !pair.Active || pair.PairID != apiclientport.PairID(mkg, htx1) {
		t.Fatalf("lập cặp: %+v %v", pair, err)
	}
	if _, err := b.coop.Bilateral().EstablishPair(as(b.owner, htx1), mkg); !isCode(err, "cooperative.bilateral_pair_state") {
		t.Fatalf("lập lại cặp đang hiệu lực → 409: %v", err)
	}

	// 3. MKG gửi lịch gom + hai bảng kê; idempotent theo khóa nghiệp vụ.
	if res := b.submit(key, "collection_schedule", item(sch.String(), schedule(sch, htx1))); res[0][0] != "accepted" {
		t.Fatalf("lịch gom: %v", res)
	}
	res := b.submit(key, "roll_count_statement", item(st1.String(), stmt(st1, htx1, 1, 46)), item(st2.String(), stmt(st2, htx1, 1, 12)))
	if res[0][0] != "accepted" || res[1][0] != "accepted" {
		t.Fatalf("bảng kê: %v", res)
	}
	if res = b.submit(key, "roll_count_statement", item(st1.String(), stmt(st1, htx1, 1, 46))); res[0][0] != "accepted" {
		t.Fatalf("gửi lại cùng nội dung → accepted (idempotent): %v", res)
	}
	if res = b.submit(key, "roll_count_statement", item(st1.String(), stmt(st1, htx1, 1, 47))); res[0] != [2]string{"rejected", "integration.business_key_conflict"} {
		t.Fatalf("cùng khóa khác nội dung → business_key_conflict: %v", res)
	}
	// Đối tác khác ghi vào HTX1 (không có cặp) → từ chối.
	st9 := ids.New()
	if res = b.submit(otherKey, "roll_count_statement", item(st9.String(), stmt(st9, htx1, 1, 5))); res[0][1] != "integration.pair_missing" {
		t.Fatalf("đối tác khác → pair_missing: %v", res)
	}

	// 4. Chỉ hai bên thấy (chính sách ở DB).
	for _, c := range []struct {
		what      string
		tid, node uuid.UUID
		want      int
	}{{"HTX của cặp", b.owner, htx1, 3}, {"HTX khác cùng tenant", b.owner, htx2, 0}, {"cán bộ KN Nút khác cùng tenant", b.owner, ids.New(), 0},
		{"tenant chủ không Nút", b.owner, uuid.Nil, 0}, {"MKG (counterparty)", mkg, uuid.Nil, 3}, {"đối tác khác", other, uuid.Nil, 0}} {
		if n := b.countRows(c.tid, c.node); n != c.want {
			t.Fatalf("%s thấy %d dòng, muốn %d", c.what, n, c.want)
		}
	}
	// Bên MKG không ghi được bản ghi bên b (WITH CHECK).
	mkgDB := db.ForModule(b.pg.Pool(t, migrations.PF, dbadmin.AppPublic), db.Module("pf/cooperative"))
	err = tenant.WithTenantTx(ctx, mkgDB, mkg, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_coop.bilateral_record (id, tenant_id, stream_key, pair_id, htx_node_id, counterparty_tenant_id, contract,
		    contract_version, record_ref, business_key, payload, payload_sha256, author_side, channel, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'pair:x', $3, $4, $5, 'htx_roll_confirmation', 1, 'k', 'k', '{}', repeat('a', 64), 'b', 'app', now(), $1, $6, 'x')`,
			ids.New(), b.owner, pair.PairID, htx1, mkg, "forge:"+ids.New().String())
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("MKG giả bản ghi bên b phải bị chính sách chặn: %v", err)
	}
	// HTX thấy qua App HTX; HTX khác không.
	recs, err := b.coop.Bilateral().Records(as(b.owner, htx1), pair.PairID, 0, 100)
	if err != nil || len(recs) != 3 || recs[0].Seq != 1 || recs[2].Seq != 3 {
		t.Fatalf("HTX đọc bản ghi của cặp: %d %v", len(recs), err)
	}
	if _, err := b.coop.Bilateral().Records(as(b.owner, htx2), pair.PairID, 0, 100); !isCode(err, "integration.pair_missing") {
		t.Fatalf("HTX khác đọc cặp → pair_missing: %v", err)
	}

	// 5. HTX xác nhận st1, phản đối st2; idempotent; số cuộn lệch → 422.
	if _, _, err := b.coop.Bilateral().Confirm(as(b.owner, htx1), confirmation(st1, htx1, 1, 40, "confirmed", nil, nil)); !isCode(err, "cooperative.bilateral_rolls_mismatch") {
		t.Fatalf("mkg_rolls lệch → 422: %v", err)
	}
	c1 := confirmation(st1, htx1, 1, 46, "confirmed", nil, nil)
	if r, created, err := b.coop.Bilateral().Confirm(as(b.owner, htx1), c1); err != nil || !created || r.AuthorSide != "b" {
		t.Fatalf("xác nhận: %+v %v %v", r, created, err)
	}
	if _, created, err := b.coop.Bilateral().Confirm(as(b.owner, htx1), c1); err != nil || created {
		t.Fatalf("xác nhận lại cùng nội dung → không tạo mới: %v %v", created, err)
	}
	ten, why := 10, "Đếm lại tại bãi chỉ có 10 cuộn"
	if _, _, err := b.coop.Bilateral().Confirm(as(b.owner, htx1), confirmation(st2, htx1, 1, 12, "disputed", &ten, &why)); err != nil {
		t.Fatalf("phản đối: %v", err)
	}
	if _, _, err := b.coop.Bilateral().Confirm(as(b.owner, htx2), confirmation(st2, htx2, 1, 12, "confirmed", nil, nil)); !isCode(err, "cooperative.bilateral_statement_unknown") {
		t.Fatalf("HTX khác xác nhận bảng kê không phải của mình → 422: %v", err)
	}

	// 6. Feed của cặp: job dựng → MKG đọc cả hai phía; đối tác khác / HTX khác → 403.
	if _, err := b.mod.Service().BuildBilateral(ctx); err != nil {
		t.Fatalf("dựng feed cặp: %v", err)
	}
	if _, err := b.mod.Service().BuildBilateral(ctx); err != nil {
		t.Fatalf("dựng lại (không có gì mới): %v", err)
	}
	r := b.call(http.MethodGet, "/v1/feeds/bilateral?pair="+htx1.String(), key, nil)
	want(t, r, http.StatusOK, "feed cặp")
	items := r.body["items"].([]any)
	if r.body["feed"] != "bilateral" || len(items) != 5 || r.body["has_more"] != false {
		t.Fatalf("feed cặp: 3 bản ghi MKG + 2 phản hồi HTX: %s", r.raw)
	}
	byContract := map[string][]map[string]any{}
	for i, x := range items {
		m := x.(map[string]any)
		if m["seq"] != float64(i+1) {
			t.Fatalf("seq liên tục theo sổ: %v", m)
		}
		byContract[m["contract"].(string)] = append(byContract[m["contract"].(string)], m)
	}
	if len(byContract["collection_schedule.v1"]) != 1 || len(byContract["roll_count_statement.v1"]) != 2 || len(byContract["htx_roll_confirmation.v1"]) != 2 {
		t.Fatalf("mục theo hợp đồng: %v", byContract)
	}
	for _, c := range []struct {
		key  string
		htx  uuid.UUID
		code string
	}{{otherKey, htx1, "integration.pair_missing"}, {key, htx2, "integration.pair_missing"}} {
		if r := b.call(http.MethodGet, "/v1/feeds/bilateral?pair="+c.htx.String(), c.key, nil); r.status != http.StatusForbidden || r.code() != c.code {
			t.Fatalf("feed cặp không phải của bên gọi → 403 %s: %d %s", c.code, r.status, r.raw)
		}
	}
	if r := b.call(http.MethodGet, "/v1/feeds/bilateral", key, nil); r.status != http.StatusBadRequest && r.status != http.StatusUnprocessableEntity {
		t.Fatalf("thiếu pair → lỗi kiểm tra: %d", r.status)
	}
	next := r.body["next_cursor"].(string)
	if r := b.call(http.MethodGet, "/v1/feeds/bilateral?pair="+htx1.String()+"&cursor="+next, key, nil); r.status != http.StatusOK || len(r.body["items"].([]any)) != 0 {
		t.Fatalf("đọc tiếp từ cursor đầu feed → rỗng: %s", r.raw)
	}

	// 7. Tổng kiểm ngày của cổng (sổ thật) = tổng kiểm MKG tính trên bản sao feed cặp — cả hai phía.
	for contract, mine := range byContract {
		name, _, _ := strings.Cut(contract, ".v")
		ct, err := integration.Get(name, 1)
		if err != nil {
			t.Fatal(err)
		}
		days := map[string][]integration.BilateralDayRecord{}
		for _, m := range mine {
			raw, _ := json.Marshal(m["record"])
			bk, _ := ct.BusinessKeyOf(raw)
			sum, _ := integration.PayloadSHA256(raw)
			at, _ := time.Parse(time.RFC3339Nano, m["recorded_at"].(string))
			d := clock.BusinessDate(at).String()
			days[d] = append(days[d], integration.BilateralDayRecord{BusinessKey: bk, ContractVersion: 1, PayloadSHA256: sum})
		}
		for d, rs := range days {
			n, sum := integration.BilateralDayDigest(rs)
			cr := b.call(http.MethodGet, "/v1/feeds/"+name+"/checksums?from="+d+"&to="+d+"&pair="+htx1.String(), key, nil)
			want(t, cr, http.StatusOK, "tổng kiểm "+name)
			day := cr.body["days"].([]any)[0].(map[string]any)
			if cr.body["algorithm"] != "bilateral_day.v1" || day["record_count"] != float64(n) || day["sha256"] != sum {
				t.Fatalf("tổng kiểm %s ngày %s: cổng %v, MKG %d %s", name, d, day, n, sum)
			}
		}
	}
	// Đối tác khác: tổng kiểm của mình rỗng (không thấy bản ghi của MKG).
	today := clock.BusinessDate(time.Now()).String()
	cr := b.call(http.MethodGet, "/v1/feeds/roll_count_statement/checksums?from="+today+"&to="+today, otherKey, nil)
	want(t, cr, http.StatusOK, "tổng kiểm đối tác khác")
	if d := cr.body["days"].([]any)[0].(map[string]any); d["record_count"] != float64(0) {
		t.Fatalf("đối tác khác không thấy bản ghi của cặp khác: %s", cr.raw)
	}

	// 8. Chấm dứt cặp: ghi + feed bị từ chối; lịch sử còn; lập lại → mở lại.
	if _, err := b.coop.Bilateral().TerminatePair(as(b.owner, htx1), pair.PairID, "ngắn"); err == nil {
		t.Fatal("lý do < 10 ký tự → lỗi kiểm tra")
	}
	if p, err := b.coop.Bilateral().TerminatePair(as(b.owner, htx1), pair.PairID, "Hết hợp đồng thu mua vụ Thu Đông"); err != nil || p.Active {
		t.Fatalf("chấm dứt: %+v %v", p, err)
	}
	st3 := ids.New()
	if res = b.submit(key, "roll_count_statement", item(st3.String(), stmt(st3, htx1, 1, 3))); res[0][1] != "integration.pair_missing" {
		t.Fatalf("sau chấm dứt → pair_missing: %v", res)
	}
	if r := b.call(http.MethodGet, "/v1/feeds/bilateral?pair="+htx1.String(), key, nil); r.status != http.StatusForbidden || r.code() != "integration.pair_missing" {
		t.Fatalf("feed cặp đã chấm dứt → 403: %d %s", r.status, r.raw)
	}
	if recs, err := b.coop.Bilateral().Records(as(b.owner, htx1), pair.PairID, 0, 100); err != nil || len(recs) != 5 {
		t.Fatalf("HTX vẫn đọc lịch sử: %d %v", len(recs), err)
	}
	if cr := b.call(http.MethodGet, "/v1/feeds/htx_roll_confirmation/checksums?pair="+htx1.String(), key, nil); cr.status != http.StatusOK {
		t.Fatalf("tổng kiểm lịch sử sau chấm dứt: %d %s", cr.status, cr.raw)
	}
	if _, _, err := b.coop.Bilateral().Confirm(as(b.owner, htx1), confirmation(st1, htx1, 1, 46, "confirmed", nil, nil)); !isCode(err, "integration.pair_missing") {
		t.Fatalf("xác nhận sau chấm dứt → pair_missing: %v", err)
	}
	if _, err := b.coop.Bilateral().EstablishPair(as(b.owner, htx1), mkg); err != nil {
		t.Fatalf("lập lại: %v", err)
	}
	if r := b.call(http.MethodGet, "/v1/feeds/bilateral?pair="+htx1.String(), key, nil); r.status != http.StatusOK {
		t.Fatalf("lập lại → feed mở: %d %s", r.status, r.raw)
	}
	pairs, err := b.coop.Bilateral().Pairs(as(b.owner, htx1))
	if err != nil || len(pairs) != 1 || !pairs[0].Active {
		t.Fatalf("danh sách cặp của HTX: %+v %v", pairs, err)
	}
}

// W5.7 (R-INT-10, ADR-0034) trên sổ song phương THẬT (W5.6): số bản ghi + băm theo công thức bilateral_day.v1 của
// các cặp mà bên gọi là counterparty; đối tác khác không thấy; khoảng > 31 ngày → 400; thiếu scope → 403.
func TestBilateralDailyChecksums_W57Integration(t *testing.T) {
	b := newBilateralEnv(t)
	partner, other := ids.New(), ids.New()
	key, key2 := b.onboardBilateral(partner), b.onboardBilateral(other)
	htx := b.seedHTX()
	if _, err := b.coop.Bilateral().EstablishPair(as(b.owner, htx), partner); err != nil {
		t.Fatal(err)
	}
	if _, err := b.coop.Bilateral().EstablishPair(as(b.owner, htx), other); err != nil {
		t.Fatal(err)
	}
	refA, refB, refC := ids.New(), ids.New(), ids.New()
	if res := b.submit(key, "roll_count_statement", item(refA.String(), stmt(refA, htx, 1, 46)), item(refB.String(), stmt(refB, htx, 1, 12))); res[0][0] != "accepted" || res[1][0] != "accepted" {
		t.Fatalf("lô đối tác 1: %v", res)
	}
	if res := b.submit(key2, "roll_count_statement", item(refC.String(), stmt(refC, htx, 1, 9))); res[0][0] != "accepted" {
		t.Fatalf("lô đối tác 2: %v", res)
	}
	today := clock.BusinessDate(time.Now()).String()
	r := b.call(http.MethodGet, "/v1/feeds/roll_count_statement/checksums?from="+today+"&to="+today, key, nil)
	want(t, r, http.StatusOK, "tổng kiểm ngày")
	days := r.body["days"].([]any)
	day := days[0].(map[string]any)
	// Tính lại từ sổ song phương theo đúng công thức.
	var lines []string
	owner := b.pg.Pool(t, migrations.PF, dbadmin.Migrator)
	if err := tenant.WithTenantTx(context.Background(), owner, partner, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT business_key, contract_version, payload_sha256 FROM pf_coop.bilateral_record
		  WHERE counterparty_tenant_id = $1 AND contract = 'roll_count_statement'`, partner)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var bk, sum string
			var v int
			if err := rows.Scan(&bk, &v, &sum); err != nil {
				return err
			}
			lines = append(lines, bk+"\t"+strconv.Itoa(v)+"\t"+sum+"\n")
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	h := sha256.Sum256([]byte(strings.Join(lines, "")))
	if r.body["kind"] != "bilateral_day" || r.body["algorithm"] != "bilateral_day.v1" || len(days) != 1 || day["date"] != today ||
		day["record_count"] != float64(2) || day["sha256"] != hex.EncodeToString(h[:]) || len(lines) != 2 {
		t.Fatalf("tổng kiểm ngày của bên gọi (2 bản ghi, không gồm đối tác khác): %s", r.raw)
	}
	if r = b.call(http.MethodGet, "/v1/feeds/roll_count_statement/checksums?from=2026-01-01&to=2026-03-01", key, nil); r.status != http.StatusUnprocessableEntity && r.status != http.StatusBadRequest {
		t.Fatalf("> 31 ngày → lỗi kiểm tra: %d %s", r.status, r.raw)
	}
	_, onlyRoll := b.onboardIngest(ids.New(), "roll_count_statement")
	if r = b.call(http.MethodGet, "/v1/feeds/collection_schedule/checksums", onlyRoll, nil); r.status != http.StatusForbidden {
		t.Fatalf("thiếu scope hợp đồng → 403: %d %s", r.status, r.raw)
	}
	if r = b.call(http.MethodGet, "/v1/feeds/roll_count_statement/checksums?pair="+ids.New().String(), key, nil); r.status != http.StatusForbidden || r.code() != "integration.pair_missing" {
		t.Fatalf("pair lạ → 403 pair_missing: %d %s", r.status, r.raw)
	}
}
