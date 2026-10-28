package app

// W11.25 — thao tác ghi của nâng bậc: đưa vào / bỏ khỏi danh sách (FN-09 B4, luồng ngoại lệ "thêm người Chưa đủ → bắt
// buộc lý do"), lập danh sách NLTH từ thành tích (bước 3 To-Be: quy đổi số tháng, kiểm tỷ lệ chỉ tiêu), xuất Mẫu số
// 1/2/4 (hệ thống tự chia theo loại, chặn Mẫu số 2), tải lại tệp (dựng lại + SHA-256).
// Mọi lần ghi kiểm lại trạng thái trong CHÍNH giao dịch ghi (nạp lại thế giới).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

const maxCaseKeys = 500

func cleanKeys(keys []string) ([]string, error) {
	if len(keys) == 0 || len(keys) > maxCaseKeys {
		return nil, problem.Validation.WithField("keys", fmt.Sprintf("chọn 1–%d kỳ nâng bậc", maxCaseKeys))
	}
	seen := map[string]bool{}
	var out []string
	for _, k := range keys {
		if _, _, ok := domain.ParseCaseKey(k); !ok {
			return nil, port.ErrCaseNotFound.WithDetail("khóa kỳ " + k)
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out, nil
}

func checkReason(r string, need bool) (string, error) {
	r = strings.TrimSpace(r)
	if need && r == "" {
		return "", problem.Validation.WithField("reason", "thêm người chưa đủ điều kiện hoặc cần xem xét phải có lý do (FN-09)")
	}
	if utf8.RuneCountInString(r) > 1000 {
		return "", problem.Validation.WithField("reason", "tối đa 1000 ký tự")
	}
	return r, nil
}

// writeWorld chạy fn trong một giao dịch ghi với thế giới nạp lại tại ngày day.
func (s *Service) writeWorld(ctx context.Context, c rc, day time.Time, fn func(tx pgx.Tx, w *promoWorld) error) error {
	return s.writeWorldOpt(ctx, c, day, true, fn)
}

func (s *Service) writeWorldOpt(ctx context.Context, c rc, day time.Time, needRules bool, fn func(tx pgx.Tx, w *promoWorld) error) error {
	units, err := s.unitRefs(ctx, c)
	if err != nil {
		return err
	}
	return s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		w, err := loadPromo(ctx, tx, day, units)
		if err != nil {
			return err
		}
		if needRules && w.noRules {
			return port.ErrPromotionRules
		}
		return fn(tx, w)
	})
}

func (s *Service) promoEvent(ctx context.Context, tx pgx.Tx, c rc, e store.PromoEvent, idem string) error {
	e.ID, e.Actor = ids.New(), c.ref
	_, err := store.InsertPromoEvent(ctx, tx, c.tenant, e, s.Clock.Now(), idem)
	return classify(err)
}

func dayPtr(t time.Time) *time.Time { return &t }

// nlthOpen: hồ sơ có kỳ NLTH đang trong danh sách/đã xuất (chưa có quyết định) — chặn đưa vào Mẫu số 1 cùng lần nâng.
func (w *promoWorld) nlthOpen(staff uuid.UUID) bool {
	for _, c := range w.cands {
		if c.StaffID == staff && c.Kind == domain.PromoNLTH {
			if st, _ := w.status(c); st == domain.PromoListed || st == domain.PromoExported {
				return true
			}
		}
	}
	return false
}

