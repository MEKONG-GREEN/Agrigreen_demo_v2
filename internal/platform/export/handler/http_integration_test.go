package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func letter(version int, closing string) doctpl.Template {
	return doctpl.Template{Key: "thu.cong_van", Version: version, Title: "Công văn thử", Owner: "pf/export",
		Vars: []doctpl.Var{{Key: "so", Label: "Số"}, {Key: "ngay", Label: "Ngày", Required: true}, {Key: "noi_dung", Label: "Nội dung", Required: true}},
		Lists: []doctpl.List{{Key: "dong", Label: "Dòng", Fields: []string{"ten", "gia_tri"}}},
		Body: []doctpl.Block{
			{Kind: doctpl.KindColumns, Left: []doctpl.Line{{Text: "Số: {so}"}}, Right: []doctpl.Line{{Text: "{ngay}", Italic: true}}},
			doctpl.Title("CÔNG VĂN"),
			{Kind: doctpl.KindParagraph, Text: "{noi_dung}"},
			{Kind: doctpl.KindTable, Each: "dong", Columns: []doctpl.Column{{Header: "Tên", Value: "{dong.ten}", Width: 4000}, {Header: "Giá trị", Value: "{dong.gia_tri}", Width: 2000, Align: doctpl.AlignRight}}},
			{Kind: doctpl.KindParagraph, Text: closing},
		}}
}

type fixture struct {
	t    *testing.T
	env  *pgtest.Env
	b    tenant.Beginner
	clk  *clock.Fixed
	svc  *app.Service
	r    *gin.Engine
	tid  uuid.UUID
	who  map[string]authz.Principal
	base context.Context
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	f := &fixture{t: t, env: env, b: env.Pool(t, migrations.PF, dbadmin.AppPlatform), clk: clock.NewFixed(time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)), tid: ids.New()}
	f.svc = f.service(letter(1, "Trân trọng./."))
	f.who = map[string]authz.Principal{
		"admin":  {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: authz.KNAdmin}},
		"tinh":   {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: authz.KNTinh, Department: "ke_hoach_tai_chinh"}},
		"other":  {ActorRef: ids.New(), TenantID: ids.New(), KN: &authz.KNContext{Role: authz.KNAdmin}},
		"plat":   {ActorRef: ids.New(), TenantID: f.tid, Roles: []string{authz.RolePlatformAdmin}},
		"farmer": {ActorRef: ids.New(), TenantID: f.tid, Roles: []string{"farmer"}},
	}
	f.base = authz.WithPrincipal(context.Background(), f.who["tinh"])
	gin.SetMode(gin.TestMode)
	f.r = gin.New()
	f.r.Use(func(c *gin.Context) {
		if p, ok := f.who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
		c.Next()
	})
	Handler{Svc: f.svc}.RegisterApp(f.r.Group("/v1"))
	return f
}

var access = port.AccessFunc(func(ctx context.Context) bool {
	p, _ := authz.FromContext(ctx)
	if p.KN != nil {
		return p.KN.Role == authz.KNAdmin
	}
	return p.Has(authz.RolePlatformAdmin)
})

func (f *fixture) service(tpls ...doctpl.Template) *app.Service {
	svc, err := app.NewService(db.ForModule(f.b, app.DBModule), f.clk, access, []port.TemplateRegistration{{Owner: "pf/export", Templates: tpls}})
	if err != nil {
		f.t.Fatal(err)
	}
	return svc
}

func request(subject, idem string, vars map[string]string) port.Request {
	v := map[string]string{"noi_dung": "Báo cáo kết quả.\nĐề nghị tổng hợp."}
	for k, x := range vars {
		v[k] = x
	}
	return port.Request{Template: "thu.cong_van", Subject: subject, FileName: "cong-van.docx", IdemKey: idem,
		Data:   doctpl.Data{Vars: v, Lists: map[string][]doctpl.Item{"dong": {{"ten": "Diện tích", "gia_tri": "1.234,5"}}}},
		Number: &port.Numbering{Series: "BC-TTKN", Var: "so"}, Date: &port.DateLine{Var: "ngay", Place: "An Giang"}}
}

