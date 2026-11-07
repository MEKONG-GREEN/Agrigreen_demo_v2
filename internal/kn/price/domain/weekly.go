package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// WeeklyCatalogItem is a plan row from the official 2026 program workbook.
type WeeklyCatalogItem struct {
	Code, Parent, Name, Plan string
}

// WeeklyCatalog contains only the two programs covered by FN-TI-02.13.
// Source: BÁO CÁO TIẾN ĐỘ 12 CT KHUYẾN NÔNG.xlsx, sheets I and II,
// Quyết định 2855/QĐ-UBND ngày 19/6/2026. Weekly results in that file are not seeded.
func WeeklyCatalog(program string) []WeeklyCatalogItem {
	if program == "training" {
		return []WeeklyCatalogItem{
			{"train_staff", "", "Tập huấn nâng cao cho cán bộ/viên chức khuyến nông (lớp)", "7"},
			{"train_tot", "", "Tham dự Tập huấn ToT do Trung ương, Viện, Trường tổ chức (người)", "30"},
			{"train_community", "", "Tập huấn viên chức khuyến nông, Tổ KNCĐ (lớp)", "10"},
			{"train_farmers", "", "Tập huấn nông dân chủ chốt (lớp)", "12"},
			{"train_local", "", "Tập huấn theo nhu cầu địa phương (lớp)", "10"},
			{"train_exchange", "", "Học tập, trao đổi kinh nghiệm (cuộc)", "3"},
		}
	}
	if program != "information" {
		return nil
	}
	return []WeeklyCatalogItem{
		{"info_ag", "", "Tuyên truyền với Báo và PTTH An Giang", ""},
		{"info_tv", "info_ag", "Truyền hình (chuyên mục)", "28"},
		{"info_sea", "info_ag", "Câu chuyện kinh tế biển (câu chuyện)", "4"},
		{"info_summary", "info_ag", "Video clip phóng sự tổng kết (clip)", "1"},
		{"info_news", "info_ag", "Trang báo điện tử An Giang (số báo)", "28"},
		{"info_nnmt", "", "Thông tin trên Báo Nông nghiệp và MT", ""},
		{"info_nnmt_news", "info_nnmt", "Bài viết trên trang báo điện tử NNMT", "7"},
		{"info_nnmt_video", "info_nnmt", "Video clip", "3"},
		{"info_danviet", "", "Bài viết trên Báo điện tử Dân Việt", "4"},
		{"info_bulletin", "", "Bản tin số Khuyến nông và thị trường", "12"},
		{"info_prices", "", "Thu thập thông tin giá cả nông sản (phiếu)", "6552"},
		{"info_web", "", "Trang Website Khuyến nông (tin, bài/quý)", "510"},
		{"info_youtube", "", "Short video trên kênh Youtube KN (video)", "65"},
		{"info_print", "", "In tài liệu kỹ thuật (cuốn)", "2000"},
		{"info_visits", "", "Dẫn đoàn tham quan Hội thảo, hội nghị... (chuyến)", "5"},
		{"info_talk", "", "Tọa đàm chuyên đề (tọa đàm)", "4"},
		{"info_harvest", "", "Tổ chức, tham quan ngày hội thu hoạch (cuộc)", "4"},
		{"info_skills", "", "Tập huấn phương pháp, kỹ năng phục vụ công tác TTTT (lớp)", "3"},
		{"info_contest", "", "Hội thi kỹ năng khuyến nông... (hội thi)", "1"},
	}
}

// WeeklyTitle returns the approved program name, or an empty string for an unknown program.
func WeeklyTitle(program string) string {
	switch program {
	case "information":
		return "Chương trình thông tin"
	case "training":
		return "Chương trình đào tạo huấn luyện và hợp tác"
	}
	return ""
}

var weeklyNumber = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// ParseWeeklyNumber bounds ordinary decimal syntax before invoking the decimal parser.
// Quantity and plan allow four fractional digits; money is an integer number of đồng.
func ParseWeeklyNumber(value string, money bool) (decimal.Decimal, error) {
	if len(value) == 0 || len(value) > 24 || !weeklyNumber.MatchString(value) {
		return decimal.Zero, fmt.Errorf("phải là số thập phân không âm, không dùng ký hiệu mũ")
	}
	p := strings.SplitN(value, ".", 2)
	maxDigits := 15
	if money {
		maxDigits = 18
	}
	if len(p[0]) > maxDigits || (money && len(p) > 1) || (len(p) > 1 && len(p[1]) > 4) {
		return decimal.Zero, fmt.Errorf("vượt giới hạn %d chữ số%s", maxDigits, map[bool]string{true: ", kinh phí tính bằng đồng", false: ", tối đa 4 số lẻ"}[money])
	}
	return decimal.NewFromString(value)
}

