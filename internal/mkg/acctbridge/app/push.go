package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Luồng đẩy (B-04, B-07, R-62): chief_accountant duyệt (≠ người dựng) → job đẩy lần 1 → worker gọi
// LedgerAdapter.Push với IdempotencyKey tất định của bút toán:
//   - thành công → sổ 'pushed' + mkg.acctbridge.journal.pushed (costing: kỳ → posted);
//   - ErrAdapterUnavailable (hoặc lỗi lạ của bộ điều hợp) → lần thử ghi 'unavailable', hạn thử lại giãn cách
//     mũ (1, 2, 4… phút, trần 1 giờ); job định kỳ acctbridge_push_retry tạo job mới tới RetryMax lần, hết
//     lượt → DLQ (push_failed, retry_exhausted) + cảnh báo;
//   - ErrAdapterRejected → DLQ ngay (push_failed, adapter_rejected) + cảnh báo; vận hành xử lý nguyên nhân
//     (vd sửa ánh xạ rồi dựng lại, hoặc mở tài khoản bên đích) rồi đẩy lại bằng POST …/retry-push.
// "DLQ" của module là trạng thái bền push_failed + push_attempt (không bao giờ tự xóa); thông điệp hàng
// đợi được ACK vì kết quả đã ghi. Lỗi hạ tầng của chính worker (DB) vẫn đi DLQ RabbitMQ như mọi consumer.

// PushJob là kết quả duyệt / đẩy lại: bút toán + job đẩy.
type PushJob struct {
	Journal store.Journal
	Job     store.Job
}

func (s *Service) enqueuePush(ctx context.Context, tx pgx.Tx, jr store.Journal, attempt int, by uuid.UUID, idem *string) (store.Job, error) {
	last, err := store.LatestBuildJob(ctx, tx, jr.PeriodID)
	if err != nil {
		return store.Job{}, err
	}
	if last == nil {
		return store.Job{}, fmt.Errorf("acctbridge: bút toán %s không có job dựng", jr.JournalID)
	}
	j := store.Job{TenantID: jr.TenantID, JobID: ids.New(), Kind: "push", PeriodID: jr.PeriodID, PeriodCode: jr.PeriodCode,
		PeriodFrom: last.PeriodFrom, PeriodTo: last.PeriodTo, CloseRunID: jr.CloseRunID, ExpectedSourceHash: jr.SourceHash,
		JournalID: &jr.JournalID, Attempt: intPtr(attempt), RequestedBy: by, RequestedAt: s.Clock.Now(), IdemKey: idem}
	if err := store.InsertJob(ctx, tx, j); err != nil {
		return store.Job{}, err
	}
	return j, outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: j.TenantID, Type: port.EventJournalPushRequested, SchemaVersion: 1, OccurredAt: j.RequestedAt,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Kind: outbox.KindInternal,
		Payload: port.JournalPushRequestedV1{JobID: j.JobID, JournalID: jr.JournalID, Attempt: attempt, RequestedBy: by, RequestedAt: j.RequestedAt},
	})
}

func (s *Service) replayPushJob(ctx context.Context, idem string, out *PushJob) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		j, err := store.JobByIdem(ctx, tx, idem)
		if err != nil || j == nil || j.JournalID == nil {
			return false, err
		}
		jr, err := store.GetJournal(ctx, tx, *j.JournalID, false)
		if err != nil || jr == nil {
			return false, err
		}
		out.Journal, out.Job = *jr, *j
		return true, nil
	}
}

// Approve (chief_accountant, bề mặt quản trị MFA) duyệt bút toán 'built' — người duyệt KHÁC người dựng
// (R-56; CHECK DB lớp hai) — và tạo job đẩy lần 1 (202).
func (s *Service) Approve(ctx context.Context, journal uuid.UUID, idem string) (PushJob, bool, error) {
	p, err := authz.Require(ctx, RoleChief)
	if err != nil {
		return PushJob{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return PushJob{}, false, err
	}
	var out PushJob
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		jr, err := store.GetJournal(ctx, tx, journal, false)
		if err != nil {
			return err
		}
		if jr == nil {
			return ErrJournalNotFound
		}
		if err := store.LockPeriod(ctx, tx, p.TenantID, jr.PeriodID); err != nil {
			return err
		}
		if jr, err = store.GetJournal(ctx, tx, journal, true); err != nil {
			return err
		}
		if jr.Status == domain.StatusPushed {
			return ErrPushedImmutable
		}
		if jr.Status != domain.StatusBuilt {
			return ErrNotBuilt.WithDetail("trạng thái: " + jr.Status)
		}
		if err := authz.EnsureDistinctApproverFor(jr.BuiltBy, p, RoleChief); err != nil {
			return err
		}
		now := s.Clock.Now()
		e := journalEventFrom(*jr, domain.StatusApproved, p.ActorRef, now, idem)
		if err := s.appendJournal(ctx, tx, &e); err != nil {
			return err
		}
		job, err := s.enqueuePush(ctx, tx, *jr, 1, p.ActorRef, &idem)
		if err != nil {
			return err
		}
		cur, err := store.GetJournal(ctx, tx, journal, false)
		if err != nil {
			return err
		}
		out.Journal, out.Job = *cur, job
		return nil
	}, s.replayPushJob(ctx, idem, &out))
	return out, replayed, err
}