func (f *fixture) call(as, method, path string, want int) []byte {
	f.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Test-As", as)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	if w.Code != want {
		f.t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
	}
	return w.Body.Bytes()
}

// TestExportRun_W10_17: lần xuất ghi sổ byte + băm, số tự cấp {n}/BC-TTKN, dòng địa danh/ngày giờ Việt Nam, khứ hồi chữ,
// idempotency, xuất lại cùng nội dung → cùng byte và dùng lại số.
func TestExportRun_W10_17(t *testing.T) {
	f := newFixture(t)
	ctx := f.base
	run, err := f.svc.Export(ctx, request("thu:bao-cao-1", "k1", nil))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(run.Body)
	if run.Seq != 1 || run.DocNumber != "1/BC-TTKN" || !run.DocNumberAuto || hex.EncodeToString(sum[:]) != run.BodyHash || run.Size != int64(len(run.Body)) {
		t.Fatalf("run = %+v", run)
	}
	c, err := docx.Extract(run.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Số: 1/BC-TTKN", "An Giang, ngày 20 tháng 10 năm 2026", "CÔNG VĂN", "Báo cáo kết quả.\nĐề nghị tổng hợp.", "Trân trọng./."} {
		if !strings.Contains(c.Text(), want) {
			t.Fatalf("thiếu %q:\n%s", want, c.Text())
		}
	}
	if got := c.Tables[1][1]; got[0] != "Diện tích" || got[1] != "1.234,5" {
		t.Fatalf("bảng = %v", c.Tables[1])
	}
	again, err := f.svc.Export(ctx, request("thu:bao-cao-1", "k1", nil))
	if err != nil || again.ID != run.ID || !bytes.Equal(again.Body, run.Body) {
		t.Fatalf("idempotency: %v %v", err, again.ID)
	}
	second, err := f.svc.Export(ctx, request("thu:bao-cao-1", "k2", nil))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == run.ID || second.Seq != 2 || second.DocNumber != "1/BC-TTKN" || !bytes.Equal(second.Body, run.Body) || second.InputHash != run.InputHash {
		t.Fatalf("xuất lại: seq %d số %s cùng byte %v", second.Seq, second.DocNumber, bytes.Equal(second.Body, run.Body))
	}
	got, err := f.svc.Run(ctx, "thu:bao-cao-1", second.ID)
	if err != nil || !bytes.Equal(got.Body, run.Body) {
		t.Fatalf("Run: %v", err)
	}
	if _, err := f.svc.Run(ctx, "thu:khac", second.ID); !errors.Is(err, port.ErrRunNotFound) {
		t.Fatalf("subject khác phải 404, có %v", err)
	}
	runs, err := f.svc.Runs(ctx, "thu:bao-cao-1")
	if err != nil || len(runs) != 2 || runs[0].ID != second.ID || runs[0].Body != nil {
		t.Fatalf("Runs: %v %d", err, len(runs))
	}
	// Số nhập tay: dùng nguyên văn, không cấp số.
	manual, err := f.svc.Export(ctx, request("thu:bao-cao-tay", "k1", map[string]string{"so": "15/BC-TTKN", "ngay": "An Giang, ngày 01 tháng 10 năm 2026"}))
	if err != nil || manual.DocNumber != "15/BC-TTKN" || manual.DocNumberAuto {
		t.Fatalf("số tay: %v %+v", err, manual.DocNumber)
	}
	next, err := f.svc.Export(ctx, request("thu:bao-cao-2", "k1", nil))
	if err != nil || next.DocNumber != "2/BC-TTKN" {
		t.Fatalf("số kế tiếp = %v %s", err, next.DocNumber)
	}
	// Tenant khác: không thấy lần xuất, đánh số riêng từ 1.
	octx := authz.WithPrincipal(context.Background(), f.who["other"])
	if _, err := f.svc.Run(octx, "thu:bao-cao-1", run.ID); !errors.Is(err, port.ErrRunNotFound) {
		t.Fatalf("RLS: %v", err)
	}
	if o, err := f.svc.Export(octx, request("thu:bao-cao-1", "k1", nil)); err != nil || o.DocNumber != "1/BC-TTKN" {
		t.Fatalf("tenant khác: %v %s", err, o.DocNumber)
	}
}

