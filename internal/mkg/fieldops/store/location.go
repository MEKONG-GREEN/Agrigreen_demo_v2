package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// Location là một dòng sổ roll_count_location (dấu GPS tùy chọn, G3-2).
type Location struct {
	RollCountID uuid.UUID
	Status      string
	Lat, Lng    *string
	AccuracyM   *string
}

func decStr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

// InsertLocation ghi dấu GPS của một bản ghi đếm (cùng giao dịch với roll_count).
func InsertLocation(ctx context.Context, tx pgx.Tx, r RollCount, l domain.Location) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.roll_count_location
	    (id, tenant_id, stream_key, roll_count_id, order_id, gps_status, lat, lng, accuracy_m, event_time, actor_ref, device_id, idem_key,
	     rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9::numeric, $10, $11, $12, $13, $14)`,
		ids.New(), r.TenantID, "location:"+r.ID.String(), r.ID, r.OrderID, l.Status, decStr(l.Lat), decStr(l.Lng), decStr(l.AccuracyM),
		r.EventTime, r.ActorRef, r.DeviceID, r.IdemKey+":location", domain.RuleLocation)
	return err
}

// LocationsOf đọc dấu GPS của mọi bản ghi đếm của một lệnh gom.
func LocationsOf(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (map[uuid.UUID]Location, error) {
	rows, err := tx.Query(ctx, `SELECT roll_count_id, gps_status, lat::text, lng::text, accuracy_m::text
	    FROM mkg_fieldops.roll_count_location WHERE order_id = $1`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]Location{}
	for rows.Next() {
		var l Location
		if err := rows.Scan(&l.RollCountID, &l.Status, &l.Lat, &l.Lng, &l.AccuracyM); err != nil {
			return nil, err
		}
		out[l.RollCountID] = l
	}
	return out, rows.Err()
}
