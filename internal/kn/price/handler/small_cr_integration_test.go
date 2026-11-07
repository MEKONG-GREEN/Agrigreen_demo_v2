package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

const smallCRDay = "2026-10-12"

type smallCR283 struct {
	t     *testing.T
	env   *pgtest.Env
	tid   uuid.UUID
	clock *clock.Fixed
	svc   *app.Service
	r     *gin.Engine
	xa    [3]uuid.UUID
	n     int
}

func newSmallCR283(t *testing.T) *smallCR283 {
	t.Helper()
	env := pgtest.Get(t)
	f := &smallCR283{t: t, env: env, tid: ids.New(), clock: clock.NewFixed(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)), xa: [3]uuid.UUID{ids.New(), ids.New(), ids.New()}}
	prof := port.Profile{Session: testOwnerSession, Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA", "kn_tw": "TW"}, ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
	var err error
	f.svc, err = app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), f.clock, []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	items, tickets := app.NewItemService(f.svc), app.NewTicketService(f.svc)
	f.svc.Points.Submitted, f.svc.Reminders = tickets.Submitted, app.PointReminders{S: f.svc}
	items.Hooks.HasPrice, items.Hooks.HasSheets = tickets.HasPrice, tickets.Submitted
	tickets.WireBoard()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
		"plain": {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tinh"}},
		"view":  {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}, ViewOnly: true}},
		"tw":    {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tw"}},
		"other": {ActorRef: ids.New(), TenantID: ids.New(), KN: &authz.KNContext{Role: "kn_tinh", Duties: []string{authz.DutyPriceAdmin}}},
	}
	for i, name := range []string{"xaA", "xaB", "xaC"} {
		who[name] = authz.Principal{ActorRef: f.xa[i], TenantID: f.tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}}
	}
	gin.SetMode(gin.TestMode)
	f.r = gin.New()
	f.r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindTestSession(t, p)))
		}
	})
	f.r.Use(idempotency.Middleware(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), db.Ops)))
	g := f.r.Group("/v1")
	Handler{Svc: f.svc}.RegisterApp(g)
	PointHandler{Svc: f.svc}.RegisterApp(g)
	ItemHandler{Svc: items}.RegisterApp(g)
	TicketHandler{Svc: tickets}.RegisterApp(g)
	BoardHandler{Svc: tickets}.RegisterApp(g)
	f.call("admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)
	seedCommunes(t, f.tid, "2025-07-01", "", "CR283A", "CR283B", "CR283C")
	for i, commune := range []string{"CR283A", "CR283B", "CR283C"} {
		f.call("admin", http.MethodPost, "/v1/price/points", map[string]any{"admin_unit_code": commune, "region_code": "an_giang", "valid_from": "2026-10-08"}, http.StatusCreated)
		f.call("admin", http.MethodPost, fmt.Sprintf("/v1/price/points/XBG%04d/officers", i+1), map[string]any{"role": "xa", "officer_ref": f.xa[i], "valid_from": "2026-10-08"}, http.StatusCreated)
	}
	f.call("tinh", http.MethodPost, "/v1/price/items", map[string]any{"name": "Giá thử283", "variety": "Loại 1", "group_code": "rau_mau", "unit_code": "kg", "kind": "basic", "valid_from": "2026-10-08"}, http.StatusCreated)
	f.at("2026-10-09T00:00:00Z")
	return f
}

func (f *smallCR283) at(timestamp string) {
	f.t.Helper()
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		f.t.Fatal(err)
	}
	f.clock.Advance(t.Sub(f.clock.Now()))
}

func (f *smallCR283) request(as, method, path string, body any, key string) *httptest.ResponseRecorder {
	f.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	f.n++
	if key == "" {
		key = fmt.Sprintf("small-cr283-%d", f.n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(b))
	r.Header.Set("X-Test-As", as)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(idempotency.Header, key)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, r)
	return w
}

func (f *smallCR283) call(as, method, path string, body any, want int) map[string]any {
	f.t.Helper()
	w := f.request(as, method, path, body, "")
	if w.Code != want {
		f.t.Fatalf("%s %s %s: got=%d want=%d %s", as, method, path, w.Code, want, w.Body.String())
	}
	return calJSON(f.t, w.Body.Bytes())
}

func (f *smallCR283) save(as, point string, version int, submit bool, price string) string {
	f.t.Helper()
	s := f.call(as, http.MethodPost, "/v1/price/tickets/sheet", map[string]any{"point": point, "version": version, "submit": submit, "lines": []any{map[string]any{"item_code": "MH00001", "min": price, "max": price, "note": "Đã kiểm tra giá"}}}, http.StatusCreated)
	return fmt.Sprint(s["ticket"].(map[string]any)["id"])
}

