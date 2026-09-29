package handler

import (
	"encoding/json"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

func pct(p interface{ StringFixed(int32) string }, ok bool) *string {
	if !ok {
		return nil
	}
	s := p.StringFixed(2)
	return &s
}

type pointDTO struct {
	Lat json.Number `json:"lat"`
	Lng json.Number `json:"lng"`
}

type specDTO struct {
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	Kind             string          `json:"kind"`
	ParentFacilityID *string         `json:"parent_facility_id"`
	Location         pointDTO        `json:"location"`
	Boundary         json.RawMessage `json:"boundary,omitempty"`
	ProvinceCode     string          `json:"province_code"`
	Status           string          `json:"status"`
	CapacityKg       string          `json:"capacity_kg"`
	CapacityTons     string          `json:"capacity_tons"`
	EffectiveFrom    string          `json:"effective_from"`
}

func toSpec(s domain.FacilitySpec) specDTO {
	d := specDTO{
		Code: s.Code, Name: s.Name, Kind: string(s.Kind), Location: pointDTO{Lat: json.Number(s.Location.Lat.String()), Lng: json.Number(s.Location.Lng.String())},
		Boundary: s.Boundary, ProvinceCode: s.ProvinceCode, Status: string(s.Status), CapacityKg: s.Capacity.String(),
		CapacityTons: s.Capacity.Tonnes(), EffectiveFrom: ts(s.EffectiveFrom),
	}
	if s.ParentFacilityID != nil {
		p := s.ParentFacilityID.String()
		d.ParentFacilityID = &p
	}
	return d
}

type facilityDTO struct {
	ID string `json:"id"`
	specDTO
	Version    int64  `json:"version"`
	VersionID  string `json:"version_id"`
	ProposedBy string `json:"proposed_by"`
	ApprovedBy string `json:"approved_by"`
	ApprovedAt string `json:"approved_at"`
}

func toFacility(f domain.Facility) facilityDTO {
	return facilityDTO{
		ID: f.ID.String(), specDTO: toSpec(f.Spec), Version: f.VersionSeq, VersionID: f.VersionID.String(),
		ProposedBy: f.ProposedBy.String(), ApprovedBy: f.ApprovedBy.String(), ApprovedAt: ts(f.ApprovedAt),
	}
}

type versionDTO struct {
	ID         string `json:"id"`
	FacilityID string `json:"facility_id"`
	Version    int64  `json:"version"`
	specDTO
	Supersedes   *string `json:"supersedes_id"`
	ChangeReason *string `json:"change_reason"`
	ProposedBy   string  `json:"proposed_by"`
	EventTime    string  `json:"event_time"`
	RecordedAt   string  `json:"recorded_at"`
	State        string  `json:"state"` // pending | approved | rejected
	DecidedBy    *string `json:"decided_by"`
	DecidedAt    *string `json:"decided_at"`
}

func toVersion(v domain.FacilityVersion) versionDTO {
	d := versionDTO{
		ID: v.ID.String(), FacilityID: v.FacilityID.String(), Version: v.Seq, specDTO: toSpec(v.Spec), ChangeReason: v.ChangeReason,
		ProposedBy: v.ProposedBy.String(), EventTime: ts(v.EventTime), RecordedAt: ts(v.RecordedAt), State: "pending",
		DecidedAt: tsp(v.DecidedAt),
	}
	if v.Supersedes != nil {
		s := v.Supersedes.String()
		d.Supersedes = &s
	}
	if v.Decision != nil {
		d.State = string(*v.Decision)
	}
	if v.DecidedBy != nil {
		s := v.DecidedBy.String()
		d.DecidedBy = &s
	}
	return d
}

type exportDTO struct {
	ID              string          `json:"id"`
	Contract        string          `json:"contract"`
	ContractVersion int             `json:"contract_version"`
	EventType       string          `json:"event_type"`
	SchemaVersion   int             `json:"schema_version"`
	SubjectRef      string          `json:"subject_ref"`
	Seq             int64           `json:"seq"`
	Payload         json.RawMessage `json:"payload"`
	EventTime       string          `json:"event_time"`
	RecordedAt      string          `json:"recorded_at"`
}

func toExport(x store.ValveExport) exportDTO {
	return exportDTO{
		ID: x.ID.String(), Contract: x.Contract, ContractVersion: x.ContractVersion, EventType: x.EventType,
		SchemaVersion: x.SchemaVersion, SubjectRef: x.SubjectRef.String(), Seq: x.Seq, Payload: x.Payload,
		EventTime: ts(x.EventTime), RecordedAt: ts(x.RecordedAt),
	}
}

type sourceDTO struct {
	Kind   string  `json:"kind"`
	Ref    string  `json:"ref"`
	Ratio  string  `json:"ratio"`
	MassKg *string `json:"mass_kg,omitempty"`
}

func toSources(ss []domain.Source) []sourceDTO {
	out := make([]sourceDTO, 0, len(ss))
	for _, s := range ss {
		d := sourceDTO{Kind: string(s.Kind), Ref: s.Ref.String(), Ratio: s.Ratio.StringFixed(domain.RatioScale)}
		if s.MassKg != nil {
			m := s.MassKg.String()
			d.MassKg = &m
		}
		out = append(out, d)
	}
	return out
}

type lotDTO struct {
	ID          string      `json:"id"`
	FacilityID  string      `json:"facility_id"`
	Code        string      `json:"code"`
	Product     string      `json:"product"`
	Origin      string      `json:"origin"`
	BatchID     *string     `json:"batch_id"`
	InitialKg   string      `json:"initial_kg"`
	MoisturePct *string     `json:"moisture_pct"`
	Sources     []sourceDTO `json:"sources"`
	EventTime   string      `json:"event_time"`
	RecordedAt  string      `json:"recorded_at"`
	Seq         int64       `json:"seq"`
}

func toLot(l domain.Lot) lotDTO {
	d := lotDTO{
		ID: l.ID.String(), FacilityID: l.FacilityID.String(), Code: l.Code, Product: string(l.Product), Origin: l.Origin,
		InitialKg: l.Initial.String(), Sources: toSources(l.Sources), EventTime: ts(l.EventTime), RecordedAt: ts(l.RecordedAt), Seq: l.Seq,
	}
	if l.MoisturePct != nil {
		d.MoisturePct = pct(*l.MoisturePct, true)
	}
	if l.BatchID != nil {
		s := l.BatchID.String()
		d.BatchID = &s
	}
	return d
}

type movementDTO struct {
	ID               string  `json:"id"`
	LotID            string  `json:"lot_id"`
	FacilityID       string  `json:"facility_id"`
	Seq              int64   `json:"seq"`
	Kind             string  `json:"kind"`
	QtyKg            string  `json:"qty_kg"`
	BalanceAfterKg   string  `json:"balance_after_kg"`
	RefType          *string `json:"ref_type"`
	RefID            *string `json:"ref_id"`
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason *string `json:"correction_reason"`
	EventTime        string  `json:"event_time"`
	RecordedAt       string  `json:"recorded_at"`
}

func toMovement(m domain.Movement) movementDTO {
	d := movementDTO{
		ID: m.ID.String(), LotID: m.LotID.String(), FacilityID: m.FacilityID.String(), Seq: m.Seq, Kind: string(m.Kind),
		QtyKg: m.Qty.String(), BalanceAfterKg: m.BalanceAfter.String(), RefType: m.RefType, CorrectionReason: m.CorrectionReason,
		EventTime: ts(m.EventTime), RecordedAt: ts(m.RecordedAt),
	}
	if m.RefID != nil {
		s := m.RefID.String()
		d.RefID = &s
	}
	if m.SupersedesID != nil {
		s := m.SupersedesID.String()
		d.SupersedesID = &s
	}
	return d
}

func toMovements(ms []domain.Movement) []movementDTO {
	out := make([]movementDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, toMovement(m))
	}
	return out
}

