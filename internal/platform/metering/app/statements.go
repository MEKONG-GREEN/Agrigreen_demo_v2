package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

var statementReaders = []string{port.RoleMeteringViewer, port.RoleBillingOfficer, port.RoleBillingApprover}

// StatementRequest là yêu cầu lập bảng kê kỳ tháng.
type StatementRequest struct {
	Period           string
	ClientID         *uuid.UUID // nil = mọi khách hàng có sử dụng trong kỳ
	SupersedesID     *uuid.UUID // bảng kê bù: thay bảng kê này (cùng khách hàng, cùng kỳ)
	CorrectionReason *string
	IdemKey          string
}

// RequestStatement (billing_officer) ghi yêu cầu lập bảng kê + outbox pf.metering.statement.requested
// trong MỘT giao dịch (mẫu §12) → 202 + job; worker dựng ảnh chụp. Kỳ phải đã kết thúc (giờ VN).
func (s *Service) RequestStatement(ctx context.Context, in StatementRequest) (store.Job, bool, error) {
	p, err := authz.Require(ctx, port.RoleBillingOfficer)
	if err != nil {
		return store.Job{}, false, err
	}
	if in.IdemKey == "" {
		return store.Job{}, false, problem.IdempotencyRequired
	}
	_, end, err := domain.ParsePeriod(in.Period)
	if err != nil {
		return store.Job{}, false, problem.Validation.WithField("period", err.Error())
	}
	now := s.Clock.Now()
	if now.Before(end) {
		return store.Job{}, false, port.ErrPeriodNotEnded.WithDetail("kỳ " + in.Period + " kết thúc lúc " + end.Format(time.RFC3339))
	}
	if in.CorrectionReason != nil {
		r := strings.TrimSpace(*in.CorrectionReason)
		in.CorrectionReason = &r
	}
	switch {
	case (in.SupersedesID == nil) != (in.CorrectionReason == nil):
		return store.Job{}, false, problem.Validation.WithField("correction_reason", "bảng kê bù cần cả supersedes_id và correction_reason")
	case in.SupersedesID != nil && in.ClientID == nil:
		return store.Job{}, false, problem.Validation.WithField("client_id", "bảng kê bù phải ghi khách hàng")
	case in.CorrectionReason != nil && (len([]rune(*in.CorrectionReason)) < 10 || len([]rune(*in.CorrectionReason)) > 500):
		return store.Job{}, false, problem.Validation.WithField("correction_reason", "10–500 ký tự")
	}
	j := store.Job{ID: ids.New(), Period: in.Period, ClientID: in.ClientID, SupersedesID: in.SupersedesID, CorrectionReason: in.CorrectionReason,
		RequestedBy: p.ActorRef, RequestedAt: now, IdemKey: in.IdemKey}
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.JobByIdem(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			j, replayed = *prev, true
			return nil
		}
		if in.SupersedesID != nil {
			old, err := store.CurrentOf(ctx, tx, *in.SupersedesID, false)
			if err != nil {
				return err
			}
			if old == nil || old.ClientID != *in.ClientID || old.Period != in.Period {
				return ErrNotFound.WithDetail("không có bảng kê cần thay của khách hàng và kỳ này")
			}
			if old.SupersededBy != nil {
				return port.ErrStatementFrozen.WithDetail("bảng kê đã được thay bởi " + old.SupersededBy.String())
			}
		}
		if err := store.InsertJob(ctx, tx, p.TenantID, &j); err != nil {
			return err
		}
		var client *string
		if in.ClientID != nil {
			c := in.ClientID.String()
			client = &c
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: p.TenantID, Type: port.EventStatementRequested, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: port.Producer,
			Payload: port.StatementRequestedV1{JobID: j.ID.String(), Period: j.Period, ClientID: client, RequestedBy: p.ActorRef.String(),
				RequestedAt: now.UTC().Format(time.RFC3339Nano)},
		})
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		var prev *store.Job
		if rerr := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.JobByIdem(ctx, tx, in.IdemKey)
			return e
		}); rerr == nil && prev != nil {
			return *prev, true, nil
		}
	}
	return j, replayed, err
}

