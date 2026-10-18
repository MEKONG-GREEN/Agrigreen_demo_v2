// Package dispatch: quy tắc thuần của "Báo cáo Chi cục" KN v7.7 (FN-TI-02.06) — định dạng tệp, tiêu đề/nội dung
// email tự điền, giới hạn đính kèm, kiểm địa chỉ hộp thư đơn vị, cờ Google Sheet (TẮT mặc định, ADR-0026 "Đề xuất")
// và van chặn trường lạ trước khi dữ liệu ra ngoài lãnh thổ (RB-7, gap §4.3). Không DB, không float (R-10).
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// Định dạng xuất.
const (
	FormatXLSX   = "xlsx"
	FormatPDF    = "pdf"
	FormatGSheet = "gsheet"
)

// Formats là định dạng hợp lệ.
var Formats = []string{FormatXLSX, FormatPDF, FormatGSheet}

// Trạng thái lần gửi.
const (
	StatusExported = "exported" // đã xuất tệp, chưa gửi ("chỉ tải file, không gửi")
	StatusSending  = "sending"
	StatusSent     = "sent"
	StatusFailed   = "failed" // "Gửi lỗi" — cho gửi lại
)

// Loại người nhận.
const (
	KindTo = "to"
	KindCC = "cc"
)

// DefaultAttachmentLimitMB: tổng dung lượng đính kèm tối đa (tạm; tham số reports.dispatch_attachment_limit_mb).
const DefaultAttachmentLimitMB = 10

// ValidAddress kiểm một địa chỉ hộp thư (không tên hiển thị, không ký tự xuống dòng).
func ValidAddress(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 254 || strings.ContainsAny(s, "\r\n,;<> ") {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && strings.Contains(s[strings.LastIndex(s, "@"):], ".")
}

// Mask che địa chỉ hộp thư cho người chỉ xem (vd "tr•••@angiang.gov.vn").
func Mask(s string) string {
	at := strings.LastIndex(s, "@")
	if at <= 0 {
		return "•••"
	}
	local := []rune(s[:at])
	keep := min(2, len(local))
	return string(local[:keep]) + "•••" + s[at:]
}

// Subject là tiêu đề tự điền (sửa được).
func Subject(department, periodLabel, season string, provisional bool) string {
	s := fmt.Sprintf("Báo cáo %s — %s — %s", department, periodLabel, season)
	if provisional {
		s += " (Tạm tính)"
	}
	return s
}

// BodyFor renders a message with trusted application labels.
func BodyFor(profile port.DispatchProfile, periodLabel, rangeLabel string, forms []string, provisional bool, missing []string, links []string) string {
	var b strings.Builder
	b.WriteString(profile.Salutation + "\n\n")
	fmt.Fprintf(&b, "%s gửi báo cáo tổng hợp %s (%s): %s.\n", profile.SenderName, periodLabel, rangeLabel, strings.Join(forms, ", "))
	if provisional {
		b.WriteString("Số liệu TẠM TÍNH (kỳ chưa chốt hoặc còn xã chưa nộp).\n")
		if len(missing) > 0 {
			b.WriteString("Xã chưa nộp: " + strings.Join(missing, ", ") + ".\n")
		}
	}
	if len(links) > 0 {
		b.WriteString("\nTệp vượt giới hạn đính kèm — tải tại (liên kết có hạn):\n")
		for _, l := range links {
			b.WriteString("- " + l + "\n")
		}
	}
	b.WriteString("\nThư gửi tự động từ hệ thống AgriGreen.")
	return b.String()
}

// FileName dựng tên tệp "T01_Tuần 36.xlsx" (bỏ ký tự cấm trong tên tệp).
func FileName(formCode, periodLabel, ext string) string {
	n := formCode + "_" + periodLabel
	n = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, n)
	return n + "." + ext
}

// ---------------------------------------------------------------------------
// Google Sheet (RB-7): TẮT mặc định
// ---------------------------------------------------------------------------

// GSheetConfig là cấu hình bộ chuyển Google Sheet.
type GSheetConfig struct {
	Enabled   bool
	ADR       string // mã ADR đã CHẤP NHẬN cho nguồn ngoài lãnh thổ (ADR-0007 cơ chế), vd "ADR-0026"
	ShareMode string // recipients | anyone_with_link (Q-14: khách chọn)
}

