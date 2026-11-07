package app

// CNTS-V4-OUTBREAK-MULTI (#252, ADR-0056 mục 7, BRD CNTS v4.0 FN-11 BR-03, UR-XA-10 DB-04/DB-14/DB-15, FN-10 BR-07, BR-08):
// hộ chăn nuôi bị ảnh hưởng của một phiên bản ổ dịch. Hộ chọn phải có trong danh sách hộ chăn nuôi (#251), thuộc xã của
// ổ dịch, trạng thái Đang dùng hoặc Chờ bổ sung vị trí (hộ vô hiệu hóa không chọn được — BR-07). Hộ chưa có được nhập
// ngay ở BM-10 và tạo trong danh sách ở trạng thái "Chờ bổ sung vị trí" (BR-08, origin_outbreak_ref), cùng giao dịch với
// phiên bản ổ dịch. DB-14 chụp loại chăn nuôi của phiên bản hộ lúc ghi.

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	lhdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/domain"
	lhstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/store"
	exdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// NewHousehold là hộ chưa có trong danh sách, nhập tại DB-04 (FN-10 BR-08): họ tên chủ hộ (HCN-02), SĐT (HCN-03, tùy
// chọn), ấp (HCN-04), vật nuôi (HCN-05), loại chăn nuôi DB-14 do cán bộ chọn (HCN-06). Xã = xã của ổ dịch.
type NewHousehold struct {
	OwnerName  string
	OwnerPhone string
	lhdomain.Fields
}

// maxOwnerLabels là số lời gọi kho Display tối đa mỗi lần đọc chi tiết/Word (như danh sách hộ chăn nuôi).
const maxOwnerLabels = 300

func newField(i int, f string) string { return "new_households[" + strconv.Itoa(i) + "]." + f }

func selField(i int) string { return "households[" + strconv.Itoa(i) + "]" }

// checkNew kiểm hộ nhập mới (chưa gọi kho): HCN-02 bắt buộc ≤ 200 ký tự, HCN-03 chỉ chữ số, ấp không chứa SĐT/CCCD,
// loại chăn nuôi, vật nuôi thuộc danh mục FN-06 đang dùng tại ngày on. Gọi NGOÀI giao dịch.
func (s *Service) checkNew(ctx context.Context, commune string, hs []NewHousehold, on time.Time) error {
	p, bad := ErrOutbreakInvalid, false
	for i := range hs {
		h := &hs[i]
		h.OwnerName = lhdomain.NormalizeOwnerName(h.OwnerName)
		h.OwnerPhone = strings.TrimSpace(h.OwnerPhone)
		h.CommuneCode = commune
		if !lhdomain.ValidOwnerName(h.OwnerName) {
			p, bad = p.WithField(newField(i, lhdomain.FieldOwnerName), "bắt buộc, tối đa 200 ký tự (HCN-02)"), true
		}
		if h.OwnerPhone != "" && !lhdomain.ValidPhoneDigits(h.OwnerPhone) {
			p, bad = p.WithField(newField(i, lhdomain.FieldPhone), "chỉ nhập chữ số (HCN-03)"), true
		}
		for _, e := range h.Normalize() {
			p, bad = p.WithField(newField(i, e.Field), e.Reason), true
		}
		if exdomain.ContainsPII(h.Hamlet) {
			p, bad = p.WithField(newField(i, lhdomain.FieldHamlet), exdomain.ErrPIIInText.Error()), true
		}
	}
	if bad {
		return p
	}
	for i, h := range hs {
		for j, a := range h.Animals {
			it, err := s.Catalogs.ItemAt(ctx, knport.CatalogCNTSSpecies, a.Species, on)
			switch {
			case errors.Is(err, mdport.ErrCatalogItemNotFound), errors.Is(err, mdport.ErrNotEffective), err == nil && !it.Active:
				return ErrOutbreakInvalid.WithField(newField(i, lhdomain.FieldAnimals+"["+strconv.Itoa(j)+"].species_code"),
					"không có trong danh mục vật nuôi (FN-06)")
			case err != nil:
				return err
			}
		}
	}
	return nil
}

// registerNew ghi họ tên, SĐT chủ hộ nhập mới vào kho định danh (mục đích extension.contact) — NGOÀI giao dịch DB. Trùng
// SĐT → dùng chủ thể đã có (như danh sách hộ chăn nuôi). Giao dịch hỏng sau đó để lại chủ thể mồ côi (ADR-0056 mục 3).
func (s *Service) registerNew(ctx context.Context, hs []NewHousehold) ([]uuid.UUID, error) {
	if len(hs) == 0 {
		return nil, nil
	}
	if s.Vault == nil {
		return nil, ErrDependency.WithDetail("chưa nối kho định danh — không nhập được hộ mới")
	}
	out := make([]uuid.UUID, len(hs))
	for i, h := range hs {
		cr, err := s.Vault.Create(ctx, client.PurposeContact, client.Identity{Kind: client.SubjectHousehold, FullName: h.OwnerName,
			Phone: h.OwnerPhone, AreaCode: h.CommuneCode})
		if err != nil && (!errors.Is(err, client.ErrDuplicate) || cr.Ref == uuid.Nil) {
			return nil, err
		}
		out[i] = cr.Ref
	}
	return out, nil
}

