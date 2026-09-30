package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/store"
	costingport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Luồng dựng (mẫu mục 12, B-02): mkg.costing.period.closed → job dựng (đầu vào chốt: kỳ, lần đóng kỳ,
// source_hash mong đợi, phiên bản ánh xạ hiệu lực đến cuối kỳ) → worker đọc PeriodSummary qua
// costing/port.ClosedPeriods, kiểm lần đóng kỳ + băm, dựng tất định, ghi sổ + bảng chiếu cùng giao dịch.

var (
	periodCodeRe = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)
	sha256Re     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func (s *Service) enqueueBuild(ctx context.Context, tx pgx.Tx, j store.Job) error {
	if err := store.InsertJob(ctx, tx, j); err != nil {
		return err
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: j.TenantID, Type: port.EventJournalBuildRequested, SchemaVersion: 1, OccurredAt: j.RequestedAt,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Kind: outbox.KindInternal,
		Payload: port.JournalBuildRequestedV1{JobID: j.JobID, PeriodID: j.PeriodID, CloseRunID: j.CloseRunID, RequestedBy: j.RequestedBy,
			RequestedAt: j.RequestedAt},
	})
}

// mappingFor chốt phiên bản ánh xạ đã duyệt hiệu lực đến ngày cuối kỳ (thời điểm sự việc); nil = chưa có.
func mappingFor(ctx context.Context, tx pgx.Tx, j *store.Job) error {
	m, err := store.EffectiveMapping(ctx, tx, domain.PeriodEndDate(j.PeriodTo, clock.Zone()))
	if err != nil {
		return err
	}
	if m != nil {
		j.MappingVersion, j.MappingVersionID = intPtr(m.VersionSeq), &m.VersionID
	}
	return nil
}

// HandlePeriodClosed là inbox.Handler của mkg.acctbridge.period_closed (← mkg.costing.period.closed): tạo
// job dựng bút toán, idempotent theo (period_id, close_run_id, phiên bản ánh xạ). Người yêu cầu = người
// đóng kỳ (ClosedBy) — người đó không được duyệt bút toán của kỳ (dựng ≠ duyệt, R-56).
func (s *Service) HandlePeriodClosed(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != costingport.EventPeriodClosed {
		return nil
	}
	var e costingport.PeriodClosedV1
	if err := json.Unmarshal(env.Payload, &e); err != nil {
		return fmt.Errorf("acctbridge: payload %s hỏng: %w", env.EventType, err)
	}
	if e.PeriodID == uuid.Nil || e.CloseRunID == uuid.Nil || !periodCodeRe.MatchString(e.PeriodCode) || !sha256Re.MatchString(e.ResultSetSHA256) ||
		!e.To.After(e.From) || e.ClosedBy == uuid.Nil {
		return fmt.Errorf("acctbridge: payload %s thiếu trường bắt buộc", env.EventType)
	}
	if err := store.LockPeriod(ctx, tx, env.TenantID, e.PeriodID); err != nil {
		return err
	}
	j := store.Job{TenantID: env.TenantID, JobID: ids.New(), Kind: "build", PeriodID: e.PeriodID, PeriodCode: e.PeriodCode, PeriodFrom: e.From,
		PeriodTo: e.To, CloseRunID: e.CloseRunID, ExpectedSourceHash: e.ResultSetSHA256, RequestedBy: e.ClosedBy, RequestedAt: s.Clock.Now()}
	if err := mappingFor(ctx, tx, &j); err != nil {
		return err
	}
	if prev, err := store.ActiveBuildJob(ctx, tx, e.PeriodID, e.CloseRunID, j.MappingVersion); err != nil || prev != nil {
		return err // đã có job cho cùng đầu vào — sự kiện khác cùng kỳ/lần đóng
	}
	return s.enqueueBuild(ctx, tx, j)
}

