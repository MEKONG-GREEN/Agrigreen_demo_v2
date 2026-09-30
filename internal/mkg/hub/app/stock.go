package app

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func (s *Service) operator(ctx context.Context, idem string, eventTime *time.Time) (authz.Principal, error) {
	p, err := authz.Require(ctx, RoleHubOperator)
	if err != nil {
		return p, err
	}
	if err := requireIdem(idem); err != nil {
		return p, err
	}
	if eventTime.IsZero() {
		*eventTime = s.Clock.Now()
	}
	*eventTime = eventTime.UTC()
	return p, mapErr(domain.CheckEventTime("event_time", *eventTime, s.Clock.Now()))
}

// activeFacility đọc cơ sở hiện hành; requireActive → cơ sở ngừng dùng bị từ chối.
func activeFacility(ctx context.Context, tx pgx.Tx, id uuid.UUID, requireActive bool) (*domain.Facility, error) {
	f, err := store.Current(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, ErrFacilityNotFound
	}
	if requireActive && f.Spec.Status != domain.StatusActive {
		return nil, ErrFacilityInactive
	}
	return f, nil
}

// ---------------------------------------------------------------------------
// Nhận lô
// ---------------------------------------------------------------------------

// ReceiveInput là một lần nhận hàng vào cơ sở, tạo MỘT lô có N nguồn.
type ReceiveInput struct {
	LotID       *uuid.UUID // UUIDv7 do thiết bị sinh khi ngoại tuyến (R-11); nil → server sinh
	FacilityID  uuid.UUID
	Code        string
	Product     domain.Product
	Mass        mass.Kg
	MoisturePct *decimal.Decimal
	Sources     []domain.SourceInput
	EventTime   time.Time
	DeviceID    *string
	IdemKey     string
	// WeighTicketRef (G3, Q-MKG-04): phiếu cân HUB_IN đã về hub (sự kiện nội bộ) —
	// id lô = lot_ref của phiếu, khối lượng = phiếu (Mass để trống hoặc bằng phiếu).
	WeighTicketRef *uuid.UUID
	// ManualReason: lý do nhập tay (cân hỏng) — bắt buộc khi tham số hub.weigh.v1
	// ticket_required = true và không có WeighTicketRef.
	ManualReason string
}

// ReceiveResult là kết quả nhận lô.
type ReceiveResult struct {
	Lot      domain.Lot
	Movement domain.Movement
	// OverCapacity: tồn sau khi nhận vượt sức chứa danh định — vẫn ghi nhận
	// (hàng đã về, không được mất bản ghi) nhưng báo cho người vận hành.
	OverCapacity bool
}

