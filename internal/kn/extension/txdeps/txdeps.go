// Package txdeps: dữ liệu module khác của MỘT thao tác KN đọc NGOÀI giao dịch (P0-POOL-KN-2, #139 — tổng quát hóa cơ chế
// reportcycle/app/deps.go của #125).
//
// Gốc lắp ráp mở MỘT pool cho mọi module (ADR-0028); port của module khác (pf/party, pf/masterdata, pf/accounts, cổng nội bộ
// KN như KHTCPrograms) mở giao dịch RIÊNG trên pool đó. Gọi port bên trong giao dịch KN thì thao tác giữ một kết nối trong
// khi chờ kết nối thứ hai — N thao tác song song với N = cỡ pool treo cả API tới hết hạn.
//
// Cách dùng: đầu thao tác gắn bộ nhớ vào ctx (With); mọi lời gọi port của module khác đi qua Get với khóa ổn định. Ngoài
// giao dịch Get nạp ngay và nhớ (cả lỗi). Run chạy thân giao dịch với bộ nhớ "niêm phong": Get thiếu khóa → ghi nhận, trả
// ErrNeed (không gọi port); hết thân giao dịch mà còn khóa thiếu (dù thân trả lỗi gì hay nuốt lỗi) → hủy giao dịch, nạp phần
// thiếu NGOÀI giao dịch, chạy lại thân từ đầu (đọc lại và kiểm lại mọi thứ trong giao dịch mới). Quá MaxAttempts lần →
// ErrExhausted (bên gọi trả 503 dependency_unavailable — mặc định chặn).
//
// Ngữ nghĩa giữ nguyên: giá trị đọc lúc thao tác như trước (trước đây port cũng đọc ở giao dịch riêng, không cùng ảnh chụp
// với giao dịch KN); lỗi của port chỉ trả cho thân giao dịch khi thân thực sự cần giá trị đó (lỗi được nhớ cùng khóa).
// Thân giao dịch phải đặt lại mọi kết quả ra ngoài ở đầu mỗi lần chạy và không có tác dụng phụ ngoài DB.
package txdeps

import (
	"context"
	"errors"
	"fmt"
)

// ErrNeed: thân giao dịch cần dữ liệu module khác chưa nạp — hủy giao dịch, nạp ngoài giao dịch, chạy lại.
var ErrNeed = errors.New("txdeps: cần dữ liệu module khác — nạp ngoài giao dịch rồi chạy lại")

// ErrExhausted: chạy lại MaxAttempts lần vẫn thiếu dữ liệu (dữ liệu module khác đổi liên tục / khóa không ổn định).
var ErrExhausted = errors.New("txdeps: dữ liệu module khác thay đổi liên tục")

// MaxAttempts: số lần chạy thân giao dịch tối đa (lần đầu thường thiếu, lần hai đủ; dư cho nhánh mới lộ ra khóa mới).
const MaxAttempts = 5

type entry struct {
	v   any
	err error
}

// Set là bộ nhớ dữ liệu module khác của MỘT thao tác (không dùng chung giữa goroutine).
type Set struct {
	sealed bool
	vals   map[string]entry
	want   map[string]func(context.Context) (any, error)
	order  []string
}

// New tạo bộ nhớ rỗng.
func New() *Set {
	return &Set{vals: map[string]entry{}, want: map[string]func(context.Context) (any, error){}}
}

type ctxKey struct{}

// With gắn bộ nhớ MỚI vào ctx (đầu thao tác).
func With(ctx context.Context) context.Context { return context.WithValue(ctx, ctxKey{}, New()) }

// detach bỏ bộ nhớ khỏi ctx cho lời gọi port: cài đặt port có thể là use case KN khác (vd KHTCPrograms) tự mở giao dịch
// bằng Run — không được dùng chung (và niêm phong) bộ nhớ của thao tác gọi.
func detach(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, (*Set)(nil))
}

// From trả bộ nhớ của ctx (nil nếu không có).
func From(ctx context.Context) *Set {
	d, _ := ctx.Value(ctxKey{}).(*Set)
	return d
}

// Get trả giá trị của khóa: đã nhớ → giá trị (và lỗi) đã nhớ; ngoài giao dịch → nạp ngay, nhớ; trong giao dịch (niêm
// phong) → ghi nhận khóa thiếu, trả ErrNeed. ctx không mang bộ nhớ → gọi load trực tiếp (hành vi cũ).
func Get[T any](ctx context.Context, key string, load func(context.Context) (T, error)) (T, error) {
	d := From(ctx)
	if d == nil {
		return load(ctx)
	}
	var zero T
	if e, ok := d.vals[key]; ok {
		if e.err != nil {
			return zero, e.err
		}
		v, _ := e.v.(T)
		return v, nil
	}
	if d.sealed {
		if _, ok := d.want[key]; !ok {
			d.want[key] = func(ctx context.Context) (any, error) { return load(ctx) }
			d.order = append(d.order, key)
		}
		return zero, ErrNeed
	}
	v, err := load(detach(ctx))
	d.vals[key] = entry{v: v, err: err}
	return v, err
}

// Pending: còn khóa đã ghi nhận mà chưa nạp.
func (d *Set) Pending() bool { return d != nil && len(d.want) > 0 }

// fill nạp mọi khóa đã ghi nhận — CHỈ gọi ngoài giao dịch.
func (d *Set) fill(ctx context.Context) {
	for _, k := range d.order {
		if load, ok := d.want[k]; ok {
			v, err := load(detach(ctx))
			d.vals[k] = entry{v: v, err: err}
		}
	}
	d.want = map[string]func(context.Context) (any, error){}
	d.order = nil
}

// Run chạy body bằng runTx (mở MỘT giao dịch và gọi body với giao dịch đó) với bộ nhớ của ctx niêm phong; thiếu dữ liệu →
// hủy, nạp ngoài giao dịch, chạy lại. ctx không mang bộ nhớ → chạy một lần như cũ.
func Run[X any](ctx context.Context, runTx func(body func(X) error) error, body func(X) error) error {
	d := From(ctx)
	if d == nil {
		return runTx(body)
	}
	if d.sealed {
		return fmt.Errorf("txdeps: Run lồng trong giao dịch đang niêm phong")
	}
	for attempt := 1; ; attempt++ {
		d.sealed = true
		err := runTx(func(x X) error {
			if err := body(x); err != nil {
				return err
			}
			if d.Pending() {
				return ErrNeed // hủy giao dịch: thân đã chạy với giá trị thiếu (lỗi bị nuốt)
			}
			return nil
		})
		d.sealed = false
		if !d.Pending() {
			if errors.Is(err, ErrNeed) {
				return fmt.Errorf("txdeps: ErrNeed không kèm khóa thiếu: %w", err)
			}
			return err
		}
		if attempt >= MaxAttempts {
			return ErrExhausted
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d.fill(ctx)
	}
}
