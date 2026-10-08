package app

// Phía HTX của thu gom rơm MKG (G3-2, g3-conventions §4, §5; RB-6 chiều mua):
//
//   - consumer van công bố collection_schedule / htx_collection_statement: kiểm payload
//     bằng bộ kiểm hợp đồng lúc chạy (contracts/publication — mặc định từ chối), ghi sổ
//     nhận idempotent theo (hợp đồng, ref, revision), bản revision lớn nhất là hiện hành;
//     phát pf.cropcycle.collection_*.received để pf/notify báo quản lý HTX;
//   - quản lý HTX (coop_manager) của đúng HTX xác nhận / tranh chấp bảng kê theo revision
//     hiện hành (409 nếu đã cũ), sổ chỉ ghi thêm, bản sau cùng revision thay bản trước;
//   - tài khoản dịch vụ MKG (vai mkg_sync, tenant MKG) kéo phản hồi theo con trỏ feed_seq qua
//     cấp quyền chia sẻ htx_collection_responses (ADR-0020): đọc ở tenant BÊN CẤP, chỉ phản hồi
//     thuộc bảng kê do chính tenant người gọi gửi qua van, chỉ các trường MKG cần để ghi
//     mkg_fieldops.htx_confirmation (không có người phản hồi, không có dữ liệu khác), ghi sổ đọc.
//
// ADR-0020 (chủ sản phẩm chốt 29–30/09/2026): MKG là tenant riêng. Consumer van ghi sổ nhận,
// bảng chiếu và sự kiện received vào tenant BÊN CẤP (tenant chương trình của HTX) đã cấp
// valve_collection cho tenant phong bì — source_tenant_id giữ tenant MKG. Không có cấp quyền →
// lỗi party.valve_share_grant_missing → giao lại → DLQ.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi thu gom (docs/errors.md, mục pf/cropcycle).
var (
	ErrStatementNotFound = problem.New(http.StatusNotFound, "cropcycle.collection_statement_not_found", "Không tìm thấy bảng kê thu gom")
	ErrStatementStale    = problem.New(http.StatusConflict, "cropcycle.collection_statement_stale",
		"MKG đã gửi bản sửa của bảng kê này — tải lại và phản hồi theo bản mới nhất")
	ErrCollectionRole = problem.New(http.StatusForbidden, "cropcycle.collection_manager_only",
		"Chỉ quản lý HTX của đúng HTX được xác nhận hoặc tranh chấp bảng kê thu gom")
)

// valveActor là người ghi của sổ nhận van (hệ thống — không có người dùng).
var valveActor = uuid.MustParse("00000000-0000-7000-8000-00000000cc01")

// errValve đánh dấu payload van không hợp lệ: không tự lành → giao lại rồi DLQ (R-62).
var errValve = errors.New("cropcycle: payload van công bố không hợp lệ")

// collectionScheduleV1 là payload hợp đồng collection_schedule v1.
type collectionScheduleV1 struct {
	ScheduleRef uuid.UUID `json:"schedule_ref"`
	HTXRef      uuid.UUID `json:"htx_ref"`
	CommuneCode string    `json:"commune_code"`
	WindowFrom  string    `json:"window_from"`
	WindowTo    string    `json:"window_to"`
	Status      string    `json:"status"`
	Revision    int       `json:"revision"`
}

// htxCollectionStatementV1 là payload hợp đồng htx_collection_statement v1.
type htxCollectionStatementV1 struct {
	StatementRef uuid.UUID  `json:"statement_ref"`
	HTXRef       uuid.UUID  `json:"htx_ref"`
	PlotRef      uuid.UUID  `json:"plot_ref"`
	SeasonRef    *uuid.UUID `json:"season_ref"`
	CollectedOn  string     `json:"collected_on"`
	Rolls        int        `json:"rolls"`
	Revision     int        `json:"revision"`
}