// Receive nhận một lô: sổ lô + dịch chuyển receive + bảng chiếu tồn trong MỘT giao dịch.
func (s *Service) Receive(ctx context.Context, in ReceiveInput) (res ReceiveResult, replayed bool, err error) {
	p, err := s.operator(ctx, in.IdemKey, &in.EventTime)
	if err != nil {
		return res, false, err
	}
	if !domain.ValidProduct(in.Product) {
		return res, false, problem.Validation.WithField("product", "straw_bale, straw_loose, dried_straw hoặc compressed_bale")
	}
	if in.WeighTicketRef == nil && in.Mass.Decimal().Sign() <= 0 {
		return res, false, problem.Validation.WithField("mass_kg", "phải > 0")
	}
	if in.Mass.Decimal().IsNegative() {
		return res, false, problem.Validation.WithField("mass_kg", "phải > 0")
	}
	manualReason := strings.TrimSpace(in.ManualReason)
	if n := len([]rune(manualReason)); n > 0 && (n < 10 || n > 500) {
		return res, false, problem.Validation.WithField("manual_reason", "10..500 ký tự")
	}
	sources, err := domain.BuildSources(in.Sources)
	if err != nil {
		return res, false, mapErr(err)
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		f, err := activeFacility(ctx, tx, in.FacilityID, true)
		if err != nil {
			return err
		}
		id := ids.New()
		if in.LotID != nil {
			id = *in.LotID
		}
		w, err := s.receiptBasis(ctx, tx, &in, manualReason)
		if err != nil {
			return err
		}
		if w != nil {
			id = w.LotID
		}
		code := in.Code
		if code == "" {
			code = domain.LotCode(id)
		} else if err := domain.ValidLotCode(code); err != nil {
			return mapErr(err)
		}
		if in.LotID != nil || w != nil {
			if prev, err := store.LotByID(ctx, tx, id); err != nil {
				return err
			} else if prev != nil {
				return ErrIDReused
			}
		}
		res.Lot = domain.Lot{
			ID: id, TenantID: p.TenantID, FacilityID: f.ID, Code: code, Product: in.Product, Origin: domain.OriginReceipt,
			Initial: in.Mass, MoisturePct: in.MoisturePct, Sources: sources, EventTime: in.EventTime, ActorRef: p.ActorRef,
			IdemKey: in.IdemKey,
		}
		if err := insert(store.InsertLot(ctx, tx, &res.Lot, in.DeviceID)); err != nil {
			return err
		}
		res.Movement = domain.Movement{
			ID: ids.New(), TenantID: p.TenantID, FacilityID: f.ID, LotID: id, Kind: domain.MoveReceive, Qty: in.Mass,
			BalanceAfter: in.Mass, EventTime: in.EventTime, ActorRef: p.ActorRef, IdemKey: in.IdemKey + ":mv",
		}
		if err := insert(store.InsertMovement(ctx, tx, &res.Movement, in.DeviceID)); err != nil {
			return err
		}
		if err := store.InsertBalance(ctx, tx, p.TenantID, res.Lot, in.Mass, res.Movement.Seq); err != nil {
			return err
		}
		if err := s.recordReceiptBasis(ctx, tx, p.TenantID, p.ActorRef, res.Lot, w, manualReason); err != nil {
			return err
		}
		used, err := store.UsedKg(ctx, tx, f.ID)
		if err != nil {
			return err
		}
		res.OverCapacity = used.Cmp(f.Spec.Capacity) > 0
		return nil
	}, func(tx pgx.Tx) (bool, error) {
		l, err := store.LotByIdem(ctx, tx, in.IdemKey)
		if err != nil || l == nil {
			return false, err
		}
		m, err := store.MovementByIdem(ctx, tx, in.IdemKey+":mv")
		if err != nil || m == nil {
			return false, err
		}
		res = ReceiveResult{Lot: *l, Movement: *m}
		return true, nil
	})
	return res, replayed, err
}

// ---------------------------------------------------------------------------
// Mẻ biến đổi
// ---------------------------------------------------------------------------

// TransformIn là lượng lấy từ một lô vào mẻ.
type TransformIn struct {
	LotID uuid.UUID
	Mass  mass.Kg
}

// TransformOut là một lô ra của mẻ.
type TransformOut struct {
	Code        string
	Product     domain.Product
	Mass        mass.Kg
	MoisturePct *decimal.Decimal
}

// TransformInput là một mẻ sấy / nén / phân loại.
type TransformInput struct {
	FacilityID uuid.UUID
	Kind       domain.BatchKind
	Inputs     []TransformIn
	Outputs    []TransformOut
	EventTime  time.Time
	DeviceID   *string
	IdemKey    string
}

// TransformResult là mẻ đã ghi cùng các lô ra.
type TransformResult struct {
	Batch   domain.Batch
	Outputs []domain.Lot
}

