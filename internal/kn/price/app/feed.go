package app

// Bảng giá ngày đã duyệt cho cổng tích hợp (W11.81 — port.PublicBoardFeed, hợp đồng contracts/integration/feeds/
// price_board_daily.v1.json; BRD Báo giá v1.3 FN-TI-02.07, BR-04 FN-TI-02.06). Chỉ phiên bản ĐÃ DUYỆT hiện hành của mỗi
// ngày (Board.Published — cùng "nguồn đã chốt" với view pf_price_pub.v1_board_daily): ngày chưa duyệt lần nào không có
// bản ghi; đang mở duyệt / duyệt lại → vẫn phiên bản đã duyệt gần nhất, updating = true; duyệt lại → board_version mới.
// Chỉ dòng toàn tỉnh (CT-04) đã chụp lúc duyệt — không giá theo xã, không phiếu, không cán bộ (không dữ liệu cá nhân).

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
)

// PublicFeedFrom là ngày báo giá sớm nhất đưa vào tập price_board_daily: tập phát TOÀN BỘ lịch sử đã duyệt (vài chục
// nghìn dòng mỗi năm) để phần feed còn giữ + ảnh chụp luôn là trạng thái đầy đủ; không có cửa sổ trượt nên không bao giờ
// retract chỉ vì ngày cũ.
var PublicFeedFrom = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

var provinceCodeRe = regexp.MustCompile(`^[0-9]{2}$`)

// PublicFeed cài đặt port.PublicBoardFeed trên bảng giá của bên dùng Profile (rỗng → bên dùng đầu tiên).
type PublicFeed struct {
	T       *TicketService
	Profile string
}

// PublishedRecords (port.PublicBoardFeed) trả mọi dòng của phiên bản đã duyệt hiện hành của mọi ngày báo giá đến hôm
// nay (giờ VN) của tenant publisher, sắp theo ngày rồi mã mặt hàng.
func (f PublicFeed) PublishedRecords(ctx context.Context, dataset string, publisher uuid.UUID, publisherCode string) ([]json.RawMessage, error) {
	if dataset != port.FeedPriceBoardDaily || !provinceCodeRe.MatchString(publisherCode) || publisher == uuid.Nil {
		return nil, port.ErrUnknownPublicDataset.WithDetail(dataset)
	}
	prof, ok := f.T.calendarProfile(f.Profile)
	if !ok {
		return nil, port.ErrProfileNotFound
	}
	now := f.T.Clock.Now()
	to := domain.BusinessDay(now)
	var out []json.RawMessage
	err := f.T.tx(ctx, publisher, func(tx pgx.Tx) error {
		boards, err := store.BoardsBetween(ctx, tx, prof.Name, PublicFeedFrom, to)
		if err != nil {
			return err
		}
		cur := map[string]domain.Board{}
		days := make([]time.Time, 0, len(boards))
		for d, b := range boards {
			if b.Published() {
				day := domain.BusinessDay(d)
				cur[versionKey(day, b.Version)] = b
				days = append(days, day)
			}
		}
		if len(days) == 0 {
			return nil
		}
		sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
		lines, err := store.BoardLinesBetween(ctx, tx, prof.Name, days[0], days[len(days)-1])
		if err != nil {
			return err
		}
		weeks, err := store.ApprovedWeeks(ctx, tx, prof.Name, days[0], days[len(days)-1])
		if err != nil {
			return err
		}
		for _, l := range lines { // đã sắp theo ngày, phiên bản, mã mặt hàng
			day := domain.BusinessDay(l.Day)
			k := versionKey(day, l.Version)
			b, ok := cur[k]
			if !ok { // dòng của phiên bản cũ (đã bị thay) — không phát
				continue
			}
			approved := ""
			if b.ApprovedAt != nil {
				approved = b.ApprovedAt.UTC().Format(time.RFC3339Nano)
			}
			rec, err := json.Marshal(map[string]any{"province_code": publisherCode, "board_date": day.Format(time.DateOnly),
				"item_code": l.Item, "item_name": l.ItemName, "group_code": l.GroupCode, "unit_code": l.UnitCode,
				"price_min": l.Min.String(), "price_max": l.Max.String(), "price_avg": l.Avg.String(), "points_reported": l.Points,
				"board_version": l.Version, "approved_at": approved, "week_key": weeks[k], "updating": reopenedView(b, now)})
			if err != nil {
				return err
			}
			out = append(out, rec)
		}
		return nil
	})
	return out, err
}
