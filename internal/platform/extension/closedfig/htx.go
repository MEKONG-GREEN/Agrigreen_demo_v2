package closedfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
)

// Trạng thái xác nhận HTX (D2, ADR-0053 §8) ở hạt của họ closed_figures/v1:
//   - Dòng của đơn vị là HTX (unit_kind = port.UnitKindCooperative, unit = mã Nút HTX) lấy trạng thái từ
//     port.HTXConfirmations (D2: Nút là bí danh của neo HTX ĐÃ nhận quyền → confirmed; HTX tạm/chưa neo → unconfirmed).
//     Không nối cổng → unconfirmed (thận trọng: không gắn "đã xác nhận" khi không biết).
//   - Dòng của đơn vị khác (xã, cơ sở giết mổ, hộ/điểm, mặt hàng, xã báo giá) không phải HTX → giữ trạng thái của nguồn
//     (hiện mọi nguồn 'confirmed').
//   - Kỳ × biểu: confirmation_status = unconfirmed nếu có ÍT NHẤT MỘT dòng unconfirmed; phần chưa xác nhận lộ ra ở
//     SumUnconfirmed/LastUnconfirmed (cộng theo dòng). totals do nguồn tính (dòng Khu vực/Toàn tỉnh) không tách được.
//   - Đổi trạng thái chỉ đổi rows[].confirmation, ConfirmationStatus và StatusHash — KHÔNG đổi FiguresHash.

// applyHTX cập nhật trạng thái xác nhận của các dòng HTX theo cổng D2 (gọi NGOÀI giao dịch đọc — cổng mở giao dịch riêng).
func applyHTX(ctx context.Context, h port.HTXConfirmations, figs []port.ClosedFigure) error {
	return applyHTXAt(ctx, h, figs, nil)
}

func applyHTXAt(ctx context.Context, h port.HTXConfirmations, figs []port.ClosedFigure, at *time.Time) error {
	type rowT = map[string]any
	type target struct {
		f    *port.ClosedFigure
		rows []rowT
	}
	var targets []target
	var refs []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for i := range figs {
		f := &figs[i]
		if f.Rows == nil || !strings.Contains(string(f.Rows), port.UnitKindCooperative) {
			continue
		}
		var rows []rowT
		if err := json.Unmarshal(f.Rows, &rows); err != nil {
			return fmt.Errorf("closedfig: rows: %w", err)
		}
		targets = append(targets, target{f, rows})
		for _, r := range rows {
			if r["unit_kind"] != port.UnitKindCooperative {
				continue
			}
			if id, err := uuid.Parse(fmt.Sprint(r["unit"])); err == nil && !seen[id] {
				seen[id] = true
				refs = append(refs, id)
			}
		}
	}
	if len(refs) == 0 {
		return nil
	}
	status := map[uuid.UUID]string{}
	if h != nil {
		var m map[uuid.UUID]string
		var err error
		if at != nil {
			history, ok := h.(port.HTXConfirmationHistory)
			if !ok {
				return fmt.Errorf("closedfig: missing historical HTX confirmation reader")
			}
			m, err = history.ConfirmationsAt(ctx, refs, *at)
		} else {
			m, err = h.Confirmations(ctx, refs)
		}
		if err != nil {
			return err
		}
		status = m
	}
	for _, tg := range targets {
		f, rows := tg.f, tg.rows
		f.ConfirmationStatus = port.ConfirmationConfirmed
		var st []string
		for _, r := range rows {
			if r["unit_kind"] == port.UnitKindCooperative {
				s := port.ConfirmationUnconfirmed
				if id, err := uuid.Parse(fmt.Sprint(r["unit"])); err == nil && status[id] == port.ConfirmationConfirmed {
					s = port.ConfirmationConfirmed
				}
				r["confirmation"] = s
			}
			if r["confirmation"] == port.ConfirmationUnconfirmed {
				f.ConfirmationStatus = port.ConfirmationUnconfirmed
			}
			st = append(st, fmt.Sprint(r["key"])+"="+fmt.Sprint(r["confirmation"]))
		}
		raw, err := json.Marshal(rows)
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(strings.Join(st, ";")))
		hs := hex.EncodeToString(sum[:])
		f.Rows, f.StatusHash = raw, &hs
	}
	return nil
}
