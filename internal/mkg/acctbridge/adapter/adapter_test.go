package adapter_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

func journal() port.SummaryJournal {
	jid := uuid.MustParse("0192a000-0000-7000-8000-000000000011")
	return port.SummaryJournal{JournalID: jid, TenantID: uuid.MustParse("0192a000-0000-7000-8000-0000000000ff"),
		PeriodID: uuid.MustParse("0192a000-0000-7000-8000-000000000012"), PeriodCode: "2026-09",
		CloseRunID: uuid.MustParse("0192a000-0000-7000-8000-000000000013"), Currency: "VND",
		Lines: []port.JournalLine{
			{LineNo: 1, Account: "152", Side: port.Debit, Amount: money.MustAmount("100.00"), Category: "straw_purchase"},
			{LineNo: 2, Account: "331", Side: port.Credit, Amount: money.MustAmount("100.00"), Category: "straw_purchase"},
		},
		TotalDebit: money.MustAmount("100.00"), TotalCredit: money.MustAmount("100.00"),
		SourceHash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", MappingVersion: 1, IdempotencyKey: domain.IdempotencyKey(jid)}
}

// B-06 / B-07: bộ xuất tệp ghi CSV + XML + tệp ký vào kho đối tượng; đẩy lại → cùng ExternalRef, cùng byte.
func TestFileExportIdempotent_B06(t *testing.T) {
	st, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fe := &adapter.FileExport{Store: st, Clock: clock.NewFixed(time.Now())}
	ctx := context.Background()
	j := journal()
	r1, err := fe.Push(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := fe.Push(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Ref != r2.Ref || r1.ArtifactSHA256 != r2.ArtifactSHA256 || r1.Adapter != adapter.NameFileExport {
		t.Fatalf("đẩy lại phải cùng ExternalRef: %+v %+v", r1, r2)
	}
	stem := r1.Ref
	rc, info, err := st.Get(ctx, stem+".csv")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, domain.RenderCSV(j)) || info.SHA256 != domain.SHA256Hex(got) {
		t.Fatal("CSV ở kho khác bản dựng")
	}
	rc, _, err = st.Get(ctx, stem+".sha256")
	if err != nil {
		t.Fatal(err)
	}
	man, _ := io.ReadAll(rc)
	_ = rc.Close()
	if domain.SHA256Hex(man) != r1.ArtifactSHA256 || !bytes.Contains(man, []byte(domain.SHA256Hex(domain.RenderXML(j))+"  2026-09_"+j.JournalID.String()+".xml")) {
		t.Fatalf("tệp ký: %s", man)
	}
	// Cùng khóa, nội dung khác (không thể xảy ra với bút toán bất biến) → từ chối vĩnh viễn.
	j.Lines[0].Description, j.Lines[1].Description = "khác", "khác"
	if _, err := fe.Push(ctx, j); !errors.Is(err, port.ErrAdapterRejected) {
		t.Fatalf("nội dung khác dưới cùng khóa: %v", err)
	}
	// Không cân → từ chối.
	u := journal()
	u.Lines[1].Amount = money.MustAmount("99.00")
	if _, err := fe.Push(ctx, u); !errors.Is(err, port.ErrAdapterRejected) {
		t.Fatalf("không cân: %v", err)
	}
	// Chưa có kho → tạm không nhận.
	if _, err := (&adapter.FileExport{}).Push(ctx, journal()); !errors.Is(err, port.ErrAdapterUnavailable) {
		t.Fatalf("chưa có kho: %v", err)
	}
}

func TestFakeAndUnconfigured(t *testing.T) {
	ctx := context.Background()
	f := adapter.NewFake(clock.NewFixed(time.Now()))
	f.FailNext(port.ErrAdapterUnavailable)
	if _, err := f.Push(ctx, journal()); !errors.Is(err, port.ErrAdapterUnavailable) {
		t.Fatal(err)
	}
	r1, err := f.Push(ctx, journal())
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := f.Push(ctx, journal())
	if r1.Ref != r2.Ref || f.Accepted() != 1 || f.Calls() != 3 {
		t.Fatalf("bộ giả idempotent: %+v %+v accepted=%d calls=%d", r1, r2, f.Accepted(), f.Calls())
	}
	if _, err := (adapter.Unconfigured{Reason: "x"}).Push(ctx, journal()); !errors.Is(err, port.ErrAdapterUnavailable) {
		t.Fatal(err)
	}
}
