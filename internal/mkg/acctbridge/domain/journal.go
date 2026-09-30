package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	abport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	costingport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// Trạng thái bút toán của module: bốn trạng thái của port + superseded (bị bản dựng lại thay thế
// TRƯỚC khi đẩy — bản đã đẩy không bao giờ bị thay).
const (
	StatusBuilt      = string(abport.JournalBuilt)
	StatusApproved   = string(abport.JournalApproved)
	StatusPushed     = string(abport.JournalPushed)
	StatusPushFailed = string(abport.JournalPushFailed)
	StatusSuperseded = "superseded"
)

// ErrEmptyPeriod: kỳ không có số tiền nào khác 0 — không có bút toán để dựng.
var ErrEmptyPeriod = errors.New("acctbridge: kỳ không có số phát sinh")

// MissingRule mô tả dòng tổng hợp không ánh xạ được.
type MissingRule struct {
	Category      costingport.CostCategory
	DimensionKind string
}

func (m MissingRule) Error() string {
	return fmt.Sprintf("%v: %s × %s", ErrMappingMissing, m.Category, m.DimensionKind)
}

func (m MissingRule) Unwrap() error { return ErrMappingMissing }

// Built là kết quả dựng tất định: dòng đã gộp và sắp, tổng, băm dòng.
type Built struct {
	Lines       []abport.JournalLine
	TotalDebit  money.Amount
	TotalCredit money.Amount
	LinesSHA256 string
}

// Build dựng bút toán tổng hợp từ PeriodSummary của costing theo bảng ánh xạ. Tất định: cùng
// (Summary.Lines, Mapping.Rules) → cùng dòng, cùng thứ tự, cùng băm (B-03). Chỉ TỔNG theo
// (tài khoản, bên, nhóm, chiều) — không có giao dịch chi tiết (mục 16, B-01).
func Build(periodCode string, lines []costingport.SummaryLine, m Mapping) (Built, error) {
	// Gộp theo (tài khoản, nhóm, chiều) với số có dấu: Nợ dương, Có âm — điều chỉnh âm tự bù vào dòng cùng
	// tài khoản; bên của dòng theo dấu của tổng.
	type key struct {
		account, category, dim string
	}
	sums := map[key]money.Amount{}
	add := func(k key, a money.Amount) {
		cur, ok := sums[k]
		if !ok {
			cur = money.MustAmount("0")
		}
		sums[k] = cur.Add(a)
	}
	for _, l := range lines {
		if l.Amount.IsZero() {
			continue
		}
		r, ok := m.Lookup(l.Category, l.DimensionKind)
		if !ok {
			return Built{}, MissingRule{Category: l.Category, DimensionKind: l.DimensionKind}
		}
		dim := ""
		if l.DimensionRef != nil && r.DimensionOn != DimOnNone {
			dim = l.DimensionKind + ":" + l.DimensionRef.String()
		}
		dDim, cDim := "", ""
		switch r.DimensionOn {
		case DimOnDebit:
			dDim = dim
		case DimOnCredit:
			cDim = dim
		}
		// Dòng dương: Nợ DebitAccount / Có CreditAccount; dòng âm (điều chỉnh) tự đảo bên nhờ dấu.
		add(key{r.DebitAccount, string(l.Category), dDim}, l.Amount)
		add(key{r.CreditAccount, string(l.Category), cDim}, l.Amount.Neg())
	}
	type row struct {
		key
		side string
		amt  money.Amount
	}
	rows := make([]row, 0, len(sums))
	for k, a := range sums {
		switch a.Decimal().Sign() {
		case 1:
			rows = append(rows, row{k, string(abport.Debit), a})
		case -1:
			rows = append(rows, row{k, string(abport.Credit), a.Neg()})
		}
	}
	if len(rows) == 0 {
		return Built{}, ErrEmptyPeriod
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.side != b.side {
			return a.side == string(abport.Debit) // Nợ trước Có
		}
		if a.account != b.account {
			return a.account < b.account
		}
		if a.category != b.category {
			return a.category < b.category
		}
		return a.dim < b.dim
	})
	out := Built{TotalDebit: money.MustAmount("0"), TotalCredit: money.MustAmount("0")}
	for i, r := range rows {
		out.Lines = append(out.Lines, abport.JournalLine{
			LineNo: i + 1, Account: r.account, Side: abport.Side(r.side), Amount: r.amt, Category: costingport.CostCategory(r.category),
			Dimension: r.dim, Description: fmt.Sprintf("Tổng hợp %s kỳ %s", r.category, periodCode),
		})
		if r.side == string(abport.Debit) {
			out.TotalDebit = out.TotalDebit.Add(r.amt)
		} else {
			out.TotalCredit = out.TotalCredit.Add(r.amt)
		}
	}
	if err := CheckBalanced(out.Lines); err != nil {
		return Built{}, err
	}
	out.LinesSHA256 = LinesSHA256(out.Lines)
	return out, nil
}

// CheckBalanced kiểm Σ Nợ = Σ Có > 0 và mọi dòng > 0 (ErrUnbalanced của port).
func CheckBalanced(lines []abport.JournalLine) error {
	dr, cr := money.MustAmount("0"), money.MustAmount("0")
	for _, l := range lines {
		if l.Amount.Decimal().Sign() <= 0 {
			return fmt.Errorf("%w: dòng %d số tiền phải > 0", abport.ErrUnbalanced, l.LineNo)
		}
		switch l.Side {
		case abport.Debit:
			dr = dr.Add(l.Amount)
		case abport.Credit:
			cr = cr.Add(l.Amount)
		default:
			return fmt.Errorf("%w: dòng %d bên %q", abport.ErrUnbalanced, l.LineNo, l.Side)
		}
	}
	if !dr.Equal(cr) || dr.IsZero() {
		return fmt.Errorf("%w: Nợ %s, Có %s", abport.ErrUnbalanced, dr, cr)
	}
	return nil
}

// LinesSHA256 là băm tất định của các dòng (tài khoản, bên, số tiền, nhóm, chiều).
func LinesSHA256(lines []abport.JournalLine) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "%d|%s|%s|%s|%s|%s\n", l.LineNo, l.Account, l.Side, l.Amount.String(), l.Category, l.Dimension)
	}
	s := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(s[:])
}

// IdempotencyKey là khóa đẩy tất định của bút toán — không phụ thuộc lần thử (port.SummaryJournal).
func IdempotencyKey(journalID uuid.UUID) string { return "acctbridge:" + journalID.String() }

// PeriodEndDate là ngày cuối kỳ (giờ Việt Nam) của khoảng [from, to) — chọn phiên bản ánh xạ theo thời
// điểm sự việc (hiệu lực đến hết kỳ).
func PeriodEndDate(to time.Time, zone *time.Location) time.Time {
	d := to.Add(-time.Nanosecond).In(zone)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

// Backoff là giãn cách trước lần thử kế tiếp sau lần thứ attempt (1-based) tạm không nhận: 1 phút × 2^(n−1),
// trần 1 giờ (R-62; tạm, chờ vận hành chốt).
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := time.Minute
	for i := 1; i < attempt && d < time.Hour; i++ {
		d *= 2
	}
	if d > time.Hour {
		d = time.Hour
	}
	return d
}

// DefaultRetryMax là số lần đẩy tối đa khi phần mềm kế toán tạm không nhận trước khi vào DLQ
// (acctbridge.push.retry_max — tạm, chờ khách).
const DefaultRetryMax = 8
