package domain

import (
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	khtcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtc/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
)

// Row là một dòng phiếu = một hạng mục phòng phụ trách trong Kế hoạch năm (FN-PN-03.02 "dòng mẫu các CT sinh từ Kế hoạch
// năm, không nhập tay").
type Row struct {
	EntryRef   uuid.UUID
	No         int
	Code       string
	Name       string
	Unit       string
	PlannedQty *decimal.Decimal
	ScaleNote  string
	// ModuleSourced: hạng mục khai báo nguồn module (BR-DL-10) — ô "Thực hiện trong kỳ" do hệ thống điền, phòng không nhập.
	ModuleSourced bool
	SourceKey     string
	// Module: số của hạng mục nguồn module từ ảnh chụp "Cập nhật số liệu" của kỳ (W11.64) — nil = chưa cập nhật.
	Module *ModuleValue
}

// ModuleValue là số của hạng mục nguồn module (W11.64 khtcsource): trong kỳ, lũy kế, cờ "Tạm tính" (BR-DL-03).
type ModuleValue struct {
	InPeriod    *decimal.Decimal // cum/season: lũy kế − gốc; sum: tổng phát sinh
	Cumulative  *decimal.Decimal // lũy kế năm (cum) hoặc vụ (season)
	Provisional bool
}

// Tab là một tab chương trình / CT-DA của phiếu (BRD: "Tab chương trình = các chương trình có hạng mục do phòng phụ
// trách trong Kế hoạch năm có hiệu lực; không gắn cứng mã chương trình").
type Tab struct {
	Key   string // mã dòng CT/DA trong kế hoạch
	Label string // "CT III: …", "Dự án TW: …"
	Plan  string // tab kế hoạch (programs, prev_year, central_project…)
	Rows  []Row
}

// TrackedIn: hạng mục còn theo dõi trong kỳ bắt đầu từ ngày from — ngừng từ ngày X thì kỳ bắt đầu từ X trở đi không còn
// (BR-DL-21: tuần sau ngày ngừng không có, quý chứa ngày ngừng vẫn có).
func TrackedIn(e port.KHTCPlanEntry, from time.Time) bool {
	if e.Tracking != "stopped" {
		return true
	}
	return e.StoppedFrom != nil && e.StoppedFrom.After(from)
}

// BuildTabs dựng tab và dòng phiếu của phòng dept từ phiên bản Kế hoạch năm (thứ tự theo tab kế hoạch rồi thứ tự dòng).
// names: tên CT/DA theo program_ref (dòng CT không ghi tên riêng thì lấy tên danh sách chủ).
func BuildTabs(plan port.KHTCPlan, dept string, from time.Time, names map[uuid.UUID]string) []Tab {
	byRef := make(map[uuid.UUID]port.KHTCPlanEntry, len(plan.Entries))
	for _, e := range plan.Entries {
		byRef[e.Ref] = e
	}
	top := func(e port.KHTCPlanEntry) (port.KHTCPlanEntry, bool) {
		for i := 0; i < 16 && e.ParentRef != nil; i++ {
			p, ok := byRef[*e.ParentRef]
			if !ok {
				return e, false
			}
			e = p
		}
		return e, e.Kind == "program"
	}
	tabs := map[uuid.UUID]*Tab{}
	var order []uuid.UUID
	for _, e := range plan.Entries {
		if e.Kind != "item" || e.Department != dept || !TrackedIn(e, from) {
			continue
		}
		p, ok := top(e)
		if !ok || !TrackedIn(p, from) {
			continue
		}
		t := tabs[p.Ref]
		if t == nil {
			t = &Tab{Key: p.Ref.String(), Label: tabLabel(p, names), Plan: p.Tab}
			tabs[p.Ref] = t
			order = append(order, p.Ref)
		}
		r := Row{EntryRef: e.Ref, No: len(t.Rows) + 1, Code: e.Code, Name: e.Name, Unit: e.UnitCode, PlannedQty: e.PlannedQty,
			ScaleNote: e.ScaleNote, ModuleSourced: e.DataSource == "module" || e.SourceKey != "", SourceKey: e.SourceKey}
		t.Rows = append(t.Rows, r)
	}
	out := make([]Tab, 0, len(order))
	for _, tab := range khtcdomain.Tabs {
		for _, ref := range order {
			if tabs[ref].Plan == tab {
				out = append(out, *tabs[ref])
			}
		}
	}
	for _, ref := range order { // tab kế hoạch lạ (phiên bản sau) — giữ cuối
		if !slices.Contains(khtcdomain.Tabs, tabs[ref].Plan) {
			out = append(out, *tabs[ref])
		}
	}
	return out
}

func tabLabel(p port.KHTCPlanEntry, names map[uuid.UUID]string) string {
	name := p.Name
	if name == "" && p.ProgramRef != nil {
		name = names[*p.ProgramRef]
	}
	if name == "" {
		name = p.Code
	}
	if p.Tab == khtcdomain.TabPrograms && p.Roman != "" {
		return "CT " + p.Roman + ": " + name
	}
	if l := khtcdomain.TabLabel[p.Tab]; l != "" && p.Tab != khtcdomain.TabPrograms {
		return l + ": " + name
	}
	return name
}

// Rows trả mọi dòng của các tab theo thứ tự.
func Rows(tabs []Tab) []Row {
	var out []Row
	for _, t := range tabs {
		out = append(out, t.Rows...)
	}
	return out
}
