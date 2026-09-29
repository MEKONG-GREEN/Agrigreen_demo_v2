package handler_test

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// Mã đơn vị theo danh mục chính thức sau sắp xếp 2025 (docs/requirements/danh-muc-hanh-chinh.md):
// An Giang 91 (Phường Bình Đức 30292, Phường Mỹ Thới 30301), Cần Thơ 92 (Phường Cái Khế 31120).
const (
	pAG, cBinhDuc, cMyThoi = "91", "30292", "30301"
	pCT, cCaiKhe           = "92", "31120"
)

type fakeUnits struct{ units map[string]mdport.AdminUnit }

func newFakeUnits() *fakeUnits {
	f := &fakeUnits{units: map[string]mdport.AdminUnit{}}
	add := func(code, name string, lvl mdport.Level, parent string) {
		f.units[code] = mdport.AdminUnit{Code: code, Name: name, Level: lvl, ParentCode: parent}
	}
	add(pAG, "An Giang", mdport.LevelProvince, "")
	add(cBinhDuc, "Phường Bình Đức", mdport.LevelCommune, pAG)
	add(cMyThoi, "Phường Mỹ Thới", mdport.LevelCommune, pAG)
	add(pCT, "Cần Thơ", mdport.LevelProvince, "")
	add(cCaiKhe, "Phường Cái Khế", mdport.LevelCommune, pCT)
	return f
}

func (f *fakeUnits) Get(_ context.Context, code string, _ time.Time) (mdport.AdminUnit, error) {
	u, ok := f.units[code]
	if !ok {
		return mdport.AdminUnit{}, mdport.ErrUnitNotFound
	}
	return u, nil
}

func (f *fakeUnits) Descendants(_ context.Context, code string, _ time.Time) ([]string, error) {
	out := []string{code}
	for c, u := range f.units {
		if u.ParentCode == code {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeUnits) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}

type fakeParams struct {
	mu sync.Mutex
	v  map[string]decimal.Decimal
}

func (f *fakeParams) set(key, val string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.v[key] = decimal.RequireFromString(val)
}

func (f *fakeParams) At(_ context.Context, key string, at time.Time) (mdport.ParamValue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.v[key]
	if !ok {
		return mdport.ParamValue{}, mdport.ErrParamNotFound
	}
	return mdport.ParamValue{Key: key, Value: v, Version: 1, EffectiveFrom: at}, nil
}

type fakeCatalogs struct{}

var catalogNames = map[string]string{
	"season/HT": "Hè Thu", "season/DX": "Đông Xuân", "rice_variety/ST25": "ST25", "rice_variety/OM5451": "OM 5451", "pest/ray_nau": "Rầy nâu",
	"farm_stage/lam_dat": "Làm đất", "machine_type/MLD": "Máy làm đất",
}

func (fakeCatalogs) Get(_ context.Context, catalog, code string) (mdport.CatalogItem, error) {
	n, ok := catalogNames[catalog+"/"+code]
	if !ok {
		return mdport.CatalogItem{}, mdport.ErrCatalogItemNotFound
	}
	return mdport.CatalogItem{Catalog: catalog, Code: code, Name: n, Active: true}, nil
}

func (fakeCatalogs) List(context.Context, string) ([]mdport.CatalogItem, error) { return nil, nil }

// orgTree: tổ chức giả + phạm vi theo org_ref (thay pf/party).
type orgTree struct {
	orgs  map[uuid.UUID]partyport.Org
	scope map[uuid.UUID]authz.Scope

	national, pAG, pCT, tBinhDuc, tCaiKhe, dcrd, coop, coopCaiKhe uuid.UUID
}

func newOrgTree() *orgTree {
	o := &orgTree{orgs: map[uuid.UUID]partyport.Org{}, scope: map[uuid.UUID]authz.Scope{}}
	add := func(kind partyport.OrgKind, unit, name string, sc authz.Scope) uuid.UUID {
		ref := ids.New()
		o.orgs[ref] = partyport.Org{Ref: ref, Kind: kind, Code: "HTX-" + unit, Name: name, AdminUnitCode: unit, Active: true}
		o.scope[ref] = sc
		return ref
	}
	o.national = add(partyport.OrgTTKNNational, "", "TTKN Quốc gia", authz.Scope{All: true})
	o.dcrd = add(partyport.OrgDCRD, "", "Cục KTHT", authz.Scope{All: true})
	o.pAG = add(partyport.OrgTTKNProvince, pAG, "TTKN An Giang", authz.Scope{Units: []string{pAG, cBinhDuc, cMyThoi}})
	o.pCT = add(partyport.OrgTTKNProvince, pCT, "TTKN Cần Thơ", authz.Scope{Units: []string{pCT, cCaiKhe}})
	o.tBinhDuc = add(partyport.OrgExtensionTeam, cBinhDuc, "Tổ KNCĐ Bình Đức", authz.Scope{Units: []string{cBinhDuc}})
	o.tCaiKhe = add(partyport.OrgExtensionTeam, cCaiKhe, "Tổ KNCĐ Cái Khế", authz.Scope{Units: []string{cCaiKhe}})
	o.coop = add(partyport.OrgCooperative, cBinhDuc, "HTX Lúa Xanh Bình Đức", authz.Scope{Units: []string{cBinhDuc}})
	o.coopCaiKhe = add(partyport.OrgCooperative, cCaiKhe, "HTX Cái Khế", authz.Scope{Units: []string{cCaiKhe}})
	return o
}

func (o *orgTree) Get(_ context.Context, ref uuid.UUID) (partyport.Org, error) {
	org, ok := o.orgs[ref]
	if !ok {
		return partyport.Org{}, partyport.ErrOrgNotFound
	}
	return org, nil
}

func (o *orgTree) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, nil
	}
	return o.scope[p.OrgRef], nil
}

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func dp(s string) *decimal.Decimal {
	x := d(s)
	return &x
}