// TestDocNumberGapSafe_W10_17: cấp số đồng thời không trùng, không hở; giao dịch hỏng sau khi cấp không để lại lỗ; năm
// mới (giờ Việt Nam) đánh lại từ 1; cùng subject đồng thời chỉ một số.
func TestDocNumberGapSafe_W10_17(t *testing.T) {
	f := newFixture(t)
	ctx := f.base
	// Thiếu biến bắt buộc → dựng lỗi SAU khi đã cấp số trong giao dịch → hủy cả giao dịch.
	bad := request("thu:loi", "k1", nil)
	delete(bad.Data.Vars, "noi_dung")
	if _, err := f.svc.Export(ctx, bad); !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("thiếu biến phải export.invalid, có %v", err)
	}
	const n = 12
	var wg sync.WaitGroup
	numbers := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := f.svc.Export(ctx, request(fmt.Sprintf("thu:dong-thoi-%02d", i), "k1", nil))
			numbers[i], errs[i] = r.DocNumber, err
		}(i)
	}
	// Cùng subject đồng thời: một số duy nhất.
	same := make([]string, 4)
	for i := range same {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := f.svc.Export(ctx, request("thu:cung-doi-tuong", fmt.Sprintf("s%d", i), nil))
			if err != nil {
				t.Error(err)
			}
			same[i] = r.DocNumber
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range same[1:] {
		if s != same[0] {
			t.Fatalf("cùng subject nhận nhiều số: %v", same)
		}
	}
	all := append(append([]string{}, numbers...), same[0])
	slices.Sort(all)
	want := []string{}
	for i := 1; i <= n+1; i++ {
		want = append(want, fmt.Sprintf("%d/BC-TTKN", i))
	}
	slices.Sort(want)
	if !slices.Equal(all, want) {
		t.Fatalf("số cấp = %v, muốn 1..%d không hở", all, n+1)
	}
	// 17:30 UTC 31/12/2026 = 00:30 01/01/2027 giờ Việt Nam → năm mới, số 1.
	f.clk.Advance(time.Date(2026, 12, 31, 17, 30, 0, 0, time.UTC).Sub(f.clk.Now()))
	r, err := f.svc.Export(ctx, request("thu:nam-moi", "k1", nil))
	if err != nil || r.DocNumber != "1/BC-TTKN" {
		t.Fatalf("năm mới: %v %s", err, r.DocNumber)
	}
	c, _ := docx.Extract(r.Body)
	if !strings.Contains(c.Text(), "An Giang, ngày 01 tháng 01 năm 2027") {
		t.Fatalf("ngày giờ Việt Nam:\n%s", c.Text())
	}
}

