package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// PlotFacts cài geoport.PlotFacts trên thửa giả (tenant phải có trong context như pf/geo thật).
func (f *fakePlots) PlotFacts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]geoport.Plot, error) {
	if _, err := tenant.FromContext(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID]geoport.Plot{}
	for _, r := range refs {
		if p, ok := f.plots[r]; ok {
			out[r] = p
		}
	}
	return out, nil
}

// fakeUnits: mọi xã 311xx thuộc tỉnh 92 (Cần Thơ, sau sắp xếp 2025 chỉ tỉnh → xã).
type fakeUnits struct{}

func (fakeUnits) Get(ctx context.Context, code string, _ time.Time) (mdport.AdminUnit, error) {
	if _, err := tenant.FromContext(ctx); err != nil {
		return mdport.AdminUnit{}, err
	}
	switch {
	case code == "92":
		return mdport.AdminUnit{Code: "92", Level: mdport.LevelProvince}, nil
	case strings.HasPrefix(code, "311"):
		return mdport.AdminUnit{Code: code, Level: mdport.LevelCommune, ParentCode: "92"}, nil
	}
	return mdport.AdminUnit{}, mdport.ErrUnitNotFound
}
func (fakeUnits) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (fakeUnits) Current(context.Context, string, time.Time) ([]string, error)     { return nil, nil }

type fakeCheckpoint struct {
	mu    sync.Mutex
	allow bool
	seen  []meteringport.Egress
}

func (c *fakeCheckpoint) Pass(_ context.Context, e meteringport.Egress) (meteringport.Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, e)
	return meteringport.Decision{Allowed: c.allow}, nil
}

func (c *fakeCheckpoint) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}

