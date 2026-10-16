package port

import (
	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/extensionsrc"
)

// Transitional aliases let all KN callers move before the capability declarations
// are removed in the next step. No platform package imports KN.
type ReportQuery = extensionsrc.ReportQuery
type Completeness = extensionsrc.Completeness
type SowingRow = extensionsrc.SowingRow
type SowingData = extensionsrc.SowingData
type PestRow = extensionsrc.PestRow
type PestData = extensionsrc.PestData
type ProgramRow = extensionsrc.ProgramRow
type ProgramData = extensionsrc.ProgramData
type TaskRow = extensionsrc.TaskRow
type TaskData = extensionsrc.TaskData
type SummaryRow = extensionsrc.SummaryRow
type SummaryData = extensionsrc.SummaryData
type ReportData = extensionsrc.ReportData

const (
	SourceSowing          = reportsport.SourceSowing
	SourceGrowthPest      = reportsport.SourceGrowthPest
	SourceProgramProgress = reportsport.SourceProgramProgress
	SourceTasksSLA        = reportsport.SourceTasksSLA
	SourcePeriodSummary   = reportsport.SourcePeriodSummary
)
