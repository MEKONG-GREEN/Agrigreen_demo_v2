package app

// CNTS-V4-CHECKIN-MONITOR (#250, BRD CNTS v4.0 FN-12 BR-07/BR-08, AC-10, AC-13, Mục III.5 dòng 13): job
// extension_cnts_alert (mỗi 5 phút, canh mốc tròn — chạy 00:00 giờ Việt Nam, ngay sau mốc 23:59) sinh cảnh báo "Chưa
// check-out" (Cần chú ý) cho người theo dõi lịch cán bộ xã khi mục đã check-in mà tới 23:59 ngày công tác chưa check-out;
// check-out đồng bộ sau đó (bản ngoại tuyến, có thời điểm trong ngày công tác) → hệ thống tự đóng cảnh báo. Mỗi mục một
// cảnh báo (chống trùng theo mục); người dùng đóng tay thì không mở lại.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/store"
	extdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	extstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Giới hạn mỗi lần chạy job cho một tenant.
const (
	noCheckoutPage       = 1000 // check-in chưa check-out đọc mỗi trang (đọc HẾT các trang của cửa sổ quét)
	noCheckoutRaiseLimit = 500  // cảnh báo mới
	noCheckoutOpenPage   = 500  // cảnh báo đang mở xét tự đóng mỗi trang (đi hết các trang)
)

// FollowersFunc trả người theo dõi lịch của owner (người duyệt được lịch, FN-12 BR-04) — gốc lắp ráp nối dịch vụ lịch
// công tác. Gọi ngoài giao dịch DB.
type FollowersFunc func(ctx context.Context, tid uuid.UUID, dept, province string, owner uuid.UUID, at time.Time) ([]uuid.UUID, error)

// noCheckoutScope là phạm vi chống trùng của mục.
func noCheckoutScope(item uuid.UUID) string { return "item:" + item.String() }

// NoCheckout sinh cảnh báo dòng 13 cho check-in đã qua 23:59 ngày công tác mà chưa check-out, và tự đóng cảnh báo của mục
// đã check-out. Cửa sổ quét: ngày công tác trong CheckinMaxLateDays + 1 ngày gần nhất (check-out gửi muộn tối đa 7 ngày).
// Trả số cảnh báo mới.
func (s *Service) NoCheckout(ctx context.Context, tid uuid.UUID, now time.Time) (int, error) {
	zone := clock.Zone()
	today := vnDay(now)
	to := today
	if now.Before(extdomain.NoCheckoutAt(today, zone)) {
		to = today.AddDate(0, 0, -1) // hôm nay chưa tới 23:59
	}
	from := today.AddDate(0, 0, -(extdomain.CheckinMaxLateDays + 1))
	var open []extstore.UnclosedCheckin
	if err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		open = open[:0] // thân giao dịch có thể chạy lại (txdeps)
		var after *extstore.UnclosedCheckin
		for {
			page, e := extstore.UnclosedCheckins(ctx, tx, DeptCNTS, from, to, after, noCheckoutPage)
			if e != nil {
				return e
			}
			open = append(open, page...)
			if len(page) < noCheckoutPage {
				return nil
			}
			after = &page[len(page)-1]
		}
	}); err != nil {
		return 0, err
	}
	// Người nhận đọc NGOÀI giao dịch (cổng module khác — P0-POOL-KN-2).
	type ownerKey struct {
		province string
		owner    uuid.UUID
	}
	recips := map[ownerKey][]uuid.UUID{}
	for _, c := range open {
		k := ownerKey{c.ProvinceCode, c.OwnerRef}
		if _, ok := recips[k]; ok || s.Followers == nil {
			continue
		}
		rs, err := s.Followers(ctx, tid, c.Department, c.ProvinceCode, c.OwnerRef, now)
		if err != nil {
			return 0, err
		}
		recips[k] = rs
	}
	n := 0
	for _, c := range open {
		if n >= noCheckoutRaiseLimit {
			break
		}
		created := false
		err := s.tx(ctx, tid, func(tx pgx.Tx) error {
			src := c.ItemID
			title := fmt.Sprintf("Chưa check-out · %s · ngày %s · check-in %s", c.Content, c.Day.Format("02/01/2006"),
				c.CapturedAt.In(clock.Zone()).Format("15:04"))
			var e error
			_, created, e = s.raise(ctx, tx, tid, Raise{Kind: domain.KindNoCheckout, Subject: c.CommuneCode, SubjectKind: "commune",
				Scope: noCheckoutScope(c.ItemID), CommuneCode: c.CommuneCode, ProvinceCode: c.ProvinceCode, TemplateCode: "BM-06",
				SourceRef: &src, Title: title, Recipients: recips[ownerKey{c.ProvinceCode, c.OwnerRef}], At: now,
				Extra: map[string]any{"schedule_id": c.ScheduleID.String(), "item_id": c.ItemID.String(), "checkin_id": c.CheckinID.String(),
					"owner_ref": c.OwnerRef.String(), "day": c.Day.Format(time.DateOnly), "checkin_at": c.CapturedAt.UTC().Format(time.RFC3339),
					"summary": "Đã check-in, đến 23:59 cùng ngày chưa check-out"}})
			return e
		})
		if dup(err) {
			continue // bản chạy khác vừa sinh
		}
		if err != nil {
			return n, err
		}
		if created {
			n++
		}
	}
	if err := s.closeCheckedOut(ctx, tid, now); err != nil && !dup(err) {
		return n, err
	}
	return n, nil
}

