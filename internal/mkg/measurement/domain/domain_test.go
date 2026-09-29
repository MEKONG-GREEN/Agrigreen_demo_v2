package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

func kg(s string) *mass.Kg          { k := mass.MustKg(s); return &k }
func pct(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

func hubInput(in, out, cust string, moisture *decimal.Decimal, rolls *port.RollTotal) ReconcileInput {
	return ReconcileInput{
		Route: port.RouteHub, Basis: port.PricingPerTonne, HubIn: kg(in), HubOut: kg(out), CustomerIn: kg(cust),
		HubInMoisture: moisture, Rolls: rolls, Tolerance: DefaultTolerance(), LossNorm: DefaultLossNorm(),
	}
}

// proto plant-weighing (seed mekong-green): việc 5800 cuộn cuộn / 5700 cuộn gom;
// ghe 1 xếp 4400 cuộn, ước 20 kg/cuộn = 88 t, nhà máy cân 138 400 − 41 600 = 96 800 kg
// → +10,00 % → CỜ (> 5 %), chưa nghiêm trọng (≤ 15 %); sản lượng quy đổi 22,000 kg/cuộn.
func TestGoldenPrototypeSeedRollVsMass(t *testing.T) {
	rolls := &port.RollTotal{Rolls: 4400, KgPerRollEstimate: mass.MustKg("20"), EstimateSource: "default"}
	r, err := Reconcile(ReconcileInput{
		Route: port.RouteDirect, Basis: port.PricingPerTonne, CustomerIn: kg("96800"), Rolls: rolls,
		Tolerance: DefaultTolerance(), LossNorm: DefaultLossNorm(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.RollEstimate.String() != "88000.000" || r.RollVariancePct.StringFixed(2) != "10.00" || r.RollCheck != CheckFlag {
		t.Fatalf("ước %s, chênh %s, trạng thái %s", r.RollEstimate, r.RollVariancePct, r.RollCheck)
	}
	if r.Yield.String() != "22.000" || !r.YieldAggregated || r.Outcome != port.OutcomeDirectAggregate {
		t.Fatalf("yield %s gộp %v outcome %s", r.Yield, r.YieldAggregated, r.Outcome)
	}
	// Cùng số liệu, hợp đồng theo cuộn: không cờ, chỉ tham khảo (RB-6 cấu hình theo cách tính giá).
	r2, _ := Reconcile(ReconcileInput{Route: port.RouteDirect, Basis: port.PricingPerBale, CustomerIn: kg("96800"), Rolls: rolls,
		Tolerance: DefaultTolerance(), LossNorm: DefaultLossNorm()})
	if r2.RollCheck != RollInformational {
		t.Fatalf("per_bale: %s", r2.RollCheck)
	}
	// Ghe cân 101 200 kg (+15,00 %) vẫn chỉ cờ; 101 300 (+15,11 %) → nghiêm trọng.
	for mass, want := range map[string]string{"101200": CheckFlag, "101300": CheckCritical, "92400": CheckWithin, "83600": CheckWithin} {
		r3, _ := Reconcile(ReconcileInput{Route: port.RouteDirect, Basis: port.PricingPerTonne, CustomerIn: kg(mass), Rolls: rolls,
			Tolerance: DefaultTolerance(), LossNorm: DefaultLossNorm()})
		if r3.RollCheck != want {
			t.Fatalf("%s: %s (%s), muốn %s", mass, r3.RollCheck, r3.RollVariancePct, want)
		}
	}
}

func TestGoldenHubRouteInternalAndCommercial(t *testing.T) {
	cases := []struct {
		name                               string
		in, out, cust                      string
		moisture                           *decimal.Decimal
		expected, abnormal, internal       string
		diff, diffPct, commercial, outcome string
	}{
		// ẩm 18 % → định mức 3 % → dự kiến 2 904; hao 3 800 → bất thường 896 (0,93 % ≤ 1 %) → bình thường.
		{"trong ngưỡng", "96800", "93000", "92500", pct("18.00"), "2904.000", "896.000", InternalNormal, "-500.000", "-0.54", CheckWithin, port.OutcomeWithinTolerance},
		// hao 4 800 → bất thường 1 896 (1,96 %) → cảnh báo; chỉ phần bất thường cảnh báo.
		{"hao hụt bất thường", "96800", "92000", "92000", pct("18.00"), "2904.000", "1896.000", InternalAbnormal, "0.000", "0.00", CheckWithin, port.OutcomeLossAlert},
		// không đo ẩm → định mức mặc định 3 %.
		{"không đo ẩm", "10000", "9800", "9790", nil, "300.000", "0.000", InternalNormal, "-10.000", "-0.10", CheckWithin, port.OutcomeWithinTolerance},
		// HUB_OUT > HUB_IN: cân không nhất quán → cảnh báo.
		{"tăng khối lượng", "10000", "10100", "10100", pct("10.00"), "100.000", "0.000", InternalGain, "0.000", "0.00", CheckWithin, port.OutcomeLossAlert},
		// khách cân thiếu 10,53 % → vượt cờ 5 % → tranh chấp (ưu tiên hơn cảnh báo hao hụt).
		{"tranh chấp cờ", "96800", "95000", "85000", pct("14.99"), "968.000", "832.000", InternalNormal, "-10000.000", "-10.53", CheckFlag, port.OutcomeDisputeOpened},
		{"tranh chấp nghiêm trọng", "96800", "95000", "80000", pct("26.00"), "9680.000", "0.000", InternalNormal, "-15000.000", "-15.79", CheckCritical, port.OutcomeDisputeOpened},
		// đúng 5,00 % chưa vượt.
		{"biên 5 %", "100000", "100000", "95000", pct("0.00"), "1000.000", "0.000", InternalNormal, "-5000.000", "-5.00", CheckWithin, port.OutcomeWithinTolerance},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := Reconcile(hubInput(c.in, c.out, c.cust, c.moisture, nil))
			if err != nil {
				t.Fatal(err)
			}
			want := c.internal
			got := []string{r.ExpectedLoss.String(), r.AbnormalLoss.String(), r.InternalStatus, r.CommercialDiff.String(), r.CommercialDiffPct.StringFixed(2), r.CommercialStatus}
			exp := []string{c.expected, c.abnormal, want, c.diff, c.diffPct, c.commercial}
			for i := range got {
				if got[i] != exp[i] {
					t.Fatalf("trường %d: %q, muốn %q (%v)", i, got[i], exp[i], got)
				}
			}
			if r.Outcome != c.outcome || r.RollCheck != RollPending || r.YieldAggregated {
				t.Fatalf("outcome %s roll %s", r.Outcome, r.RollCheck)
			}
		})
	}
}

func TestReconcileIncompleteAndDirect(t *testing.T) {
	in := hubInput("1", "1", "1", nil, nil)
	in.CustomerIn = nil
	if _, err := Reconcile(in); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("thiếu CUSTOMER_IN: %v", err)
	}
	// DIRECT: không tách hao hụt dù có số cuộn.
	r, err := Reconcile(ReconcileInput{Route: port.RouteDirect, Basis: port.PricingPerTonne, CustomerIn: kg("5000"),
		Tolerance: DefaultTolerance(), LossNorm: DefaultLossNorm()})
	if err != nil || r.InternalStatus != StatusNotApplicable || r.CommercialStatus != StatusNotApplicable || r.ExpectedLoss != nil {
		t.Fatalf("DIRECT: %+v %v", r, err)
	}
}

func TestLossNormBands(t *testing.T) {
	n := DefaultLossNorm()
	if err := n.Validate(); err != nil {
		t.Fatal(err)
	}
	for m, want := range map[string]string{"0.00": "1", "14.99": "1", "15.00": "3", "24.99": "6", "25.00": "10", "100.00": "10"} {
		if got := n.LossPctFor(pct(m)); !got.Equal(decimal.RequireFromString(want)) {
			t.Fatalf("ẩm %s: %s muốn %s", m, got, want)
		}
	}
	bad := LossNorm{Bands: []Band{{"0.00", "10.00", "1.00"}, {"12.00", "20.00", "2.00"}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("khoảng hở phải lỗi")
	}
	tol := DefaultTolerance()
	tol.CommercialCrit = decimal.NewFromInt(1)
	if tol.Validate() == nil {
		t.Fatal("nghiêm trọng < cờ phải lỗi")
	}
}

func TestNormalizeDraftRules(t *testing.T) {
	ev := ids.New()
	base := func() port.TicketDraft {
		return port.TicketDraft{ID: ids.New(), IdemKey: "k-1", LotRef: ids.New(), Point: port.PointHubIn, Route: port.RouteHub,
			Source: port.SourceEdge, MassKg: mass.MustKg("100"), EventTime: time.Now(), ActorRef: ids.New(),
			Integration: &port.Integration{Window: port.Window{DeviceID: "SCALE-01", Epoch: 1, CounterFrom: 1, CounterTo: 5},
				NetKg: mass.MustKg("100"), BatchIDs: []uuid.UUID{ids.New()}, RuleVersion: "weigh.integrate.v1"}}
	}
	ok := base()
	if err := NormalizeDraft(&ok); err != nil || ok.MoistureState != port.MoistureNotMeasured {
		t.Fatalf("hợp lệ: %v %s", err, ok.MoistureState)
	}
	bad := map[string]func(d *port.TicketDraft){
		"edge thiếu integration": func(d *port.TicketDraft) { d.Integration = nil },
		"edge thiếu rule":        func(d *port.TicketDraft) { d.Integration.RuleVersion = "" },
		"edge CUSTOMER_IN":       func(d *port.TicketDraft) { d.Point = port.PointCustomerIn },
		"DIRECT HUB_IN":          func(d *port.TicketDraft) { d.Route = port.RouteDirect },
		"mass ≠ net":             func(d *port.TicketDraft) { d.MassKg = mass.MustKg("99") },
		"tay thiếu bằng chứng": func(d *port.TicketDraft) {
			d.Source, d.Integration, d.Reason = port.SourceManualEvidence, nil, "Cân hub hỏng cảm biến"
		},
		"tay thiếu lý do": func(d *port.TicketDraft) {
			d.Source, d.Integration, d.EvidenceRef = port.SourceManualEvidence, nil, &ev
		},
		"khách ở HUB_IN":   func(d *port.TicketDraft) { d.Source, d.Integration = port.SourceCustomerIntegration, nil },
		"bù thiếu lý do":   func(d *port.TicketDraft) { d.Supersedes = &ev },
		"id không phải v7": func(d *port.TicketDraft) { d.ID = uuid.New() },
		"ẩm 3 chữ số":      func(d *port.TicketDraft) { d.MoisturePct = "12.345" },
	}
	for name, mut := range bad {
		d := base()
		mut(&d)
		if err := NormalizeDraft(&d); !errors.Is(err, port.ErrInvalidTicket) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	gap := base()
	gap.Integration.Gaps = []int64{3}
	if err := NormalizeDraft(&gap); !errors.Is(err, port.ErrWindowIncomplete) {
		t.Fatalf("cửa sổ thiếu: %v", err)
	}
}

func TestDisputeLifecycleAndBucket(t *testing.T) {
	s, err := NextDisputeStatus(DisputeOpen, ActStartReview)
	if err != nil || s != DisputeUnderReview {
		t.Fatal(s, err)
	}
	if _, err := NextDisputeStatus(DisputeOpen, ActResolve); !errors.Is(err, ErrDisputeTransition) {
		t.Fatal("resolve khi chưa xem xét phải lỗi")
	}
	if _, err := NextDisputeStatus(DisputeResolved, ActAddEvidence); !errors.Is(err, ErrDisputeTransition) {
		t.Fatal("đã đóng không nhận bằng chứng")
	}
	for k, want := range map[string]string{"0": "0-500", "499.999": "0-500", "500": "500-1000", "9999.999": "5000-10000", "96800": "10000+"} {
		if got := Bucket(mass.MustKg(k)); got != want {
			t.Fatalf("%s: %s", k, got)
		}
	}
	sh, err := ShareOf(mass.MustKg("96800"), "0.250000")
	if err != nil || sh.String() != "24200.000" {
		t.Fatal(sh, err)
	}
}
