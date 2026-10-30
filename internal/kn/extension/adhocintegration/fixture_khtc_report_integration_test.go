// Test fixture copied from existing KN77 helpers only, without duplicating legacy Test cases.
// Colocated probes may inspect their own module internals; composition-root tests remain port-only.
package adhocintegration_test

import (
	"net/http"
	"testing"

	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"

	golden "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/golden/khtc"

	"github.com/shopspring/decimal"
)

type reportFx struct {
	*kfFx
	src *goldenReader
}

func newReportFx(t *testing.T) *reportFx { return newReportFxWith(t, nil) }

func newReportFxWith(t *testing.T, wrapControl func(formsport.PeriodControl) formsport.PeriodControl) *reportFx {
	f := newKHTCFormFxWith(t, wrapControl)
	f.planBody["base_version"] = 1
	f.planBody["decision_no"] = "02/QD-KH"
	f.planBody["decision_date"] = "2026-01-01"
	f.planBody["effective_from"] = "2026-01-01"
	for _, entry := range f.planBody["entries"].([]map[string]any) {
		if entry["ref"] == f.itemMod {
			entry["source_key"] = "season:trong_trot:pl01:lua_td:luy_ke"
		}
		if entry["ref"] == f.price {
			entry["source_key"] = "sum:thong_tin_truyen_thong:price-tickets:tickets_collected"
		}
	}
	c, b := f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", f.planBody)
	f.ok(c, b, nil)
	src := &goldenReader{}
	src.set([]golden.SourcePeriod{{Key: "2026-W40", To: "2026-10-06", Closed: true, Cumulative: decimal.NewFromInt(100)}, {Key: "2026-W41", To: "2026-10-13", Closed: true, Cumulative: decimal.NewFromInt(120)}}, "before")
	f.ext.UseKHTCSourceReader(src)
	return &reportFx{kfFx: f, src: src}
}