// StatementJob đọc trạng thái yêu cầu lập bảng kê.
func (s *Service) StatementJob(ctx context.Context, id uuid.UUID) (store.Job, error) {
	p, err := authz.Require(ctx, statementReaders...)
	if err != nil {
		return store.Job{}, err
	}
	var j *store.Job
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		j, e = store.JobByID(ctx, tx, id, false)
		return e
	})
	if err != nil {
		return store.Job{}, err
	}
	if j == nil {
		return store.Job{}, ErrNotFound
	}
	return *j, nil
}

// HandleStatementRequested là inbox.Handler của consumer port.StatementConsumer (worker, pool
// metering_app): dựng ảnh chụp bất biến + bảng kê draft cho từng khách hàng của job, trong giao dịch
// của inbox.Process (kết quả + inbox commit cùng nhau, R-61). Job đã xong → không làm gì.
func (s *Service) HandleStatementRequested(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	var pl port.StatementRequestedV1
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		return fmt.Errorf("metering: payload %s hỏng: %w", env.EventType, err)
	}
	id, err := uuid.Parse(pl.JobID)
	if err != nil {
		return fmt.Errorf("metering: job_id không hợp lệ: %w", err)
	}
	j, err := store.JobByID(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if j == nil || j.Status != "queued" {
		return nil
	}
	op := env.TenantID
	clients := []uuid.UUID{}
	if j.ClientID != nil {
		clients = append(clients, *j.ClientID)
	} else if clients, err = store.ClientsWithUsage(ctx, tx, j.Period); err != nil {
		return err
	}
	var made []uuid.UUID
	for _, c := range clients {
		sid, err := s.draftStatement(ctx, tx, op, *j, c)
		if err != nil {
			return err
		}
		if sid != uuid.Nil {
			made = append(made, sid)
		}
	}
	return store.CompleteJob(ctx, tx, j.ID, made, nil, s.Clock.Now())
}

