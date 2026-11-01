package app

// BF-CN-VACC (#168, bảng cảnh báo Mục III.5 dòng 11, quy tắc liên biểu số 7): job extension_cnts_alert quét lịch tiêm BM-07
// (extension/cntscal, chỉ đọc) — mũi quá ngày dự kiến chưa có ngày tiêm thực tế sinh cảnh báo "Mũi tiêm quá hạn" (mức Cần
// chú ý, cấu hình xử lý của loại như các cảnh báo khác) cho cán bộ theo dõi điểm của hộ mô hình; lứa theo địa bàn (không gắn
// hộ) → cán bộ xã phụ trách xã. Sinh cảnh báo phát pf.extension.cnts_alert.changed → pf/notify báo người nhận. Mỗi mũi một
// cảnh báo (chống trùng theo lứa × vaccine × lần × ngày dự kiến); ghi ngày thực tế, dời ngày dự kiến hoặc hủy lịch → tự đóng.
// Dời sang ngày mới rồi lại quá hạn → cảnh báo mới gắn ngày mới; cảnh báo do hệ thống tự đóng không chặn sinh lại, cảnh báo
// người dùng đóng tay thì không mở lại cho cùng ngày dự kiến (Codex #168, docs/kn/gia-dinh/BF-CN-VACC.md).
// Nông hộ (người nhận thứ hai của dòng 11) chưa có tài khoản KN liên kết hộ mô hình — chờ W11.55 (GĐ2).

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/store"
	caldomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/domain"
	calstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/store"
	mstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// Giới hạn mỗi lần chạy job cho một tenant.
const (
	vaccineBatchPage  = 2000 // lứa đọc mỗi trang (đọc HẾT các trang — Codex #168)
	vaccineRaiseLimit = 500  // cảnh báo mới
	vaccineOpenLimit  = 2000 // cảnh báo đang mở xét tự đóng
	vaccineMaxGen     = 50   // số lần sinh lại tối đa của một mũi × ngày dự kiến (phòng thủ vòng lặp)
)

// doseScope là phạm vi chống trùng của mũi d trong lứa batch: lứa × vaccine × lần × NGÀY DỰ KIẾN hiện hành (Codex #168 —
// dời ngày dự kiến là vi phạm mới). gen > 0: lần sinh lại sau khi cảnh báo trước của cùng ngày dự kiến do hệ thống tự đóng.
func doseScope(batch uuid.UUID, d caldomain.Dose, gen int) string {
	s := "dose:" + batch.String() + ":" + d.Key() + ":" + d.Expected.Format(time.DateOnly)
	if gen > 0 {
		s += ":r" + strconv.Itoa(gen+1)
	}
	return s
}

// doseBase bỏ hậu tố lần sinh lại ":rN" của khóa (khóa gốc kết thúc bằng ngày YYYY-MM-DD nên không nhầm).
func doseBase(key string) string {
	if i := strings.LastIndex(key, ":r"); i > 0 {
		if _, err := strconv.Atoi(key[i+2:]); err == nil {
			return key[:i]
		}
	}
	return key
}

// vnDay là ngày lịch Việt Nam của t, dạng UTC 00:00 (như cntscal).
func vnDay(t time.Time) time.Time {
	v := t.In(clock.Zone())
	return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
}