// WeeklyItem is a row snapshot. Opening balances are copied exactly from the prior closed week.
type WeeklyItem struct {
	ID              string           `json:"id"`
	ParentID        string           `json:"parent_id"`
	Name            string           `json:"name"`
	Group           bool             `json:"group"`
	Plan            decimal.Decimal  `json:"plan"`
	AdjustedPlan    *decimal.Decimal `json:"adjusted_plan"`
	PlanReason      string           `json:"plan_reason"`
	OpeningProgress decimal.Decimal  `json:"opening_progress"`
	OpeningMoney    decimal.Decimal  `json:"opening_money"`
	Progress        decimal.Decimal  `json:"progress"`
	Money           decimal.Decimal  `json:"money"`
	ProgressSet     bool             `json:"progress_set"`
	MoneySet        bool             `json:"money_set"`
	MoneyOverride   *decimal.Decimal `json:"money_override"`
	Evaluation      string           `json:"evaluation"`
	Note            string           `json:"note"`
}

// Cumulative returns current balances and the percentage, with nil for a zero plan or group.
func (i WeeklyItem) Cumulative() (progress, money decimal.Decimal, rate *decimal.Decimal) {
	if i.Group {
		return decimal.Zero, decimal.Zero, nil
	}
	progress, money = i.OpeningProgress.Add(i.Progress), i.OpeningMoney.Add(i.Money)
	if i.MoneyOverride != nil {
		money = *i.MoneyOverride
	}
	plan := i.Plan
	if i.AdjustedPlan != nil {
		plan = *i.AdjustedPlan
	}
	if plan.IsPositive() {
		r := progress.Mul(decimal.NewFromInt(100)).DivRound(plan, 2)
		rate = &r
	}
	return progress, money, rate
}

// HasData is used before converting a leaf into a group or confirming deletion.
func (i WeeklyItem) HasData() bool {
	return i.ProgressSet || i.MoneySet || !i.Progress.IsZero() || !i.Money.IsZero() || !i.OpeningProgress.IsZero() || !i.OpeningMoney.IsZero() ||
		i.MoneyOverride != nil || i.Evaluation != "" || i.Note != ""
}

// WeeklyIssue is a row result saved with an Excel import and downloadable as a workbook.
type WeeklyIssue struct {
	Line   int      `json:"line"`
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Errors []string `json:"errors"`
}

// WeeklyReport contains the immutable input snapshot of one reporting week.
type WeeklyReport struct {
	ID           string        `json:"id"`
	Number       int           `json:"number"`
	Key          string        `json:"key"`
	From         string        `json:"from"`
	To           string        `json:"to"`
	Status       string        `json:"status"`
	Items        []WeeklyItem  `json:"items,omitempty"`
	ImportIssues []WeeklyIssue `json:"import_issues"`
}

// WeeklyBook is the program ledger payload. Tombstones affect projections, including old weeks,
// while each closed report's input bytes remain untouched in the ledger history.
type WeeklyBook struct {
	Reports []WeeklyReport  `json:"reports"`
	Deleted map[string]bool `json:"deleted"`
}

// WeeklyRow adds the derived values displayed by the API and the Excel export.
type WeeklyRow struct {
	WeeklyItem
	Number             string           `json:"number"`
	CumulativeProgress decimal.Decimal  `json:"cumulative_progress"`
	CumulativeMoney    decimal.Decimal  `json:"cumulative_money"`
	Rate               *decimal.Decimal `json:"rate"`
	Achieved           bool             `json:"achieved"`
}

// WeeklyProjection is computed from the selected week and the current tombstones.
type WeeklyProjection struct {
	WeeklyReport
	Rows       []WeeklyRow     `json:"rows"`
	ItemCount  int             `json:"item_count"`
	Achieved   int             `json:"achieved"`
	TotalMoney decimal.Decimal `json:"total_money"`
}