// HandleCollectionValve là inbox.Handler của consumer port.CollectionValveConsumer. Chạy
// trong giao dịch inbox (vai cropcycle_app, tenant của phong bì). Idempotent hai lớp:
// shared.inbox theo event_id và sổ nhận theo (hợp đồng, ref, revision).
func (s *Service) HandleCollectionValve(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	var name string
	switch env.EventType {
	case port.EventMKGCollectionSchedulePublished:
		name = port.ContractCollectionSchedule
	case port.EventMKGHTXCollectionStatementPublished:
		name = port.ContractHTXCollectionStatement
	default:
		return nil
	}
	c, err := publication.Get(name)
	if err != nil {
		return err
	}
	if err := c.Validate(env.Payload); err != nil {
		return fmt.Errorf("%w (%s): %w", errValve, env.EventType, err)
	}
	grantors, err := partyport.ValveTargets(ctx, s.ValveGrants, env.TenantID, partyport.KindValveCollection, s.now())
	if err != nil {
		return err
	}
	// ADR-0024 mục 3: điểm kiểm soát xuất kênh valve ở phía nhận, mỗi bên cấp một lần, TRƯỚC khi ghi
	// vào tenant bên cấp. Tenant dữ liệu = tenant phong bì (MKG); enforce + từ chối → giao lại → DLQ.
	for _, g := range grantors {
		if err := meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
			SourceID: env.EventID, Channel: meter.ChannelValve, DataTenantID: env.TenantID,
			CounterpartyKind: meter.CounterpartyTenant, CounterpartyRef: g.Tenant(), Product: name, ContractVersion: env.SchemaVersion,
			Records: 1, Bytes: int64(len(env.Payload)), OccurredAt: env.OccurredAt, TraceID: env.TraceID,
		}, s.Log); err != nil {
			return err
		}
	}
	source := env.TenantID
	for _, g := range grantors {
		genv := env
		genv.TenantID = g.Tenant() // ghi ở tenant bên cấp; nguồn giữ ở source_tenant_id
		if err := tenant.Within(ctx, tx, g, func() error {
			if name == port.ContractCollectionSchedule {
				return s.receiveSchedule(ctx, tx, genv, source)
			}
			return s.receiveStatement(ctx, tx, genv, source)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) receipt(env outbox.Envelope, source uuid.UUID, contract string, ref, coop uuid.UUID, revision int, current bool) store.ValveReceipt {
	return store.ValveReceipt{ID: ids.New(), TenantID: env.TenantID, Contract: contract, SubjectRef: ref, Revision: revision,
		CooperativeRef: coop, SourceEventID: env.EventID, SourceTenantID: source, Payload: env.Payload, BecameCurrent: current,
		EventTime: env.OccurredAt, ActorRef: valveActor, IdemKey: store.ReceiptIdemKey(contract, ref, revision)}
}

func (s *Service) receiveSchedule(ctx context.Context, tx pgx.Tx, env outbox.Envelope, source uuid.UUID) error {
	var p collectionScheduleV1
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("%w: %w", errValve, err)
	}
	from, err1 := domain.ParseDay(p.WindowFrom)
	to, err2 := domain.ParseDay(p.WindowTo)
	if err1 != nil || err2 != nil || to.Before(from) || p.Revision < 0 || p.ScheduleRef == uuid.Nil || p.HTXRef == uuid.Nil {
		return fmt.Errorf("%w: window_from/window_to/revision/ref", errValve)
	}
	key := store.ReceiptIdemKey(port.ContractCollectionSchedule, p.ScheduleRef, p.Revision)
	if dup, err := store.ReceiptExists(ctx, tx, key); err != nil || dup {
		return err
	}
	cur, err := store.ScheduleByRef(ctx, tx, p.ScheduleRef, true)
	if err != nil {
		return err
	}
	current := cur == nil || p.Revision > cur.Revision
	r := s.receipt(env, source, port.ContractCollectionSchedule, p.ScheduleRef, p.HTXRef, p.Revision, current)
	if err := ledger.Classify(store.InsertReceipt(ctx, tx, &r)); err != nil {
		return err
	}
	if !current {
		return nil // bản cũ đến muộn: chỉ lưu sổ nhận
	}
	v := store.ScheduleView{ScheduleRef: p.ScheduleRef, CooperativeRef: p.HTXRef, CommuneCode: p.CommuneCode, WindowFrom: from,
		WindowTo: to, Status: p.Status, Revision: p.Revision, ReceiptID: r.ID, SourceTenantID: source,
		PublishedAt: env.OccurredAt, ReceivedAt: r.RecordedAt}
	if err := store.PutSchedule(ctx, tx, env.TenantID, v); err != nil {
		return err
	}
	return enqueue(ctx, tx, env.TenantID, port.EventCollectionScheduleReceived, env.OccurredAt, port.CollectionScheduleReceivedV1{
		ScheduleRef: p.ScheduleRef, CooperativeRef: p.HTXRef, CommuneCode: p.CommuneCode, WindowFrom: p.WindowFrom,
		WindowTo: p.WindowTo, Status: p.Status, Revision: p.Revision, First: cur == nil,
	})
}

func (s *Service) receiveStatement(ctx context.Context, tx pgx.Tx, env outbox.Envelope, source uuid.UUID) error {
	var p htxCollectionStatementV1
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("%w: %w", errValve, err)
	}
	on, err := domain.ParseDay(p.CollectedOn)
	if err != nil || p.Rolls < 0 || p.Revision < 1 || p.StatementRef == uuid.Nil || p.HTXRef == uuid.Nil || p.PlotRef == uuid.Nil {
		return fmt.Errorf("%w: collected_on/rolls/revision/ref", errValve)
	}
	key := store.ReceiptIdemKey(port.ContractHTXCollectionStatement, p.StatementRef, p.Revision)
	if dup, err := store.ReceiptExists(ctx, tx, key); err != nil || dup {
		return err
	}
	cur, err := store.StatementByRef(ctx, tx, p.StatementRef, true)
	if err != nil {
		return err
	}
	current := cur == nil || p.Revision > cur.Revision
	r := s.receipt(env, source, port.ContractHTXCollectionStatement, p.StatementRef, p.HTXRef, p.Revision, current)
	if err := ledger.Classify(store.InsertReceipt(ctx, tx, &r)); err != nil {
		return err
	}
	if !current {
		return nil
	}
	v := store.StatementView{StatementRef: p.StatementRef, CooperativeRef: p.HTXRef, PlotRef: p.PlotRef, SeasonRef: p.SeasonRef,
		CollectedOn: on, Rolls: p.Rolls, Revision: p.Revision, ReceiptID: r.ID, SourceTenantID: source,
		PublishedAt: env.OccurredAt, ReceivedAt: r.RecordedAt}
	if err := store.PutStatement(ctx, tx, env.TenantID, v); err != nil {
		return err
	}
	return enqueue(ctx, tx, env.TenantID, port.EventCollectionStatementReceived, env.OccurredAt, port.CollectionStatementReceivedV1{
		StatementRef: p.StatementRef, CooperativeRef: p.HTXRef, PlotRef: p.PlotRef, CollectedOn: p.CollectedOn, Rolls: p.Rolls,
		Revision: p.Revision, First: cur == nil,
	})
}

