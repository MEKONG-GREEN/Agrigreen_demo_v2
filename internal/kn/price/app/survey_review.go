package app

// Rà soát phiếu điều tra, cảnh báo bất thường, chốt đợt (W11.78; BRD Báo giá v1.3 FN-TI-02.12, CT-06, bảng 3.4):
//   - Danh sách phiếu + thẻ chỉ số (đã gửi, có cờ, chờ xác minh, đã duyệt, đã loại); cờ CT-06 tính lúc đọc theo trung vị
//     giá bán cùng mặt hàng trong đợt (≥ 5 phiếu, ngưỡng phiếu điều tra của cấu hình ngưỡng — mặc định 40 %).
//   - Họ tên nông hộ, cán bộ điều tra chỉ trả cho Cấp Tỉnh, Admin (đọc theo lô ở kho định danh); SĐT chỉ ở chi tiết phiếu.
//   - Thao tác: Cần xác minh → Ghi nhận xác minh (nội dung, giá đúng nếu có — lưu cả giá gốc, BR-04; "Không xác minh
//     được") → Duyệt (ghi chú bắt buộc nếu có cờ và chưa xác minh) / Loại (lý do bắt buộc); Xóa phiếu nhập nhầm (lý do, đợt
//     chưa chốt); Duyệt các phiếu không có cờ. Mọi thao tác là phiên bản mới của phiếu (nhật ký — BR-05).