// ErrGSheetDisabled: chưa bật (mặc định).
var ErrGSheetDisabled = errors.New("dispatch: Google Sheet đang tắt (ADR-0026 chưa chấp nhận)")

// GSheetConfigFromEnv: bật CHỈ khi AG_REPORTS_GSHEET=on VÀ AG_EXTDATA_GSHEET_ADR khai mã ADR đã chấp nhận VÀ
// AG_NONPII_ALLOWLIST chứa "reports.gsheet" (cơ chế ADR-0007). Thiếu bất kỳ điều kiện nào → tắt.
func GSheetConfigFromEnv(getenv func(string) string) GSheetConfig {
	c := GSheetConfig{ShareMode: "anyone_with_link"}
	if m := getenv("AG_REPORTS_GSHEET_SHARE"); m == "recipients" {
		c.ShareMode = m
	}
	allow := false
	for _, x := range strings.Split(getenv("AG_NONPII_ALLOWLIST"), ",") {
		if strings.TrimSpace(x) == "reports.gsheet" {
			allow = true
		}
	}
	c.ADR = strings.TrimSpace(getenv("AG_EXTDATA_GSHEET_ADR"))
	c.Enabled = strings.EqualFold(getenv("AG_REPORTS_GSHEET"), "on") && c.ADR != "" && allow
	return c
}

// Sheet là một trang tính gửi sang Google (chỉ số tổng hợp theo xã/khu vực/tỉnh).
type Sheet struct {
	Title  string
	Header []string
	Rows   [][]string
}

// ErrValve: van chặn — có trường không thuộc danh sách cho phép.
var ErrValve = errors.New("dispatch: van chặn dữ liệu ra ngoài lãnh thổ")

// labelSources là cột chữ được phép ra ngoài: STT, tên khu vực, tên xã, nhãn chỉ tiêu (dữ liệu tổ chức, không PII).
var labelSources = map[string]bool{port.FixedRowNo: true, port.FixedRegionName: true, port.FixedCommuneName: true, port.FixedIndicatorLabel: true}

// Valve dựng trang tính từ lưới tổng hợp với chính sách MẶC ĐỊNH CHẶN: cột số (decimal) và cột nhãn tổ chức được
// đi; cột chữ/ngày/danh mục khác bị chặn → lỗi (không tự bỏ qua). Không có thông tin người (tên, SĐT, email).
func Valve(def port.FormDefinition, rows []map[string]string) (Sheet, error) {
	sh := Sheet{Title: def.Code + " " + def.Title}
	var allowed []port.FormColumn
	for _, c := range def.Columns {
		switch {
		case c.DType == port.DTypeNumber || c.DType == port.DTypeInteger:
		case c.Kind == port.ColumnFixed && labelSources[c.FixedSource]:
		default:
			return Sheet{}, fmt.Errorf("%w: cột %s (%s, %s) không thuộc danh sách cho phép", ErrValve, c.Letter, c.Kind, c.DType)
		}
		allowed = append(allowed, c)
		sh.Header = append(sh.Header, c.Letter+" · "+c.Title)
	}
	for _, r := range rows {
		out := make([]string, len(allowed))
		for i, c := range allowed {
			v := r[c.Letter]
			if v != "" && (c.DType == port.DTypeNumber || c.DType == port.DTypeInteger) {
				if _, err := decimal.NewFromString(v); err != nil {
					return Sheet{}, fmt.Errorf("%w: ô %s không phải số", ErrValve, c.Letter)
				}
			}
			out[i] = v
		}
		sh.Rows = append(sh.Rows, out)
	}
	return sh, nil
}

// GSheetPublisher tạo Google Sheet bằng tài khoản Google của TTKN tỉnh (bí mật trong KMS lãnh thổ, Admin kết nối).
// CHƯA có hiện thực trong repo — chỉ dựng khi ADR-0026 được chấp nhận và khách trả lời Q-14.
type GSheetPublisher interface {
	Publish(ctx context.Context, title string, sheets []Sheet, shareMode string, shareWith []string) (url string, err error)
}