// VaccineOverdue sinh cảnh báo dòng 11 cho mũi quá hạn và tự đóng cảnh báo của mũi đã tiêm/dời/hủy. Trả số cảnh báo mới.
func (s *Service) VaccineOverdue(ctx context.Context, tid uuid.UUID, now time.Time) (int, error) {
	n := 0
	today := vnDay(now)
	ctx = txdeps.With(ctx) // tỉnh của xã, cán bộ xã nhận cảnh báo đọc ngoài giao dịch (P0-POOL-KN-2)
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		n = 0                        // thân giao dịch có thể chạy lại (txdeps)
		overdue := map[string]bool{} // khóa chống trùng còn vi phạm
		var after *calstore.BatchCursor
		for page := true; page; {
			batches, err := calstore.ListBatches(ctx, tx, calstore.BatchFilter{All: true, Communes: []string{}, After: after,
				Limit: vaccineBatchPage})
			if err != nil {
				return err
			}
			if page = len(batches) == vaccineBatchPage; page {
				after = calstore.CursorOf(batches[len(batches)-1])
			}
			if err := s.vaccinePage(ctx, tx, tid, batches, today, now, overdue, &n); err != nil {
				return err
			}
		}
		open, err := store.OpenByKind(ctx, tx, domain.KindVaccineOverdue, vaccineOpenLimit)
		if err != nil {
			return err
		}
		for i := range open {
			a := &open[i]
			if overdue[a.DedupKey] || !strings.Contains(a.DedupKey, ":dose:") {
				continue
			}
			// Phòng thủ: xác minh lại lịch nguồn trước khi tự đóng — mũi còn quá hạn thì không đóng.
			if still, err := stillOverdue(ctx, tx, a.SourceRef, a.DedupKey, today); err != nil || still {
				if err != nil {
					return err
				}
				continue
			}
			if err := s.closeResolved(ctx, tx, tid, a.DedupKey, now); err != nil {
				return err
			}
		}
		return nil
	})
	if dup(err) {
		return 0, nil
	}
	return n, err
}

// vaccineKey là khóa chống trùng cảnh báo lần sinh gen của mũi d trong lứa b.
func vaccineKey(b calstore.Batch, d caldomain.Dose, gen int) (key, subject, kind string) {
	subject, kind = b.CommuneCode, "commune"
	if b.HouseholdRef != nil {
		subject, kind = b.HouseholdRef.String(), "household"
	}
	return domain.DedupKey(domain.KindVaccineOverdue, subject, doseScope(b.Ref, d, gen)), subject, kind
}

// vaccineSlot tìm lần sinh của cảnh báo mũi d (ngày dự kiến hiện hành) đang quá hạn:
//   - cảnh báo chưa đóng → (gen, false): giữ, không sinh thêm (job chạy lặp không trùng);
//   - chưa có → (gen, true): sinh mới;
//   - người dùng đóng tay → (-1, false): KHÔNG mở lại cho cùng ngày dự kiến;
//   - hệ thống tự đóng (đã tiêm rồi bỏ ngày thực tế, dời đi rồi dời về) → xét lần sinh kế; cảnh báo cũ giữ nguyên lịch sử.
func vaccineSlot(ctx context.Context, tx pgx.Tx, b calstore.Batch, d caldomain.Dose) (int, bool, error) {
	for gen := 0; gen < vaccineMaxGen; gen++ {
		key, _, _ := vaccineKey(b, d, gen)
		cur, err := store.ByDedup(ctx, tx, key)
		switch {
		case err != nil:
			return 0, false, err
		case cur == nil:
			return gen, true, nil
		case cur.Status != domain.StatusClosed:
			return gen, false, nil
		case !cur.AutoClosed:
			return -1, false, nil
		}
	}
	return -1, false, nil
}

// stillOverdue: lịch nguồn src (bản hiện hành) còn mũi quá hạn mang khóa key (bỏ qua lần sinh).
func stillOverdue(ctx context.Context, tx pgx.Tx, src *uuid.UUID, key string, today time.Time) (bool, error) {
	if src == nil {
		return false, nil
	}
	b, err := calstore.LatestBatch(ctx, tx, *src)
	if err != nil || b == nil || b.Event == caldomain.BatchCancelled {
		return false, err
	}
	base := doseBase(key)
	for _, d := range b.Doses {
		if k, _, _ := vaccineKey(*b, d, 0); k == base && domain.VaccineOverdue(d.Expected, d.Actual, today) {
			return true, nil
		}
	}
	return false, nil
}