// ---------------------------------------------------------------------------
// Đọc (Application API)
// ---------------------------------------------------------------------------

// collectionFilter: quản lý HTX = HTX của mình; quản trị = toàn tenant (hoặc một HTX);
// cán bộ KN = xã trong phạm vi (chỉ lịch gom, units = true).
func (a access) collectionFilter(coop uuid.UUID, units bool) store.CollectionFilter {
	switch a.kind {
	case accAll:
		if coop != uuid.Nil {
			return store.CollectionFilter{Coop: coop}
		}
		return store.CollectionFilter{All: true}
	case accCoop:
		if a.p.OrgRef == uuid.Nil || (coop != uuid.Nil && coop != a.p.OrgRef) {
			return store.CollectionFilter{Units: []string{}}
		}
		return store.CollectionFilter{Coop: a.p.OrgRef}
	case accKN:
		if !units {
			return store.CollectionFilter{Units: []string{}}
		}
		if a.sc.All {
			if coop != uuid.Nil {
				return store.CollectionFilter{Coop: coop}
			}
			return store.CollectionFilter{All: true}
		}
		return store.CollectionFilter{Units: a.sc.Units}
	}
	return store.CollectionFilter{Units: []string{}}
}

// CollectionListInput là bộ lọc danh sách lịch gom / bảng kê.
type CollectionListInput struct {
	CooperativeRef uuid.UUID // quản trị / KN toàn quốc lọc theo HTX; quản lý HTX luôn là HTX của mình
	Status         string
	After          uuid.UUID
	Limit          int
}

