package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// ResultSchemaVersion là phiên bản contracts/optimizer/result.v1.schema.json.
const ResultSchemaVersion = 1

// ResultEnvelope là phong bì optimizer nộp. Số đọc bằng json.Number rồi sang
// decimal (R-10) — Go không làm phép tính float trên tiền/khối lượng.
type ResultEnvelope struct {
	SchemaVersion    int             `json:"schema_version"`
	SimulationID     *string         `json:"simulation_id"`
	SnapshotID       *string         `json:"snapshot_id"`
	SnapshotSHA256   string          `json:"snapshot_sha256"`
	AlgorithmVersion string          `json:"algorithm_version"`
	EngineVersion    string          `json:"engine_version"`
	Mode             string          `json:"mode"`
	Status           string          `json:"status"`
	Error            *ResultError    `json:"error"`
	Result           json.RawMessage `json:"result"`
	Sensitivity      json.RawMessage `json:"sensitivity"`
	Selection        json.RawMessage `json:"selection"`
}

// ResultError là lỗi do optimizer báo.
type ResultError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type num = json.Number

type resultBody struct {
	ParameterSetVersion    string `json:"parameter_set_version"`
	HubCount               int    `json:"hub_count"`
	DeliveredTons          num    `json:"delivered_tons"`
	PlantDemandCoveragePct num    `json:"plant_demand_coverage_pct"`
	Plant                  struct {
		AnnualDemandTons num `json:"annual_demand_tons"`
	} `json:"plant"`
	Costs struct {
		CollectionCost     num  `json:"collection_cost"`
		WarehouseCost      num  `json:"warehouse_cost"`
		HandlingCost       num  `json:"handling_cost"`
		TransportationCost num  `json:"transportation_cost"`
		TotalLogisticsCost num  `json:"total_delivered_logistics_cost"`
		CostPerTon         *num `json:"cost_per_ton"`
	} `json:"costs"`
	Capex struct {
		Total num `json:"total"`
	} `json:"capex"`
	Opex struct {
		Total num `json:"total"`
	} `json:"opex"`
	Financial struct {
		LifecycleYears      int    `json:"lifecycle_years"`
		Discounted          bool   `json:"discounted"`
		TCO                 num    `json:"tco"`
		TCOPerTon           *num   `json:"tco_per_ton"`
		LifecycleCostPerTon *num   `json:"lifecycle_cost_per_ton"`
		ROIPct              *num   `json:"roi_pct"`
		PaybackYears        *int   `json:"payback_years"`
		PaybackStatus       string `json:"payback_status"`
	} `json:"financial"`
	Baseline struct {
		Available          bool `json:"available"`
		DeliveredTons      num  `json:"delivered_tons"`
		CollectionCost     num  `json:"collection_cost"`
		TransportationCost num  `json:"transportation_cost"`
		TotalCost          num  `json:"total_delivered_logistics_cost"`
		CostPerTon         *num `json:"baseline_cost_per_ton"`
	} `json:"baseline"`
	Hubs []struct {
		CapacityWarning json.RawMessage `json:"capacity_warning"`
	} `json:"hubs"`
}

type sensitivityBody struct {
	HighSensitivity bool `json:"high_sensitivity"`
	SwingPct        num  `json:"swing_pct"`
}

// SensitivitySummary là tóm tắt kết quả độ nhạy.
type SensitivitySummary struct {
	HighSensitivity bool
	SwingPct        decimal.Decimal
}

// ErrResultInvalid: phong bì kết quả không đúng hợp đồng.
var ErrResultInvalid = errors.New("simulation: phong bì kết quả không hợp lệ")

func dec(n num) (decimal.Decimal, error) {
	if n == "" {
		return decimal.Zero, fmt.Errorf("%w: thiếu số", ErrResultInvalid)
	}
	return decimal.NewFromString(string(n))
}