// Transform chạy một mẻ: trừ lô vào (transform_out), tạo lô ra có nguồn là các
// lô vào với tỷ lệ theo khối lượng (Σ = 1), ghi hao hụt = vào − ra ≥ 0. Thành
// phần đầu vào được chụp TẠI THỜI ĐIỂM MẺ CHẠY vào sổ mẻ (mục 16).
func (s *Service) Transform(ctx context.Context, in TransformInput) (res TransformResult, replayed bool, err error) {
	p, err := s.operator(ctx, in.IdemKey, &in.EventTime)
	if err != nil {
		return res, false, err
	}
	if !domain.ValidBatchKind(in.Kind) {
		return res, false, problem.Validation.WithField("kind", "drying, compression hoặc sorting")
	}
	inputs := make([]domain.BatchInput, len(in.Inputs))
	for i, x := range in.Inputs {
		inputs[i] = domain.BatchInput{LotID: x.LotID, MassKg: x.Mass}
	}
	outputs := make([]domain.BatchOutput, len(in.Outputs))
	for i, y := range in.Outputs {
		id := ids.New()
		code := y.Code
		if code == "" {
			code = domain.LotCode(id)
		} else if err := domain.ValidLotCode(code); err != nil {
			return res, false, mapErr(err)
		}
		outputs[i] = domain.BatchOutput{LotID: id, Code: code, Product: y.Product, MassKg: y.Mass, MoisturePct: y.MoisturePct}
	}
	tin, tout, loss, err := domain.Totals(inputs, outputs)
	if err != nil {
		return res, false, mapErr(err)
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		f, err := activeFacility(ctx, tx, in.FacilityID, true)
		if err != nil {
			return err
		}
		// Khóa lô vào theo thứ tự lot_id để hai mẻ song song không khóa chéo.
		order := make([]int, len(inputs))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool { return inputs[order[a]].LotID.String() < inputs[order[b]].LotID.String() })
		balances := make([]domain.Balance, len(inputs))
		for _, i := range order {
			b, err := store.BalanceForUpdate(ctx, tx, inputs[i].LotID)
			if err != nil {
				return err
			}
			if b == nil {
				return ErrLotNotFound.WithDetail("lô vào " + inputs[i].LotID.String())
			}
			if b.FacilityID != f.ID {
				return ErrLotOtherFacility
			}
			lot, err := store.LotByID(ctx, tx, b.LotID)
			if err != nil {
				return err
			}
			inputs[i].MoisturePct, inputs[i].Product, inputs[i].Sources = b.MoisturePct, b.Product, lot.Sources
			balances[i] = *b
		}
		srcs, err := domain.OutputSources(inputs)
		if err != nil {
			return mapErr(err)
		}
		res.Batch = domain.Batch{
			ID: ids.New(), TenantID: p.TenantID, FacilityID: f.ID, Kind: in.Kind, Inputs: inputs, Outputs: outputs,
			Input: tin, Output: tout, Loss: loss, EventTime: in.EventTime, ActorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		if err := insert(store.InsertBatch(ctx, tx, &res.Batch, in.DeviceID)); err != nil {
			return err
		}
		ref := "batch"
		for i, x := range inputs {
			after, err := domain.Apply(balances[i].Remaining, mass.Kg{}.Sub(x.MassKg))
			if err != nil {
				return ErrInsufficientStock.WithDetail("lô " + balances[i].Code + " còn " + balances[i].Remaining.String() + " kg")
			}
			m := domain.Movement{
				ID: ids.New(), TenantID: p.TenantID, FacilityID: f.ID, LotID: x.LotID, Kind: domain.MoveTransformOut,
				Qty: mass.Kg{}.Sub(x.MassKg), BalanceAfter: after, RefType: &ref, RefID: &res.Batch.ID, EventTime: in.EventTime,
				ActorRef: p.ActorRef, IdemKey: in.IdemKey + ":in:" + x.LotID.String(),
			}
			if err := insert(store.InsertMovement(ctx, tx, &m, in.DeviceID)); err != nil {
				return err
			}
			if err := store.SetBalance(ctx, tx, x.LotID, after, m.Seq); err != nil {
				return err
			}
		}
		for i, y := range outputs {
			batchID := res.Batch.ID
			l := domain.Lot{
				ID: y.LotID, TenantID: p.TenantID, FacilityID: f.ID, Code: y.Code, Product: y.Product, Origin: domain.OriginTransform,
				BatchID: &batchID, Initial: y.MassKg, MoisturePct: y.MoisturePct, Sources: srcs, EventTime: in.EventTime,
				ActorRef: p.ActorRef, IdemKey: in.IdemKey + ":lot:" + strconv.Itoa(i),
			}
			if err := insert(store.InsertLot(ctx, tx, &l, in.DeviceID)); err != nil {
				return err
			}
			m := domain.Movement{
				ID: ids.New(), TenantID: p.TenantID, FacilityID: f.ID, LotID: l.ID, Kind: domain.MoveTransformIn, Qty: y.MassKg,
				BalanceAfter: y.MassKg, RefType: &ref, RefID: &batchID, EventTime: in.EventTime, ActorRef: p.ActorRef,
				IdemKey: in.IdemKey + ":out:" + strconv.Itoa(i),
			}
			if err := insert(store.InsertMovement(ctx, tx, &m, in.DeviceID)); err != nil {
				return err
			}
			if err := store.InsertBalance(ctx, tx, p.TenantID, l, y.MassKg, m.Seq); err != nil {
				return err
			}
			res.Outputs = append(res.Outputs, l)
		}
		return nil
	}, func(tx pgx.Tx) (bool, error) {
		b, err := store.BatchByIdem(ctx, tx, in.IdemKey)
		if err != nil || b == nil {
			return false, err
		}
		res = TransformResult{Batch: *b}
		for _, y := range b.Outputs {
			l, err := store.LotByID(ctx, tx, y.LotID)
			if err != nil {
				return false, err
			}
			if l != nil {
				res.Outputs = append(res.Outputs, *l)
			}
		}
		return true, nil
	})
	return res, replayed, err
}

