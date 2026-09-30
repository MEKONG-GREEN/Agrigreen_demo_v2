package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ContractInput là nội dung một phiên bản hợp đồng mua rơm (proto CT-01..03).
type ContractInput struct {
	HTXRef         uuid.UUID
	Basis          port.PricingBasis
	UnitPrice      money.Price
	CommittedKg    mass.Kg
	MaxMoisturePct *decimal.Decimal
	ValidFrom      string
	ValidTo        string
	Note           string
	Reason         string
	IdemKey        string
}

func (in ContractInput) spec() (domain.ContractSpec, error) {
	from, err := domain.ParseDate("valid_from", in.ValidFrom)
	if err != nil {
		return domain.ContractSpec{}, err
	}
	to, err := domain.ParseDate("valid_to", in.ValidTo)
	if err != nil {
		return domain.ContractSpec{}, err
	}
	sp := domain.ContractSpec{Basis: in.Basis, UnitPrice: in.UnitPrice, CommittedKg: in.CommittedKg, MaxMoisturePct: in.MaxMoisturePct,
		ValidFrom: from, ValidTo: to, Note: in.Note}
	return sp, domain.ValidateContract(sp)
}

// ProposeContract lập hợp đồng mới (phiên bản 1, chờ duyệt). CT-01: HTX đã có hợp đồng hiệu lực chồng
// thời gian → 409 orders.contract_overlap.
func (s *Service) ProposeContract(ctx context.Context, in ContractInput) ([]store.Contract, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return nil, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return nil, false, err
	}
	if in.HTXRef == uuid.Nil {
		return nil, false, mapErr(domain.Field("htx_ref", "bắt buộc"))
	}
	sp, err := in.spec()
	if err != nil {
		return nil, false, mapErr(err)
	}
	reason, err := domain.CheckReason("reason", in.Reason, false)
	if err != nil {
		return nil, false, mapErr(err)
	}
	var out []store.Contract
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, p.TenantID, "htx:"+in.HTXRef.String()); err != nil {
			return err
		}
		if err := checkOverlap(ctx, tx, in.HTXRef, sp.ValidFrom, sp.ValidTo, uuid.Nil); err != nil {
			return err
		}
		if err := store.Lock(ctx, tx, p.TenantID, "code:contract"); err != nil {
			return err
		}
		n, err := store.NextCode(ctx, tx, "contract_current", "contract_id")
		if err != nil {
			return err
		}
		c := contractRow(sp, in.HTXRef, ids.New(), 1, domain.Code("HDR", n, 5), p.ActorRef, strPtr(reason))
		c.Meta = store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: domain.RuleContract}
		c.Kind, c.Status = "proposed", "pending"
		if err := store.InsertContractEvent(ctx, tx, p.TenantID, &c); err != nil {
			return err
		}
		out, err = store.ContractVersions(ctx, tx, c.ContractID)
		return err
	}, s.replayContract(ctx, in.IdemKey, &out))
	return out, replayed, err
}

// ReviseContract đề xuất phiên bản mới (chờ duyệt) cho hợp đồng đang hiệu lực. Mã và HTX giữ nguyên.
func (s *Service) ReviseContract(ctx context.Context, id uuid.UUID, in ContractInput) ([]store.Contract, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return nil, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return nil, false, err
	}
	sp, err := in.spec()
	if err != nil {
		return nil, false, mapErr(err)
	}
	reason, err := domain.CheckReason("reason", in.Reason, true)
	if err != nil {
		return nil, false, mapErr(err)
	}
	var out []store.Contract
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, p.TenantID, "contract:"+id.String()); err != nil {
			return err
		}
		vs, err := store.ContractVersions(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(vs) == 0 {
			return ErrContractNotFound
		}
		if vs[0].Status != "active" {
			return ErrContractTransition.WithDetail("chỉ sửa hợp đồng đang hiệu lực, không có phiên bản chờ duyệt")
		}
		c := contractRow(sp, vs[0].HTXRef, id, vs[0].Version+1, vs[0].Code, p.ActorRef, &reason)
		c.Meta = store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: domain.RuleContract}
		c.Kind, c.Status = "proposed", "pending"
		if err := store.InsertContractEvent(ctx, tx, p.TenantID, &c); err != nil {
			return err
		}
		out, err = store.ContractVersions(ctx, tx, id)
		return err
	}, s.replayContract(ctx, in.IdemKey, &out))
	return out, replayed, err
}

