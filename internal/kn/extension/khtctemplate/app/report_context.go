package app

import (
	"sort"
	"strings"

	report "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	source "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/shopspring/decimal"
)

// ContextFromDraft projects the same approved inputs used by the report, without
// copying raw module records. This pure adapter is shared by preview and integration.
func ContextFromDraft(d report.Draft, terminal string) port.KHTCTemplateContext {
	data := d.Data
	c := port.KHTCTemplateContext{PeriodID: data.Period.ID, PeriodName: data.Period.Name, PeriodType: data.Period.Type, PeriodStart: data.Period.From, PlanVersionID: data.Plan.VersionID,
		Values: map[string]string{}, Lists: map[string][]port.KHTCTemplateRow{}, Texts: map[string]string{}, Provisional: append([]string{}, data.Provisional...), InputHash: data.InputHash}
	for key, v := range d.Admin {
		c.Values["admin."+key] = v.Value()
	}
	for key, v := range d.Texts {
		c.Texts[key] = v
	}
	programNames := map[string]string{}
	for _, entry := range data.Plan.Entries {
		if entry.Kind == "program" {
			programNames[entry.Ref.String()] = entry.Name
			if entry.ProgramRef != nil {
				programNames[entry.ProgramRef.String()] = entry.Name
			}
		}
	}
	parents := map[string]port.KHTCPlanEntry{}
	for _, row := range data.Rows {
		if row.Kind == "program" {
			programNames[row.Ref.String()] = row.Name
			if row.ProgramRef != nil {
				programNames[row.ProgramRef.String()] = row.Name
			}
		}
	}
	for _, entry := range data.Plan.Entries {
		parents[entry.Ref.String()] = entry
	}
	for _, row := range data.Rows {
		programRef := ""
		if row.ProgramRef != nil {
			programRef = row.ProgramRef.String()
		}
		if programRef == "" {
			at := row.Ref.String()
			for depth := 0; depth < 8; depth++ {
				entry, ok := parents[at]
				if !ok {
					break
				}
				if entry.Kind == "program" {
					programRef = entry.Ref.String()
					if entry.ProgramRef != nil {
						programRef = entry.ProgramRef.String()
					}
					break
				}
				if entry.ParentRef == nil {
					break
				}
				at = entry.ParentRef.String()
			}
		}
		parentRef := ""
		if row.ParentRef != nil {
			parentRef = row.ParentRef.String()
		}
		values := emptyValues()
		values["tab"] = row.Tab
		values["plan.item_name"] = row.Name
		values["plan.program_name"] = programNames[programRef]
		values["plan.roman"] = row.Roman
		values["plan.unit"] = row.Unit
		values["plan.department"] = row.Department
		if row.Planned != nil {
			values["plan.planned"] = *row.Planned
		}
		values["calculated.year_to_date"] = row.YearToDate
		if row.Percent != nil {
			values["calculated.percent"] = *row.Percent
		}
		origin := "form"
		if row.SourceKey != "" {
			origin = "module"
		}
		values["data_source"] = origin
		values[origin+".value"] = row.InPeriod
		values[origin+".content"] = row.Content
		values[origin+".unit"] = row.Unit
		values[origin+".label"] = row.Name
		values[origin+".department"] = row.Department
		values[origin+".status"] = row.Status
		values[origin+".people"] = row.People
		values[origin+".budget"] = row.Budget
		status := "normal"
		if row.Missing {
			status = "no_send"
			if origin == "module" {
				status = "provisional"
				if terminal == "closed" {
					status = "unclosed"
				}
			}
		} else if row.InPeriod == "0" && row.Content == "" {
			status = "no_activity"
		}
		r := port.KHTCTemplateRow{Ref: row.Ref.String(), ParentRef: parentRef, ProgramRef: programRef, Department: row.Department, Status: status, Values: values}
		switch row.Kind {
		case "item":
			c.Lists["plan_item"] = append(c.Lists["plan_item"], r)
		case "program":
			c.Lists["program"] = append(c.Lists["program"], r)
		}
	}
	hasAquacultureTotal := false
	for _, m := range data.Metrics {
		if m.Form == "cnts-bm18" || m.Form == "bm-18" {
			hasAquacultureTotal = true
		}
	}
	for _, m := range data.Metrics {
		v := emptyValues()
		v["data_source"] = "module"
		v["module.label"] = m.Label
		v["module.unit"] = m.Unit
		v["module.department"] = m.Department
		v["calculated.year_to_date"] = decimalString(m.Value.YearToDate)
		amount := m.Value.Increment
		if m.Value.Sum != nil {
			amount = m.Value.Sum
		}
		if m.Stock {
			amount = m.Value.Cumulative
			v["calculated.year_to_date"] = ""
		}
		v["module.value"] = decimalString(amount)
		status := "normal"
		if m.Value.Provisional() {
			status = "provisional"
			if terminal == "closed" {
				status = "unclosed"
			}
		} else if m.Value.NoSource {
			status = "no_activity"
		}
		section := "A.I.1"
		if m.Department == "chan_nuoi_thuy_san" {
			section = "A.I.3"
			if m.Stock {
				section = "A.I.2"
			}
		}
		v["section"] = section
		r := port.KHTCTemplateRow{Ref: m.Key, Department: m.Department, Status: status, Values: v}
		if m.Value.Mode == source.ModePrice {
			v["module.from_price"] = decimalString(m.Value.Min)
			v["module.to_price"] = decimalString(m.Value.Max)
			if days := m.Value.PriceDays; len(days) > 0 {
				last := days[len(days)-1]
				v["module.from_price"] = last.Min.String()
				v["module.to_price"] = last.Max.String()
			}
			c.Lists["commodity"] = append(c.Lists["commodity"], r)
		} else if section != "A.I.3" || !hasAquacultureTotal || m.Form != "cnts-bm17" && m.Form != "bm-17" {
			c.Lists["metric"] = append(c.Lists["metric"], r)
		}
	}
	departments := append([]string{}, data.Period.Departments...)
	sort.Strings(departments)
	submitted := map[string]bool{}
	for _, s := range data.Submissions {
		submitted[s.Department] = true
	}
	for _, dept := range departments {
		sections := []string{"A.II.3.2", "I.3.2"}
		if dept == "hanh_chinh_tong_hop" {
			sections = []string{"A.IV", "II.1", "II.2"}
		}
		fields := data.FormTexts[dept]
		keys := make([]string, 0, len(fields))
		for key := range fields {
			if key != "kho_khan" && key != "nhiem_vu_ky_toi" && key != "nhiem_vu_cchc" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, section := range sections {
			added := false
			for _, key := range keys {
				if strings.TrimSpace(fields[key]) == "" {
					continue
				}
				v := emptyValues()
				v["section"] = section
				v["form.department"] = dept
				v["form.content"] = fields[key]
				c.Lists["form_bullet"] = append(c.Lists["form_bullet"], port.KHTCTemplateRow{Ref: dept + ":" + section + ":" + key, Department: dept, Status: "normal", Values: v})
				added = true
			}
			if !added {
				v := emptyValues()
				v["section"] = section
				v["form.department"] = dept
				status := "no_send"
				if submitted[dept] {
					status = "no_activity"
				}
				c.Lists["form_bullet"] = append(c.Lists["form_bullet"], port.KHTCTemplateRow{Ref: dept + ":" + section, Department: dept, Status: status, Values: v})
			}
		}
	}
	return c
}

func decimalString(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return d.String()
}
func emptyValues() map[string]string {
	v := map[string]string{}
	for _, source := range []string{"plan", "form", "module", "calculated"} {
		for _, field := range []string{"item_name", "program_name", "roman", "planned", "unit", "department", "value", "content", "label", "status", "people", "budget", "from_price", "to_price", "year_to_date", "percent"} {
			if domainField(source, field) {
				v[source+"."+field] = ""
			}
		}
	}
	return v
}
func domainField(source, field string) bool {
	// Keep the projection whitelist identical to the authoring registry.
	return domain.AllowedField(source, field)
}