// draftStatement dựng bảng kê draft của một khách hàng; trả Nil khi không có sử dụng trong kỳ.
func (s *Service) draftStatement(ctx context.Context, tx pgx.Tx, op uuid.UUID, j store.Job, client uuid.UUID) (uuid.UUID, error) {
	if j.SupersedesID == nil {
		active, err := store.ActiveStatement(ctx, tx, client, j.Period)
		if err != nil {
			return uuid.Nil, err
		}
		if active != nil {
			return active.StatementID, nil // đã có bảng kê hiện hành: sửa = bảng kê bù, không lập trùng
		}
	} else {
		old, err := store.CurrentOf(ctx, tx, *j.SupersedesID, true)
		if err != nil {
			return uuid.Nil, err
		}
		if old == nil || old.SupersededBy != nil {
			return uuid.Nil, nil // đã được thay ở lần giao trước / bởi job khác
		}
	}
	stream := domain.UsageStream(client, j.Period)
	agg, err := store.AggregateStream(ctx, tx, stream, 0)
	if err != nil || agg == nil {
		return uuid.Nil, err
	}
	snap := snapshotOf(client, j.Period, stream, agg)
	body, sum, err := snap.Canonical()
	if err != nil {
		return uuid.Nil, err
	}
	_, end, _ := domain.ParsePeriod(j.Period)
	priced, err := s.price(ctx, op, agg.Lines, end.Add(-time.Microsecond))
	if err != nil {
		return uuid.Nil, err
	}
	sid := ids.New()
	now := s.Clock.Now()
	if err := store.InsertSnapshot(ctx, tx, op, store.SnapshotRow{StatementID: sid, JobID: j.ID, ClientID: client, Period: j.Period,
		StreamKey: stream, FirstSeq: agg.FirstSeq, LastSeq: agg.LastSeq, ChainHeadHash: snap.ChainHeadHash, Body: body, SnapshotSHA256: sum}); err != nil {
		return uuid.Nil, err
	}
	if err := store.InsertLines(ctx, tx, op, sid, priced.lines); err != nil {
		return uuid.Nil, err
	}
	ev := store.StatementEvent{ID: ids.New(), StatementID: sid, Action: "drafted", ClientID: client, ClientTenantID: agg.ClientTenantID,
		Period: j.Period, JobID: j.ID, SnapshotSHA256: sum, FirstSeq: agg.FirstSeq, LastSeq: agg.LastSeq, ChainHeadHash: snap.ChainHeadHash,
		RequestedBy: j.RequestedBy, Subscription: priced.subscription, Total: priced.total, PriceVersion: priced.version, EventTime: now,
		SupersedesID: j.SupersedesID, CorrectionReason: j.CorrectionReason, ActorRef: j.RequestedBy, RuleVersion: domain.RuleStatementV1}
	if err := store.InsertStatementEvent(ctx, tx, op, domain.StatementStream(client, j.Period), ev); err != nil {
		return uuid.Nil, ledger.Classify(err)
	}
	if err := store.InsertCurrent(ctx, tx, op, store.Current{StatementID: sid, ClientID: client, ClientTenantID: agg.ClientTenantID,
		Period: j.Period, JobID: j.ID, SnapshotSHA256: sum, FirstSeq: agg.FirstSeq, LastSeq: agg.LastSeq, ChainHeadHash: snap.ChainHeadHash,
		RuleVersion: domain.RuleStatementV1, Subscription: priced.subscription, Total: priced.total, PriceVersion: priced.version,
		RequestedBy: j.RequestedBy, RequestedAt: now, SupersedesID: j.SupersedesID, CorrectionReason: j.CorrectionReason}); err != nil {
		return uuid.Nil, err
	}
	if j.SupersedesID != nil {
		if err := store.MarkSuperseded(ctx, tx, *j.SupersedesID, sid); err != nil {
			return uuid.Nil, err
		}
	}
	return sid, nil
}

func snapshotOf(client uuid.UUID, period, stream string, agg *store.Aggregate) domain.Snapshot {
	snap := domain.Snapshot{RuleVersion: domain.RuleStatementV1, UnitsRules: agg.Rules, ClientID: client.String(),
		ClientTenantID: agg.ClientTenantID.String(), Period: period, StreamKey: stream, FirstSeq: agg.FirstSeq, LastSeq: agg.LastSeq,
		ChainHeadHash: store.HexHash(agg.LastHash)}
	for _, l := range agg.Lines {
		snap.Lines = append(snap.Lines, domain.SnapshotLine{Product: l.Product, StatusClass: l.StatusClass, Calls: l.Calls, Units: l.Units})
	}
	return snap
}

type pricedLines struct {
	lines        []store.Line
	subscription *string
	total        *string
	version      *string
}