// ProjectWeekly filters tombstones, renumbers main items and sums leaf amounts exactly once.
func ProjectWeekly(report WeeklyReport, deleted map[string]bool) WeeklyProjection {
	p := WeeklyProjection{WeeklyReport: report, Rows: []WeeklyRow{}}
	p.Items = nil
	n := 0
	for _, item := range report.Items {
		if deleted[item.ID] {
			continue
		}
		row := WeeklyRow{WeeklyItem: item}
		if item.ParentID == "" {
			n++
			row.Number = fmt.Sprint(n)
		}
		row.CumulativeProgress, row.CumulativeMoney, row.Rate = item.Cumulative()
		row.Achieved = row.Rate != nil && row.Rate.GreaterThanOrEqual(decimal.NewFromInt(100))
		if !item.Group {
			p.ItemCount++
			p.TotalMoney = p.TotalMoney.Add(row.CumulativeMoney)
			if row.Achieved {
				p.Achieved++
			}
		}
		p.Rows = append(p.Rows, row)
	}
	return p
}

// WeeklyPatch changes explicitly supplied fields; empty Excel cells are omitted.
type WeeklyPatch struct {
	ID            string  `json:"id"`
	Progress      *string `json:"progress"`
	Money         *string `json:"money"`
	MoneyOverride *string `json:"money_override"`
	ClearOverride bool    `json:"clear_override"`
	Evaluation    *string `json:"evaluation"`
	Note          *string `json:"note"`
}

// ApplyWeeklyPatches validates the whole manual save before it can be appended to the ledger.
func ApplyWeeklyPatches(report *WeeklyReport, deleted map[string]bool, program string, patches []WeeklyPatch) error {
	seen := map[string]bool{}
	for _, patch := range patches {
		at := slices.IndexFunc(report.Items, func(i WeeklyItem) bool { return i.ID == patch.ID && !deleted[i.ID] })
		if at < 0 || seen[patch.ID] {
			return fmt.Errorf("hạng mục không tồn tại hoặc trùng dòng")
		}
		seen[patch.ID] = true
		item := &report.Items[at]
		if item.Group {
			return fmt.Errorf("%s: mục nhóm không nhập số liệu", item.Name)
		}
		for _, number := range []struct {
			input  *string
			target *decimal.Decimal
			set    *bool
			money  bool
		}{{patch.Progress, &item.Progress, &item.ProgressSet, false}, {patch.Money, &item.Money, &item.MoneySet, true}} {
			if number.input != nil {
				value, err := ParseWeeklyNumber(*number.input, number.money)
				if err != nil {
					return fmt.Errorf("%s: %w", item.Name, err)
				}
				*number.target = value
				*number.set = true
			}
		}
		if patch.MoneyOverride != nil && patch.ClearOverride {
			return fmt.Errorf("%s: không đồng thời ghi và xóa kinh phí lũy kế", item.Name)
		}
		if patch.ClearOverride {
			item.MoneyOverride = nil
		}
		if patch.MoneyOverride != nil {
			value, err := ParseWeeklyNumber(*patch.MoneyOverride, true)
			if err != nil {
				return fmt.Errorf("%s: %w", item.Name, err)
			}
			item.MoneyOverride = &value
		}
		for _, field := range []struct {
			input  *string
			target *string
		}{{patch.Evaluation, &item.Evaluation}, {patch.Note, &item.Note}} {
			if field.input == nil {
				continue
			}
			if utf8.RuneCountInString(*field.input) > 2000 || !utf8.ValidString(*field.input) {
				return fmt.Errorf("%s: đánh giá/ghi chú tối đa 2000 ký tự", item.Name)
			}
			*field.target = strings.TrimSpace(*field.input)
		}
		if program != "information" && item.Note != "" {
			return fmt.Errorf("chỉ Chương trình thông tin có cột Ghi chú")
		}
		progress, money, _ := item.Cumulative()
		if progress.GreaterThan(decimal.RequireFromString("999999999999999.9999")) || money.GreaterThan(decimal.RequireFromString("999999999999999999")) {
			return fmt.Errorf("%s: lũy kế vượt giới hạn lưu số liệu", item.Name)
		}
	}
	return nil
}
