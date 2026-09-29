package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
)

// SnapshotSchemaVersion là phiên bản hợp đồng contracts/optimizer/snapshot.v1.schema.json.
const SnapshotSchemaVersion = 1

// AlgorithmVersion là phiên bản thuật toán optimizer được ghim vào ảnh chụp mới
// (optimizer/src/agrigreen_optimizer/__init__.py ALGORITHM_VERSION).
const AlgorithmVersion = "agrigreen-optimizer/2026.09.24+py.1"

// NoNetwork là network_version khi chưa có mạng đường thủy đã phát hành.
const NoNetwork = "none"

// Snapshot là ảnh chụp đầu vào gửi optimizer. Thứ tự trường cố định → JSON tất
// định; số thập phân là chuỗi. KHÔNG có tên, toạ độ (RB-7).
type Snapshot struct {
	SchemaVersion       int                          `json:"schema_version"`
	SnapshotID          string                       `json:"snapshot_id"`
	SimulationID        string                       `json:"simulation_id"`
	TenantRef           string                       `json:"tenant_ref"`
	AlgorithmVersion    string                       `json:"algorithm_version"`
	NetworkVersion      string                       `json:"network_version"`
	CreatedAt           string                       `json:"created_at"`
	Scenario            SnapScenario                 `json:"scenario"`
	Hubs                []SnapHub                    `json:"hubs"`
	Cooperatives        []SnapCooperative            `json:"cooperatives"`
	Seasons             []SnapSeason                 `json:"seasons"`
	HarvestObservations map[string][]SnapObservation `json:"harvest_observations"`
	Radius              []SnapRadius                 `json:"radius"`
	Routes              []SnapRoute                  `json:"routes"`
	Params              SnapParams                   `json:"params"`
	Options             SnapOptions                  `json:"options"`
}

// SnapScenario là cấu hình kịch bản.
type SnapScenario struct {
	ScenarioID                 string    `json:"scenario_id"`
	Code                       string    `json:"code"`
	LifecycleYears             int       `json:"lifecycle_years"`
	Discounted                 bool      `json:"discounted"`
	BaselineMode               string    `json:"baseline_mode"`
	BaselineManualCostPerTon   *string   `json:"baseline_manual_cost_per_ton"`
	BaselineScenarioID         *string   `json:"baseline_scenario_id"`
	BaselineScenarioCostPerTon *string   `json:"baseline_scenario_cost_per_ton"`
	Plant                      SnapPlant `json:"plant"`
}

// SnapPlant là nhà máy đầu ra (không toạ độ).
type SnapPlant struct {
	PlantRef         *string `json:"plant_ref"`
	AnnualDemandTons *string `json:"annual_demand_tons"`
}

// SnapHub là cấu hình một Hub trong kịch bản.
type SnapHub struct {
	ScenarioHubID      string  `json:"scenario_hub_id"`
	HubRef             string  `json:"hub_ref"`
	Code               string  `json:"code"`
	SortOrder          int     `json:"sort_order"`
	ServiceRadiusKm    string  `json:"service_radius_km"`
	DesignCapacityTons string  `json:"design_capacity_tons"`
	BargePayloadTons   *string `json:"barge_payload_tons"`
	ModeFieldHub       string  `json:"mode_field_hub"`
	ModeHubPlant       string  `json:"mode_hub_plant"`
	LandMode           string  `json:"land_mode"`
}

// SnapCooperative là nguồn cung của một HTX (chỉ mã tham chiếu).
type SnapCooperative struct {
	CooperativeRef string           `json:"cooperative_ref"`
	MemberCount    int              `json:"member_count"`
	Seasons        []SnapCoopSeason `json:"seasons"`
}

// SnapCoopSeason là sản lượng của HTX trong một vụ.
type SnapCoopSeason struct {
	SeasonKey     string `json:"season_key"`
	PaddyTons     string `json:"paddy_tons"`
	PlantedAreaHa string `json:"planted_area_ha"`
}

// SnapSeason là một vụ.
type SnapSeason struct {
	SeasonKey  string `json:"season_key"`
	SeasonType string `json:"season_type"`
	EndMonth   int    `json:"end_month"`
	SortOrder  int    `json:"sort_order"`
}

// SnapObservation là tấn thu hoạch tại một ngày trong năm.
type SnapObservation struct {
	DayOfYear int    `json:"day_of_year"`
	Tons      string `json:"tons"`
}

// SnapRadius là khoảng cách chim bay HTX ↔ Hub (cặp nằm trong bán kính, FN-05).
type SnapRadius struct {
	CooperativeRef string `json:"cooperative_ref"`
	HubRef         string `json:"hub_ref"`
	RadiusKm       string `json:"radius_km"`
}

