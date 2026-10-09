package ingest

// Port THẬT của module chủ cho hợp đồng song phương (W5.6 — ADR-0034, thay sổ đệm Staging của W5.4): bản ghi đã qua
// hợp đồng của collection_schedule, roll_count_statement (author_side a) đi vào sổ song phương pf_coop của
// pf/cooperative qua cổng hẹp port.BilateralLedger, ở tenant đối tác gửi (tenant MKG lấy từ bản ghi khóa) — chỉ nhánh
// counterparty của chính sách hai bên. htx_roll_confirmation (author_side b) KHÔNG nối qua cổng: HTX ghi trên App HTX;
// kênh api/file của bên b chờ gắn khách hàng API ↔ Nút HTX (định danh máy pf/accounts, W7.2) → mặc định từ chối
// (integration.no_owner_port).

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// BilateralTarget là tên đích ghi vào kết quả bản ghi của hợp đồng song phương.
const BilateralTarget = "pf/cooperative.bilateral_record"

// PairSink là RecordSink của hợp đồng song phương bên a: tách khóa cặp (htx_ref) rồi gọi sổ song phương.
// Không có cặp hiệu lực → rejected integration.pair_missing; cùng khóa nghiệp vụ khác nội dung → rejected
// integration.business_key_conflict; lỗi DB → lỗi tạm (giao lại).
type PairSink struct {
	Ledger port.BilateralLedger
}

// Ingest cài đặt RecordSink.
func (s PairSink) Ingest(ctx context.Context, r Record) error {
	if s.Ledger == nil {
		return port.ErrBilateralUnavailable
	}
	c, err := integration.Get(r.Contract, r.ContractVersion)
	if err != nil {
		return ErrRecordInvalid
	}
	key, err := c.PairKeyOf(r.Payload)
	if err != nil {
		return ErrRecordInvalid.WithDetail(err.Error())
	}
	htx, err := uuid.Parse(key)
	if err != nil {
		return ErrRecordInvalid.WithField("htx_ref", "phải là UUID")
	}
	return s.Ledger.AppendCounterparty(ctx, r.PartnerTenant, port.BilateralSubmission{Contract: r.Contract, ContractVersion: r.ContractVersion,
		EntityKey: r.EntityKey, BusinessKey: r.BusinessKey, HTXRef: htx, Payload: json.RawMessage(r.Payload), Channel: port.ChannelAPI,
		ClientID: r.ClientID, BatchID: r.BatchID, Index: r.Index, ReceivedAt: r.ReceivedAt})
}

// BilateralOwners nối các hợp đồng song phương do bên a ghi (author_side a) vào sổ song phương thật. Hợp đồng bên b
// không có owner (mặc định từ chối).
func BilateralOwners(ledger port.BilateralLedger) map[string]Owner {
	out := map[string]Owner{}
	if ledger == nil {
		return out
	}
	for _, name := range integration.BilateralContracts() {
		c, err := latest(name)
		if err != nil || c.Bilateral == nil || c.Bilateral.AuthorSide != port.SideA {
			continue
		}
		out[name] = Owner{Target: BilateralTarget, Sink: PairSink{Ledger: ledger}}
	}
	return out
}

func latest(name string) (*integration.Contract, error) {
	all, err := integration.All()
	if err != nil {
		return nil, err
	}
	var best *integration.Contract
	for _, c := range all {
		if c.Name == name && c.Kind == integration.KindIngest && (best == nil || c.Version > best.Version) {
			best = c
		}
	}
	if best == nil {
		return nil, ErrContractUnknown
	}
	return best, nil
}
