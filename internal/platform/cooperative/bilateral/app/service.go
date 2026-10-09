// Package app là use case của sổ song phương MKG ↔ HTX (W5.6 — ADR-0034, kiến trúc v4.1 §38.2) trong pf/cooperative:
//
//   - Ledger cài đặt apiclient/port.BilateralLedger cho cổng tích hợp (bên a — MKG): ghi collection_schedule,
//     roll_count_statement đã qua hợp đồng vào cặp đang hiệu lực, đọc cặp/bản ghi cho feed của cặp và tổng kiểm ngày.
//     Mọi giao dịch ở tenant đối tác (app.tenant_id = counterparty, không app.node_id) — chỉ nhánh counterparty của
//     chính sách hai bên.
//   - Service là phía HTX (App HTX, Application API): người dùng vai coop_manager có đơn vị công tác (org_ref) là Nút
//     HTX lập / chấm dứt cặp với tenant đối tác, xem bản ghi của cặp và ghi xác nhận / phản đối số cuộn
//     (htx_roll_confirmation, author_side b). Giao dịch ở tenant của Principal, app.node_id = org_ref — cán bộ KN cấp
//     trên cùng tenant không thấy (so khớp Nút bằng, không node_closure).
//
// Chỉ ghi thêm (RB-1): chấm dứt, xác nhận, phản đối, sửa đều là bản ghi mới. Không tổng hợp (ADR-0034 §5).
package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/bilateral/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi phía HTX (docs/errors.md mục pf/cooperative — luồng song phương).
var (
	ErrNotHTX = problem.New(http.StatusForbidden, "cooperative.bilateral_not_htx",
		"Chỉ người quản lý HTX (đơn vị công tác là Nút HTX có hồ sơ) thao tác luồng song phương")
	ErrPairState        = problem.New(http.StatusConflict, "cooperative.bilateral_pair_state", "Cặp song phương đã ở trạng thái này")
	ErrStatementUnknown = problem.New(http.StatusUnprocessableEntity, "cooperative.bilateral_statement_unknown",
		"Không có bảng kê số cuộn (statement_ref, statement_revision) này trong cặp của HTX")
	ErrRollsMismatch = problem.New(http.StatusUnprocessableEntity, "cooperative.bilateral_rolls_mismatch",
		"mkg_rolls phải bằng số cuộn của revision bảng kê được phản hồi")
)

// Tên hợp đồng song phương (contracts/integration/ingest).
const (
	ContractSchedule     = "collection_schedule"
	ContractStatement    = "roll_count_statement"
	ContractConfirmation = "htx_roll_confirmation"
)

// MaxPageLimit là số bản ghi tối đa mỗi trang đọc.
const MaxPageLimit = 1000

// Ledger cài đặt apiclientport.BilateralLedger (bên a qua cổng tích hợp).
type Ledger struct {
	DB    tenant.Beginner // đã bọc vai r_cooperative
	Clock clock.Clock
}

var _ apiclientport.BilateralLedger = Ledger{}

func (l Ledger) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if l.DB == nil {
		return apiclientport.ErrBilateralUnavailable
	}
	return tenant.WithTenantTx(ctx, l.DB, tid, fn)
}

// prepared là bản ghi đã kiểm hợp đồng + chuẩn hóa.
type prepared struct {
	contract    *integration.Contract
	payload     []byte
	sha         string
	entityKey   string
	businessKey string
	htx         uuid.UUID
}

