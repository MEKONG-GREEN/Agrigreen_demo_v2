package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
)

// SupportFilter lọc danh sách yêu cầu hỗ trợ cho cán bộ (quản lý HTX, khuyến nông).
type SupportFilter struct {
	All         bool      // platform_admin / TTKNQG: mọi đơn vị
	Units       []string  // xã/tỉnh trong phạm vi
	Actor       uuid.UUID // khác Nil: thêm yêu cầu có nhiệm vụ đang giao cho người này
	Cooperative uuid.UUID // khác Nil: chỉ HTX này (quản lý HTX: HTX của mình)
	TaskStatus  string
	Category    string
	Urgency     string
	CommuneCode string
	GPSMissing  *bool
	Before      uuid.UUID // con trỏ: id < Before (mới nhất trước)
	Limit       int
}

// SupportForStaff liệt kê yêu cầu trong phạm vi, mới nhất trước (cursor = id, UUIDv7).
func SupportForStaff(ctx context.Context, tx pgx.Tx, f SupportFilter) ([]domain.SupportRequest, error) {
	return collectSupport(tx.Query(ctx, "SELECT "+supportCols+supportFrom+`
	  WHERE ($1::boolean OR s.commune_code = ANY($2::text[]) OR s.province_code = ANY($2::text[])
	         OR ($3::uuid IS NOT NULL AND t.assignee_ref = $3::uuid))
	    AND ($4::uuid IS NULL OR s.cooperative_ref = $4::uuid)
	    AND ($5::text = '' OR t.status = $5::text)
	    AND ($6::text = '' OR s.category = $6::text)
	    AND ($7::text = '' OR s.urgency = $7::text)
	    AND ($8::text = '' OR s.commune_code = $8::text)
	    AND ($9::boolean IS NULL OR (s.location IS NULL) = $9::boolean)
	    AND ($10::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR s.id < $10::uuid)
	  ORDER BY s.id DESC LIMIT $11`,
		f.All, units(f.Units), nilIfZero(f.Actor), nilIfZero(f.Cooperative), f.TaskStatus, f.Category, f.Urgency, f.CommuneCode,
		f.GPSMissing, f.Before, f.Limit))
}

// TasksByIDs đọc nhiệm vụ hiện hành theo danh sách mã (không khóa).
func TasksByIDs(ctx context.Context, tx pgx.Tx, taskIDs []uuid.UUID) (map[uuid.UUID]domain.Task, error) {
	out := map[uuid.UUID]domain.Task{}
	if len(taskIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, "SELECT "+taskCols+` FROM pf_extension.task_current WHERE task_id = ANY($1::uuid[])`, uuidStrings(taskIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out[t.ID] = t
	}
	return out, rows.Err()
}
