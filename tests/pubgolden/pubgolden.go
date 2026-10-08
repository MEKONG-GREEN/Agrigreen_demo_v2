// Package pubgolden là kiểm thử ĐỐI CHỨNG (golden) của W6.3 (R-DB-10): nguồn chỉ số pf/metrics trên view công bố
// pf_<module>_pub phải trả ĐÚNG giá trị, watermark, lỗi và danh sách phạm vi như nguồn cũ (port.Source chạy bằng
// vai của module chủ) trên cùng dữ liệu đã gieo. Kiểm thử tích hợp của module chủ bọc nguồn cũ bằng Compare — mọi
// khẳng định có sẵn tự kiểm luôn nguồn _pub. Chỉ dùng trong kiểm thử. Nằm ngoài internal/ (như gốc lắp ráp: chỉ
// import gốc module pf/metrics và port/) để kiểm thử của module chủ dùng được mà không phá R-02.
package pubgolden

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// AsOfAter là nguồn tính với mốc recorded_at tường minh (nguồn cũ của module chủ và nguồn _pub đều có).
type AsOfAter interface {
	AsOfAt(ctx context.Context, key port.Key, d clock.Date, cutoff time.Time, sc port.Scope) (port.Value, error)
}

// pubSource là nguồn _pub của pf/metrics (metrics.Module.PubSource).
type pubSource interface {
	port.Source
	port.ScopeLister
	AsOfAter
}

// Golden gọi cả nguồn cũ lẫn nguồn _pub, hỏng kiểm thử khi lệch, trả kết quả của nguồn cũ.
type Golden struct {
	t   testing.TB
	old port.Source
	pub pubSource
	geo port.GeometryResolver
}

// Module dựng pf/metrics trên vai đăng nhập app_platform (SET LOCAL ROLE r_metrics) của database kiểm thử.
func Module(t testing.TB) *metrics.Module {
	t.Helper()
	return metrics.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, metrics.Deps{})
}

// Compare bọc nguồn cũ old của module chủ dữ liệu module (vd "pf/cropcycle").
func Compare(t testing.TB, module string, old port.Source) Golden {
	t.Helper()
	m := Module(t)
	var pub pubSource
	if p := m.PubSource(module); p != nil {
		pub = p
	}
	if pub == nil {
		t.Fatalf("pubgolden: %s không có nguồn _pub", module)
	}
	return Golden{t: t, old: old, pub: pub, geo: m.Service().Geometry}
}

// AsOfAt thực hiện AsOfAter.
func (g Golden) AsOfAt(ctx context.Context, key port.Key, d clock.Date, cutoff time.Time, sc port.Scope) (port.Value, error) {
	g.t.Helper()
	ov, oerr := g.old.(AsOfAter).AsOfAt(ctx, key, d, cutoff, sc)
	nv, nerr := g.pub.AsOfAt(ctx, key, d, cutoff, sc)
	if !sameErr(oerr, nerr) {
		g.t.Fatalf("pubgolden %s %v: lỗi lệch — cũ %v, _pub %v", key, sc, oerr, nerr)
	}
	if oerr == nil && !sameValue(ov, nv) {
		g.t.Fatalf("pubgolden %s %v: giá trị lệch\n cũ  %+v\n _pub %+v", key, sc, ov, nv)
	}
	return ov, oerr
}

// AsOf thực hiện port.Source (đối chứng cả lỗi ngày chưa kết thúc).
func (g Golden) AsOf(ctx context.Context, key port.Key, d clock.Date, sc port.Scope) (port.Value, error) {
	g.t.Helper()
	ov, oerr := g.old.AsOf(ctx, key, d, sc)
	nv, nerr := g.pub.AsOf(ctx, key, d, sc)
	if !sameErr(oerr, nerr) || (oerr == nil && !sameValue(ov, nv)) {
		g.t.Fatalf("pubgolden AsOf %s %v: cũ %+v %v, _pub %+v %v", key, sc, ov, oerr, nv, nerr)
	}
	return ov, oerr
}

// Scopes thực hiện port.ScopeLister.
func (g Golden) Scopes(ctx context.Context, key port.Key, d clock.Date, tid uuid.UUID) ([]port.Scope, error) {
	g.t.Helper()
	ol, oerr := g.old.(port.ScopeLister).Scopes(ctx, key, d, tid)
	ns, nerr := g.pub.Scopes(ctx, key, d, tid)
	if !sameErr(oerr, nerr) || !reflect.DeepEqual(ol, ns) {
		g.t.Fatalf("pubgolden Scopes %s: cũ %+v %v, _pub %+v %v", key, ol, oerr, ns, nerr)
	}
	return ol, oerr
}

// Geometries đối chứng port.GeometryResolver cũ (pf/geo) với pf_geo_pub.v1_layer_geometry.
func (g Golden) Geometries(ctx context.Context, old port.GeometryResolver, refs []port.GeometryRef) (map[port.GeometryRef]json.RawMessage, error) {
	g.t.Helper()
	om, oerr := old.Geometries(ctx, refs)
	nm, nerr := g.geo.Geometries(ctx, refs)
	if !sameErr(oerr, nerr) || len(om) != len(nm) {
		g.t.Fatalf("pubgolden Geometries: cũ %v %v, _pub %v %v", om, oerr, nm, nerr)
	}
	for k, v := range om {
		if string(nm[k]) != string(v) {
			g.t.Fatalf("pubgolden Geometries %v: cũ %s, _pub %s", k, v, nm[k])
		}
	}
	return om, oerr
}

var sentinels = []error{port.ErrNoData, port.ErrNotClosed, port.ErrUnknownMetric}

func sameErr(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	for _, s := range sentinels {
		if errors.Is(a, s) || errors.Is(b, s) {
			return errors.Is(a, s) && errors.Is(b, s)
		}
	}
	return a.Error() == b.Error()
}

func sameValue(a, b port.Value) bool {
	return a.Key == b.Key && a.DefinitionVersion == b.DefinitionVersion && a.BusinessDate == b.BusinessDate &&
		a.Scope.TenantID == b.Scope.TenantID && slices.Equal(a.Scope.Units, b.Scope.Units) && a.Scope.Dimension == b.Scope.Dimension &&
		reflect.DeepEqual(a.Scope.OrgRef, b.Scope.OrgRef) && a.Value.String() == b.Value.String() && a.Unit == b.Unit &&
		a.Cutoff.Equal(b.Cutoff) && a.Watermark.LastEventID == b.Watermark.LastEventID &&
		a.Watermark.RecordedAt.Equal(b.Watermark.RecordedAt) && a.ComputedAt.Equal(b.ComputedAt)
}