// price áp tham số giá TÙY CHỌN (metering.price.default.<sản phẩm>.per_unit_vnd, …subscription_vnd_per_month)
// có hiệu lực tại thời điểm cuối kỳ. Chưa đặt giá → chỉ đơn vị (amount = null). Không float.
func (s *Service) price(ctx context.Context, op uuid.UUID, lines []store.AggLine, at time.Time) (pricedLines, error) {
	var out pricedLines
	var used []string
	total := decimal.Zero
	anyPrice := false
	prices := map[string]*money.Price{}
	versions := map[string]string{}
	for _, l := range lines {
		pl := store.Line{Product: l.Product, StatusClass: l.StatusClass, Calls: l.Calls, Units: l.Units}
		if l.Product != "" {
			pr, seen := prices[l.Product]
			if !seen {
				key := fmt.Sprintf(port.ParamPricePerUnitFmt, domain.PricePlanDefault, l.Product)
				v, ok, err := s.param(ctx, op, key, at)
				if err != nil {
					return out, err
				}
				if ok {
					p, err := money.ParsePrice(v.Value.String())
					if err != nil {
						return out, fmt.Errorf("metering: tham số giá %s v%d: %w", key, v.Version, err)
					}
					pr = &p
					versions[l.Product] = fmt.Sprintf("%s@v%d", key, v.Version)
					used = append(used, versions[l.Product])
				}
				prices[l.Product] = pr
			}
			if pr != nil {
				amt, err := money.ParseAmount(pr.Decimal().Mul(decimal.NewFromInt(l.Units)).Round(money.AmountScale).StringFixed(money.AmountScale))
				if err != nil {
					return out, err
				}
				ps, as, vs := pr.String(), amt.String(), versions[l.Product]
				pl.UnitPrice, pl.Amount, pl.PriceVersion = &ps, &as, &vs
				total = total.Add(amt.Decimal())
				anyPrice = true
			}
		}
		out.lines = append(out.lines, pl)
	}
	key := fmt.Sprintf(port.ParamPriceSubscriptionFmt, domain.PricePlanDefault)
	v, ok, err := s.param(ctx, op, key, at)
	if err != nil {
		return out, err
	}
	if ok {
		a, err := money.ParseAmount(v.Value.String())
		if err != nil {
			return out, fmt.Errorf("metering: tham số giá %s v%d: %w", key, v.Version, err)
		}
		str := a.String()
		out.subscription = &str
		total = total.Add(a.Decimal())
		anyPrice = true
		used = append(used, fmt.Sprintf("%s@v%d", key, v.Version))
	}
	if anyPrice {
		t, err := money.ParseAmount(total.StringFixed(money.AmountScale))
		if err != nil {
			return out, err
		}
		ts := t.String()
		out.total = &ts
		sort.Strings(used)
		vs := strings.Join(used, ";")
		out.version = &vs
	}
	return out, nil
}

// ApproveStatement (billing_approver ≠ người yêu cầu — R-56 ở đây VÀ CHECK DB) đóng băng bảng kê.
// Đã duyệt bởi chính người gọi → trả lại (replayed); bởi người khác hoặc đã bị thay → 409 statement_frozen.
func (s *Service) ApproveStatement(ctx context.Context, id uuid.UUID) (port.Statement, bool, error) {
	p, err := authz.Require(ctx, port.RoleBillingApprover)
	if err != nil {
		return port.Statement{}, false, err
	}
	replayed := false
	var st port.Statement
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.CurrentOf(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrNotFound
		}
		switch {
		case cur.Status == string(port.StatementApproved) && cur.ApprovedBy != nil && *cur.ApprovedBy == p.ActorRef:
			replayed = true
		case cur.Status == string(port.StatementApproved):
			return port.ErrStatementFrozen
		case cur.SupersededBy != nil:
			return port.ErrStatementFrozen.WithDetail("bảng kê đã được thay bởi " + cur.SupersededBy.String())
		case cur.RequestedBy == p.ActorRef:
			return port.ErrSelfApproval
		default:
			now := s.Clock.Now()
			if err := store.InsertStatementEvent(ctx, tx, p.TenantID, domain.StatementStream(cur.ClientID, cur.Period), store.StatementEvent{
				ID: ids.New(), StatementID: cur.StatementID, Action: "approved", ClientID: cur.ClientID, ClientTenantID: cur.ClientTenantID,
				Period: cur.Period, JobID: cur.JobID, SnapshotSHA256: cur.SnapshotSHA256, FirstSeq: cur.FirstSeq, LastSeq: cur.LastSeq,
				ChainHeadHash: cur.ChainHeadHash, RequestedBy: cur.RequestedBy, Subscription: cur.Subscription, Total: cur.Total,
				PriceVersion: cur.PriceVersion, EventTime: now, ActorRef: p.ActorRef, RuleVersion: domain.RuleStatementV1,
			}); err != nil {
				return ledger.Classify(err)
			}
			if err := store.MarkApproved(ctx, tx, cur.StatementID, p.ActorRef, now); err != nil {
				return err
			}
		}
		st, err = loadStatement(ctx, tx, id)
		return err
	})
	return st, replayed, err
}

