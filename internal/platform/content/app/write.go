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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// CreateInput là dữ liệu tạo tài liệu mới (kèm bản nháp phiên bản 1).
type CreateInput struct {
	Kind      port.Kind
	Audience  domain.Audience
	ParentRef *uuid.UUID
	Content   ContentInput
	IdemKey   string
}

// prepare kiểm và chuẩn bị nội dung một phiên bản.
func (s *Service) prepare(ctx context.Context, kind port.Kind, in ContentInput) (domain.Content, error) {
	c := in.content()
	if err := domain.ValidateContent(kind, c); err != nil {
		return c, fieldProblem(err)
	}
	if err := s.checkTags(ctx, c.Tags); err != nil {
		return c, err
	}
	return c, nil
}

// Create tạo tài liệu ở trạng thái nháp (KN FR-12 luồng phụ: lưu nháp trước khi xuất bản).
// Gọi lặp cùng IdemKey trả lại tài liệu cũ, replayed = true.
func (s *Service) Create(ctx context.Context, in CreateInput) (v View, replayed bool, err error) {
	p, err := authz.Require(ctx, editorRoles...)
	if err != nil {
		return v, false, err
	}
	if in.IdemKey == "" {
		return v, false, problem.IdempotencyRequired
	}
	if !domain.ValidKind(in.Kind) {
		return v, false, problem.Validation.WithField("kind", "phải là procedure, guide, news, alert hoặc supplement")
	}
	if err := domain.ValidateAudience(in.Kind, in.Audience, in.ParentRef); err != nil {
		return v, false, fieldProblem(err)
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return v, false, err
	}
	if err := canEdit(p, sc, in.Kind, in.Audience); err != nil {
		return v, false, err
	}
	aud, err := s.resolveAudience(ctx, in.Audience)
	if err != nil {
		return v, false, err
	}
	c, err := s.prepare(ctx, in.Kind, in.Content)
	if err != nil {
		return v, false, err
	}
	now := s.Clock.Now()
	one := 1
	draft := domain.VersionDraft
	doc := domain.Document{
		Ref: ids.New(), Kind: in.Kind, Audience: aud, ParentRef: in.ParentRef, Status: domain.StatusDraft,
		CreatedBy: p.ActorRef, LastVersionNo: 1, WorkingVersionNo: &one, WorkingStatus: &draft, Tags: []string{},
	}
	ver := domain.Version{ID: ids.New(), DocumentRef: doc.Ref, VersionNo: 1, Content: c, EventTime: now, AuthorRef: p.ActorRef, IdemKey: in.IdemKey}

	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		if prev, err := store.VersionByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			d, err := store.DocumentByRef(ctx, tx, prev.DocumentRef, false)
			if err != nil {
				return err
			}
			doc, ver, replayed = *d, *prev, true
			return nil
		}
		if in.ParentRef != nil {
			if err := checkParent(ctx, tx, *in.ParentRef); err != nil {
				return err
			}
		}
		if err := store.InsertDocument(ctx, tx, p.TenantID, &doc); err != nil {
			return err
		}
		return ledger.Classify(store.InsertVersion(ctx, tx, p.TenantID, &ver))
	})
	if err != nil {
		return v, false, err
	}
	return View{Doc: doc, Version: &ver, VersionStatus: domain.VersionDraft}, replayed, nil
}

// checkParent: bản bổ sung địa phương gắn quy trình chuẩn TW đã xuất bản (KN FR-13).
func checkParent(ctx context.Context, tx pgx.Tx, parent uuid.UUID) error {
	d, err := store.DocumentByRef(ctx, tx, parent, false)
	if err != nil {
		return err
	}
	switch {
	case d == nil:
		return ErrParentInvalid.WithDetail("không có quy trình gốc")
	case d.Kind != port.KindProcedure || d.Audience.Level != port.AudienceNational:
		return ErrParentInvalid.WithDetail("quy trình gốc phải là quy trình kỹ thuật chuẩn quốc gia")
	case d.Status != domain.StatusPublished:
		return ErrParentInvalid.WithDetail("quy trình gốc chưa xuất bản hoặc đã gỡ")
	}
	return nil
}

