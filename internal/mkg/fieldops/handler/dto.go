package handler

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
)

const dateLayout = "2006-01-02"

type orderDTO struct {
	OrderID     uuid.UUID   `json:"order_id"`
	Status      string      `json:"status"`
	Revision    int         `json:"revision"`
	HTXRef      uuid.UUID   `json:"htx_ref"`
	CommuneCode string      `json:"commune_code"`
	WindowFrom  string      `json:"window_from"`
	WindowTo    string      `json:"window_to"`
	PlotRefs    []uuid.UUID `json:"plot_refs"`
	CrewRef     *uuid.UUID  `json:"crew_ref"`
	Reason      *string     `json:"reason"`
	CreatedBy   uuid.UUID   `json:"created_by"`
	ApprovedBy  *uuid.UUID  `json:"approved_by"`
	ApprovedAt  *time.Time  `json:"approved_at"`
	Published   bool        `json:"published"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

func toOrder(o store.Order) orderDTO {
	return orderDTO{
		OrderID: o.OrderID, Status: o.Status, Revision: o.Revision, HTXRef: o.HTXRef, CommuneCode: o.CommuneCode,
		WindowFrom: o.WindowFrom.Format(dateLayout), WindowTo: o.WindowTo.Format(dateLayout), PlotRefs: o.PlotRefs,
		CrewRef: o.CrewRef, Reason: o.Reason, CreatedBy: o.CreatorRef, ApprovedBy: o.ApproverRef, ApprovedAt: o.ApprovedAt,
		Published: o.Published, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
	}
}

type orderEventDTO struct {
	ID         uuid.UUID  `json:"id"`
	Seq        int64      `json:"seq"`
	Kind       string     `json:"kind"`
	Revision   int        `json:"revision"`
	CrewRef    *uuid.UUID `json:"crew_ref"`
	Reason     *string    `json:"reason"`
	ActorRef   uuid.UUID  `json:"actor_ref"`
	EventTime  time.Time  `json:"event_time"`
	RecordedAt time.Time  `json:"recorded_at"`
}

func toOrderEvent(e store.OrderEvent) orderEventDTO {
	return orderEventDTO{ID: e.ID, Seq: e.Seq, Kind: e.Kind, Revision: e.Revision, CrewRef: e.CrewRef, Reason: e.Reason,
		ActorRef: e.ActorRef, EventTime: e.EventTime, RecordedAt: e.RecordedAt}
}

type exportDTO struct {
	ID              uuid.UUID       `json:"id"`
	Contract        string          `json:"contract"`
	ContractVersion int             `json:"contract_version"`
	EventType       string          `json:"event_type"`
	SubjectRef      uuid.UUID       `json:"subject_ref"`
	Payload         json.RawMessage `json:"payload"`
	EventTime       time.Time       `json:"event_time"`
	RecordedAt      time.Time       `json:"recorded_at"`
}

func toExports(xs []store.ValveExport) []exportDTO {
	out := make([]exportDTO, 0, len(xs))
	for _, x := range xs {
		out = append(out, exportDTO{ID: x.ID, Contract: x.Contract, ContractVersion: x.ContractVersion, EventType: x.EventType,
			SubjectRef: x.SubjectRef, Payload: x.Payload, EventTime: x.EventTime, RecordedAt: x.RecordedAt})
	}
	return out
}

func toOrderResult(r app.OrderResult) map[string]any {
	return map[string]any{"order": toOrder(r.Order), "event": toOrderEvent(r.Event), "publications": toExports(r.Exports)}
}

type rollDTO struct {
	ID                 uuid.UUID  `json:"id"`
	Seq                int64      `json:"seq"`
	OrderID            uuid.UUID  `json:"order_id"`
	PlotRef            uuid.UUID  `json:"plot_ref"`
	HTXRef             uuid.UUID  `json:"htx_ref"`
	Stage              string     `json:"stage"`
	Rolls              int        `json:"rolls"`
	LooseKg            *string    `json:"loose_kg"`
	DiscrepancyReason  *string    `json:"discrepancy_reason"`
	HTXConfirmationRef *uuid.UUID `json:"htx_confirmation_ref"`
	SupersedesID       *uuid.UUID `json:"supersedes_id"`
	CorrectionReason   *string    `json:"correction_reason"`
	DeviceID           *string    `json:"device_id"`
	RuleVersion        string     `json:"rule_version"`
	ActorRef           uuid.UUID  `json:"actor_ref"`
	EventTime          time.Time  `json:"event_time"`
	RecordedAt         time.Time  `json:"recorded_at"`
	// G3-2: dấu GPS tùy chọn (sổ roll_count_location) — chỉ có ở GET …/roll-counts.
	GPS       *gpsDTO `json:"gps,omitempty"`
	GPSStatus *string `json:"gps_status,omitempty"`
}

// gpsDTO là tọa độ dạng chuỗi thập phân (không float, R-10).
type gpsDTO struct {
	Lat       string  `json:"lat"`
	Lng       string  `json:"lng"`
	AccuracyM *string `json:"accuracy_m"`
}

func toRoll(r store.RollCount) rollDTO {
	d := rollDTO{ID: r.ID, Seq: r.Seq, OrderID: r.OrderID, PlotRef: r.PlotRef, HTXRef: r.HTXRef, Stage: r.Stage, Rolls: r.Rolls,
		DiscrepancyReason: r.DiscrepancyReason, HTXConfirmationRef: r.HTXConfirmationRef, SupersedesID: r.SupersedesID,
		CorrectionReason: r.CorrectionReason, DeviceID: r.DeviceID, RuleVersion: r.RuleVersion, ActorRef: r.ActorRef,
		EventTime: r.EventTime, RecordedAt: r.RecordedAt}
	if r.LooseKg != nil {
		s := r.LooseKg.String()
		d.LooseKg = &s
	}
	return d
}

type totalDTO struct {
	PlotRef           uuid.UUID `json:"plot_ref"`
	Stage             string    `json:"stage"`
	Rolls             int       `json:"rolls"`
	LooseKg           *string   `json:"loose_kg"`
	RollCountID       uuid.UUID `json:"roll_count_id"`
	DiscrepancyReason *string   `json:"discrepancy_reason"`
	EventTime         time.Time `json:"event_time"`
}

func toTotal(t store.Total) totalDTO {
	d := totalDTO{PlotRef: t.PlotRef, Stage: t.Stage, Rolls: t.Rolls, RollCountID: t.RollCountID,
		DiscrepancyReason: t.DiscrepancyReason, EventTime: t.EventTime}
	if t.LooseKg != nil {
		s := t.LooseKg.String()
		d.LooseKg = &s
	}
	return d
}

type reconDTO struct {
	OrderID           uuid.UUID  `json:"order_id"`
	PlotRef           uuid.UUID  `json:"plot_ref"`
	HTXRef            uuid.UUID  `json:"htx_ref"`
	StatementRef      uuid.UUID  `json:"statement_ref"`
	StatementRevision int        `json:"statement_revision"`
	MKGRolls          int        `json:"mkg_rolls"`
	MKGRollCountID    uuid.UUID  `json:"mkg_roll_count_id"`
	HTXRolls          *int       `json:"htx_rolls"`
	Outcome           *string    `json:"outcome"`
	ConfirmationID    *uuid.UUID `json:"confirmation_id"`
	ConfirmedRevision *int       `json:"confirmed_revision"`
	Status            string     `json:"status"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func toRecon(r store.Recon) reconDTO {
	return reconDTO{OrderID: r.OrderID, PlotRef: r.PlotRef, HTXRef: r.HTXRef, StatementRef: r.StatementRef,
		StatementRevision: r.StatementRevision, MKGRolls: r.MKGRolls, MKGRollCountID: r.MKGRollCountID, HTXRolls: r.HTXRolls,
		Outcome: r.Outcome, ConfirmationID: r.ConfirmationID, ConfirmedRevision: r.ConfirmedRevision, Status: r.Status,
		UpdatedAt: r.UpdatedAt}
}

func toCountResult(r app.CountResult) map[string]any {
	out := map[string]any{"roll_count": toRoll(r.Count), "total": toTotal(r.Total), "publications": toExports(r.Exports)}
	if r.Recon != nil {
		out["reconciliation"] = toRecon(*r.Recon)
	} else {
		out["reconciliation"] = nil
	}
	return out
}

type confirmDTO struct {
	ID                uuid.UUID  `json:"id"`
	Seq               int64      `json:"seq"`
	OrderID           uuid.UUID  `json:"order_id"`
	PlotRef           uuid.UUID  `json:"plot_ref"`
	HTXRef            uuid.UUID  `json:"htx_ref"`
	StatementRef      uuid.UUID  `json:"statement_ref"`
	StatementRevision int        `json:"statement_revision"`
	RollCountID       uuid.UUID  `json:"roll_count_id"`
	MKGRolls          int        `json:"mkg_rolls"`
	Outcome           string     `json:"outcome"`
	HTXRolls          int        `json:"htx_rolls"`
	Reason            *string    `json:"reason"`
	Channel           string     `json:"channel"`
	EvidenceRef       *uuid.UUID `json:"evidence_ref"`
	ActorRef          uuid.UUID  `json:"actor_ref"`
	EventTime         time.Time  `json:"event_time"`
	RecordedAt        time.Time  `json:"recorded_at"`
}

func toConfirm(c store.Confirmation) confirmDTO {
	return confirmDTO{ID: c.ID, Seq: c.Seq, OrderID: c.OrderID, PlotRef: c.PlotRef, HTXRef: c.HTXRef, StatementRef: c.StatementRef,
		StatementRevision: c.StatementRevision, RollCountID: c.RollCountID, MKGRolls: c.MKGRolls, Outcome: c.Outcome,
		HTXRolls: c.HTXRolls, Reason: c.Reason, Channel: c.Channel, EvidenceRef: c.EvidenceRef, ActorRef: c.ActorRef,
		EventTime: c.EventTime, RecordedAt: c.RecordedAt}
}

type shiftDTO struct {
	ID            uuid.UUID `json:"id"`
	CrewRef       uuid.UUID `json:"crew_ref"`
	OrderID       uuid.UUID `json:"order_id"`
	Kind          string    `json:"kind"`
	Headcount     *int      `json:"headcount"`
	EquipmentRefs []string  `json:"equipment_refs"`
	ActorRef      uuid.UUID `json:"actor_ref"`
	DeviceID      *string   `json:"device_id"`
	EventTime     time.Time `json:"event_time"`
	RecordedAt    time.Time `json:"recorded_at"`
}

func toShift(s store.CrewShift) shiftDTO {
	return shiftDTO{ID: s.ID, CrewRef: s.CrewRef, OrderID: s.OrderID, Kind: s.Kind, Headcount: s.Headcount,
		EquipmentRefs: s.EquipmentRefs, ActorRef: s.ActorRef, DeviceID: s.DeviceID, EventTime: s.EventTime, RecordedAt: s.RecordedAt}
}