func (f *smallCR283) board(as string) map[string]any {
	f.t.Helper()
	return f.call(as, http.MethodGet, "/v1/price/boards/"+smallCRDay, nil, http.StatusOK)
}

func (f *smallCR283) op(action string) map[string]any {
	f.t.Helper()
	b := f.board("tinh")
	return f.call("tinh", http.MethodPost, "/v1/price/boards/"+smallCRDay+"/"+action, map[string]any{"seq": b["seq"], "reason": "Đối chiếu283"}, http.StatusCreated)
}

func (f *smallCR283) snapshot() string {
	f.t.Helper()
	var out string
	err := tenant.WithTenantTx(context.Background(), f.env.AdminPool(f.t, migrations.PF), f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT coalesce(string_agg(h, ',' ORDER BY h), '') FROM (
		  SELECT encode(row_hash, 'hex') h FROM pf_price.board UNION ALL SELECT encode(row_hash, 'hex') FROM pf_price.board_line
		  UNION ALL SELECT encode(row_hash, 'hex') FROM pf_price.board_point UNION ALL SELECT encode(row_hash, 'hex') FROM pf_price.board_ticket
		  UNION ALL SELECT encode(row_hash, 'hex') FROM pf_price.ticket UNION ALL SELECT encode(row_hash, 'hex') FROM pf_price.ticket_line
		  UNION ALL SELECT encode(row_hash, 'hex') FROM pf_price.price_alert) h`).Scan(&out)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// FN-TI-02.06 luồng phụ: mọi đường sửa đều làm mất quyền hủy ở DTO,
// POST trực tiếp vẫn bị chặn, phiên bản đã duyệt và chuỗi băm không đổi khi chặn.
func TestPrice283CancelReopenChanges(t *testing.T) {
	for _, change := range []string{"correct", "redate_out", "redate_in", "delete", "proxy", "reopen_alert"} {
		t.Run(change, func(t *testing.T) {
			f := newSmallCR283(t)
			f.save("xaA", "XBG0001", 0, true, "10000")
			oldB := f.save("xaB", "XBG0002", 0, true, "10000")
			f.at("2026-10-09T05:01:00Z")
			f.call("tinh", http.MethodPost, "/v1/price/boards/2026-10-09/approve", map[string]any{"seq": 0}, http.StatusCreated)
			f.at(smallCRDay + "T00:00:00Z")
			id := f.save("xaA", "XBG0001", 0, true, "20000")
			f.at(smallCRDay + "T05:01:00Z")
			f.call("tinh", http.MethodPost, "/v1/price/alerts/decide", map[string]any{"ticket_id": id, "item_code": "MH00001", "decision": "keep", "note": "Giá đã xác nhận"}, http.StatusCreated)
			f.op("approve")
			b := f.op("reopen")
			if b["can"].(map[string]any)["cancel_reopen"] != true {
				t.Fatalf("chưa sửa phải hủy được: %v", b["can"])
			}
			published, _ := json.Marshal(b["published"])
			version := f.call("tinh", http.MethodGet, "/v1/price/tickets/"+id, nil, http.StatusOK)["ticket"].(map[string]any)["version"]
			body := map[string]any{"version": version, "reason": "Đối chiếu sau mở duyệt"}
			switch change {
			case "correct":
				body["lines"] = []any{map[string]any{"item_code": "MH00001", "min": "19000", "max": "19000"}}
				f.call("tinh", http.MethodPost, "/v1/price/tickets/"+id+"/correct", body, http.StatusCreated)
			case "redate_out":
				body["day"] = "2026-10-08"
				f.call("tinh", http.MethodPost, "/v1/price/tickets/"+id+"/redate", body, http.StatusCreated)
			case "redate_in":
				f.call("tinh", http.MethodPost, "/v1/price/boards/2026-10-09/reopen", map[string]any{"seq": 1, "reason": "Cho phép chuyển ngày phiếu"}, http.StatusCreated)
				f.call("tinh", http.MethodPost, "/v1/price/tickets/"+oldB+"/redate", map[string]any{"version": 1, "reason": "Đổi về ngày cần sửa", "day": smallCRDay}, http.StatusCreated)
			case "delete":
				f.call("tinh", http.MethodPost, "/v1/price/tickets/"+id+"/delete", body, http.StatusCreated)
			case "proxy":
				f.call("tinh", http.MethodPost, "/v1/price/tickets/proxy", map[string]any{"point": "XBG0002", "day": smallCRDay, "version": 0, "submit": true, "reason": "Nhập thay xã", "lines": []any{map[string]any{"item_code": "MH00001", "min": "10000", "max": "10000"}}}, http.StatusCreated)
			case "reopen_alert":
				f.call("tinh", http.MethodPost, "/v1/price/alerts/decide", map[string]any{"ticket_id": id, "item_code": "MH00001", "decision": "reopen", "note": "Rà soát lại"}, http.StatusCreated)
			}
			b = f.board("tinh")
			if b["can"].(map[string]any)["cancel_reopen"] != false {
				t.Fatalf("FN-TI-02.06 đã sửa phải vô hiệu Hủy mở duyệt: %v", b["can"])
			}
			before := f.snapshot()
			for i := 0; i < 2; i++ {
				w := f.request("tinh", http.MethodPost, "/v1/price/boards/"+smallCRDay+"/cancel-reopen", map[string]any{"seq": b["seq"]}, "283-denied-cancel")
				if w.Code != http.StatusConflict || calJSON(t, w.Body.Bytes())["code"] != "price.board_changed" {
					t.Fatalf("phải chặn hủy sau sửa: %d %s", w.Code, w.Body.String())
				}
			}
			if f.snapshot() != before {
				t.Fatal("lệnh bị chặn làm thay đổi sổ / chuỗi băm")
			}
			got, _ := json.Marshal(f.board("tinh")["published"])
			if string(got) != string(published) {
				t.Fatal("thay đổi phiên bản đã duyệt khi chưa duyệt lại")
			}
		})
	}
}

func TestPrice283CancelUnchangedPermissionsAndReplay(t *testing.T) {
	f := newSmallCR283(t)
	f.at(smallCRDay + "T00:00:00Z")
	f.save("xaA", "XBG0001", 0, true, "10000")
	f.at(smallCRDay + "T05:01:00Z")
	f.op("approve")
	b := f.op("reopen")
	for _, as := range []string{"plain", "view", "admin", "xaA", "tw", "other"} {
		if f.board(as)["can"].(map[string]any)["cancel_reopen"] != false {
			t.Fatalf("%s được quyền hủy", as)
		}
	}
	before := f.snapshot()
	for _, as := range []string{"plain", "view", "admin", "xaA", "tw"} {
		f.call(as, http.MethodPost, "/v1/price/boards/"+smallCRDay+"/cancel-reopen", map[string]any{"seq": b["seq"]}, http.StatusForbidden)
	}
	if f.snapshot() != before {
		t.Fatal("yêu cầu không có quyền đã ghi sổ")
	}
	body := map[string]any{"seq": b["seq"]}
	first := f.request("tinh", http.MethodPost, "/v1/price/boards/"+smallCRDay+"/cancel-reopen", body, "283-unchanged-cancel")
	if first.Code != http.StatusCreated || calJSON(t, first.Body.Bytes())["state"] != "approved" {
		t.Fatalf("hủy chưa sửa: %d %s", first.Code, first.Body.String())
	}
	after := f.snapshot()
	replay := f.request("tinh", http.MethodPost, "/v1/price/boards/"+smallCRDay+"/cancel-reopen", body, "283-unchanged-cancel")
	if replay.Code != first.Code || !bytes.Equal(replay.Body.Bytes(), first.Body.Bytes()) || f.snapshot() != after {
		t.Fatal("replay hủy chưa sửa không giữ phản hồi/sổ")
	}
}

func (f *smallCR283) reminders() []port.ReminderIssuedV1 {
	f.t.Helper()
	var raw []string
	err := tenant.WithTenantTx(context.Background(), f.env.AdminPool(f.t, migrations.PF), f.tid, func(tx pgx.Tx) error {
		r, err := tx.Query(context.Background(), `SELECT payload::text FROM shared.outbox WHERE tenant_id=$1 AND event_type=$2 ORDER BY occurred_at, id`, f.tid, port.EventReminderIssued)
		if err != nil {
			return err
		}
		raw, err = pgx.CollectRows(r, pgx.RowTo[string])
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	out := make([]port.ReminderIssuedV1, 0, len(raw))
	for _, s := range raw {
		var p port.ReminderIssuedV1
		if err := json.Unmarshal([]byte(s), &p); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// FN-XA-03.04 BR-01/02, AC-01/02: 07:30 và 10:30 chỉ nhắc xã chưa gửi;
// lọc trước khi tạo outbox cho cả chuông và email, không nhầm ngày/tenant, không trùng khi job lặp.
func TestPrice283Reminder0730SubmittedExcluded(t *testing.T) {
	f := newSmallCR283(t)
	f.save("xaC", "XBG0003", 0, true, "10000") // đã gửi ngày khác vẫn cần nhắc hôm nay
	f.call("admin", http.MethodPost, "/v1/price/calendar/schedule", map[string]any{"weekdays": []string{"mon", "tue", "wed", "thu", "fri"}, "lock_at": "12:00", "respond_by": "13:30", "remind1_at": "07:30", "remind2_at": "10:30", "channels": []string{"in_app", "email"}, "template": "Nhập giá {ngày} cho {xã} trước {giờ khóa}", "valid_from": smallCRDay}, http.StatusCreated)
	f.at(smallCRDay + "T00:00:00Z")
	f.save("xaA", "XBG0001", 0, true, "10000")
	f.save("xaB", "XBG0002", 0, false, "10000")
	job := func(at string) app.CalendarJobResult {
		t.Helper()
		f.at(at)
		r, err := f.svc.CalendarJobTenant(context.Background(), f.tid, f.clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := job(smallCRDay + "T00:29:59Z"); r != (app.CalendarJobResult{}) {
		t.Fatalf("chưa tới 07:30: %+v", r)
	}
	if r := job(smallCRDay + "T00:30:20Z"); r.Reminders != 1 || r.Events != 2 {
		t.Fatalf("07:30 phải bỏ xã Đã gửi, giữ Bản nháp/Chưa nhập: %+v", r)
	}
	if r := job(smallCRDay + "T00:30:40Z"); r != (app.CalendarJobResult{}) {
		t.Fatalf("nhắc trùng lần 1: %+v", r)
	}
	ps := f.reminders()
	if len(ps) != 2 {
		t.Fatalf("outbox lần 1: %d", len(ps))
	}
	seen := map[string]bool{}
	for _, p := range ps {
		if p.Round != 1 || p.Day != smallCRDay || !p.InApp || !p.Email || len(p.Communes) != 1 || len(p.RecipientRefs) != 1 {
			t.Fatalf("ngày/kênh/người nhận lần 1: %+v", p)
		}
		code := p.Communes[0]
		if code == "XBG0002" && p.RecipientRefs[0] == f.xa[1] || code == "XBG0003" && p.RecipientRefs[0] == f.xa[2] {
			seen[code] = true
		} else {
			t.Fatalf("xã đã gửi hoặc người nhận sai được đưa vào outbox: %+v", p)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("người nhận lần 1 bị trùng: %v", seen)
	}
	f.at(smallCRDay + "T03:29:00Z")
	f.save("xaB", "XBG0002", 1, true, "10000")
	if r := job(smallCRDay + "T03:30:15Z"); r.Reminders != 1 || r.Events != 1 {
		t.Fatalf("10:30 chỉ nhắc xã Chưa nhập: %+v", r)
	}
	if r := job(smallCRDay + "T03:30:40Z"); r != (app.CalendarJobResult{}) {
		t.Fatalf("nhắc trùng lần 2: %+v", r)
	}
	ps = f.reminders()
	if len(ps) != 3 || ps[2].Round != 2 || len(ps[2].RecipientRefs) != 1 || ps[2].RecipientRefs[0] != f.xa[2] {
		t.Fatalf("outbox 10:30: %+v", ps)
	}
	if r := job("2026-10-17T00:30:20Z"); r != (app.CalendarJobResult{}) || len(f.reminders()) != 3 {
		t.Fatalf("không nhắc ngoài ngày báo giá: %+v", r)
	}
	other := newSmallCR283(t)
	other.at(smallCRDay + "T00:30:20Z")
	if r, err := other.svc.CalendarJobTenant(context.Background(), other.tid, other.clock.Now()); err != nil || r.Events != 3 {
		t.Fatalf("tenant khác không được dùng trạng thái đã gửi của tenant trước: %+v %v", r, err)
	}
	for _, p := range other.reminders() {
		for _, ref := range p.RecipientRefs {
			if ref == f.xa[0] || ref == f.xa[1] || ref == f.xa[2] {
				t.Fatal("lẫn người nhận sang tenant khác")
			}
		}
	}
	if len(f.reminders()) != 3 {
		t.Fatal("job tenant khác ghi vào tenant gốc")
	}
}
