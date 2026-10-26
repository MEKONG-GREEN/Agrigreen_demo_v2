// Package tenant là cửa duy nhất vào DB nghiệp vụ (RB-2, mục 9).
//
// Mọi truy cập DB đi qua WithTenantTx. Ngữ cảnh tenant đặt bằng
// set_config('app.tenant_id', $1, true) — tức SET LOCAL — trong từng giao dịch
// (R-30). Không bao giờ đặt ở mức phiên: PgBouncer transaction mode sẽ làm
// rò rỉ sang request khác.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrMissing báo thiếu tenant trong ngữ cảnh.
var ErrMissing = errors.New("tenant: thiếu tenant_id")

type ctxKey struct{}

// WithID gắn tenant vào context. Chỉ middleware xác thực được gọi — tenant
// lấy từ token, không bao giờ từ path, query hay body (R-16). Ngoài middleware, chỉ các nơi có
// tên trong danh sách duyệt của archlint agtenantgate (tenant của phong bì sự kiện, job theo tenant…);
// đổi sang tenant bên cấp sau kiểm cấp quyền dùng WithGrant (H4).
func WithID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext lấy tenant đã xác thực.
func FromContext(ctx context.Context) (uuid.UUID, error) {
	id, ok := ctx.Value(ctxKey{}).(uuid.UUID)
	if !ok || id == uuid.Nil {
		return uuid.Nil, ErrMissing
	}
	return id, nil
}

// Beginner là thứ mở được giao dịch — *pgxpool.Pool hoặc pgx.Tx (savepoint).
type Beginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

var _ Beginner = (*pgxpool.Pool)(nil)

// DefaultAcquireTimeout là thời gian chờ tối đa để MỞ giao dịch (lấy kết nối từ pool + BEGIN) khi tiến trình
// chưa đặt khác (P0-POOL-SHARED, #126): pool cạn thì request/job nhận lỗi rõ ràng thay vì treo tới hạn của ctx
// (hoặc vô hạn với ctx không hạn). Ghi đè bằng AG_DB_ACQUIRE_TIMEOUT (db.OptionsFromEnv → db.Open).
const DefaultAcquireTimeout = 30 * time.Second

// ErrAcquireTimeout: hết AcquireTimeout mà chưa lấy được kết nối/mở được giao dịch (pool cạn hoặc DB chậm).
var ErrAcquireTimeout = errors.New("tenant: hết thời gian chờ kết nối DB để mở giao dịch")

var acquireTimeout atomic.Int64

func init() { acquireTimeout.Store(int64(DefaultAcquireTimeout)) }

// SetAcquireTimeout đặt thời gian chờ mở giao dịch cho MỌI pool của tiến trình (d ≤ 0 = không giới hạn, chỉ theo ctx).
func SetAcquireTimeout(d time.Duration) {
	if d < 0 {
		d = 0
	}
	acquireTimeout.Store(int64(d))
}

// AcquireTimeout trả thời gian chờ mở giao dịch hiện hành (0 = không giới hạn).
func AcquireTimeout() time.Duration { return time.Duration(acquireTimeout.Load()) }

// beginBounded mở giao dịch với hạn chờ AcquireTimeout (chỉ cho bước lấy kết nối + BEGIN; thân giao dịch chạy
// theo ctx của người gọi). Hạn của ctx người gọi ngắn hơn thì hạn đó thắng.
func beginBounded(ctx context.Context, db Beginner, opts pgx.TxOptions) (pgx.Tx, error) {
	d := AcquireTimeout()
	if d <= 0 {
		return db.BeginTx(ctx, opts)
	}
	bctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	tx, err := db.BeginTx(bctx, opts)
	if err != nil && ctx.Err() == nil && errors.Is(bctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("%w (%s): %w", ErrAcquireTimeout, d, err)
	}
	return tx, err
}

// WithTenantTx mở giao dịch, đặt app.tenant_id bằng SET LOCAL, chạy fn.
// fn trả lỗi → rollback. Không có đường nào khác để chạm bảng nghiệp vụ.
func WithTenantTx(ctx context.Context, db Beginner, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return WithTenantTxOptions(ctx, db, tid, pgx.TxOptions{}, fn)
}

// WithTenantTxOptions như WithTenantTx, cho phép chọn mức cô lập.
func WithTenantTxOptions(ctx context.Context, db Beginner, tid uuid.UUID, opts pgx.TxOptions, fn func(pgx.Tx) error) (err error) {
	if tid == uuid.Nil {
		return ErrMissing
	}
	tx, err := beginBounded(ctx, db, opts)
	if err != nil {
		return fmt.Errorf("tenant: mở giao dịch: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	// is_local = true: giá trị tự hết khi giao dịch kết thúc.
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tid.String()); err != nil {
		return fmt.Errorf("tenant: đặt ngữ cảnh: %w", err)
	}
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("tenant: commit: %w", err)
	}
	return nil
}