// DecideContract duyệt / từ chối một phiên bản chờ duyệt. Người duyệt (sales_approver) KHÁC người lập
// (R-56). Duyệt: kiểm lại CT-01; phiên bản hiệu lực trước (nếu có) → superseded trong cùng giao dịch.
func (s *Service) DecideContract(ctx context.Context, id uuid.UUID, version int, approve bool, reason, idem string) ([]store.Contract, bool, error) {
	p, err := authz.Require(ctx, RoleApprover)
	if err != nil {
		return nil, false, err
	}
	if err := requireIdem(idem); err != nil {
		return nil, false, err
	}
	r, err := domain.CheckReason("reason", reason, !approve)
	if err != nil {
		return nil, false, mapErr(err)
	}
	var out []store.Contract
	now := s.Clock.Now()
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, p.TenantID, "contract:"+id.String()); err != nil {
			return err
		}
		c, err := store.ContractVersion(ctx, tx, id, version)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrContractNotFound
		}
		if c.Status != "pending" {
			return ErrContractTransition.WithDetail("phiên bản không ở trạng thái chờ duyệt")
		}
		if err := authz.EnsureDistinctApproverFor(c.ProposerRef, p, RoleApprover); err != nil {
			return err
		}
		meta := store.Meta{EventTime: now, ActorRef: p.ActorRef, RuleVersion: domain.RuleContract}
		if approve {
			if err := store.Lock(ctx, tx, p.TenantID, "htx:"+c.HTXRef.String()); err != nil {
				return err
			}
			if err := checkOverlap(ctx, tx, c.HTXRef, c.ValidFrom, c.ValidTo, id); err != nil {
				return err
			}
			vs, err := store.ContractVersions(ctx, tx, id)
			if err != nil {
				return err
			}
			for _, old := range vs {
				if old.Status != "active" {
					continue
				}
				sup := old
				sup.Meta = meta
				sup.ID, sup.IdemKey, sup.Kind, sup.Status = ids.New(), idem+":supersede", "superseded", "superseded"
				if err := store.InsertContractEvent(ctx, tx, p.TenantID, &sup); err != nil {
					return err
				}
			}
			c.Kind, c.Status, c.ApproverRef = "approved", "active", &p.ActorRef
			if r != "" {
				c.Reason = &r
			}
		} else {
			c.Kind, c.Status, c.Reason = "rejected", "rejected", &r
		}
		c.Meta = meta
		c.ID, c.IdemKey = ids.New(), idem
		if err := store.InsertContractEvent(ctx, tx, p.TenantID, c); err != nil {
			return err
		}
		out, err = store.ContractVersions(ctx, tx, id)
		return err
	}, s.replayContract(ctx, idem, &out))
	return out, replayed, err
}

// TerminateContract chấm dứt hợp đồng đang hiệu lực (proto setContractStatus huy / het_han) — có lý do.
func (s *Service) TerminateContract(ctx context.Context, id uuid.UUID, reason, idem string) ([]store.Contract, bool, error) {
	p, err := authz.Require(ctx, RoleApprover)
	if err != nil {
		return nil, false, err
	}
	if err := requireIdem(idem); err != nil {
		return nil, false, err
	}
	r, err := domain.CheckReason("reason", reason, true)
	if err != nil {
		return nil, false, mapErr(err)
	}
	var out []store.Contract
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, p.TenantID, "contract:"+id.String()); err != nil {
			return err
		}
		vs, err := store.ContractVersions(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(vs) == 0 {
			return ErrContractNotFound
		}
		var active *store.Contract
		for i := range vs {
			if vs[i].Status == "active" {
				active = &vs[i]
			}
		}
		if active == nil {
			return ErrContractTransition.WithDetail("hợp đồng không có phiên bản hiệu lực")
		}
		c := *active
		c.Meta = store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: idem, RuleVersion: domain.RuleContract}
		c.Kind, c.Status, c.Reason = "terminated", "terminated", &r
		if err := store.InsertContractEvent(ctx, tx, p.TenantID, &c); err != nil {
			return err
		}
		out, err = store.ContractVersions(ctx, tx, id)
		return err
	}, s.replayContract(ctx, idem, &out))
	return out, replayed, err
}

