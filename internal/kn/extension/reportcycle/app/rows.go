package app

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// Mã vấn đề của engine mẫu (reports/form) mà P4 phân loại: vấn đề "tính" (công thức, ràng buộc) lấy từ lượt tính
// trên dòng đã ghép mọi khe kỳ; còn lại (ô nhập, kỳ đóng, bắt buộc…) lấy từ lượt kiểm dữ liệu người nhập.
var computedIssue = map[string]bool{"reports.formula_error": true, "reports.rule_failed": true, "reports.rule_error": true}

var xrefRe = regexp.MustCompile(`XREF\("([A-Za-z0-9_.-]+)"`)

// formInfo là phiên bản mẫu đã phân tích cho P4.
type formInfo struct {
	ver        repport.FormVersion
	def        repport.FormDefinition
	block      bool
	slotLetter map[int][]string // khe kỳ → chữ cái các cột theo kỳ của khe
	letterSlot map[string]int
	formula    map[string]bool
	xrefCodes  []string
	hasInd     bool
}

func analyze(v repport.FormVersion) formInfo {
	fi := formInfo{ver: v, def: v.Definition, block: len(v.Definition.PeriodBlocks) > 0, slotLetter: map[int][]string{}, letterSlot: map[string]int{},
		formula: map[string]bool{}, hasInd: len(v.Definition.Rows.Indicators) > 0}
	add := func(code string) {
		if code != "" && !slices.Contains(fi.xrefCodes, code) {
			fi.xrefCodes = append(fi.xrefCodes, code)
		}
	}
	for _, c := range v.Definition.Columns {
		if c.PeriodBlock != "" && c.PeriodSlot > 0 {
			fi.slotLetter[c.PeriodSlot] = append(fi.slotLetter[c.PeriodSlot], c.Letter)
			fi.letterSlot[c.Letter] = c.PeriodSlot
		}
		if c.Kind == repport.ColumnFormula {
			fi.formula[c.Letter] = true
			for _, m := range xrefRe.FindAllStringSubmatch(c.Formula, -1) {
				add(m[1])
			}
		}
		if strings.HasPrefix(c.FixedSource, repport.FixedXRefPrefix) {
			parts := strings.Split(strings.TrimPrefix(c.FixedSource, repport.FixedXRefPrefix), ":")
			add(parts[0])
		}
	}
	for _, r := range v.Definition.Rules {
		for _, m := range xrefRe.FindAllStringSubmatch(r.Expr+" "+r.When, -1) {
			add(m[1])
		}
	}
	return fi
}

// formCache đọc phiên bản mẫu một lần mỗi thao tác.
type formCache struct {
	s   *Service
	tid uuid.UUID
	m   map[uuid.UUID]formInfo
}

func (s *Service) forms(tid uuid.UUID) *formCache {
	return &formCache{s: s, tid: tid, m: map[uuid.UUID]formInfo{}}
}

func (c *formCache) get(ctx context.Context, versionID uuid.UUID) (formInfo, error) {
	if fi, ok := c.m[versionID]; ok {
		return fi, nil
	}
	if c.s.Forms == nil {
		return formInfo{}, ErrDependency.WithDetail("chưa nối engine mẫu báo cáo (pf/reports)")
	}
	v, err := c.s.Forms.FormVersion(ctx, c.tid, versionID)
	if err != nil {
		return formInfo{}, err
	}
	fi := analyze(v)
	c.m[versionID] = fi
	return fi, nil
}

// rowKey chuẩn hóa khóa dòng: mã xã hoặc mã xã/chỉ tiêu.
func rowKey(commune, indicator string) string {
	if indicator == "" {
		return commune
	}
	return commune + "/" + indicator
}

// systemCells điền ô cố định tên Khu vực/Xã (theo cấu trúc lúc nhập/chạy) và ô Tỉnh giao.
func systemCells(fi formInfo, communeName, regionName string, provincial map[string]string) map[string]string {
	out := map[string]string{}
	for _, c := range fi.def.Columns {
		switch {
		case c.Kind == repport.ColumnFixed && c.FixedSource == repport.FixedCommuneName && communeName != "":
			out[c.Letter] = communeName
		case c.Kind == repport.ColumnFixed && c.FixedSource == repport.FixedRegionName && regionName != "":
			out[c.Letter] = regionName
		case c.Kind == repport.ColumnProvincial:
			if v, ok := provincial[c.Letter]; ok {
				out[c.Letter] = v
			}
		}
	}
	return out
}

// latestRows trả các dòng của bài nộp dùng làm nguồn: bản "Đã gửi" mới nhất, nếu chưa có thì (khi allowDraft)
// bản mới nhất.
func latestRows(ctx context.Context, tx pgx.Tx, t store.Task, allowDraft bool) (*store.Submission, error) {
	id := t.SubmittedSubmissionID
	if id == nil && allowDraft {
		id = t.LatestSubmissionID
	}
	if id == nil {
		return nil, nil
	}
	return store.SubmissionByID(ctx, tx, *id)
}

