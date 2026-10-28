package txdeps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeTx đếm số giao dịch và ghi: thân trả lỗi → "rollback" (bỏ ghi của lần đó).
type fakeTx struct {
	open, commits int
	writes        []string
	inTx          bool
}

func (f *fakeTx) run(body func(*fakeTx) error) error {
	f.open++
	f.inTx = true
	defer func() { f.inTx = false }()
	before := len(f.writes)
	if err := body(f); err != nil {
		f.writes = f.writes[:before]
		return err
	}
	f.commits++
	return nil
}

func TestRunLoadsOutsideTxAndReruns(t *testing.T) {
	ctx := With(context.Background())
	tx := &fakeTx{}
	loads := 0
	unit := func(ctx context.Context) (string, error) {
		if tx.inTx {
			t.Fatal("port gọi trong giao dịch")
		}
		loads++
		return "89", nil
	}
	var got string
	err := Run(ctx, tx.run, func(*fakeTx) error {
		got = ""
		p, err := Get(ctx, "unit:30001", unit)
		if err != nil {
			return err
		}
		tx.writes = append(tx.writes, p)
		got = p
		return nil
	})
	if err != nil || got != "89" || loads != 1 || tx.open != 2 || tx.commits != 1 || len(tx.writes) != 1 {
		t.Fatalf("err=%v got=%q loads=%d open=%d commits=%d writes=%v", err, got, loads, tx.open, tx.commits, tx.writes)
	}
	// Đã nhớ: giao dịch sau không chạy lại, không nạp lại.
	if err := Run(ctx, tx.run, func(*fakeTx) error { _, err := Get(ctx, "unit:30001", unit); return err }); err != nil || loads != 1 || tx.open != 3 {
		t.Fatalf("nhớ: err=%v loads=%d open=%d", err, loads, tx.open)
	}
}

func TestRunRetriesWhenBodySwallowsErr(t *testing.T) {
	ctx := With(context.Background())
	tx := &fakeTx{}
	var names []string
	err := Run(ctx, tx.run, func(*fakeTx) error {
		names = nil
		n, err := Get(ctx, "names", func(context.Context) ([]string, error) { return []string{"A"}, nil })
		if err == nil { // lỗi tra tên không chặn (nuốt) — Run vẫn phải chạy lại khi có khóa thiếu
			names = n
		}
		tx.writes = append(tx.writes, fmt.Sprint(len(names)))
		return nil
	})
	if err != nil || len(names) != 1 || tx.commits != 1 || len(tx.writes) != 1 || tx.writes[0] != "1" {
		t.Fatalf("err=%v names=%v commits=%d writes=%v", err, names, tx.commits, tx.writes)
	}
}

func TestErrorRememberedAndSurfacedOnlyWhenUsed(t *testing.T) {
	ctx := With(context.Background())
	tx := &fakeTx{}
	boom := errors.New("port lỗi")
	err := Run(ctx, tx.run, func(*fakeTx) error {
		if _, err := Get(ctx, "a", func(context.Context) (int, error) { return 0, boom }); err != nil {
			return err
		}
		return nil
	})
	if !errors.Is(err, boom) || tx.open != 2 {
		t.Fatalf("lỗi port phải trả cho thân giao dịch (fail closed): %v open=%d", err, tx.open)
	}
}

func TestExhausted(t *testing.T) {
	ctx := With(context.Background())
	tx := &fakeTx{}
	i := 0
	err := Run(ctx, tx.run, func(*fakeTx) error {
		i++
		_, err := Get(ctx, fmt.Sprint("k", i), func(context.Context) (int, error) { return 1, nil })
		return err
	})
	if !errors.Is(err, ErrExhausted) || tx.open != MaxAttempts || tx.commits != 0 {
		t.Fatalf("err=%v open=%d commits=%d", err, tx.open, tx.commits)
	}
}

func TestNoSetIsPassThrough(t *testing.T) {
	ctx := context.Background()
	tx := &fakeTx{}
	calls := 0
	err := Run(ctx, tx.run, func(*fakeTx) error {
		_, err := Get(ctx, "x", func(context.Context) (int, error) { calls++; return 1, nil })
		return err
	})
	if err != nil || calls != 1 || tx.open != 1 {
		t.Fatalf("không bộ nhớ: err=%v calls=%d open=%d", err, calls, tx.open)
	}
}

// Port là use case KN khác tự chạy Run (vd KHTCPrograms): không dùng chung bộ nhớ của thao tác gọi (không đệ quy, không treo).
func TestNestedRunInLoaderIsDetached(t *testing.T) {
	ctx := With(context.Background())
	outer, inner := &fakeTx{}, &fakeTx{}
	program := func(ctx context.Context) (string, error) {
		if From(ctx) != nil {
			t.Fatal("port nhận bộ nhớ của thao tác gọi")
		}
		var name string
		err := Run(ctx, inner.run, func(*fakeTx) error { name = "CT-01"; return nil })
		return name, err
	}
	var got string
	err := Run(ctx, outer.run, func(*fakeTx) error {
		p, err := Get(ctx, "program", program)
		got = p
		return err
	})
	if err != nil || got != "CT-01" || outer.open != 2 || inner.open != 1 {
		t.Fatalf("err=%v got=%q outer=%d inner=%d", err, got, outer.open, inner.open)
	}
}

// P0-POOL-SHARED-2 (#143): Run lồng trong thân đang niêm phong (consumer inbox gọi use case tự chạy Run) dùng chung bộ
// nhớ — khóa thiếu của Run trong lan lên Run ngoài cùng (hủy, nạp ngoài giao dịch, chạy lại), không lỗi "Run lồng".
func TestNestedRunInSealedBodySharesSet(t *testing.T) {
	ctx := With(context.Background())
	outer, inner := &fakeTx{}, &fakeTx{}
	loads := 0
	unit := func(ctx context.Context) (string, error) {
		if outer.inTx || inner.inTx {
			t.Fatal("port gọi trong giao dịch")
		}
		loads++
		return "89", nil
	}
	var got string
	err := Run(ctx, outer.run, func(*fakeTx) error {
		got = ""
		return Run(ctx, inner.run, func(*fakeTx) error {
			p, err := Get(ctx, "unit", unit)
			got = p
			return err
		})
	})
	if err != nil || got != "89" || loads != 1 || outer.open != 2 || outer.commits != 1 {
		t.Fatalf("err=%v got=%q loads=%d outer=%d/%d inner=%d", err, got, loads, outer.open, outer.commits, inner.open)
	}
}

// Load panic (vd thư viện SMTP) → lỗi của khóa, thân chạy lại nhận lỗi đó; tiến trình không sập.
func TestLoadPanicBecomesError(t *testing.T) {
	ctx := With(context.Background())
	tx := &fakeTx{}
	err := Run(ctx, tx.run, func(*fakeTx) error {
		_, err := Get(ctx, "smtp", func(context.Context) (int, error) { panic("smtp: kết nối đứt") })
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "load panic") || tx.commits != 0 {
		t.Fatalf("err=%v commits=%d", err, tx.commits)
	}
}

// RunMax: số lần chạy tối đa riêng (consumer inbox).
func TestRunMaxAttempts(t *testing.T) {
	ctx := With(context.Background())
	tx := &fakeTx{}
	n := 0
	err := RunMax(ctx, 3, tx.run, func(*fakeTx) error {
		n++
		_, err := Get(ctx, fmt.Sprint("k", n), func(context.Context) (int, error) { return n, nil })
		return err
	})
	if !errors.Is(err, ErrExhausted) || tx.open != 3 {
		t.Fatalf("err=%v open=%d", err, tx.open)
	}
}