// TestTemplateVersionsImmutable_W10_17: phiên bản mẫu đã ghi sổ bất biến (đổi nội dung cùng phiên bản → drift), phiên
// bản mới dùng được; sổ chặn UPDATE/DELETE; DB kiểm băm byte và mẫu tham chiếu.
func TestTemplateVersionsImmutable_W10_17(t *testing.T) {
	f := newFixture(t)
	ctx := f.base
	if _, err := f.svc.Export(ctx, request("thu:mau", "k1", nil)); err != nil {
		t.Fatal(err)
	}
	drift := f.service(letter(1, "Nội dung khác, cùng phiên bản."))
	if _, err := drift.Export(ctx, request("thu:mau", "k2", nil)); !errors.Is(err, port.ErrTemplateDrift) {
		t.Fatalf("cần export.template_drift, có %v", err)
	}
	v2 := f.service(letter(1, "Trân trọng./."), letter(2, "Trân trọng (bản 2)./."))
	r2, err := v2.Export(ctx, request("thu:mau", "k3", nil))
	if err != nil || r2.TemplateVersion != 2 {
		t.Fatalf("phiên bản mới: %v %d", err, r2.TemplateVersion)
	}
	if _, err := app.NewService(f.b, f.clk, access, []port.TemplateRegistration{{Templates: []doctpl.Template{letter(1, "a"), letter(1, "b")}}}); err == nil {
		t.Fatal("đăng ký trùng phiên bản phải lỗi khởi động")
	}
	admin := f.env.AdminPool(t, migrations.PF)
	for _, sql := range []string{
		`UPDATE pf_export.export_run SET file_name = 'x.docx'`,
		`DELETE FROM pf_export.template_version`,
		`UPDATE pf_export.doc_number SET subject = 'thu:x'`,
	} {
		if _, err := admin.Exec(context.Background(), sql); err == nil {
			t.Fatalf("sổ chỉ ghi thêm phải chặn: %s", sql)
		}
	}
	// Ghi thẳng bằng vai module: băm sai và mẫu không có trong sổ đều bị DB từ chối.
	err = db.WithModuleTenantTx(context.Background(), f.b, app.DBModule, f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), `INSERT INTO pf_export.export_run (id, tenant_id, stream_key, subject, template_key, template_version,
  template_hash, input_hash, file_name, content_type, body, body_hash, event_time, actor_ref, idem_key, rule_version)
  VALUES ($1, $2, 'run:thu:x', 'thu:x', 'thu.cong_van', 1, $3, $3, 'x.docx', $4, '\x00'::bytea, $3, now(), $1, 'x1', 'test')`,
			ids.New(), f.tid, strings.Repeat("a", 64), docx.ContentType)
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "export_run_body_hash") {
		t.Fatalf("băm sai phải bị CHECK chặn, có %v", err)
	}
}

// TestAdminAPI_W10_17: màn quản trị — danh sách mẫu (đăng ký + đã ghi sổ), lịch sử, tải lại; chỉ Admin.
func TestAdminAPI_W10_17(t *testing.T) {
	f := newFixture(t)
	run, err := f.svc.Export(f.base, request("thu:quan-tri", "k1", nil))
	if err != nil {
		t.Fatal(err)
	}
	var tpl struct {
		Items []struct {
			Key      string `json:"key"`
			Latest   int    `json:"latest"`
			Sections []string
			Versions []struct {
				Version    int        `json:"version"`
				Registered bool       `json:"registered"`
				RecordedAt *time.Time `json:"recorded_at"`
				Drift      bool       `json:"drift"`
			} `json:"versions"`
		} `json:"items"`
	}
	if err := json.Unmarshal(f.call("admin", http.MethodGet, "/v1/exports/templates", http.StatusOK), &tpl); err != nil {
		t.Fatal(err)
	}
	if len(tpl.Items) != 1 || tpl.Items[0].Key != "thu.cong_van" || tpl.Items[0].Latest != 1 || len(tpl.Items[0].Versions) != 1 ||
		!tpl.Items[0].Versions[0].Registered || tpl.Items[0].Versions[0].RecordedAt == nil || tpl.Items[0].Versions[0].Drift {
		t.Fatalf("templates = %+v", tpl)
	}
	var runs struct {
		Items []RunDTO `json:"items"`
	}
	_ = json.Unmarshal(f.call("plat", http.MethodGet, "/v1/exports/runs?template=thu.cong_van", http.StatusOK), &runs)
	if len(runs.Items) != 1 || runs.Items[0].ID != run.ID || runs.Items[0].DocNumber != "1/BC-TTKN" || runs.Items[0].BodyHash != run.BodyHash {
		t.Fatalf("runs = %+v", runs)
	}
	body := f.call("admin", http.MethodGet, "/v1/exports/runs/"+run.ID.String()+"/download", http.StatusOK)
	if !bytes.Equal(body, run.Body) {
		t.Fatal("tải lại khác byte")
	}
	f.call("tinh", http.MethodGet, "/v1/exports/templates", http.StatusForbidden)
	f.call("farmer", http.MethodGet, "/v1/exports/runs", http.StatusForbidden)
	f.call("", http.MethodGet, "/v1/exports/runs", http.StatusUnauthorized)
	f.call("other", http.MethodGet, "/v1/exports/runs/"+run.ID.String()+"/download", http.StatusNotFound)
	f.call("admin", http.MethodGet, "/v1/exports/runs/khong-phai-uuid/download", http.StatusNotFound)
}
