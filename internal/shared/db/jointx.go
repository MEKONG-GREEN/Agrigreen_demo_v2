package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Giao dịch dùng chung qua ctx (W6.1): port của module năng lực (vd cap/workflow) ghi CÙNG giao dịch của module gọi
// — thay đổi nghiệp vụ và sổ của năng lực cùng commit (R-21) — mà hợp đồng port/ không import driver DB (archlint
// mục 8). Bên gọi gắn giao dịch đang mở bằng JoinTx trước khi gọi port; cài đặt của port lấy lại bằng JoinedTx.
// Giao dịch vẫn chạy dưới vai của bên gọi: port chỉ được dùng lối ghi mà vai đó có quyền (hàm DB được cấp EXECUTE).

type joinedTxKey struct{}

// JoinTx trả ctx mang giao dịch tx đang mở của bên gọi.
func JoinTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, joinedTxKey{}, tx)
}

// JoinedTx trả giao dịch đã gắn bằng JoinTx (false nếu không có).
func JoinedTx(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(joinedTxKey{}).(pgx.Tx)
	return tx, ok && tx != nil
}