var scheduleStatuses = map[string]bool{"": true, "planned": true, "confirmed": true, "in_progress": true, "completed": true, "cancelled": true}

// ListCollectionSchedules liệt kê lịch gom MKG (quản lý HTX, quản trị, cán bộ KN theo xã).
func (s *Service) ListCollectionSchedules(ctx context.Context, in CollectionListInput) (Page[store.ScheduleView], error) {
	a, err := s.access(ctx, append([]string{port.RoleCoopManager, authz.RolePlatformAdmin}, knRoles...)...)
	if err != nil {
		return Page[store.ScheduleView]{}, err
	}
	if !scheduleStatuses[in.Status] {
		return Page[store.ScheduleView]{}, problem.Validation.WithField("status", "planned | confirmed | in_progress | completed | cancelled")
	}
	limit := clampLimit(in.Limit)
	f := a.collectionFilter(in.CooperativeRef, true)
	var items []store.ScheduleView
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListSchedules(ctx, tx, f, in.Status, in.After, limit+1)
		return e
	})
	if err != nil {
		return Page[store.ScheduleView]{}, err
	}
	pg := Page[store.ScheduleView]{Items: items}
	if len(items) > limit {
		pg.Items = items[:limit]
		last := items[limit-1].ScheduleRef
		pg.Next = &last
	}
	return pg, nil
}

// StatementItem là bảng kê kèm mã thửa (nếu tra được qua pf/geo).
type StatementItem struct {
	store.StatementView
	PlotCode string
}

// ListCollectionStatements liệt kê bảng kê số cuộn (quản lý HTX của mình, quản trị).
// status: pending | confirmed | disputed | "".
func (s *Service) ListCollectionStatements(ctx context.Context, in CollectionListInput) (Page[StatementItem], error) {
	a, err := s.access(ctx, port.RoleCoopManager, authz.RolePlatformAdmin)
	if err != nil {
		return Page[StatementItem]{}, err
	}
	switch in.Status {
	case "", store.StatementPending, store.StatementConfirmed, store.StatementDisputed:
	default:
		return Page[StatementItem]{}, problem.Validation.WithField("status", "pending | confirmed | disputed")
	}
	limit := clampLimit(in.Limit)
	f := a.collectionFilter(in.CooperativeRef, false)
	var rows []store.StatementView
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ListStatements(ctx, tx, f, in.Status, in.After, limit+1)
		return e
	})
	if err != nil {
		return Page[StatementItem]{}, err
	}
	pg := Page[StatementItem]{}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1].StatementRef
		pg.Next = &last
	}
	pg.Items = s.withPlotCodes(ctx, rows)
	return pg, nil
}

// withPlotCodes gắn mã thửa (best effort — thiếu pf/geo hoặc thửa đã đổi thì để trống).
func (s *Service) withPlotCodes(ctx context.Context, rows []store.StatementView) []StatementItem {
	out := make([]StatementItem, 0, len(rows))
	codes := map[uuid.UUID]string{}
	for _, r := range rows {
		code, ok := codes[r.PlotRef]
		if !ok && s.Plots != nil {
			if pl, err := s.Plots.Get(ctx, r.PlotRef); err == nil {
				code = pl.Code
			}
			codes[r.PlotRef] = code
		}
		out = append(out, StatementItem{StatementView: r, PlotCode: code})
	}
	return out
}

// StatementDetail là bảng kê kèm lịch sử phản hồi.
type StatementDetail struct {
	Statement StatementItem
	Responses []store.Response
}