// Statement đọc một bảng kê.
func (s *Service) Statement(ctx context.Context, id uuid.UUID) (port.Statement, error) {
	p, err := authz.Require(ctx, statementReaders...)
	if err != nil {
		return port.Statement{}, err
	}
	var st port.Statement
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		st, e = loadStatement(ctx, tx, id)
		return e
	})
	return st, err
}

// StatementView là bảng kê kèm thông tin bảng chiếu (cho Admin API).
type StatementView struct {
	port.Statement
	RequestedAt      time.Time
	ApprovedAt       *time.Time
	CorrectionReason *string
	SupersededBy     *uuid.UUID
	JobID            uuid.UUID
	PriceVersion     *string
}

// Statements liệt kê bảng kê của tenant đơn vị vận hành.
func (s *Service) Statements(ctx context.Context, period string, client, after uuid.UUID, limit int) (Page[StatementView], error) {
	p, err := authz.Require(ctx, statementReaders...)
	if err != nil {
		return Page[StatementView]{}, err
	}
	limit = clampLimit(limit)
	var out []StatementView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		cs, err := store.Statements(ctx, tx, store.StatementFilter{Period: period, ClientID: client, After: after, Limit: limit + 1})
		if err != nil {
			return err
		}
		for _, c := range cs {
			v, err := viewOf(ctx, tx, c)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return nil
	})
	return page(out, limit, func(v StatementView) uuid.UUID { return v.ID }), err
}

// StatementDetail đọc bảng kê kèm bảng chiếu.
func (s *Service) StatementDetail(ctx context.Context, id uuid.UUID) (StatementView, error) {
	p, err := authz.Require(ctx, statementReaders...)
	if err != nil {
		return StatementView{}, err
	}
	var v StatementView
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		c, err := store.CurrentOf(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrNotFound
		}
		v, err = viewOf(ctx, tx, *c)
		return err
	})
	return v, err
}

func viewOf(ctx context.Context, tx pgx.Tx, c store.Current) (StatementView, error) {
	st, err := statementFrom(ctx, tx, c)
	if err != nil {
		return StatementView{}, err
	}
	return StatementView{Statement: st, RequestedAt: c.RequestedAt, ApprovedAt: c.ApprovedAt, CorrectionReason: c.CorrectionReason,
		SupersededBy: c.SupersededBy, JobID: c.JobID, PriceVersion: c.PriceVersion}, nil
}

func loadStatement(ctx context.Context, tx pgx.Tx, id uuid.UUID) (port.Statement, error) {
	c, err := store.CurrentOf(ctx, tx, id, false)
	if err != nil {
		return port.Statement{}, err
	}
	if c == nil {
		return port.Statement{}, ErrNotFound
	}
	return statementFrom(ctx, tx, *c)
}