import (
	"bytes"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// FormFilter là bộ lọc danh sách phiếu.
type FormFilter struct {
	Crop, Item, Commune string
	Flag                string // "" | flag | noflag
	Status              string
	Limit, Offset       int
}

// FormRow là một phiếu trong danh sách rà soát.
type FormRow struct {
	ID          uuid.UUID
	Seq         int64
	RowNo       int
	Commune     string
	CommuneName string
	Crop        string
	Item        string
	ItemName    string
	OfficerID   *uuid.UUID
	OfficerName string // Cấp Tỉnh, Admin
	FarmerName  string // Cấp Tỉnh, Admin
	Price       decimal.Decimal
	OrigPrice   decimal.Decimal
	Corrected   bool
	Median      *decimal.Decimal
	Pct         *decimal.Decimal
	Flag        string
	CostPerKg   decimal.Decimal
	Status      string
}

// FormKPIs là hàng thẻ chỉ số (B1).
type FormKPIs struct {
	Sent, Flagged, Verify, Approved, Rejected, Pending int
}

// PriceDist là phân bố giá bán của một mặt hàng theo xã (luồng phụ).
type PriceDist struct {
	Commune, CommuneName string
	N                    int
	Min, Max, Median     decimal.Decimal
}

// FormList là màn Rà soát phiếu.
type FormList struct {
	Round        RoundSummary
	KPIs         FormKPIs
	Total        int // số phiếu sau lọc
	Rows         []FormRow
	Dist         []PriceDist // khi lọc một mặt hàng
	Items        []Ref       // mặt hàng có phiếu trong đợt (bộ lọc)
	Communes     []Ref
	ThresholdPct decimal.Decimal
	CanReview    bool
	CanClose     bool
}

type reviewCtx struct {
	sn     *roundSnap
	states map[string]domain.ItemState
	names  map[string]string
	pct    decimal.Decimal
	devs   map[uuid.UUID]domain.Deviation
}

func (s *SurveyService) loadReview(ctx context.Context, tx pgx.Tx, a surveyAccess, round uuid.UUID, pct decimal.Decimal) (*reviewCtx, error) {
	sn, err := s.loadRound(ctx, tx, a.prof.Name, round, true)
	if err != nil {
		return nil, err
	}
	isn, err := loadSnap(ctx, tx)
	if err != nil {
		return nil, err
	}
	names, err := communeNames(ctx, tx)
	if err != nil {
		return nil, err
	}
	rc := &reviewCtx{sn: sn, states: allItemStates(isn), names: names, pct: pct, devs: map[uuid.UUID]domain.Deviation{}}
	prices := map[string][]decimal.Decimal{}
	for _, f := range sn.forms {
		if f.Status != domain.FormDeleted {
			prices[f.Item] = append(prices[f.Item], effPrice(f))
		}
	}
	for _, f := range sn.forms {
		if f.Status != domain.FormDeleted {
			rc.devs[f.FormID] = domain.CT06(effPrice(f), prices[f.Item], pct)
		}
	}
	return rc, nil
}

// allItemStates trả trạng thái mới nhất của mọi mặt hàng (kể cả đã ngưng — tên hiển thị của phiếu cũ).
func allItemStates(isn *snap) map[string]domain.ItemState {
	out := map[string]domain.ItemState{}
	for _, v := range isn.vers {
		st := domain.ItemState{Item: v.V, Version: v.Seq}
		if cur, ok := out[v.Key]; !ok || v.Seq >= cur.Version {
			out[v.Key] = st
		}
	}
	return out
}

func (rc *reviewCtx) row(f store.SurveyForm) FormRow {
	d := rc.devs[f.FormID]
	calc := domain.CT05(figures(f))
	return FormRow{ID: f.FormID, Seq: f.Seq, RowNo: f.RowNo, Commune: f.Commune, CommuneName: rc.names[f.Commune], Crop: f.Crop, Item: f.Item,
		ItemName: itemLabel(rc.states[f.Item]), OfficerID: f.OfficerID, Price: effPrice(f), OrigPrice: f.SalePrice,
		Corrected: f.VerifiedPrice != nil, Median: d.Median, Pct: d.Pct, Flag: d.Flag, CostPerKg: calc.CostPerKg, Status: f.Status}
}

// figures là số liệu tính CT-05 của phiếu (giá bán hiệu chỉnh nếu có).
func figures(f store.SurveyForm) domain.FormFigures {
	return domain.FormFigures{AreaHa: f.AreaHa, OutputKg: f.OutputKg, SalePrice: effPrice(f), OtherIncome: f.OtherIncome, Costs: f.Costs}
}

func (s *SurveyService) surveyPct(ctx context.Context, a surveyAccess) (decimal.Decimal, error) {
	_, _, pct, err := s.AlertParams(tenant.WithID(ctx, a.c.tenant), a.prof.Name)
	if err != nil {
		return decimal.Zero, err
	}
	if !pct.IsPositive() {
		pct = decimal.NewFromInt(40)
	}
	return pct, nil
}

// ListForms trả màn Rà soát phiếu của đợt (Cấp Tỉnh, Admin).
func (s *SurveyService) ListForms(ctx context.Context, profile string, round uuid.UUID, f FormFilter) (FormList, error) {
	return s.listForms(ctx, profile, round, f, true)
}

func (s *SurveyService) listForms(ctx context.Context, profile string, round uuid.UUID, f FormFilter, withNames bool) (FormList, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return FormList{}, err
	}
	if err := a.needRead(); err != nil {
		return FormList{}, err
	}
	pct, err := s.surveyPct(ctx, a)
	if err != nil {
		return FormList{}, err
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	var (
		out FormList
		rc  *reviewCtx
	)
	today := s.today()
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		rc, err = s.loadReview(ctx, tx, a, round, pct)
		return err
	})
	if err != nil {
		return FormList{}, err
	}
	d := s.detail(rc.sn, today, rc.names)
	out.Round, out.ThresholdPct = d.RoundSummary, pct
	st := out.Round.Status
	out.CanReview = a.tinh && (st == domain.RoundCollecting || st == domain.RoundReviewing)
	items, communes := map[string]bool{}, map[string]bool{}
	byItemCommune := map[string][]decimal.Decimal{}
	var rows []FormRow
	for _, fm := range rc.sn.forms {
		if fm.Status == domain.FormDeleted {
			continue
		}
		r := rc.row(fm)
		k := &out.KPIs
		k.Sent++
		if r.Flag != "" && fm.Status != domain.FormApproved && fm.Status != domain.FormRejected {
			k.Flagged++
		}
		switch fm.Status {
		case domain.FormVerify:
			k.Verify++
		case domain.FormApproved:
			k.Approved++
		case domain.FormRejected:
			k.Rejected++
		}
		if domain.FormPending(fm.Status) {
			k.Pending++
		}
		if !items[fm.Item] {
			items[fm.Item] = true
			out.Items = append(out.Items, Ref{Code: fm.Item, Name: r.ItemName})
		}
		if !communes[fm.Commune] {
			communes[fm.Commune] = true
			out.Communes = append(out.Communes, Ref{Code: fm.Commune, Name: r.CommuneName})
		}
		if (f.Crop != "" && fm.Crop != f.Crop) || (f.Item != "" && fm.Item != f.Item) || (f.Commune != "" && fm.Commune != f.Commune) ||
			(f.Status != "" && fm.Status != f.Status) || (f.Flag == "flag" && r.Flag == "") || (f.Flag == "noflag" && r.Flag != "") {
			continue
		}
		if f.Item != "" {
			byItemCommune[fm.Commune] = append(byItemCommune[fm.Commune], r.Price)
		}
		rows = append(rows, r)
	}
	out.CanClose = out.CanReview && out.KPIs.Pending == 0
	slices.SortFunc(out.Items, func(x, y Ref) int { return strings.Compare(x.Name, y.Name) })
	slices.SortFunc(out.Communes, func(x, y Ref) int { return strings.Compare(x.Name, y.Name) })
	// Phiếu có cờ chưa xử lý lên đầu, rồi theo xã, mặt hàng, dòng tệp.
	slices.SortStableFunc(rows, func(x, y FormRow) int {
		fx, fy := x.Flag != "" && domain.FormPending(x.Status), y.Flag != "" && domain.FormPending(y.Status)
		if fx != fy {
			if fx {
				return -1
			}
			return 1
		}
		if c := strings.Compare(x.CommuneName, y.CommuneName); c != 0 {
			return c
		}
		if c := strings.Compare(x.ItemName, y.ItemName); c != 0 {
			return c
		}
		return x.RowNo - y.RowNo
	})
	out.Total = len(rows)
	if f.Offset < len(rows) {
		rows = rows[f.Offset:min(len(rows), f.Offset+f.Limit)]
	} else {
		rows = nil
	}
	for c, ps := range byItemCommune {
		mn, mx := ps[0], ps[0]
		for _, p := range ps {
			mn, mx = decimal.Min(mn, p), decimal.Max(mx, p)
		}
		out.Dist = append(out.Dist, PriceDist{Commune: c, CommuneName: rc.names[c], N: len(ps), Min: mn, Max: mx, Median: *domain.Median(ps)})
	}
	slices.SortFunc(out.Dist, func(x, y PriceDist) int { return strings.Compare(x.CommuneName, y.CommuneName) })
	if withNames && a.pii && len(rows) > 0 {
		if err := s.fillNames(ctx, rc, rows); err != nil {
			return FormList{}, err
		}
	}
	out.Rows = rows
	return out, nil
}

