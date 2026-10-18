package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func dp(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

// Mục II.3.1: trạng thái phiếu nhìn theo giờ khóa và bảng giá ngày.
func TestTicketView(t *testing.T) {
	cases := []struct {
		status string
		locked bool
		board  string
		want   string
	}{
		{"", false, BoardOpen, ViewNotEntered},
		{"", true, BoardOpen, ViewMissed},
		{TicketDraft, false, BoardOpen, ViewDraft},
		{TicketDraft, true, BoardOpen, ViewMissed}, // BR-06: bản nháp đến giờ khóa tính là Không nộp
		{TicketSubmitted, false, BoardOpen, ViewSubmitted},
		{TicketSubmitted, true, BoardOpen, ViewLocked},
		{TicketSubmitted, true, BoardApproved, ViewApproved},
		{TicketSubmitted, true, BoardReopened, ViewLocked}, // đang mở duyệt: về Đã khóa
		{TicketLate, true, BoardOpen, ViewLate},
		{TicketLate, true, BoardApproved, ViewApproved},
		{TicketDeleted, true, BoardApproved, ViewDeleted},
	}
	for _, c := range cases {
		if got := TicketView(c.status, c.locked, c.board); got != c.want {
			t.Errorf("TicketView(%q, %v, %q) = %q, muốn %q", c.status, c.locked, c.board, got, c.want)
		}
	}
	if !BoardEditable(BoardOpen) || !BoardEditable(BoardReopened) || BoardEditable(BoardApproved) || BoardEditable(BoardApproving) {
		t.Fatal("BoardEditable: Chưa duyệt / Đang mở duyệt sửa được; Đã duyệt / Đang duyệt không")
	}
}

// Mục II.3.2: Tạm tính sau giờ khóa đọc là Chờ rà soát; yêu cầu xác minh quá hạn đọc là Quá hạn phản hồi.
func TestAlertView(t *testing.T) {
	rb := time.Date(2026, 10, 2, 6, 30, 0, 0, time.UTC) // 13:30 giờ VN
	if v := AlertView(AlertProvisional, false, time.Time{}, rb); v != AlertProvisional {
		t.Fatal(v)
	}
	if v := AlertView(AlertProvisional, true, time.Time{}, rb); v != AlertViewPending {
		t.Fatal(v)
	}
	if v := AlertView(AlertRequested, true, rb, rb.Add(-time.Minute)); v != AlertRequested {
		t.Fatal(v)
	}
	if v := AlertView(AlertRequested, true, rb, rb.Add(time.Minute)); v != AlertViewOverdue { // AC-02 FN-XA-03.05: 13:31
		t.Fatal(v)
	}
	if v := AlertView(AlertReopened, true, time.Time{}, rb); v != AlertViewPending {
		t.Fatal(v)
	}
	for _, s := range []string{AlertAdjusted, AlertKept, AlertExcluded} {
		if !AlertResolved(s) {
			t.Errorf("%s phải là đã xử lý (BR-02)", s)
		}
	}
	if AlertResolved(AlertResponded) || AlertResolved(AlertRequested) || AlertResolved(AlertProvisional) {
		t.Fatal("phản hồi / yêu cầu / tạm tính chưa xử lý")
	}
}

// BR-02 FN-XA-03.01: giá dương, thấp nhất ≤ cao nhất, tối đa 2 số lẻ; Không có hàng không có giá.
func TestValidateLine(t *testing.T) {
	ok := []Line{
		{ItemCode: "MH00001", Min: dp("9000"), Max: dp("9000")},
		{ItemCode: "MH00001", Min: dp("12.5"), Max: dp("13.25")},
		{ItemCode: "MH00001", NoGoods: true},
		{ItemCode: "MH00001"}, // dòng trống (bản nháp)
	}
	for _, l := range ok {
		if err := ValidateLine(l); err != nil {
			t.Errorf("%+v: %v", l, err)
		}
	}
	bad := []Line{
		{ItemCode: "X1", Min: dp("1"), Max: dp("1")},
		{ItemCode: "MH00001", Min: dp("9000")},
		{ItemCode: "MH00001", Min: dp("10000"), Max: dp("9000")},
		{ItemCode: "MH00001", Min: dp("0"), Max: dp("9000")},
		{ItemCode: "MH00001", Min: dp("-1"), Max: dp("9000")},
		{ItemCode: "MH00001", Min: dp("1.234"), Max: dp("2")},
		{ItemCode: "MH00001", Min: dp("1"), Max: dp("2"), NoGoods: true},
		{ItemCode: "MH00001", Min: dp("1"), Max: dp("1000000000000")},
	}
	for _, l := range bad {
		if err := ValidateLine(l); err == nil {
			t.Errorf("%+v phải lỗi", l)
		}
	}
	if err := ValidPair(decimal.NewFromInt(2), decimal.NewFromInt(1)); !errors.Is(err, ErrPrice) {
		t.Fatalf("ValidPair: %v", err)
	}
	a := Line{ItemCode: "MH00001", Min: dp("9000"), Max: dp("9000.00"), Note: " Mưa "}
	if !a.Same(Line{ItemCode: "MH00001", Min: dp("9000"), Max: dp("9000"), Note: "Mưa"}) || a.Same(Line{ItemCode: "MH00001", NoGoods: true}) {
		t.Fatal("Same")
	}
	if !a.Filled() || !a.Priced() || (Line{NoGoods: true}).Priced() || !(Line{NoGoods: true}).Filled() || (Line{}).Filled() {
		t.Fatal("Filled / Priced")
	}
}

// CT-01, CT-03; AC-03 FN-XA-03.01: Dưa leo kỳ trước 9.000, nhập 13.000–15.000 → ▲ +55,56 %.
func TestEvaluate(t *testing.T) {
	thirty := decimal.NewFromInt(30)
	e := Evaluate(decimal.NewFromInt(13000), decimal.NewFromInt(15000), decimal.NewFromInt(9000), thirty)
	if e.Direction != AlertHigh || e.Price.String() != "14000" || e.ChangePct.StringFixed(2) != "55.56" || !e.HasRef {
		t.Fatalf("AC-03: %+v", e)
	}
	if e := Evaluate(decimal.NewFromInt(6300), decimal.NewFromInt(6300), decimal.NewFromInt(9000), thirty); e.Direction != AlertLow {
		t.Fatalf("−30 %% → Thấp bất thường: %+v", e)
	}
	if e := Evaluate(decimal.NewFromInt(9000), decimal.NewFromInt(10000), decimal.NewFromInt(9000), thirty); e.Direction != AlertNone {
		t.Fatalf("+5,56 %% → bình thường: %+v", e)
	}
	if e := Evaluate(decimal.NewFromInt(9000), decimal.NewFromInt(9000), decimal.Zero, thirty); e.HasRef || e.Direction != AlertNone {
		t.Fatalf("không có giá tham chiếu → Giá mới: %+v", e)
	}
	if m := Mid(decimal.RequireFromString("0.01"), decimal.RequireFromString("0.02")); m.String() != "0.015" {
		t.Fatalf("Mid: %s", m)
	}
}

// BR-05 FN-TI-02.04: bảng giá, giá tham chiếu dùng giá hiệu chỉnh; Không có hàng không có giá.
func TestEffective(t *testing.T) {
	l := Line{ItemCode: "MH00002", Min: dp("38000"), Max: dp("38000")}
	if lo, hi, ok := Effective(l, nil, nil); !ok || lo.String() != "38000" || hi.String() != "38000" {
		t.Fatal("giá Xã gửi")
	}
	if lo, _, ok := Effective(l, dp("56000"), dp("56000")); !ok || lo.String() != "56000" {
		t.Fatal("AC-05: giá hiệu chỉnh 56.000")
	}
	if _, _, ok := Effective(Line{NoGoods: true}, nil, nil); ok {
		t.Fatal("Không có hàng")
	}
	if !TextRequired(" Mưa ", MaxNote) || TextRequired("  ", MaxNote) {
		t.Fatal("TextRequired")
	}
	if m := DefaultRequestMessage("Dưa leo", "Mỹ Chánh", decimal.RequireFromString("55.5556")); m !=
		"Giá Dưa leo tại Mỹ Chánh chênh 55.6% so với giá tham chiếu, đề nghị xã kiểm tra lại" {
		t.Fatal(m)
	}
}
