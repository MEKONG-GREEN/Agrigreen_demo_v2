package app

// BF-CN-VACC (#168, CN-09, UR-TINH-11 "mô hình, hộ, ngày nhận giống"): lịch tiêm gắn hộ mô hình CỦA KN (W11.47,
// extension/cntsmodel — chỉ đọc sổ của gói đó, KN không ghi dữ liệu HTX). Hộ cho biết xã, điểm, dự án, loại mô hình của
// điểm và cán bộ theo dõi điểm ("CB theo dõi MH" của BM-07). Chủ hộ chỉ hiện nhãn che bớt từ kho định danh.

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	mstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// ModelInfo là hộ mô hình của lứa kèm điểm trình diễn và dự án.
type ModelInfo struct {
	HouseholdRef uuid.UUID
	PersonRef    uuid.UUID
	CommuneCode  string
	SiteRef      uuid.UUID
	SiteName     string
	ModelType    string // loại mô hình của điểm
	OfficerRef   *uuid.UUID
	ProjectRef   uuid.UUID
	ProjectCode  string
	ProjectName  string
	Open         bool   // dự án, điểm chưa kết thúc và hộ chưa rút
	OwnerLabel   string // nhãn chủ hộ đã che bớt (điền ngoài giao dịch)
}

// loadModel đọc hộ mô hình (bản hiện hành) cùng điểm, dự án. Hộ không có trong sổ W11.47 → không có trong kết quả.
func loadModel(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]*ModelInfo, error) {
	out := map[uuid.UUID]*ModelInfo{}
	sites := map[uuid.UUID]*mstore.Site{}
	projects := map[uuid.UUID]*mstore.Project{}
	for _, ref := range refs {
		if _, done := out[ref]; done {
			continue
		}
		h, err := mstore.LatestHousehold(ctx, tx, ref)
		if err != nil {
			return nil, err
		}
		if h == nil {
			continue
		}
		x, ok := sites[h.SiteRef]
		if !ok {
			if x, err = mstore.LatestSite(ctx, tx, h.SiteRef); err != nil {
				return nil, err
			}
			sites[h.SiteRef] = x
		}
		p, ok := projects[h.ProjectRef]
		if !ok {
			if p, err = mstore.LatestProject(ctx, tx, h.ProjectRef); err != nil {
				return nil, err
			}
			projects[h.ProjectRef] = p
		}
		if x == nil || p == nil {
			continue
		}
		out[ref] = &ModelInfo{HouseholdRef: h.Ref, PersonRef: h.PersonRef, CommuneCode: h.CommuneCode, SiteRef: x.Ref, SiteName: x.Name,
			ModelType: x.ModelType, OfficerRef: x.OfficerRef, ProjectRef: p.Ref, ProjectCode: p.Code, ProjectName: p.Name,
			Open: p.Event != mdomain.EventClosed && x.Event != mdomain.EventClosed && h.Event != mdomain.EventWithdrawn}
	}
	return out, nil
}

// ownerLabels điền nhãn chủ hộ đã che bớt từ kho định danh (lỗi kho không chặn đọc). Gọi NGOÀI giao dịch.
func (s *Service) ownerLabels(ctx context.Context, infos map[uuid.UUID]*ModelInfo) {
	if s.Vault == nil {
		return
	}
	seen := map[uuid.UUID]string{}
	for _, m := range infos {
		if m == nil || m.PersonRef == uuid.Nil {
			continue
		}
		label, ok := seen[m.PersonRef]
		if !ok {
			if d, err := s.Vault.Display(ctx, client.PurposeFieldSupport, m.PersonRef); err == nil {
				label = d.Label
			}
			seen[m.PersonRef] = label
		}
		m.OwnerLabel = label
	}
}

// modelHousehold đọc hộ mô hình được chọn khi lập lịch: phải có trong sổ W11.47, trong phạm vi người gọi, còn tham gia.
func (s *Service) modelHousehold(ctx context.Context, c caller, ref uuid.UUID) (*ModelInfo, error) {
	var m map[uuid.UUID]*ModelInfo
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		m, e = loadModel(ctx, tx, []uuid.UUID{ref})
		return e
	}); err != nil {
		return nil, err
	}
	info := m[ref]
	if info == nil || !c.sees(info.CommuneCode) || (c.k.Role == knport.SessionRoleCommune && !c.k.AllowsHousehold(ref)) {
		return nil, ErrBatchInvalid.WithField("household_ref", "không có hộ mô hình (FN-10) này trong phạm vi")
	}
	if !info.Open {
		return nil, ErrBatchInvalid.WithField("household_ref", "hộ đã rút khỏi mô hình hoặc điểm, dự án đã kết thúc")
	}
	s.ownerLabels(ctx, m)
	return info, nil
}
