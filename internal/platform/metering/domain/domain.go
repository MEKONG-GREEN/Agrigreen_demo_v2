// Package domain: quy tắc thuần của pf/metering — cửa sổ giờ Việt Nam, khóa chuỗi, ngưỡng bất thường,
// ảnh chụp bảng kê chuẩn tắc và CSV tất định. Không float (agnofloat, R-10): khối lượng là int64, hệ
// số và tiền là decimal / shared/money.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
)

// AllProducts là khóa sản phẩm của dòng bộ đếm tổng (hạn mức gói tính trên tổng mọi sản phẩm).
const AllProducts = "*"

// RuleEgressV1 là phiên bản quy tắc của điểm kiểm soát xuất (hạn mức ngày + bất thường × trung vị 28 ngày).
const RuleEgressV1 = "metering.egress.v1"

// RuleStatementV1 là phiên bản quy tắc lập bảng kê (gộp theo sản phẩm × lớp trạng thái; giá tùy chọn).
const RuleStatementV1 = "metering.statement.v1"

// PricePlanDefault là tên gói giá dùng trong khóa metering.price.<plan>.* (chờ khách — Q5-4). Gói hạn
// mức của pf/apiclient chưa có tên gói giá; mọi khách hàng dùng "default" cho tới khi khách chốt.
const PricePlanDefault = "default"

