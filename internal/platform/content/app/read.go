package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ListInput là bộ lọc danh sách.
type ListInput struct {
	Kind     string
	Tag      string
	Category string
	Q        string
	Statuses []domain.Status // chỉ bề mặt quản trị
	// Cursor người đọc: (published_at, ref); cursor quản trị: ref.
	AfterTime *time.Time
	AfterRef  uuid.UUID
	Limit     int
}

// Page là một trang kết quả.
type Page struct {
	Items []domain.Document
	// NextTime/NextRef: cursor trang sau; NextRef == Nil → hết.
	NextTime *time.Time
	NextRef  uuid.UUID
}

func (in ListInput) filter(vis store.Visibility) (store.Filter, error) {
	if in.Kind != "" && !domain.ValidKind(port.Kind(in.Kind)) {
		return store.Filter{}, problem.Validation.WithField("kind", "không hợp lệ")
	}
	limit := in.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return store.Filter{
		Vis: vis, Statuses: in.Statuses, Kind: in.Kind, Tag: in.Tag, Category: in.Category,
		TSQuery: domain.SearchQuery(in.Q), AfterTime: in.AfterTime, AfterRef: in.AfterRef, Limit: limit + 1,
	}, nil
}

func (s *Service) readerVisibility(ctx context.Context) (authz.Principal, store.Visibility, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return p, store.Visibility{}, problem.Unauthenticated
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return p, store.Visibility{}, err
	}
	vis, err := s.visibility(ctx, sc)
	return p, vis, err
}

// List liệt kê tài liệu ĐÃ XUẤT BẢN trong phạm vi người gọi: toàn quốc + tỉnh + xã
// của mình (KN US-LIB-04, HTX US-NEWS-01). Mọi người dùng đã xác thực của tenant
// đều thấy tài liệu toàn quốc.
func (s *Service) List(ctx context.Context, in ListInput) (Page, error) {
	p, vis, err := s.readerVisibility(ctx)
	if err != nil {
		return Page{}, err
	}
	in.Statuses = []domain.Status{domain.StatusPublished}
	f, err := in.filter(vis)
	if err != nil {
		return Page{}, err
	}
	return s.page(ctx, p, f)
}

func (s *Service) page(ctx context.Context, p authz.Principal, f store.Filter) (Page, error) {
	var items []domain.Document
	err := s.tx(ctx, p, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListDocuments(ctx, tx, f)
		return e
	})
	if err != nil {
		return Page{}, err
	}
	var pg Page
	limit := f.Limit - 1
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		pg.NextRef, pg.NextTime = last.Ref, last.PublishedAt
	}
	pg.Items = items
	return pg, nil
}

// Get đọc bản xuất bản hiện hành của tài liệu kèm URL tệp có hạn và các hướng
// dẫn đặc thù địa phương trong phạm vi (KN FR-13: phân biệt chuẩn quốc gia và
// hướng dẫn riêng tỉnh). Ngoài phạm vi, chưa xuất bản hoặc đã gỡ → 404.
func (s *Service) Get(ctx context.Context, ref uuid.UUID) (View, error) {
	p, vis, err := s.readerVisibility(ctx)
	if err != nil {
		return View{}, err
	}
	var v View
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		d, err := store.DocumentByRef(ctx, tx, ref, false)
		if err != nil {
			return err
		}
		if d == nil || !d.Visible() || !visible(vis, *d) {
			return ErrNotFound
		}
		ver, err := store.Version(ctx, tx, ref, *d.PublishedVersionNo)
		if err != nil {
			return err
		}
		if ver == nil {
			return ErrNotFound
		}
		v = View{Doc: *d, Version: ver, VersionStatus: domain.VersionPublished}
		if d.Kind == port.KindProcedure {
			v.Supplements, err = store.Supplements(ctx, tx, ref, vis)
		}
		return err
	})
	if err != nil {
		return View{}, err
	}
	v.Attachments = s.presign(ctx, v.Version.Content.Attachments)
	return v, nil
}

// VersionInfo là một dòng lịch sử phiên bản.
type VersionInfo struct {
	Version domain.Version
	Status  domain.VersionStatus
	// PublishedAt/PublisherRef của lần xuất bản phiên bản này (nếu có).
	PublishedAt  *time.Time
	PublisherRef *uuid.UUID
	ReturnReason string
}

func history(versions []domain.Version, events []domain.Event) []VersionInfo {
	st := domain.VersionStatuses(versions, events)
	out := make([]VersionInfo, 0, len(versions))
	idx := map[int]int{}
	for _, v := range versions {
		idx[v.VersionNo] = len(out)
		out = append(out, VersionInfo{Version: v, Status: st[v.VersionNo]})
	}
	for _, e := range events {
		if e.VersionNo == nil {
			continue
		}
		i, ok := idx[*e.VersionNo]
		if !ok {
			continue
		}
		switch e.Action {
		case domain.ActionPublished:
			at, by := e.EventTime, e.ActorRef
			out[i].PublishedAt, out[i].PublisherRef = &at, &by
		case domain.ActionReturned:
			out[i].ReturnReason = e.Reason
		}
	}
	return out
}