// prepare kiểm bản ghi theo hợp đồng song phương (mặc định từ chối) và phía tác giả side.
func prepare(contract string, version int, raw []byte, side string) (prepared, error) {
	c, err := integration.Get(contract, version)
	if err != nil || c.Kind != integration.KindIngest || c.Bilateral == nil {
		return prepared{}, apiclientport.ErrPairMissing.WithDetail("Hợp đồng " + contract + " không phải hợp đồng song phương")
	}
	if c.Bilateral.AuthorSide != side {
		return prepared{}, apiclientport.ErrBilateralWrongSide.WithDetail("Hợp đồng " + c.ID() + " do bên " + c.Bilateral.AuthorSide + " ghi")
	}
	if err := c.ValidateRecord(raw); err != nil {
		return prepared{}, apiclientport.ErrBilateralRecordInvalid.WithDetail(trim(err.Error()))
	}
	payload, err := integration.CanonicalJSON(raw)
	if err != nil {
		return prepared{}, apiclientport.ErrBilateralRecordInvalid
	}
	p := prepared{contract: c, payload: payload}
	if p.sha, err = integration.PayloadSHA256(payload); err != nil {
		return prepared{}, apiclientport.ErrBilateralRecordInvalid
	}
	if p.entityKey, err = c.EntityKeyOf(payload); err != nil {
		return prepared{}, apiclientport.ErrBilateralRecordInvalid.WithDetail(err.Error())
	}
	if p.businessKey, err = c.BusinessKeyOf(payload); err != nil {
		return prepared{}, apiclientport.ErrBilateralRecordInvalid.WithDetail(err.Error())
	}
	key, err := c.PairKeyOf(payload)
	if err != nil {
		return prepared{}, apiclientport.ErrBilateralRecordInvalid.WithDetail(err.Error())
	}
	if p.htx, err = uuid.Parse(key); err != nil {
		return prepared{}, apiclientport.ErrBilateralRecordInvalid.WithField("htx_ref", "phải là UUID")
	}
	return p, nil
}

func trim(s string) string {
	if r := []rune(s); len(r) > 300 {
		return string(r[:300])
	}
	return s
}

// AppendCounterparty cài đặt BilateralLedger.
func (l Ledger) AppendCounterparty(ctx context.Context, counterparty uuid.UUID, s apiclientport.BilateralSubmission) error {
	p, err := prepare(s.Contract, s.ContractVersion, s.Payload, apiclientport.SideA)
	if err != nil {
		return err
	}
	if s.HTXRef != uuid.Nil && s.HTXRef != p.htx {
		return apiclientport.ErrBilateralRecordInvalid.WithField("htx_ref", "khác khóa cặp đã tách ở cổng")
	}
	if s.Channel != apiclientport.ChannelAPI && s.Channel != apiclientport.ChannelFile {
		s.Channel = apiclientport.ChannelAPI
	}
	pairID := apiclientport.PairID(counterparty, p.htx)
	at := s.ReceivedAt
	if at.IsZero() {
		at = l.Clock.Now()
	}
	var pair *store.Pair
	err = l.tx(ctx, counterparty, func(tx pgx.Tx) error {
		var err error
		if pair, err = store.PairByID(ctx, tx, pairID); err != nil {
			return err
		}
		if pair == nil || !pair.Active() || pair.Counterparty != counterparty {
			return apiclientport.ErrPairMissing
		}
		idx := s.Index
		cid, bid := s.ClientID, s.BatchID
		return store.InsertRecord(ctx, tx, store.Record{ID: ids.New(), TenantID: pair.TenantID, PairID: pairID, HTXNodeID: pair.HTXNodeID,
			Counterparty: counterparty, Contract: p.contract.Name, ContractVersion: p.contract.Version, RecordRef: p.entityKey,
			BusinessKey: p.businessKey, Payload: p.payload, PayloadSHA256: p.sha, AuthorSide: apiclientport.SideA, Channel: s.Channel,
			ClientID: &cid, BatchID: &bid, ItemIndex: &idx, EventTime: at, ActorRef: s.ClientID})
	})
	if !errors.Is(err, ledger.ErrDuplicate) {
		return err
	}
	return l.sameContent(ctx, counterparty, pairID, p)
}

// sameContent: khóa nghiệp vụ đã ghi — cùng nội dung → nil (gọi lặp), khác → 409.
func (l Ledger) sameContent(ctx context.Context, tid, pairID uuid.UUID, p prepared) error {
	var prev string
	if err := l.tx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		prev, err = store.RecordHash(ctx, tx, pairID, p.contract.Name, p.contract.Version, p.businessKey)
		return err
	}); err != nil {
		return err
	}
	if prev != p.sha {
		return apiclientport.ErrBilateralKeyConflict
	}
	return nil
}

