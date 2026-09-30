package adapter

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

var _ port.HTXResponseFeed = (*PlatformHTTP)(nil)

// HTXResponses gọi GET /v1/partner/htx-collection/responses (vai mkg_sync, cấp quyền
// htx_collection_responses — ADR-0020). 403/404 (chưa cấp vai / chưa cấp quyền / đã thu hồi)
// → Unavailable: job không tiến con trỏ, thử lại lượt sau.
func (c *PlatformHTTP) HTXResponses(ctx context.Context, after int64, limit int) (port.HTXResponsePage, error) {
	var body struct {
		Items []struct {
			ResponseID        uuid.UUID `json:"response_id"`
			Cursor            int64     `json:"cursor"`
			StatementRef      uuid.UUID `json:"statement_ref"`
			StatementRevision int       `json:"statement_revision"`
			HTXRef            uuid.UUID `json:"htx_ref"`
			PlotRef           uuid.UUID `json:"plot_ref"`
			MKGRolls          int       `json:"mkg_rolls"`
			Outcome           string    `json:"outcome"`
			HTXRolls          int       `json:"htx_rolls"`
			Reason            *string   `json:"reason"`
			RespondedAt       time.Time `json:"responded_at"`
		} `json:"items"`
		NextCursor int64 `json:"next_cursor"`
	}
	path := c.partnerPath("/v1/partner/htx-collection/responses?after=" + strconv.FormatInt(after, 10) + "&limit=" + strconv.Itoa(limit))
	if err := c.get(ctx, path, &body); err != nil {
		if errors.Is(err, port.ErrDirectoryNotFound) {
			return port.HTXResponsePage{}, unavailable("luồng phản hồi HTX: chưa cấp vai mkg_sync / chưa có cấp quyền chia sẻ htx_collection_responses (ADR-0020)")
		}
		return port.HTXResponsePage{}, err
	}
	pg := port.HTXResponsePage{Next: body.NextCursor}
	for _, it := range body.Items {
		pg.Items = append(pg.Items, port.HTXResponse{ResponseID: it.ResponseID, Cursor: it.Cursor, StatementRef: it.StatementRef,
			StatementRevision: it.StatementRevision, HTXRef: it.HTXRef, PlotRef: it.PlotRef, MKGRolls: it.MKGRolls, Outcome: it.Outcome,
			HTXRolls: it.HTXRolls, Reason: it.Reason, RespondedAt: it.RespondedAt})
	}
	if pg.Next < after {
		pg.Next = after
	}
	return pg, nil
}

// FakeHTXFeed là luồng phản hồi trong bộ nhớ (kiểm thử, dữ liệu tổng hợp R-95).
type FakeHTXFeed struct {
	mu    sync.Mutex
	items []port.HTXResponse
	next  int64
	// Down: mô phỏng nền tảng không gọi được.
	Down bool
	// Calls đếm số lần gọi.
	Calls int
}

// Add thêm một phản hồi vào cuối luồng (gán con trỏ tăng dần) và trả con trỏ.
func (f *FakeHTXFeed) Add(r port.HTXResponse) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	r.Cursor = f.next
	f.items = append(f.items, r)
	return r.Cursor
}

// HTXResponses cài đặt port.HTXResponseFeed.
func (f *FakeHTXFeed) HTXResponses(_ context.Context, after int64, limit int) (port.HTXResponsePage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.Down {
		return port.HTXResponsePage{}, port.ErrDirectoryUnavailable
	}
	i := sort.Search(len(f.items), func(i int) bool { return f.items[i].Cursor > after })
	pg := port.HTXResponsePage{Next: after}
	for ; i < len(f.items) && len(pg.Items) < limit; i++ {
		pg.Items = append(pg.Items, f.items[i])
		pg.Next = f.items[i].Cursor
	}
	return pg, nil
}
