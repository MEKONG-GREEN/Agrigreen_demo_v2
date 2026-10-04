package store

// Bật module theo đơn vị (W1.4, migration pf 000088): sổ module_enablement_event + bảng chiếu
// module_enablement_current. Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
)

// ModuleEvent là một dòng sổ module_enablement_event.
type ModuleEvent struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	Module     string
	Status     string // enabled | disabled
	PrevStatus string
	Reason     *string
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
	RecordedAt time.Time
}

// ModuleState là một dòng bảng chiếu module_enablement_current.
type ModuleState struct {
	Module    string
	Status    string
	Reason    *string
	UpdatedBy uuid.UUID
	UpdatedAt time.Time
}

// ModuleStream là stream_key của một module trong tenant.
func ModuleStream(module string) string { return "module:" + module }

// LockModule khóa (tenant, module) đến hết giao dịch: lần ghi đầu chưa có dòng chiếu để FOR UPDATE.
func LockModule(ctx context.Context, tx pgx.Tx, tid uuid.UUID, module string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "pf_party.module_enablement:"+tid.String()+":"+module)
	return err
}

// InsertModuleEvent ghi sổ; DB tính seq, chuỗi băm.
func InsertModuleEvent(ctx context.Context, tx pgx.Tx, e *ModuleEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_party.module_enablement_event
	    (id, tenant_id, stream_key, module, status, prev_status, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING seq, recorded_at`,
		e.ID, e.TenantID, ModuleStream(e.Module), e.Module, e.Status, e.PrevStatus, e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey,
		domain.ModuleEnablementRuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

const moduleEventCols = `id, tenant_id, module, status, prev_status, reason, event_time, actor_ref, idem_key, seq, recorded_at`

func scanModuleEvent(row pgx.Row) (ModuleEvent, error) {
	var e ModuleEvent
	err := row.Scan(&e.ID, &e.TenantID, &e.Module, &e.Status, &e.PrevStatus, &e.Reason, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt)
	return e, err
}

// ModuleEventByIdem đọc dòng sổ đã ghi với idem_key.
func ModuleEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*ModuleEvent, error) {
	return noRows(scanModuleEvent(tx.QueryRow(ctx, "SELECT "+moduleEventCols+" FROM pf_party.module_enablement_event WHERE idem_key = $1::text", key)))
}

// ModuleEvents trả sổ của một module, seq > after, tăng dần.
func ModuleEvents(ctx context.Context, tx pgx.Tx, module string, after int64, limit int) ([]ModuleEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+moduleEventCols+` FROM pf_party.module_enablement_event
	    WHERE stream_key = $1 AND seq > $2 ORDER BY seq LIMIT $3`, ModuleStream(module), after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModuleEvent{}
	for rows.Next() {
		e, err := scanModuleEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

const moduleStateCols = `module, status, reason, updated_by, updated_at`

func scanModuleState(row pgx.Row) (ModuleState, error) {
	var s ModuleState
	err := row.Scan(&s.Module, &s.Status, &s.Reason, &s.UpdatedBy, &s.UpdatedAt)
	return s, err
}

// ModuleStateOf đọc trạng thái hiện hành của module (nil = chưa có dòng = bật mặc định).
func ModuleStateOf(ctx context.Context, tx pgx.Tx, module string) (*ModuleState, error) {
	return noRows(scanModuleState(tx.QueryRow(ctx, "SELECT "+moduleStateCols+" FROM pf_party.module_enablement_current WHERE module = $1", module)))
}

// ModuleStates trả mọi dòng bảng chiếu của tenant, theo tên module.
func ModuleStates(ctx context.Context, tx pgx.Tx) ([]ModuleState, error) {
	rows, err := tx.Query(ctx, "SELECT "+moduleStateCols+" FROM pf_party.module_enablement_current ORDER BY module")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModuleState{}
	for rows.Next() {
		s, err := scanModuleState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PutModuleState ghi bảng chiếu (cùng giao dịch với sổ, R-21).
func PutModuleState(ctx context.Context, tx pgx.Tx, e ModuleEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.module_enablement_current
	    (tenant_id, module, status, reason, last_event_id, last_seq, updated_by, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	  ON CONFLICT (tenant_id, module) DO UPDATE SET status = EXCLUDED.status, reason = EXCLUDED.reason,
	    last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq, updated_by = EXCLUDED.updated_by,
	    updated_at = EXCLUDED.updated_at`,
		e.TenantID, e.Module, e.Status, e.Reason, e.ID, e.Seq, e.ActorRef, e.EventTime.UTC())
	return err
}
