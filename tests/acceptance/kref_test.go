package acceptance

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref"
	krefport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// W2.5 (ADR-0029): thao tác thật qua Admin API (tạo cây, di chuyển nhánh, vô hiệu hóa HTX, duyệt/thu hồi cấp
// quyền, nạp đơn vị hành chính) → sự kiện trên shared.outbox → consumer pf.kref (app_worker, r_kref) → k_ref
// KHỚP bản dựng độc lập từ nguồn; dựng lại toàn bộ cho cùng kết quả và sửa được k_ref đã lệch.
func TestKRefIncrementalEqualsRebuild_W25(t *testing.T) {
	w := newWorld(t)
	s := w.s
	ctx := s.Ctx()

	// Di chuyển nhánh tổ Cái Răng (kèm HTX Cái Răng) sang TTKN An Giang, đổi xã tại chỗ.
	w.want(w.root.Do(harness.Admin, http.MethodPost, "/v1/orgs/"+w.teamBT+"/move", "", map[string]any{
		"new_parent_ref": w.orgP91, "new_admin_unit_code": communeLX, "reason": "Điều chuyển địa bàn theo quyết định 12/2026",
	}), 200, "di chuyển nhánh")
	w.want(w.root.Do(harness.Admin, http.MethodPost, "/v1/cooperatives/"+w.coopLX+"/deactivate", "", map[string]any{
		"reason": "HTX giải thể theo quyết định của UBND xã năm 2026",
	}), 200, "vô hiệu hóa HTX")
	kept := s.GrantShare(w.tid, harness.NewTenant(), "Thu gom lúa vụ Hè Thu theo hợp đồng", "cooperative_directory")
	gone := s.GrantShare(w.tid, harness.NewTenant(), "Thử nghiệm cấp quyền sẽ thu hồi", "plot_directory")
	if r := gone.Approver.Do(harness.Admin, http.MethodPost, "/v1/share-grants/"+gone.ID+"/revoke", "", map[string]any{
		"reason": "Hết nhu cầu chia sẻ dữ liệu"}); r.Status/100 != 2 {
		t.Fatalf("thu hồi cấp quyền: %d %s", r.Status, trim(r.Raw))
	}

	// Sự kiện k_ref của tenant theo thứ tự ghi.
	rows, err := s.AdminPF().Query(ctx, `SELECT id, event_type, schema_version, tenant_id, occurred_at, recorded_at, producer, payload
	  FROM shared.outbox WHERE tenant_id = $1 AND event_type = ANY($2::text[]) ORDER BY recorded_at, id`, w.tid, kref.Events)
	if err != nil {
		t.Fatal(err)
	}
	envs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (outbox.Envelope, error) {
		var e outbox.Envelope
		return e, r.Scan(&e.EventID, &e.EventType, &e.SchemaVersion, &e.TenantID, &e.OccurredAt, &e.RecordedAt, &e.Producer, &e.Payload)
	})
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, e := range envs {
		count[e.EventType]++
	}
	if count[kref.EventOrgCreated] != 10 || count[kref.EventOrgMoved] != 1 || count[kref.EventOrgDeactivated] != 1 ||
		count[kref.EventShareGrantApprove] != 2 || count[kref.EventShareGrantRevoke] != 1 || count[kref.EventAdminUnitChanged] == 0 {
		t.Fatalf("sự kiện nguồn: %v", count)
	}

	workerPool := s.PF(dbadmin.AppWorker)
	mod := kref.New(workerPool, clock.System{})
	b := db.ForModule(workerPool, kref.DBModule)
	for _, e := range envs {
		if _, err := inbox.Process(ctx, b, kref.Consumer, e, mod.Handler()); err != nil {
			t.Fatalf("%s %s: %v", e.EventType, e.EventID, err)
		}
	}

	pty := party.New(s.PF(party.DBRole), clock.System{})
	md := masterdata.New(s.PF(masterdata.DBRole), clock.System{})
	src := krefport.Sources{Orgs: pty.KRefOrgs(), ShareGrants: pty.KRefShareGrants(), AdminUnits: md.KRefAdminUnits(),
		Tenants: kref.UnionTenants(party.KRefTenants(s.PF(dbadmin.OutboxPublisher)), masterdata.KRefTenants(s.PF(dbadmin.OutboxPublisher)))}
	want, err := kref.Expected(ctx, src, w.tid)
	if err != nil {
		t.Fatal(err)
	}
	got, err := mod.Snapshot(ctx, w.tid)
	if err != nil {
		t.Fatal(err)
	}
	if d := kref.Diff(got, want); len(d) != 0 {
		t.Fatalf("cộng dồn từ sự kiện lệch nguồn ở %v", d)
	}
	coopBT := uuid.MustParse(w.coopBT)
	anc := map[uuid.UUID]int{}
	for _, c := range got.Closure {
		if c.Descendant == coopBT {
			anc[c.Ancestor] = c.Depth
		}
	}
	if anc[uuid.MustParse(w.orgP91)] != 2 || anc[uuid.MustParse(w.orgNational)] != 3 || len(anc) != 4 {
		t.Fatalf("HTX Cái Răng phải nằm dưới TTKN An Giang sau di chuyển nhánh: %v", anc)
	}
	if len(got.ShareGrants) != 1 || got.ShareGrants[0].GrantID.String() != kept.ID {
		t.Fatalf("chỉ cấp quyền còn hiệu lực: %+v", got.ShareGrants)
	}
	if i := slices.IndexFunc(got.Orgs, func(o krefport.Org) bool { return o.Ref.String() == w.coopLX }); i < 0 || got.Orgs[i].Active {
		t.Fatal("HTX Long Xuyên phải đã vô hiệu hóa trong k_ref")
	}

	// Làm lệch k_ref (mô phỏng sự kiện mất) rồi dựng lại toàn bộ → khớp nguồn.
	if _, err := s.AdminPF().Exec(ctx, `DELETE FROM k_ref.node_closure WHERE tenant_id = $1 AND depth > 0`, w.tid); err != nil {
		t.Fatal(err)
	}
	res, err := mod.Rebuild(ctx, src)
	if err != nil || res.Tenants < 1 {
		t.Fatalf("dựng lại: %+v %v", res, err)
	}
	after, err := mod.Snapshot(ctx, w.tid)
	if err != nil {
		t.Fatal(err)
	}
	if d := kref.Diff(after, want); len(d) != 0 {
		t.Fatalf("dựng lại lệch nguồn ở %v", d)
	}
}