// fillNames điền họ tên nông hộ, cán bộ điều tra của các dòng (một lần đọc theo lô).
func (s *SurveyService) fillNames(ctx context.Context, rc *reviewCtx, rows []FormRow) error {
	farmer := map[uuid.UUID]uuid.UUID{}
	for _, f := range rc.sn.forms {
		farmer[f.FormID] = f.FarmerRef
	}
	officer := map[uuid.UUID]uuid.UUID{}
	for _, o := range rc.sn.officers {
		officer[o.OfficerID] = o.SubjectRef
	}
	var refs []uuid.UUID
	for _, r := range rows {
		refs = append(refs, farmer[r.ID])
		if r.OfficerID != nil {
			refs = append(refs, officer[*r.OfficerID])
		}
	}
	idm, err := s.identities(ctx, refs)
	if err != nil {
		return err
	}
	for i := range rows {
		rows[i].FarmerName = idm[farmer[rows[i].ID]].FullName
		if rows[i].OfficerID != nil {
			rows[i].OfficerName = idm[officer[*rows[i].OfficerID]].FullName
		}
	}
	return nil
}

// FormLog là một dòng nhật ký phiếu.
type FormLog struct {
	Seq    int64
	Op     string
	Status string
	Note   string
	Price  *decimal.Decimal
	At     time.Time
	Actor  uuid.UUID
}

