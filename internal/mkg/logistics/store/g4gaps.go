package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Lịch sử phiên bản danh mục phương tiện (G4: web cần danh sách, không chỉ phiên bản hiện hành)
// ---------------------------------------------------------------------------

// VesselVersions liệt kê phiên bản của một phương tiện theo version tăng dần, sau afterVersion.
func VesselVersions(ctx context.Context, tx pgx.Tx, vessel uuid.UUID, afterVersion, limit int) ([]VesselEvent, error) {
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, seq, vessel_id, kind, version, code, vehicle_kind, name, owner_ref, registered_tons::text,
	    straw_payload_tons::text, registration_expiry, rate_type, rate_vnd::text, status, reason, event_time, recorded_at, actor_ref, idem_key,
	    rule_version FROM mkg_logistics.vessel_event WHERE vessel_id = $1 AND version > $2 ORDER BY version, seq LIMIT $3`,
		vessel, afterVersion, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (VesselEvent, error) {
		var e VesselEvent
		var rt, pt, rate *string
		if err := r.Scan(&e.ID, &e.TenantID, &e.Seq, &e.VesselID, &e.Kind, &e.Version, &e.Code, &e.VehicleKind, &e.Name, &e.OwnerRef, &rt, &pt,
			&e.RegistrationExpiry, &e.RateType, &rate, &e.Status, &e.Reason, &e.EventTime, &e.RecordedAt, &e.ActorRef, &e.IdemKey,
			&e.RuleVersion); err != nil {
			return e, err
		}
		var err error
		if e.RegisteredTons, err = parseDec(rt); err != nil {
			return e, err
		}
		if e.StrawPayloadTons, err = parseDec(pt); err != nil {
			return e, err
		}
		e.RateVND, err = parseDec(rate)
		return e, err
	})
}

// ---------------------------------------------------------------------------
// Chứng từ chuyến có người ký (L-11, proto ePOD / e-bill) — mkg 000021
// ---------------------------------------------------------------------------

// TripDocument là một dòng sổ mkg_logistics.trip_document.
type TripDocument struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Seq              int64
	TripID           uuid.UUID
	Kind             string // epod | ebill | minutes
	FileRef          uuid.UUID
	FileSHA256       string
	SignerRole       string // driver | consignee | hub_operator | dispatcher
	SignerRef        *uuid.UUID
	SignedAt         time.Time
	Note             *string
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
	RuleVersion      string
}

const tripDocCols = `id, tenant_id, seq, trip_id, kind, file_ref, file_sha256, signer_role, signer_ref, signed_at, note, event_time, recorded_at,
  supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version`

// InsertTripDocument ghi sổ chứng từ chuyến.
func InsertTripDocument(ctx context.Context, tx pgx.Tx, d *TripDocument) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_logistics.trip_document
	    (id, tenant_id, stream_key, trip_id, kind, file_ref, file_sha256, signer_role, signer_ref, signed_at, note, event_time, supersedes_id,
	     correction_reason, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18) RETURNING seq, recorded_at`,
		d.ID, d.TenantID, "tripdoc:"+d.TripID.String(), d.TripID, d.Kind, d.FileRef, d.FileSHA256, d.SignerRole, d.SignerRef, d.SignedAt.UTC(),
		d.Note, d.EventTime.UTC(), d.SupersedesID, d.CorrectionReason, d.ActorRef, d.DeviceID, d.IdemKey, d.RuleVersion).Scan(&d.Seq, &d.RecordedAt)
}

func scanTripDoc(r pgx.Row) (TripDocument, error) {
	var d TripDocument
	err := r.Scan(&d.ID, &d.TenantID, &d.Seq, &d.TripID, &d.Kind, &d.FileRef, &d.FileSHA256, &d.SignerRole, &d.SignerRef, &d.SignedAt, &d.Note,
		&d.EventTime, &d.RecordedAt, &d.SupersedesID, &d.CorrectionReason, &d.ActorRef, &d.DeviceID, &d.IdemKey, &d.RuleVersion)
	return d, err
}

// TripDocuments liệt kê chứng từ của chuyến theo chuỗi sổ.
func TripDocuments(ctx context.Context, tx pgx.Tx, trip uuid.UUID) ([]TripDocument, error) {
	rows, err := tx.Query(ctx, "SELECT "+tripDocCols+" FROM mkg_logistics.trip_document WHERE trip_id = $1 ORDER BY seq", trip)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (TripDocument, error) { return scanTripDoc(r) })
}

// TripDocumentByIdem đọc chứng từ theo idem_key; nil nếu chưa có.
func TripDocumentByIdem(ctx context.Context, tx pgx.Tx, key string) (*TripDocument, error) {
	rows, err := tx.Query(ctx, "SELECT "+tripDocCols+" FROM mkg_logistics.trip_document WHERE idem_key = $1", key)
	if err != nil {
		return nil, err
	}
	ds, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TripDocument, error) { return scanTripDoc(r) })
	if err != nil || len(ds) == 0 {
		return nil, err
	}
	return &ds[0], nil
}

// TripDocumentByID đọc một chứng từ; nil nếu chưa có.
func TripDocumentByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*TripDocument, error) {
	rows, err := tx.Query(ctx, "SELECT "+tripDocCols+" FROM mkg_logistics.trip_document WHERE id = $1", id)
	if err != nil {
		return nil, err
	}
	ds, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TripDocument, error) { return scanTripDoc(r) })
	if err != nil || len(ds) == 0 {
		return nil, err
	}
	return &ds[0], nil
}
