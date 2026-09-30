package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// WarnStockShortage là mã cảnh báo (không chặn) khi tồn hub của dạng hàng < khối lượng đơn bán (proto stockWarning).
const WarnStockShortage = "orders.stock_shortage"

// Nguồn của kiểm tồn.
const (
	StockSourceHub         = "hub"         // đọc hub/port.Stock (tồn sổ lot_balance)
	StockSourceUnavailable = "unavailable" // chưa nối hub / hub lỗi — không cảnh báo, không chặn
)

// StockCheck là kết quả kiểm tồn của đơn bán (O-05) — tính lúc đọc, KHÔNG ghi sổ (tồn đổi theo thời gian).
type StockCheck struct {
	Source       string
	Product      string
	OrderedKg    decimal.Decimal
	AvailableKg  decimal.Decimal
	ShortageKg   decimal.Decimal // > 0 khi thiếu
	Facilities   int             // số cơ sở còn tồn dạng hàng này
	Shortage     bool
	Blocking     bool   // orders.sales.stock_shortage_blocking (mặc định 0 = chỉ cảnh báo)
	ParamVersion string // phiên bản tham số blocking đã áp
}

// stockCheck đọc tồn hub của dạng hàng (mọi cơ sở của tenant) cho đơn nháp / đã xác nhận. Lỗi đọc hub không
// bao giờ làm hỏng thao tác đơn bán (chỉ cảnh báo): trả nguồn "unavailable".
func (s *Service) stockCheck(ctx context.Context, tid uuid.UUID, o store.SalesOrder) *StockCheck {
	if o.Status != domain.SODraft && o.Status != domain.SOConfirmed {
		return nil
	}
	c := &StockCheck{Source: StockSourceUnavailable, Product: o.Product, OrderedKg: o.OrderedKg}
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		pv, err := effectiveParam(ctx, tx, port.ParamStockShortageBlocking, s.Clock.Now())
		if err != nil {
			return err
		}
		c.Blocking, c.ParamVersion = pv.Value.Sign() > 0, pv.Label
		return nil
	}); err != nil {
		return c
	}
	if s.Stock == nil {
		return c
	}
	levels, err := s.Stock.StockByProduct(ctx, tid, o.Product, nil)
	if err != nil {
		return c
	}
	total := decimal.Zero
	for _, l := range levels {
		d, err := decimal.NewFromString(l.AvailableKg)
		if err != nil {
			return c
		}
		total = total.Add(d)
		if d.Sign() > 0 {
			c.Facilities++
		}
	}
	c.Source, c.AvailableKg = StockSourceHub, total
	if total.LessThan(o.OrderedKg) {
		c.Shortage, c.ShortageKg = true, o.OrderedKg.Sub(total)
	}
	return c
}

// withStock gắn kiểm tồn vào view đơn bán (sau giao dịch ghi / đọc).
func (s *Service) withStock(ctx context.Context, tid uuid.UUID, v *SOView) {
	v.Stock = s.stockCheck(ctx, tid, v.Order)
}