func (s *Service) loadHistory(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]VersionInfo, error) {
	vs, err := store.Versions(ctx, tx, ref)
	if err != nil {
		return nil, err
	}
	es, err := store.Events(ctx, tx, ref)
	if err != nil {
		return nil, err
	}
	return history(vs, es), nil
}

// PublishedVersions liệt kê các phiên bản ĐÃ TỪNG xuất bản (hiện hành và bị thay)
// của tài liệu đang hiển thị — xuất bản bản mới không xóa bản cũ.
func (s *Service) PublishedVersions(ctx context.Context, ref uuid.UUID) (domain.Document, []VersionInfo, error) {
	p, vis, err := s.readerVisibility(ctx)
	if err != nil {
		return domain.Document{}, nil, err
	}
	var (
		doc domain.Document
		out []VersionInfo
	)
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		d, err := store.DocumentByRef(ctx, tx, ref, false)
		if err != nil {
			return err
		}
		if d == nil || !d.Visible() || !visible(vis, *d) {
			return ErrNotFound
		}
		doc = *d
		all, err := s.loadHistory(ctx, tx, ref)
		for _, h := range all {
			if h.Status == domain.VersionPublished || h.Status == domain.VersionSuperseded {
				out = append(out, h)
			}
		}
		return err
	})
	return doc, out, err
}

// ---------------------------------------------------------------------------
// Bề mặt quản trị (người soạn, người duyệt)
// ---------------------------------------------------------------------------

func (s *Service) staffVisibility(ctx context.Context) (authz.Principal, store.Visibility, error) {
	p, err := authz.Require(ctx, staffRoles...)
	if err != nil {
		return p, store.Visibility{}, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return p, store.Visibility{}, err
	}
	// Quản trị chỉ thấy đúng phạm vi của mình (không mở rộng lên tỉnh cha).
	return p, visOf(sc), nil
}

// AdminList liệt kê mọi trạng thái trong phạm vi (sắp theo ref giảm dần).
func (s *Service) AdminList(ctx context.Context, in ListInput) (Page, error) {
	p, vis, err := s.staffVisibility(ctx)
	if err != nil {
		return Page{}, err
	}
	for _, st := range in.Statuses {
		switch st {
		case domain.StatusDraft, domain.StatusReview, domain.StatusPublished, domain.StatusRetired:
		default:
			return Page{}, problem.Validation.WithField("status", "không hợp lệ")
		}
	}
	f, err := in.filter(vis)
	if err != nil {
		return Page{}, err
	}
	f.ByRef, f.AfterTime = true, nil
	return s.page(ctx, p, f)
}

// AdminGet đọc tài liệu (mọi trạng thái) kèm lịch sử phiên bản.
func (s *Service) AdminGet(ctx context.Context, ref uuid.UUID) (domain.Document, []VersionInfo, error) {
	p, vis, err := s.staffVisibility(ctx)
	if err != nil {
		return domain.Document{}, nil, err
	}
	var (
		doc domain.Document
		out []VersionInfo
	)
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		d, err := store.DocumentByRef(ctx, tx, ref, false)
		if err != nil {
			return err
		}
		if d == nil || !visible(vis, *d) {
			return ErrNotFound
		}
		doc = *d
		out, err = s.loadHistory(ctx, tx, ref)
		return err
	})
	return doc, out, err
}

// AdminVersion đọc đầy đủ một phiên bản (xem trước giao diện mobile trước khi
// xuất bản — HTX US-CMS-01 AC-1: cùng hình dạng dữ liệu như bản xuất bản).
func (s *Service) AdminVersion(ctx context.Context, ref uuid.UUID, no int) (View, error) {
	p, vis, err := s.staffVisibility(ctx)
	if err != nil {
		return View{}, err
	}
	var v View
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		d, err := store.DocumentByRef(ctx, tx, ref, false)
		if err != nil {
			return err
		}
		if d == nil || !visible(vis, *d) {
			return ErrNotFound
		}
		hist, err := s.loadHistory(ctx, tx, ref)
		if err != nil {
			return err
		}
		for _, h := range hist {
			if h.Version.VersionNo == no {
				ver := h.Version
				v = View{Doc: *d, Version: &ver, VersionStatus: h.Status}
				return nil
			}
		}
		return ErrVersionNotFound
	})
	if err != nil {
		return View{}, err
	}
	v.Attachments = s.presign(ctx, v.Version.Content.Attachments)
	return v, nil
}