// G5 D-01 (collection_volume v1, proto SUB-DATA; C-3 k tạm): ô xã × vụ × tháng chỉ khi ≥ k nông hộ
// phân biệt; ô dưới k và ô ẩn bổ sung (publicdata.kanon.v1) KHÔNG BAO GIỜ trả, chỉ đếm Suppressed
// trên cả bộ lọc; bộ lọc không đổi việc ẩn; phần tử chỉ có trường hợp đồng (không thửa/nông hộ/HTX);
// tenant khác → rỗng. ADR-0024 (L-05): consumer van collection gọi Guard kênh valve; enforce + từ
// chối → lỗi, không ghi bảng kê.
func TestCollectionVolumeKAnonymity_G5_D01(t *testing.T) {
	f := newFixture(t)
	f.mod.UseCollectionVolume(f.plots, fakeUnits{})
	cp := &fakeCheckpoint{allow: false}
	f.mod.UseEgress(cp, meteringport.ModeObserve, nil)
	f.publishedSOP("SOP OM5451", "OM5451")
	adm := f.platformAdmin()
	area := decimal.RequireFromString("1.5000")
	plot := func(commune string, coop uuid.UUID) uuid.UUID {
		ref := ids.New()
		f.plots.put(geoport.Plot{Ref: ref, Code: "G5" + ref.String()[28:], CommuneCode: commune, CooperativeRef: coop, HouseholdRef: ids.New(),
			Status: geoport.StatusActive, Version: 1, AreaHa: area}, 0)
		return ref
	}
	// 31117: 4 nông hộ (≥ k = 3) · 31121: 3 nông hộ (= k, nhưng nhỏ nhất của hàng → ẩn bổ sung) ·
	// 31120: 2 nông hộ (< k → ẩn sơ cấp).
	type pl struct {
		ref   uuid.UUID
		coop  uuid.UUID
		rolls int
	}
	var all []pl
	for range 4 {
		all = append(all, pl{plot("31117", f.coopA), f.coopA, 10})
	}
	for range 3 {
		all = append(all, pl{plot("31121", f.coopA), f.coopA, 7})
	}
	for range 2 {
		all = append(all, pl{plot("31120", f.coopB), f.coopB, 5})
	}
	for _, p := range all {
		season := adm.mustOpen(p.ref)
		pay := statementPayload(ids.New(), p.coop, p.ref, p.rolls, 1)
		pay["season_ref"] = season
		if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, pay); err != nil {
			t.Fatalf("van bảng kê (observe): %v", err)
		}
	}
	// Bảng kê chưa gắn vụ: lưu, nhưng không thuộc ô nào.
	if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, statementPayload(ids.New(), f.coopA, all[0].ref, 99, 1)); err != nil {
		t.Fatal(err)
	}
	if cp.n() != len(all)+1 {
		t.Fatalf("mỗi phong bì × bên cấp một Egress kênh valve: %d", cp.n())
	}
	cp.mu.Lock()
	e := cp.seen[0]
	cp.mu.Unlock()
	if e.Channel != meteringport.ChannelValve || e.DataTenantID != f.mkg || e.CounterpartyRef != f.tenant || e.Product != port.ContractHTXCollectionStatement {
		t.Fatalf("Egress van collection: %+v", e)
	}

	src := f.mod.CollectionVolumeSource()
	ctx := context.Background()
	q := apiclientport.CollectionQuery{DataTenant: f.tenant, K: 3}
	pg, err := src.CollectionCells(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 1 || pg.Suppressed != 2 || pg.Next != "" {
		t.Fatalf("một ô công bố, hai ô ẩn (sơ cấp + bổ sung): %+v", pg)
	}
	it := pg.Items[0]
	if it.CommuneCode != "31117" || it.ProvinceCode != "92" || it.SeasonCode != "2026-DX" || it.PeriodMonth != "2026-09" ||
		it.RollsCollected != 40 || it.AreaHaCollected != "6.00" || it.KApplied != 3 {
		t.Fatalf("ô 31117: %+v", it)
	}
	p, err := dataproduct.Get(apiclientport.ProductCollectionVolume)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(it)
	if err := p.Validate(raw); err != nil {
		t.Fatalf("phần tử ngoài hợp đồng: %v %s", err, raw)
	}
	for _, banned := range []string{all[0].ref.String(), f.coopA.String(), "household", "plot", "cooperative", "31120", "31121"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("phần tử không được có %q: %s", banned, raw)
		}
	}
	// k = 10 (mặc định tạm C-3): mọi ô dưới k → không trả gì.
	if pg10, err := src.CollectionCells(ctx, apiclientport.CollectionQuery{DataTenant: f.tenant, K: 10}); err != nil || len(pg10.Items) != 0 || pg10.Suppressed != 3 {
		t.Fatalf("k = 10: %+v %v", pg10, err)
	}
	// Bộ lọc không làm lộ ô ẩn: chỉ chọn ô sau khi ẩn.
	for name, fq := range map[string]apiclientport.CollectionQuery{
		"cùng vụ":   {DataTenant: f.tenant, K: 3, SeasonCode: "2026-DX"},
		"tháng 09":  {DataTenant: f.tenant, K: 3, FromMonth: "2026-09", ToMonth: "2026-09"},
		"tỉnh 92":   {DataTenant: f.tenant, K: 3, ProvinceCode: "92"},
		"giới hạn1": {DataTenant: f.tenant, K: 3, Limit: 1},
	} {
		got, err := src.CollectionCells(ctx, fq)
		if err != nil || len(got.Items) != 1 || got.Items[0].CommuneCode != "31117" || got.Suppressed != 2 || got.Next != "" {
			t.Fatalf("%s: %+v %v", name, got, err)
		}
	}
	for name, fq := range map[string]apiclientport.CollectionQuery{
		"vụ khác":   {DataTenant: f.tenant, K: 3, SeasonCode: "2025-DX"},
		"tỉnh khác": {DataTenant: f.tenant, K: 3, ProvinceCode: "89"},
		"tháng sau": {DataTenant: f.tenant, K: 3, FromMonth: "2026-10"},
	} {
		if got, err := src.CollectionCells(ctx, fq); err != nil || len(got.Items) != 0 || got.Suppressed != 0 {
			t.Fatalf("%s: %+v %v", name, got, err)
		}
	}
	// Cách ly tenant: tenant phong bì MKG và tenant lạ không có ô.
	for _, tid := range []uuid.UUID{f.mkg, ids.New()} {
		if got, err := src.CollectionCells(ctx, apiclientport.CollectionQuery{DataTenant: tid, K: 3}); err != nil || len(got.Items) != 0 || got.Suppressed != 0 {
			t.Fatalf("tenant %s: %+v %v", tid, got, err)
		}
	}
	// Tham số sai.
	for name, bad := range map[string]apiclientport.CollectionQuery{
		"k < 2": {DataTenant: f.tenant, K: 1}, "tháng sai": {DataTenant: f.tenant, K: 3, FromMonth: "2026-13"},
		"tỉnh sai": {DataTenant: f.tenant, K: 3, ProvinceCode: "9"}, "thiếu tenant": {K: 3},
		"con trỏ sai": {DataTenant: f.tenant, K: 3, After: "!!"},
	} {
		if _, err := src.CollectionCells(ctx, bad); err == nil {
			t.Errorf("%s phải lỗi", name)
		}
	}

	// enforce + từ chối → lỗi metering.egress_quota_exceeded, bảng kê không ghi.
	f.mod.UseEgress(cp, meteringport.ModeEnforce, nil)
	extra := plot("31117", f.coopA)
	pay := statementPayload(ids.New(), f.coopA, extra, 50, 1)
	pay["season_ref"] = adm.mustOpen(extra)
	_, err = f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, pay)
	var pr *problem.Problem
	if !errors.As(err, &pr) || pr.Code != "metering.egress_quota_exceeded" {
		t.Fatalf("enforce + từ chối: %v", err)
	}
	if got, _ := src.CollectionCells(ctx, q); got.Items[0].RollsCollected != 40 {
		t.Fatalf("bảng kê bị chặn không được ghi: %+v", got)
	}
	cp.allow = true
	if _, err := f.deliver(f.mkg, port.EventMKGHTXCollectionStatementPublished, pay); err != nil {
		t.Fatalf("enforce + trong hạn mức: %v", err)
	}
	if got, _ := src.CollectionCells(ctx, q); got.Items[0].RollsCollected != 90 {
		t.Fatalf("sau khi cho qua: %+v", got)
	}
}
