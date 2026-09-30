package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// InsertVesselCode ghi số hiệu ghe/sà lan của một bản ghi xếp ghe (sổ roll_count_vessel, mkg 000020 — cùng
// giao dịch với roll_count). Sổ riêng thay vì thêm cột: chuỗi băm của roll_count tính trên cả dòng.
func InsertVesselCode(ctx context.Context, tx pgx.Tx, r RollCount, code string) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.roll_count_vessel
	    (id, tenant_id, stream_key, roll_count_id, order_id, vessel_code, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		ids.New(), r.TenantID, "vessel:"+r.ID.String(), r.ID, r.OrderID, code, r.EventTime, r.ActorRef, r.DeviceID, r.IdemKey+":vessel",
		domain.RuleVessel)
	return err
}

// VesselCodeOf đọc số hiệu ghe của một bản ghi đếm (nil nếu không có).
func VesselCodeOf(ctx context.Context, tx pgx.Tx, rollCountID uuid.UUID) (*string, error) {
	var code string
	err := tx.QueryRow(ctx, `SELECT vessel_code FROM mkg_fieldops.roll_count_vessel WHERE roll_count_id = $1`, rollCountID).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &code, nil
}

// VesselCodesOf đọc số hiệu ghe của mọi bản ghi xếp ghe của một lệnh gom, theo roll_count_id.
func VesselCodesOf(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := tx.Query(ctx, `SELECT roll_count_id, vessel_code FROM mkg_fieldops.roll_count_vessel WHERE order_id = $1`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var c string
		if err := rows.Scan(&id, &c); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}
