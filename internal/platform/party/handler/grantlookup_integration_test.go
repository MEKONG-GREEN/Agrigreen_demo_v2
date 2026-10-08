package handler_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/grantlookup"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// fakeCheckpoint là điểm kiểm soát xuất giả (pf/metering chưa nối ở kiểm thử module).
type fakeCheckpoint struct {
	mu    sync.Mutex
	allow bool
	err   error
	seen  []meter.Egress
}

func (c *fakeCheckpoint) Pass(_ context.Context, e meter.Egress) (meter.Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, e)
	if c.err != nil {
		return meter.Decision{}, c.err
	}
	return meter.Decision{Allowed: c.allow}, nil
}

func (c *fakeCheckpoint) calls() []meter.Egress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]meter.Egress(nil), c.seen...)
}

// grant đề xuất (a) + duyệt (b) một cấp quyền của tenant f.tenant cho grantee.
func (f *fixture) grant(grantee uuid.UUID, kinds ...string) string {
	f.t.Helper()
	a, b := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin), f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	r := a.adminDo(http.MethodPost, "/v1/share-grants", proposal(grantee, kinds...))
	wantStatus(f.t, r, http.StatusCreated, "đề xuất")
	id := str(r.body, "id")
	wantStatus(f.t, b.adminDo(http.MethodPost, "/v1/share-grants/"+id+"/approve", nil), http.StatusCreated, "duyệt")
	return id
}

// ADR-0022 mục 3 / G5-1 (P): GrantLookup.ActiveGrant cho cmd/api-public — không cần vai OIDC; chỉ
// loại dp_*; chưa duyệt / hết hạn / chưa hiệu lực / thu hồi → share_grant_missing; grantor chỉ chọn
// bên cấp (sai → missing, nhiều bên cấp + Nil → ambiguous); tenant bên thứ ba khác không thấy.
func TestGrantLookupActiveGrant_ADR0022(t *testing.T) {
	f := newFixture(t)
	third := ids.New()
	gl := f.mod.GrantLookup()
	ctx := context.Background() // KHÔNG có authz.Principal — bên gọi là khách hàng API
	now := time.Now()

	// Chưa có / chỉ đề xuất → missing.
	if _, err := gl.ActiveGrant(ctx, third, uuid.Nil, port.KindDPCollectionVolume, now); !errors.Is(err, port.ErrShareGrantMissing) {
		t.Fatalf("chưa cấp → missing: %v", err)
	}
	a := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	r := a.adminDo(http.MethodPost, "/v1/share-grants", proposal(third, port.KindDPCollectionVolume))
	wantStatus(t, r, http.StatusCreated, "đề xuất")
	if _, err := gl.ActiveGrant(ctx, third, uuid.Nil, port.KindDPCollectionVolume, now); !errors.Is(err, port.ErrShareGrantMissing) {
		t.Fatalf("chưa duyệt → missing: %v", err)
	}
	wantStatus(t, f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin).adminDo(http.MethodPost, "/v1/share-grants/"+str(r.body, "id")+"/approve", nil),
		http.StatusCreated, "duyệt")

	g, err := gl.ActiveGrant(ctx, third, uuid.Nil, port.KindDPCollectionVolume, time.Now())
	if err != nil || g.Grantor != f.tenant || g.Grantee != third || !g.Covers(port.KindDPCollectionVolume) {
		t.Fatalf("cấp quyền hiệu lực: %+v %v", g, err)
	}
	if g2, err := gl.ActiveGrant(ctx, third, f.tenant, port.KindDPCollectionVolume, time.Now()); err != nil || g2.ID != g.ID {
		t.Fatalf("grantor đúng: %+v %v", g2, err)
	}
	for name, c := range map[string]struct {
		grantee, grantor uuid.UUID
		kind             string
		at               time.Time
	}{
		"grantor khác":           {third, ids.New(), port.KindDPCollectionVolume, time.Now()},
		"loại dp khác":           {third, uuid.Nil, port.KindDPHubCapacity, time.Now()},
		"tenant bên thứ ba khác": {ids.New(), uuid.Nil, port.KindDPCollectionVolume, time.Now()},
		"trước valid_from":       {third, uuid.Nil, port.KindDPCollectionVolume, now.Add(-time.Hour)},
		"sau valid_to (hết hạn)": {third, uuid.Nil, port.KindDPCollectionVolume, now.Add(40 * 24 * time.Hour)},
		"loại lạ":                {third, uuid.Nil, "dp_raw_mrv", time.Now()},
		"grantee Nil":            {uuid.Nil, uuid.Nil, port.KindDPCollectionVolume, time.Now()},
	} {
		if _, err := gl.ActiveGrant(ctx, c.grantee, c.grantor, c.kind, c.at); !errors.Is(err, port.ErrShareGrantMissing) {
			t.Errorf("%s → share_grant_missing: %v", name, err)
		}
	}

	// Loại KHÔNG phải dp_* (đã cấp cho cùng tenant) không đọc được qua GrantLookup — chỉ qua
	// Authorize (vai mkg_sync): cổng không vai OIDC không được mở rộng sang dữ liệu khác.
	f.grant(third, port.KindPlotDirectory)
	if _, err := gl.ActiveGrant(ctx, third, uuid.Nil, port.KindPlotDirectory, time.Now()); !errors.Is(err, port.ErrShareGrantMissing) {
		t.Fatalf("loại không phải dp_* → missing: %v", err)
	}

	// Hai bên cấp cùng loại: Nil → ambiguous; chọn đúng bên cấp → được.
	other := f.tenant
	f.tenant = ids.New()
	f.grant(third, port.KindDPCollectionVolume)
	second := f.tenant
	f.tenant = other
	if _, err := gl.ActiveGrant(ctx, third, uuid.Nil, port.KindDPCollectionVolume, time.Now()); !errors.Is(err, port.ErrShareGrantorAmbiguous) {
		t.Fatalf("hai bên cấp + grantor Nil → ambiguous: %v", err)
	}
	if g3, err := gl.ActiveGrant(ctx, third, second, port.KindDPCollectionVolume, time.Now()); err != nil || g3.Grantor != second {
		t.Fatalf("chọn bên cấp thứ hai: %+v %v", g3, err)
	}

	// Thu hồi → missing ngay ở lời gọi kế tiếp.
	wantStatus(t, a.adminDo(http.MethodPost, "/v1/share-grants/"+g.ID.String()+"/revoke", map[string]any{"reason": "Ngừng hợp tác với bên thứ ba (thử)"}),
		http.StatusCreated, "thu hồi")
	if _, err := gl.ActiveGrant(ctx, third, f.tenant, port.KindDPCollectionVolume, time.Now()); !errors.Is(err, port.ErrShareGrantMissing) {
		t.Fatalf("thu hồi → missing: %v", err)
	}
}

