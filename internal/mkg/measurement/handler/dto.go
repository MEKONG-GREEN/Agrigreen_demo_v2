package handler

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

func pct(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.StringFixed(domain.PctScale)
	return &s
}

func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type deviceDTO struct {
	DeviceID     string     `json:"device_id"`
	Epoch        int        `json:"epoch"`
	FacilityRef  uuid.UUID  `json:"facility_ref"`
	Point        string     `json:"point"`
	PublicKey    string     `json:"public_key"`
	KeyID        string     `json:"key_id"`
	CertSHA256   string     `json:"cert_sha256"`
	ScaleModel   string     `json:"scale_model"`
	Status       string     `json:"status"`
	ProposedBy   uuid.UUID  `json:"proposed_by"`
	ProposedAt   time.Time  `json:"proposed_at"`
	DecidedBy    *uuid.UUID `json:"decided_by"`
	DecidedAt    *time.Time `json:"decided_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	RevokeReason *string    `json:"revoke_reason"`
	Health       healthDTO  `json:"health"`
}

type healthDTO struct {
	LastSeenAt  *time.Time `json:"last_seen_at"`
	BufferPct   *string    `json:"buffer_pct"`
	ClockSkewMs *int64     `json:"clock_skew_ms"`
	LastCounter *int64     `json:"last_counter"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

func toDevice(d store.DeviceRow) deviceDTO {
	return deviceDTO{DeviceID: d.DeviceID, Epoch: d.Epoch, FacilityRef: d.FacilityRef, Point: string(d.Point),
		PublicKey: base64.StdEncoding.EncodeToString(d.PublicKey), KeyID: d.KeyID, CertSHA256: d.CertSHA256, ScaleModel: d.ScaleModel,
		Status: d.RawStatus, ProposedBy: d.ProposedBy, ProposedAt: d.ProposedAt, DecidedBy: d.DecidedBy, DecidedAt: d.DecidedAt,
		RevokedAt: d.RevokedAt, RevokeReason: d.RevokeReason,
		Health: healthDTO{LastSeenAt: d.LastSeenAt, BufferPct: d.BufferPct, ClockSkewMs: d.ClockSkewMs, LastCounter: d.LastCounter,
			UpdatedAt: d.HealthUpdatedAt}}
}

type sessionDTO struct {
	ID          uuid.UUID  `json:"id"`
	LotRef      uuid.UUID  `json:"lot_ref"`
	Point       string     `json:"point"`
	Route       string     `json:"route"`
	FacilityRef uuid.UUID  `json:"facility_ref"`
	DeviceID    string     `json:"device_id"`
	Epoch       int        `json:"epoch"`
	CounterFrom int64      `json:"counter_from"`
	CounterTo   *int64     `json:"counter_to"`
	Status      string     `json:"status"`
	OpenedAt    time.Time  `json:"opened_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	MoisturePct *string    `json:"moisture_pct"`
	TareKg      *mass.Kg   `json:"tare_kg"`
}

func toSession(s port.Session) sessionDTO {
	out := sessionDTO{ID: s.ID, LotRef: s.LotRef, Point: string(s.Point), Route: string(s.Route), FacilityRef: s.FacilityRef,
		DeviceID: s.Window.DeviceID, Epoch: s.Window.Epoch, CounterFrom: s.Window.CounterFrom, Status: string(s.Status),
		OpenedAt: s.OpenedAt, ClosedAt: s.ClosedAt, MoisturePct: opt(s.MoisturePct), TareKg: s.TareKg}
	if s.Status == port.SessionClosed {
		to := s.Window.CounterTo
		out.CounterTo = &to
	}
	return out
}

type windowDTO struct {
	DeviceID       string      `json:"device_id"`
	Epoch          int         `json:"epoch"`
	CounterFrom    int64       `json:"counter_from"`
	CounterTo      int64       `json:"counter_to"`
	ReadingsUsed   int         `json:"readings_used"`
	ReadingsTotal  int         `json:"readings_total"`
	BatchIDs       []uuid.UUID `json:"batch_ids"`
	ReadingsSHA256 *string     `json:"readings_sha256"`
}

type ticketDTO struct {
	ID               uuid.UUID  `json:"id"`
	LotRef           uuid.UUID  `json:"lot_ref"`
	Point            string     `json:"point"`
	Route            string     `json:"route"`
	Source           string     `json:"source"`
	MassKg           mass.Kg    `json:"mass_kg"`
	MoisturePct      *string    `json:"moisture_pct"`
	MoistureState    string     `json:"moisture_state"`
	SessionRef       *uuid.UUID `json:"session_ref"`
	RuleVersion      *string    `json:"rule_version"`
	Window           *windowDTO `json:"window"`
	EvidenceRef      *uuid.UUID `json:"evidence_ref"`
	CustomerRef      *uuid.UUID `json:"customer_ref"`
	Reason           *string    `json:"reason"`
	SupersedesID     *uuid.UUID `json:"supersedes_id"`
	CorrectionReason *string    `json:"correction_reason"`
	EventTime        time.Time  `json:"event_time"`
	RecordedAt       time.Time  `json:"recorded_at"`
	Seq              int64      `json:"seq"`
	ActorRef         uuid.UUID  `json:"actor_ref"`
	Effective        *bool      `json:"effective,omitempty"`
}

func toTicket(t port.Ticket) ticketDTO {
	out := ticketDTO{ID: t.ID, LotRef: t.LotRef, Point: string(t.Point), Route: string(t.Route), Source: string(t.Source), MassKg: t.MassKg,
		MoisturePct: opt(t.MoisturePct), MoistureState: t.MoistureState, SessionRef: t.SessionRef, EvidenceRef: t.EvidenceRef,
		CustomerRef: t.CustomerRef, Reason: opt(t.Reason), SupersedesID: t.Supersedes, CorrectionReason: opt(t.CorrectionReason),
		EventTime: t.EventTime, RecordedAt: t.RecordedAt, Seq: t.Seq, ActorRef: t.ActorRef}
	if in := t.Integration; in != nil {
		rv := in.RuleVersion
		out.RuleVersion = &rv
		out.Window = &windowDTO{DeviceID: in.Window.DeviceID, Epoch: in.Window.Epoch, CounterFrom: in.Window.CounterFrom,
			CounterTo: in.Window.CounterTo, ReadingsUsed: in.ReadingsUsed, ReadingsTotal: in.ReadingsTotal, BatchIDs: in.BatchIDs,
			ReadingsSHA256: opt(in.ReadingsSHA256)}
	}
	return out
}

type pointDTO struct {
	MassKg   mass.Kg   `json:"mass_kg"`
	TicketID uuid.UUID `json:"ticket_id"`
	At       time.Time `json:"event_time"`
}

type lotMassDTO struct {
	LotRef     uuid.UUID `json:"lot_ref"`
	Route      string    `json:"route"`
	HubIn      *pointDTO `json:"hub_in"`
	HubOut     *pointDTO `json:"hub_out"`
	CustomerIn *pointDTO `json:"customer_in"`
	AsOf       time.Time `json:"as_of"`
}

func toLotMass(m store.LotMass) lotMassDTO {
	out := lotMassDTO{LotRef: m.LotRef, Route: string(m.Route), AsOf: m.AsOf}
	for pt, pm := range m.Points {
		d := &pointDTO{MassKg: pm.Kg, TicketID: pm.TicketID, At: pm.At}
		switch pt {
		case port.PointHubIn:
			out.HubIn = d
		case port.PointHubOut:
			out.HubOut = d
		case port.PointCustomerIn:
			out.CustomerIn = d
		}
	}
	return out
}

type reconDTO struct {
	ID               uuid.UUID  `json:"id"`
	LotRef           uuid.UUID  `json:"lot_ref"`
	Route            string     `json:"route"`
	PricingBasis     string     `json:"pricing_basis"`
	HubInKg          *mass.Kg   `json:"hub_in_kg"`
	HubOutKg         *mass.Kg   `json:"hub_out_kg"`
	CustomerInKg     *mass.Kg   `json:"customer_in_kg"`
	HubInMoisturePct *string    `json:"hub_in_moisture_pct"`
	Internal         any        `json:"internal"`
	Commercial       any        `json:"commercial"`
	RollCheck        any        `json:"roll_check"`
	YieldKgPerRoll   *mass.Kg   `json:"yield_kg_per_roll"`
	YieldAggregated  bool       `json:"yield_aggregated"`
	Outcome          string     `json:"outcome"`
	ToleranceVersion string     `json:"tolerance_version"`
	LossNormVersion  *string    `json:"loss_norm_version"`
	DisputeID        *uuid.UUID `json:"dispute_id"`
	SupersedesID     *uuid.UUID `json:"supersedes_id"`
	RuleVersion      string     `json:"rule_version"`
	EventTime        time.Time  `json:"event_time"`
	RecordedAt       time.Time  `json:"recorded_at"`
}

func toRecon(r store.Reconciliation) reconDTO {
	x := r.Result
	return reconDTO{ID: r.ID, LotRef: r.LotRef, Route: string(r.Route), PricingBasis: string(r.Basis), HubInKg: r.HubIn, HubOutKg: r.HubOut,
		CustomerInKg: r.CustomerIn, HubInMoisturePct: pct(r.HubInMoisture),
		Internal: map[string]any{"status": x.InternalStatus, "loss_kg": x.InternalLoss, "loss_norm_pct": pct(x.LossNormPct),
			"expected_loss_kg": x.ExpectedLoss, "abnormal_loss_kg": x.AbnormalLoss},
		Commercial: map[string]any{"status": x.CommercialStatus, "diff_kg": x.CommercialDiff, "diff_pct": pct(x.CommercialDiffPct)},
		RollCheck: map[string]any{"status": x.RollCheck, "rolls": x.Rolls, "kg_per_roll_estimate": x.KgPerRollEstimate,
			"estimate_kg": x.RollEstimate, "variance_pct": pct(x.RollVariancePct)},
		YieldKgPerRoll: x.Yield, YieldAggregated: x.YieldAggregated, Outcome: x.Outcome, ToleranceVersion: r.ToleranceLabel,
		LossNormVersion: r.LossNormLabel, DisputeID: r.DisputeID, SupersedesID: r.SupersedesID, RuleVersion: domain.RuleReconcile,
		EventTime: r.EventTime, RecordedAt: r.RecordedAt}
}

type disputeDTO struct {
	ID               uuid.UUID   `json:"id"`
	LotRef           uuid.UUID   `json:"lot_ref"`
	Status           string      `json:"status"`
	Severity         string      `json:"severity"`
	ReconciliationID uuid.UUID   `json:"reconciliation_id"`
	HubOutKg         *mass.Kg    `json:"hub_out_kg"`
	CustomerInKg     *mass.Kg    `json:"customer_in_kg"`
	DiffKg           mass.Kg     `json:"diff_kg"`
	DiffPct          *string     `json:"diff_pct"`
	EvidenceRefs     []uuid.UUID `json:"evidence_refs"`
	Resolution       *string     `json:"resolution"`
	AgreedKg         *mass.Kg    `json:"agreed_kg"`
	OpenedAt         time.Time   `json:"opened_at"`
	OpenedBy         uuid.UUID   `json:"opened_by"`
	Events           []any       `json:"events,omitempty"`
}

func toDispute(d store.Dispute, evs []store.DisputeEvent) disputeDTO {
	refs := d.EvidenceRefs
	if refs == nil {
		refs = []uuid.UUID{}
	}
	out := disputeDTO{ID: d.ID, LotRef: d.LotRef, Status: d.Status, Severity: d.Severity, ReconciliationID: d.ReconciliationID,
		HubOutKg: d.HubOut, CustomerInKg: d.CustomerIn, DiffKg: d.Diff, DiffPct: pct(d.DiffPct), EvidenceRefs: refs,
		Resolution: d.Resolution, AgreedKg: d.AgreedKg, OpenedAt: d.OpenedAt, OpenedBy: d.OpenedBy}
	for _, e := range evs {
		out.Events = append(out.Events, map[string]any{"seq": e.Seq, "action": e.Action, "status_after": e.StatusAfter,
			"evidence_refs": e.EvidenceRefs, "resolution": e.Resolution, "agreed_kg": e.AgreedKg, "note": e.Note,
			"event_time": e.EventTime, "actor_ref": e.ActorRef})
	}
	return out
}

type paramDTO struct {
	Kind          string          `json:"kind"`
	Version       int64           `json:"version"`
	ID            uuid.UUID       `json:"id"`
	Status        string          `json:"status"`
	EffectiveFrom time.Time       `json:"effective_from"`
	Provisional   bool            `json:"provisional"`
	Reason        string          `json:"reason"`
	ProposedBy    uuid.UUID       `json:"proposed_by"`
	DecidedBy     *uuid.UUID      `json:"decided_by"`
	DecidedAt     *time.Time      `json:"decided_at"`
	Values        json.RawMessage `json:"values"`
}

func toParam(p store.ParamVersion) paramDTO {
	var vals any
	if t := p.Tolerance; t != nil {
		vals = map[string]string{"commercial_flag_pct": t.CommercialFlag.StringFixed(2), "commercial_critical_pct": t.CommercialCrit.StringFixed(2),
			"roll_flag_pct": t.RollFlag.StringFixed(2), "roll_critical_pct": t.RollCrit.StringFixed(2)}
	}
	if n := p.LossNorm; n != nil {
		vals = map[string]any{"bands": n.Bands, "default_loss_pct": n.DefaultLoss.StringFixed(2), "abnormal_alert_pct": n.AlertPct.StringFixed(2)}
	}
	raw, _ := json.Marshal(vals)
	return paramDTO{Kind: p.Kind, Version: p.Seq, ID: p.ID, Status: p.Status, EffectiveFrom: p.EffectiveFrom, Provisional: p.Provisional,
		Reason: p.Reason, ProposedBy: p.ProposedBy, DecidedBy: p.DecidedBy, DecidedAt: p.DecidedAt, Values: raw}
}

type exportDTO struct {
	ID              uuid.UUID       `json:"id"`
	Contract        string          `json:"contract"`
	ContractVersion int             `json:"contract_version"`
	EventType       string          `json:"event_type"`
	SchemaVersion   int             `json:"schema_version"`
	SubjectRef      uuid.UUID       `json:"subject_ref"`
	Revision        int             `json:"revision"`
	Payload         json.RawMessage `json:"payload"`
	PayloadSHA256   string          `json:"payload_sha256"`
	RecordedAt      time.Time       `json:"recorded_at"`
}

func toExport(v store.ValveExport) exportDTO {
	const hexd = "0123456789abcdef"
	h := make([]byte, 0, 64)
	for _, c := range v.PayloadSHA256 {
		h = append(h, hexd[c>>4], hexd[c&15])
	}
	return exportDTO{ID: v.ID, Contract: v.Contract, ContractVersion: v.ContractVersion, EventType: v.EventType, SchemaVersion: v.SchemaVersion,
		SubjectRef: v.SubjectRef, Revision: v.Revision, Payload: v.Payload, PayloadSHA256: string(h), RecordedAt: v.RecordedAt}
}