// Contract đọc mọi phiên bản của hợp đồng.
func (s *Service) Contract(ctx context.Context, id uuid.UUID) ([]store.Contract, error) {
	var out []store.Contract
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ContractVersions(ctx, tx, id)
		if e == nil && len(out) == 0 {
			return ErrContractNotFound
		}
		return e
	})
	return out, err
}

// ListContracts liệt kê hợp đồng (phiên bản mới nhất).
func (s *Service) ListContracts(ctx context.Context, htx *uuid.UUID, status string, limit int) ([]store.Contract, error) {
	var out []store.Contract
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListContracts(ctx, tx, htx, status, clampLimit(limit))
		return e
	})
	return out, err
}

// EffectiveContract trả hợp đồng hiệu lực của HTX tại ngày on (HTTP, người dùng tenant MKG).
func (s *Service) EffectiveContract(ctx context.Context, htx uuid.UUID, on time.Time) (port.ContractTerms, error) {
	p, err := authz.Require(ctx, RoleOfficer, RoleApprover)
	if err != nil {
		return port.ContractTerms{}, err
	}
	t, err := s.ContractFor(ctx, p.TenantID, htx, on)
	if errors.Is(err, port.ErrNoContract) {
		return t, ErrNoContract
	}
	return t, err
}

// ContractFor cài đặt port.Contracts (proto contractFor): hợp đồng hiệu lực của HTX bao trùm ngày
// nghiệp vụ (giờ VN) của on. Không có → port.ErrNoContract — người gọi KHÔNG đoán giá (proto PM-03).
func (s *Service) ContractFor(ctx context.Context, tid, htx uuid.UUID, on time.Time) (port.ContractTerms, error) {
	var t port.ContractTerms
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		c, err := store.ContractOn(ctx, tx, htx, domain.Day(on))
		if err != nil {
			return err
		}
		if c == nil {
			return port.ErrNoContract
		}
		t, err = contractTerms(*c)
		return err
	})
	return t, err
}

func contractTerms(c store.Contract) (port.ContractTerms, error) {
	price, err := money.ParsePrice(c.UnitPrice.String())
	if err != nil {
		return port.ContractTerms{}, err
	}
	kg, err := mass.ParseKg(c.CommittedKg.String())
	if err != nil {
		return port.ContractTerms{}, err
	}
	t := port.ContractTerms{ContractID: c.ContractID, Code: c.Code, HTXRef: c.HTXRef, Basis: port.PricingBasis(c.Basis), UnitPrice: price,
		CommittedKg: kg, ValidFrom: c.ValidFrom, ValidTo: c.ValidTo, Version: c.Version}
	if c.MaxMoisturePct.Valid {
		m := c.MaxMoisturePct.Decimal.StringFixed(2)
		t.MaxMoisturePct = &m
	}
	return t, nil
}

func checkOverlap(ctx context.Context, tx pgx.Tx, htx uuid.UUID, from, to time.Time, except uuid.UUID) error {
	cs, err := store.ActiveContracts(ctx, tx, htx, from, to, except)
	if err != nil {
		return err
	}
	if len(cs) > 0 {
		return ErrContractOverlap.WithDetail("hợp đồng " + cs[0].Code + " hiệu lực " + cs[0].ValidFrom.Format("2006-01-02") + " – " +
			cs[0].ValidTo.Format("2006-01-02") + " — chấm dứt hợp đồng cũ trước")
	}
	return nil
}

func contractRow(sp domain.ContractSpec, htx, id uuid.UUID, version int, code string, proposer uuid.UUID, reason *string) store.Contract {
	return store.Contract{ContractID: id, Version: version, Code: code, HTXRef: htx, Basis: string(sp.Basis), UnitPrice: sp.UnitPrice.Decimal(),
		CommittedKg: sp.CommittedKg.Decimal(), MaxMoisturePct: nullDec(sp.MaxMoisturePct), ValidFrom: sp.ValidFrom, ValidTo: sp.ValidTo,
		Note: strPtr(sp.Note), ProposerRef: proposer, Reason: reason}
}

func (s *Service) replayContract(ctx context.Context, idem string, out *[]store.Contract) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		id, err := store.EntityByIdem(ctx, tx, "contract_event", "contract_id", idem)
		if err != nil || id == nil {
			return false, err
		}
		*out, err = store.ContractVersions(ctx, tx, *id)
		return err == nil, err
	}
}

func clampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}
