package domain

import (
	"slices"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// NormScopes kiểm và chuẩn hóa scope của một đăng ký sản phẩm: mỗi scope là scope của hợp đồng
// sản phẩm (dp:<product>:read|export) hoặc usage:read. Trả danh sách đã sắp, không trùng; ok =
// false khi có scope lạ hoặc rỗng.
func NormScopes(p *dataproduct.Product, in []string) ([]string, bool) {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != port.ScopeUsageRead && (p == nil || !p.HasScope(s)) {
			return nil, false
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out, len(out) > 0
}

// NormAuthMethods kiểm phương thức xác thực của khách hàng: luôn có api_key (bắt buộc có, ADR-0022
// mục 5); mtls chưa hiện thực nên chưa nhận.
func NormAuthMethods(in []string) ([]string, bool) {
	out := []string{string(port.AuthAPIKey)}
	for _, m := range in {
		switch port.AuthMethod(strings.TrimSpace(m)) {
		case port.AuthAPIKey:
		case port.AuthOAuthClientCredentials:
			if !slices.Contains(out, m) {
				out = append(out, string(port.AuthOAuthClientCredentials))
			}
		default:
			return nil, false
		}
	}
	slices.Sort(out)
	return out, true
}

// Giới hạn trên của gói hạn mức (khớp CHECK của pf_apiclient.quota_plan).
const (
	MaxRequestsPerMinute = 100_000
	MaxDailyUnits        = 1_000_000_000
	MaxMonthlyUnits      = 100_000_000_000
	MaxExportRows        = 100_000_000
)

// PlanProblem trả lý do gói hạn mức không hợp lệ ("" = hợp lệ) và tên trường.
func PlanProblem(p port.Plan) (field, reason string) {
	switch {
	case p.RequestsPerMinute < 1 || p.RequestsPerMinute > MaxRequestsPerMinute:
		return "requests_per_minute", "1–100000"
	case p.DailyUnits < 1 || p.DailyUnits > MaxDailyUnits:
		return "daily_units", "1–1000000000"
	case p.MonthlyUnits < 1 || p.MonthlyUnits > MaxMonthlyUnits:
		return "monthly_units", "1–100000000000"
	case p.DailyUnits > p.MonthlyUnits:
		return "daily_units", "không lớn hơn monthly_units"
	case p.MaxExportRows < 1 || p.MaxExportRows > MaxExportRows:
		return "max_export_rows", "1–100000000"
	}
	return "", ""
}

// DefaultPlan là gói hạn mức mặc định khi khách hàng chưa có phiên bản nào được duyệt (Version 0 —
// "tạm, chờ khách", g5-conventions §1.2).
func DefaultPlan(clientID [16]byte) port.Plan {
	return port.Plan{ClientID: clientID, Version: 0, RequestsPerMinute: port.DefaultRequestsPerMinute, DailyUnits: port.DefaultDailyUnits,
		MonthlyUnits: port.DefaultMonthlyUnits, MaxExportRows: port.DefaultMaxExportRows}
}

// RotateGrace trả hạn chồng lấn của khóa cũ khi xoay: now + DefaultRotateGrace, không quá hạn khóa.
func RotateGrace(now, expiresAt time.Time) time.Time {
	g := now.Add(port.DefaultRotateGrace)
	if g.After(expiresAt) {
		return expiresAt
	}
	return g
}

// ClientStream / PlanStream là stream_key của sổ.
func ClientStream(id string) string { return "client:" + id }

// PlanStream là stream_key của sổ gói hạn mức một khách hàng.
func PlanStream(id string) string { return "plan:" + id }

// IngestContract: name là hợp đồng nạp contracts/integration/ingest hợp lệ (bất kỳ phiên bản/trạng thái — việc
// phục vụ do cổng kiểm lúc gọi, W5.4).
func IngestContract(name string) bool {
	all, err := integration.All()
	if err != nil {
		return false
	}
	for _, c := range all {
		if c.Name == name && c.Kind == integration.KindIngest {
			return true
		}
	}
	return false
}

// NormIngestScopes kiểm và chuẩn hóa scope của một đăng ký hợp đồng nạp: chỉ ingest:<hợp đồng>:write hoặc
// usage:read.
func NormIngestScopes(contract string, in []string) ([]string, bool) {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != port.ScopeUsageRead && s != port.IngestScope(contract) {
			return nil, false
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out, len(out) > 0
}
