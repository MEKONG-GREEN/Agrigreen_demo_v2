package domain

import (
	"bytes"
	"encoding/csv"
	"sort"
	"strconv"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
)

// CSVHeader là dòng đầu của CSV bảng kê (UTF-8, phân cách dấu phẩy, xuống dòng LF).
var CSVHeader = []string{
	"statement_id", "client_id", "client_tenant_id", "period", "status", "product", "status_class",
	"calls", "units", "unit_price_vnd", "amount_vnd", "price_version", "snapshot_sha256", "chain_head_hash",
	"first_seq", "last_seq", "rule_version", "supersedes_id",
}

// Dòng tổng cuối CSV (cột product).
const (
	CSVSubscriptionRow = "_subscription"
	CSVTotalRow        = "_total"
)

// StatementCSV dựng CSV TẤT ĐỊNH của bảng kê (ADR-0023 mục 4): chỉ từ dữ liệu đã ghi (không thời điểm
// tải, không người tải) — cùng bảng kê → cùng byte. Dòng sắp theo (sản phẩm, lớp trạng thái); tiền ở
// dạng thập phân cố định (đơn giá 4 chữ số, thành tiền 2 chữ số), rỗng khi chưa có giá.
func StatementCSV(st port.Statement) ([]byte, error) {
	lines := append([]port.StatementLine(nil), st.Lines...)
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Product != lines[j].Product {
			return lines[i].Product < lines[j].Product
		}
		return lines[i].StatusClass < lines[j].StatusClass
	})
	sup := ""
	if st.SupersedesID != uuid.Nil {
		sup = st.SupersedesID.String()
	}
	common := func(product, class, calls, units, price, amount, pv string) []string {
		return []string{
			st.ID.String(), st.ClientID.String(), st.ClientTenantID.String(), st.Period, string(st.Status), product, class,
			calls, units, price, amount, pv, st.SnapshotSHA256, st.ChainHeadHash,
			strconv.FormatInt(st.FirstSeq, 10), strconv.FormatInt(st.LastSeq, 10), st.RuleVersion, sup,
		}
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	rows := [][]string{CSVHeader}
	var calls, units int64
	for _, l := range lines {
		price, amount := "", ""
		if l.UnitPrice != nil {
			price = l.UnitPrice.String()
		}
		if l.Amount != nil {
			amount = l.Amount.String()
		}
		rows = append(rows, common(l.Product, l.StatusClass, strconv.FormatInt(l.Calls, 10), strconv.FormatInt(l.Units, 10), price, amount, l.PriceVersion))
		calls += l.Calls
		units += l.Units
	}
	if st.Subscription != nil {
		rows = append(rows, common(CSVSubscriptionRow, "", "", "", "", st.Subscription.String(), ""))
	}
	total := ""
	if st.Total != nil {
		total = st.Total.String()
	}
	rows = append(rows, common(CSVTotalRow, "", strconv.FormatInt(calls, 10), strconv.FormatInt(units, 10), "", total, ""))
	if err := w.WriteAll(rows); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