func parseAmount(s *string) (*money.Amount, error) {
	if s == nil {
		return nil, nil
	}
	a, err := money.ParseAmount(*s)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func statementFrom(ctx context.Context, tx pgx.Tx, c store.Current) (port.Statement, error) {
	lines, err := store.LinesOf(ctx, tx, c.StatementID)
	if err != nil {
		return port.Statement{}, err
	}
	st := port.Statement{ID: c.StatementID, ClientID: c.ClientID, ClientTenantID: c.ClientTenantID, Period: c.Period,
		Status: port.StatementStatus(c.Status), FirstSeq: c.FirstSeq, LastSeq: c.LastSeq, ChainHeadHash: c.ChainHeadHash,
		SnapshotSHA256: c.SnapshotSHA256, RuleVersion: c.RuleVersion, RequestedBy: c.RequestedBy}
	if c.ApprovedBy != nil {
		st.ApprovedBy = *c.ApprovedBy
	}
	if c.SupersedesID != nil {
		st.SupersedesID = *c.SupersedesID
	}
	if st.Subscription, err = parseAmount(c.Subscription); err != nil {
		return st, err
	}
	if st.Total, err = parseAmount(c.Total); err != nil {
		return st, err
	}
	for _, l := range lines {
		sl := port.StatementLine{Product: l.Product, StatusClass: l.StatusClass, Calls: l.Calls, Units: l.Units}
		if l.UnitPrice != nil {
			p, err := money.ParsePrice(*l.UnitPrice)
			if err != nil {
				return st, err
			}
			sl.UnitPrice = &p
		}
		if sl.Amount, err = parseAmount(l.Amount); err != nil {
			return st, err
		}
		if l.PriceVersion != nil {
			sl.PriceVersion = *l.PriceVersion
		}
		st.Lines = append(st.Lines, sl)
	}
	return st, nil
}

// Verification là kết quả dựng lại ảnh chụp của bảng kê từ sổ.
type Verification struct {
	Stored  string
	Rebuilt string
	Match   bool
}

// VerifyStatement dựng lại ảnh chụp từ sổ sử dụng (seq first..last của stream kỳ) và so SHA-256 —
// cùng sổ → cùng băm (tất định).
func (s *Service) VerifyStatement(ctx context.Context, id uuid.UUID) (Verification, error) {
	p, err := authz.Require(ctx, statementReaders...)
	if err != nil {
		return Verification{}, err
	}
	var v Verification
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		snap, err := store.SnapshotOf(ctx, tx, id)
		if err != nil {
			return err
		}
		if snap == nil {
			return ErrNotFound
		}
		agg, err := store.AggregateStream(ctx, tx, snap.StreamKey, snap.LastSeq)
		if err != nil {
			return err
		}
		if agg == nil {
			v = Verification{Stored: snap.SnapshotSHA256}
			return nil
		}
		_, sum, err := snapshotOf(snap.ClientID, snap.Period, snap.StreamKey, agg).Canonical()
		if err != nil {
			return err
		}
		v = Verification{Stored: snap.SnapshotSHA256, Rebuilt: sum, Match: sum == snap.SnapshotSHA256}
		return nil
	})
	return v, err
}

// StatementCSV trả CSV tất định của bảng kê (lập hóa đơn NGOÀI hệ thống). Lần tải đi qua điểm kiểm
// soát xuất kênh report_export (ADR-0023 mục 4.5; observe/enforce theo AG_EGRESS_MODE qua port.Guard).
func (s *Service) StatementCSV(ctx context.Context, id uuid.UUID) ([]byte, port.Statement, error) {
	p, err := authz.Require(ctx, port.RoleBillingOfficer, port.RoleBillingApprover)
	if err != nil {
		return nil, port.Statement{}, err
	}
	var st port.Statement
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		st, e = loadStatement(ctx, tx, id)
		return e
	}); err != nil {
		return nil, port.Statement{}, err
	}
	body, err := domain.StatementCSV(st)
	if err != nil {
		return nil, st, err
	}
	if err := port.Guard(ctx, s, s.mode(), port.Egress{SourceID: ids.New(), Channel: port.ChannelReportExport, DataTenantID: p.TenantID,
		CounterpartyKind: port.CounterpartyActor, CounterpartyRef: p.ActorRef, Product: "metering_statement", ContractVersion: 1,
		Records: int64(len(st.Lines)), Bytes: int64(len(body)), OccurredAt: s.Clock.Now(), ActorRef: p.ActorRef,
		TraceID: telemetry.TraceID(ctx)}, s.log()); err != nil {
		return nil, st, err
	}
	return body, st, nil
}