// closeCheckedOut tự đóng cảnh báo "Chưa check-out" của mục đã có check-out (BR-08: check-out đồng bộ trễ), đi hết cảnh
// báo đang mở của loại theo con trỏ ổn định, mỗi trang một giao dịch ngắn.
func (s *Service) closeCheckedOut(ctx context.Context, tid uuid.UUID, now time.Time) error {
	var after *store.OpenCursor
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var next *store.OpenCursor
		err := s.tx(ctx, tid, func(tx pgx.Tx) error {
			next = nil // thân giao dịch có thể chạy lại (txdeps)
			open, err := store.OpenByKind(ctx, tx, domain.KindNoCheckout, after, noCheckoutOpenPage)
			if err != nil {
				return err
			}
			if len(open) == noCheckoutOpenPage {
				next = store.OpenCursorOf(open[len(open)-1])
			}
			for i := range open {
				a := &open[i]
				if a.SourceRef == nil {
					continue
				}
				o, err := extstore.CheckoutOfItem(ctx, tx, *a.SourceRef)
				if err != nil {
					return err
				}
				if o == nil {
					continue
				}
				if err := s.closeNoCheckout(ctx, tx, tid, a.DedupKey, *o, now); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil || next == nil {
			return err
		}
		after = next
	}
}

// closeNoCheckout tự đóng cảnh báo của khóa key khi mục đã check-out o.
func (s *Service) closeNoCheckout(ctx context.Context, tx pgx.Tx, tid uuid.UUID, key string, o extstore.Checkout, at time.Time) error {
	if err := store.Lock(ctx, tx, key); err != nil {
		return err
	}
	a, err := store.ByDedup(ctx, tx, key)
	if err != nil || a == nil || a.Status == domain.StatusClosed {
		return err
	}
	z := clock.Zone()
	note := fmt.Sprintf("Đã check-out lúc %s", o.CapturedAt.In(z).Format("15:04 02/01/2006"))
	if extdomain.SyncedLate(o.Day, o.EventTime, z) {
		note += fmt.Sprintf(", Đồng bộ trễ lúc %s", o.EventTime.In(z).Format("15:04 02/01/2006"))
	}
	_, err = s.apply(ctx, tx, tid, a, domain.ActionAutoClosed, applyInput{Note: note + " — hệ thống tự đóng", Actor: SystemActor, At: at,
		Idem: "auto:" + key + ":" + itoa(a.LastSeq)})
	return err
}