// AttachmentRef chọn một tệp đã tải lên (hoặc có trong phiên bản trước) cho phiên bản mới.
type AttachmentRef struct {
	ID   uuid.UUID
	Role string
}

// VersionInput là dữ liệu một phiên bản nháp mới.
type VersionInput struct {
	Content     ContentInput
	Attachments []AttachmentRef
	IdemKey     string
}

// loadForEdit khóa tài liệu và kiểm quyền soạn.
func (s *Service) loadForEdit(ctx context.Context, tx pgx.Tx, p authz.Principal, sc authz.Scope, ref uuid.UUID) (*domain.Document, error) {
	d, err := store.DocumentByRef(ctx, tx, ref, true)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, ErrNotFound
	}
	if err := canEdit(p, sc, d.Kind, d.Audience); err != nil {
		if !visible(visOf(sc), *d) {
			return nil, ErrNotFound // không lộ sự tồn tại ngoài phạm vi
		}
		return nil, err
	}
	return d, nil
}

func visOf(sc authz.Scope) store.Visibility { return store.Visibility{All: sc.All, Units: sc.Units} }

// NewVersion thêm một phiên bản nháp. Phiên bản cũ giữ nguyên trong sổ; bản đã
// xuất bản vẫn hiển thị cho người đọc tới khi bản mới được duyệt.
func (s *Service) NewVersion(ctx context.Context, ref uuid.UUID, in VersionInput) (v View, replayed bool, err error) {
	p, err := authz.Require(ctx, editorRoles...)
	if err != nil {
		return v, false, err
	}
	if in.IdemKey == "" {
		return v, false, problem.IdempotencyRequired
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return v, false, err
	}
	var doc *domain.Document
	var ver domain.Version
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		if prev, err := store.VersionByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			if prev.DocumentRef != ref {
				return problem.IdempotencyConflict
			}
			d, err := store.DocumentByRef(ctx, tx, ref, false)
			doc, ver, replayed = d, *prev, true
			return err
		}
		d, err := s.loadForEdit(ctx, tx, p, sc, ref)
		if err != nil {
			return err
		}
		doc = d
		switch {
		case d.Status == domain.StatusRetired:
			return ErrInvalidTransition.WithDetail("tài liệu đã gỡ")
		case d.WorkingStatus != nil && *d.WorkingStatus == domain.VersionReview:
			return ErrInvalidTransition.WithDetail("phiên bản đang chờ duyệt — người duyệt trả lại trước khi soạn tiếp")
		}
		c, err := s.prepare(ctx, d.Kind, in.Content)
		if err != nil {
			return err
		}
		atts, err := resolveAttachments(ctx, tx, d, in.Attachments)
		if err != nil {
			return err
		}
		c.Attachments = atts
		if err := domain.ValidateContent(d.Kind, c); err != nil {
			return fieldProblem(err)
		}
		base := d.LastVersionNo
		ver = domain.Version{
			ID: ids.New(), DocumentRef: ref, VersionNo: d.LastVersionNo + 1, Content: c, BasedOnVersion: &base,
			EventTime: s.Clock.Now(), AuthorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		if err := ledger.Classify(store.InsertVersion(ctx, tx, p.TenantID, &ver)); err != nil {
			return err
		}
		draft := domain.VersionDraft
		no := ver.VersionNo
		d.LastVersionNo, d.WorkingVersionNo, d.WorkingStatus = no, &no, &draft
		if d.Status == domain.StatusReview {
			d.Status = domain.StatusDraft
		}
		return store.UpdateDocument(ctx, tx, d, nil, false)
	})
	if err != nil {
		return v, false, err
	}
	return View{Doc: *doc, Version: &ver, VersionStatus: domain.VersionDraft}, replayed, nil
}