// IncludeCases đưa các kỳ NLTX/TNVK vào danh sách ("Đã đưa vào danh sách"); kỳ "Chưa đủ"/"Cần xem xét" bắt buộc lý do.
func (s *Service) IncludeCases(ctx context.Context, keys []string, reason, idem string) (int, error) {
	c, err := s.promoCaller(ctx, true)
	if err != nil {
		return 0, err
	}
	keys, err = cleanKeys(keys)
	if err != nil {
		return 0, err
	}
	n := 0
	err = s.writeWorld(ctx, c, s.day(time.Time{}), func(tx pgx.Tx, w *promoWorld) error {
		for _, k := range keys {
			x, ok := w.byKey[k]
			if !ok {
				return port.ErrCaseNotFound.WithDetail(k)
			}
			if x.Kind == domain.PromoNLTH {
				return port.ErrPromotionConflict.WithDetail("kỳ trước hạn được lập từ thành tích (proposals), không đưa vào bằng thao tác này")
			}
			st, _ := w.status(x)
			if st != domain.PromoPending {
				continue
			}
			if x.Kind == domain.PromoNLTX && w.nlthOpen(x.StaffID) {
				return port.ErrPromotionConflict.WithDetail("người đã có trong Mẫu số 2 (nâng bậc trước hạn) cùng lần nâng — không đưa vào Mẫu số 1")
			}
			r, err := checkReason(reason, x.Cond != domain.CondOK)
			if err != nil {
				return err
			}
			if x.Cond == domain.CondOK {
				r = ""
			}
			if err := s.promoEvent(ctx, tx, c, store.PromoEvent{StaffID: x.StaffID, CaseKey: k, Kind: string(x.Kind), Action: domain.PromoActInclude,
				DueOn: dayPtr(x.Due), Reason: r}, stableKey(idem, "promo:include:"+k)); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// ExcludeCases bỏ các kỳ NLTX/TNVK khỏi danh sách (về "Chưa xử lý"); kỳ đã xuất/có quyết định không bỏ được.
func (s *Service) ExcludeCases(ctx context.Context, keys []string, idem string) (int, error) {
	c, err := s.promoCaller(ctx, true)
	if err != nil {
		return 0, err
	}
	keys, err = cleanKeys(keys)
	if err != nil {
		return 0, err
	}
	n := 0
	err = s.writeWorld(ctx, c, s.day(time.Time{}), func(tx pgx.Tx, w *promoWorld) error {
		for _, k := range keys {
			x, ok := w.byKey[k]
			if !ok {
				return port.ErrCaseNotFound.WithDetail(k)
			}
			st, _ := w.status(x)
			if x.Kind == domain.PromoNLTH || st != domain.PromoListed {
				return port.ErrPromotionConflict.WithDetail("chỉ bỏ được kỳ thường xuyên/vượt khung đang ở trạng thái Đã đưa vào danh sách")
			}
			if err := s.promoEvent(ctx, tx, c, store.PromoEvent{StaffID: x.StaffID, CaseKey: k, Kind: string(x.Kind), Action: domain.PromoActExclude,
				DueOn: dayPtr(x.Due)}, stableKey(idem, "promo:exclude:"+k)); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// --- NLTH. ---

// PoolOption là một thành tích chọn được cho NLTH.
type PoolOption struct {
	Award     domain.PromoAward
	Candidate domain.Candidate
}

// PoolRow là một viên chức trong danh sách chọn NLTH.
type PoolRow struct {
	StaffID   uuid.UUID
	Code      string
	Name      string
	UnitName  string
	Step      int
	Steps     int
	NormalDue *time.Time
	Block     string
	Options   []PoolOption
}

// NLTHPool là danh sách chọn NLTH (FN-09 bước 3): viên chức đang công tác, trừ người đang trong danh sách đến kỳ thường
// xuyên trong window tháng (chưa có quyết định) và người đã có kỳ NLTH đang mở.
type NLTHPool struct {
	Day                     time.Time
	QuotaYear, Quota        int
	QuotaUsed, QuotaPending int
	Excluded                int
	Rows                    []PoolRow
}

// PromotionPool trả danh sách chọn NLTH.
func (s *Service) PromotionPool(ctx context.Context, window int) (NLTHPool, error) {
	c, err := s.promoCaller(ctx, false)
	if err != nil {
		return NLTHPool{}, err
	}
	day := s.day(time.Time{})
	w, _, err := s.world(ctx, c, day)
	if err != nil {
		return NLTHPool{}, err
	}
	end := domain.AddMonths(day, validWindow(window))
	out := NLTHPool{Day: day, QuotaYear: w.quotaYear, Quota: w.quota, QuotaUsed: w.used, QuotaPending: w.pendingNLTH()}
	due := map[uuid.UUID]bool{}
	for _, x := range w.cands {
		if x.Kind == domain.PromoNLTX && !x.Due.After(end) {
			if st, _ := w.status(x); st != domain.PromoDecided {
				due[x.StaffID] = true
			}
		}
	}
	used := w.usedAwards()
	var staff []uuid.UUID
	for _, h := range w.order {
		sub, ok := w.subject(h, used)
		if !ok {
			continue
		}
		if due[h.StaffID] {
			out.Excluded++
			continue
		}
		if w.nlthOpen(h.StaffID) {
			continue
		}
		row := PoolRow{StaffID: h.StaffID, Code: h.Code, UnitName: w.cat.UnitName(sub.Snap.Unit), Step: sub.Snap.Step, Steps: len(sub.Coefficients)}
		staff = append(staff, h.StaffID)
		switch {
		case sub.Snap.Grade == "" || sub.Snap.Step < 1 || len(sub.Coefficients) == 0:
			row.Block = "Chưa có ngạch, bậc hoặc bảng hệ số"
		case sub.Snap.Step >= len(sub.Coefficients):
			row.Block = "Đang ở bậc cuối (xét thâm niên vượt khung)"
		case len(sub.Awards) == 0:
			row.Block = "Chưa có thành tích ghi trong hồ sơ"
		default:
			sub.Proposals = nil
			for _, a := range sub.Awards {
				if a.Used || a.Year < w.quotaYear-w.rules.AchievementYears {
					continue
				}
				sub.Proposals = append(sub.Proposals, a.EventID)
			}
			for _, x := range domain.Compute(sub, w.rules, w.quotaYear) {
				switch x.Kind {
				case domain.PromoNLTX:
					d := x.Due
					row.NormalDue = &d
				case domain.PromoNLTH:
					row.Options = append(row.Options, PoolOption{Award: *x.Award, Candidate: x})
				}
			}
			if len(row.Options) == 0 {
				row.Block = "Thành tích đã dùng hoặc quá số năm xét"
			}
		}
		out.Rows = append(out.Rows, row)
	}
	names, err := s.names(ctx, w, staff)
	if err != nil {
		return NLTHPool{}, err
	}
	for i := range out.Rows {
		out.Rows[i].Name = names[out.Rows[i].StaffID]
	}
	return out, nil
}

// Proposal là một đề xuất NLTH: viên chức + thành tích.
type Proposal struct {
	StaffID      uuid.UUID
	AwardEventID uuid.UUID
}

// ProposeNLTH đưa người vào danh sách NLTH từ thành tích (quy đổi số tháng, kiểm điều kiện, kiểm tỷ lệ chỉ tiêu của năm
// — ngày nâng ở năm sau lập dạng "dự kiến", không tính chỉ tiêu năm nay). Người "Chưa đủ"/"Cần xem xét" bắt buộc lý do.
func (s *Service) ProposeNLTH(ctx context.Context, ps []Proposal, reason, idem string) ([]string, error) {
	c, err := s.promoCaller(ctx, true)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 || len(ps) > maxCaseKeys {
		return nil, problem.Validation.WithField("items", "chọn 1–500 người")
	}
	var keys []string
	err = s.writeWorld(ctx, c, s.day(time.Time{}), func(tx pgx.Tx, w *promoWorld) error {
		used := w.usedAwards()
		thisYear := 0
		seen := map[uuid.UUID]bool{}
		type planned struct {
			x domain.Candidate
		}
		var todo []planned
		for _, p := range ps {
			if seen[p.StaffID] {
				return problem.Validation.WithField("items", "mỗi người một thành tích trong một lần lập")
			}
			seen[p.StaffID] = true
			h, ok := w.heads[p.StaffID]
			if !ok {
				return port.ErrStaffNotFound
			}
			if w.nlthOpen(p.StaffID) {
				return port.ErrPromotionConflict.WithDetail(h.Code + ": đã có trong danh sách nâng bậc trước hạn")
			}
			sub, ok := w.subject(h, used)
			if !ok {
				return problem.Validation.WithField("items", h.Code+": chỉ viên chức đang công tác")
			}
			sub.Proposals = []uuid.UUID{p.AwardEventID}
			var x *domain.Candidate
			for _, y := range domain.Compute(sub, w.rules, w.quotaYear) {
				if y.Kind == domain.PromoNLTH {
					v := y
					x = &v
				}
			}
			if x == nil {
				return port.ErrPromotionConflict.WithDetail(h.Code + ": không lập được NLTH (bậc cuối, thiếu mốc xếp bậc hoặc thành tích không có trong hồ sơ)")
			}
			for _, b := range x.Blocked {
				if !strings.HasPrefix(b, "ngày nâng") { // ngày ngoài năm chỉ tiêu → lập "dự kiến" được
					return port.ErrPromotionBlocked.WithDetail(h.Code + ": " + b)
				}
			}
			if _, err := checkReason(reason, x.Cond != domain.CondOK); err != nil {
				return err
			}
			if x.Planned == 0 {
				thisYear++
			}
			todo = append(todo, planned{*x})
		}
		if total := w.used + w.pendingNLTH() + thisYear; thisYear > 0 && total > w.quota {
			return port.ErrNLTHQuota.WithDetail(fmt.Sprintf("năm %d: %d/%d người", w.quotaYear, total, w.quota))
		}
		r, _ := checkReason(reason, false)
		for _, p := range todo {
			x := p.x
			m := x.EarlyMonths
			rr := ""
			if x.Cond != domain.CondOK {
				rr = r
			}
			if err := s.promoEvent(ctx, tx, c, store.PromoEvent{StaffID: x.StaffID, CaseKey: x.Key, Kind: string(domain.PromoNLTH),
				Action: domain.PromoActPropose, DueOn: dayPtr(x.Due), AwardEvent: &x.Award.EventID, Months: &m, Reason: rr},
				stableKey(idem, "promo:propose:"+x.Key)); err != nil {
				return err
			}
			keys = append(keys, x.Key)
		}
		return nil
	})
	return keys, err
}

// WithdrawNLTH gỡ người khỏi danh sách NLTH (thành tích vẫn giữ, chưa tính là đã dùng).
func (s *Service) WithdrawNLTH(ctx context.Context, keys []string, idem string) (int, error) {
	c, err := s.promoCaller(ctx, true)
	if err != nil {
		return 0, err
	}
	keys, err = cleanKeys(keys)
	if err != nil {
		return 0, err
	}
	n := 0
	err = s.writeWorld(ctx, c, s.day(time.Time{}), func(tx pgx.Tx, w *promoWorld) error {
		for _, k := range keys {
			cs, ok := w.cases[k]
			if !ok || cs.Kind != domain.PromoNLTH || cs.Gone {
				return port.ErrCaseNotFound.WithDetail(k)
			}
			if cs.Status != domain.PromoListed {
				return port.ErrPromotionConflict.WithDetail("chỉ gỡ được kỳ trước hạn chưa xuất danh sách")
			}
			if err := s.promoEvent(ctx, tx, c, store.PromoEvent{StaffID: cs.Staff, CaseKey: k, Kind: string(domain.PromoNLTH),
				Action: domain.PromoActWithdraw, AwardEvent: cs.Award}, stableKey(idem, "promo:withdraw:"+k)); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// --- Xuất Mẫu số 1/2/4. ---

// SignerTitles là chức danh người ký hợp lệ.
var SignerTitles = []string{"Giám đốc", "Phó Giám đốc"}

// PromoExportInput là tham số xuất (FN-09 B4: tháng xét, người lập, người ký; % TNVK đang hưởng, số tháng hưởng, ghi
// chú, HS chênh lệch bảo lưu sau nâng theo từng kỳ — tùy chọn).
type PromoExportInput struct {
	Keys          []string
	ReviewMonth   time.Time
	Preparer      *uuid.UUID
	SignerTitle   string
	TNVKPct       map[string]string
	Months        map[string]string
	Notes         map[string]string
	ReservedAfter map[string]string
}

// PromoExported là một tệp đã xuất (không chứa tệp — tải bằng DownloadPromoExport).
type PromoExported struct {
	ID       uuid.UUID
	Form     string
	FileName string
	SHA256   string
	Size     int
	Count    int
}

func pct(s string) (decimal.Decimal, bool) {
	d, err := decimal.NewFromString(strings.ReplaceAll(strings.TrimSpace(s), ",", "."))
	if err != nil || d.IsNegative() || d.GreaterThan(decimal.NewFromInt(100)) || !d.Equal(d.Round(2)) {
		return d, false
	}
	return d, true
}

func coeffStr(d decimal.Decimal) string { return d.StringFixed(2) }

// exportItem dựng dòng xuất (không PII) của kỳ x với tham số in.
func (w *promoWorld) exportItem(x domain.Candidate, cs *caseState, in PromoExportInput) (domain.ExportItem, error) {
	h, sn := w.heads[x.StaffID], w.snaps[x.StaffID]
	it := domain.ExportItem{CaseKey: x.Key, StaffID: x.StaffID, Code: h.Code, Kind: x.Kind, Due: x.Due.Format(time.DateOnly),
		UnitName: w.cat.UnitName(sn.Unit), Degree: w.cat.Name(domain.KindEducationLevel, sn.TopDegree), Position: w.cat.Name(domain.KindPosition, sn.Position),
		Grade: x.Grade, GradeName: w.cat.Name(domain.KindGrade, x.Grade), FromStep: x.FromStep, ToStep: x.ToStep, FromCoeff: coeffStr(x.FromCoeff),
		ToCoeff: coeffStr(x.ToCoeff), Diff: coeffStr(x.Diff), StepSince: x.StepSince.Format(time.DateOnly), Reserved: x.Reserved,
		ReservedAfter: x.Reserved, NextDue: x.NextDue.Format(time.DateOnly), MonthsEnjoyed: domain.MonthsEnjoyed(x.Due)}
	if sn.Position == "" {
		it.Position = ""
	}
	if sn.TopDegree == "" {
		it.Degree = ""
	}
	if cs != nil {
		it.Reason = cs.Reason
	}
	if v, ok := in.Months[x.Key]; ok && v != "" {
		m, ok := domain.ValidMonthsEnjoyed(v)
		if !ok {
			return it, problem.Validation.WithField("months", "số tháng được hưởng 0–12, một chữ số thập phân")
		}
		it.MonthsEnjoyed = m
	}
	if v := strings.TrimSpace(in.Notes[x.Key]); v != "" {
		if utf8.RuneCountInString(v) > 300 {
			return it, problem.Validation.WithField("notes", "ghi chú tối đa 300 ký tự")
		}
		it.Note = v
	}
	if v, ok := in.ReservedAfter[x.Key]; ok {
		if v == "" {
			it.ReservedAfter = ""
		} else if d, ok := pct(v); ok {
			it.ReservedAfter = d.StringFixed(2)
		} else {
			return it, problem.Validation.WithField("reserved_after", "hệ số 0–100, tối đa 2 chữ số thập phân")
		}
	}
	switch x.Kind {
	case domain.PromoNLTH:
		id := x.Award.EventID
		it.AwardEventID, it.AwardLabel, it.EarlyMonths = &id, x.Award.Label(), x.EarlyMonths
		it.MonthsEnjoyed = ""
	case domain.PromoTNVK:
		from := x.PctFrom
		if v, ok := in.TNVKPct[x.Key]; ok && strings.TrimSpace(v) != "" {
			d, ok := pct(v)
			if !ok {
				return it, problem.Validation.WithField("tnvk_pct", "% TNVK đang hưởng 0–100")
			}
			from = &d
		}
		to := w.rules.TNVKStartPct
		if from != nil && from.IsPositive() {
			to = from.Add(w.rules.TNVKStepPct)
		}
		f := decimal.Zero
		if from != nil {
			f = *from
			it.PctFrom = from.String()
		}
		it.PctTo = to.String()
		it.Diff = x.FromCoeff.Mul(to.Sub(f)).Div(decimal.NewFromInt(100)).Round(2).StringFixed(2)
		if x.PctSince != nil {
			it.PctSince = x.PctSince.Format(time.DateOnly)
		}
	}
	return it, nil
}

// ExportPromotion xuất các kỳ đã chọn: tự chia theo loại thành Mẫu số 1 (NLTX), 2 (NLTH), 4 (TNVK); chặn Mẫu số 2 khi vượt
// chỉ tiêu, thành tích đã dùng, ngày nâng ngoài năm chỉ tiêu, đang ở bậc cuối (luồng ngoại lệ FN-09). Ghi sổ xuất, trạng
// thái "Đã xuất danh sách" và nhật ký xuất hồ sơ trong một giao dịch.
func (s *Service) ExportPromotion(ctx context.Context, in PromoExportInput, idem string) ([]PromoExported, error) {
	c, err := s.promoCaller(ctx, true)
	if err != nil {
		return nil, err
	}
	keys, err := cleanKeys(in.Keys)
	if err != nil {
		return nil, err
	}
	if in.ReviewMonth.IsZero() {
		return nil, problem.Validation.WithField("review_month", "bắt buộc (YYYY-MM)")
	}
	in.ReviewMonth = time.Date(in.ReviewMonth.Year(), in.ReviewMonth.Month(), 1, 0, 0, 0, 0, time.UTC)
	if in.SignerTitle == "" {
		in.SignerTitle = SignerTitles[0]
	}
	if !contains(SignerTitles, in.SignerTitle) {
		return nil, problem.Validation.WithField("signer_title", "Giám đốc | Phó Giám đốc")
	}
	day := s.day(time.Time{})
	groups := map[string][]domain.ExportItem{}
	var w0 *promoWorld
	// Dựng dòng + kiểm quy tắc trong giao dịch đọc; ghi trong giao dịch sau (sau khi đọc kho định danh dựng tệp).
	err = s.writeWorld(ctx, c, day, func(_ pgx.Tx, w *promoWorld) error {
		w0 = w
		nlth := 0
		var blocked []string
		for _, k := range keys {
			x, ok := w.byKey[k]
			if !ok {
				return port.ErrCaseNotFound.WithDetail(k)
			}
			st, cs := w.status(x)
			switch {
			case st == domain.PromoDecided:
				return port.ErrPromotionConflict.WithDetail(k + ": đã có quyết định")
			case st == domain.PromoPending && x.Cond != domain.CondOK:
				return problem.Validation.WithField("keys", w.heads[x.StaffID].Code+": chưa đủ điều kiện — đưa vào danh sách kèm lý do trước khi xuất")
			case x.Kind == domain.PromoNLTX && w.nlthOpen(x.StaffID):
				return port.ErrPromotionConflict.WithDetail(w.heads[x.StaffID].Code + ": đã có trong Mẫu số 2 cùng lần nâng — không đưa vào Mẫu số 1")
			}
			if x.Kind == domain.PromoNLTH {
				nlth++
				for _, b := range x.Blocked {
					blocked = append(blocked, w.heads[x.StaffID].Code+": "+b)
				}
				if x.FromStep >= x.Steps {
					blocked = append(blocked, w.heads[x.StaffID].Code+": đang ở bậc cuối")
				}
			}
			it, err := w.exportItem(x, cs, in)
			if err != nil {
				return err
			}
			groups[x.Kind.Form()] = append(groups[x.Kind.Form()], it)
		}
		if nlth > 0 && w.used+nlth > w.quota {
			blocked = append(blocked, fmt.Sprintf("vượt chỉ tiêu nâng bậc trước thời hạn năm %d: %d/%d", w.quotaYear, w.used+nlth, w.quota))
		}
		if len(blocked) > 0 {
			return port.ErrPromotionBlocked.WithDetail("Không xuất được Mẫu số 2: " + strings.Join(blocked, "; "))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	params := map[string]string{"signer_title": in.SignerTitle}
	if in.Preparer == nil { // người lập mặc định = hồ sơ gắn tài khoản người xuất
		for _, h := range w0.order {
			if h.AccountRef != nil && *h.AccountRef == c.ref {
				id := h.StaffID
				in.Preparer = &id
			}
		}
	}
	if in.Preparer != nil {
		if _, ok := w0.heads[*in.Preparer]; !ok {
			return nil, problem.Validation.WithField("preparer_id", "người lập phải là mã hồ sơ nhân sự")
		}
		params["preparer"] = in.Preparer.String()
	}
	forms := make([]string, 0, len(groups))
	for f := range groups {
		forms = append(forms, f)
	}
	sort.Strings(forms)
	known := s.Clock.Now().UTC()
	var out []PromoExported
	var rows []store.PromoExport
	for _, f := range forms {
		items := groups[f]
		data, name, err := s.renderPromo(ctx, w0, f, in.ReviewMonth, day, items, params, c.ref, known)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		var staff []uuid.UUID
		for _, it := range items {
			staff = append(staff, it.StaffID)
		}
		row := store.PromoExport{ID: ids.New(), Action: "export", Form: f, ReviewMonth: in.ReviewMonth, AsOf: day, KnownAt: &known, Params: params,
			Items: items, StaffIDs: staff, FileName: name, SHA256: hex.EncodeToString(sum[:]), ByteSize: int64(len(data)), Actor: c.ref}
		rows = append(rows, row)
		out = append(out, PromoExported{ID: row.ID, Form: f, FileName: name, SHA256: row.SHA256, Size: len(data), Count: len(items)})
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		for _, r := range rows {
			if _, err := store.InsertPromoExport(ctx, tx, c.tenant, r, s.Clock.Now(), stableKey(idem, "promo-export:"+r.Form)); err != nil {
				return classify(err)
			}
			for _, it := range r.Items {
				id := r.ID
				due, _ := time.Parse(time.DateOnly, it.Due)
				if err := s.promoEvent(ctx, tx, c, store.PromoEvent{StaffID: it.StaffID, CaseKey: it.CaseKey, Kind: string(it.Kind),
					Action: domain.PromoActExport, DueOn: &due, ExportID: &id, AwardEvent: it.AwardEventID},
					stableKey(idem, "promo:export:"+it.CaseKey)); err != nil {
					return err
				}
			}
			if err := store.InsertAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "export", StaffIDs: r.StaffIDs, ItemCount: len(r.StaffIDs),
				Scope: string(domain.ScopeAll), Actor: c.ref, Query: map[string]string{"promotion_form": r.Form,
					"review_month": r.ReviewMonth.Format("2006-01")}}, s.Clock.Now()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// promoPeople đọc họ tên, giới tính, năm sinh (kho định danh, theo lô) cho các dòng.
func (s *Service) promoPeople(ctx context.Context, w *promoWorld, staff []uuid.UUID) (map[uuid.UUID]domain.PromoPerson, error) {
	var refs []uuid.UUID
	by := map[uuid.UUID]uuid.UUID{}
	for _, id := range staff {
		if h, ok := w.heads[id]; ok {
			if _, dup := by[h.SubjectRef]; !dup {
				refs = append(refs, h.SubjectRef)
				by[h.SubjectRef] = id
			}
		}
	}
	got, err := s.readPII(ctx, refs, client.PurposeHRMProfile, true, client.SectionHRMProfile)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]domain.PromoPerson{}
	for ref, it := range got {
		p := domain.PromoPerson{FullName: it.FullName, Gender: it.Fields["gender"]}
		if it.Erased {
			p.FullName = client.ErasedLabel
		}
		if b := it.Fields["birth_date"]; len(b) >= 4 {
			p.BirthYear = b[:4]
		}
		out[by[ref]] = p
	}
	return out, nil
}

// renderPromo dựng tệp Excel của mẫu form.
func (s *Service) renderPromo(ctx context.Context, w *promoWorld, form string, month, asOf time.Time, items []domain.ExportItem,
	params map[string]string, actor uuid.UUID, at time.Time) ([]byte, string, error) {
	staff := make([]uuid.UUID, 0, len(items)+1)
	for _, it := range items {
		staff = append(staff, it.StaffID)
	}
	var prep uuid.UUID
	if p, err := uuid.Parse(params["preparer"]); err == nil {
		prep = p
		staff = append(staff, p)
	}
	for _, h := range w.heads {
		if h.AccountRef != nil && *h.AccountRef == actor {
			staff = append(staff, h.StaffID)
		}
	}
	people, err := s.promoPeople(ctx, w, staff)
	if err != nil {
		return nil, "", err
	}
	exporter := "tài khoản " + actor.String()[:8]
	for _, h := range w.heads {
		if h.AccountRef != nil && *h.AccountRef == actor && people[h.StaffID].FullName != "" {
			exporter = people[h.StaffID].FullName
		}
	}
	t := domain.BuildForm(form, month, asOf, items, people)
	data, err := renderXLSX(t, xlsxMeta{Agency: s.Rep.Agency, Org: s.Rep.Org, Preparer: people[prep].FullName,
		SignerTitle: strings.ToUpper(params["signer_title"]), Exporter: exporter, ExportedAt: at})
	return data, domain.FormFileName(form, month), err
}

// PromoExportView là một lần xuất trong lịch sử (FN-09 luồng phụ "xem lại các file đã xuất"; AC-04).
type PromoExportView struct {
	store.PromoExport
	Names      map[uuid.UUID]string
	Status     map[string]domain.PromoStatus // khóa kỳ → trạng thái hiện hành
	DecisionNo string                        // quyết định còn hiệu lực đã lập từ danh sách này
}

// PromotionExports trả các lần xuất, mới trước (không PII).
func (s *Service) PromotionExports(ctx context.Context, limit int) ([]PromoExportView, error) {
	c, err := s.promoCaller(ctx, false)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []PromoExportView
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		xs, err := store.PromoExports(ctx, tx, limit)
		if err != nil {
			return err
		}
		decs, err := store.Decisions(ctx, tx, uuid.Nil)
		if err != nil {
			return err
		}
		cancelled := map[uuid.UUID]bool{}
		for _, d := range decs {
			if d.Action == "cancel" {
				cancelled[d.DecisionID] = true
			}
		}
		for _, x := range xs {
			v := PromoExportView{PromoExport: x}
			for _, d := range decs {
				if d.Action == "issue" && d.ExportID != nil && *d.ExportID == x.ID && !cancelled[d.DecisionID] {
					v.DecisionNo = d.No
				}
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

// PromotionExport trả một lần xuất kèm họ tên và trạng thái từng kỳ (khung "Thêm quyết định từ danh sách").
func (s *Service) PromotionExport(ctx context.Context, id uuid.UUID) (PromoExportView, error) {
	c, err := s.promoCaller(ctx, false)
	if err != nil {
		return PromoExportView{}, err
	}
	w, _, err := s.worldOpt(ctx, c, s.day(time.Time{}), false)
	if err != nil {
		return PromoExportView{}, err
	}
	var v PromoExportView
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		x, ok, err := store.PromoExportByID(ctx, tx, id)
		if err == nil && (!ok || x.Action != "export") {
			err = port.ErrPromoExportNotFound
		}
		v.PromoExport = x
		return err
	})
	if err != nil {
		return PromoExportView{}, err
	}
	if v.Names, err = s.names(ctx, w, v.StaffIDs); err != nil {
		return PromoExportView{}, err
	}
	v.Status = map[string]domain.PromoStatus{}
	for _, it := range v.Items {
		if cs, ok := w.cases[it.CaseKey]; ok {
			v.Status[it.CaseKey] = cs.Status
		}
	}
	for _, did := range w.decOrder {
		d := w.decisions[did]
		if d.active() && d.Issue.ExportID != nil && *d.Issue.ExportID == id {
			v.DecisionNo = d.Issue.No
		}
	}
	return v, nil
}

// PromoFile là tệp tải lại.
type PromoFile struct {
	Data     []byte
	FileName string
	Drift    bool
	SHA256   string
}

// DownloadPromotionExport tải lại một lần xuất: dựng lại từ dòng đã lưu + định danh hiện hành, so SHA-256 (drift khi định
// danh ở kho đã đổi); ghi sổ 'download' + nhật ký xuất hồ sơ; qua điểm kiểm soát xuất.
func (s *Service) DownloadPromotionExport(ctx context.Context, id uuid.UUID) (PromoFile, error) {
	c, err := s.promoCaller(ctx, false)
	if err != nil {
		return PromoFile{}, err
	}
	var x store.PromoExport
	if err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		r, ok, err := store.PromoExportByID(ctx, tx, id)
		if err == nil && (!ok || r.Action != "export") {
			err = port.ErrPromoExportNotFound
		}
		x = r
		return err
	}); err != nil {
		return PromoFile{}, err
	}
	w, _, err := s.worldOpt(ctx, c, s.day(time.Time{}), false)
	if err != nil {
		return PromoFile{}, err
	}
	data, name, err := s.renderPromo(ctx, w, x.Form, x.ReviewMonth, x.AsOf, x.Items, x.Params, x.Actor, *x.KnownAt)
	if err != nil {
		return PromoFile{}, err
	}
	sum := sha256.Sum256(data)
	hexsum := hex.EncodeToString(sum[:])
	drift := hexsum != x.SHA256
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if _, err := store.InsertPromoExport(ctx, tx, c.tenant, store.PromoExport{ID: ids.New(), Action: "download", Form: x.Form,
			ReviewMonth: x.ReviewMonth, AsOf: x.AsOf, ExportID: &id, Params: x.Params, StaffIDs: x.StaffIDs, FileName: name, SHA256: hexsum,
			ByteSize: int64(len(data)), Drift: &drift, Actor: c.ref}, s.Clock.Now(), stableKey("", "promo-download")); err != nil {
			return err
		}
		return store.InsertAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "export", StaffIDs: x.StaffIDs, ItemCount: len(x.StaffIDs),
			Scope: string(domain.ScopeAll), Actor: c.ref, Query: map[string]string{"promotion_form": x.Form, "download": id.String()}}, s.Clock.Now())
	})
	if err != nil {
		return PromoFile{}, err
	}
	s.reportEgress(ctx, c, domain.ReportCode("promotion_"+x.Form), len(x.StaffIDs), len(data))
	return PromoFile{Data: data, FileName: name, Drift: drift, SHA256: x.SHA256}, nil
}