// fakeExt là cổng dữ liệu pf/extension giả: số liệu cố định, ghi lại truy vấn.
type fakeExt struct {
	mu   sync.Mutex
	last extport.ReportQuery
}

func (f *fakeExt) record(q extport.ReportQuery) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = q
}

func (f *fakeExt) lastQuery() extport.ReportQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

func inScope(sc authz.Scope, commune, province string) bool {
	return sc.All || sc.Allows(commune) || sc.Allows(province)
}

func (f *fakeExt) Sowing(_ context.Context, q extport.ReportQuery) (extport.SowingData, error) {
	f.record(q)
	all := []extport.SowingRow{
		{ProvinceCode: pAG, CommuneCode: cBinhDuc, SeasonCode: "HT", VarietyCode: "ST25", SownAreaHa: d("1234.5678"), Cooperatives: 2, Reports: 3},
		{ProvinceCode: pAG, CommuneCode: cBinhDuc, SeasonCode: "HT", VarietyCode: "OM5451", SownAreaHa: d("0.1"), Cooperatives: 1, Reports: 1},
		{ProvinceCode: pCT, CommuneCode: cCaiKhe, SeasonCode: "HT", VarietyCode: "ST25", SownAreaHa: d("10"), Cooperatives: 1, Reports: 1},
	}
	var out extport.SowingData
	for _, r := range all {
		if inScope(q.Scope, r.CommuneCode, r.ProvinceCode) {
			out.Rows = append(out.Rows, r)
		}
	}
	out.Completeness = extport.Completeness{ExpectedCommunes: 2, ReportedCommunes: 1, MissingCommunes: []string{cMyThoi}, Provisional: true}
	return out, nil
}

func (f *fakeExt) GrowthPest(_ context.Context, q extport.ReportQuery) (extport.PestData, error) {
	f.record(q)
	return extport.PestData{}, nil
}

func (f *fakeExt) ProgramProgress(_ context.Context, q extport.ReportQuery) (extport.ProgramData, error) {
	f.record(q)
	return extport.ProgramData{Targets: map[string]*decimal.Decimal{}}, nil
}

func (f *fakeExt) TasksSLA(_ context.Context, q extport.ReportQuery) (extport.TaskData, error) {
	f.record(q)
	return extport.TaskData{}, nil
}

func (f *fakeExt) PeriodSummary(_ context.Context, q extport.ReportQuery) (extport.SummaryData, error) {
	f.record(q)
	return extport.SummaryData{}, nil
}