// ---------------------------------------------------------------------------
// Xuất theo rủi ro xuống cấp
// ---------------------------------------------------------------------------

// DispatchInput là một phiếu xuất.
type DispatchInput struct {
	FacilityID     uuid.UUID
	Mass           mass.Kg
	DestinationRef *uuid.UUID
	// Allocations rỗng → phân bổ theo đề xuất rủi ro (risk_suggested); có → người
	// vận hành chỉ định lô (manual), tổng phải bằng Mass.
	Allocations []domain.Allocation
	EventTime   time.Time
	DeviceID    *string
	IdemKey     string
}

// DispatchResult là phiếu xuất đã ghi.
type DispatchResult struct {
	Dispatch  store.Dispatch
	Movements []domain.Movement
}

// Dispatch xuất hàng: phân bổ ĐỦ theo lô ("không có lô thì không có hàng"),
// mặc định lô rủi ro xuống cấp cao nhất trước (không FEFO). Kiểm tồn lại trong
// giao dịch, trên các lô đã khóa.
func (s *Service) Dispatch(ctx context.Context, in DispatchInput) (res DispatchResult, replayed bool, err error) {
	p, err := s.operator(ctx, in.IdemKey, &in.EventTime)
	if err != nil {
		return res, false, err
	}
	if in.Mass.Decimal().Sign() <= 0 {
		return res, false, problem.Validation.WithField("mass_kg", "phải > 0")
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		f, err := activeFacility(ctx, tx, in.FacilityID, false)
		if err != nil {
			return err
		}
		res, err = s.dispatchTx(ctx, tx, p.TenantID, p.ActorRef, f.ID, in, domain.MoveDispatch, "dispatch", nil)
		return err
	}, func(tx pgx.Tx) (bool, error) {
		d, err := store.DispatchByIdem(ctx, tx, in.IdemKey)
		if err != nil || d == nil {
			return false, err
		}
		ms, err := store.MovementsByRef(ctx, tx, "dispatch", d.ID)
		if err != nil {
			return false, err
		}
		res = DispatchResult{Dispatch: *d, Movements: ms}
		return true, nil
	})
	return res, replayed, err
}

// rank xếp lô còn tồn của cơ sở theo hub.risk.v2 (tham số hiện hành hoặc mặc
// định tạm) với cảnh báo môi trường từ EnvReadings; trả chuỗi ghim phiên bản.
func (s *Service) rank(ctx context.Context, tx pgx.Tx, tid, facility uuid.UUID, lots []domain.Balance, now time.Time) ([]domain.Ranked, string, error) {
	rp, pinned, err := activeRisk(ctx, tx)
	if err != nil {
		return nil, "", err
	}
	var readings []domain.EnvReading
	if len(lots) > 0 {
		from := lots[0].ReceivedAt
		for _, l := range lots {
			if l.ReceivedAt.Before(from) {
				from = l.ReceivedAt
			}
		}
		rs, err := s.env().Readings(ctx, tid, facility, from, now)
		if err != nil {
			return nil, "", err
		}
		for _, r := range rs {
			readings = append(readings, domain.EnvReading{FacilityID: r.FacilityID, SensorID: r.SensorID, HumidityPct: r.HumidityPct, TempC: r.TempC, At: r.At})
		}
	}
	return rp.RankV2(pinned, lots, readings, now), pinned, nil
}

