package app

// W11.25 — SỔ QUYẾT ĐỊNH NÂNG BẬC (FN-10) và JOB TÍNH MỐC HẰNG NGÀY (FN-09 bước 1).
//
// Lưu quyết định = MỘT giao dịch: dòng 'issue' ở promotion_decision + dòng 'step' ở staff_event cho từng người NLTX/NLTH
// (bậc, hệ số trong hồ sơ là phép chiếu — cập nhật ngay), trạng thái "Đã có quyết định" cho kỳ của người có tên, "trả về
// bảng theo dõi kèm ghi chú" cho người trong danh sách gốc không có tên (BR-02). Hủy = SỰ KIỆN BÙ: dòng 'cancel'
// (supersedes dòng issue, lý do) + dòng 'void' cho từng dòng bậc đã ghi (khôi phục bậc, hệ số trước đó) + trạng thái về
// "Đã xuất danh sách"; quyết định không xóa (BR-03). Tệp quyết định lưu ở Kho tài liệu (FN-14, thư mục hệ thống
// "nang_bac") bằng dòng 'attach'.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
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
)

// DecisionPersonInput là một người có tên trong quyết định.
type DecisionPersonInput struct {
	CaseKey     string     // từ danh sách đã xuất
	StaffID     uuid.UUID  // nhập quyết định đã ban hành
	EffectiveOn *time.Time // ngày hưởng (vắng → ngày đến kỳ của danh sách)
	ToStep      int        // nhập quyết định đã ban hành — NLTX/NLTH: bậc sau quyết định
	PctTo       string     // nhập quyết định đã ban hành — TNVK: % sau quyết định
}

// DecisionInput là một quyết định cần lưu (FN-10 B2, B3; luồng "Nhập quyết định đã ban hành").
type DecisionInput struct {
	ExportID    *uuid.UUID
	Kind        string // bắt buộc khi không có ExportID
	No          string
	DecidedOn   time.Time
	Issuer      string
	People      []DecisionPersonInput
	Acknowledge bool // đã xem cảnh báo (số quyết định trùng)
}

// DecisionResult là kết quả lưu.
type DecisionResult struct {
	ID       uuid.UUID
	Updated  int // số hồ sơ được ghi bậc mới
	Returned int // số người trong danh sách gốc không có tên — trả về bảng theo dõi
	Warnings []string
}

func normDecision(in *DecisionInput) error {
	v, bad := problem.Validation, false
	in.No = strings.Join(strings.Fields(in.No), " ")
	if in.No == "" || utf8.RuneCountInString(in.No) > 60 {
		v, bad = v.WithField("decision_no", "số quyết định 1–60 ký tự"), true
	}
	in.Issuer = strings.TrimSpace(in.Issuer)
	if in.Issuer == "" || utf8.RuneCountInString(in.Issuer) > 200 {
		v, bad = v.WithField("issuer", "cơ quan ban hành 1–200 ký tự"), true
	}
	if in.DecidedOn.IsZero() {
		v, bad = v.WithField("decided_on", "bắt buộc"), true
	}
	if len(in.People) == 0 || len(in.People) > maxCaseKeys {
		v, bad = v.WithField("people", "chọn ít nhất một người có tên trong quyết định"), true
	}
	if bad {
		return v
	}
	return nil
}

// stepAt chiếu ngạch, bậc của hồ sơ tại ngày at.
func (w *promoWorld) stepAt(staff uuid.UUID, at time.Time) domain.Snapshot {
	return domain.Project(w.events[staff], at, nil)
}

func (w *promoWorld) coeff(ctx context.Context, tx pgx.Tx, grade string, step int, at time.Time) (decimal.Decimal, int, error) {
	sc, err := store.ScalesAt(ctx, tx, grade, at)
	if err != nil || len(sc) == 0 {
		return decimal.Zero, 0, err
	}
	if step < 1 || step > len(sc[0].Coefficients) {
		return decimal.Zero, len(sc[0].Coefficients), nil
	}
	return sc[0].Coefficients[step-1], len(sc[0].Coefficients), nil
}