// FormDetail là chi tiết phiếu (B2): các khoản chi phí, giá trị tự tính, nông hộ, cán bộ điều tra, nhật ký.
type FormDetail struct {
	FormRow
	AreaHa, OutputKg, OtherIncome decimal.Decimal
	Costs                         map[string]decimal.Decimal
	Calc                          domain.Calc
	Note                          string
	Unverifiable                  bool
	FarmerPhone                   string
	OfficerPhone                  string
	OfficerUnit                   string
	ImportID                      uuid.UUID
	Log                           []FormLog
}

// GetForm trả chi tiết phiếu (Cấp Tỉnh, Admin) — họ tên, SĐT nông hộ và cán bộ điều tra từ kho định danh.
func (s *SurveyService) GetForm(ctx context.Context, profile string, round, form uuid.UUID) (FormDetail, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return FormDetail{}, err
	}
	if err := a.needRead(); err != nil {
		return FormDetail{}, err
	}
	pct, err := s.surveyPct(ctx, a)
	if err != nil {
		return FormDetail{}, err
	}
	var (
		d                   FormDetail
		farmerRef, offerRef uuid.UUID
	)
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		rc, err := s.loadReview(ctx, tx, a, round, pct)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(rc.sn.forms, func(f store.SurveyForm) bool { return f.FormID == form })
		if i < 0 {
			return port.ErrSurveyFormNotFound
		}
		f := rc.sn.forms[i]
		d = FormDetail{FormRow: rc.row(f), AreaHa: f.AreaHa, OutputKg: f.OutputKg, OtherIncome: f.OtherIncome, Costs: f.Costs,
			Calc: domain.CT05(figures(f)), Note: f.Note, Unverifiable: f.Unverifiable, ImportID: f.ImportID}
		farmerRef = f.FarmerRef
		if f.OfficerID != nil {
			for _, o := range rc.sn.officers {
				if o.OfficerID == *f.OfficerID {
					offerRef, d.OfficerUnit = o.SubjectRef, o.UnitName
				}
			}
		}
		h, err := store.SurveyFormHistory(ctx, tx, form)
		if err != nil {
			return err
		}
		for _, v := range h {
			d.Log = append(d.Log, FormLog{Seq: v.Seq, Op: v.Op, Status: v.Status, Note: v.Note, Price: v.VerifiedPrice, At: v.EventTime, Actor: v.Actor})
		}
		return nil
	})
	if err != nil || !a.pii {
		return d, err
	}
	refs := []uuid.UUID{farmerRef}
	if offerRef != uuid.Nil {
		refs = append(refs, offerRef)
	}
	idm, err := s.identities(ctx, refs)
	if err != nil {
		return d, err
	}
	d.FarmerName, d.FarmerPhone = idm[farmerRef].FullName, MaskOrPhone(idm[farmerRef].Phone)
	if offerRef != uuid.Nil {
		d.OfficerName, d.OfficerPhone = idm[offerRef].FullName, MaskOrPhone(idm[offerRef].Phone)
	}
	return d, nil
}

// MaskOrPhone đưa SĐT 84xxxxxxxxx về dạng 0xxxxxxxxx để hiển thị (Cấp Tỉnh liên hệ — không che).
func MaskOrPhone(p string) string {
	if strings.HasPrefix(p, "84") && len(p) == 11 {
		return "0" + p[2:]
	}
	return p
}

// FormAction là thao tác rà soát một phiếu.
type FormAction struct {
	Op           string // request_verify | record_verify | approve | reject | delete
	Note         string
	Price        *decimal.Decimal // record_verify: giá bán đúng (nếu có)
	Unverifiable bool             // record_verify: "Không xác minh được"
	Seq          int64            // phiên bản đang xem (khóa lạc quan; 0 = không kiểm)
}