// RetryPush (accounting_bridge_operator) đẩy lại bút toán đã vào DLQ (push_failed) sau khi xử lý nguyên
// nhân: job mới, lần thử kế tiếp, CÙNG IdempotencyKey (đích không ghi trùng).
func (s *Service) RetryPush(ctx context.Context, journal uuid.UUID, idem string) (PushJob, bool, error) {
	p, err := authz.Require(ctx, RoleOperator)
	if err != nil {
		return PushJob{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return PushJob{}, false, err
	}
	var out PushJob
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		jr, err := store.GetJournal(ctx, tx, journal, true)
		if err != nil {
			return err
		}
		if jr == nil {
			return ErrJournalNotFound
		}
		if jr.Status != domain.StatusPushFailed {
			return ErrNotRetryable.WithDetail("trạng thái: " + jr.Status)
		}
		job, err := s.enqueuePush(ctx, tx, *jr, jr.PushAttempts+1, p.ActorRef, &idem)
		if err != nil {
			return err
		}
		out.Journal, out.Job = *jr, job
		return nil
	}, s.replayPushJob(ctx, idem, &out))
	return out, replayed, err
}

// SummaryJournal dựng port.SummaryJournal từ bảng chiếu (đầu vào của bộ điều hợp và tệp xuất).
func SummaryJournal(jr store.Journal) port.SummaryJournal {
	sj := port.SummaryJournal{JournalID: jr.JournalID, TenantID: jr.TenantID, PeriodID: jr.PeriodID, PeriodCode: jr.PeriodCode,
		CloseRunID: jr.CloseRunID, Currency: jr.Currency, Lines: jr.Lines, TotalDebit: jr.TotalDebit, TotalCredit: jr.TotalCredit,
		SourceHash: jr.SourceHash, MappingVersion: jr.MappingVersion, BuiltBy: jr.BuiltBy, IdempotencyKey: domain.IdempotencyKey(jr.JournalID)}
	if jr.ApprovedBy != nil {
		sj.ApprovedBy = *jr.ApprovedBy
	}
	if jr.ApprovedAt != nil {
		sj.ApprovedAt = *jr.ApprovedAt
	}
	return sj
}