// GetCollectionStatement đọc một bảng kê và các phản hồi (quản lý HTX của mình, quản trị).
func (s *Service) GetCollectionStatement(ctx context.Context, ref uuid.UUID) (StatementDetail, error) {
	a, err := s.access(ctx, port.RoleCoopManager, authz.RolePlatformAdmin)
	if err != nil {
		return StatementDetail{}, err
	}
	var d StatementDetail
	var st *store.StatementView
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if st, e = store.StatementByRef(ctx, tx, ref, false); e != nil || st == nil {
			return e
		}
		d.Responses, e = store.ResponsesOfStatement(ctx, tx, ref)
		return e
	})
	if err != nil {
		return d, err
	}
	if st == nil || !a.sees("", st.CooperativeRef) {
		return d, ErrStatementNotFound
	}
	d.Statement = s.withPlotCodes(ctx, []store.StatementView{*st})[0]
	return d, nil
}

// ---------------------------------------------------------------------------
// Phản hồi của HTX
// ---------------------------------------------------------------------------

// RespondInput là xác nhận / tranh chấp một bảng kê.
type RespondInput struct {
	StatementRef uuid.UUID
	Revision     int // revision của bảng kê người dùng đã xem (bắt buộc)
	Outcome      string
	HTXRolls     *int
	Reason       string
	ID           *uuid.UUID // id bản ghi đồng bộ (UUIDv7 máy sinh); nil = hệ sinh
	EventTime    *time.Time // thời điểm người dùng bấm (đồng bộ ngoại tuyến); nil = bây giờ
	DeviceID     *string
	IdemKey      string
}

// RespondResult là kết quả ghi phản hồi.
type RespondResult struct {
	Response  store.Response
	Statement store.StatementView
	Replayed  bool
}

// RespondToStatement ghi phản hồi của quản lý HTX với bảng kê của HTX mình. Revision đã
// cũ → 409 cropcycle.collection_statement_stale (MKG đã gửi bản sửa).
func (s *Service) RespondToStatement(ctx context.Context, in RespondInput) (RespondResult, error) {
	p, err := authz.Require(ctx, port.RoleCoopManager)
	if err != nil {
		return RespondResult{}, ErrCollectionRole
	}
	if p.OrgRef == uuid.Nil {
		return RespondResult{}, ErrCollectionRole
	}
	if in.IdemKey == "" {
		return RespondResult{}, problem.IdempotencyRequired
	}
	if in.Revision < 1 {
		return RespondResult{}, problem.Validation.WithField("revision", "bắt buộc — phiên bản bảng kê đã xem (≥ 1)")
	}
	now := s.now()
	at := now
	if in.EventTime != nil {
		if in.EventTime.After(now.Add(5 * time.Minute)) {
			return RespondResult{}, problem.Validation.WithField("event_time", "không được ở tương lai")
		}
		at = in.EventTime.UTC()
	}
	replay := func(tx pgx.Tx) (*RespondResult, error) {
		r, err := store.ResponseByIdem(ctx, tx, in.IdemKey)
		if err != nil || r == nil {
			return nil, err
		}
		st, err := store.StatementByRef(ctx, tx, r.StatementRef, false)
		if err != nil || st == nil {
			return nil, err
		}
		return &RespondResult{Response: *r, Statement: *st, Replayed: true}, nil
	}
	var res RespondResult
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := replay(tx); err != nil || prev != nil {
			if prev != nil {
				res = *prev
			}
			return err
		}
		st, err := store.StatementByRef(ctx, tx, in.StatementRef, true)
		if err != nil {
			return err
		}
		if st == nil || st.CooperativeRef != p.OrgRef {
			return ErrStatementNotFound
		}
		if in.Revision != st.Revision {
			return ErrStatementStale.With("current_revision", st.Revision)
		}
		superseding := st.ResponseRevision != nil && *st.ResponseRevision == st.Revision
		chk, err := domain.CheckResponse(in.Outcome, st.Rolls, in.HTXRolls, in.Reason, superseding)
		if err != nil {
			return fieldProblem(err)
		}
		id := ids.New()
		if in.ID != nil {
			id = *in.ID
		}
		r := store.Response{ID: id, TenantID: p.TenantID, StatementRef: st.StatementRef, StatementRevision: st.Revision,
			CooperativeRef: st.CooperativeRef, PlotRef: st.PlotRef, MKGRolls: st.Rolls, Outcome: chk.Outcome, HTXRolls: chk.HTXRolls,
			Reason: strPtr(chk.Reason), SourceTenantID: st.SourceTenantID, EventTime: at, ActorRef: p.ActorRef, DeviceID: in.DeviceID,
			IdemKey: in.IdemKey}
		if superseding {
			r.SupersedesID, r.CorrectionReason = st.ResponseID, strPtr(chk.Reason)
		}
		if err := store.InsertResponse(ctx, tx, &r); err != nil {
			return err
		}
		if err := store.SetStatementResponse(ctx, tx, r); err != nil {
			return err
		}
		cur, err := store.StatementByRef(ctx, tx, st.StatementRef, false)
		if err != nil {
			return err
		}
		res = RespondResult{Response: r, Statement: *cur}
		return nil
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *RespondResult
		if rerr := s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = replay(tx)
			return e
		}); rerr == nil && prev != nil {
			return *prev, nil
		}
		return RespondResult{}, ErrIDReused
	}
	if isUnique(err, "") {
		return RespondResult{}, ErrIDReused
	}
	return res, err
}

