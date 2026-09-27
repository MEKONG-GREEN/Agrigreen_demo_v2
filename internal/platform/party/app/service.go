// Package app: use case của pf/party. Giao dịch mở ở đây.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Vai nghiệp vụ — khai báo ở port để lớp ngoài không phải import app.
const (
	RoleConsentWriter = port.RoleConsentWriter
	RoleConsentReader = port.RoleConsentReader
)

// Mã lỗi của module.
var (
	ErrNotFound = problem.New(404, "party.consent_not_found", "Không tìm thấy bản ghi đồng ý")
	ErrIDReuse  = problem.New(409, "party.consent_id_reused", "id đã dùng cho bản ghi khác")
)

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai party_app
	Clock clock.Clock
}

var _ port.ConsentChecker = (*Service)(nil)

// RecordInput là dữ liệu vào của Record.
type RecordInput struct {
	ID          *uuid.UUID // UUIDv7 sinh trên thiết bị khi ngoại tuyến (R-11); nil → sinh ở server
	SubjectRef  uuid.UUID
	Purpose     string
	TextVersion string
	Status      domain.Status
	Channel     domain.Channel
	EventTime   time.Time
	DeviceID    *string
	IdemKey     string // = Idempotency-Key của request
}

// Record ghi một bản ghi đồng ý: sổ + bảng chiếu + outbox trong MỘT giao dịch.
// Gọi lặp cùng IdemKey trả lại bản ghi cũ, replayed = true.
func (s *Service) Record(ctx context.Context, in RecordInput) (c domain.Consent, replayed bool, err error) {
	p, err := authz.Require(ctx, RoleConsentWriter)
	if err != nil {
		return c, false, err
	}
	if in.IdemKey == "" {
		return c, false, problem.IdempotencyRequired
	}
	id := ids.New()
	if in.ID != nil {
		id = *in.ID
	}
	c = domain.Consent{
		ID: id, TenantID: p.TenantID, SubjectRef: in.SubjectRef, Purpose: in.Purpose, TextVersion: in.TextVersion,
		Status: in.Status, Channel: in.Channel, EventTime: in.EventTime.UTC(), ActorRef: p.ActorRef,
		DeviceID: in.DeviceID, IdemKey: in.IdemKey,
	}
	if err := c.Validate(s.Clock.Now()); err != nil {
		var fe domain.FieldError
		if errors.As(err, &fe) {
			return c, false, problem.Validation.WithField(fe.Field, fe.Err.Error())
		}
		return c, false, err
	}

	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			c, replayed = *prev, true
			return nil
		}
		if in.ID != nil {
			if prev, err := store.ByID(ctx, tx, id); err != nil {
				return err
			} else if prev != nil {
				return ErrIDReuse
			}
		}
		if err := store.Insert(ctx, tx, &c); err != nil {
			return ledger.Classify(err)
		}
		cur, err := store.CurrentFor(ctx, tx, c.SubjectRef, c.Purpose)
		if err != nil {
			return err
		}
		if c.Supersedes(cur) {
			if err := store.UpsertCurrent(ctx, tx, c); err != nil {
				return err
			}
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: c.TenantID, Type: port.EventConsentRecorded, SchemaVersion: 1,
			OccurredAt: c.EventTime, TraceID: telemetry.TraceID(ctx), Producer: "pf/party",
			Payload: port.ConsentRecordedV1{
				ConsentID: c.ID, SubjectRef: c.SubjectRef, Purpose: c.Purpose, Status: string(c.Status),
				TextVersion: c.TextVersion, EventTime: c.EventTime.Format(time.RFC3339), Seq: c.Seq,
			},
		})
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		// Hai request cùng khóa chạy song song: bên thua đọc lại bản ghi của bên thắng.
		var prev *domain.Consent
		rerr := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.ByIdemKey(ctx, tx, in.IdemKey)
			return e
		})
		if rerr == nil && prev != nil {
			return *prev, true, nil
		}
	}
	return c, replayed, err
}

// Get đọc một bản ghi.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Consent, error) {
	p, err := authz.Require(ctx, RoleConsentReader, RoleConsentWriter)
	if err != nil {
		return domain.Consent{}, err
	}
	var c *domain.Consent
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		c, e = store.ByID(ctx, tx, id)
		return e
	})
	if err != nil {
		return domain.Consent{}, err
	}
	if c == nil {
		return domain.Consent{}, ErrNotFound
	}
	return *c, nil
}

// Page là một trang kết quả.
type Page struct {
	Items []domain.Consent
	Next  *uuid.UUID
}

// List liệt kê lịch sử đồng ý của một chủ thể, phân trang bằng cursor.
func (s *Service) List(ctx context.Context, subject uuid.UUID, after uuid.UUID, limit int) (Page, error) {
	p, err := authz.Require(ctx, RoleConsentReader, RoleConsentWriter)
	if err != nil {
		return Page{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var items []domain.Consent
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListBySubject(ctx, tx, subject, after, limit+1)
		return e
	})
	if err != nil {
		return Page{}, err
	}
	var pg Page
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].ID
		pg.Next = &last
	}
	pg.Items = items
	return pg, nil
}

// HasConsent thực hiện port.ConsentChecker cho module khác.
func (s *Service) HasConsent(ctx context.Context, subject uuid.UUID, purpose string) (bool, error) {
	var ok bool
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		ok, e = store.HasConsent(ctx, tx, subject, purpose)
		return e
	})
	return ok, err
}