// RequestRebuild (accounting_bridge_operator) yêu cầu dựng lại bút toán của kỳ theo phiên bản ánh xạ
// hiện hành — chỉ TRƯỚC khi đẩy (bản mới supersedes bản cũ). 202 + job.
func (s *Service) RequestRebuild(ctx context.Context, period uuid.UUID, idem string) (store.Job, bool, error) {
	p, err := authz.Require(ctx, RoleOperator)
	if err != nil {
		return store.Job{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return store.Job{}, false, err
	}
	var out store.Job
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockPeriod(ctx, tx, p.TenantID, period); err != nil {
			return err
		}
		last, err := store.LatestBuildJob(ctx, tx, period)
		if err != nil {
			return err
		}
		if last == nil {
			return ErrPeriodUnknown
		}
		j := store.Job{TenantID: p.TenantID, JobID: ids.New(), Kind: "build", PeriodID: period, PeriodCode: last.PeriodCode,
			PeriodFrom: last.PeriodFrom, PeriodTo: last.PeriodTo, CloseRunID: last.CloseRunID, ExpectedSourceHash: last.ExpectedSourceHash,
			RequestedBy: p.ActorRef, RequestedAt: s.Clock.Now(), IdemKey: &idem}
		if err := mappingFor(ctx, tx, &j); err != nil {
			return err
		}
		act, err := store.ActiveJournal(ctx, tx, period, false)
		if err != nil {
			return err
		}
		if act != nil && act.Status == domain.StatusPushed {
			return ErrPushedImmutable
		}
		if act != nil && j.MappingVersion != nil && act.MappingVersion == *j.MappingVersion && act.CloseRunID == j.CloseRunID {
			return ErrRebuildNoChange
		}
		if prev, err := store.ActiveBuildJob(ctx, tx, period, j.CloseRunID, j.MappingVersion); err != nil {
			return err
		} else if prev != nil && prev.Status == "queued" {
			out = *prev // đang chờ worker — trả job cũ
			return nil
		} else if prev != nil {
			return ErrRebuildNoChange
		}
		out = j
		return s.enqueueBuild(ctx, tx, j)
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.JobByIdem(ctx, tx, idem)
		if err != nil || prev == nil {
			return false, err
		}
		out = *prev
		return true, nil
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return out, false, ErrRebuildNoChange
	}
	return out, replayed, err
}

