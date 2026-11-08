package handler

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type weeklyHTTP284 struct {
	router *gin.Engine
	svc    *app.WeeklyService
	tid    uuid.UUID
	who    map[string]authz.Principal
}

func weeklyFixture284(t *testing.T) weeklyHTTP284 {
	t.Helper()
	env := pgtest.Get(t)
	tid := ids.New()
	prof := port.Profile{Name: "kn", Department: "thong_tin_truyen_thong", Session: testOwnerSession, Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA", "kn_tw": "TW"}, ManageBy: []string{"admin"}}
	s, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clock.NewFixed(time.Date(2027, 2, 18, 4, 0, 0, 0, time.UTC)), []port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	who := map[string]authz.Principal{}
	for name, role := range map[string]string{"tinh": "kn_tinh", "tinh2": "kn_tinh", "xa": "kn_xa", "tw": "kn_tw", "admin": "admin", "support": "kn_tinh", "other": "kn_tinh", "wrongdept": "kn_tinh", "readonly": "kn_tinh", "support_nomfa": "kn_tinh"} {
		k := &authz.KNContext{Role: role, Department: "thong_tin_truyen_thong", Scope: authz.Scope{All: true}}
		if role == "admin" {
			k.Department = ""
		}
		if role == "kn_xa" {
			k.OrgRef = ids.New()
		}
		if name == "wrongdept" {
			k.Department = "hanh_chinh_tong_hop"
		}
		if name == "readonly" {
			k.ViewOnly = true
		}
		if name == "support" || name == "support_nomfa" {
			k.Support = &authz.KNSupport{SessionID: ids.New(), Reason: "Hỗ trợ lập báo cáo tuần theo yêu cầu"}
		}
		p := authz.Principal{ActorRef: ids.New(), TenantID: tid, KN: k, MFA: name == "support"}
		if name == "other" {
			p.TenantID = ids.New()
		}
		who[name] = bindTestSession(t, p)
	}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	w := app.NewWeeklyService(app.NewTicketService(s))
	WeeklyHandler{Svc: w}.RegisterApp(r.Group("/v1"))
	return weeklyHTTP284{r, w, tid, who}
}

func (e weeklyHTTP284) request(as, method, path, key string, body []byte, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("X-Test-As", as)
	req.Header.Set("Content-Type", contentType)
	if key == "" {
		key = ids.New().String()
	}
	req.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e weeklyHTTP284) call(t *testing.T, as, program string, input *app.WeeklyCommand, selected string, want int, key string) app.WeeklyView {
	t.Helper()
	method, path := http.MethodGet, "/v1/price/weekly-programs/"+program
	var body []byte
	if input != nil {
		method = http.MethodPost
		path += "/actions"
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
	} else if selected != "" {
		path += "?week_id=" + selected
	}
	w := e.request(as, method, path, key, body, "application/json")
	if w.Code != want {
		t.Fatalf("%s %s %s: status %d want %d: %s", as, method, path, w.Code, want, w.Body.String())
	}
	var v app.WeeklyView
	if want == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

func weeklyCommand284(v app.WeeklyView, action string) app.WeeklyCommand {
	return app.WeeklyCommand{Version: v.Version, WeekID: v.Report.ID, Action: action}
}
func ptr284(s string) *string { return &s }
func findWeekly284(t *testing.T, v app.WeeklyView, name string) domain.WeeklyRow {
	t.Helper()
	for _, r := range v.Report.Rows {
		if strings.Contains(r.Name, name) {
			return r
		}
	}
	t.Fatalf("missing row %s", name)
	return domain.WeeklyRow{}
}
func assertDecimal284(t *testing.T, d decimal.Decimal, want string) {
	t.Helper()
	if d.String() != want {
		t.Fatalf("amount %s want %s", d, want)
	}
}

func TestWeeklyACArithmeticStateAudit284(t *testing.T) {
	e := weeklyFixture284(t)
	v := e.call(t, "tinh", "training", nil, "", 200, "")
	if len(v.Report.Rows) != 6 || v.Report.Status != "draft" || v.Report.To == "2026-11-08" || v.PlanYear != 2026 {
		t.Fatalf("initial catalog/calendar: %+v", v)
	}
	farmers := findWeekly284(t, v, "nông dân chủ chốt")
	in := weeklyCommand284(v, "save")
	in.Rows = []domain.WeeklyPatch{{ID: farmers.ID, Progress: ptr284("10"), Money: ptr284("803458000")}}
	v = e.call(t, "tinh", "training", &in, "", 200, "")
	in = weeklyCommand284(v, "close")
	e.call(t, "tinh", "training", &in, "", 409, "")
	in.Confirm = true
	v = e.call(t, "tinh", "training", &in, "", 200, "")
	firstID, closedVersion := v.Report.ID, v.Version
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	var beforeHash, beforePayload []byte
	if err := admin.QueryRow(context.Background(), `SELECT row_hash,payload::text FROM pf_price.weekly_program WHERE tenant_id=$1 AND program='training' AND seq=$2`, e.tid, closedVersion).Scan(&beforeHash, &beforePayload); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"save", "add", "delete", "adjust", "close"} {
		denied := weeklyCommand284(v, action)
		denied.Confirm = true
		e.call(t, "tinh", "training", &denied, "", 409, "")
	}
	in = weeklyCommand284(v, "new")
	v = e.call(t, "tinh", "training", &in, "", 200, "")
	row := findWeekly284(t, v, "nông dân chủ chốt")
	assertDecimal284(t, row.OpeningProgress, "10")
	assertDecimal284(t, row.OpeningMoney, "803458000")
	in = weeklyCommand284(v, "save")
	in.Rows = []domain.WeeklyPatch{{ID: row.ID, Progress: ptr284("2"), Money: ptr284("10000000")}}
	v = e.call(t, "tinh", "training", &in, "", 200, "")
	row = findWeekly284(t, v, "nông dân chủ chốt")
	assertDecimal284(t, row.CumulativeProgress, "12")
	assertDecimal284(t, row.CumulativeMoney, "813458000")
	assertDecimal284(t, v.Report.TotalMoney, "813458000")
	if row.Rate == nil || !row.Rate.Equal(decimal.NewFromInt(100)) || !row.Achieved || v.Report.Achieved != 1 {
		t.Fatal("AC01 immediate 100% projection")
	}
	in = weeklyCommand284(v, "save")
	in.Rows = []domain.WeeklyPatch{{ID: row.ID, MoneyOverride: ptr284("900000000")}}
	v = e.call(t, "support", "training", &in, "", 200, "")
	in = weeklyCommand284(v, "close")
	in.Confirm = true
	v = e.call(t, "tinh", "training", &in, "", 200, "")
	in = weeklyCommand284(v, "new")
	v = e.call(t, "tinh", "training", &in, "", 200, "")
	row = findWeekly284(t, v, "nông dân chủ chốt")
	assertDecimal284(t, row.OpeningMoney, "900000000")
	if row.MoneyOverride != nil {
		t.Fatal("override must reset at new week")
	}
	in = weeklyCommand284(v, "save")
	in.Rows = []domain.WeeklyPatch{{ID: row.ID, Money: ptr284("10000000")}}
	v = e.call(t, "tinh", "training", &in, "", 200, "")
	assertDecimal284(t, v.Report.TotalMoney, "910000000")
	// Deleting from the current draft changes projections of old weeks but never their ledger bytes.
	in = weeklyCommand284(v, "delete")
	in.ItemID = row.ID
	e.call(t, "tinh", "training", &in, "", 409, "")
	in.Confirm = true
	v = e.call(t, "tinh", "training", &in, "", 200, "")
	assertDecimal284(t, v.Report.TotalMoney, "0")
	old := e.call(t, "tinh", "training", nil, firstID, 200, "")
	if len(old.Report.Rows) != 5 {
		t.Fatal("tombstone must apply to old-week projection")
	}
	assertDecimal284(t, old.Report.TotalMoney, "0")
	var afterHash, afterPayload []byte
	if err := admin.QueryRow(context.Background(), `SELECT row_hash,payload::text FROM pf_price.weekly_program WHERE tenant_id=$1 AND program='training' AND seq=$2`, e.tid, closedVersion).Scan(&afterHash, &afterPayload); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeHash, afterHash) || !bytes.Equal(beforePayload, afterPayload) {
		t.Fatal("closed ledger hash or payload changed")
	}
	if len(v.Log) != int(v.Version) || v.Log[0].Actor != e.who["tinh"].ActorRef || v.Log[0].At.IsZero() || v.Log[0].Action != "delete" {
		t.Fatal("complete actor/time audit required")
	}
	// AC02: adjusted plan overrides the decision's original scale.
	info := e.call(t, "tinh", "information", nil, "", 200, "")
	price := findWeekly284(t, info, "Thu thập")
	in = weeklyCommand284(info, "adjust")
	in.ItemID = price.ID
	in.Plan = "3822"
	in.Reason = "Điều chỉnh số phiếu theo kế hoạch được duyệt"
	info = e.call(t, "tinh", "information", &in, "", 200, "")
	in = weeklyCommand284(info, "save")
	in.Rows = []domain.WeeklyPatch{{ID: price.ID, Progress: ptr284("1934")}}
	info = e.call(t, "tinh", "information", &in, "", 200, "")
	price = findWeekly284(t, info, "Thu thập")
	assertDecimal284(t, *price.Rate, "50.6")
	assertDecimal284(t, price.Plan, "6552")
}