func decPtr(n *num) (*decimal.Decimal, error) {
	if n == nil {
		return nil, nil
	}
	d, err := dec(*n)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ParseEnvelope đọc phong bì kết quả (số giữ dạng json.Number).
func ParseEnvelope(raw []byte) (ResultEnvelope, error) {
	var e ResultEnvelope
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&e); err != nil {
		return e, fmt.Errorf("%w: %w", ErrResultInvalid, err)
	}
	if e.SchemaVersion != ResultSchemaVersion {
		return e, fmt.Errorf("%w: schema_version %d", ErrResultInvalid, e.SchemaVersion)
	}
	switch e.Status {
	case StatusCompleted:
		if len(e.Result) == 0 || string(e.Result) == "null" {
			return e, fmt.Errorf("%w: thiếu result", ErrResultInvalid)
		}
	case StatusFailed:
		if e.Error == nil || e.Error.Code == "" {
			return e, fmt.Errorf("%w: thiếu error", ErrResultInvalid)
		}
	default:
		return e, fmt.Errorf("%w: status %q", ErrResultInvalid, e.Status)
	}
	return e, nil
}

// Summarize trích cột tóm tắt từ result (decimal).
func Summarize(result json.RawMessage) (ResultSummary, error) {
	var b resultBody
	d := json.NewDecoder(bytes.NewReader(result))
	d.UseNumber()
	if err := d.Decode(&b); err != nil {
		return ResultSummary{}, fmt.Errorf("%w: %w", ErrResultInvalid, err)
	}
	if b.Financial.LifecycleYears < 1 || b.Financial.PaybackStatus == "" {
		return ResultSummary{}, fmt.Errorf("%w: thiếu khối financial", ErrResultInvalid)
	}
	s := ResultSummary{ParameterSetVersion: b.ParameterSetVersion, LifecycleYears: b.Financial.LifecycleYears, Discounted: b.Financial.Discounted,
		HubCount: b.HubCount, PaybackYears: b.Financial.PaybackYears, PaybackStatus: b.Financial.PaybackStatus, BaselineAvailable: b.Baseline.Available}
	var err error
	must := func(dst *decimal.Decimal, n num) {
		if err == nil {
			*dst, err = dec(n)
		}
	}
	opt := func(dst **decimal.Decimal, n *num) {
		if err == nil {
			*dst, err = decPtr(n)
		}
	}
	must(&s.DeliveredTons, b.DeliveredTons)
	must(&s.PlantDemandCoveragePct, b.PlantDemandCoveragePct)
	must(&s.PlantAnnualDemandTons, b.Plant.AnnualDemandTons)
	must(&s.Capex, b.Capex.Total)
	must(&s.OpexYear1, b.Opex.Total)
	must(&s.CollectionCost, b.Costs.CollectionCost)
	must(&s.WarehouseCost, b.Costs.WarehouseCost)
	must(&s.HandlingCost, b.Costs.HandlingCost)
	must(&s.TransportationCost, b.Costs.TransportationCost)
	must(&s.TotalLogisticsCost, b.Costs.TotalLogisticsCost)
	must(&s.TCO, b.Financial.TCO)
	must(&s.BaselineDeliveredTons, b.Baseline.DeliveredTons)
	must(&s.BaselineCollection, b.Baseline.CollectionCost)
	must(&s.BaselineTransportation, b.Baseline.TransportationCost)
	must(&s.BaselineTotalCost, b.Baseline.TotalCost)
	opt(&s.CostPerTon, b.Costs.CostPerTon)
	opt(&s.TCOPerTon, b.Financial.TCOPerTon)
	opt(&s.LifecycleCostPerTon, b.Financial.LifecycleCostPerTon)
	opt(&s.ROIPct, b.Financial.ROIPct)
	opt(&s.BaselineCostPerTon, b.Baseline.CostPerTon)
	if err != nil {
		return ResultSummary{}, fmt.Errorf("%w: %w", ErrResultInvalid, err)
	}
	for _, h := range b.Hubs {
		if len(h.CapacityWarning) > 0 && string(h.CapacityWarning) != "null" {
			s.ShortfallWarnings++
		}
	}
	return s, nil
}

// SummarizeSensitivity trích tóm tắt độ nhạy.
func SummarizeSensitivity(raw json.RawMessage) (SensitivitySummary, error) {
	var b sensitivityBody
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&b); err != nil {
		return SensitivitySummary{}, fmt.Errorf("%w: %w", ErrResultInvalid, err)
	}
	sw, err := dec(b.SwingPct)
	if err != nil {
		return SensitivitySummary{}, err
	}
	return SensitivitySummary{HighSensitivity: b.HighSensitivity, SwingPct: sw.Round(2)}, nil
}