// HandleBuildRequested là inbox.Handler của mkg.acctbridge.journal_build (← journal_build.requested).
// Lỗi nghiệp vụ (chưa có ánh xạ, kỳ chưa đóng, băm lệch, kỳ rỗng, bản đã đẩy) → job failed có mã, ACK;
// lỗi hạ tầng (costing chưa nối, DB) → trả lỗi để hàng đợi giao lại (cuối cùng DLQ, R-62).
func (s *Service) HandleBuildRequested(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != port.EventJournalBuildRequested {
		return nil
	}
	var pl port.JournalBuildRequestedV1
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		return fmt.Errorf("acctbridge: payload %s hỏng: %w", env.EventType, err)
	}
	j, err := store.JobByID(ctx, tx, pl.JobID, true)
	if err != nil {
		return err
	}
	if j == nil || j.Kind != "build" || j.Status != "queued" {
		return nil // tenant khác hoặc đã xử lý — giao lặp an toàn
	}
	if err := store.LockPeriod(ctx, tx, j.TenantID, j.PeriodID); err != nil {
		return err
	}
	now := s.Clock.Now()
	fail := func(code string) error {
		s.metrics().buildFail(ctx, code)
		return store.FinishJob(ctx, tx, j.JobID, nil, &code, now)
	}
	if j.MappingVersionID == nil {
		return fail(CodeMappingMissing)
	}
	m, err := store.GetMapping(ctx, tx, *j.MappingVersionID, false)
	if err != nil {
		return err
	}
	if m == nil || m.Status != "approved" {
		return fail(CodeMappingMissing)
	}
	if s.Periods == nil {
		return errors.New("acctbridge: chưa nối costing/port.ClosedPeriods")
	}
	sum, err := s.Periods.ClosedPeriod(ctx, j.TenantID, j.PeriodID)
	switch {
	case errors.Is(err, costingport.ErrPeriodNotClosed), errors.Is(err, costingport.ErrPeriodNotFound):
		return fail(CodePeriodNotClosed)
	case err != nil:
		return err
	}
	if sum.CloseRunID != j.CloseRunID || sum.ResultSetSHA256 != j.ExpectedSourceHash || sum.PeriodID != j.PeriodID {
		return fail(CodeSourceMismatch)
	}
	built, err := domain.Build(j.PeriodCode, sum.Lines, domain.Mapping{Version: m.VersionSeq, Rules: m.Rules, Provisional: m.Provisional})
	switch {
	case errors.Is(err, domain.ErrMappingMissing):
		return fail(CodeMappingMissing)
	case errors.Is(err, domain.ErrEmptyPeriod):
		return fail(CodeEmptyPeriod)
	case err != nil:
		return err
	}
	act, err := store.ActiveJournal(ctx, tx, j.PeriodID, true)
	if err != nil {
		return err
	}
	if act != nil && act.Status == domain.StatusPushed {
		return fail(CodePushedImmutable) // sau khi đẩy: điều chỉnh bằng bút toán của costing ở kỳ sau
	}
	if act != nil && act.CloseRunID == j.CloseRunID && act.MappingVersion == m.VersionSeq {
		return store.FinishJob(ctx, tx, j.JobID, &act.JournalID, nil, now) // cùng đầu vào → cùng bút toán
	}
	version, err := store.NextJournalVersion(ctx, tx, j.PeriodID)
	if err != nil {
		return err
	}
	jid := ids.New()
	e := store.JournalEvent{ID: ids.New(), TenantID: j.TenantID, JournalID: jid, PeriodID: j.PeriodID, Kind: domain.StatusBuilt, Version: version,
		PeriodCode: j.PeriodCode, CloseRunID: j.CloseRunID, SourceHash: sum.ResultSetSHA256, LinesSHA256: built.LinesSHA256,
		MappingVersion: m.VersionSeq, Currency: "VND", Lines: built.Lines, LineCount: len(built.Lines), TotalDebit: built.TotalDebit,
		TotalCredit: built.TotalCredit, BuiltBy: j.RequestedBy, BuildJobID: &j.JobID, EventTime: now, ActorRef: j.RequestedBy,
		IdemKey: "built:" + j.JobID.String()}
	if act != nil {
		// Bản dựng lại TRƯỚC khi đẩy: bản cũ → superseded (ghi trước để giữ "một bản hiệu lực mỗi kỳ").
		old, err := store.BuiltEvent(ctx, tx, act.JournalID)
		if err != nil {
			return err
		}
		reason := fmt.Sprintf("Dựng lại theo lần đóng kỳ %s, ánh xạ phiên bản %d (thay phiên bản bút toán %d)", j.CloseRunID, m.VersionSeq, act.Version)
		sup := journalEventFrom(*act, domain.StatusSuperseded, j.RequestedBy, now, "superseded:"+act.JournalID.String())
		sup.SupersededBy = &jid
		if err := s.appendJournal(ctx, tx, &sup); err != nil {
			return err
		}
		e.SupersedesJournalID, e.CorrectionReason = &act.JournalID, &reason
		if old != nil {
			e.SupersedesID = &old.ID
		}
	}
	if err := s.appendJournal(ctx, tx, &e); err != nil {
		return err
	}
	return store.FinishJob(ctx, tx, j.JobID, &jid, nil, now)
}

// journalEventFrom dựng sự kiện trạng thái (không có dòng) từ bảng chiếu.
func journalEventFrom(j store.Journal, kind string, actor uuid.UUID, at time.Time, idem string) store.JournalEvent {
	return store.JournalEvent{ID: ids.New(), TenantID: j.TenantID, JournalID: j.JournalID, PeriodID: j.PeriodID, Kind: kind, Version: j.Version,
		PeriodCode: j.PeriodCode, CloseRunID: j.CloseRunID, SourceHash: j.SourceHash, LinesSHA256: j.LinesSHA256, MappingVersion: j.MappingVersion,
		Currency: j.Currency, LineCount: j.LineCount, TotalDebit: j.TotalDebit, TotalCredit: j.TotalCredit, BuiltBy: j.BuiltBy, EventTime: at,
		ActorRef: actor, IdemKey: idem}
}

func (s *Service) appendJournal(ctx context.Context, tx pgx.Tx, e *store.JournalEvent) error {
	if err := insert(store.InsertJournalEvent(ctx, tx, e)); err != nil {
		return err
	}
	return store.ApplyJournalEvent(ctx, tx, *e)
}