// resolveAttachments lấy siêu dữ liệu tệp: tệp đã tải lên cho tài liệu, hoặc tệp
// có trong phiên bản gần nhất (giữ lại khi soạn tiếp, kể cả tệp của bản nhân bản).
func resolveAttachments(ctx context.Context, tx pgx.Tx, d *domain.Document, refs []AttachmentRef) ([]domain.Attachment, error) {
	if len(refs) == 0 {
		return []domain.Attachment{}, nil
	}
	known := map[uuid.UUID]domain.Attachment{}
	if last, err := store.Version(ctx, tx, d.Ref, d.LastVersionNo); err != nil {
		return nil, err
	} else if last != nil {
		for _, a := range last.Content.Attachments {
			known[a.ID] = a
		}
	}
	want := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		want = append(want, r.ID)
	}
	rows, err := store.AttachmentsByIDs(ctx, tx, d.Ref, want)
	if err != nil {
		return nil, err
	}
	for id, r := range rows {
		known[id] = domain.Attachment{ID: r.ID, Key: r.Key, SHA256: r.SHA256, Size: r.Size, ContentType: r.ContentType, FileName: r.FileName}
	}
	out := make([]domain.Attachment, 0, len(refs))
	seen := map[uuid.UUID]bool{}
	for _, r := range refs {
		a, ok := known[r.ID]
		if !ok || seen[r.ID] {
			return nil, ErrAttachmentNotFound.WithField("attachment_ids", r.ID.String())
		}
		seen[r.ID] = true
		switch r.Role {
		case "":
			a.Role = domain.RoleAttachment
		case domain.RoleAttachment, domain.RoleCover:
			a.Role = r.Role
		default:
			return nil, problem.Validation.WithField("attachments.role", "phải là attachment hoặc cover")
		}
		out = append(out, a)
	}
	return out, nil
}

// replayEvent đọc lại chuyển trạng thái đã ghi với cùng khóa (gọi lặp).
func replayEvent(ctx context.Context, tx pgx.Tx, key string, ref uuid.UUID) (*domain.Document, bool, error) {
	prev, err := store.EventByIdemKey(ctx, tx, key)
	if err != nil || prev == nil {
		return nil, false, err
	}
	if prev.DocumentRef != ref {
		return nil, false, problem.IdempotencyConflict
	}
	d, err := store.DocumentByRef(ctx, tx, ref, false)
	return d, true, err
}

func (s *Service) appendEvent(ctx context.Context, tx pgx.Tx, p authz.Principal, ref uuid.UUID, no *int, a domain.Action, reason, key string) (domain.Event, error) {
	e := domain.Event{ID: ids.New(), DocumentRef: ref, VersionNo: no, Action: a, Reason: reason, EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: key}
	return e, ledger.Classify(store.InsertEvent(ctx, tx, p.TenantID, &e))
}

// Submit gửi phiên bản nháp đi duyệt.
func (s *Service) Submit(ctx context.Context, ref uuid.UUID, versionNo int, key string) (domain.Document, bool, error) {
	p, err := authz.Require(ctx, editorRoles...)
	if err != nil {
		return domain.Document{}, false, err
	}
	if key == "" {
		return domain.Document{}, false, problem.IdempotencyRequired
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return domain.Document{}, false, err
	}
	var doc *domain.Document
	var replayed bool
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		var err error
		if doc, replayed, err = replayEvent(ctx, tx, key, ref); err != nil || replayed {
			return err
		}
		d, err := s.loadForEdit(ctx, tx, p, sc, ref)
		if err != nil {
			return err
		}
		doc = d
		if d.WorkingVersionNo == nil || *d.WorkingVersionNo != versionNo || *d.WorkingStatus != domain.VersionDraft {
			return ErrInvalidTransition.WithDetail("chỉ gửi duyệt phiên bản nháp mới nhất")
		}
		if _, err := s.appendEvent(ctx, tx, p, ref, &versionNo, domain.ActionSubmitted, "", key); err != nil {
			return err
		}
		review := domain.VersionReview
		d.WorkingStatus = &review
		if d.Status == domain.StatusDraft {
			d.Status = domain.StatusReview
		}
		return store.UpdateDocument(ctx, tx, d, nil, false)
	})
	if err != nil {
		return domain.Document{}, false, err
	}
	return *doc, replayed, nil
}