// Lỗi của bằng chứng cấp quyền đổi tenant (H4 — chốt tạm trước k_ref.share_scope, mục 17.3).
var (
	// ErrGrantMissing: gọi Within / WithGrant không có bằng chứng cấp quyền (Grant zero).
	ErrGrantMissing = errors.New("tenant: thiếu bằng chứng cấp quyền đổi tenant (tenant.Grant)")
	// ErrGrantMismatch: bằng chứng cấp cho cặp tenant khác ngữ cảnh hiện tại (tenant nguồn không khớp).
	ErrGrantMismatch = errors.New("tenant: bằng chứng cấp quyền không khớp tenant hiện tại")
)

// Grant là bằng chứng rằng mã kiểm cấp quyền chia sẻ (ADR-0020; pf/party) đã cho phép đi TỪ tenant
// from SANG tenant đích. Đây là chốt tạm của H4 trước khi có k_ref.share_scope (mục 17.3): vai module
// vẫn đặt được app.tenant_id tùy ý, nên quyền đổi tenant trong mã Go chỉ đi qua Within / WithGrant,
// và cả hai đòi Grant.
//
// Trường không xuất: ngoài gói này chỉ tạo được bằng MintGrant, và archlint (agtenantgate) chỉ cho gọi
// MintGrant ở các gói kiểm cấp quyền đã duyệt (pf/party app/, store/). Giá trị zero không hợp lệ.
type Grant struct {
	from, to uuid.UUID
}

// MintGrant đúc bằng chứng đi từ tenant from sang tenant to. CHỈ gọi sau khi đã kiểm cấp quyền chia
// sẻ đang hiệu lực (hoặc ở thao tác cấp quyền của chính bên cấp) — archlint agtenantgate giới hạn nơi gọi.
func MintGrant(from, to uuid.UUID) Grant {
	return Grant{from: from, to: to}
}

// From là tenant nguồn (ngữ cảnh hiện hành khi đổi).
func (g Grant) From() uuid.UUID { return g.from }

// Tenant là tenant đích được phép vào.
func (g Grant) Tenant() uuid.UUID { return g.to }

// Valid cho biết bằng chứng có đủ hai tenant.
func (g Grant) Valid() bool { return g.from != uuid.Nil && g.to != uuid.Nil }

// WithGrant gắn tenant đích của g vào context (thay cho WithID với tenant bên cấp). Tenant hiện có
// trong context phải là g.From() — bằng chứng không dùng được từ ngữ cảnh tenant khác.
func WithGrant(ctx context.Context, g Grant) (context.Context, error) {
	if !g.Valid() {
		return ctx, ErrGrantMissing
	}
	cur, err := FromContext(ctx)
	if err != nil {
		return ctx, err
	}
	if cur != g.from {
		return ctx, ErrGrantMismatch
	}
	return context.WithValue(ctx, ctxKey{}, g.to), nil
}

// Within chạy fn trong CÙNG giao dịch tx nhưng với ngữ cảnh tenant g.Tenant() (SET LOCAL), rồi
// trả ngữ cảnh về tenant trước đó. Dùng khi một giao dịch hợp lệ phải chạm dữ liệu của
// tenant khác sau khi pf/party đã kiểm một cấp quyền chia sẻ tường minh (ADR-0020) — vd
// consumer van ghi vào tenant bên cấp, hoặc ghi bảng chiếu cấp quyền ở tenant bên nhận.
// Đòi bằng chứng g (H4): thiếu → ErrGrantMissing; ngữ cảnh hiện hành của tx ≠ g.From() → ErrGrantMismatch.
// RLS vẫn áp dụng đầy đủ (không có vai BYPASSRLS): fn chỉ thấy và ghi được dòng của tenant đích.
// fn trả lỗi → vẫn thử khôi phục: người gọi nuốt lỗi rồi dùng tiếp tx thì không chạy dưới tenant lạ. Giao dịch đã
// hỏng (lỗi SQL) thì khôi phục cũng lỗi — bỏ qua, mọi lệnh sau đều bị từ chối tới rollback; trả lỗi gốc của fn.
func Within(ctx context.Context, tx pgx.Tx, g Grant, fn func() error) error {
	if !g.Valid() {
		return ErrGrantMissing
	}
	var prev string
	if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('app.tenant_id', true), '')").Scan(&prev); err != nil {
		return fmt.Errorf("tenant: đọc ngữ cảnh: %w", err)
	}
	if prev != g.from.String() {
		return ErrGrantMismatch
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", g.to.String()); err != nil {
		return fmt.Errorf("tenant: đặt ngữ cảnh: %w", err)
	}
	if err := fn(); err != nil {
		_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", prev)
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", prev); err != nil {
		return fmt.Errorf("tenant: khôi phục ngữ cảnh: %w", err)
	}
	return nil
}

// FromCtxTx lấy tenant từ context rồi gọi WithTenantTx.
func FromCtxTx(ctx context.Context, db Beginner, fn func(pgx.Tx) error) error {
	tid, err := FromContext(ctx)
	if err != nil {
		return err
	}
	return WithTenantTx(ctx, db, tid, fn)
}