// Day trả ngày nghiệp vụ giờ VN của t dưới dạng time.Time 00:00 UTC (để ghi cột date).
func Day(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// Month trả ngày 1 của tháng nghiệp vụ giờ VN của t (00:00 UTC, cột date).
func Month(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	return time.Date(d.Year, d.Month, 1, 0, 0, 0, 0, time.UTC)
}

// Period trả kỳ YYYY-MM giờ VN của t.
func Period(t time.Time) string { return Month(t).Format("2006-01") }

// NextDayStart là thời điểm UTC bắt đầu ngày nghiệp vụ kế tiếp sau t.
func NextDayStart(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	n := time.Date(d.Year, d.Month, d.Day+1, 0, 0, 0, 0, time.UTC)
	return clock.StartOfBusinessDate(clock.Date{Year: n.Year(), Month: n.Month(), Day: n.Day()})
}

// NextMonthStart là thời điểm UTC bắt đầu tháng nghiệp vụ kế tiếp sau t.
func NextMonthStart(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	n := time.Date(d.Year, d.Month+1, 1, 0, 0, 0, 0, time.UTC)
	return clock.StartOfBusinessDate(clock.Date{Year: n.Year(), Month: n.Month(), Day: 1})
}

var periodRe = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// ParsePeriod đọc kỳ YYYY-MM; trả [bắt đầu, kết thúc) theo giờ VN dưới dạng UTC.
func ParsePeriod(p string) (start, end time.Time, err error) {
	if !periodRe.MatchString(p) {
		return time.Time{}, time.Time{}, fmt.Errorf("kỳ phải dạng YYYY-MM")
	}
	m, err := time.Parse("2006-01", p)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start = clock.StartOfBusinessDate(clock.Date{Year: m.Year(), Month: m.Month(), Day: 1})
	return start, NextMonthStart(start), nil
}

// UsageStream là stream_key của sổ sử dụng: client:<id>:<YYYY-MM>.
func UsageStream(client uuid.UUID, period string) string {
	return "client:" + client.String() + ":" + period
}

// EgressStream là stream_key của sổ xuất: egress:<kênh>:<YYYY-MM-DD>.
func EgressStream(ch meter.Channel, day time.Time) string {
	return "egress:" + string(ch) + ":" + day.Format(time.DateOnly)
}

// EgressIdemKey là idem_key của dòng egress_event: <kênh>:<source_id>:<counterparty_ref>.
func EgressIdemKey(e meter.Egress) string {
	return string(e.Channel) + ":" + e.SourceID.String() + ":" + e.CounterpartyRef.String()
}

// StatementStream là stream_key của sổ bảng kê.
func StatementStream(client uuid.UUID, period string) string {
	return "statement:" + client.String() + ":" + period
}

// StatusClass trả lớp trạng thái HTTP "2xx" | "4xx" | ….
func StatusClass(status int) string { return fmt.Sprintf("%dxx", status/100) }

// ValidChannel cho biết ch có trong port.Channels.
func ValidChannel(ch meter.Channel) bool {
	for _, c := range meter.Channels {
		if c == ch {
			return true
		}
	}
	return false
}

// ValidCounterparty cho biết loại bên nhận hợp lệ.
func ValidCounterparty(k meter.CounterpartyKind) bool {
	switch k {
	case meter.CounterpartyAPIClient, meter.CounterpartyTenant, meter.CounterpartyActor:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Tham số DLP (mặc định "tạm, chờ khách" — g5-conventions §1.2)
// ---------------------------------------------------------------------------

// EgressParams là tham số hiệu lực của một kênh.
type EgressParams struct {
	DailyQuota     int64           // 0 = không giới hạn
	AnomalyFactor  decimal.Decimal // × trung vị 28 ngày
	AnomalyRecords int64           // ngưỡng tuyệt đối khi < 7 ngày mẫu; 0 = tắt
	Source         map[string]string
}

// DefaultEgressParams trả mặc định của kênh.
func DefaultEgressParams(ch meter.Channel) EgressParams {
	p := EgressParams{AnomalyFactor: decimal.NewFromInt(3), Source: map[string]string{}}
	switch ch {
	case meter.ChannelMRVPackage:
		p.DailyQuota, p.AnomalyRecords = 50_000, 10_000
	case meter.ChannelPublicAPI:
		p.AnomalyRecords = 50_000
	case meter.ChannelReportExport:
		p.AnomalyRecords = 20_000
	case meter.ChannelValve:
		p.AnomalyRecords = 20_000
	case meter.ChannelPartnerShare:
		p.AnomalyRecords = 50_000
	}
	return p
}

// ---------------------------------------------------------------------------
// Bất thường (ADR-0024 mục 1.3)
// ---------------------------------------------------------------------------

// Threshold tính ngưỡng bất thường từ khối lượng các ngày TRƯỚC (chỉ ngày có dữ liệu, ≤ 28 ngày):
// đủ port.AnomalyMinSampleDays mẫu → floor(factor × trung vị), basis "median"; không đủ → ngưỡng
// tuyệt đối, basis "absolute". Ngưỡng 0 = tắt.
func Threshold(history []int64, p EgressParams) (int64, string) {
	if len(history) >= port.AnomalyMinSampleDays {
		m := Median(history)
		return p.AnomalyFactor.Mul(decimal.NewFromInt(m)).Floor().IntPart(), "median"
	}
	return p.AnomalyRecords, "absolute"
}

// Median là trung vị số nguyên (số mẫu chẵn → trung bình hai giữa, làm tròn xuống).
func Median(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// IsAnomaly: khối lượng ngày vượt ngưỡng (ngưỡng ≤ 0 = tắt).
func IsAnomaly(daily, threshold int64) bool { return threshold > 0 && daily > threshold }

// ---------------------------------------------------------------------------
// Ảnh chụp bảng kê (ADR-0023 mục 4)
// ---------------------------------------------------------------------------

// SnapshotLine là một dòng gộp của ảnh chụp.
type SnapshotLine struct {
	Product     string `json:"product"`
	StatusClass string `json:"status_class"`
	Calls       int64  `json:"calls"`
	Units       int64  `json:"units"`
}

// Snapshot là ảnh chụp bất biến của stream sử dụng một khách hàng × kỳ.
type Snapshot struct {
	RuleVersion    string         `json:"rule_version"`
	UnitsRules     []string       `json:"units_rules"`
	ClientID       string         `json:"client_id"`
	ClientTenantID string         `json:"client_tenant_id"`
	Period         string         `json:"period"`
	StreamKey      string         `json:"stream_key"`
	FirstSeq       int64          `json:"first_seq"`
	LastSeq        int64          `json:"last_seq"`
	ChainHeadHash  string         `json:"chain_head_hash"`
	Lines          []SnapshotLine `json:"lines"`
}

// Sort sắp dòng và quy tắc theo thứ tự cố định.
func (s *Snapshot) Sort() {
	sort.Slice(s.Lines, func(i, j int) bool {
		if s.Lines[i].Product != s.Lines[j].Product {
			return s.Lines[i].Product < s.Lines[j].Product
		}
		return s.Lines[i].StatusClass < s.Lines[j].StatusClass
	})
	sort.Strings(s.UnitsRules)
}

// Canonical trả JSON chuẩn tắc (thứ tự trường cố định theo struct, dòng đã sắp) và SHA-256 hex.
func (s Snapshot) Canonical() ([]byte, string, error) {
	s.Lines = append([]SnapshotLine(nil), s.Lines...)
	s.UnitsRules = append([]string(nil), s.UnitsRules...)
	s.Sort()
	if s.Lines == nil {
		s.Lines = []SnapshotLine{}
	}
	if s.UnitsRules == nil {
		s.UnitsRules = []string{}
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), nil
}
