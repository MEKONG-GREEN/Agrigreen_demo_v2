package handler_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// knModules là các module Khuyến nông có view công bố mà pf/metrics từng đọc (PA1 bước 2, #314).
var knModules = []string{"pf/extension", "pf/forms", "pf/price"}

// PA1 (#314): mã và định nghĩa của pf/metrics không còn nhắc tới view công bố của KN (schema suy từ tên module để
// chính tệp này không chứa tên schema).
func TestPA1NoKNPubViewReferences(t *testing.T) {
	var schemas []string
	for _, m := range knModules {
		schemas = append(schemas, "pf_"+strings.TrimPrefix(modreg.PubRole(m), "r_")+".")
	}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || (filepath.Ext(path) != ".go" && filepath.Ext(path) != ".yaml") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, s := range schemas {
			if strings.Contains(string(raw), s) {
				t.Errorf("%s còn đọc %s*", path, s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range app.PubSourceModules {
		for _, k := range knModules {
			if m == k {
				t.Errorf("PubSourceModules còn nguồn KN %s", m)
			}
		}
	}
}

// PA1 (#314): pf/metrics nối nguồn thật trên view _pub (như gốc lắp ráp) chạy xanh khi view KN trống: không có nguồn
// hay định nghĩa nào của KN, job ghi sổ một ngày không lỗi, khóa kn.* trả metrics.unknown_metric.
func TestPA1MetricsRunWithoutKNSources(t *testing.T) {
	m := metrics.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.System{}, metrics.Deps{})
	svc := m.Service()
	for _, k := range knModules {
		if m.PubSource(k) != nil || svc.Sources[k] != nil {
			t.Errorf("còn nguồn chỉ số của %s", k)
		}
	}
	for _, d := range svc.Catalog.All() {
		for _, k := range knModules {
			if d.SourceModule == k || strings.HasPrefix(string(d.Key), "kn.") {
				t.Errorf("còn định nghĩa KN: %s (%s)", d.Key, d.SourceModule)
			}
		}
	}
	if _, err := svc.RecordDay(context.Background(), ids.New(), yesterday(), nil); err != nil {
		t.Fatalf("ghi sổ mọi chỉ số khi view KN trống: %v", err)
	}

	f := newFixture(t, clock.NewFixed(time.Now()))
	expect(t, f.as(f.tenant, all, "kn_national_admin").get("/v1/metrics/kn.sown_area_ha/values"), 404, "metrics.unknown_metric")
}
