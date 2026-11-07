package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func legacyEditBody237(t *testing.T, r *gin.Engine, as, method, path string, body any) any {
	t.Helper()
	if method != "POST" {
		return body
	}
	isEdit := strings.HasSuffix(path, "/correct") || strings.HasSuffix(path, "/redate") || strings.HasSuffix(path, "/delete")
	if !isEdit {
		return body
	}
	base := path[:strings.LastIndex(path, "/")]
	handle := ids.New().String()
	req := httptest.NewRequest("POST", base+"/edit-lease/acquire", strings.NewReader(`{"handle_id":"`+handle+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-As", as)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// Authorization/state-negative legacy tests retain their original expected error even if acquisition is denied.
	if w.Code != 200 {
		return body
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	m, ok := body.(map[string]any)
	if !ok {
		return body
	}
	copy := map[string]any{}
	for k, v := range m {
		copy[k] = v
	}
	copy["edit_lease"] = out["handle_id"]
	return copy
}

type lease237Names struct {
	mu   sync.Mutex
	refs []uuid.UUID
}

func (n *lease237Names) Officers(context.Context, port.Profile, string) ([]port.Officer, error) {
	return nil, nil
}
func (n *lease237Names) EditHolderName(_ context.Context, ref uuid.UUID) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.refs = append(n.refs, ref)
	return "Cán bộ giữ phiếu237", nil
}
func newLease237(t *testing.T) (*review204Env, *lease237Names) {
	e := newReview204(t)
	e.call("tinh", "POST", "/v1/price/tickets/"+e.ticket+"/edit-lease/release", map[string]any{"handle_id": e.lease}, "", 200)
	e.call("admin", "POST", "/v1/price/points/XBG0001/officers", map[string]any{"role": "tinh", "officer_ref": nil, "valid_from": "2026-10-08"}, "", 201)
	names := &lease237Names{}
	e.tickets.Points.Officers = names
	return e, names
}
func (e *review204Env) leaseOp(as, action, handle string) map[string]any {
	return e.call(as, "POST", "/v1/price/tickets/"+e.ticket+"/edit-lease/"+action, map[string]any{"handle_id": handle}, "", 200)
}
func (e *review204Env) withLease(handle string) map[string]any {
	b := e.correction("2", "9000")
	b["edit_lease"] = handle
	return b
}

func TestPriceLease237OwnershipTabsAndEveryWrite(t *testing.T) {
	e, names := newLease237(t)
	path := "/v1/price/tickets/" + e.ticket
	a, b := ids.New().String(), ids.New().String()
	if out := e.leaseOp("tinh", "acquire", a); out["mine"] != true || out["handle_id"] != a {
		t.Fatal(out)
	}
	if out := e.leaseOp("tinh", "acquire", b); out["mine"] != true {
		t.Fatal("same actor tab denied", out)
	}
	denied := e.leaseOp("other", "acquire", ids.New().String())
	if denied["mine"] != false || denied["holder_name"] != "Cán bộ giữ phiếu237" || denied["handle_id"] != nil {
		t.Fatalf("holder/token leak or missing scoped name: %v", denied)
	}
	for _, op := range []string{"correct", "redate", "delete"} {
		before := e.counts()
		body := e.withLease(a)
		body["day"] = "2026-10-07"
		out := e.call("other", "POST", path+"/"+op, body, "", 409)
		if out["code"] != "price.edit_lease_lost" || before != e.counts() {
			t.Fatal(op, out)
		}
	}
	noLease := e.withLease("")
	e.call("tinh", "POST", path+"/correct", noLease, "", 409)
	e.leaseOp("tinh", "release", a)
	if out := e.leaseOp("other", "acquire", ids.New().String()); out["mine"] != false {
		t.Fatal("closing one tab released another", out)
	}
	e.leaseOp("other", "release", b) // foreign release must be harmless
	e.leaseOp("tinh", "renew", b)
	key := "237-correct-replay"
	body := e.withLease(b)
	first := e.raw("tinh", "POST", path+"/correct", body, key)
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	before := e.counts()
	e.leaseOp("tinh", "release", b)
	replay := e.raw("tinh", "POST", path+"/correct", body, key)
	if replay.Code != 201 || replay.Body.String() != first.Body.String() || before != e.counts() {
		t.Fatal("committed write replay broke", replay.Code)
	}
	e.version = 2
	c := ids.New().String()
	if out := e.leaseOp("other", "acquire", c); out["mine"] != true {
		t.Fatal(out)
	}
	e.call("tinh", "POST", path+"/edit-lease/renew", map[string]any{"handle_id": b}, "", 409)
	e.leaseOp("tinh", "release", b)
	if out := e.call("other", "GET", path+"/edit-lease", nil, "", 200); out["mine"] != true {
		t.Fatal("stale release evicted new holder", out)
	}
	stale := e.withLease(c)
	stale["version"] = 1
	if out := e.call("other", "POST", path+"/correct", stale, "", 409); out["code"] != "price.ticket_stale" {
		t.Fatal(out)
	}
	e.leaseOp("other", "release", c)
	c = ids.New().String()
	e.leaseOp("admin", "acquire", c)
	moved := e.withLease(c)
	moved["day"] = "2026-10-07"
	sh := e.call("admin", "POST", path+"/redate", moved, "", 201)
	e.version = sh["ticket"].(map[string]any)["version"]
	e.call("admin", "POST", path+"/delete", e.withLease(c), "", 201)
	e.leaseOp("admin", "release", c) // own cleanup remains possible after soft deletion
	if len(names.refs) == 0 {
		t.Fatal("audited name port was not used")
	}
}

func TestPriceLease237ExpiryAndAtomicRace(t *testing.T) {
	e, _ := newLease237(t)
	a := ids.New().String()
	e.leaseOp("tinh", "acquire", a)
	e.clk.Advance(90 * time.Second)
	e.leaseOp("tinh", "renew", a)
	e.clk.Advance(90 * time.Second)
	if out := e.leaseOp("other", "acquire", ids.New().String()); out["mine"] != false {
		t.Fatal("renew did not extend", out)
	}
	e.clk.Advance(30 * time.Second) // exactly expiry is expired
	e.call("tinh", "POST", "/v1/price/tickets/"+e.ticket+"/correct", e.withLease(a), "", 409)
	start := make(chan struct{})
	type result struct {
		as, handle string
		out        map[string]any
	}
	results := make(chan result, 2)
	for _, as := range []string{"tinh", "other"} {
		go func(as string) {
			<-start
			h := ids.New().String()
			w := e.raw(as, "POST", "/v1/price/tickets/"+e.ticket+"/edit-lease/acquire", map[string]any{"handle_id": h}, "")
			out := map[string]any{}
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			out["http"] = w.Code
			results <- result{as, h, out}
		}(as)
	}
	close(start)
	one, two := <-results, <-results
	if one.out["http"] != 200 || two.out["http"] != 200 || one.out["mine"] == two.out["mine"] {
		t.Fatal("not exactly one atomic winner", one, two)
	}
	winner, loser := one, two
	if one.out["mine"] != true {
		winner, loser = two, one
	}
	before := e.counts()
	e.call(loser.as, "POST", "/v1/price/tickets/"+e.ticket+"/correct", e.withLease(winner.handle), "", 409)
	if before != e.counts() {
		t.Fatal("loser wrote")
	}
	e.call(winner.as, "POST", "/v1/price/tickets/"+e.ticket+"/correct", e.withLease(winner.handle), "", 201)
	var owners, handles int
	if err := e.admin.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM pf_price.ticket_edit_lease WHERE tenant_id=$1 AND ticket_id=$2),(SELECT count(*) FROM pf_price.ticket_edit_handle WHERE tenant_id=$1 AND ticket_id=$2)`, e.tid, e.ticket).Scan(&owners, &handles); err != nil || owners != 1 || handles != 1 {
		t.Fatal("unique owner/expiry cleanup", owners, handles, err)
	}
}

func TestPriceLease237ScopeRLSAndPermissionChange(t *testing.T) {
	e, names := newLease237(t)
	path := "/v1/price/tickets/" + e.ticket
	h := ids.New().String()
	e.leaseOp("tinh", "acquire", h)
	count := len(names.refs)
	e.who["foreign"] = authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), KN: &authz.KNContext{Role: "kn_tinh"}}
	for _, as := range []string{"tw", "foreign"} {
		want := 403
		if as == "foreign" {
			want = 404
		}
		e.call(as, "GET", path+"/edit-lease", nil, "", want)
		e.call(as, "POST", path+"/edit-lease/acquire", map[string]any{"handle_id": ids.New().String()}, "", want)
	}
	if len(names.refs) != count {
		t.Fatal("out-of-scope name lookup")
	}
	e.call("xa", "POST", path+"/edit-lease/acquire", map[string]any{"handle_id": ids.New().String()}, "", 403)
	// Own-commune read resolves only this holder; no token or other directory is disclosed.
	if out := e.call("xa", "GET", path+"/edit-lease", nil, "", 200); out["holder_name"] != "Cán bộ giữ phiếu237" || out["handle_id"] != nil {
		t.Fatal(out)
	}
	for _, tid := range []uuid.UUID{e.tid, e.who["foreign"].TenantID} {
		err := db.WithModuleTenantTx(context.Background(), e.pool, app.DBModule, tid, func(tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM pf_price.ticket_edit_lease`).Scan(&n); err != nil {
				return err
			}
			want := 0
			if tid == e.tid {
				want = 1
			}
			if n != want {
				t.Errorf("RLS rows%d want%d", n, want)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	e.tickets.Points.Officers = nil // this fixture changes assignment independently of the holder-name stub
	e.call("admin", "POST", "/v1/price/points/XBG0001/officers", map[string]any{"role": "tinh", "officer_ref": e.who["other"].ActorRef.String(), "valid_from": "2026-10-08"}, "", 201)
	before := e.counts()
	e.call("tinh", "POST", path+"/correct", e.withLease(h), "", 403)
	e.call("tinh", "POST", path+"/edit-lease/renew", map[string]any{"handle_id": h}, "", 403)
	if before != e.counts() {
		t.Fatal("former permission wrote")
	}
	e.leaseOp("tinh", "release", h)
	e.leaseOp("other", "acquire", h)
	e.tickets.Hooks.Board = func(context.Context, pgx.Tx, time.Time) (string, error) { return domain.BoardApproved, nil }
	e.call("other", "POST", path+"/correct", e.withLease(h), "", 409)
	e.call("other", "POST", path+"/edit-lease/renew", map[string]any{"handle_id": h}, "", 409)
	e.leaseOp("other", "release", h)
}

func TestPriceLease237UsesClockAfterPermissionGate(t *testing.T) {
	e, _ := newLease237(t)
	e.tickets.Hooks.Board = func(context.Context, pgx.Tx, time.Time) (string, error) {
		e.clk.Advance(4 * time.Minute)
		return domain.BoardOpen, nil
	}
	value := e.leaseOp("tinh", "acquire", ids.New().String())
	expires, err := time.Parse(time.RFC3339, value["expires_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(e.clk.Now().Add(app.EditLeaseTTL)) {
		t.Fatalf("lease used time before blocking permission gate: %v", value)
	}
}

// A draft can be deleted in FN-TI-02.04 while FN-TI-02.09 can still submit it.
// The other write path must not bypass that ticket's active editor.
func TestPriceLease237ProxyCannotOverwriteLeasedDraft(t *testing.T) {
	e := newReview204(t)
	body := map[string]any{"point": "XBG0002", "day": "2026-10-08", "version": 0, "submit": false, "reason": "Phiếu nhập thay nháp237", "lines": []map[string]any{{"item_code": "MH00001", "min": "10", "max": "20"}}}
	draft := e.call("tinh", "POST", "/v1/price/tickets/proxy", body, "", 201)
	id := draft["ticket"].(map[string]any)["id"].(string)
	version := draft["ticket"].(map[string]any)["version"]
	handle := ids.New().String()
	path := "/v1/price/tickets/" + id + "/edit-lease/"
	e.call("tinh", "POST", path+"acquire", map[string]any{"handle_id": handle}, "", 200)
	body["version"] = version
	body["lines"] = []map[string]any{{"item_code": "MH00001", "min": "30", "max": "40"}}
	before := e.counts()
	denied := e.call("other", "POST", "/v1/price/tickets/proxy", body, "", 409)
	if denied["code"] != "price.edit_lease_lost" || before != e.counts() {
		t.Fatal("proxy bypassed active draft editor", denied)
	}
	// BR-06 is by person: another tab of the holder can continue the existing proxy workflow.
	own := e.call("tinh", "POST", "/v1/price/tickets/proxy", body, "", 201)
	body["version"] = own["ticket"].(map[string]any)["version"]
	e.call("tinh", "POST", path+"release", map[string]any{"handle_id": handle}, "", 200)
	after := e.call("other", "POST", "/v1/price/tickets/proxy", body, "", 201)
	body["version"] = after["ticket"].(map[string]any)["version"]
	e.call("tinh", "POST", path+"acquire", map[string]any{"handle_id": ids.New().String()}, "", 200)
	e.clk.Advance(app.EditLeaseTTL) // exact expiry must free the draft proxy path too
	e.call("other", "POST", "/v1/price/tickets/proxy", body, "", 201)
}
