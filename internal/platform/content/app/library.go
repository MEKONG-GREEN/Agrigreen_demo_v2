package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ErrLibraryItemNotFound: tài liệu không có trong danh mục cá nhân.
var ErrLibraryItemNotFound = problem.New(http.StatusNotFound, "content.library_item_not_found", "Tài liệu chưa có trong danh mục đã lưu")

// LibraryEntry là một mục đã lưu kèm tài liệu (nếu còn hiển thị với người đọc).
type LibraryEntry struct {
	Item store.LibraryItem
	Doc  *domain.Document // nil = tài liệu đã gỡ hoặc ra khỏi phạm vi (máy giữ bản đã tải, gắn nhãn "đã gỡ")
}

// SaveDocument lưu tài liệu đang hiển thị vào danh mục cá nhân của người gọi
// (US-LIB-05). Lưu lại tài liệu đã lưu → cập nhật ghi chú.
func (s *Service) SaveDocument(ctx context.Context, ref uuid.UUID, note *string) (LibraryEntry, error) {
	p, vis, err := s.readerVisibility(ctx)
	if err != nil {
		return LibraryEntry{}, err
	}
	if note != nil {
		n := strings.TrimSpace(*note)
		if utf8.RuneCountInString(n) > 500 {
			return LibraryEntry{}, problem.Validation.WithField("note", "tối đa 500 ký tự")
		}
		if n == "" {
			note = nil
		} else {
			note = &n
		}
	}
	var out LibraryEntry
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		d, err := store.DocumentByRef(ctx, tx, ref, false)
		if err != nil {
			return err
		}
		if d == nil || !d.Visible() || !visible(vis, *d) {
			return ErrNotFound
		}
		l := store.LibraryItem{ActorRef: p.ActorRef, DocumentRef: ref, Saved: true, Note: note, EventTime: s.Clock.Now()}
		if err := store.UpsertLibrary(ctx, tx, p.TenantID, &l); err != nil {
			return err
		}
		out = LibraryEntry{Item: l, Doc: d}
		return nil
	})
	return out, err
}

// UnsaveDocument bỏ tài liệu khỏi danh mục cá nhân (không xóa cứng: saved = false,
// máy nhận tombstone của library_item).
func (s *Service) UnsaveDocument(ctx context.Context, ref uuid.UUID) (store.LibraryItem, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return store.LibraryItem{}, problem.Unauthenticated
	}
	var out store.LibraryItem
	err := s.tx(ctx, p, func(tx pgx.Tx) error {
		cur, err := store.LibraryByKey(ctx, tx, p.ActorRef, ref)
		if err != nil {
			return err
		}
		if cur == nil || !cur.Saved {
			return ErrLibraryItemNotFound
		}
		cur.Saved, cur.EventTime = false, s.Clock.Now()
		if err := store.UpsertLibrary(ctx, tx, p.TenantID, cur); err != nil {
			return err
		}
		out = *cur
		return nil
	})
	return out, err
}

// LibraryList là một trang danh mục cá nhân.
type LibraryList struct {
	Items []LibraryEntry
	Next  int64 // cursor trang sau (sync_seq); 0 = hết
}

// Library liệt kê danh mục cá nhân của người gọi, mới lưu trước.
func (s *Service) Library(ctx context.Context, before int64, limit int) (LibraryList, error) {
	p, vis, err := s.readerVisibility(ctx)
	if err != nil {
		return LibraryList{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out LibraryList
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		items, err := store.LibraryPage(ctx, tx, p.ActorRef, before, limit+1)
		if err != nil {
			return err
		}
		if len(items) > limit {
			items = items[:limit]
			out.Next = items[limit-1].SyncSeq
		}
		out.Items = make([]LibraryEntry, 0, len(items))
		for _, it := range items {
			e := LibraryEntry{Item: it}
			d, err := store.DocumentByRef(ctx, tx, it.DocumentRef, false)
			if err != nil {
				return err
			}
			if d != nil && d.Visible() && visible(vis, *d) {
				e.Doc = d
			}
			out.Items = append(out.Items, e)
		}
		return nil
	})
	return out, err
}

// SyncLibraryItem là payload chiều xuống của library_item.
type SyncLibraryItem struct {
	DocumentRef string  `json:"document_ref"`
	SavedAt     string  `json:"saved_at"`
	Note        *string `json:"note"`
	Title       *string `json:"title"`
	Available   bool    `json:"available"` // false: tài liệu đã gỡ — máy giữ bản đã tải, nhãn "đã gỡ"
}

// libraryChanges giao danh mục cá nhân của CHÍNH người gọi (US-LIB-05). Bản thân
// tài liệu đi trong entity document (luôn trong phạm vi của người đã lưu).
func (s Sync) libraryChanges(ctx context.Context, scope authz.Scope, after int64, limit int) ([]syncport.Change, int64, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, after, problem.Unauthenticated
	}
	vis, err := s.Svc.visibility(ctx, scope)
	if err != nil {
		return nil, after, err
	}
	out := []syncport.Change{}
	last := after
	err = tenant.FromCtxTx(ctx, s.Svc.DB, func(tx pgx.Tx) error {
		items, err := store.LibraryChanges(ctx, tx, p.ActorRef, after, limit)
		if err != nil {
			return err
		}
		for _, it := range items {
			last = it.SyncSeq
			ch := syncport.Change{Entity: port.SyncEntityLibrary, ID: it.DocumentRef, Version: it.SyncSeq, Op: syncport.OpTombstone}
			if it.Saved {
				pl := SyncLibraryItem{DocumentRef: it.DocumentRef.String(), SavedAt: it.EventTime.UTC().Format(time.RFC3339Nano), Note: it.Note}
				d, err := store.DocumentByRef(ctx, tx, it.DocumentRef, false)
				if err != nil {
					return err
				}
				if d != nil {
					t := d.Title
					pl.Title = &t
					pl.Available = d.Visible() && visible(vis, *d)
				}
				data, err := json.Marshal(pl)
				if err != nil {
					return err
				}
				ch.Op, ch.Data = syncport.OpUpsert, data
			}
			out = append(out, ch)
		}
		return nil
	})
	return out, last, err
}
