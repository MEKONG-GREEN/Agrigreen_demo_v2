package kn77_test

// P0-POOL-KN-2 (#139): tiếp #125 cho các đường KN còn lại — CNTS (CT/DA, điểm trình diễn, hộ mô hình, cấp quyền), KHTC (Kế
// hoạch năm, tiến độ kỳ). Mọi module chạy trên CHUNG một pool vai đăng nhập app_platform nhỏ (2 kết nối) như gốc lắp ráp
// thật (ADR-0028). Port của module khác ở kiểm thử là bản giả, nhưng mỗi lời gọi MỞ MỘT GIAO DỊCH NGẮN trên cùng pool đó —
// đúng như cài đặt thật (pf/masterdata Catalogs/EffectiveCatalogs/Params, pf/party ScopeResolver, KN platformread
// Units/Orgs, pf/accounts XaScopes đều tenant.WithTenantTx trên pool của gốc lắp ráp). Port nội bộ KN (KHTCPrograms,
// ClosedCutoff, EntryUsage) là thật. Gọi song song nhiều hơn cỡ pool: đường nào giữ giao dịch KN trong khi chờ kết nối
// thứ hai làm cạn pool → lời gọi treo tới hết hạn 10 s.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// poolCallTimeout là hạn của MỖI lời gọi song song (như client thật bỏ cuộc).
const poolCallTimeout = 10 * time.Second

// touchPool là "giao dịch riêng của port module khác" trên pool chung: mượn một kết nối, chạy một câu, trả lại.
type touchPool struct{ pool *pgxpool.Pool }

func (p touchPool) touch(ctx context.Context) error {
	if p.pool == nil {
		return nil
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	_, err = tx.Exec(ctx, "SELECT 1")
	return err
}

// poolCall là một lời gọi của cơn song song; do nhận ctx đã đặt hạn poolCallTimeout.
type poolCall struct {
	name string
	want []int
	do   func(ctx context.Context) (int, string, error)
}

// runPoolStorm chạy mọi lời gọi CÙNG LÚC; lời gọi lỗi/treo/sai mã → Fatal kèm số lỗi và thời gian.
func runPoolStorm(t *testing.T, label string, poolSize int, calls []poolCall) {
	t.Helper()
	type failure struct {
		name, body string
		status     int
		err        error
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	res := make(chan failure, len(calls))
	for _, c := range calls {
		wg.Add(1)
		go func(c poolCall) {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), poolCallTimeout)
			defer cancel()
			st, body, err := c.do(ctx)
			ok := err == nil
			if ok {
				ok = false
				for _, w := range c.want {
					ok = ok || st == w
				}
			}
			if !ok {
				res <- failure{name: c.name, status: st, body: body, err: err}
			}
		}(c)
	}
	began := time.Now()
	close(start)
	wg.Wait()
	close(res)
	n := 0
	for r := range res {
		n++
		t.Errorf("%s: status=%d err=%v body=%.200s", r.name, r.status, r.err, r.body)
	}
	if n > 0 {
		t.Fatalf("P0-POOL-KN-2 %s: %d/%d lời gọi song song hỏng trên pool %d kết nối sau %s — giao dịch KN giữ kết nối trong khi chờ port module khác",
			label, n, len(calls), poolSize, time.Since(began).Round(time.Millisecond))
	}
	t.Logf("P0-POOL-KN-2 %s: %d lời gọi song song trên pool %d kết nối xong sau %s", label, len(calls), poolSize, time.Since(began).Round(time.Millisecond))
}

// httpDo gửi một yêu cầu tới máy chủ kiểm thử với ctx (hạn của lời gọi).
func httpDo(ctx context.Context, srv *httptest.Server, token string, ctxID uuid.UUID, method, path, idem string, body any) (int, string, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, srv.URL+path, rd)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if ctxID != uuid.Nil {
		req.Header.Set(authz.ContextHeader, ctxID.String())
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(raw), nil
}

// ---------------------------------------------------------------------------
// CNTS: CT/DA, điểm trình diễn, hộ mô hình, cấp quyền (extension/cntsmodel)
// ---------------------------------------------------------------------------

