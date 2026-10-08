// Package ledger là phần Go của sổ chỉ ghi thêm (mục 8).
//
// Chuỗi băm, seq, prev_hash, row_hash và chống trùng idem_key đều tính trong
// DB (shared.fn_chain). Package này chỉ cung cấp: các cột chung của bản ghi sổ,
// nhận diện ghi trùng, và xác minh chuỗi.
//
// # Thứ tự khóa khi ghi nhiều chuỗi trong một giao dịch
//
// Mỗi lần INSERT vào bảng sổ, fn_chain khóa dòng đầu chuỗi shared.chain_head
// (ledger, tenant_id, stream_key) bằng SELECT … FOR UPDATE và giữ khóa tới hết giao
// dịch. Hai giao dịch cùng ghi hai chuỗi A, B theo thứ tự ngược nhau (A rồi B / B rồi A)
// sẽ deadlock (PostgreSQL hủy một bên, 40P01). Quy ước: giao dịch ghi NHIỀU chuỗi (cùng
// hoặc khác bảng sổ) phải ghi theo thứ tự tăng dần của (tên sổ, stream_key) — sắp danh
// sách trước khi INSERT (vd slices.SortFunc theo ledger rồi stream_key; stream_key so
// sánh theo byte như collation "C"). Ghi lặp nhiều dòng cùng một chuỗi không cần sắp.
// Package không có hàm ghi nhiều chuỗi: module tự ghi bằng SQL của mình nên tự giữ
// quy ước này.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Meta là các cột bắt buộc của mọi bảng sổ theo mẫu mục 8.1 (trừ các cột do DB tính).
type Meta struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	StreamKey        string
	EventTime        time.Time // R-13: lúc xảy ra tại hiện trường
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
	RuleVersion      string
}

// Validate kiểm các ràng buộc phía ứng dụng trước khi ghi.
func (m Meta) Validate() error {
	switch {
	case m.ID == uuid.Nil || m.ID.Version() != 7:
		return errors.New("ledger: id phải là UUIDv7")
	case m.TenantID == uuid.Nil:
		return errors.New("ledger: thiếu tenant_id")
	case m.StreamKey == "":
		return errors.New("ledger: thiếu stream_key")
	case m.EventTime.IsZero():
		return errors.New("ledger: thiếu event_time")
	case m.ActorRef == uuid.Nil:
		return errors.New("ledger: thiếu actor_ref")
	case m.IdemKey == "":
		return errors.New("ledger: thiếu idem_key")
	case m.RuleVersion == "":
		return errors.New("ledger: thiếu rule_version")
	case (m.SupersedesID == nil) != (m.CorrectionReason == nil):
		return errors.New("ledger: bản ghi bù cần cả supersedes_id và correction_reason")
	}
	return nil
}

// ErrDuplicate: idem_key đã ghi — gọi lặp, đọc lại bản ghi cũ và trả như lần đầu.
var ErrDuplicate = errors.New("ledger: idem_key đã được ghi")

// ErrImmutable: có thao tác UPDATE/DELETE/TRUNCATE vào bảng sổ.
var ErrImmutable = errors.New("ledger: bảng sổ chỉ ghi thêm")

// Classify quy lỗi PostgreSQL của lần ghi sổ về lỗi của package.
func Classify(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch {
	case pg.Code == "23505" && pg.ConstraintName == "ledger_idem_pkey":
		return fmt.Errorf("%w: %s", ErrDuplicate, pg.Detail)
	case pg.Code == "42501":
		return fmt.Errorf("%w: %s", ErrImmutable, pg.Message)
	}
	return err
}

// VerifyResult là kết quả shared.verify_chain.
type VerifyResult struct {
	OK          bool
	RowsChecked int64
	BrokenAtSeq *int64
	Reason      *string
}

// Verify dựng lại chuỗi (tenant, stream) của một bảng sổ. tx phải đang ở ngữ
// cảnh tenant đó (tenant.WithTenantTx).
func Verify(ctx context.Context, tx pgx.Tx, table string, tenantID uuid.UUID, stream string) (VerifyResult, error) {
	var r VerifyResult
	err := tx.QueryRow(ctx, "SELECT ok, rows_checked, broken_at_seq, reason FROM shared.verify_chain($1::regclass, $2, $3)",
		table, tenantID, stream).Scan(&r.OK, &r.RowsChecked, &r.BrokenAtSeq, &r.Reason)
	return r, err
}