func toPair(p store.Pair) apiclientport.BilateralPair {
	out := apiclientport.BilateralPair{PairID: p.PairID, OwnerTenant: p.TenantID, HTXRef: p.HTXNodeID, Counterparty: p.Counterparty, Active: p.Active()}
	if out.Active {
		out.Since = p.LastAt
	} else {
		at := p.LastAt
		out.TerminatedAt = &at
	}
	return out
}

// CounterpartyPair cài đặt BilateralLedger.
func (l Ledger) CounterpartyPair(ctx context.Context, counterparty, htxRef uuid.UUID) (apiclientport.BilateralPair, error) {
	var pair *store.Pair
	err := l.tx(ctx, counterparty, func(tx pgx.Tx) error {
		var err error
		pair, err = store.PairByID(ctx, tx, apiclientport.PairID(counterparty, htxRef))
		return err
	})
	if err != nil {
		return apiclientport.BilateralPair{}, err
	}
	if pair == nil || pair.Counterparty != counterparty || pair.HTXNodeID != htxRef {
		return apiclientport.BilateralPair{}, apiclientport.ErrPairMissing
	}
	return toPair(*pair), nil
}

// CounterpartyPairs cài đặt BilateralLedger.
func (l Ledger) CounterpartyPairs(ctx context.Context, counterparty uuid.UUID) ([]apiclientport.BilateralPair, error) {
	var ps []store.Pair
	err := l.tx(ctx, counterparty, func(tx pgx.Tx) error {
		var err error
		ps, err = store.PairsByCounterparty(ctx, tx, counterparty)
		return err
	})
	out := make([]apiclientport.BilateralPair, 0, len(ps))
	for _, p := range ps {
		out = append(out, toPair(p))
	}
	return out, err
}

func toRecord(r store.Record) apiclientport.BilateralRecord {
	return apiclientport.BilateralRecord{ID: r.ID, PairID: r.PairID, HTXRef: r.HTXNodeID, Seq: r.Seq, Contract: r.Contract,
		ContractVersion: r.ContractVersion, EntityKey: r.RecordRef, BusinessKey: r.BusinessKey, AuthorSide: r.AuthorSide, Channel: r.Channel,
		Payload: json.RawMessage(r.Payload), PayloadSHA256: r.PayloadSHA256, RecordedAt: r.RecordedAt}
}

func toRecords(rs []store.Record) []apiclientport.BilateralRecord {
	out := make([]apiclientport.BilateralRecord, 0, len(rs))
	for _, r := range rs {
		out = append(out, toRecord(r))
	}
	return out
}

// PairRecords cài đặt BilateralLedger.
func (l Ledger) PairRecords(ctx context.Context, counterparty, pairID uuid.UUID, afterSeq int64, limit int) ([]apiclientport.BilateralRecord, error) {
	if limit <= 0 || limit > MaxPageLimit {
		limit = MaxPageLimit
	}
	var rs []store.Record
	err := l.tx(ctx, counterparty, func(tx pgx.Tx) error {
		var err error
		rs, err = store.RecordsAfter(ctx, tx, pairID, afterSeq, limit)
		return err
	})
	return toRecords(rs), err
}

// DayRecords cài đặt BilateralLedger.
func (l Ledger) DayRecords(ctx context.Context, counterparty uuid.UUID, contract string, pairID uuid.UUID, from, to time.Time) ([]apiclientport.BilateralRecord, error) {
	var rs []store.Record
	err := l.tx(ctx, counterparty, func(tx pgx.Tx) error {
		var err error
		rs, err = store.CounterpartyDayRecords(ctx, tx, counterparty, contract, pairID, from, to)
		return err
	})
	return toRecords(rs), err
}