const cntsPoolDept = partyport.DeptChanNuoiThuySan

var pk2Fac1 = uuid.MustParse("0192a000-0000-7000-8000-000000000001") // cơ sở giết mổ xã 30001

// pk2Units: danh mục đơn vị hành chính (KN platformread / pf/masterdata thật mở giao dịch riêng).
type pk2Units struct{ touchPool }

func (u pk2Units) Get(ctx context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	if err := u.touch(ctx); err != nil {
		return partyport.AdminUnit{}, err
	}
	switch code {
	case "30001", "30002":
		return partyport.AdminUnit{Code: code, Name: "Xã " + code, Level: partyport.LevelCommune, ParentCode: "89"}, nil
	case "89":
		return partyport.AdminUnit{Code: code, Name: "Tỉnh", Level: partyport.LevelProvince}, nil
	}
	return partyport.AdminUnit{}, partyport.ErrUnitNotFound
}
func (u pk2Units) Descendants(ctx context.Context, _ string, _ time.Time) ([]string, error) {
	return nil, u.touch(ctx)
}
func (u pk2Units) Current(ctx context.Context, _ string, _ time.Time) ([]string, error) {
	return nil, u.touch(ctx)
}

// pk2Catalogs: danh mục CNTS theo ngày (pf/masterdata EffectiveCatalogs thật mở giao dịch riêng).
type pk2Catalogs struct {
	knport.CNTSCatalogs
	touchPool
}

func (c pk2Catalogs) ItemAt(ctx context.Context, catalog, code string, _ time.Time) (mdport.CatalogItem, error) {
	if err := c.touch(ctx); err != nil {
		return mdport.CatalogItem{}, err
	}
	if catalog == knport.CatalogCNTSModelType && code == "GA_TP" {
		return mdport.CatalogItem{Catalog: catalog, Code: code, Name: "Gà thương phẩm", Active: true}, nil
	}
	return mdport.CatalogItem{}, fmt.Errorf("giả: %w", mdport.ErrCatalogItemNotFound)
}

func (c pk2Catalogs) ListAt(ctx context.Context, catalog string, _ time.Time) ([]mdport.CatalogItem, error) {
	if err := c.touch(ctx); err != nil {
		return nil, err
	}
	if catalog == knport.CatalogCNTSModelType {
		return []mdport.CatalogItem{{Catalog: catalog, Code: "GA_TP", Name: "Gà thương phẩm", Active: true}}, nil
	}
	return []mdport.CatalogItem{}, nil
}

func (c pk2Catalogs) FacilitiesAt(ctx context.Context, _ time.Time, _ []string) ([]knport.SlaughterFacility, error) {
	if err := c.touch(ctx); err != nil {
		return nil, err
	}
	return []knport.SlaughterFacility{{Ref: pk2Fac1, Code: "CS1", Name: "Lò mổ 1", CommuneCode: "30001", Kind: knport.FacilityConcentrated, Active: true}}, nil
}

// pk2Xa: cán bộ Cấp Xã CNTS (pf/accounts thật mở giao dịch riêng).
type pk2Xa struct {
	touchPool
	mu   sync.Mutex
	list []accport.XaScope
}

func (x *pk2Xa) XaScopes(ctx context.Context, _ uuid.UUID, _ string) ([]accport.XaScope, error) {
	if err := x.touch(ctx); err != nil {
		return nil, err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]accport.XaScope(nil), x.list...), nil
}

type pk2CNTSFx struct {
	t      *testing.T
	env    *pgtest.Env
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	tenant uuid.UUID
	xa     *pk2Xa
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
}

type pk2User struct {
	f     *pk2CNTSFx
	token string
	ctxID uuid.UUID
	ref   uuid.UUID
}

