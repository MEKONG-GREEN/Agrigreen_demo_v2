package integration

import (
	"errors"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
)

// W5.1: tám hợp đồng nhúng nạp được, đều "proposed" — phục vụ ngoài production, KHÔNG ở production.
func TestEmbeddedContractsLoad_W51(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"admin_units.v1", "collection_schedule.v1", "collection_volume_by_commune.v1", "harvest_progress_by_commune.v1",
		"htx_roll_confirmation.v1", "roll_count_statement.v1", "season_harvest_dates.v1", "waterway_routes.v1",
	}
	if len(all) != len(want) {
		t.Fatalf("muốn %d hợp đồng, có %d", len(want), len(all))
	}
	for i, c := range all {
		if c.ID() != want[i] {
			t.Fatalf("hợp đồng %d: muốn %s, có %s", i, want[i], c.ID())
		}
		if !c.Servable(false) || c.Servable(true) {
			t.Fatalf("%s: proposed phải phục vụ ngoài production và không ở production", c.ID())
		}
		if c.Record() == nil {
			t.Fatalf("%s: lược đồ bản ghi chưa giải", c.ID())
		}
	}
	for _, k := range []string{KindFeed, KindIngest} {
		if _, err := EnvelopeFor(k); err != nil {
			t.Fatal(err)
		}
	}
}

// ADR-0033 §3: hợp đồng nạp collection_schedule dùng NGUYÊN lược đồ van công bố.
func TestCollectionScheduleReusesPublication_W51(t *testing.T) {
	c, err := Get("collection_schedule", 1)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := publication.Get("collection_schedule")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Record().Properties) != len(pc.Properties) {
		t.Fatal("lược đồ phải trùng hợp đồng van")
	}
	if c.Flow != FlowBilateral || c.Bilateral.AuthorSide != "a" {
		t.Fatal("collection_schedule là song phương, bên a (MKG) ghi")
	}
}

// Mặc định từ chối: khóa ngoài hợp đồng (vd giá) → từ chối; hợp đồng không có → ErrDenied.
func TestValidateDefaultDeny_W51(t *testing.T) {
	c, err := Get("roll_count_statement", 1)
	if err != nil {
		t.Fatal(err)
	}
	ok := `{"statement_ref":"0199b8d1-0a00-7b01-8c02-d03e04f05a06","schedule_ref":null,"htx_ref":"01989e00-1111-7222-8333-944455566677","plot_ref":"01989f00-aa11-7b22-8c33-d44e55f66a77","season_ref":null,"collected_on":"2026-10-09","rolls":46,"revision":1}`
	if err := c.ValidateRecord([]byte(ok)); err != nil {
		t.Fatal(err)
	}
	if key, _ := c.EntityKeyOf([]byte(ok)); key != "0199b8d1-0a00-7b01-8c02-d03e04f05a06" {
		t.Fatalf("entity_key sai: %s", key)
	}
	bad := strings.Replace(ok, `"rolls":46`, `"rolls":46,"price_vnd":"900"`, 1)
	if err := c.ValidateRecord([]byte(bad)); !errors.Is(err, publication.ErrDenied) {
		t.Fatalf("khóa ngoài hợp đồng phải bị từ chối: %v", err)
	}
	if _, err := Get("household_payment", 1); !errors.Is(err, publication.ErrDenied) {
		t.Fatal("hợp đồng không có phải bị từ chối (household_payment chờ C-12)")
	}
}

// ADR-0035: k là tham số (C-4/K-8 chưa ký), không hằng trong mã.
func TestAggregateKIsParameter_W51(t *testing.T) {
	c, err := Get("harvest_progress_by_commune", 1)
	if err != nil {
		t.Fatal(err)
	}
	a := c.Aggregation
	if a == nil || a.KParam != "publicdata.k_min" || a.KDefault < 2 || !a.Complementary || a.Hysteresis < 1 {
		t.Fatalf("aggregation sai: %+v", a)
	}
	pending := strings.Join(c.Pending, ",")
	if !strings.Contains(pending, "C-4") {
		t.Fatal("k chưa ký: pending phải có C-4")
	}
}

func TestMetaProblems_W51(t *testing.T) {
	c, err := Parse([]byte(`{"name":"x_feed","version":1,"kind":"feed","flow":"bilateral","status":"approved","pending":["C-12"],
		"owner":"mkg/fieldops","proposed_by":"a","approved_by":"a","approved_at":"2026-10-06","purpose":"p","personal_data":true,
		"grant_class":"aggregate","entity_key":["k"],"type":"object","required":["k"],"properties":{"k":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(c.MetaProblems(), "\n")
	for _, w := range []string{"approved nhưng còn pending", "owner phải là module nền tảng", "approved_by phải khác proposed_by",
		"feed thiếu delivery", "flow bilateral cần grant_class bilateral", "grant_class aggregate khi và chỉ khi", "flow bilateral khi và chỉ khi"} {
		if !strings.Contains(got, w) {
			t.Errorf("thiếu %q trong:\n%s", w, got)
		}
	}
}
