package domain_test

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	abport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	cp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

var update = flag.Bool("update", false, "ghi lại tệp golden")

var (
	htxA     = uuid.MustParse("0192a000-0000-7000-8000-00000000a001")
	htxB     = uuid.MustParse("0192a000-0000-7000-8000-00000000a002")
	customer = uuid.MustParse("0192a000-0000-7000-8000-00000000c001")
)

func summary() []cp.SummaryLine {
	return []cp.SummaryLine{
		{Category: cp.CatStrawPurchase, DimensionKind: "htx", DimensionRef: &htxA, Amount: money.MustAmount("120000000.00"), Entries: 3},
		{Category: cp.CatStrawPurchase, DimensionKind: "htx", DimensionRef: &htxB, Amount: money.MustAmount("45500000.50"), Entries: 1},
		{Category: cp.CatTransportWaterway, DimensionKind: "mode", Amount: money.MustAmount("8100000.00"), Entries: 4},
		{Category: cp.CatTransportRoad, DimensionKind: "mode", Amount: money.MustAmount("2200000.00"), Entries: 2},
		{Category: cp.CatHubProcessing, DimensionKind: "facility", Amount: money.MustAmount("12000000.00"), Entries: 5},
		{Category: cp.CatSalesRevenue, DimensionKind: "customer", DimensionRef: &customer, Amount: money.MustAmount("310000000.00"), Entries: 2},
		// Điều chỉnh âm (bút toán điều chỉnh của costing ở kỳ sau): đảo bên.
		{Category: cp.CatStrawPurchase, DimensionKind: "htx", DimensionRef: &htxA, Amount: money.MustAmount("-500000.00"), Entries: 1},
		{Category: cp.CatLoss, DimensionKind: "none", Amount: money.MustAmount("0.00"), Entries: 1},
	}
}

func mapping() domain.Mapping {
	return domain.Mapping{Version: 1, Rules: domain.DefaultRules(), Provisional: true}
}

// B-03: Σ Nợ = Σ Có > 0; mọi dòng dương; dòng 0 bị bỏ; điều chỉnh âm đảo bên; chỉ tổng (B-01).
func TestBuildBalanced_B03(t *testing.T) {
	b, err := domain.Build("2026-09", summary(), mapping())
	if err != nil {
		t.Fatal(err)
	}
	if !b.TotalDebit.Equal(b.TotalCredit) || b.TotalDebit.String() != "497300000.50" {
		t.Fatalf("tổng: Nợ %s Có %s", b.TotalDebit, b.TotalCredit)
	}
	if err := domain.CheckBalanced(b.Lines); err != nil {
		t.Fatal(err)
	}
	var sawReversal bool
	for i, l := range b.Lines {
		if l.LineNo != i+1 || l.Amount.Decimal().Sign() <= 0 {
			t.Fatalf("dòng %d: %+v", i, l)
		}
		if l.Category == cp.CatLoss {
			t.Fatal("dòng 0 phải bị bỏ")
		}
		// Điều chỉnh âm mua rơm HTX A (−500 000) bù vào công nợ HTX A: Có 331 = 120 000 000 − 500 000.
		if l.Account == "331" && l.Side == abport.Credit && l.Dimension == "htx:"+htxA.String() && l.Amount.String() == "119500000.00" {
			sawReversal = true
		}
	}
	if !sawReversal {
		t.Fatalf("điều chỉnh âm phải bù vào dòng cùng tài khoản: %+v", b.Lines)
	}
	// Không có dòng nào mang số lượng bản ghi nguồn / giao dịch chi tiết: số dòng ≤ 2 × số (nhóm × chiều).
	if len(b.Lines) > 2*len(summary()) {
		t.Fatalf("quá nhiều dòng: %d", len(b.Lines))
	}
}