// ReviewForm thực hiện một thao tác rà soát (Cấp Tỉnh) khi đợt Đang thu thập / Đang rà soát.
func (s *SurveyService) ReviewForm(ctx context.Context, profile string, round, form uuid.UUID, act FormAction, idem string) error {
	a, err := s.access(ctx, profile)
	if err != nil {
		return err
	}
	if err := a.needTinh(); err != nil {
		return err
	}
	pct, err := s.surveyPct(ctx, a)
	if err != nil {
		return err
	}
	act.Note = strings.TrimSpace(act.Note)
	if utf8.RuneCountInString(act.Note) > 1000 {
		return problem.Validation.WithField("note", "tối đa 1000 ký tự")
	}
	return s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if err := store.LockSurveyRound(ctx, tx, a.c.tenant, round); err != nil {
			return err
		}
		rc, err := s.loadReview(ctx, tx, a, round, pct)
		if err != nil {
			return err
		}
		if st := rc.sn.state(s.today()); st != domain.RoundCollecting && st != domain.RoundReviewing {
			return port.ErrSurveyRoundState.WithDetail("Chỉ rà soát phiếu khi đợt Đang thu thập hoặc Đang rà soát")
		}
		i := slices.IndexFunc(rc.sn.forms, func(f store.SurveyForm) bool { return f.FormID == form })
		if i < 0 {
			return port.ErrSurveyFormNotFound
		}
		f := rc.sn.forms[i]
		if act.Seq != 0 && act.Seq != f.Seq {
			return port.ErrSurveyStale
		}
		next, err := applyFormAction(f, rc.devs[f.FormID], act)
		if err != nil {
			return err
		}
		return store.InsertSurveyForm(ctx, tx, ids.New(), a.c.tenant, next, s.Clock.Now(), a.c.ref, idemKey(idem, "form:"+act.Op))
	})
}

// applyFormAction trả phiên bản mới của phiếu theo luồng trạng thái bảng 3.4.
func applyFormAction(f store.SurveyForm, d domain.Deviation, act FormAction) (store.SurveyForm, error) {
	next := f
	next.Op, next.Note = act.Op, act.Note
	switch act.Op {
	case "request_verify":
		if f.Status == domain.FormVerify || f.Status == domain.FormDeleted {
			return f, port.ErrSurveyFormState
		}
		next.Status = domain.FormVerify
	case "record_verify":
		if f.Status != domain.FormVerify {
			return f, port.ErrSurveyFormState.WithDetail("Chỉ ghi nhận xác minh cho phiếu Cần xác minh")
		}
		if act.Unverifiable {
			next.Unverifiable = true
			if next.Note == "" {
				next.Note = "Không xác minh được"
			}
		} else {
			next.Unverifiable = false
		}
		if next.Note == "" {
			return f, problem.Validation.WithField("note", "nội dung xác minh bắt buộc")
		}
		if act.Price != nil {
			if !act.Price.IsPositive() {
				return f, problem.Validation.WithField("price", "giá bán đúng phải > 0")
			}
			p := act.Price.Round(2)
			next.VerifiedPrice = &p
		}
		next.Status = domain.FormVerified
	case "approve":
		if f.Status != domain.FormImported && f.Status != domain.FormVerified {
			return f, port.ErrSurveyFormState.WithDetail("Phiếu Cần xác minh phải ghi nhận kết quả xác minh trước khi duyệt")
		}
		if d.Flag != "" && f.Status != domain.FormVerified && next.Note == "" {
			return f, problem.Validation.WithField("note", "ghi chú bắt buộc khi duyệt phiếu có cờ bất thường")
		}
		next.Status = domain.FormApproved
	case "reject":
		if !domain.FormPending(f.Status) {
			return f, port.ErrSurveyFormState
		}
		if next.Note == "" {
			return f, problem.Validation.WithField("note", "lý do loại bắt buộc")
		}
		next.Status = domain.FormRejected
	case "delete":
		if f.Status == domain.FormDeleted {
			return f, port.ErrSurveyFormState
		}
		if next.Note == "" {
			return f, problem.Validation.WithField("note", "lý do xóa bắt buộc")
		}
		next.Status = domain.FormDeleted
	default:
		return f, problem.Validation.WithField("op", "request_verify | record_verify | approve | reject | delete")
	}
	return next, nil
}