// dispatchTx ghi phiếu xuất + dịch chuyển kind (dispatch | transfer_out) trong tx đã mở.
// refType/refID của dịch chuyển: "dispatch"/id phiếu, hoặc "transfer"/id phiếu chuyển.
func (s *Service) dispatchTx(ctx context.Context, tx pgx.Tx, tid, actor, facility uuid.UUID, in DispatchInput, kind domain.MovementKind,
	refType string, refID *uuid.UUID) (res DispatchResult, err error) {
	lots, err := store.Balances(ctx, tx, facility, true)
	if err != nil {
		return res, err
	}
	ranked, version, err := s.rank(ctx, tx, tid, facility, lots, s.Clock.Now())
	if err != nil {
		return res, err
	}
	var allocs []domain.Allocation
	strategy := "risk_suggested"
	if len(in.Allocations) == 0 {
		if allocs, err = domain.Allocate(ranked, in.Mass); err != nil {
			return res, mapErr(err)
		}
	} else {
		strategy = "manual"
		if allocs, err = manualAllocations(ranked, in.Allocations, in.Mass); err != nil {
			return res, err
		}
	}
	res.Dispatch = store.Dispatch{
		ID: ids.New(), TenantID: tid, FacilityID: facility, Mass: in.Mass, DestinationRef: in.DestinationRef,
		Allocations: allocs, Strategy: strategy, RuleVersion: version, EventTime: in.EventTime, ActorRef: actor,
		IdemKey: in.IdemKey,
	}
	if err := insert(store.InsertDispatch(ctx, tx, &res.Dispatch, in.DeviceID)); err != nil {
		return res, err
	}
	if refID == nil {
		refID = &res.Dispatch.ID
	}
	rem := map[uuid.UUID]mass.Kg{}
	for _, l := range lots {
		rem[l.LotID] = l.Remaining
	}
	for _, a := range allocs {
		after, err := domain.Apply(rem[a.LotID], mass.Kg{}.Sub(a.MassKg))
		if err != nil {
			return res, mapErr(err)
		}
		ref := refType
		m := domain.Movement{
			ID: ids.New(), TenantID: tid, FacilityID: facility, LotID: a.LotID, Kind: kind,
			Qty: mass.Kg{}.Sub(a.MassKg), BalanceAfter: after, RefType: &ref, RefID: refID, EventTime: in.EventTime,
			ActorRef: actor, IdemKey: in.IdemKey + ":mv:" + a.LotID.String(),
		}
		if err := insert(store.InsertMovement(ctx, tx, &m, in.DeviceID)); err != nil {
			return res, err
		}
		if err := store.SetBalance(ctx, tx, a.LotID, after, m.Seq); err != nil {
			return res, err
		}
		res.Movements = append(res.Movements, m)
	}
	return res, nil
}