// fakeMech trả "kết quả cân đối đã lưu" cố định — số có 4 chữ số thập phân để
// kiểm tệp xuất giữ nguyên, không làm tròn hay tính lại.
type fakeMech struct {
	run       mechport.RunInfo
	coop      uuid.UUID
	shortages []mechport.ShortageRow
}

func newFakeMech(coop uuid.UUID) *fakeMech {
	w := 5
	return &fakeMech{
		coop: coop,
		run: mechport.RunInfo{RunID: ids.New(), SeasonCode: "DX", SeasonYear: 2025, ComputedAt: time.Date(2026, 9, 20, 2, 0, 0, 0, time.UTC),
			RuleVersion: "cgh.balance/v1", ThresholdsComplete: true},
		shortages: []mechport.ShortageRow{{
			ResultID: ids.New(), CooperativeRef: coop, CooperativeCode: "HTX000001", CooperativeName: "HTX Lúa Xanh Bình Đức",
			ProvinceCode: pAG, CommuneCode: cBinhDuc, FarmStage: "lam_dat", MachineType: "MLD", Capacity: 3,
			Required: dp("4.9999"), RequiredWhole: &w, Shortage: 2, CoveragePct: dp("60.0012"), Status: "attention", AreaSource: "manual",
		}},
	}
}

func (f *fakeMech) Coverage(_ context.Context, q mechport.ReportQuery) (mechport.CoverageData, error) {
	w := 12
	return mechport.CoverageData{Run: f.run, Rows: []mechport.CoverageRow{{
		ProvinceCode: pAG, FarmStage: "lam_dat", MachineType: "MLD", Cooperatives: 2,
		Figures: mechport.Figures{Required: dp("11.2345"), RequiredWhole: &w, Capacity: 9, CoveragePct: dp("80.1103"), Status: "attention", StatusLabel: "Cần chú ý"},
	}}}, nil
}

func (f *fakeMech) Shortage(_ context.Context, q mechport.ReportQuery) (mechport.ShortageData, error) {
	return mechport.ShortageData{Run: f.run, Rows: f.shortages}, nil
}

func (f *fakeMech) SeasonCompare(_ context.Context, q mechport.CompareQuery) (mechport.CompareData, error) {
	return mechport.CompareData{RunA: f.run, RunB: f.run}, nil
}

// fakeDirectory: email của tài khoản (pf/accounts giả).
type fakeDirectory struct {
	emails  map[uuid.UUID]string
	members map[uuid.UUID]uuid.UUID // actor_ref → org_ref (tài khoản đã đăng ký của tổ chức)
}

// OrgRecipients: tài khoản của tổ chức có email (HTX US-DASH-03).
func (f fakeDirectory) OrgRecipients(_ context.Context, _ uuid.UUID, org uuid.UUID) ([]port.Recipient, error) {
	out := []port.Recipient{}
	for actor, o := range f.members {
		if e, ok := f.emails[actor]; ok && o == org {
			out = append(out, port.Recipient{ActorRef: actor, DisplayName: "Cán bộ HTX", MaskedEmail: e[:2] + "•••"})
		}
	}
	return out, nil
}

func (f fakeDirectory) EmailOf(_ context.Context, _ uuid.UUID, actor uuid.UUID) (string, error) {
	e, ok := f.emails[actor]
	if !ok {
		return "", port.ErrNoEmail
	}
	return e, nil
}

type fakeMailer struct {
	mu   sync.Mutex
	sent []port.Message
}

func (f *fakeMailer) Send(_ context.Context, m port.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeMailer) messages() []port.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]port.Message(nil), f.sent...)
}

var (
	_ partyport.Orgs          = (*orgTree)(nil)
	_ partyport.ScopeResolver = (*orgTree)(nil)
	_ mdport.AdminUnits       = (*fakeUnits)(nil)
	_ mdport.Params           = (*fakeParams)(nil)
	_ mdport.Catalogs         = fakeCatalogs{}
	_ extport.ReportData      = (*fakeExt)(nil)
	_ mechport.ReportData     = (*fakeMech)(nil)
	_ ccport.ReportData       = (*fakeCC)(nil)
	_ port.MailDirectory      = fakeDirectory{}
	_ port.RecipientDirectory = fakeDirectory{}
	_ port.Mailer             = (*fakeMailer)(nil)
)