// xrefs dựng tham chiếu liên biểu cùng xã cùng kỳ (vd T03 ↔ T01 cột M): ô đã tính của bản gửi mới nhất.
func xrefs(ctx context.Context, tx pgx.Tx, fi formInfo, periodID uuid.UUID, commune string) (map[string]map[string]string, error) {
	if len(fi.xrefCodes) == 0 {
		return nil, nil
	}
	out := map[string]map[string]string{}
	for _, code := range fi.xrefCodes {
		t, err := store.TaskForFormInPeriod(ctx, tx, periodID, code, commune)
		if err != nil {
			return nil, err
		}
		if t == nil {
			continue
		}
		sub, err := latestRows(ctx, tx, *t, true)
		if err != nil {
			return nil, err
		}
		if sub != nil && len(sub.Rows) > 0 {
			out[code] = sub.Rows[0].Cells
		}
	}
	return out, nil
}

// carried trả giá trị các khe kỳ KHÁC của mẫu nhiều kỳ (PL 01, 01B, 02, 07) từ bản gửi mới nhất của công việc kỳ
// đó: khóa dòng → chữ cái → giá trị. maxSlot > 0: chỉ khe ≤ maxSlot (tổng hợp kỳ n không lấy kỳ sau).
func carried(ctx context.Context, tx pgx.Tx, fi formInfo, t store.Task, maxSlot int) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	if !fi.block {
		return out, nil
	}
	sibs, err := store.SiblingTasks(ctx, tx, t.BatchID, t.TemplateID, t.CommuneCode)
	if err != nil {
		return nil, err
	}
	for _, sib := range sibs {
		if sib.ID == t.ID || (maxSlot > 0 && sib.Slot > maxSlot) {
			continue
		}
		sub, err := latestRows(ctx, tx, sib, true)
		if err != nil {
			return nil, err
		}
		if sub == nil {
			continue
		}
		for _, r := range sub.Rows {
			k := rowKey(t.CommuneCode, r.Indicator)
			for _, l := range fi.slotLetter[sib.Slot] {
				if v, ok := r.Input[l]; ok && v != "" {
					if out[k] == nil {
						out[k] = map[string]string{}
					}
					out[k][l] = v
				}
			}
		}
	}
	return out, nil
}

// evalRows kiểm + tính dòng của một xã theo mẫu (FN-XA-03.02): chỉ khe kỳ của công việc được nhập, công thức tính
// trên dòng đã ghép giá trị các khe kỳ khác. Trả dòng (ô nhập đã ghép + ô đã tính), vấn đề, hợp lệ.
func (s *Service) evalRows(fi formInfo, mode string, t store.Task, input []store.SubRow, carry map[string]map[string]string,
	system map[string]string, xr map[string]map[string]string, region string) ([]store.SubRow, []repport.CellIssue, bool) {
	mk := func(rows []store.SubRow, strip bool) []repport.EvalRow {
		out := make([]repport.EvalRow, 0, len(rows))
		for _, r := range rows {
			k := rowKey(t.CommuneCode, r.Indicator)
			in := map[string]string{}
			for l, v := range r.Input {
				l = strings.ToUpper(strings.TrimSpace(l))
				if strip {
					// Ô của khe kỳ khác trùng giá trị đã lưu: client gửi kèm cả dòng — bỏ qua (không phải sửa).
					if slot, ok := fi.letterSlot[l]; ok && slot != t.Slot && carry[k][l] == v {
						continue
					}
				}
				in[l] = v
			}
			out = append(out, repport.EvalRow{Key: k, Commune: t.CommuneCode, Region: region, Indicator: r.Indicator, Input: in, System: system, XRefs: xr})
		}
		return out
	}
	if !fi.block {
		res := s.Forms.Evaluate(fi.def, repport.EvalInput{Mode: mode, Rows: mk(input, false)})
		return toSubRows(res.Rows, mk(input, false)), res.Issues, res.Valid
	}
	a := s.Forms.Evaluate(fi.def, repport.EvalInput{Mode: mode, OpenSlots: []int{t.Slot}, Rows: mk(input, true)})
	merged := mk(input, true)
	for i := range merged {
		for l := range merged[i].Input {
			if slot, ok := fi.letterSlot[l]; ok && slot != t.Slot {
				delete(merged[i].Input, l) // khe kỳ khác: lấy giá trị đã lưu
			}
		}
		for l, v := range carry[merged[i].Key] {
			merged[i].Input[l] = v
		}
	}
	b := s.Forms.Evaluate(fi.def, repport.EvalInput{Mode: repport.EvalDraft, Rows: merged})
	var issues []repport.CellIssue
	for _, is := range a.Issues {
		if !computedIssue[is.Code] {
			issues = append(issues, is)
		}
	}
	for _, is := range b.Issues {
		if computedIssue[is.Code] {
			issues = append(issues, is)
		}
	}
	valid := !slices.ContainsFunc(issues, func(i repport.CellIssue) bool { return i.Severity == repport.SeverityError })
	return toSubRows(b.Rows, merged), issues, valid
}

func toSubRows(res []repport.EvalRowResult, in []repport.EvalRow) []store.SubRow {
	inputs := map[string]map[string]string{}
	for _, r := range in {
		inputs[r.Key] = r.Input
	}
	out := make([]store.SubRow, 0, len(res))
	for _, r := range res {
		cells := r.Cells
		if cells == nil {
			cells = map[string]string{}
		}
		inp := inputs[r.Key]
		if inp == nil {
			inp = map[string]string{}
		}
		out = append(out, store.SubRow{Key: r.Key, Indicator: r.Indicator, Input: inp, Cells: cells})
	}
	return out
}
