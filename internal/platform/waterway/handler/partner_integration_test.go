package handler_test

import (
	"net/http"
	"testing"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/porttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// ADR-0020 + G4-0 (proto FM-05): bề mặt đối tác /v1/partner/waterway/route — tài khoản dịch vụ mkg_sync của
// tenant MKG định tuyến trên mạng đã phát hành của tenant BÊN CẤP khi có cấp quyền waterway_routes; không cấp
// quyền → 403; mỗi lời gọi ghi sổ đọc; sổ đọc lỗi → không trả dữ liệu; tenant MKG không thấy mạng của chính nó.
func TestPartnerWaterwayRoute_ADR0020(t *testing.T) {
	f := newFixture(t)
	share := porttest.New()
	f.mod.UseSharing(share)
	ed := f.as(f.tenant, port.RoleEditor, port.RolePublisher)
	ed.seedNetwork()
	if no, _ := ed.build(); no != 1 {
		t.Fatalf("phiên bản: %d", no)
	}
	expect(t, f.as(f.tenant, port.RolePublisher).adm(http.MethodPost, "/v1/waterway/networks/1/publish", nil), 200, "")

	mkg := ids.New()
	sync := f.as(mkg, partyport.RolePartnerSync)
	get := "/v1/partner/waterway/route?from=14.0,106.0&to=14.2,106.0"

	// Chưa có cấp quyền → 403, không ghi sổ đọc.
	expect(t, sync.app(http.MethodGet, get), 403, "party.share_grant_missing")
	// Cấp quyền loại khác không đủ.
	share.Grant(f.tenant, mkg, partyport.KindPlotDirectory)
	expect(t, sync.app(http.MethodGet, get), 403, "party.share_grant_missing")
	if share.ReadCount() != 0 {
		t.Fatalf("không được ghi sổ đọc khi từ chối: %d", share.ReadCount())
	}
	// Không có vai mkg_sync → 403.
	expect(t, f.as(mkg).app(http.MethodGet, get), 403, "forbidden")

	share.Grant(f.tenant, mkg, partyport.KindWaterwayRoutes)
	r := sync.app(http.MethodGet, get)
	expect(t, r, 200, "")
	legs, _ := r.body["legs"].([]any)
	if r.str("grantor_tenant_id") != f.tenant.String() || r.num("network_version") != 1 || len(legs) != 1 || r.str("distance_km") == "" {
		t.Fatalf("định tuyến trên mạng bên cấp: %v", r.body)
	}
	if share.ReadCount() != 1 || share.Reads[0].Kind != partyport.KindWaterwayRoutes || share.Reads[0].Endpoint != "GET /v1/partner/waterway/route" || share.Reads[0].Items != 1 {
		t.Fatalf("sổ đọc: %+v", share.Reads)
	}
	// Mạng của tenant MKG (không có) không được dùng: đường Application API thường của MKG vẫn là Haversine.
	own := f.as(mkg).app(http.MethodGet, "/v1/waterway/routes?from=14.0,106.0&to=14.2,106.0")
	if own.body["version_no"] != nil {
		t.Fatalf("tenant MKG không có mạng riêng: %v", own.body)
	}

	// POST: chuyến nhiều điểm dừng — một chặng mỗi cặp điểm liên tiếp; Idempotency-Key bắt buộc.
	body := map[string]any{"points": []map[string]any{{"lat": 14.0, "lng": 106.0}, {"lat": 14.1, "lng": 106.0}, {"lat": 14.2, "lng": 106.0}}}
	p := sync.do(f.app.URL, http.MethodPost, "/v1/partner/waterway/route", "", body)
	expect(t, p, 200, "")
	if legs, _ := p.body["legs"].([]any); len(legs) != 2 || p.num("network_version") != 1 {
		t.Fatalf("POST nhiều điểm: %v", p.body)
	}
	if share.ReadCount() != 2 || share.Reads[1].Endpoint != "POST /v1/partner/waterway/route" || share.Reads[1].Items != 2 {
		t.Fatalf("sổ đọc POST: %+v", share.Reads)
	}
	// Một điểm → 422.
	one := map[string]any{"points": []map[string]any{{"lat": 14.0, "lng": 106.0}}}
	expect(t, sync.do(f.app.URL, http.MethodPost, "/v1/partner/waterway/route", "", one), 422, "validation_failed")

	// Sổ đọc không ghi được → 503, không trả dữ liệu.
	share.FailAudit = true
	expect(t, sync.app(http.MethodGet, get), 503, "party.partner_audit_unavailable")
}