// ---------------------------------------------------------------------------
// Phía HTX (App HTX)
// ---------------------------------------------------------------------------

// Service là use case phía HTX.
type Service struct {
	DB    tenant.Beginner // đã bọc vai r_cooperative
	Clock clock.Clock
	// Production: residency.Production — hợp đồng song phương chưa approved KHÔNG BAO GIỜ phục vụ (C-12).
	Production bool
	// AllowProposed: cờ dev/test (AG_BILATERAL_ALLOW_PROPOSED=1) cho hợp đồng proposed ngoài production.
	AllowProposed bool
}

// ErrNotServable: hợp đồng song phương chưa phục vụ ở môi trường này (proposed — bật production chờ C-12).
// Bí danh của định nghĩa duy nhất apiclient/port.ErrContractNotServable (cùng mã cho cổng và App HTX).
var ErrNotServable = apiclientport.ErrContractNotServable

// servable: hợp đồng htx_roll_confirmation v1 phục vụ được (approved không pending; proposed chỉ với cờ ngoài
// production).
func (s *Service) servable() error {
	c, err := integration.Get(ContractConfirmation, 1)
	if err != nil {
		return ErrNotServable
	}
	switch {
	case c.Servable(true):
		return nil
	case !s.Production && s.AllowProposed && c.Servable(false):
		return nil
	}
	return ErrNotServable.WithDetail("Hợp đồng " + c.ID() + " ở trạng thái " + c.Status + " (chờ " + strings.Join(c.Pending, ", ") + ")")
}

// htx kiểm người gọi là người quản lý HTX: vai coop_manager, org_ref là Nút HTX có hồ sơ ở tenant của Principal.
func (s *Service) htx(ctx context.Context) (authz.Principal, uuid.UUID, error) {
	p, err := authz.Require(ctx, partyport.RoleCoopManager)
	if err != nil {
		return p, uuid.Nil, err
	}
	if err := s.servable(); err != nil {
		return p, uuid.Nil, err
	}
	node := p.OrgRef
	if p.KN != nil {
		node = p.KN.OrgRef
	}
	if node == uuid.Nil || p.TenantID == uuid.Nil {
		return p, uuid.Nil, ErrNotHTX
	}
	var ok bool
	if err := s.tx(ctx, p, func(tx pgx.Tx) error {
		var err error
		ok, err = store.CooperativeExists(ctx, tx, node)
		return err
	}); err != nil {
		return p, uuid.Nil, err
	}
	if !ok {
		return p, uuid.Nil, ErrNotHTX
	}
	return p, node, nil
}

// tx mở giao dịch ở tenant của Principal (app.node_id do db.ForModule đặt từ Principal trong ctx).
func (s *Service) tx(ctx context.Context, p authz.Principal, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, p.TenantID, fn)
}

// Pair là cặp nhìn từ phía HTX.
type Pair = apiclientport.BilateralPair

// Pairs liệt kê cặp của HTX người gọi.
func (s *Service) Pairs(ctx context.Context) ([]Pair, error) {
	p, node, err := s.htx(ctx)
	if err != nil {
		return nil, err
	}
	var ps []store.Pair
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		var err error
		ps, err = store.PairsByHTX(ctx, tx, p.TenantID, node)
		return err
	})
	out := make([]Pair, 0, len(ps))
	for _, x := range ps {
		out = append(out, toPair(x))
	}
	return out, err
}