// IssueDecision lưu quyết định và cập nhật bậc, hệ số trong hồ sơ cùng giao dịch.
func (s *Service) IssueDecision(ctx context.Context, in DecisionInput, idem string) (DecisionResult, error) {
	c, err := s.promoCaller(ctx, true)
	if err != nil {
		return DecisionResult{}, err
	}
	if err := normDecision(&in); err != nil {
		return DecisionResult{}, err
	}
	res := DecisionResult{ID: ids.New()}
	err = s.writeWorld(ctx, c, s.day(time.Time{}), func(tx pgx.Tx, w *promoWorld) error {
		if err := store.LockDecisions(ctx, tx, c.tenant); err != nil {
			return err
		}
		var exp *store.PromoExport
		kind := domain.PromoKind(in.Kind)
		if in.ExportID != nil {
			x, ok, err := store.PromoExportByID(ctx, tx, *in.ExportID)
			if err != nil {
				return err
			}
			if !ok || x.Action != "export" {
				return port.ErrPromoExportNotFound
			}
			exp = &x
			kind, _ = domain.FormKind(x.Form)
			for _, id := range w.decOrder {
				if d := w.decisions[id]; d.active() && d.Issue.ExportID != nil && *d.Issue.ExportID == x.ID {
					return port.ErrPromotionConflict.WithDetail("danh sách này đã có quyết định " + d.Issue.No)
				}
			}
		} else if _, ok := domain.ParsePromoKind(in.Kind); !ok {
			return problem.Validation.WithField("kind", "nltx | nlth | tnvk")
		}
		for _, id := range w.decOrder {
			if d := w.decisions[id]; d.active() && strings.EqualFold(d.Issue.No, in.No) {
				res.Warnings = append(res.Warnings, "Số quyết định "+in.No+" đã có trong sổ")
			}
		}
		if len(res.Warnings) > 0 && !in.Acknowledge {
			return port.ErrDecisionWarnings.WithDetail(strings.Join(res.Warnings, "; "))
		}
		people, picked, err := s.decisionPeople(ctx, tx, w, kind, exp, in)
		if err != nil {
			return err
		}
		row := store.DecisionRow{ID: ids.New(), DecisionID: res.ID, Action: "issue", Kind: string(kind), No: in.No, DecidedOn: in.DecidedOn,
			Issuer: in.Issuer, ExportID: in.ExportID, Actor: c.ref}
		for i := range people {
			p := &people[i]
			row.StaffIDs = append(row.StaffIDs, p.StaffID)
			if kind == domain.PromoTNVK {
				continue
			}
			eff, _ := time.Parse(time.DateOnly, p.EffectiveOn)
			ev := domain.Event{ID: ids.New(), Kind: domain.EventStep, ValidFrom: eff, Precision: domain.PrecisionDay, Actor: c.ref,
				Payload: map[string]string{"step": strconv.Itoa(p.ToStep), "step_since": p.EffectiveOn, "decision_no": truncate(in.No, 50)}}
			if exp != nil {
				for _, it := range exp.Items {
					if it.CaseKey == p.CaseKey && it.ReservedAfter != "" {
						if d, err := decimal.NewFromString(it.ReservedAfter); err == nil && d.IsPositive() {
							ev.Payload["reserved_coeff"] = d.StringFixed(2)
						}
					}
				}
			}
			if _, err := store.InsertEvent(ctx, tx, c.tenant, p.StaffID, ev, s.Clock.Now(), stableKey(idem, "decision:step:"+p.StaffID.String())); err != nil {
				return classify(err)
			}
			id := ev.ID
			p.StepEventID = &id
			res.Updated++
		}
		row.People = people
		if _, err := store.InsertDecision(ctx, tx, c.tenant, row, s.Clock.Now(), stableKey(idem, "decision:issue")); err != nil {
			return classify(err)
		}
		for _, p := range people {
			if p.CaseKey == "" {
				continue
			}
			eff, _ := time.Parse(time.DateOnly, p.EffectiveOn)
			did := res.ID
			if err := s.promoEvent(ctx, tx, c, store.PromoEvent{StaffID: p.StaffID, CaseKey: p.CaseKey, Kind: string(kind), Action: domain.PromoActDecide,
				DueOn: &eff, DecisionID: &did, AwardEvent: p.AwardEventID}, stableKey(idem, "promo:decide:"+p.CaseKey)); err != nil {
				return err
			}
		}
		if exp != nil {
			for _, it := range exp.Items {
				if picked[it.CaseKey] {
					continue
				}
				due, _ := time.Parse(time.DateOnly, it.Due)
				did := res.ID
				if err := s.promoEvent(ctx, tx, c, store.PromoEvent{StaffID: it.StaffID, CaseKey: it.CaseKey, Kind: string(it.Kind),
					Action: domain.PromoActReturn, DueOn: &due, DecisionID: &did, AwardEvent: it.AwardEventID,
					Reason: "Không có tên trong quyết định " + in.No}, stableKey(idem, "promo:return:"+it.CaseKey)); err != nil {
					return err
				}
				res.Returned++
			}
		}
		return nil
	})
	return res, err
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// decisionPeople kiểm và dựng danh sách người của quyết định (bậc hiện tại tại ngày hưởng phải khớp danh sách gốc).
func (s *Service) decisionPeople(ctx context.Context, tx pgx.Tx, w *promoWorld, kind domain.PromoKind, exp *store.PromoExport,
	in DecisionInput) ([]store.DecisionPerson, map[string]bool, error) {
	picked := map[string]bool{}
	seen := map[uuid.UUID]bool{}
	var out []store.DecisionPerson
	for i, p := range in.People {
		field := fmt.Sprintf("people[%d]", i)
		var dp store.DecisionPerson
		if exp != nil {
			var item *domain.ExportItem
			for j := range exp.Items {
				if exp.Items[j].CaseKey == p.CaseKey {
					item = &exp.Items[j]
				}
			}
			if item == nil {
				return nil, nil, problem.Validation.WithField(field, "người không có trong danh sách đã xuất")
			}
			if cs, ok := w.cases[item.CaseKey]; ok && cs.Status == domain.PromoDecided {
				return nil, nil, port.ErrPromotionConflict.WithDetail(item.Code + ": kỳ này đã có quyết định")
			}
			eff, _ := time.Parse(time.DateOnly, item.Due)
			if p.EffectiveOn != nil {
				eff = *p.EffectiveOn
			}
			dp = store.DecisionPerson{StaffID: item.StaffID, CaseKey: item.CaseKey, EffectiveOn: eff.Format(time.DateOnly), Grade: item.Grade,
				FromStep: item.FromStep, ToStep: item.ToStep, FromCoeff: item.FromCoeff, ToCoeff: item.ToCoeff, PctFrom: item.PctFrom,
				PctTo: item.PctTo, AwardEventID: item.AwardEventID}
			if kind != domain.PromoTNVK {
				if cur := w.stepAt(item.StaffID, eff); cur.Grade != item.Grade || cur.Step != item.FromStep {
					return nil, nil, port.ErrDecisionStale.WithDetail(fmt.Sprintf("%s: bậc tại %s là %d (danh sách: %d)", item.Code,
						eff.Format("02/01/2006"), cur.Step, item.FromStep))
				}
			}
			picked[item.CaseKey] = true
		} else {
			h, ok := w.heads[p.StaffID]
			if !ok || h.Type != domain.StaffVienChuc {
				return nil, nil, problem.Validation.WithField(field, "chọn viên chức có hồ sơ")
			}
			if p.EffectiveOn == nil {
				return nil, nil, problem.Validation.WithField(field, "nhập ngày hưởng")
			}
			eff := *p.EffectiveOn
			cur := w.stepAt(p.StaffID, eff)
			if cur.Grade == "" {
				return nil, nil, port.ErrStepOutOfRange.WithDetail(h.Code + ": chưa có ngạch hiệu lực tại ngày hưởng")
			}
			from, steps, err := w.coeff(ctx, tx, cur.Grade, cur.Step, eff)
			if err != nil {
				return nil, nil, err
			}
			dp = store.DecisionPerson{StaffID: p.StaffID, EffectiveOn: eff.Format(time.DateOnly), Grade: cur.Grade, FromStep: cur.Step,
				FromCoeff: from.StringFixed(2)}
			if kind == domain.PromoTNVK {
				d, ok := pct(p.PctTo)
				if !ok || !d.IsPositive() {
					return nil, nil, problem.Validation.WithField(field, "nhập % TNVK sau quyết định (0–100)")
				}
				if prev := w.tnvkOf(p.StaffID, eff); prev != nil {
					dp.PctFrom = prev.Pct.String()
				}
				dp.PctTo, dp.ToStep, dp.ToCoeff = d.String(), cur.Step, dp.FromCoeff
			} else {
				if p.ToStep < 1 || steps == 0 || p.ToStep > steps {
					return nil, nil, port.ErrStepOutOfRange.WithDetail(fmt.Sprintf("%s: bậc phải từ 1 đến %d (ngạch %s)", h.Code, steps, cur.Grade))
				}
				to, _, err := w.coeff(ctx, tx, cur.Grade, p.ToStep, eff)
				if err != nil {
					return nil, nil, err
				}
				dp.ToStep, dp.ToCoeff = p.ToStep, to.StringFixed(2)
			}
		}
		if seen[dp.StaffID] {
			return nil, nil, problem.Validation.WithField(field, "một viên chức xuất hiện hai lần trong quyết định")
		}
		seen[dp.StaffID] = true
		out = append(out, dp)
	}
	return out, picked, nil
}

// CancelDecision hủy quyết định (sự kiện bù): khôi phục bậc, hệ số trước đó; thành tích NLTH không còn tính là đã dùng.
// Chặn khi cùng người có quyết định nâng bậc còn hiệu lực với ngày hưởng sau (hủy quyết định sau trước).
func (s *Service) CancelDecision(ctx context.Context, id uuid.UUID, reason, idem string) error {
	c, err := s.promoCaller(ctx, true)
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) < 10 || utf8.RuneCountInString(reason) > 500 {
		return problem.Validation.WithField("reason", "hủy quyết định cần lý do 10–500 ký tự (BR-03)")
	}
	return s.writeWorldOpt(ctx, c, s.day(time.Time{}), false, func(tx pgx.Tx, w *promoWorld) error {
		if err := store.LockDecisions(ctx, tx, c.tenant); err != nil {
			return err
		}
		d, ok := w.decisions[id]
		if !ok || d.Issue.ID == uuid.Nil {
			return port.ErrDecisionNotFound
		}
		if !d.active() {
			return port.ErrDecisionCancelled
		}
		for _, p := range d.Issue.People {
			for _, oid := range w.decOrder {
				o := w.decisions[oid]
				if oid == id || !o.active() {
					continue
				}
				for _, q := range o.Issue.People {
					if q.StaffID == p.StaffID && q.EffectiveOn > p.EffectiveOn && (o.Issue.Kind == d.Issue.Kind ||
						o.Issue.Kind != string(domain.PromoTNVK) && d.Issue.Kind != string(domain.PromoTNVK)) {
						return port.ErrDecisionOrder.WithDetail("quyết định " + o.Issue.No)
					}
				}
			}
		}
		now := s.Clock.Now()
		why := truncate("Hủy quyết định "+d.Issue.No+": "+reason, 500)
		for _, p := range d.Issue.People {
			if p.StepEventID == nil {
				continue
			}
			var target *domain.Event
			for _, e := range domain.Effective(w.events[p.StaffID]) {
				if e.ID == *p.StepEventID {
					x := e
					target = &x
				}
			}
			if target == nil {
				continue // dòng bậc đã được sửa/hủy ở hồ sơ
			}
			ev := domain.Event{ID: ids.New(), Kind: target.Kind, ValidFrom: target.ValidFrom, Precision: target.Precision, Payload: map[string]string{},
				Void: true, Supersedes: &target.ID, Reason: why, Actor: c.ref}
			if _, err := store.InsertEvent(ctx, tx, c.tenant, p.StaffID, ev, now, stableKey(idem, "decision:void:"+p.StaffID.String())); err != nil {
				return classify(err)
			}
		}
		issue := d.Issue
		cancel := store.DecisionRow{ID: ids.New(), DecisionID: id, Action: "cancel", Kind: issue.Kind, No: issue.No, DecidedOn: issue.DecidedOn,
			Issuer: issue.Issuer, ExportID: issue.ExportID, StaffIDs: issue.StaffIDs, Supersedes: &issue.ID, Reason: reason, Actor: c.ref}
		if _, err := store.InsertDecision(ctx, tx, c.tenant, cancel, now, stableKey(idem, "decision:cancel")); err != nil {
			return classify(err)
		}
		for _, p := range issue.People {
			if p.CaseKey == "" {
				continue
			}
			did := id
			if err := s.promoEvent(ctx, tx, c, store.PromoEvent{StaffID: p.StaffID, CaseKey: p.CaseKey, Kind: issue.Kind, Action: domain.PromoActUndecide,
				DecisionID: &did, AwardEvent: p.AwardEventID, Reason: truncate(why, 1000)}, stableKey(idem, "promo:undecide:"+p.CaseKey)); err != nil {
				return err
			}
		}
		return nil
	})
}