type rankedDTO struct {
	LotID       string  `json:"lot_id"`
	Code        string  `json:"code"`
	Product     string  `json:"product"`
	RemainingKg string  `json:"remaining_kg"`
	InitialKg   string  `json:"initial_kg"`
	MoisturePct *string `json:"moisture_pct"`
	ReceivedAt  string  `json:"received_at"`
	AgeDays     int64   `json:"age_days"`
	RiskScore   string  `json:"risk_score"`
}

func toRanked(rs []domain.Ranked) []rankedDTO {
	out := make([]rankedDTO, 0, len(rs))
	for _, r := range rs {
		d := rankedDTO{
			LotID: r.LotID.String(), Code: r.Code, Product: string(r.Product), RemainingKg: r.Remaining.String(),
			InitialKg: r.Initial.String(), ReceivedAt: ts(r.ReceivedAt), AgeDays: r.AgeDays, RiskScore: r.Risk.StringFixed(1),
		}
		if r.MoisturePct != nil {
			d.MoisturePct = pct(*r.MoisturePct, true)
		}
		out = append(out, d)
	}
	return out
}

type allocationDTO struct {
	LotID     string  `json:"lot_id"`
	MassKg    string  `json:"mass_kg"`
	RiskScore *string `json:"risk_score,omitempty"`
}

func toAllocations(as []domain.Allocation) []allocationDTO {
	out := make([]allocationDTO, 0, len(as))
	for _, a := range as {
		d := allocationDTO{LotID: a.LotID.String(), MassKg: a.MassKg.String()}
		if a.Risk != nil {
			s := a.Risk.StringFixed(1)
			d.RiskScore = &s
		}
		out = append(out, d)
	}
	return out
}

type capacityDTO struct {
	CapacityKg    string `json:"capacity_kg"`
	UsedKg        string `json:"used_kg"`
	AvailableKg   string `json:"available_kg"`
	CapacityTons  string `json:"capacity_tons"`
	UsedTons      string `json:"used_tons"`
	AvailableTons string `json:"available_tons"`
	AsOf          string `json:"as_of"`
}

func toCapacity(c domain.Capacity) capacityDTO {
	return capacityDTO{
		CapacityKg: c.Capacity.String(), UsedKg: c.Used.String(), AvailableKg: c.Available.String(),
		CapacityTons: c.Capacity.Tonnes(), UsedTons: c.Used.Tonnes(), AvailableTons: c.Available.Tonnes(), AsOf: ts(c.AsOf),
	}
}

func inventoryDTO(inv app.Inventory) map[string]any {
	return map[string]any{
		"facility": toFacility(inv.Facility), "lots": toRanked(inv.Lots), "capacity": toCapacity(inv.Capacity), "risk_rule": inv.Rule,
	}
}

func kgp(k *mass.Kg) *string {
	if k == nil {
		return nil
	}
	s := k.String()
	return &s
}