// HandlePushRequested là inbox.Handler của mkg.acctbridge.journal_push (← journal_push.requested).
func (s *Service) HandlePushRequested(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != port.EventJournalPushRequested {
		return nil
	}
	var pl port.JournalPushRequestedV1
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		return fmt.Errorf("acctbridge: payload %s hỏng: %w", env.EventType, err)
	}
	j, err := store.JobByID(ctx, tx, pl.JobID, true)
	if err != nil {
		return err
	}
	if j == nil || j.Kind != "push" || j.Status != "queued" || j.JournalID == nil || j.Attempt == nil {
		return nil // tenant khác hoặc đã xử lý — giao lặp an toàn
	}
	if err := store.LockPeriod(ctx, tx, j.TenantID, j.PeriodID); err != nil {
		return err
	}
	now := s.Clock.Now()
	jr, err := store.GetJournal(ctx, tx, *j.JournalID, true)
	if err != nil {
		return err
	}
	switch {
	case jr == nil:
		code := CodeNotPushable
		return store.FinishJob(ctx, tx, j.JobID, nil, &code, now)
	case jr.Status == domain.StatusPushed:
		return store.FinishJob(ctx, tx, j.JobID, nil, nil, now) // đã nhận — không gọi đích lần nữa (một ExternalRef)
	case jr.Status != domain.StatusApproved && jr.Status != domain.StatusPushFailed:
		code := CodeNotPushable // bị bản dựng lại thay thế trước khi đẩy
		return store.FinishJob(ctx, tx, j.JobID, nil, &code, now)
	}
	attempt := *j.Attempt
	adapter := s.Adapter
	if adapter == nil {
		return errors.New("acctbridge: chưa nối LedgerAdapter")
	}
	sj := SummaryJournal(*jr)
	timeout := s.PushTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	ref, perr := adapter.Push(pctx, sj)
	cancel()
	done := s.Clock.Now()
	a := store.Attempt{TenantID: j.TenantID, ID: ids.New(), JournalID: jr.JournalID, JobID: j.JobID, Attempt: attempt, Adapter: adapter.Name(),
		IdempotencyKey: sj.IdempotencyKey, StartedAt: now, FinishedAt: done}
	m := s.metrics()
	switch {
	case perr == nil:
		a.Outcome, a.ExternalRef, a.ArtifactSHA256 = "succeeded", &ref.Ref, strPtr(ref.ArtifactSHA256)
		if err := store.InsertAttempt(ctx, tx, a); err != nil {
			return err
		}
		e := journalEventFrom(*jr, domain.StatusPushed, j.RequestedBy, done, "pushed:"+jr.JournalID.String())
		name := ref.Adapter
		if name == "" {
			name = adapter.Name()
		}
		e.Adapter, e.ExternalRef, e.ArtifactSHA256, e.Attempt = &name, &ref.Ref, strPtr(ref.ArtifactSHA256), &attempt
		if err := s.appendJournal(ctx, tx, &e); err != nil {
			return err
		}
		if err := store.FinishJob(ctx, tx, j.JobID, nil, nil, done); err != nil {
			return err
		}
		m.attempt(ctx, adapter.Name(), "succeeded")
		m.journalPushed(ctx)
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: j.TenantID, Type: port.EventJournalPushed, SchemaVersion: 1, OccurredAt: done,
			TraceID: telemetry.TraceID(ctx), Producer: Producer, Kind: outbox.KindInternal,
			Payload: port.JournalPushedV1{JournalID: jr.JournalID, PeriodID: jr.PeriodID, PeriodCode: jr.PeriodCode, CloseRunID: jr.CloseRunID,
				SourceHash: jr.SourceHash, Adapter: name, ExternalRef: ref.Ref, LineCount: jr.LineCount, TotalDebit: jr.TotalDebit,
				TotalCredit: jr.TotalCredit, Attempt: attempt, MappingVersion: jr.MappingVersion, PushedAt: done, EventTime: done},
		})
	case errors.Is(perr, port.ErrAdapterRejected):
		code := CodeRejected
		a.Outcome, a.ErrorCode = "rejected", &code
		m.attempt(ctx, adapter.Name(), "rejected")
		return s.deadLetter(ctx, tx, *jr, *j, a, code, "rejected", done)
	default: // ErrAdapterUnavailable hoặc lỗi lạ của bộ điều hợp: tạm thời
		code := CodeUnavailable
		a.Outcome, a.ErrorCode = "unavailable", &code
		m.attempt(ctx, adapter.Name(), "unavailable")
		if attempt >= s.retryMax() {
			exhausted := CodeRetryExhausted
			return s.deadLetter(ctx, tx, *jr, *j, a, exhausted, "retry_exhausted", done)
		}
		// push_failed (đẩy lại tay từ DLQ) lỗi tạm thời: giữ push_failed, không hẹn giờ — vận hành đẩy lại khi
		// đích sẵn sàng. approved: hẹn thử lại giãn cách mũ.
		var at *time.Time
		if jr.Status == domain.StatusApproved {
			next := done.Add(domain.Backoff(attempt))
			at = &next
		}
		a.NextRetryAt = at
		if err := store.InsertAttempt(ctx, tx, a); err != nil {
			return err
		}
		if err := store.ScheduleRetry(ctx, tx, jr.JournalID, attempt, code, at); err != nil {
			return err
		}
		return store.FinishJob(ctx, tx, j.JobID, nil, &code, done)
	}
}

// deadLetter ghi lần thử + sổ push_failed + cảnh báo (R-62).
func (s *Service) deadLetter(ctx context.Context, tx pgx.Tx, jr store.Journal, j store.Job, a store.Attempt, code, reason string, at time.Time) error {
	if err := store.InsertAttempt(ctx, tx, a); err != nil {
		return err
	}
	e := journalEventFrom(jr, domain.StatusPushFailed, j.RequestedBy, at, fmt.Sprintf("push_failed:%s:%d", jr.JournalID, a.Attempt))
	e.FailureCode, e.Attempt = &code, &a.Attempt
	if err := s.appendJournal(ctx, tx, &e); err != nil {
		return err
	}
	s.metrics().dead(ctx, reason)
	return store.FinishJob(ctx, tx, j.JobID, nil, &code, at)
}

// TenantLister liệt kê tenant cần xét (job định kỳ, vai ag_ops_jobs).
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// RetryResult tổng kết một lượt thử lại.
type RetryResult struct{ Enqueued int }

// RetryDue tạo job đẩy mới cho bút toán approved có hạn thử lại ≤ bây giờ (job định kỳ ở api-internal,
// chỉ leader). Tác nhân = SystemActorRef; idempotent (hạn được xóa cùng giao dịch tạo job).
func (s *Service) RetryDue(ctx context.Context, tenants TenantLister) (RetryResult, error) {
	var res RetryResult
	tids, err := tenants(ctx)
	if err != nil {
		return res, err
	}
	for _, tid := range tids {
		err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			due, err := store.DueRetries(ctx, tx, s.Clock.Now(), 100)
			if err != nil {
				return err
			}
			for _, jr := range due {
				if _, err := s.enqueuePush(ctx, tx, jr, jr.PushAttempts+1, SystemActorRef, nil); err != nil {
					return err
				}
				if err := store.ClearRetry(ctx, tx, jr.JournalID); err != nil {
					return err
				}
				res.Enqueued++
			}
			return nil
		})
		if err != nil {
			return res, err
		}
	}
	return res, nil
}