func TestWeeklyPermissionsNumbersGroups284(t *testing.T) {
	e := weeklyFixture284(t)
	e.call(t, "", "information", nil, "", 401, "")
	v := e.call(t, "tinh", "information", nil, "", 200, "")
	for _, as := range []string{"xa", "tw"} {
		denied := e.call(t, as, "information", nil, "", 200, "")
		if denied.Report != nil || denied.Version != 0 || len(denied.Log) != 0 || len(denied.Reports) != 0 {
			t.Fatal("AC07 data leaked to", as)
		}
		in := weeklyCommand284(v, "save")
		e.call(t, as, "information", &in, "", 403, "")
	}
	for _, as := range []string{"admin", "wrongdept", "readonly", "support_nomfa"} {
		e.call(t, as, "information", nil, "", 403, "")
	}
	foreign := e.call(t, "other", "information", nil, "", 200, "")
	if foreign.Report.ID == v.Report.ID {
		t.Fatal("tenant isolation")
	}
	in := weeklyCommand284(v, "save")
	e.call(t, "other", "information", &in, "", 404, "")
	for _, bad := range []string{"-1", "not a number", "1e100000", "1e-100000", strings.Repeat("9", 100000), "NaN", "Infinity"} {
		in = weeklyCommand284(v, "save")
		in.Rows = []domain.WeeklyPatch{{ID: v.Report.Rows[1].ID, Progress: &bad}}
		e.call(t, "tinh", "information", &in, "", 422, "")
	}
	in = weeklyCommand284(v, "save")
	in.Rows = []domain.WeeklyPatch{{ID: v.Report.Rows[0].ID, Progress: ptr284("1")}}
	e.call(t, "tinh", "information", &in, "", 422, "")
	in = weeklyCommand284(v, "add")
	in.Item = &app.WeeklyNewItem{Name: "Mục bổ sung có quy mô 0 (lớp)", Plan: "0"}
	v = e.call(t, "tinh", "information", &in, "", 200, "")
	zero := findWeekly284(t, v, "Mục bổ sung")
	if zero.Rate != nil {
		t.Fatal("zero scale no division")
	}
	in = weeklyCommand284(v, "add")
	in.Item = &app.WeeklyNewItem{Name: "Mục con bổ sung (lớp)", Plan: "2", ParentID: zero.ID}
	v = e.call(t, "tinh", "information", &in, "", 200, "")
	if !findWeekly284(t, v, "Mục bổ sung").Group {
		t.Fatal("empty leaf must become group")
	}
	child := findWeekly284(t, v, "Mục con bổ sung")
	in = weeklyCommand284(v, "save")
	in.Rows = []domain.WeeklyPatch{{ID: child.ID, Progress: ptr284("1")}}
	v = e.call(t, "tinh", "information", &in, "", 200, "")
	in = weeklyCommand284(v, "add")
	in.Item = &app.WeeklyNewItem{Name: "Không được có cháu (lớp)", Plan: "1", ParentID: child.ID}
	e.call(t, "tinh", "information", &in, "", 422, "")
	in = weeklyCommand284(v, "delete")
	in.ItemID = zero.ID
	e.call(t, "tinh", "information", &in, "", 409, "")
	in.Confirm = true
	v = e.call(t, "tinh", "information", &in, "", 200, "")
	for _, r := range v.Report.Rows {
		if r.ID == zero.ID || r.ID == child.ID {
			t.Fatal("group deletion must cascade")
		}
	}
	number := 0
	for _, r := range v.Report.Rows {
		if r.ParentID == "" {
			number++
			if r.Number != fmt.Sprint(number) {
				t.Fatal("main item renumbering")
			}
		}
	}
	// r_price has tenant RLS and no UPDATE permission; no role can edit the immutable ledger row.
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	var canUpdate bool
	if err := admin.QueryRow(context.Background(), `SELECT has_table_privilege('r_price','pf_price.weekly_program','UPDATE')`).Scan(&canUpdate); err != nil || canUpdate {
		t.Fatal("module write grant", err, canUpdate)
	}
	if _, err := admin.Exec(context.Background(), `UPDATE pf_price.weekly_program SET action='save' WHERE tenant_id=$1`, e.tid); err == nil {
		t.Fatal("ledger accepted direct UPDATE")
	}
	ctx := authz.WithPrincipal(context.Background(), e.who["other"])
	if err := db.WithModuleTenantTx(ctx, e.svc.DB, app.DBModule, e.who["other"].TenantID, func(tx pgx.Tx) error {
		var n int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_price.weekly_program WHERE tenant_id=$1`, e.tid).Scan(&n)
		if n != 0 {
			t.Fatal("RLS foreign rows", n)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWeeklyConcurrentCASReplay284(t *testing.T) {
	for _, pair := range [][2]string{{"save", "save"}, {"close", "save"}, {"close", "delete"}, {"new", "new"}} {
		t.Run(pair[0]+"_"+pair[1], func(t *testing.T) {
			e := weeklyFixture284(t)
			v := e.call(t, "tinh", "training", nil, "", 200, "")
			if pair[0] == "new" {
				in := weeklyCommand284(v, "close")
				in.Confirm = true
				v = e.call(t, "tinh", "training", &in, "", 200, "")
			}
			start := make(chan struct{})
			results := make(chan int, 2)
			var wg sync.WaitGroup
			for j, action := range pair {
				wg.Add(1)
				go func(j int, action string) {
					defer wg.Done()
					in := weeklyCommand284(v, action)
					in.Confirm = true
					if action == "save" {
						in.Rows = []domain.WeeklyPatch{{ID: v.Report.Rows[0].ID, Progress: ptr284(fmt.Sprint(j + 1))}}
					}
					if action == "delete" {
						in.ItemID = v.Report.Rows[0].ID
					}
					data, _ := json.Marshal(in)
					<-start
					as := "tinh"
					if j == 1 {
						as = "tinh2"
					}
					w := e.request(as, http.MethodPost, "/v1/price/weekly-programs/training/actions", "", data, "application/json")
					results <- w.Code
				}(j, action)
			}
			close(start)
			wg.Wait()
			close(results)
			statuses := []int{}
			for code := range results {
				statuses = append(statuses, code)
			}
			slices.Sort(statuses)
			if !slices.Equal(statuses, []int{200, 409}) {
				t.Fatalf("atomic one-winner: %v", statuses)
			}
			after := e.call(t, "tinh", "training", nil, "", 200, "")
			if after.Version != v.Version+1 {
				t.Fatal("concurrent CAS wrote more than one event")
			}
		})
	}
	e := weeklyFixture284(t)
	v := e.call(t, "tinh", "information", nil, "", 200, "")
	in := weeklyCommand284(v, "save")
	in.Rows = []domain.WeeklyPatch{{ID: v.Report.Rows[1].ID, Money: ptr284("25")}}
	key := ids.New().String()
	first := e.call(t, "tinh", "information", &in, "", 200, key)
	replay := e.call(t, "tinh", "information", &in, "", 200, key)
	if first.Version != replay.Version {
		t.Fatal("idempotent replay appended an event")
	}
	e.call(t, "xa", "information", &in, "", 403, key)
	e.call(t, "tinh2", "information", &in, "", 409, key)
	changed := in
	changed.Rows = []domain.WeeklyPatch{{ID: v.Report.Rows[1].ID, Money: ptr284("26")}}
	e.call(t, "tinh", "information", &changed, "", 409, key)
	e.call(t, "tinh", "information", &in, "", 409, "")
}

func weeklyCSV284(t *testing.T, rows [][]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(rows); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (e weeklyHTTP284) upload(t *testing.T, as, program, action, key string, v app.WeeklyView, data []byte, sha string, confirm bool, want int) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, value := range map[string]string{"version": fmt.Sprint(v.Version), "week_id": v.Report.ID, "sha256": sha, "confirm_overwrite": fmt.Sprint(confirm)} {
		if err := writer.WriteField(k, value); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.CreateFormFile("file", "bao-cao.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	w := e.request(as, http.MethodPost, "/v1/price/weekly-programs/"+program+"/imports/"+action, key, body.Bytes(), writer.FormDataContentType())
	if want != 0 && w.Code != want {
		t.Fatalf("import %s: %d want %d: %s", action, w.Code, want, w.Body.String())
	}
	return w
}

func TestWeeklyExcelPartialTemplateExport284(t *testing.T) {
	e := weeklyFixture284(t)
	v := e.call(t, "tinh", "information", nil, "", 200, "")
	leaf := v.Report.Rows[1]
	in := weeklyCommand284(v, "save")
	in.Rows = []domain.WeeklyPatch{{ID: leaf.ID, Progress: ptr284("1"), Money: ptr284("50")}}
	v = e.call(t, "tinh", "information", &in, "", 200, "")
	data := weeklyCSV284(t, [][]string{{"Tên hạng mục", "Tiến độ tuần", "Kinh phí tuần", "Đánh giá tuần", "Lũy kế"},
		{leaf.Name, "5", "", "Hoàn thành trong tuần", "1e100000"},
		{v.Report.Rows[2].Name, "-1", "2", "", ""}, {v.Report.Rows[3].Name, "not-number", "3", "", ""}, {"Tên chưa có", "1", "1", "", ""},
		{v.Report.Rows[0].Name, "1", "1", "", ""}, {v.Report.Rows[4].Name, "2", "3", "", ""}, {v.Report.Rows[4].Name, "3", "4", "", ""},
		{v.Report.Rows[6].Name, "1e100000", "1", "", ""}, {v.Report.Rows[7].Name, "1e-100000", "1", "", ""},
	})
	w := e.upload(t, "tinh", "information", "preview", "", v, data, "", false, 200)
	var preview app.WeeklyPreview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.ValidRows != 1 || preview.ErrorRows != 8 || preview.OverwriteRows != 1 {
		t.Fatalf("AC05 preview %+v", preview)
	}
	e.upload(t, "tinh", "information", "confirm", "", v, data, preview.SHA256, false, 409)
	key := ids.New().String()
	w = e.upload(t, "tinh", "information", "confirm", key, v, data, preview.SHA256, true, 200)
	var imported app.WeeklyView
	if err := json.Unmarshal(w.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	row := findWeekly284(t, imported, "Truyền hình")
	assertDecimal284(t, row.Progress, "5")
	assertDecimal284(t, row.Money, "50")
	if row.Evaluation != "Hoàn thành trong tuần" {
		t.Fatal("evaluation not imported")
	}
	if len(imported.Report.ImportIssues) != 9 {
		t.Fatal("error rows not retained")
	}
	w = e.upload(t, "tinh", "information", "confirm", key, v, data, preview.SHA256, true, 200)
	var replay app.WeeklyView
	if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Version != imported.Version {
		t.Fatal("import replay wrote again")
	}
	e.upload(t, "xa", "information", "confirm", key, v, data, preview.SHA256, true, 403)
	e.upload(t, "tinh", "information", "confirm", "", imported, append(data, 'x'), preview.SHA256, true, 409)
	ctx := authz.WithPrincipal(context.Background(), e.who["tinh"])
	x, err := e.svc.ExportWeekly(ctx, "kn", "information", imported.Report.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(x.Body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	merges, err := f.GetMergeCells("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	ranges := []string{}
	for _, m := range merges {
		ranges = append(ranges, m.GetStartAxis()+":"+m.GetEndAxis())
	}
	for _, want := range []string{"D4:E4", "G4:H4", "A4:A5"} {
		if !slices.Contains(ranges, want) {
			t.Fatal("two-tier merge missing", want, ranges)
		}
	}
	title, err := f.GetCellValue("Sheet1", "A2")
	if err != nil || !strings.Contains(title, imported.Report.To) || !strings.Contains(title, fmt.Sprint(imported.Report.Number)) {
		t.Fatal("selected period title", title, err)
	}
	value, err := f.GetCellValue("Sheet1", "D7")
	if err != nil || value != "5" {
		t.Fatal("export weekly value", value, err)
	}
	style, err := f.GetCellStyle("Sheet1", "D7")
	if err != nil || style == 0 {
		t.Fatal("border style", style, err)
	}
	template, err := e.svc.ExportWeekly(ctx, "kn", "information", imported.Report.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	tf, err := excelize.OpenReader(bytes.NewReader(template.Body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tf.Close() }()
	for _, cell := range []string{"D7", "E7", "F7", "G7", "H7", "I7"} {
		value, err := tf.GetCellValue("Sheet1", cell)
		if err != nil || value != "" {
			t.Fatal("template must clear data", cell, value, err)
		}
	}
	// Filled exported template is accepted; formula in an ignored cumulative column is not evaluated.
	if err := tf.SetCellDefault("Sheet1", "D7", "6"); err != nil {
		t.Fatal(err)
	}
	if err := tf.SetCellFormula("Sheet1", "G7", "1/0"); err != nil {
		t.Fatal(err)
	}
	buf, err := tf.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	w = e.upload(t, "tinh", "information", "preview", "", imported, buf.Bytes(), "", false, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.ValidRows != 17 || preview.ErrorRows != 2 {
		t.Fatalf("filled template and group row errors: %+v", preview)
	}
	issues, err := e.svc.ExportIssues(ctx, "kn", "information", imported.Report.ID)
	if err != nil {
		t.Fatal(err)
	}
	if issues.Rows != 8 {
		t.Fatal("downloadable error list", issues.Rows)
	}
	// Closed-state import is rejected by the service even with a valid old preview.
	in = weeklyCommand284(imported, "close")
	in.Confirm = true
	closed := e.call(t, "tinh", "information", &in, "", 200, "")
	e.upload(t, "tinh", "information", "confirm", "", closed, data, preview.SHA256, true, 409)
}

func TestWeeklyConcurrentOpenImportClose284(t *testing.T) {
	e := weeklyFixture284(t)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			<-start
			results <- e.request("tinh", http.MethodGet, "/v1/price/weekly-programs/training", "", nil, "")
		}()
	}
	close(start)
	a, b := <-results, <-results
	if a.Code != 200 || b.Code != 200 {
		t.Fatalf("concurrent open: %d/%d", a.Code, b.Code)
	}
	var first, second app.WeeklyView
	if err := json.Unmarshal(a.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 1 || first.Report.ID != second.Report.ID {
		t.Fatal("concurrent open created duplicate first week")
	}
	data := weeklyCSV284(t, [][]string{{"Tên hạng mục", "Tiến độ tuần", "Kinh phí tuần"}, {first.Report.Rows[0].Name, "1", "100"}})
	previewResponse := e.upload(t, "tinh", "training", "preview", "", first, data, "", false, 200)
	var preview app.WeeklyPreview
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	start = make(chan struct{})
	statuses := make(chan int, 2)
	go func() {
		<-start
		in := weeklyCommand284(first, "close")
		in.Confirm = true
		body, _ := json.Marshal(in)
		statuses <- e.request("tinh", http.MethodPost, "/v1/price/weekly-programs/training/actions", "", body, "application/json").Code
	}()
	go func() {
		<-start
		statuses <- e.upload(t, "tinh2", "training", "confirm", "", first, data, preview.SHA256, true, 0).Code
	}()
	close(start)
	codes := []int{<-statuses, <-statuses}
	slices.Sort(codes)
	if !slices.Equal(codes, []int{200, 409}) {
		t.Fatal("close/import atomic winner", codes)
	}
	latest := e.call(t, "tinh", "training", nil, "", 200, "")
	if latest.Report.Status == "draft" {
		in := weeklyCommand284(latest, "close")
		in.Confirm = true
		latest = e.call(t, "tinh", "training", &in, "", 200, "")
	}
	denied := e.upload(t, "tinh", "training", "confirm", "", latest, data, preview.SHA256, true, 409)
	if !strings.Contains(denied.Body.String(), "price.weekly_closed") {
		t.Fatal("closed import must fail on state, not file hash", denied.Body.String())
	}
}

func TestWeeklyDetachedEvaluationHTTP284(t *testing.T) {
	e := weeklyFixture284(t)
	v := e.call(t, "tinh", "training", nil, "", 200, "")
	leaf := v.Report.Rows[0]
	n := v.Report.Number
	data := weeklyCSV284(t, [][]string{{"Tên hạng mục", fmt.Sprintf("Tiến độ thực hiện tuần %d", n), fmt.Sprintf("Kinh phí tuần %d", n), fmt.Sprintf("Tiến độ thực hiện tuần %d", n+1), fmt.Sprintf("Kinh phí tuần %d", n+1), fmt.Sprintf("Đánh giá tuần %d", n), fmt.Sprintf("Đánh giá tuần %d", n+1), "Lũy kế"}, {leaf.Name, "2", "100", "3", "200", "Fixture đánh giá đúng kỳ", "Fixture thuộc kỳ sau", "1e100000"}})
	w := e.upload(t, "tinh", "training", "preview", "", v, data, "", false, 200)
	var preview app.WeeklyPreview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.ValidRows != 1 || preview.ErrorRows != 0 || !strings.Contains(preview.SourceColumns, fmt.Sprintf("Đánh giá tuần %d", n)) {
		t.Fatalf("detached evaluation preview: %+v", preview)
	}
	untouched := e.call(t, "tinh", "training", nil, "", 200, "")
	if untouched.Version != v.Version || untouched.Report.Rows[0].Evaluation != "" {
		t.Fatal("preview wrote data")
	}
	w = e.upload(t, "tinh", "training", "confirm", "", v, data, preview.SHA256, true, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	row := v.Report.Rows[0]
	assertDecimal284(t, row.Progress, "2")
	assertDecimal284(t, row.Money, "100")
	if row.Evaluation != "Fixture đánh giá đúng kỳ" {
		t.Fatal("must import evaluation from the same source period", row.Evaluation)
	}
	missing := weeklyCSV284(t, [][]string{{"Tên hạng mục", fmt.Sprintf("Tiến độ thực hiện tuần %d", n), fmt.Sprintf("Kinh phí tuần %d", n), fmt.Sprintf("Đánh giá tuần %d", n+1), "Lũy kế"}, {leaf.Name, "4", "300", "Không được lấy từ kỳ sau", "1e100000"}})
	w = e.upload(t, "tinh", "training", "preview", "", v, missing, "", false, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.SourceColumns, "đánh giá: không có") {
		t.Fatal("must disclose missing evaluation column", preview.SourceColumns)
	}
	w = e.upload(t, "tinh", "training", "confirm", "", v, missing, preview.SHA256, true, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	row = v.Report.Rows[0]
	assertDecimal284(t, row.Progress, "4")
	assertDecimal284(t, row.Money, "300")
	if row.Evaluation != "Fixture đánh giá đúng kỳ" {
		t.Fatal("another week's evaluation overwrote existing text", row.Evaluation)
	}
}