func newCNTSPoolFx(t *testing.T, poolSize int32) *pk2CNTSFx {
	env := pgtest.Get(t)
	pool := sharedUnitPool(t, env, poolSize)
	tp := touchPool{pool: pool}
	f := &pk2CNTSFx{t: t, env: env, iss: authztest.New(t), tenant: ids.New(), xa: &pk2Xa{touchPool: tp}, ctxs: map[uuid.UUID]*authz.KNContext{}}
	mod := extension.New(pool, clock.NewFixed(time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)), extension.Deps{Units: pk2Units{tp}})
	mod.UseCNTSCatalogs(pk2Catalogs{touchPool: tp})
	mod.UseXaScopes(f.xa)
	mod.UseCNTSModelVault(clienttest.New())
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	eng.Use(logErrors(t))
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), func(c *gin.Context) {
		p, ok := authz.FromContext(c.Request.Context())
		id, err := uuid.Parse(c.GetHeader(authz.ContextHeader))
		f.mu.Lock()
		kc := f.ctxs[id]
		f.mu.Unlock()
		if !ok || err != nil || kc == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		cp := *kc
		p.KN = &cp
		selected, err := registry.ResolveSelected(c.Request.Context(), p)
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		p.SelectedSession = &selected
		scope, _ := json.Marshal(cp)
		ctx := idempotency.WithAuthorizationScope(c.Request.Context(), scope)
		c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, p))
		c.Next()
	}, idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *pk2CNTSFx) user(kc authz.KNContext) pk2User {
	sub := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: sub, TenantID: f.tenant, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	return pk2User{f: f, token: tok, ctxID: kc.ID, ref: sub}
}

func (u pk2User) call(ctx context.Context, method, path string, body any) (int, string, error) {
	idem := ""
	if method == http.MethodPost {
		idem = key()
	}
	return httpDo(ctx, u.f.srv, u.token, u.ctxID, method, path, idem, body)
}

// must gọi tuần tự (pool đủ cho một thao tác lồng) và trả thân JSON.
func (u pk2User) must(method, path string, body any, want int, what string) map[string]any {
	u.f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, raw, err := u.call(ctx, method, path, body)
	if err != nil || st != want {
		u.f.t.Fatalf("%s: muốn %d, nhận %d %v %s", what, want, st, err, raw)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(raw), &m)
	return m
}

func pk2Tinh(group string) authz.KNContext {
	return authz.KNContext{Role: authz.KNTinh, Department: cntsPoolDept, ProvinceGroup: group, Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}}
}

func pk2XaCtx(communes ...string) authz.KNContext {
	return authz.KNContext{Role: authz.KNXa, Department: cntsPoolDept, Communes: communes, Scope: authz.Scope{Units: communes}}
}

// seedKHTCProgram ghi một CT/DA chủ của KHTC (D20) để dự án CNTS gắn vào (cổng KHTCPrograms nội bộ KN đọc lại).
func (f *pk2CNTSFx) seedKHTCProgram(code string) string {
	ref := ids.New()
	if _, err := f.env.AdminPool(f.t, migrations.PF).Exec(context.Background(), `INSERT INTO pf_extension.khtc_program (id, tenant_id, stream_key,
	    program_ref, code, name, kind, status, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, 'Chương trình KHTC', 'project', 'active', now(), $6, $7, 'test')`,
		ids.New(), f.tenant, "program:"+ref.String(), ref, code, ids.New(), "seed-"+ref.String()); err != nil {
		f.t.Fatal(err)
	}
	return ref.String()
}