// ApproveUnflagged duyệt hàng loạt các phiếu Đã nhập không có cờ (B4). Trả số phiếu đã duyệt.
func (s *SurveyService) ApproveUnflagged(ctx context.Context, profile string, round uuid.UUID, idem string) (int, error) {
	a, err := s.access(ctx, profile)
	if err != nil {
		return 0, err
	}
	if err := a.needTinh(); err != nil {
		return 0, err
	}
	pct, err := s.surveyPct(ctx, a)
	if err != nil {
		return 0, err
	}
	n := 0
	err = s.tx(ctx, a.c.tenant, func(tx pgx.Tx) error {
		if err := store.LockSurveyRound(ctx, tx, a.c.tenant, round); err != nil {
			return err
		}
		rc, err := s.loadReview(ctx, tx, a, round, pct)
		if err != nil {
			return err
		}
		if st := rc.sn.state(s.today()); st != domain.RoundCollecting && st != domain.RoundReviewing {
			return port.ErrSurveyRoundState
		}
		now := s.Clock.Now()
		for _, f := range rc.sn.forms {
			if f.Status != domain.FormImported || rc.devs[f.FormID].Flag != "" {
				continue
			}
			next := f
			next.Status, next.Op, next.Note = domain.FormApproved, "approve", "Duyệt hàng loạt (không có cờ)"
			if err := store.InsertSurveyForm(ctx, tx, ids.New(), a.c.tenant, next, now, a.c.ref, idemKey(idem, "bulk:"+f.FormID.String())); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

var formStatusVI = map[string]string{domain.FormImported: "Đã nhập", domain.FormVerify: "Cần xác minh", domain.FormVerified: "Đã xác minh",
	domain.FormApproved: "Đã duyệt", domain.FormRejected: "Loại", domain.FormDeleted: "Đã xóa"}

var flagVI = map[string]string{domain.FlagHigh: "Cao bất thường", domain.FlagLow: "Thấp bất thường"}

func pctOpt(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return signedPct(*d)
}

func decOpt(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return d.Round(0).String()
}

// ExportForms xuất danh sách phiếu và cờ bất thường ra Excel (luồng phụ FN-TI-02.12) — KHÔNG họ tên, SĐT nông hộ
// (BR-03 FN-TI-02.11); Cấp Tỉnh, Admin.
func (s *SurveyService) ExportForms(ctx context.Context, profile string, round uuid.UUID) (Export, error) {
	v, err := s.listForms(ctx, profile, round, FormFilter{Limit: 500}, false)
	if err != nil {
		return Export{}, err
	}
	all := v.Rows
	for off := len(all); off < v.Total; off += 500 {
		more, err := s.listForms(ctx, profile, round, FormFilter{Limit: 500, Offset: off}, false)
		if err != nil {
			return Export{}, err
		}
		all = append(all, more.Rows...)
	}
	header := []string{"STT", "Dòng tệp", "Xã", "Nhóm cây", "Mặt hàng", "Giá bán", "Giá gốc", "Trung vị đợt", "% lệch", "Cờ", "Giá thành / kg",
		"Trạng thái"}
	rows := make([][]string, 0, len(all))
	for i, r := range all {
		orig := ""
		if r.Corrected {
			orig = r.OrigPrice.Round(0).String()
		}
		rows = append(rows, []string{strconv.Itoa(i + 1), strconv.Itoa(r.RowNo), r.CommuneName, domain.CropNames[r.Crop], r.ItemName,
			r.Price.Round(0).String(), orig, decOpt(r.Median), pctOpt(r.Pct), flagVI[r.Flag], r.CostPerKg.Round(0).String(), formStatusVI[r.Status]})
	}
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Phiếu điều tra", header, rows); err != nil {
		return Export{}, err
	}
	x := Export{Body: buf.Bytes(), ContentType: tabular.ContentTypeXLSX, FileName: "phieu-dieu-tra-" + round.String()[:8] + ".xlsx", Rows: len(rows)}
	return s.observe(ctx, ProductSurveyFormsExport, x, nil)
}
