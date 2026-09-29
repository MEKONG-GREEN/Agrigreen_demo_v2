package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// GIS US-INT-02: cổng port.Layer cho bản đồ chuẩn hóa của pf/geo — tuyến của phiên bản đang
// phát hành trong khung nhìn (không có tuyến nháp), cờ đi được theo tải trọng, phân trang nội bộ
// tới limit; tenant khác không có phiên bản.
func TestLayerPortForStandardMap_GIS_US_INT_02(t *testing.T) {
	f := newFixture(t)
	ed := f.as(f.tenant, port.RoleEditor)
	ed.seedNetwork()
	no, _ := ed.build()
	expect(t, f.as(f.tenant, port.RolePublisher).adm(http.MethodPost, fmt.Sprintf("/v1/waterway/networks/%d/publish", no), nil), 200, "")
	ctx := tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{TenantID: f.tenant, ActorRef: ids.New(),
		Roles: []string{"map_consumer"}}), f.tenant)
	pg, err := f.mod.Layer().SegmentsInBBox(ctx, 105.9, 13.9, 106.2, 14.3, decimal.NewFromInt(100), 1000)
	if err != nil || pg.VersionNo != no || len(pg.Items) != 3 || pg.Truncated {
		t.Fatalf("lớp đường thủy: %+v %v", pg, err)
	}
	passable := 0
	for _, s := range pg.Items {
		if s.Passable {
			passable++
		}
		if len(s.Line) < 2 || s.Name == "Nháp thẳng" {
			t.Fatalf("tuyến: %+v", s)
		}
	}
	if pg.PassableCount != passable {
		t.Fatalf("đếm tuyến đi được: %d ≠ %d", pg.PassableCount, passable)
	}
	if small, err := f.mod.Layer().SegmentsInBBox(ctx, 105.9, 13.9, 106.2, 14.3, decimal.Zero, 2); err != nil || len(small.Items) != 2 || !small.Truncated {
		t.Fatalf("giới hạn: %+v %v", small, err)
	}
	other := ids.New()
	octx := tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{TenantID: other, ActorRef: ids.New()}), other)
	if o, err := f.mod.Layer().SegmentsInBBox(octx, 105.9, 13.9, 106.2, 14.3, decimal.Zero, 1000); err != nil || o.VersionNo != 0 || len(o.Items) != 0 {
		t.Fatalf("tenant khác: %+v %v", o, err)
	}
}