// loadForReview khóa tài liệu và kiểm vai/phạm vi người duyệt.
func loadForReview(ctx context.Context, tx pgx.Tx, p authz.Principal, sc authz.Scope, ref uuid.UUID) (*domain.Document, error) {
	d, err := store.DocumentByRef(ctx, tx, ref, true)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, ErrNotFound
	}
	if err := canReview(p, sc, d.Audience); err != nil {
		if !visible(visOf(sc), *d) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return d, nil
}

// Return trả phiên bản đang chờ duyệt về cho người soạn, kèm lý do.
func (s *Service) Return(ctx context.Context, ref uuid.UUID, versionNo int, reason, key string) (domain.Document, bool, error) {
	p, err := authz.Require(ctx, port.RoleReviewer)
	if err != nil {
		return domain.Document{}, false, err
	}
	if key == "" {
		return domain.Document{}, false, problem.IdempotencyRequired
	}
	if err := domain.ValidateReason(reason); err != nil {
		return domain.Document{}, false, fieldProblem(err)
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return domain.Document{}, false, err
	}
	var doc *domain.Document
	var replayed bool
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		var err error
		if doc, replayed, err = replayEvent(ctx, tx, key, ref); err != nil || replayed {
			return err
		}
		d, err := loadForReview(ctx, tx, p, sc, ref)
		if err != nil {
			return err
		}
		doc = d
		if d.WorkingVersionNo == nil || *d.WorkingVersionNo != versionNo || *d.WorkingStatus != domain.VersionReview {
			return ErrInvalidTransition.WithDetail("phiên bản không ở trạng thái chờ duyệt")
		}
		if _, err := s.appendEvent(ctx, tx, p, ref, &versionNo, domain.ActionReturned, reason, key); err != nil {
			return err
		}
		draft := domain.VersionDraft
		d.WorkingStatus = &draft
		if d.Status == domain.StatusReview {
			d.Status = domain.StatusDraft
		}
		return store.UpdateDocument(ctx, tx, d, nil, false)
	})
	if err != nil {
		return domain.Document{}, false, err
	}
	return *doc, replayed, nil
}