// ---------------------------------------------------------------------------
// Luồng phản hồi cho MKG (tài khoản dịch vụ vai mkg_sync)
// ---------------------------------------------------------------------------

// ResponseFeedPage là một trang luồng phản hồi; Next = feed_seq cuối (con trỏ lần sau).
type ResponseFeedPage struct {
	Items []store.Response
	Next  int64
}

// partnerGrant kiểm cấp quyền chia sẻ loại kind cho tenant người gọi (ADR-0020). Chưa nối
// pf/party → 403 (mặc định từ chối).
func (s *Service) partnerGrant(ctx context.Context, kind string, grantor uuid.UUID) (partyport.ShareGrant, error) {
	if s.Sharing == nil {
		if _, err := authz.Require(ctx, port.RoleMKGSync); err != nil {
			return partyport.ShareGrant{}, err
		}
		return partyport.ShareGrant{}, partyport.ErrShareGrantMissing
	}
	return s.Sharing.Authorize(ctx, kind, grantor)
}

// CollectionResponseFeed trả phản hồi của HTX thuộc bảng kê do tenant của người gọi (bên nhận)
// gửi qua van, feed_seq > after — đọc ở tenant BÊN CẤP của cấp quyền htx_collection_responses
// (ADR-0020; RB-2: phạm vi = phản hồi MKG cần). Chỉ vai mkg_sync; mỗi lời gọi một dòng sổ đọc.
func (s *Service) CollectionResponseFeed(ctx context.Context, grantor uuid.UUID, after int64, limit int, endpoint, query string) (ResponseFeedPage, error) {
	g, err := s.partnerGrant(ctx, partyport.KindHTXCollectionResponses, grantor)
	if err != nil {
		return ResponseFeedPage{}, err
	}
	if after < 0 {
		return ResponseFeedPage{}, problem.Validation.WithField("after", "≥ 0")
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	pg := ResponseFeedPage{Next: after}
	err = s.inTx(ctx, g.Grantor, func(tx pgx.Tx) error {
		var e error
		pg.Items, e = store.ResponseFeed(ctx, tx, g.Grantee, after, limit)
		return e
	})
	if err != nil {
		return ResponseFeedPage{}, err
	}
	if n := len(pg.Items); n > 0 {
		pg.Next = pg.Items[n-1].FeedSeq
	}
	if err := s.Sharing.RecordRead(ctx, g, partyport.PartnerRead{Kind: partyport.KindHTXCollectionResponses, Endpoint: endpoint,
		Query: query, Items: len(pg.Items)}); err != nil {
		return ResponseFeedPage{}, err
	}
	return pg, nil
}
