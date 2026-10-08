package port_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ADR-0023 mục 2: metering.units.v1.
func TestUnitsV1_ADR0023(t *testing.T) {
	for _, c := range []struct {
		name string
		call meter.Call
		want int64
	}{
		{"đọc 200", meter.Call{Kind: meter.CallRead, Status: 200}, 1},
		{"đọc lặp idempotent", meter.Call{Kind: meter.CallRead, Status: 200, Replayed: true}, 0},
		{"đọc 403", meter.Call{Kind: meter.CallRead, Status: 403}, 0},
		{"đọc 429", meter.Call{Kind: meter.CallRead, Status: 429}, 0},
		{"yêu cầu xuất 202", meter.Call{Kind: meter.CallExportRequest, Status: 202}, 0},
		{"tải 1 dòng", meter.Call{Kind: meter.CallFileDownload, Status: 200, Rows: 1}, 1},
		{"tải 1000 dòng", meter.Call{Kind: meter.CallFileDownload, Status: 200, Rows: 1000}, 1},
		{"tải 1001 dòng", meter.Call{Kind: meter.CallFileDownload, Status: 200, Rows: 1001}, 2},
		{"tải rỗng", meter.Call{Kind: meter.CallFileDownload, Status: 200}, 1},
		{"meta", meter.Call{Kind: meter.CallMeta, Status: 200}, 0},
	} {
		if got := meter.UnitsV1(c.call); got != c.want {
			t.Errorf("%s: muốn %d, có %d", c.name, c.want, got)
		}
	}
}

type fakeCP struct {
	d   meter.Decision
	err error
	n   int
}

func (f *fakeCP) Pass(context.Context, meter.Egress) (meter.Decision, error) {
	f.n++
	return f.d, f.err
}

func code(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

// ADR-0024 mục 2: observe không bao giờ từ chối (kể cả checkpoint lỗi); enforce từ chối đúng mã;
// chưa nối checkpoint → hành vi cũ.
func TestGuardModes_ADR0024(t *testing.T) {
	ctx := context.Background()
	e := meter.Egress{Channel: meter.ChannelReportExport}
	if err := meter.Guard(ctx, nil, meter.ModeEnforce, e, nil); err != nil {
		t.Fatalf("chưa nối checkpoint phải giữ hành vi cũ: %v", err)
	}
	deny := &fakeCP{d: meter.Decision{Allowed: false, Reason: "quota_exceeded"}}
	if err := meter.Guard(ctx, deny, meter.ModeObserve, e, nil); err != nil {
		t.Fatalf("observe không được từ chối: %v", err)
	}
	if err := meter.Guard(ctx, deny, meter.ModeEnforce, e, nil); code(err) != "metering.egress_quota_exceeded" {
		t.Fatalf("enforce phải từ chối bằng metering.egress_quota_exceeded: %v", err)
	}
	broken := &fakeCP{err: errors.New("db down")}
	if err := meter.Guard(ctx, broken, meter.ModeObserve, e, nil); err != nil {
		t.Fatalf("observe: checkpoint lỗi vẫn tiếp tục: %v", err)
	}
	if err := meter.Guard(ctx, broken, meter.ModeEnforce, e, nil); code(err) != "metering.egress_unavailable" {
		t.Fatalf("enforce: checkpoint lỗi → metering.egress_unavailable: %v", err)
	}
	ok := &fakeCP{d: meter.Decision{Allowed: true}}
	if err := meter.Guard(ctx, ok, meter.ModeEnforce, e, nil); err != nil || ok.n != 1 {
		t.Fatalf("cho qua: %v (gọi %d lần)", err, ok.n)
	}
}

func TestParseMode_ADR0024(t *testing.T) {
	for in, want := range map[string]meter.Mode{"": meter.ModeObserve, "observe": meter.ModeObserve, " enforce ": meter.ModeEnforce} {
		if got, err := meter.ParseMode(in); err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	if _, err := meter.ParseMode("block"); err == nil {
		t.Fatal("giá trị lạ phải lỗi (không lặng lẽ observe)")
	}
	if port.EgressParam(port.ParamEgressDailyQuotaFmt, meter.ChannelValve) != "metering.egress.valve.daily_records_quota" {
		t.Fatal("khóa tham số kênh sai")
	}
}

// A5 (03/10/2026): xuất nội bộ (Internal) luôn observe — enforce + vượt hạn mức hoặc checkpoint lỗi vẫn cho qua,
// checkpoint vẫn được gọi (ghi egress_event); kênh ngoài (Internal = false) vẫn bị chặn.
func TestGuardInternalNeverDenied_A5(t *testing.T) {
	ctx := context.Background()
	in := meter.Egress{Channel: meter.ChannelReportExport, Internal: true}
	deny := &fakeCP{d: meter.Decision{Allowed: false, Reason: "quota_exceeded"}}
	if err := meter.Guard(ctx, deny, meter.ModeEnforce, in, nil); err != nil || deny.n != 1 {
		t.Fatalf("nội bộ ở enforce không được từ chối: %v (gọi %d lần)", err, deny.n)
	}
	if err := meter.Guard(ctx, &fakeCP{err: errors.New("db down")}, meter.ModeEnforce, in, nil); err != nil {
		t.Fatalf("nội bộ: checkpoint lỗi vẫn tiếp tục: %v", err)
	}
	out := in
	out.Internal = false
	if err := meter.Guard(ctx, deny, meter.ModeEnforce, out, nil); code(err) != "metering.egress_quota_exceeded" {
		t.Fatalf("kênh ngoài vẫn phải bị chặn ở enforce: %v", err)
	}
	if in.EffectiveMode(meter.ModeEnforce) != meter.ModeObserve || out.EffectiveMode(meter.ModeEnforce) != meter.ModeEnforce {
		t.Fatal("EffectiveMode")
	}
}