// Publish xuất bản phiên bản đang chờ duyệt. Người xuất bản không được là tác
// giả của phiên bản (R-56). Bản xuất bản trước chuyển thành "superseded" — vẫn
// còn nguyên trong sổ. Phát pf.content.document.published (và
// pf.content.alert.published với cảnh báo khẩn) trong cùng giao dịch.
func (s *Service) Publish(ctx context.Context, ref uuid.UUID, versionNo int, key string) (View, bool, error) {
	p, err := authz.Require(ctx, port.RoleReviewer)
	if err != nil {
		return View{}, false, err
	}
	if key == "" {
		return View{}, false, problem.IdempotencyRequired
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return View{}, false, err
	}
	var (
		doc      *domain.Document
		ver      *domain.Version
		replayed bool
	)
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		var err error
		if doc, replayed, err = replayEvent(ctx, tx, key, ref); err != nil {
			return err
		}
		if replayed {
			ver, err = store.Version(ctx, tx, ref, versionNo)
			return err
		}
		d, err := loadForReview(ctx, tx, p, sc, ref)
		if err != nil {
			return err
		}
		doc = d
		if d.WorkingVersionNo == nil || *d.WorkingVersionNo != versionNo || *d.WorkingStatus != domain.VersionReview {
			return ErrInvalidTransition.WithDetail("chỉ xuất bản phiên bản đang chờ duyệt")
		}
		v, err := store.Version(ctx, tx, ref, versionNo)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrVersionNotFound
		}
		ver = v
		// R-56: tác giả không tự duyệt bài của mình.
		if v.AuthorRef == p.ActorRef {
			return problem.SelfApproval.WithDetail("người duyệt không được xuất bản phiên bản do chính mình soạn")
		}
		if err := domain.ValidatePublishable(d.Kind, v.Content); err != nil {
			return fieldProblem(err)
		}
		if d.ParentRef != nil {
			if err := checkParent(ctx, tx, *d.ParentRef); err != nil {
				return err
			}
		}
		e, err := s.appendEvent(ctx, tx, p, ref, &versionNo, domain.ActionPublished, "", key)
		if err != nil {
			return err
		}
		first := d.PublishedVersionNo == nil
		now := e.EventTime
		pub := p.ActorRef
		d.Status = domain.StatusPublished
		d.PublishedVersionNo, d.PublishedVersionID, d.PublishedAt, d.PublisherRef = &versionNo, &v.ID, &now, &pub
		d.WorkingVersionNo, d.WorkingStatus = nil, nil
		d.Title, d.Summary, d.Category, d.Tags = v.Content.Title, v.Content.Summary, v.Content.Category, v.Content.Tags
		search := domain.SearchText(v.Content)
		if err := store.UpdateDocument(ctx, tx, d, &search, true); err != nil {
			return err
		}
		return s.emitPublished(ctx, tx, p, d, v, e, first)
	})
	if err != nil {
		return View{}, false, err
	}
	return View{Doc: *doc, Version: ver, VersionStatus: domain.VersionPublished}, replayed, nil
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Service) emitPublished(ctx context.Context, tx pgx.Tx, p authz.Principal, d *domain.Document, v *domain.Version, e domain.Event, first bool) error {
	var parent *string
	if d.ParentRef != nil {
		ps := d.ParentRef.String()
		parent = &ps
	}
	at := e.EventTime.UTC().Format(time.RFC3339Nano)
	units := d.Audience.Units
	if units == nil {
		units = []string{}
	}
	if err := outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: p.TenantID, Type: port.EventDocumentPublished, SchemaVersion: 1,
		OccurredAt: e.EventTime, TraceID: telemetry.TraceID(ctx), Producer: "pf/content",
		Payload: port.DocumentPublishedV1{
			DocumentRef: d.Ref.String(), Kind: string(d.Kind), VersionNo: v.VersionNo, VersionID: v.ID.String(),
			Title: v.Content.Title, Category: strOrNil(v.Content.Category), Tags: v.Content.Tags,
			AudienceLevel: string(d.Audience.Level), AudienceUnits: units, ProvinceCode: strOrNil(d.Audience.ProvinceCode),
			ParentRef: parent, AuthorRef: v.AuthorRef.String(), PublisherRef: p.ActorRef.String(), PublishedAt: at,
			FirstVersion: first, Seq: e.Seq,
		},
	}); err != nil {
		return err
	}
	if d.Kind != port.KindAlert {
		return nil
	}
	// HTX US-NEWS-03: cảnh báo khẩn tự kích hoạt thông báo đẩy theo vùng ảnh hưởng.
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: p.TenantID, Type: port.EventAlertPublished, SchemaVersion: 1,
		OccurredAt: e.EventTime, TraceID: telemetry.TraceID(ctx), Producer: "pf/content",
		Payload: port.AlertPublishedV1{
			DocumentRef: d.Ref.String(), VersionNo: v.VersionNo, Title: v.Content.Title, Summary: v.Content.Summary,
			AudienceLevel: string(d.Audience.Level), AudienceUnits: units, ProvinceCode: strOrNil(d.Audience.ProvinceCode),
			PublisherRef: p.ActorRef.String(), PublishedAt: at,
		},
	})
}