// TestP0PoolKN2_CNTSModelNoNestedTx: sửa CT/DA gắn CT/DA KHTC (KHTCPrograms trong giao dịch), thêm/sửa điểm trình diễn
// (đơn vị, danh mục loại mô hình, cán bộ Cấp Xã trong giao dịch), sửa hộ mô hình đổi người theo dõi (cán bộ Cấp Xã trong
// giao dịch), cấp quyền (đơn vị, cơ sở giết mổ, cán bộ Cấp Xã trong giao dịch) cùng tạo dự án, danh sách dự án/hộ — song
// song trên pool 2 kết nối.
func TestP0PoolKN2_CNTSModelNoNestedTx(t *testing.T) {
	const poolSize = 2
	const per = 5
	f := newCNTSPoolFx(t, poolSize)
	chu := f.user(pk2Tinh("chu_nhiem"))
	officer := f.user(pk2XaCtx("30001"))
	keeper := f.user(pk2XaCtx("30001"))
	grantees := make([]pk2User, per)
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}, {ActorRef: keeper.ref, Communes: []string{"30001"}}}
	for i := range grantees {
		grantees[i] = f.user(pk2XaCtx("30001"))
		f.xa.list = append(f.xa.list, accport.XaScope{ActorRef: grantees[i].ref, Communes: []string{"30001"}})
	}
	prog := f.seedKHTCProgram("CT-KNQG-01")
	const base = "/v1/extension/cnts/"
	project := func(code string) map[string]any {
		return map[string]any{"code": code, "name": "Mô hình gà an toàn sinh học " + code, "kind": "knqg", "basis": "QĐ 123/QĐ-BNN",
			"start_date": "2027-03-01", "end_date": "2027-12-31", "budget": "450000000"}
	}
	site := func(pref, name string) map[string]any {
		return map[string]any{"project_ref": pref, "commune_code": "30001", "name": name, "model_type_code": "GA_TP", "scale": "1500",
			"scale_unit": "con", "officer_ref": officer.ref.String(), "diary_form": "BM-08"}
	}

	// Tuần tự: dự án để sửa, một điểm, hộ để sửa (mỗi lời gọi song song sửa bản ghi của riêng nó — không xung đột phiên bản).
	var projects []string
	for i := 0; i < per; i++ {
		projects = append(projects, str(chu.must(http.MethodPost, base+"projects", project(fmt.Sprintf("DA-SUA-%d", i)), 201, "tạo dự án"), "ref"))
	}
	sref := str(chu.must(http.MethodPost, base+"demo-sites", site(projects[0], "Điểm ấp Bình Hòa"), 201, "thêm điểm"), "ref")
	var households []string
	for i := 0; i < per; i++ {
		h := chu.must(http.MethodPost, base+"model-households", map[string]any{"site_ref": sref, "owner_name": fmt.Sprintf("Nguyễn Văn Hộ %d", i),
			"owner_phone": fmt.Sprintf("09123450%02d", i), "scale": "500", "scale_unit": "con", "keeper_kind": "officer",
			"keeper_ref": officer.ref.String(), "joined_on": "2027-03-05"}, 201, "thêm hộ")
		households = append(households, str(h, "ref"))
	}

	var calls []poolCall
	for i := 0; i < per; i++ {
		i := i
		calls = append(calls,
			poolCall{name: fmt.Sprintf("UpdateProject#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return chu.call(ctx, http.MethodPost, base+"projects/"+projects[i], mergeMap(project(fmt.Sprintf("DA-SUA-%d", i)),
					map[string]any{"base_version": 1, "program_ref": prog}))
			}},
			poolCall{name: fmt.Sprintf("CreateSite#%d", i), want: []int{201}, do: func(ctx context.Context) (int, string, error) {
				return chu.call(ctx, http.MethodPost, base+"demo-sites", site(projects[0], fmt.Sprintf("Điểm song song %d", i)))
			}},
			poolCall{name: fmt.Sprintf("UpdateHousehold#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return chu.call(ctx, http.MethodPost, base+"model-households/"+households[i], map[string]any{"base_version": 1,
					"keeper_kind": "officer", "keeper_ref": keeper.ref.String(), "scale": "600", "scale_unit": "con", "joined_on": "2027-03-05"})
			}},
			poolCall{name: fmt.Sprintf("CreateGrant#%d", i), want: []int{201}, do: func(ctx context.Context) (int, string, error) {
				return chu.call(ctx, http.MethodPost, base+"grants", map[string]any{"grantee_ref": grantees[i].ref.String(),
					"role": "theo_doi_mo_hinh", "commune_codes": []string{"30001"}, "site_refs": []string{sref}, "forms": []string{"BM-08"}})
			}},
			poolCall{name: fmt.Sprintf("CreateProject#%d", i), want: []int{201}, do: func(ctx context.Context) (int, string, error) {
				return chu.call(ctx, http.MethodPost, base+"projects", project(fmt.Sprintf("DA-MOI-%d", i)))
			}},
			poolCall{name: fmt.Sprintf("ListProjects#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return chu.call(ctx, http.MethodGet, base+"projects", nil)
			}},
			poolCall{name: fmt.Sprintf("ListHouseholds#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return officer.call(ctx, http.MethodGet, base+"model-households?site_ref="+sref, nil)
			}},
		)
	}
	runPoolStorm(t, "CNTS hộ mô hình/dự án/điểm/cấp quyền", poolSize, calls)

	// Ngữ nghĩa giữ nguyên: dự án gắn CT/DA KHTC, hộ đổi người theo dõi, điểm và quyền đã ghi.
	p := chu.must(http.MethodGet, base+"projects/"+projects[1], nil, 200, "đọc dự án")
	if pr, _ := p["program"].(map[string]any); str(p, "program_ref") != prog || pr == nil || str(pr, "code") != "CT-KNQG-01" {
		t.Fatalf("dự án sau sửa: %v", p)
	}
	h := chu.must(http.MethodGet, base+"model-households/"+households[2], nil, 200, "đọc hộ")
	if str(h, "keeper_ref") != keeper.ref.String() {
		t.Fatalf("hộ sau sửa: %v", h)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_grant WHERE tenant_id = $1`, f.tenant); n != per {
		t.Fatalf("quyền đã cấp: %d, muốn %d", n, per)
	}
	// Kiểm vẫn chạy trong giao dịch: điểm ở xã ngoài tỉnh, cán bộ không phụ trách, loại mô hình sai → từ chối như cũ.
	bad := site(projects[0], "Điểm sai")
	bad["model_type_code"] = "BO"
	chu.must(http.MethodPost, base+"demo-sites", bad, 422, "loại mô hình không có")
	out := site(projects[0], "Điểm ngoài")
	out["officer_ref"] = ids.New().String()
	chu.must(http.MethodPost, base+"demo-sites", out, 422, "cán bộ không phải Cấp Xã CNTS")
}

func (f *pk2CNTSFx) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.env.AdminPool(f.t, migrations.PF).QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func mergeMap(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// KHTC: Kế hoạch năm (ClosedCutoff, EntryUsage nội bộ KN), tiến độ kỳ (cap/forms Progress)
// ---------------------------------------------------------------------------

// khtcCall gọi API KHTC trong tiến trình với ctx (hạn của lời gọi song song).
func (f *khtcFx) callCtx(ctx context.Context, method, path, role, dept string, body any) (int, string, error) {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	k := authz.KNContext{Role: role, Department: dept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}}
	if role == authz.KNAdmin {
		k = authz.KNContext{Role: role, Scope: authz.Scope{All: true}}
	}
	kj, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(kj))
	req.Header.Set("X-Test-Actor", ids.New().String())
	if method == http.MethodPost {
		req.Header.Set(idempotency.Header, "t-"+ids.New().String())
	}
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	if err := ctx.Err(); err != nil {
		return w.Code, w.Body.String(), err
	}
	return w.Code, w.Body.String(), nil
}

func khtcPlanBody(year int, prog1, prog2 string) map[string]any {
	p1, g1, i1, p2, i2 := ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String()
	return map[string]any{"base_version": 0, "decision_no": "123/QĐ-SNN", "decision_date": "2026-10-01", "entries": []map[string]any{
		{"ref": p1, "tab": "programs", "kind": "program", "program_ref": prog1, "sort_order": 1},
		{"ref": g1, "tab": "programs", "kind": "group", "parent_ref": p1, "name": "Xây dựng mô hình"},
		{"ref": i1, "tab": "programs", "kind": "item", "parent_ref": g1, "name": "Mô hình lúa chất lượng cao", "unit_code": "ha",
			"planned_qty": "120.5", "budget": "350000000", "funding_code": "nstinh", "department_code": "trong_trot", "communes": []string{"30001"}},
		{"ref": p2, "tab": "central_project", "kind": "program", "program_ref": prog2, "name": "Dự án KN TW nuôi tôm"},
		{"ref": i2, "tab": "central_project", "kind": "item", "parent_ref": p2, "name": "Điểm trình diễn", "unit_code": "mo_hinh",
			"planned_qty": "3", "department_code": "chan_nuoi_thuy_san", "data_source": "module", "source_key": "cnts.demo_points"},
	}}
}

// TestP0PoolKN2_KHTCNoNestedTx: lưu Kế hoạch năm (ngày chốt kỳ KHTC đã đóng — ClosedCutoff — đọc trong giao dịch lưu),
// xem tiến độ kỳ KHTC (cap/forms Progress, nguồn, nháp) và danh sách kỳ — song song trên pool 2 kết nối chung của
// pf/extension + cap/forms.
func TestP0PoolKN2_KHTCNoNestedTx(t *testing.T) {
	const poolSize = 2
	const per = 6
	env := pgtest.Get(t)
	f := newKHTCFxPool(t, sharedUnitPool(t, env, poolSize))
	tt, cn := partyport.DeptTrongTrot, partyport.DeptChanNuoiThuySan
	c, b := f.do(http.MethodPost, "/v1/extension/khtc/form-template/install", authz.KNAdmin, "", uuid.Nil, "", nil)
	f.ok(c, b, nil)
	var p khtcPeriod
	c, b = f.khtc(http.MethodPost, "/periods", ids.New(), "", map[string]any{"report_type": "week", "year": 2026, "no": 41, "departments": []string{tt, cn}})
	f.ok(c, b, &p)
	for _, cat := range []struct{ kind, code, name string }{{"unit", "ha", "Héc ta"}, {"unit", "mo_hinh", "Mô hình"},
		{"field", "trong_trot", "Trồng trọt"}, {"funding_source", "nstinh", "Ngân sách tỉnh"}} {
		c, b = f.do(http.MethodPost, "/v1/kn/khtc/catalogs/"+cat.kind, authz.KNAdmin, "", uuid.Nil, "", map[string]any{"code": cat.code, "name": cat.name})
		f.ok(c, b, nil)
	}
	program := func(code, kind string) string {
		var r struct{ Ref string }
		c, b := f.do(http.MethodPost, "/v1/kn/khtc/programs", authz.KNTinh, khtcDept, uuid.Nil, "", map[string]any{"code": code, "name": "CT " + code,
			"kind": kind, "field_code": "trong_trot"})
		f.ok(c, b, &r)
		return r.Ref
	}
	prog1, prog2 := program("CT-I", "program"), program("DA-TW-01", "central_project")

	var calls []poolCall
	for i := 0; i < per; i++ {
		year := 2027 + i
		calls = append(calls,
			poolCall{name: fmt.Sprintf("SavePlan#%d", year), want: []int{201}, do: func(ctx context.Context) (int, string, error) {
				return f.callCtx(ctx, http.MethodPost, fmt.Sprintf("/v1/kn/khtc/plans/%d/versions", year), authz.KNTinh, khtcDept,
					khtcPlanBody(year, prog1, prog2))
			}},
			poolCall{name: fmt.Sprintf("Progress#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return f.callCtx(ctx, http.MethodGet, "/v1/extension/khtc/periods/"+p.ID, authz.KNTinh, khtcDept, nil)
			}},
			poolCall{name: fmt.Sprintf("Periods#%d", i), want: []int{200}, do: func(ctx context.Context) (int, string, error) {
				return f.callCtx(ctx, http.MethodGet, "/v1/extension/khtc/periods", authz.KNTinh, khtcDept, nil)
			}},
		)
	}
	runPoolStorm(t, "KHTC Kế hoạch năm/tiến độ kỳ", poolSize, calls)
	got := f.get(p.ID)
	if got.State != "opened" || len(got.Forms) != 2 {
		t.Fatalf("tiến độ kỳ: %+v", got)
	}
	c, b = f.do(http.MethodGet, "/v1/kn/khtc/plans/2027", authz.KNTinh, khtcDept, uuid.Nil, "", nil)
	if c != http.StatusOK || !strings.Contains(string(b), "Mô hình lúa chất lượng cao") {
		t.Fatalf("Kế hoạch năm 2027: %d %s", c, b)
	}
}
