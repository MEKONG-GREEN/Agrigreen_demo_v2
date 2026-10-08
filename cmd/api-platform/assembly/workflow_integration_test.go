package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type wfAdmin struct {
	t   *testing.T
	srv *httptest.Server
	iss *authztest.FakeIssuer
}

type wfActor struct {
	a     *wfAdmin
	actor uuid.UUID
	token string
}

func (a *wfAdmin) as(tid uuid.UUID, roles ...string) wfActor {
	actor := ids.New()
	return wfActor{a: a, actor: actor, token: a.iss.Sign(a.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, MFA: true})}
}

func (c wfActor) post(path string, body any) (int, map[string]any) {
	c.a.t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, c.a.srv.URL+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "k-"+ids.New().String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.a.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return resp.StatusCode, m
}

// wfRow đọc yêu cầu + quyết định của một đối tượng trong pf_workflow (vai migrator, đặt tenant — RLS áp).
type wfRow struct {
	requester, decider *uuid.UUID
	outcome            *string
}

func readWF(t *testing.T, pool *pgxpool.Pool, tid uuid.UUID, kind, subject string) (wfRow, bool) {
	t.Helper()
	var r wfRow
	found := false
	err := tenant.WithTenantTx(context.Background(), pool, tid, func(tx pgx.Tx) error {
		err := tx.QueryRow(context.Background(), `SELECT q.requester_ref, d.decider_ref, d.outcome
		    FROM pf_workflow.request q LEFT JOIN pf_workflow.decision d ON d.request_id = q.id
		   WHERE q.kind = $1 AND q.subject_ref = $2::uuid`, kind, subject).Scan(&r.requester, &r.decider, &r.outcome)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, found
}

// W6.1: gốc lắp ráp nối cap/workflow vào duyệt tham số (pf/masterdata) và duyệt cấp quyền chia sẻ (pf/party). Mã lỗi,
// trạng thái HTTP không đổi; mỗi lần gửi duyệt/đề xuất có một yêu cầu, mỗi lần duyệt/từ chối/thu hồi đề xuất có một
// quyết định cùng giao dịch; phiên bản gửi duyệt trước khi nối workflow được lập bù yêu cầu lúc duyệt.
func TestWorkflowWiredApprovals_W61(t *testing.T) {
	env := pgtest.Get(t)
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}
	mods, _, err := Build(context.Background(), open, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	iss := authztest.New(t)
	ops := env.Pool(t, migrations.PF, "platform_api")
	r := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	g := r.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops))
	mods.MasterData.RegisterAdmin(g)
	mods.Party.RegisterAdmin(g)
	// Module masterdata KHÔNG nối workflow — như phiên bản gửi duyệt trước W6.1.
	legacy := masterdata.New(env.Pool(t, migrations.PF, masterdata.DBRole), clock.System{})
	lr := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	legacy.RegisterAdmin(lr.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	adm := &wfAdmin{t: t, srv: httptest.NewServer(r), iss: iss}
	t.Cleanup(adm.srv.Close)
	old := &wfAdmin{t: t, srv: httptest.NewServer(lr), iss: iss}
	t.Cleanup(old.srv.Close)
	mig := env.Pool(t, migrations.PF, "ag_migrator")

	tid := ids.New()
	setter := adm.as(tid, mdport.RoleParamSetter, mdport.RoleParamApprover)
	approver := adm.as(tid, mdport.RoleParamApprover)
	propose := func(c wfActor, key, value string) string {
		code, b := c.post("/v1/masterdata/params/versions", map[string]any{"key": key, "value": value, "unit": "ha/máy/vụ",
			"effective_from": "2026-01-01", "source_doc": "QĐ số 1/QĐ-KTHT"})
		if code != 201 {
			t.Fatalf("đề xuất: %d %v", code, b)
		}
		id := b["id"].(string)
		if code, b := c.post("/v1/masterdata/params/versions/"+id+"/submit", map[string]any{}); code != 200 || b["status"] != "submitted" {
			t.Fatalf("gửi duyệt: %d %v", code, b)
		}
		return id
	}

	// Tham số: gửi duyệt → yêu cầu; tự duyệt → 403 cũ, không có quyết định; người khác duyệt → quyết định approved.
	id := propose(setter, "cgh.norm_ha_per_machine:MGD:thu_hoach", "90")
	if row, ok := readWF(t, mig, tid, masterdata.ParamApprovalKind, id); !ok || *row.requester != setter.actor || row.decider != nil {
		t.Fatalf("yêu cầu sau gửi duyệt: %+v %v", row, ok)
	}
	if code, b := setter.post("/v1/masterdata/params/versions/"+id+"/approve", map[string]any{}); code != 403 || b["code"] != "self_approval_forbidden" {
		t.Fatalf("tự duyệt: %d %v", code, b)
	}
	if code, b := approver.post("/v1/masterdata/params/versions/"+id+"/approve", map[string]any{}); code != 200 ||
		b["version"].(map[string]any)["status"] != "approved" {
		t.Fatalf("duyệt: %d %v", code, b)
	}
	if row, _ := readWF(t, mig, tid, masterdata.ParamApprovalKind, id); row.decider == nil || *row.decider != approver.actor || *row.outcome != "approved" {
		t.Fatalf("quyết định duyệt: %+v", row)
	}
	// Từ chối.
	id2 := propose(setter, "cgh.norm_ha_per_machine:MGD:lam_dat", "70")
	if code, b := approver.post("/v1/masterdata/params/versions/"+id2+"/reject", map[string]any{"note": "Thiếu căn cứ văn bản"}); code != 200 || b["status"] != "rejected" {
		t.Fatalf("từ chối: %d %v", code, b)
	}
	if row, _ := readWF(t, mig, tid, masterdata.ParamApprovalKind, id2); row.outcome == nil || *row.outcome != "rejected" {
		t.Fatalf("quyết định từ chối: %+v", row)
	}
	// Lập bù: gửi duyệt qua module chưa nối workflow → chưa có yêu cầu; duyệt qua module đã nối → yêu cầu + quyết định.
	oldSetter := wfActor{a: old, actor: setter.actor, token: setter.token}
	id3 := propose(oldSetter, "cgh.norm_ha_per_machine:MGD:gieo_sa", "80")
	if _, ok := readWF(t, mig, tid, masterdata.ParamApprovalKind, id3); ok {
		t.Fatal("module chưa nối workflow không được ghi yêu cầu")
	}
	if code, b := approver.post("/v1/masterdata/params/versions/"+id3+"/approve", map[string]any{}); code != 200 {
		t.Fatalf("duyệt bản cũ: %d %v", code, b)
	}
	if row, ok := readWF(t, mig, tid, masterdata.ParamApprovalKind, id3); !ok || *row.requester != setter.actor || *row.decider != approver.actor {
		t.Fatalf("lập bù: %+v %v", row, ok)
	}

	// Cấp quyền chia sẻ: đề xuất → yêu cầu; tự duyệt → 403 cũ; quản trị khác duyệt → approved; thu hồi đề xuất → cancelled.
	mkg := ids.New()
	admA, admB := adm.as(tid, authz.RolePlatformAdmin), adm.as(tid, authz.RolePlatformAdmin)
	grant := func() string {
		code, b := admA.post("/v1/share-grants", map[string]any{"grantee_tenant_id": mkg.String(), "kinds": []string{partyport.KindCooperativeDirectory},
			"purpose": "MKG lập lệnh gom rơm theo HTX của chương trình (W6.1)", "valid_to": time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)})
		if code != 201 {
			t.Fatalf("đề xuất cấp quyền: %d %v", code, b)
		}
		return b["id"].(string)
	}
	gid := grant()
	if row, ok := readWF(t, mig, tid, partyport.ShareGrantApprovalKind, gid); !ok || *row.requester != admA.actor {
		t.Fatalf("yêu cầu cấp quyền: %+v %v", row, ok)
	}
	if code, b := admA.post("/v1/share-grants/"+gid+"/approve", nil); code != 403 || b["code"] != "self_approval_forbidden" {
		t.Fatalf("tự duyệt cấp quyền: %d %v", code, b)
	}
	if code, b := admB.post("/v1/share-grants/"+gid+"/approve", nil); code != 201 || b["status"] != "active" {
		t.Fatalf("duyệt cấp quyền: %d %v", code, b)
	}
	if row, _ := readWF(t, mig, tid, partyport.ShareGrantApprovalKind, gid); row.decider == nil || *row.decider != admB.actor || *row.outcome != "approved" {
		t.Fatalf("quyết định cấp quyền: %+v", row)
	}
	// Thu hồi cấp quyền ĐÃ duyệt: không thêm quyết định (quyết định đã có).
	if code, b := admA.post("/v1/share-grants/"+gid+"/revoke", map[string]any{"reason": "Ngừng hợp tác với bên nhận (thử)"}); code != 201 && code != 200 {
		t.Fatalf("thu hồi: %d %v", code, b)
	}
	if row, _ := readWF(t, mig, tid, partyport.ShareGrantApprovalKind, gid); *row.outcome != "approved" {
		t.Fatalf("thu hồi sau duyệt không đổi quyết định: %+v", row)
	}
	gid2 := grant()
	if code, b := admA.post("/v1/share-grants/"+gid2+"/revoke", map[string]any{"reason": "Rút đề xuất trước khi duyệt (thử)"}); code != 201 && code != 200 {
		t.Fatalf("thu hồi đề xuất: %d %v", code, b)
	}
	if row, _ := readWF(t, mig, tid, partyport.ShareGrantApprovalKind, gid2); row.outcome == nil || *row.outcome != "cancelled" || *row.decider != admA.actor {
		t.Fatalf("hủy đề xuất: %+v", row)
	}
}