// SnapRoute là một kết quả định tuyến đã dùng (FN-04).
type SnapRoute struct {
	From           string  `json:"from"`
	To             string  `json:"to"`
	Mode           string  `json:"mode"`
	DistanceKm     string  `json:"distance_km"`
	Source         string  `json:"source"`
	StraightKm     *string `json:"straight_km,omitempty"`
	WaterwayAccess *bool   `json:"waterway_access,omitempty"`
}

// SnapParams là bộ tham số đã giải.
type SnapParams struct {
	SetVersion  string            `json:"set_version"`
	AllApproved bool              `json:"all_approved"`
	Unapproved  []UnapprovedParam `json:"unapproved"`
	Values      []SnapParamValue  `json:"values"`
}

// UnapprovedParam là tham số đang dùng giá trị danh mục vì chưa có phiên bản phê duyệt.
type UnapprovedParam struct {
	Code   string `json:"code"`
	Number int    `json:"number"`
	Name   string `json:"name"`
}

// SnapParamValue là một tham số.
type SnapParamValue struct {
	Code           string            `json:"code"`
	Number         int               `json:"number"`
	Name           string            `json:"name"`
	Classification string            `json:"classification"`
	Value          *string           `json:"value"`
	TextValue      map[string]string `json:"text_value"`
	Min            *string           `json:"min"`
	Max            *string           `json:"max"`
	Approved       bool              `json:"approved"`
	ParamVersion   *int              `json:"param_version"`
}

// SnapOptions là chế độ chạy.
type SnapOptions struct {
	Mode             string   `json:"mode"`
	SensitivityCodes []string `json:"sensitivity_codes"`
	MaxHubs          *int     `json:"max_hubs"`
}

// Canonical tuần tự hóa ảnh chụp thành byte tất định và băm sha256 của chúng.
// Go sắp khóa map khi Marshal; struct giữ thứ tự khai báo.
func (s Snapshot) Canonical() ([]byte, string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), nil
}

// ErrSnapshotTampered: byte đọc lại không cho cùng băm đã lưu.
var ErrSnapshotTampered = errors.New("simulation: ảnh chụp không khớp băm đã lưu")

// DecodeSnapshot đọc lại ảnh chụp từ jsonb và kiểm băm (bất biến + toàn vẹn).
func DecodeSnapshot(stored []byte, wantSHA string) (Snapshot, []byte, error) {
	var s Snapshot
	if err := json.Unmarshal(stored, &s); err != nil {
		return s, nil, err
	}
	b, sha, err := s.Canonical()
	if err != nil {
		return s, nil, err
	}
	if sha != wantSHA {
		return s, nil, ErrSnapshotTampered
	}
	return s, b, nil
}

// ---------------------------------------------------------------------------
// Tham số
// ---------------------------------------------------------------------------

// ParamKey dựng khóa pf/masterdata của một tham số (dim rỗng = giá trị chính).
func ParamKey(code, dim string) string {
	if dim == "" {
		return port.ParamKeyPrefix + code
	}
	return port.ParamKeyPrefix + code + ":" + dim
}

// ParamLookup là kết quả tra một khóa.
type ParamLookup struct {
	Found bool
	Value decimal.Decimal
	Ver   int
}