// AttachDecisionFile lưu tệp quyết định vào Kho tài liệu (thư mục hệ thống "nang_bac", gắn người có tên, số, ngày quyết
// định) rồi ghi dòng 'attach'. Tải lại tệp khác → dòng 'attach' mới (tệp cũ vẫn ở kho).
func (s *Service) AttachDecisionFile(ctx context.Context, id uuid.UUID, fileName string, content []byte, idem string) (uuid.UUID, error) {
	c, err := s.promoCaller(ctx, true)
	if err != nil {
		return uuid.Nil, err
	}
	if !s.docsReady() {
		return uuid.Nil, port.ErrDocsUnavailable
	}
	var issue store.DecisionRow
	var folder uuid.UUID
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		rows, err := store.Decisions(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, r := range rows {
			switch r.Action {
			case "issue":
				issue = r
			case "cancel":
				return port.ErrDecisionCancelled
			}
		}
		if issue.ID == uuid.Nil {
			return port.ErrDecisionNotFound
		}
		fs, err := store.CurrentFolders(ctx, tx)
		if err != nil {
			return err
		}
		for _, f := range fs {
			if f.SystemKey == "nang_bac" && f.Status == "active" {
				folder = f.FolderID
			}
		}
		if folder == uuid.Nil {
			return port.ErrDocsUnavailable.WithDetail("chưa có thư mục hệ thống \"Quyết định nâng bậc\" — Admin cài thư mục mặc định (FN-14)")
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	issued := issue.DecidedOn
	year := issued.Year()
	doc, err := s.UploadDoc(ctx, DocInput{FolderID: folder, Title: truncate("Quyết định "+issue.No, 300), DocType: domain.DocNangBac, Year: &year,
		StaffIDs: issue.StaffIDs, DocNo: issue.No, IssuedOn: &issued, Note: domain.PromoKind(issue.Kind).Label()}, fileName, content,
		stableKeyOrEmpty(idem, "decision:file"))
	if err != nil {
		return uuid.Nil, err
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		row := issue
		row.ID, row.Action, row.DocID, row.People, row.Supersedes, row.Reason, row.Actor = ids.New(), "attach", &doc.DocID, nil, nil, "", c.ref
		_, err := store.InsertDecision(ctx, tx, c.tenant, row, s.Clock.Now(), stableKey(idem, "decision:attach"))
		return classify(err)
	})
	return doc.DocID, err
}

func stableKeyOrEmpty(idem, suffix string) string {
	if idem == "" {
		return ""
	}
	return idem + ":" + suffix
}

// DecisionView là một quyết định trong sổ.
type DecisionView struct {
	Issue     store.DecisionRow
	DocID     *uuid.UUID
	Cancelled *store.DecisionRow
	Rows      []store.DecisionRow
	Names     map[uuid.UUID]string
	Source    string // danh sách gốc: tên tệp đã xuất hoặc "Nhập từ quyết định đã ban hành"
}

// DecisionFilter là bộ lọc sổ quyết định (FN-10 B1; AC-02: tìm theo số, năm, tên viên chức).
type DecisionFilter struct {
	Year   int
	Kind   string
	Issuer string
	Q      string
}

// Decisions trả sổ quyết định (mới trước) theo bộ lọc.
func (s *Service) Decisions(ctx context.Context, f DecisionFilter) ([]DecisionView, error) {
	c, err := s.promoCaller(ctx, false)
	if err != nil {
		return nil, err
	}
	return s.decisionViews(ctx, c, uuid.Nil, f)
}

// Decision trả một quyết định kèm lịch sử dòng sổ.
func (s *Service) Decision(ctx context.Context, id uuid.UUID) (DecisionView, error) {
	c, err := s.promoCaller(ctx, false)
	if err != nil {
		return DecisionView{}, err
	}
	xs, err := s.decisionViews(ctx, c, id, DecisionFilter{})
	if err != nil {
		return DecisionView{}, err
	}
	if len(xs) == 0 {
		return DecisionView{}, port.ErrDecisionNotFound
	}
	return xs[0], nil
}

func (s *Service) decisionViews(ctx context.Context, c rc, only uuid.UUID, f DecisionFilter) ([]DecisionView, error) {
	w, _, err := s.worldOpt(ctx, c, s.day(time.Time{}), false)
	if err != nil {
		return nil, err
	}
	sources := map[uuid.UUID]string{}
	if err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		xs, err := store.PromoExports(ctx, tx, 1000)
		for _, x := range xs {
			sources[x.ID] = x.FileName
		}
		return err
	}); err != nil {
		return nil, err
	}
	var staff []uuid.UUID
	for _, id := range w.decOrder {
		staff = append(staff, w.decisions[id].Issue.StaffIDs...)
	}
	names, err := s.names(ctx, w, staff)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(f.Q))
	var out []DecisionView
	for i := len(w.decOrder) - 1; i >= 0; i-- {
		id := w.decOrder[i]
		d := w.decisions[id]
		if d.Issue.ID == uuid.Nil || only != uuid.Nil && id != only {
			continue
		}
		is := d.Issue
		if f.Year != 0 && is.DecidedOn.Year() != f.Year || f.Kind != "" && is.Kind != f.Kind || f.Issuer != "" && is.Issuer != f.Issuer {
			continue
		}
		v := DecisionView{Issue: is, DocID: d.DocID, Cancelled: d.Cancel, Rows: d.Rows, Names: map[uuid.UUID]string{},
			Source: "Nhập từ quyết định đã ban hành"}
		if is.ExportID != nil {
			v.Source = sources[*is.ExportID]
		}
		match := q == "" || strings.Contains(strings.ToLower(is.No), q)
		for _, sid := range is.StaffIDs {
			v.Names[sid] = names[sid]
			match = match || strings.Contains(strings.ToLower(names[sid]), q)
		}
		if match {
			out = append(out, v)
		}
	}
	return out, nil
}

