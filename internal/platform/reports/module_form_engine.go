// Package-level pure form operations for composition roots.
// These functions keep callers outside the capability's implementation packages.
package reports

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/form"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formimport"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

type FormOp = form.Op
type FormMeta = form.Meta
type FormOpError = form.OpError
type FormValidationError = form.ValidationError
type FormLayoutOptions = form.LayoutOptions

const (
	FormCodeFormulaCycle      = form.CodeFormulaCycle
	FormCodeFormulaInvalidRef = form.CodeFormulaInvalidRef
	FormIssueInvalidType      = form.IssueInvalidType
	FormIssueNotEditable      = form.IssueNotEditable
	FormIssueOutOfRange       = form.IssueOutOfRange
	FormIssuePeriodClosed     = form.IssuePeriodClosed
	FormIssueRequired         = form.IssueRequired
	FormIssueRuleFailed       = form.IssueRuleFailed
	FormIssueUnknownColumn    = form.IssueUnknownColumn
	FormOpAddIndicator        = form.OpAddIndicator
	FormOpDeleteColumn        = form.OpDeleteColumn
	FormOpDeleteHeaderRow     = form.OpDeleteHeaderRow
	FormOpDeleteIndicator     = form.OpDeleteIndicator
	FormOpInsertColumn        = form.OpInsertColumn
	FormOpInsertHeaderRow     = form.OpInsertHeaderRow
	FormOpMerge               = form.OpMerge
	FormOpSetGroups           = form.OpSetGroups
	FormOpSetHeaderCell       = form.OpSetHeaderCell
	FormOpSetMeta             = form.OpSetMeta
	FormOpSetPeriodBlock      = form.OpSetPeriodBlock
	FormOpSetRule             = form.OpSetRule
	FormOpSetTotals           = form.OpSetTotals
	FormOpUnmerge             = form.OpUnmerge
	FormOpUpdateColumn        = form.OpUpdateColumn
)

// ErrFormImportInvalid identifies unreadable or unrecognized workbook layouts.
var ErrFormImportInvalid = formimport.ErrInvalid

func ValidateForm(def port.FormDefinition) error { return form.Validate(def) }
func EvaluateForm(def port.FormDefinition, in port.EvalInput) port.EvalResult {
	return form.Evaluate(def, in)
}
func FormTotals(def port.FormDefinition, rows []port.EvalRowResult) []port.TotalRowResult {
	return form.Totals(def, rows)
}
func ApplyForm(def port.FormDefinition, ops []FormOp) (port.FormDefinition, error) {
	return form.Apply(def, ops)
}
func CloneForm(def port.FormDefinition) port.FormDefinition { return form.Clone(def) }
func LayoutForm(def port.FormDefinition, options FormLayoutOptions) port.FormDefinition {
	return form.Layout(def, options)
}
func ToFormRowFormula(source string, dataRow int) (string, error) {
	return formimport.ToRowFormula(source, dataRow)
}