// B-03: cùng đầu vào → cùng dòng, cùng băm (thứ tự dòng tổng hợp không ảnh hưởng).
func TestBuildDeterministic_B03(t *testing.T) {
	a, err := domain.Build("2026-09", summary(), mapping())
	if err != nil {
		t.Fatal(err)
	}
	rev := summary()
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	shuffled := domain.Mapping{Version: 1, Rules: append([]domain.Rule(nil), domain.DefaultRules()...)}
	for i, j := 0, len(shuffled.Rules)-1; i < j; i, j = i+1, j-1 {
		shuffled.Rules[i], shuffled.Rules[j] = shuffled.Rules[j], shuffled.Rules[i]
	}
	b, err := domain.Build("2026-09", rev, shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if a.LinesSHA256 != b.LinesSHA256 || len(a.Lines) != len(b.Lines) {
		t.Fatalf("băm khác: %s vs %s", a.LinesSHA256, b.LinesSHA256)
	}
	if domain.RulesSHA256(domain.DefaultRules()) != domain.RulesSHA256(shuffled.Rules) {
		t.Fatal("băm bảng ánh xạ phải không phụ thuộc thứ tự")
	}
}

func TestBuildMappingMissingAndEmpty(t *testing.T) {
	m := domain.Mapping{Version: 1, Rules: []domain.Rule{{Category: cp.CatStrawPurchase, DimensionKind: "htx", DebitAccount: "152", CreditAccount: "331",
		DimensionOn: domain.DimOnCredit}}}
	_, err := domain.Build("2026-09", summary(), m)
	var mr domain.MissingRule
	if !errors.Is(err, domain.ErrMappingMissing) || !errors.As(err, &mr) || mr.Category != cp.CatTransportWaterway {
		t.Fatalf("thiếu ánh xạ: %v", err)
	}
	if _, err := domain.Build("2026-09", []cp.SummaryLine{{Category: cp.CatLoss, DimensionKind: "none", Amount: money.MustAmount("0")}}, mapping()); !errors.Is(err, domain.ErrEmptyPeriod) {
		t.Fatalf("kỳ rỗng: %v", err)
	}
	// Nợ/Có triệt tiêu về 0 → rỗng.
	if _, err := domain.Build("2026-09", []cp.SummaryLine{
		{Category: cp.CatStorage, DimensionKind: "facility", Amount: money.MustAmount("10")},
		{Category: cp.CatStorage, DimensionKind: "facility", Amount: money.MustAmount("-10")},
	}, mapping()); !errors.Is(err, domain.ErrEmptyPeriod) {
		t.Fatalf("triệt tiêu: %v", err)
	}
}

func TestCheckBalancedRejects(t *testing.T) {
	ls := []abport.JournalLine{
		{LineNo: 1, Account: "152", Side: abport.Debit, Amount: money.MustAmount("10.00")},
		{LineNo: 2, Account: "331", Side: abport.Credit, Amount: money.MustAmount("9.99")},
	}
	if err := domain.CheckBalanced(ls); !errors.Is(err, abport.ErrUnbalanced) {
		t.Fatalf("lệch: %v", err)
	}
	ls[1].Amount = money.MustAmount("10.00")
	if err := domain.CheckBalanced(ls); err != nil {
		t.Fatal(err)
	}
}

// B-05: bảng ánh xạ hợp lệ; mặc định tạm hợp lệ và phủ mọi nhóm của costing.
func TestValidateRules_B05(t *testing.T) {
	if err := domain.ValidateRules(domain.DefaultRules()); err != nil {
		t.Fatal(err)
	}
	m := mapping()
	for _, c := range []cp.CostCategory{cp.CatStrawPurchase, cp.CatCollectionLabour, cp.CatConsumables, cp.CatTransportRoad, cp.CatTransportWaterway,
		cp.CatHubProcessing, cp.CatStorage, cp.CatHandling, cp.CatLoss, cp.CatSalesRevenue, cp.CatCarbonRevenue, cp.CatCarbonShare} {
		if _, ok := m.Lookup(c, "anything"); !ok {
			t.Errorf("mặc định thiếu nhóm %s", c)
		}
	}
	bad := [][]domain.Rule{
		nil,
		{{Category: "straw_purchase", DimensionKind: "*", DebitAccount: "152", CreditAccount: "152", DimensionOn: "none"}},
		{{Category: "straw_purchase", DimensionKind: "*", DebitAccount: "abc", CreditAccount: "331", DimensionOn: "none"}},
		{{Category: "Straw", DimensionKind: "*", DebitAccount: "152", CreditAccount: "331", DimensionOn: "none"}},
		{{Category: "straw_purchase", DimensionKind: "*", DebitAccount: "152", CreditAccount: "331", DimensionOn: "both"}},
		{{Category: "straw_purchase", DimensionKind: "*", DebitAccount: "152", CreditAccount: "331", DimensionOn: "none"},
			{Category: "straw_purchase", DimensionKind: "*", DebitAccount: "621", CreditAccount: "331", DimensionOn: "none"}},
	}
	for i, r := range bad {
		if err := domain.ValidateRules(r); err == nil {
			t.Errorf("bộ %d phải bị từ chối", i)
		}
	}
	// Khớp đúng chiều thắng "*".
	m2 := domain.Mapping{Rules: []domain.Rule{
		{Category: "storage", DimensionKind: "*", DebitAccount: "627", CreditAccount: "331", DimensionOn: "none"},
		{Category: "storage", DimensionKind: "facility", DebitAccount: "6277", CreditAccount: "331", DimensionOn: "none"},
	}}
	if r, _ := m2.Lookup("storage", "facility"); r.DebitAccount != "6277" {
		t.Fatalf("khớp đúng chiều: %+v", r)
	}
	if r, _ := m2.Lookup("storage", "mode"); r.DebitAccount != "627" {
		t.Fatalf("dự phòng *: %+v", r)
	}
}

func TestBackoff_R62(t *testing.T) {
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		if got := domain.Backoff(i + 1); got != w {
			t.Errorf("lần %d: %v, muốn %v", i+1, got, w)
		}
	}
}