// attach dựng DB-04 của phiên bản ổ dịch trong giao dịch ghi: khóa và kiểm hộ chọn (theo thứ tự mã để không khóa chéo),
// rồi tạo hộ nhập mới ở trạng thái Chờ bổ sung vị trí (mã HCN cấp từ bộ đếm, origin_outbreak_ref). Hộ chọn đứng trước,
// hộ mới sau, theo thứ tự nhập.
func (s *Service) attach(ctx context.Context, tx pgx.Tx, c caller, obRef uuid.UUID, commune string, sel []uuid.UUID,
	hs []NewHousehold, persons []uuid.UUID, idem string) ([]store.HouseholdLine, error) {
	order := slices.Clone(sel)
	slices.SortFunc(order, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, r := range order {
		if err := lhstore.Lock(ctx, tx, r); err != nil {
			return nil, err
		}
	}
	lines := make([]store.HouseholdLine, 0, len(sel)+len(hs))
	for i, r := range sel {
		h, err := lhstore.Latest(ctx, tx, r)
		switch {
		case err != nil:
			return nil, err
		case h == nil:
			return nil, ErrOutbreakInvalid.WithField(selField(i), "không có trong danh sách hộ chăn nuôi")
		case h.Status == lhdomain.StatusDeactivated:
			return nil, ErrOutbreakInvalid.WithField(selField(i), "hộ "+h.Code+" đã vô hiệu hóa (FN-10 BR-07)")
		case h.CommuneCode != commune:
			return nil, ErrOutbreakInvalid.WithField(selField(i), "hộ "+h.Code+" thuộc xã khác xã của ổ dịch")
		}
		lines = append(lines, store.HouseholdLine{HouseholdRef: h.Ref, HouseholdSeq: h.Seq, LivestockType: h.LivestockType,
			Code: h.Code, Hamlet: h.Hamlet, PersonRef: h.PersonRef, Status: h.Status})
	}
	m := c.meta("", s.now(), idem)
	for i, n := range hs {
		code, err := lhstore.NextCode(ctx, tx, c.p.TenantID)
		if err != nil {
			return nil, err
		}
		ref := ids.New()
		if err := lhstore.InsertKey(ctx, tx, c.p.TenantID, ref, code); err != nil {
			return nil, err
		}
		origin := obRef
		h := lhstore.Household{
			Meta: lhstore.Meta{ID: ids.New(), EventTime: m.EventTime, ActorRef: m.ActorRef, OnBehalfOf: m.OnBehalfOf,
				SupportReason: m.SupportReason, IdemKey: "lhh:ob:" + idem + ":" + strconv.Itoa(i)},
			Ref: ref, Code: code, Event: lhdomain.EventCreatedPending, Status: lhdomain.StatusPending, PersonRef: persons[i],
			Fields: n.Fields, OriginOutbreakRef: &origin,
		}
		if err := lhstore.Insert(ctx, tx, c.p.TenantID, &h); err != nil {
			return nil, err
		}
		lines = append(lines, store.HouseholdLine{HouseholdRef: ref, HouseholdSeq: h.Seq, LivestockType: h.LivestockType,
			Code: code, Hamlet: h.Hamlet, PersonRef: h.PersonRef, Status: h.Status})
	}
	return lines, nil
}

// HouseholdView là một hộ DB-04 của phiên bản ổ dịch kèm nhãn chủ hộ che bớt (chỉ ở chi tiết, Word).
type HouseholdView struct {
	store.HouseholdLine
	OwnerLabel string
}

// ownerLabels tra nhãn che bớt (Display) của chủ hộ — tối đa maxOwnerLabels chủ thể; lỗi tra cứu không chặn đọc.
func (s *Service) ownerLabels(ctx context.Context, vs []OutbreakView) {
	if s.Vault == nil {
		return
	}
	labels := map[uuid.UUID]string{}
	for i := range vs {
		for j := range vs[i].Households {
			h := &vs[i].Households[j]
			l, ok := labels[h.PersonRef]
			if !ok && len(labels) < maxOwnerLabels {
				if d, err := s.Vault.Display(ctx, client.PurposeContact, h.PersonRef); err == nil {
					l = d.Label
				}
				labels[h.PersonRef] = l
			}
			h.OwnerLabel = l
		}
	}
}