// pairEvent ghi một sự kiện vòng đời cặp sau khi kiểm trạng thái hiện tại; idem_key 'pair:<id>:<n>' chặn hai lần ghi
// đồng thời trên cùng trạng thái.
func (s *Service) pairEvent(ctx context.Context, p authz.Principal, node, counterparty uuid.UUID, action string, reason *string) (Pair, error) {
	pairID := apiclientport.PairID(counterparty, node)
	now := s.Clock.Now()
	var out store.Pair
	err := s.tx(ctx, p, func(tx pgx.Tx) error {
		cur, err := store.PairByID(ctx, tx, pairID)
		if err != nil {
			return err
		}
		var n int64
		if cur != nil {
			n = cur.Events
		}
		switch {
		case action == "established" && cur != nil && cur.Active():
			return ErrPairState.WithDetail("Cặp đang hiệu lực")
		case action == "terminated" && (cur == nil || !cur.Active()):
			return apiclientport.ErrPairMissing
		}
		e := store.PairEvent{ID: ids.New(), TenantID: p.TenantID, PairID: pairID, HTXNodeID: node, Counterparty: counterparty,
			AuthorSide: apiclientport.SideB, Action: action, Reason: reason, EventTime: now, ActorRef: p.ActorRef,
			IdemKey: "pair:" + pairID.String() + ":" + itoa64(n+1)}
		if err := store.InsertPairEvent(ctx, tx, e); err != nil {
			return err
		}
		out = store.Pair{PairID: pairID, TenantID: p.TenantID, HTXNodeID: node, Counterparty: counterparty, LastAction: action, LastAt: now, Events: n + 1}
		return nil
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return Pair{}, ErrPairState.WithDetail("Cặp vừa được thao tác đồng thời — tải lại")
	}
	return toPair(out), err
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// EstablishPair lập cặp song phương giữa HTX người gọi và tenant đối tác counterparty (chỉ bên HTX lập).
func (s *Service) EstablishPair(ctx context.Context, counterparty uuid.UUID) (Pair, error) {
	p, node, err := s.htx(ctx)
	if err != nil {
		return Pair{}, err
	}
	if counterparty == uuid.Nil || counterparty == p.TenantID {
		return Pair{}, problem.Validation.WithField("counterparty_tenant_id", "tenant đối tác khác tenant của HTX")
	}
	return s.pairEvent(ctx, p, node, counterparty, "established", nil)
}

// TerminatePair chấm dứt cặp pairID của HTX người gọi (lý do 10–500 ký tự). Bản ghi cũ giữ nguyên (chỉ ghi thêm);
// bên đối tác bị từ chối ghi và đọc feed cặp từ lời gọi kế tiếp.
func (s *Service) TerminatePair(ctx context.Context, pairID uuid.UUID, reason string) (Pair, error) {
	p, node, err := s.htx(ctx)
	if err != nil {
		return Pair{}, err
	}
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return Pair{}, problem.Validation.WithField("reason", "10–500 ký tự")
	}
	var cur *store.Pair
	if err := s.tx(ctx, p, func(tx pgx.Tx) error {
		var err error
		cur, err = store.PairByID(ctx, tx, pairID)
		return err
	}); err != nil {
		return Pair{}, err
	}
	if cur == nil || cur.HTXNodeID != node || cur.TenantID != p.TenantID {
		return Pair{}, apiclientport.ErrPairMissing
	}
	return s.pairEvent(ctx, p, node, cur.Counterparty, "terminated", &reason)
}

// Record là bản ghi nhìn từ phía HTX.
type Record = apiclientport.BilateralRecord

// Records trả tối đa limit bản ghi của cặp pairID (của HTX người gọi) có seq > after.
func (s *Service) Records(ctx context.Context, pairID uuid.UUID, after int64, limit int) ([]Record, error) {
	p, node, err := s.htx(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaxPageLimit {
		limit = 100
	}
	var rs []store.Record
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		cur, err := store.PairByID(ctx, tx, pairID)
		if err != nil {
			return err
		}
		if cur == nil || cur.HTXNodeID != node {
			return apiclientport.ErrPairMissing
		}
		rs, err = store.RecordsAfter(ctx, tx, pairID, after, limit)
		return err
	})
	return toRecords(rs), err
}

// statementView là các trường của roll_count_statement mà xác nhận đối chiếu.
type statementView struct {
	Rolls int64 `json:"rolls"`
}

type confirmationView struct {
	StatementRef      string  `json:"statement_ref"`
	StatementRevision int64   `json:"statement_revision"`
	MKGRolls          int64   `json:"mkg_rolls"`
	Outcome           string  `json:"outcome"`
	HTXRolls          *int64  `json:"htx_rolls"`
	Reason            *string `json:"reason"`
}

