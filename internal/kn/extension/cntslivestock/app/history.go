package app

// Lịch sử thay đổi HCN-09 (BR-05, AC-08): người sửa, thời điểm, giá trị cũ → mới. Trường không phải dữ liệu cá nhân lấy
// từ chuỗi phiên bản của sổ (cũ = phiên bản trước); họ tên/SĐT lấy nhãn che bớt của bản sửa ở kho định danh (ADR-0056
// mục 4 — lệch BRD một dòng: hiện dạng che bớt, không nguyên văn).

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

func (s *Service) history(ctx context.Context, vs []store.Household, cks map[uuid.UUID]store.Checkin, species map[string]string) []Change {
	revs := s.revisions(ctx, vs)
	communes := map[string]string{}
	commune := func(code string) string {
		if n, ok := communes[code]; ok {
			return n
		}
		n := code
		if s.Units != nil {
			if u, err := s.Units.Get(ctx, code, s.today()); err == nil && u.Name != "" {
				n = u.Name + " (" + code + ")"
			}
		}
		communes[code] = n
		return n
	}
	out := make([]Change, 0, len(vs))
	for i, v := range vs {
		ch := Change{Seq: v.Seq, Event: v.Event, ActorRef: v.ActorRef, At: v.EventTime, Reason: v.Reason}
		if i > 0 {
			p := vs[i-1]
			for _, f := range v.ChangedFields {
				item := ChangeItem{Field: f}
				switch f {
				case domain.FieldCommune:
					item.Old, item.New = commune(p.CommuneCode), commune(v.CommuneCode)
				case domain.FieldHamlet:
					item.Old, item.New = p.Hamlet, v.Hamlet
				case domain.FieldAnimals:
					item.Old, item.New = animals(p.Animals, species), animals(v.Animals, species)
				case domain.FieldType:
					item.Old, item.New = domain.TypeLabels[p.LivestockType], domain.TypeLabels[v.LivestockType]
				case domain.FieldStatus:
					item.Old, item.New = domain.StatusLabels[p.Status], domain.StatusLabels[v.Status]
				case domain.FieldLocation:
					item.Old, item.New = location(p.CheckinRef, cks), location(v.CheckinRef, cks)
				case domain.FieldOwnerName, domain.FieldPhone:
					item.Old, item.New = client.ErasedLabel, client.ErasedLabel
					if v.PIIRevisionRef != nil {
						if r, ok := revs[*v.PIIRevisionRef]; ok {
							item.Old, item.New = r.PrevLabel, r.NextLabel
							if f == domain.FieldPhone {
								item.Old, item.New = r.PrevPhone, r.NextPhone
							}
						}
					}
				}
				ch.Items = append(ch.Items, item)
			}
		}
		out = append(out, ch)
	}
	return out
}

// revisions đọc nhãn che bớt của các bản sửa họ tên/SĐT; lỗi kho không chặn đọc (dòng lịch sử hiện "(đã xóa định danh)").
func (s *Service) revisions(ctx context.Context, vs []store.Household) map[uuid.UUID]client.Revision {
	out := map[uuid.UUID]client.Revision{}
	refs := []uuid.UUID{}
	for _, v := range vs {
		if v.PIIRevisionRef != nil {
			refs = append(refs, *v.PIIRevisionRef)
		}
	}
	rs, ok := client.AsRevisions(s.Vault)
	if len(refs) == 0 || !ok {
		return out
	}
	if len(refs) > client.MaxRevisionRefs {
		refs = refs[len(refs)-client.MaxRevisionRefs:]
	}
	got, err := rs.Revisions(ctx, client.PurposeContact, refs)
	if err != nil {
		return out
	}
	for _, r := range got {
		out[r.Ref] = r
	}
	return out
}

func animals(as []domain.Animal, names map[string]string) string {
	parts := make([]string, 0, len(as))
	for _, a := range as {
		n := names[a.Species]
		if n == "" {
			n = a.Species
		}
		parts = append(parts, fmt.Sprintf("%s: %d", n, a.Heads))
	}
	return strings.Join(parts, "; ")
}

func location(ref *uuid.UUID, cks map[uuid.UUID]store.Checkin) string {
	if ref == nil {
		return "Chưa có vị trí"
	}
	c, ok := cks[*ref]
	if !ok {
		return "(vị trí đã xóa)"
	}
	return fmt.Sprintf("%.6f, %.6f (±%s m)", c.Lat, c.Lon, c.AccuracyM.String())
}