// ADR-0020 + ADR-0022 (E2E-G5-04): lần đọc của khách hàng Public API ghi một dòng partner_read_log ở
// tenant bên cấp (người đọc = mã khách hàng API, tenant bên nhận lấy từ bản ghi khóa) và KHÔNG đi
// qua kênh partner_share (egress public_api do api-public ghi cùng sổ sử dụng). Khách hàng của
// tenant khác → partner_audit_unavailable.
func TestRecordReadAPIClient_ADR0022(t *testing.T) {
	f := newFixture(t)
	third := ids.New()
	f.grant(third, port.KindDPHubCapacity)
	cp := &fakeCheckpoint{allow: false}
	f.mod.UseEgress(cp, meter.ModeEnforce, nil)
	g, err := f.mod.GrantLookup().ActiveGrant(context.Background(), third, uuid.Nil, port.KindDPHubCapacity, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	client := apiclientport.Client{ID: ids.New(), ClientTenantID: third, Status: apiclientport.ClientActive}
	ctx := apiclientport.WithCaller(context.Background(), apiclientport.Caller{Client: client, KeyPrefix: "abcdefgh2345"})
	pa := f.mod.PartnerAccess()
	if err := pa.RecordRead(ctx, g, port.PartnerRead{Kind: port.KindDPHubCapacity, Endpoint: "GET /v1/data/hub-capacities", Items: 3}); err != nil {
		t.Fatalf("ghi sổ đọc của khách hàng API: %v", err)
	}
	if n := f.countIn(f.tenant, "pf_party.partner_read_log WHERE actor_ref = '"+client.ID.String()+"' AND grantee_tenant_id = '"+third.String()+"'"); n != 1 {
		t.Fatalf("sổ đọc ở tenant bên cấp: %d", n)
	}
	if len(cp.calls()) != 0 {
		t.Fatalf("khách hàng API không đi qua kênh partner_share: %+v", cp.calls())
	}
	foreign := apiclientport.WithCaller(context.Background(), apiclientport.Caller{Client: apiclientport.Client{ID: ids.New(), ClientTenantID: ids.New()}})
	if err := pa.RecordRead(foreign, g, port.PartnerRead{Kind: port.KindDPHubCapacity}); !errors.Is(err, port.ErrPartnerAuditUnavailable) {
		t.Fatalf("khách hàng của tenant khác: %v", err)
	}
}

// ADR-0024 mục 3 (L-06, P2): bề mặt /v1/partner/* đi qua Guard kênh partner_share — một Egress
// (tenant dữ liệu = bên cấp, bên nhận = tenant người gọi, số phần tử = số dòng trả); observe không
// từ chối, kể cả khi checkpoint lỗi; enforce + từ chối → 429 metering.egress_quota_exceeded, không
// ghi sổ đọc; enforce + checkpoint lỗi → 503 metering.egress_unavailable; nil → hành vi cũ
// (TestShareGrantLifecycle_SoD_Audit_ADR0020 không đổi).
func TestPartnerShareEgress_ADR0024(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	_ = tr
	mkg := ids.New()
	f.grant(mkg, port.KindCooperativeDirectory)
	sa := f.as(mkg, uuid.Nil, port.RolePartnerSync)
	reads := func() int { return f.countIn(f.tenant, "pf_party.partner_read_log") }

	cp := &fakeCheckpoint{allow: false}
	f.mod.UseEgress(cp, meter.ModeObserve, nil)
	wantStatus(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusOK, "observe + vượt hạn mức vẫn cho qua")
	calls := cp.calls()
	if len(calls) != 1 {
		t.Fatalf("một Egress mỗi lần đọc: %+v", calls)
	}
	e := calls[0]
	if e.Channel != meter.ChannelPartnerShare || e.DataTenantID != f.tenant || e.CounterpartyKind != meter.CounterpartyTenant ||
		e.CounterpartyRef != mkg || e.Product != port.KindCooperativeDirectory || e.Records != 2 || e.ActorRef != sa.actor || e.SourceID == uuid.Nil {
		t.Fatalf("Egress partner_share: %+v", e)
	}
	if reads() != 1 {
		t.Fatalf("sổ đọc: %d", reads())
	}

	cp.err = errors.New("metering down")
	wantStatus(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusOK, "observe + checkpoint lỗi vẫn tiếp tục")
	if reads() != 2 {
		t.Fatalf("sổ đọc: %d", reads())
	}

	f.mod.UseEgress(cp, meter.ModeEnforce, nil)
	wantCode(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusServiceUnavailable, "metering.egress_unavailable", "enforce + checkpoint lỗi")
	cp.err = nil
	wantCode(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusTooManyRequests, "metering.egress_quota_exceeded", "enforce + vượt hạn mức")
	if reads() != 2 {
		t.Fatalf("enforce từ chối → không ghi sổ đọc: %d", reads())
	}
	cp.allow = true
	wantStatus(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusOK, "enforce + trong hạn mức")
	if reads() != 3 {
		t.Fatalf("sổ đọc: %d", reads())
	}

	f.mod.UseEgress(nil, meter.ModeEnforce, nil)
	n := len(cp.calls())
	wantStatus(t, sa.appDo(http.MethodGet, "/v1/partner/cooperatives", nil), http.StatusOK, "chưa nối checkpoint")
	if len(cp.calls()) != n || reads() != 4 {
		t.Fatalf("nil → hành vi cũ: %d %d", len(cp.calls()), reads())
	}
}

// G5 K-05 (KN US-MRV-05 — đơn vị thẩm định chỉ xem, mọi lần truy cập có nhật ký; ADR-0022 mục 3):
// bộ điều hợp KHÔNG kéo kho định danh party/port/grantlookup (cmd/api-public, RB-3) hiện thực
// port.APIReadLog — một dòng partner_read_log ở tenant BÊN CẤP cho mỗi lần đọc của khách hàng Public
// API (người đọc = mã khách hàng, tenant bên nhận từ bản ghi khóa); ghi lại cùng mã yêu cầu không thêm
// dòng; không có Caller / Caller tenant khác / loại không có trong cấp quyền / loại không phải dp_* →
// party.partner_audit_unavailable (module gọi không trả dữ liệu); không qua kênh partner_share.
func TestAPIReadLogAdapter_K05(t *testing.T) {
	f := newFixture(t)
	verifier := ids.New()
	f.grant(verifier, port.KindDPMRVPackageStatus)
	gl := grantlookup.New(f.pool, clock.System{})
	ctx := context.Background()
	g, err := gl.ActiveGrant(ctx, verifier, uuid.Nil, port.KindDPMRVPackageStatus, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	client := apiclientport.Client{ID: ids.New(), ClientTenantID: verifier, Status: apiclientport.ClientActive}
	req := ids.New()
	call := apiclientport.WithCaller(ctx, apiclientport.Caller{Client: client, KeyPrefix: "abcdefgh2345", RequestID: req, ReceivedAt: time.Now()})
	read := port.PartnerRead{Kind: port.KindDPMRVPackageStatus, Endpoint: "GET /v1/data/mrv-packages", Query: "status=completed", Items: 2}
	var _ port.APIReadLog = gl
	if err := gl.RecordRead(call, g, read); err != nil {
		t.Fatalf("ghi sổ đọc của đơn vị thẩm định: %v", err)
	}
	where := "pf_party.partner_read_log WHERE actor_ref = '" + client.ID.String() + "' AND grantee_tenant_id = '" + verifier.String() +
		"' AND grant_id = '" + g.ID.String() + "' AND kind = 'dp_mrv_package_status' AND endpoint = 'GET /v1/data/mrv-packages' AND item_count = 2" +
		" AND query = 'status=completed' AND idem_key = 'read:api:" + req.String() + "'"
	if n := f.countIn(f.tenant, where); n != 1 {
		t.Fatalf("một dòng sổ đọc ở tenant bên cấp: %d", n)
	}
	if n := f.countIn(verifier, "pf_party.partner_read_log"); n != 0 {
		t.Fatalf("sổ đọc không ở tenant bên thứ ba: %d", n)
	}
	// Ghi lại trong CÙNG yêu cầu (cùng AG-Request-Id) → trùng idem_key → lỗi, không thêm dòng.
	var pe *problem.Problem
	if err := gl.RecordRead(call, g, read); !errors.As(err, &pe) || pe.Code != port.ErrPartnerAuditUnavailable.Code {
		t.Fatalf("cùng mã yêu cầu ghi hai lần: %v", err)
	}
	if n := f.countIn(f.tenant, "pf_party.partner_read_log"); n != 1 {
		t.Fatalf("không thêm dòng: %d", n)
	}
	for name, bad := range map[string]struct {
		ctx  context.Context
		read port.PartnerRead
	}{
		"không có Caller":          {ctx, read},
		"Caller của tenant khác":   {apiclientport.WithCaller(ctx, apiclientport.Caller{Client: apiclientport.Client{ID: ids.New(), ClientTenantID: ids.New()}}), read},
		"loại không có trong cấp":  {call, port.PartnerRead{Kind: port.KindDPHubCapacity, Endpoint: "GET /v1/data/hub-capacities"}},
		"loại không phải dp_*":     {call, port.PartnerRead{Kind: port.KindCooperativeDirectory, Endpoint: "GET /v1/partner/cooperatives"}},
		"người dùng OIDC không có": {authz.WithPrincipal(ctx, authz.Principal{TenantID: verifier, ActorRef: ids.New()}), read},
	} {
		if err := gl.RecordRead(bad.ctx, g, bad.read); !errors.Is(err, port.ErrPartnerAuditUnavailable) {
			t.Fatalf("%s → party.partner_audit_unavailable: %v", name, err)
		}
	}
	if n := f.countIn(f.tenant, "pf_party.partner_read_log"); n != 1 {
		t.Fatalf("lần ghi bị từ chối không thêm dòng: %d", n)
	}
}