func manualAllocations(ranked []domain.Ranked, req []domain.Allocation, total mass.Kg) ([]domain.Allocation, error) {
	byLot := map[uuid.UUID]domain.Ranked{}
	for _, r := range ranked {
		byLot[r.LotID] = r
	}
	seen := map[uuid.UUID]bool{}
	sum := mass.Kg{}
	out := make([]domain.Allocation, 0, len(req))
	for i, a := range req {
		f := "allocations[" + strconv.Itoa(i) + "]"
		if seen[a.LotID] {
			return nil, ErrDuplicateLotInput
		}
		seen[a.LotID] = true
		r, ok := byLot[a.LotID]
		if !ok {
			return nil, ErrLotOtherFacility.WithDetail(f + ": lô không còn tồn ở cơ sở này")
		}
		if a.MassKg.Decimal().Sign() <= 0 {
			return nil, problem.Validation.WithField(f+".mass_kg", "phải > 0")
		}
		if a.MassKg.Cmp(r.Remaining) > 0 {
			return nil, ErrInsufficientStock.WithDetail(f + ": vượt tồn còn lại của lô")
		}
		risk := r.Risk
		out = append(out, domain.Allocation{LotID: a.LotID, MassKg: a.MassKg, Risk: &risk})
		sum = sum.Add(a.MassKg)
	}
	if sum.Cmp(total) != 0 {
		return nil, ErrAllocationMismatch
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Sửa sai bằng dịch chuyển bù
// ---------------------------------------------------------------------------

// CorrectInput là yêu cầu bù một dịch chuyển nhận/xuất ghi sai.
type CorrectInput struct {
	MovementID uuid.UUID
	Reason     string
	EventTime  time.Time
	DeviceID   *string
	IdemKey    string
}

// Correct ghi dòng bù ngược dấu (kind = correction, supersedes_id + lý do) —
// không UPDATE/DELETE dòng gốc (RB-1). Mỗi dòng chỉ được bù một lần; sai tiếp
// thì ghi nghiệp vụ mới (nhận/xuất đúng).
func (s *Service) Correct(ctx context.Context, in CorrectInput) (m domain.Movement, replayed bool, err error) {
	p, err := s.operator(ctx, in.IdemKey, &in.EventTime)
	if err != nil {
		return m, false, err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		orig, err := store.MovementByID(ctx, tx, in.MovementID)
		if err != nil {
			return err
		}
		if orig == nil {
			return ErrMovementNotFound
		}
		qty, err := domain.Compensate(*orig, in.Reason)
		if err != nil {
			return mapErr(err)
		}
		b, err := store.BalanceForUpdate(ctx, tx, orig.LotID) // khóa trước khi kiểm "đã bù" — tuần tự hóa hai lần bù song song
		if err != nil {
			return err
		}
		if b == nil {
			return ErrLotNotFound
		}
		if prev, err := store.CorrectionOf(ctx, tx, orig.ID); err != nil {
			return err
		} else if prev != nil {
			return ErrAlreadyCorrected
		}
		after, err := domain.Apply(b.Remaining, qty)
		if err != nil {
			return ErrInsufficientStock.WithDetail("bù làm tồn của lô âm: hàng của lô đã được xuất/biến đổi")
		}
		ref, reason := "movement", strings.TrimSpace(in.Reason)
		m = domain.Movement{
			ID: ids.New(), TenantID: p.TenantID, FacilityID: orig.FacilityID, LotID: orig.LotID, Kind: domain.MoveCorrection,
			Qty: qty, BalanceAfter: after, RefType: &ref, RefID: &orig.ID, EventTime: in.EventTime, SupersedesID: &orig.ID,
			CorrectionReason: &reason, ActorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		if err := insert(store.InsertMovement(ctx, tx, &m, in.DeviceID)); err != nil {
			return err
		}
		return store.SetBalance(ctx, tx, orig.LotID, after, m.Seq)
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.MovementByIdem(ctx, tx, in.IdemKey)
		if prev != nil {
			m = *prev
		}
		return prev != nil, err
	})
	return m, replayed, err
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// Inventory là tồn của một cơ sở — dựng từ lô (nguồn sự thật duy nhất).
type Inventory struct {
	Facility domain.Facility
	Lots     []domain.Ranked // theo rủi ro giảm dần
	Capacity domain.Capacity
	Rule     string
}

// Inventory đọc tồn theo lô và sức chứa của cơ sở.
func (s *Service) Inventory(ctx context.Context, facility uuid.UUID) (Inventory, error) {
	var inv Inventory
	now := s.Clock.Now()
	err := s.read(ctx, func(tx pgx.Tx) error {
		f, err := activeFacility(ctx, tx, facility, false)
		if err != nil {
			return err
		}
		lots, err := store.Balances(ctx, tx, f.ID, false)
		if err != nil {
			return err
		}
		used := mass.Kg{}
		for _, l := range lots {
			used = used.Add(l.Remaining)
		}
		ranked, version, err := s.rank(ctx, tx, tenantOf(ctx), f.ID, lots, now)
		if err != nil {
			return err
		}
		inv = Inventory{Facility: *f, Lots: ranked, Capacity: domain.Snapshot(f.ID, now, f.Spec.Capacity, used), Rule: version}
		return nil
	})
	return inv, err
}

// Suggestion là đề xuất thứ tự xuất.
type Suggestion struct {
	Rule        string
	Lots        []domain.Ranked
	Allocations []domain.Allocation // có khi truyền lượng cần xuất
	Shortfall   *mass.Kg
}

// SuggestDispatch đề xuất thứ tự xuất theo rủi ro xuống cấp (không khóa, không ghi).
func (s *Service) SuggestDispatch(ctx context.Context, facility uuid.UUID, qty *mass.Kg) (Suggestion, error) {
	inv, err := s.Inventory(ctx, facility)
	if err != nil {
		return Suggestion{}, err
	}
	sg := Suggestion{Rule: inv.Rule, Lots: inv.Lots}
	if qty != nil {
		allocs, err := domain.Allocate(inv.Lots, *qty)
		if err != nil {
			if !errors.Is(err, domain.ErrInsufficientStock) {
				return Suggestion{}, mapErr(err)
			}
			short := qty.Sub(inv.Capacity.Used)
			sg.Shortfall = &short
			allocs, _ = domain.Allocate(inv.Lots, inv.Capacity.Used)
		}
		sg.Allocations = allocs
	}
	return sg, nil
}

// LotView là lô kèm tồn và lịch sử dịch chuyển.
type LotView struct {
	Lot       domain.Lot
	Remaining mass.Kg
	Movements []domain.Movement
}

// Lot đọc lô, tồn còn lại và mọi dịch chuyển.
func (s *Service) Lot(ctx context.Context, id uuid.UUID) (LotView, error) {
	var v LotView
	err := s.read(ctx, func(tx pgx.Tx) error {
		l, err := store.LotByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if l == nil {
			return ErrLotNotFound
		}
		v.Lot = *l
		if v.Movements, err = store.MovementsOfLot(ctx, tx, id); err != nil {
			return err
		}
		for _, m := range v.Movements {
			v.Remaining = v.Remaining.Add(m.Qty)
		}
		return nil
	})
	return v, err
}

// LotInfo cài đặt port.Lots cho module mkg khác.
func (s *Service) LotInfo(ctx context.Context, id uuid.UUID) (port.LotInfo, error) {
	v, err := s.Lot(ctx, id)
	if err != nil {
		return port.LotInfo{}, err
	}
	info := port.LotInfo{
		LotID: v.Lot.ID, FacilityID: v.Lot.FacilityID, Product: string(v.Lot.Product), InitialKg: v.Lot.Initial.String(),
		RemainingKg: v.Remaining.String(),
	}
	for _, src := range v.Lot.Sources {
		info.Sources = append(info.Sources, port.LotSource{Kind: string(src.Kind), Ref: src.Ref, Ratio: src.Ratio.StringFixed(domain.RatioScale)})
	}
	return info, nil
}

// LotOrigin cài đặt port.LotOrigins: tenant tường minh, không kiểm vai (chỉ đọc lô + dịch chuyển).
func (s *Service) LotOrigin(ctx context.Context, tid, id uuid.UUID) (port.LotInfo, error) {
	var info port.LotInfo
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		l, err := store.LotByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if l == nil {
			return port.ErrLotNotFound
		}
		ms, err := store.MovementsOfLot(ctx, tx, id)
		if err != nil {
			return err
		}
		var rem mass.Kg
		for _, m := range ms {
			rem = rem.Add(m.Qty)
		}
		info = port.LotInfo{LotID: l.ID, FacilityID: l.FacilityID, Product: string(l.Product), InitialKg: l.Initial.String(),
			RemainingKg: rem.String()}
		for _, src := range l.Sources {
			info.Sources = append(info.Sources, port.LotSource{Kind: string(src.Kind), Ref: src.Ref, Ratio: src.Ratio.StringFixed(domain.RatioScale)})
		}
		return nil
	})
	return info, err
}

// RebuildProjections dựng lại bảng chiếu tồn theo lô của tenant từ sổ (R-22).
func (s *Service) RebuildProjections(ctx context.Context) (int64, error) {
	p, err := authz.Require(ctx, RoleHubApprover)
	if err != nil {
		return 0, err
	}
	return s.RebuildTenant(ctx, p.TenantID)
}

// RebuildTenant dựng lại bảng chiếu của tenant tid từ sổ — tenant TƯỜNG MINH, không kiểm vai: chỉ cho công cụ
// vận hành chạy trên BẢN SAO database (diễn tập R-22, cmd/rebuilddrill). API dùng RebuildProjections (có vai).
func (s *Service) RebuildTenant(ctx context.Context, tid uuid.UUID) (int64, error) {
	var n int64
	err := s.writeOnly(ctx, tid, func(tx pgx.Tx) error {
		var e error
		n, e = store.RebuildBalances(ctx, tx)
		return e
	})
	return n, err
}

func (s *Service) writeOnly(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	_, err := s.write(ctx, tid, fn, func(pgx.Tx) (bool, error) { return false, nil })
	return err
}