func goldenJournal(t *testing.T) abport.SummaryJournal {
	t.Helper()
	b, err := domain.Build("2026-09", summary(), mapping())
	if err != nil {
		t.Fatal(err)
	}
	jid := uuid.MustParse("0192a000-0000-7000-8000-000000000001")
	return abport.SummaryJournal{
		JournalID: jid, TenantID: uuid.MustParse("0192a000-0000-7000-8000-0000000000ff"), PeriodID: uuid.MustParse("0192a000-0000-7000-8000-000000000002"),
		PeriodCode: "2026-09", CloseRunID: uuid.MustParse("0192a000-0000-7000-8000-000000000003"), Currency: "VND", Lines: b.Lines,
		TotalDebit: b.TotalDebit, TotalCredit: b.TotalCredit, SourceHash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		MappingVersion: 1, IdempotencyKey: domain.IdempotencyKey(jid),
		// Người duyệt / thời điểm KHÔNG vào tệp (tất định theo nội dung bút toán).
		ApprovedBy: uuid.New(), ApprovedAt: time.Now(),
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (chạy go test -run Golden -update để tạo)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s khác golden byte-cho-byte:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// B-06: tệp xuất CSV + XML tất định byte-cho-byte (UTF-8 không BOM, CRLF cho CSV, LF cho XML).
func TestExportGolden_B06(t *testing.T) {
	j := goldenJournal(t)
	csv, xml := domain.RenderCSV(j), domain.RenderXML(j)
	golden(t, "journal_2026-09.csv", csv)
	golden(t, "journal_2026-09.xml", xml)
	if bytes.HasPrefix(csv, []byte{0xEF, 0xBB, 0xBF}) || !bytes.Contains(csv, []byte("\r\n")) {
		t.Fatal("CSV phải UTF-8 không BOM, CRLF")
	}
	// Người duyệt khác, thời điểm khác → cùng byte.
	j2 := goldenJournal(t)
	if !bytes.Equal(domain.RenderCSV(j2), csv) || !bytes.Equal(domain.RenderXML(j2), xml) {
		t.Fatal("tệp xuất phải tất định")
	}
	man := domain.Manifest(map[string][]byte{"b.xml": xml, "a.csv": csv})
	want := domain.SHA256Hex(csv) + "  a.csv\n" + domain.SHA256Hex(xml) + "  b.xml\n"
	if string(man) != want {
		t.Fatalf("manifest: %q", man)
	}
}

func TestCSVQuoting(t *testing.T) {
	j := goldenJournal(t)
	j.Lines = []abport.JournalLine{
		{LineNo: 1, Account: "152", Side: abport.Debit, Amount: money.MustAmount("1.00"), Category: "straw_purchase", Description: `a;b "c"`},
		{LineNo: 2, Account: "331", Side: abport.Credit, Amount: money.MustAmount("1.00"), Category: "straw_purchase", Description: "x"},
	}
	out := string(domain.RenderCSV(j))
	if !bytes.Contains([]byte(out), []byte(`;"a;b ""c"""`+"\r\n")) {
		t.Fatalf("trích dẫn CSV: %s", out)
	}
	x := string(domain.RenderXML(j))
	if !bytes.Contains([]byte(x), []byte(`a;b &#34;c&#34;`)) {
		t.Fatalf("thoát XML: %s", x)
	}
}