// DecisionBook dựng tệp Excel sổ quyết định (FN-10 luồng phụ; AC-03) — không PII (số, ngày, cơ quan, loại, số người).
func (s *Service) DecisionBook(ctx context.Context, f DecisionFilter) ([]byte, string, error) {
	xs, err := s.Decisions(ctx, f)
	if err != nil {
		return nil, "", err
	}
	t := domain.Table{Code: "SO_QD", Title: "SỔ QUYẾT ĐỊNH NÂNG BẬC LƯƠNG", Period: "NĂM " + strconv.Itoa(f.Year), AsOf: s.day(time.Time{}),
		Columns: []domain.Column{{Key: "stt", Label: "Số TT"}, {Key: "no", Label: "Số quyết định"}, {Key: "date", Label: "Ngày quyết định"},
			{Key: "issuer", Label: "Cơ quan ban hành"}, {Key: "kind", Label: "Loại"}, {Key: "source", Label: "Danh sách gốc"},
			{Key: "count", Label: "Số người"}, {Key: "file", Label: "Tệp"}, {Key: "status", Label: "Trạng thái"}}}
	if f.Year == 0 {
		t.Period = "TẤT CẢ CÁC NĂM"
	}
	sort.SliceStable(xs, func(i, j int) bool { return xs[i].Issue.DecidedOn.Before(xs[j].Issue.DecidedOn) })
	for i, x := range xs {
		st, file := "Đã cập nhật bậc, hệ số", "Chưa có tệp"
		if x.Cancelled != nil {
			st = "Đã hủy: " + x.Cancelled.Reason
		}
		if x.DocID != nil {
			file = "Có (Kho tài liệu)"
		}
		t.Rows = append(t.Rows, domain.Row{Kind: domain.RowPerson, Cells: []string{strconv.Itoa(i + 1), x.Issue.No,
			x.Issue.DecidedOn.Format("02/01/2006"), x.Issue.Issuer, domain.PromoKind(x.Issue.Kind).Label(), x.Source,
			strconv.Itoa(len(x.Issue.StaffIDs)), file, st}})
	}
	data, err := renderXLSX(t, xlsxMeta{Agency: s.Rep.Agency, Org: s.Rep.Org, Exporter: "hệ thống", ExportedAt: s.Clock.Now()})
	name := "SoQuyetDinhNangBac.xlsx"
	if f.Year != 0 {
		name = fmt.Sprintf("SoQuyetDinhNangBac_%d.xlsx", f.Year)
	}
	return data, name, err
}