// Confirm ghi xác nhận / phản đối số cuộn của HTX (htx_roll_confirmation v1, kênh app): bản ghi đúng hợp đồng,
// htx_ref = Nút của người gọi, phản hồi đúng một revision bảng kê đã có trong cặp (mkg_rolls khớp), phản đối phải có
// htx_rolls + lý do; cặp phải đang hiệu lực. Idempotent theo confirmation_ref (cùng nội dung → trả bản ghi cũ).
func (s *Service) Confirm(ctx context.Context, raw []byte) (Record, bool, error) {
	p, node, err := s.htx(ctx)
	if err != nil {
		return Record{}, false, err
	}
	pr, err := prepare(ContractConfirmation, 1, raw, apiclientport.SideB)
	if err != nil {
		return Record{}, false, err
	}
	if pr.htx != node {
		return Record{}, false, apiclientport.ErrBilateralRecordInvalid.WithField("htx_ref", "phải là HTX của người gọi")
	}
	var cv confirmationView
	if err := json.Unmarshal(pr.payload, &cv); err != nil {
		return Record{}, false, apiclientport.ErrBilateralRecordInvalid
	}
	if cv.Outcome == "disputed" && (cv.HTXRolls == nil || cv.Reason == nil || strings.TrimSpace(*cv.Reason) == "") {
		return Record{}, false, problem.Validation.WithField("htx_rolls", "phản đối phải có htx_rolls và reason")
	}
	now := s.Clock.Now()
	var (
		out     store.Record
		created bool
	)
	err = s.tx(ctx, p, func(tx pgx.Tx) error {
		st, err := store.RecordByKey(ctx, tx, ContractStatement, cv.StatementRef+integration.EntityKeySep+itoa64(cv.StatementRevision))
		if err != nil {
			return err
		}
		if st == nil || st.HTXNodeID != node {
			return ErrStatementUnknown
		}
		var sv statementView
		if err := json.Unmarshal(st.Payload, &sv); err != nil {
			return err
		}
		if sv.Rolls != cv.MKGRolls {
			return ErrRollsMismatch
		}
		pair, err := store.PairByID(ctx, tx, st.PairID)
		if err != nil {
			return err
		}
		if pair == nil || !pair.Active() {
			return apiclientport.ErrPairMissing
		}
		out = store.Record{ID: ids.New(), TenantID: p.TenantID, PairID: pair.PairID, HTXNodeID: node, Counterparty: pair.Counterparty,
			Contract: pr.contract.Name, ContractVersion: pr.contract.Version, RecordRef: pr.entityKey, BusinessKey: pr.businessKey,
			Payload: pr.payload, PayloadSHA256: pr.sha, AuthorSide: apiclientport.SideB, Channel: apiclientport.ChannelApp, EventTime: now,
			ActorRef: p.ActorRef}
		created = true
		return store.InsertRecord(ctx, tx, out)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		created = false
		err = s.tx(ctx, p, func(tx pgx.Tx) error {
			prev, err := store.RecordByKey(ctx, tx, ContractConfirmation, pr.businessKey)
			if err != nil {
				return err
			}
			if prev == nil || prev.PayloadSHA256 != pr.sha {
				return apiclientport.ErrBilateralKeyConflict
			}
			out = *prev
			return nil
		})
	}
	if err != nil {
		return Record{}, false, err
	}
	if created {
		// recorded_at do sổ đặt (now() của giao dịch): đọc lại để trả đúng giá trị tổng kiểm dùng.
		if err := s.tx(ctx, p, func(tx pgx.Tx) error {
			prev, err := store.RecordByKey(ctx, tx, ContractConfirmation, pr.businessKey)
			if err == nil && prev != nil {
				out = *prev
			}
			return err
		}); err != nil {
			return Record{}, false, err
		}
	}
	return toRecord(out), created, nil
}