func ptr[T any](v T) *T { return &v }

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ResolveParams dựng khối params của ảnh chụp từ kết quả tra theo khóa.
// Tham số đang dùng (có giá trị danh mục, không phải dữ liệu module khác) mà thiếu
// phiên bản phê duyệt → vào danh sách unapproved; kết quả không thể "Chính thức"
// (FN-01 BR-04). Quy tắc chặt hơn prototype (prototype chỉ xét nhóm giả định):
// nền tảng đòi MỌI tham số đã dùng đến từ phiên bản phê duyệt của pf/masterdata.
func ResolveParams(lookup map[string]ParamLookup) SnapParams {
	out := SnapParams{Unapproved: []UnapprovedParam{}, Values: make([]SnapParamValue, 0, len(Catalog))}
	h := sha256.New()
	for _, def := range Catalog {
		v := SnapParamValue{Code: def.Code, Number: def.Number, Name: def.Name, Classification: string(def.Classification),
			Min: strOrNil(def.Min), Max: strOrNil(def.Max)}
		if def.ManagedElsewhere {
			out.Values = append(out.Values, v)
			continue
		}
		main := lookup[ParamKey(def.Code, "")]
		approved := true
		switch {
		case main.Found:
			v.Value, v.ParamVersion = ptr(main.Value.String()), ptr(main.Ver)
		case def.Base != "":
			v.Value = ptr(def.Base)
			approved = len(def.Dims) > 0 // tham số nhiều chiều: xét từng chiều bên dưới
		default:
			approved = false // #48/#49 chưa chốt: không dùng, không tính là thiếu phê duyệt
		}
		if len(def.Dims) > 0 {
			v.TextValue = map[string]string{}
			for _, d := range def.Dims {
				l := lookup[ParamKey(def.Code, d.Key)]
				if l.Found {
					v.TextValue[d.Key] = l.Value.String()
				} else {
					v.TextValue[d.Key] = d.Base
					approved = false
				}
			}
		}
		v.Approved = approved
		if !approved && (def.Base != "" || main.Found) {
			out.Unapproved = append(out.Unapproved, UnapprovedParam{Code: def.Code, Number: def.Number, Name: def.Name})
		}
		out.Values = append(out.Values, v)
		b, _ := json.Marshal(v)
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	out.AllApproved = len(out.Unapproved) == 0
	out.SetVersion = hex.EncodeToString(h.Sum(nil))
	return out
}

// ParamValue đọc giá trị chính đã giải của một mã (dùng cho mặc định Hub, hệ số #10).
func (p SnapParams) ParamValue(code string) (decimal.Decimal, bool) {
	for _, v := range p.Values {
		if v.Code == code && v.Value != nil {
			d, err := decimal.NewFromString(*v.Value)
			return d, err == nil
		}
	}
	return decimal.Zero, false
}

// CorrectionFactor trả hệ số #10 theo phương thức; < 1 hoặc thiếu → 1 (như prototype).
func (p SnapParams) CorrectionFactor(mode string) decimal.Decimal {
	for _, v := range p.Values {
		if v.Code != "distance_correction" {
			continue
		}
		s, ok := v.TextValue[mode]
		if !ok && v.Value != nil {
			s = *v.Value
		}
		if d, err := decimal.NewFromString(s); err == nil && d.GreaterThanOrEqual(decimal.NewFromInt(1)) {
			return d
		}
	}
	return decimal.NewFromInt(1)
}

// ---------------------------------------------------------------------------
// Khoảng cách và kế hoạch định tuyến
// ---------------------------------------------------------------------------

const earthRadiusKm = 6371.0088

// HaversineKm là khoảng cách chim bay (km) — cùng bán kính Trái Đất với prototype.
func HaversineKm(a, b port.LatLng) float64 {
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLng := rad(b.Lat-a.Lat), rad(b.Lng-a.Lng)
	h := math.Pow(math.Sin(dLat/2), 2) + math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Pow(math.Sin(dLng/2), 2)
	return 2 * earthRadiusKm * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Node đặt tên điểm trong ảnh chụp.
func CoopNode(ref uuid.UUID) string { return "coop:" + ref.String() }

// HubNode đặt tên điểm Hub.
func HubNode(ref uuid.UUID) string { return "hub:" + ref.String() }

// PlantNode là nút nhà máy.
const PlantNode = "plant"

// HubSpec là Hub của kịch bản kèm toạ độ (chỉ phía nền tảng).
type HubSpec struct {
	Snap     SnapHub
	HubRef   uuid.UUID
	Location port.LatLng
	RadiusKm decimal.Decimal
}

// RouteNeed là một cặp điểm cần định tuyến.
type RouteNeed struct {
	From, To       string
	FromLoc, ToLoc port.LatLng
	Mode           string
}

// Plan là kết quả bước lập kế hoạch: cặp trong bán kính và các chặng cần định tuyến.
type Plan struct {
	Radius       []SnapRadius
	Needs        []RouteNeed
	Cooperatives []port.CooperativeSupply // chỉ HTX có toạ độ và nằm trong bán kính ít nhất một Hub
}

// PlanRoutes xét bán kính phục vụ (FN-05, khoảng cách chim bay) và liệt kê mọi
// chặng mà engine có thể cần: HTX→Hub, HTX→nhà máy, Hub→nhà máy, mỗi chặng cả
// đường bộ lẫn đường thủy (để FN-08 BR-04 quyết định trong optimizer).
func PlanRoutes(hubs []HubSpec, plant port.LatLng, coops []port.CooperativeSupply) Plan {
	var p Plan
	in := map[uuid.UUID]bool{}
	add := func(from, to string, a, b port.LatLng) {
		for _, m := range []string{port.ModeRoad, port.ModeWaterway} {
			p.Needs = append(p.Needs, RouteNeed{From: from, To: to, FromLoc: a, ToLoc: b, Mode: m})
		}
	}
	for _, h := range hubs {
		add(HubNode(h.HubRef), PlantNode, h.Location, plant)
		if !h.RadiusKm.IsPositive() {
			continue
		}
		r, _ := h.RadiusKm.Float64()
		for _, c := range coops {
			if c.Location == nil {
				continue
			}
			d := HaversineKm(h.Location, *c.Location)
			if d > r {
				continue
			}
			p.Radius = append(p.Radius, SnapRadius{CooperativeRef: c.CooperativeRef.String(), HubRef: h.HubRef.String(), RadiusKm: decimal.NewFromFloat(d).String()})
			add(CoopNode(c.CooperativeRef), HubNode(h.HubRef), *c.Location, h.Location)
			in[c.CooperativeRef] = true
		}
	}
	for _, c := range coops {
		if in[c.CooperativeRef] {
			add(CoopNode(c.CooperativeRef), PlantNode, *c.Location, plant)
			p.Cooperatives = append(p.Cooperatives, c)
		}
	}
	return p
}

// ToRoute ghi kết quả định tuyến của một chặng. Không tìm được tuyến → Haversine
// × hệ số #10 (FN-04 BR-03), ghi straight_km; đường thủy không tìm được = không
// tiếp giáp mạng (FN-08 BR-04b).
func ToRoute(n RouteNeed, r port.RouteResult, factor decimal.Decimal) SnapRoute {
	out := SnapRoute{From: n.From, To: n.To, Mode: n.Mode}
	if r.Found {
		out.DistanceKm, out.Source = r.DistanceKm.Round(3).String(), r.Source
	} else {
		straight := HaversineKm(n.FromLoc, n.ToLoc)
		sd := decimal.NewFromFloat(straight)
		out.DistanceKm, out.Source, out.StraightKm = sd.Mul(factor).Round(3).String(), port.SourceHaversine, ptr(sd.String())
	}
	if n.Mode == port.ModeWaterway {
		out.WaterwayAccess = ptr(r.Found)
	}
	return out
}

// SupplyToSnapshot chuyển nguồn cung sang dạng ảnh chụp (bỏ toạ độ).
func SupplyToSnapshot(coops []port.CooperativeSupply, seasons []port.SeasonDef, obs map[string][]port.HarvestObservation) ([]SnapCooperative, []SnapSeason, map[string][]SnapObservation, error) {
	sc := make([]SnapCooperative, 0, len(coops))
	for _, c := range coops {
		if c.MemberCount < 0 {
			return nil, nil, nil, fmt.Errorf("HTX %s: số hộ âm", c.CooperativeRef)
		}
		ss := make([]SnapCoopSeason, 0, len(c.Seasons))
		for _, s := range c.Seasons {
			if s.PaddyTons.IsNegative() || s.PlantedAreaHa.IsNegative() {
				return nil, nil, nil, fmt.Errorf("HTX %s vụ %s: sản lượng/diện tích âm", c.CooperativeRef, s.SeasonKey)
			}
			ss = append(ss, SnapCoopSeason{SeasonKey: s.SeasonKey, PaddyTons: s.PaddyTons.String(), PlantedAreaHa: s.PlantedAreaHa.String()})
		}
		sc = append(sc, SnapCooperative{CooperativeRef: c.CooperativeRef.String(), MemberCount: c.MemberCount, Seasons: ss})
	}
	se := make([]SnapSeason, 0, len(seasons))
	for _, s := range seasons {
		if s.EndMonth < 1 || s.EndMonth > 12 {
			return nil, nil, nil, fmt.Errorf("vụ %s: tháng kết thúc %d không hợp lệ", s.Key, s.EndMonth)
		}
		se = append(se, SnapSeason{SeasonKey: s.Key, SeasonType: strings.ToUpper(s.Type), EndMonth: s.EndMonth, SortOrder: s.SortOrder})
	}
	so := map[string][]SnapObservation{}
	types := make([]string, 0, len(obs))
	for t := range obs {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		for _, o := range obs[t] {
			if o.DayOfYear < 1 || o.DayOfYear > 365 || !o.Tons.IsPositive() {
				continue
			}
			so[strings.ToUpper(t)] = append(so[strings.ToUpper(t)], SnapObservation{DayOfYear: o.DayOfYear, Tons: o.Tons.String()})
		}
	}
	return sc, se, so, nil
}

// FormatTime là định dạng thời điểm trong ảnh chụp.
func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