// --- Job hằng ngày. ---

// JobPromotionScan là tên job tính mốc nâng bậc trong cmd/worker (-jobs).
const JobPromotionScan = "hrm_promotion_scan"

// ScanResult là kết quả job một tenant.
type ScanResult struct {
	Day    time.Time
	Counts map[string]int
	Fresh  bool // false = đã chạy trong ngày (khóa trùng)
}

// PromotionScan tính mốc NLTX/TNVK của mọi viên chức của tenant tid tại ngày now (giờ VN): số đến kỳ trong các mốc nhắc
// (1, 3, 6 tháng — tham số), số "Chưa đủ"/"Cần xem xét", số chưa tính được; ghi sổ promotion_scan (mỗi ngày một dòng) và
// báo Notifier (nếu nối). Không phụ thuộc cờ AG_HRM_RECORDS ở mức tính (không đọc kho định danh); tenant không có tham số
// nâng bậc → bỏ qua.
func (s *Service) PromotionScan(ctx context.Context, tid uuid.UUID, now time.Time, notifier port.PromotionNotifier) (ScanResult, error) {
	day := domain.BusinessDay(now)
	res := ScanResult{Day: day, Counts: map[string]int{}}
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		w, err := loadPromo(ctx, tx, day, nil)
		if err != nil {
			return err
		}
		if w.noRules {
			return port.ErrPromotionRules
		}
		for _, m := range w.rules.RemindMonths {
			end := domain.AddMonths(day, m)
			k := "due_" + strconv.Itoa(m)
			res.Counts[k] = 0
			for _, x := range w.cands {
				if x.Kind != domain.PromoNLTH && !x.Due.After(end) {
					if st, _ := w.status(x); st != domain.PromoDecided {
						res.Counts[k]++
					}
				}
			}
		}
		for _, x := range w.cands {
			if st, _ := w.status(x); st == domain.PromoDecided {
				continue
			}
			switch x.Cond {
			case domain.CondFail:
				res.Counts["fail"]++
			case domain.CondReview:
				res.Counts["review"]++
			}
			if x.Due.Before(day) {
				if st, _ := w.status(x); st == domain.PromoPending {
					res.Counts["overdue"]++
				}
			}
		}
		res.Counts["missing"] = len(w.missing)
		res.Counts["active_vien_chuc"] = w.activeVC
		err = store.InsertScan(ctx, tx, ids.New(), tid, uuid.Nil, day, res.Counts, now, "hrm:promotion-scan:"+day.Format(time.DateOnly))
		if isUnique(err) {
			return errAlreadyScanned
		}
		return err
	})
	switch {
	case errors.Is(err, errAlreadyScanned):
		return res, nil
	case errors.Is(err, port.ErrPromotionRules):
		return res, nil
	case err != nil:
		return res, err
	}
	res.Fresh = true
	if notifier != nil {
		if err := notifier.PromotionDue(ctx, tid, day, res.Counts); err != nil {
			return res, err
		}
	}
	return res, nil
}

var errAlreadyScanned = errors.New("hrm: đã tính mốc trong ngày")

// PromotionScanAll chạy job cho mọi tenant (tenants: danh sách từ pool vai ag_ops_jobs).
func (s *Service) PromotionScanAll(ctx context.Context, tenants []uuid.UUID, log *slog.Logger) (int, error) {
	n := 0
	for _, tid := range tenants {
		r, err := s.PromotionScan(ctx, tid, s.Clock.Now(), s.Promo.Notifier)
		if err != nil {
			if log != nil {
				log.Error("tính mốc nâng bậc lỗi", "event", "job."+JobPromotionScan, "tenant", tid, "error", err)
			}
			continue
		}
		if r.Fresh {
			n++
		}
	}
	return n, nil
}