// vaccinePage sinh cảnh báo cho mũi quá hạn của một trang lứa; ghi khóa còn vi phạm vào overdue.
func (s *Service) vaccinePage(ctx context.Context, tx pgx.Tx, tid uuid.UUID, batches []calstore.Batch, today, now time.Time,
	overdue map[string]bool, n *int) error {
	for _, b := range batches {
		if b.Event == caldomain.BatchCancelled {
			continue
		}
		for _, d := range b.Doses {
			if !domain.VaccineOverdue(d.Expected, d.Actual, today) {
				continue
			}
			gen, fresh, err := vaccineSlot(ctx, tx, b, d)
			if err != nil {
				return err
			}
			if gen < 0 {
				continue // người dùng đã đóng tay cảnh báo của mũi × ngày dự kiến này
			}
			key, subject, kind := vaccineKey(b, d, gen)
			overdue[key] = true
			if !fresh || *n >= vaccineRaiseLimit {
				continue
			}
			recips, site, err := s.vaccineRecipients(ctx, tx, tid, b, now)
			if err != nil {
				return err
			}
			src := b.Ref
			where := b.Locality
			if site != "" {
				where = site
			}
			title := fmt.Sprintf("Mũi tiêm quá hạn · %s lần %d · dự kiến %s", d.Vaccine, d.Shot, d.Expected.Format("02/01/2006"))
			if where != "" {
				title += " · " + where
			}
			_, created, err := s.raise(ctx, tx, tid, Raise{Kind: domain.KindVaccineOverdue, Subject: subject, SubjectKind: kind,
				Scope: doseScope(b.Ref, d, gen), CommuneCode: b.CommuneCode, ProvinceCode: s.provinceOf(ctx, b.CommuneCode, now), TemplateCode: "BM-07",
				SourceRef: &src, Title: title, Recipients: recips, At: now,
				Extra: map[string]any{"batch_ref": b.Ref.String(), "vaccine_code": d.Vaccine, "shot": d.Shot,
					"expected_date": d.Expected.Format(time.DateOnly), "summary": "Quá ngày tiêm dự kiến chưa có ngày tiêm thực tế"}})
			if err != nil {
				return err
			}
			if created {
				*n++
			}
		}
	}
	return nil
}

// vaccineRecipients: cán bộ theo dõi điểm của hộ mô hình (sổ W11.47); không có → cán bộ Cấp Xã phụ trách xã của lứa.
func (s *Service) vaccineRecipients(ctx context.Context, tx pgx.Tx, tid uuid.UUID, b calstore.Batch, now time.Time) ([]uuid.UUID, string, error) {
	site := ""
	if b.HouseholdRef != nil {
		h, err := mstore.LatestHousehold(ctx, tx, *b.HouseholdRef)
		if err != nil {
			return nil, "", err
		}
		if h != nil {
			x, err := mstore.LatestSite(ctx, tx, h.SiteRef)
			if err != nil {
				return nil, "", err
			}
			if x != nil {
				site = x.Name
				if x.OfficerRef != nil {
					return []uuid.UUID{*x.OfficerRef}, site, nil
				}
			}
		}
	}
	rs := &resolver{s: s, tid: tid, province: s.provinceOf(ctx, b.CommuneCode, now)}
	return rs.resolve(ctx, []string{domain.RecipCommune}, b.CommuneCode, uuid.Nil), site, nil
}

// closeResolved tự đóng cảnh báo của khóa key (mũi đã tiêm, dời ngày dự kiến hoặc lịch đã hủy).
func (s *Service) closeResolved(ctx context.Context, tx pgx.Tx, tid uuid.UUID, key string, at time.Time) error {
	if err := store.Lock(ctx, tx, key); err != nil {
		return err
	}
	a, err := store.ByDedup(ctx, tx, key)
	if err != nil || a == nil || a.Status == domain.StatusClosed {
		return err
	}
	_, err = s.apply(ctx, tx, tid, a, domain.ActionAutoClosed, applyInput{Note: "Mũi đã có ngày tiêm thực tế, đã dời ngày dự kiến hoặc lịch đã hủy — hệ thống tự đóng",
		Actor: SystemActor, At: at, Idem: "auto:" + key + ":" + itoa(a.LastSeq)})
	return err
}