// Retire gỡ tài liệu: ẩn với người đọc, máy hiện trường nhận tombstone. Mọi
// phiên bản vẫn còn trong sổ (không xóa cứng).
func (s *Service) Retire(ctx context.Context, ref uuid.UUID, reason, key string) (domain.Document, bool, error) {
	p, err := authz.Require(ctx, staffRoles...)
	if err != nil {
		return domain.Document{}, false, err
	}
	if key == "" {
		return domain.Document{}, false, problem.IdempotencyRequired
	}
	if err := domain.ValidateReason(reason); err != nil {
		return domain.Document{}, false, fieldProblem(err)
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return domain.Document{}, false, err
	}
	var doc *domain.Document
	var replayed bool
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		var err error
		if doc, replayed, err = replayEvent(ctx, tx, key, ref); err != nil || replayed {
			return err
		}
		d, err := store.DocumentByRef(ctx, tx, ref, true)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrNotFound
		}
		// Người soạn hoặc người duyệt có phạm vi trên tài liệu.
		editErr := canEdit(p, sc, d.Kind, d.Audience)
		if editErr != nil && canReview(p, sc, d.Audience) != nil {
			if !visible(visOf(sc), *d) {
				return ErrNotFound
			}
			return editErr
		}
		doc = d
		if d.Status == domain.StatusRetired {
			return ErrInvalidTransition.WithDetail("tài liệu đã gỡ")
		}
		e, err := s.appendEvent(ctx, tx, p, ref, nil, domain.ActionRetired, reason, key)
		if err != nil {
			return err
		}
		wasPublished := d.PublishedVersionNo != nil
		at := e.EventTime
		d.Status, d.RetiredAt, d.RetireReason = domain.StatusRetired, &at, reason
		d.WorkingVersionNo, d.WorkingStatus = nil, nil
		if !wasPublished {
			// Chưa từng xuất bản: không có gì trên máy hiện trường.
			d.PublishedVersionID, d.PublishedAt, d.PublisherRef = nil, nil, nil
		}
		return store.UpdateDocument(ctx, tx, d, nil, wasPublished)
	})
	if err != nil {
		return domain.Document{}, false, err
	}
	return *doc, replayed, nil
}

// Duplicate nhân bản tài liệu thành bản nháp mới, giữ nội dung, ảnh bìa, chuyên
// mục và tệp của phiên bản mới nhất (HTX US-CMS-02 AC-1).
func (s *Service) Duplicate(ctx context.Context, ref uuid.UUID, key string) (View, bool, error) {
	p, err := authz.Require(ctx, editorRoles...)
	if err != nil {
		return View{}, false, err
	}
	if key == "" {
		return View{}, false, problem.IdempotencyRequired
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return View{}, false, err
	}
	var (
		doc      domain.Document
		ver      domain.Version
		replayed bool
	)
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		if prev, err := store.VersionByIdemKey(ctx, tx, key); err != nil {
			return err
		} else if prev != nil {
			d, err := store.DocumentByRef(ctx, tx, prev.DocumentRef, false)
			if err != nil {
				return err
			}
			doc, ver, replayed = *d, *prev, true
			return nil
		}
		src, err := store.DocumentByRef(ctx, tx, ref, false)
		if err != nil {
			return err
		}
		if src == nil {
			return ErrNotFound
		}
		if err := canEdit(p, sc, src.Kind, src.Audience); err != nil {
			if !visible(visOf(sc), *src) {
				return ErrNotFound
			}
			return err
		}
		no := src.LastVersionNo
		if src.PublishedVersionNo != nil {
			no = *src.PublishedVersionNo
		}
		sv, err := store.Version(ctx, tx, ref, no)
		if err != nil {
			return err
		}
		if sv == nil {
			return ErrVersionNotFound
		}
		if src.ParentRef != nil {
			if err := checkParent(ctx, tx, *src.ParentRef); err != nil {
				return err
			}
		}
		one := 1
		draft := domain.VersionDraft
		doc = domain.Document{
			Ref: ids.New(), Kind: src.Kind, Audience: src.Audience, ParentRef: src.ParentRef, Status: domain.StatusDraft,
			CreatedBy: p.ActorRef, LastVersionNo: 1, WorkingVersionNo: &one, WorkingStatus: &draft, Tags: []string{},
		}
		ver = domain.Version{ID: ids.New(), DocumentRef: doc.Ref, VersionNo: 1, Content: sv.Content, EventTime: s.Clock.Now(), AuthorRef: p.ActorRef, IdemKey: key}
		if err := store.InsertDocument(ctx, tx, p.TenantID, &doc); err != nil {
			return err
		}
		return ledger.Classify(store.InsertVersion(ctx, tx, p.TenantID, &ver))
	})
	if err != nil {
		return View{}, false, err
	}
	return View{Doc: doc, Version: &ver, VersionStatus: domain.VersionDraft}, replayed, nil
}
