package port_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ADR-0023 mục 2: metering.units.v1.
func TestUnitsV1_ADR0023(t *testing.T) {
	for _, c := range []struct {
		name string
		call port.Call
		want int64
	}{
		{"đọc 200", port.Call{Kind: port.CallRead, Status: 200}, 1},
		{"đọc lặp idempotent", port.Call{Kind: port.CallRead, Status: 200, Replayed: true}, 0},
		{"đọc 403", port.Call{Kind: port.CallRead, Status: 403}, 0},
		{"đọc 429", port.Call{Kind: port.CallRead, Status: 429}, 0},
		{"yêu cầu xuất 202", port.Call{Kind: port.CallExportRequest, Status: 202}, 0},
		{"tải 1 dòng", port.Call{Kind: port.CallFileDownload, Status: 200, Rows: 1}, 1},
		{"tải 1000 dòng", port.Call{Kind: port.CallFileDownload, Status: 200, Rows: 1000}, 1},
		{"tải 1001 dòng", port.Call{Kind: port.CallFileDownload, Status: 200, Rows: 1001}, 2},
		{"tải rỗng", port.Call{Kind: port.CallFileDownload, Status: 200}, 1},
		{"meta", port.Call{Kind: port.CallMeta, Status: 200}, 0},
	} {
		if got := port.UnitsV1(c.call); got != c.want {
			t.Errorf("%s: muốn %d, có %d", c.name, c.want, got)
		}
	}
}

type fakeCP struct {
	d   port.Decision
	err error
	n   int
}

func (f *fakeCP) Pass(context.Context, port.Egress) (port.Decision, error) { f.n++; return f.d, f.err }

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
	e := port.Egress{Channel: port.ChannelReportExport}
	if err := port.Guard(ctx, nil, port.ModeEnforce, e, nil); err != nil {
		t.Fatalf("chưa nối checkpoint phải giữ hành vi cũ: %v", err)
	}
	deny := &fakeCP{d: port.Decision{Allowed: false, Reason: "quota_exceeded"}}
	if err := port.Guard(ctx, deny, port.ModeObserve, e, nil); err != nil {
		t.Fatalf("observe không được từ chối: %v", err)
	}
	if err := port.Guard(ctx, deny, port.ModeEnforce, e, nil); code(err) != "metering.egress_quota_exceeded" {
		t.Fatalf("enforce phải từ chối bằng metering.egress_quota_exceeded: %v", err)
	}
	broken := &fakeCP{err: errors.New("db down")}
	if err := port.Guard(ctx, broken, port.ModeObserve, e, nil); err != nil {
		t.Fatalf("observe: checkpoint lỗi vẫn tiếp tục: %v", err)
	}
	if err := port.Guard(ctx, broken, port.ModeEnforce, e, nil); code(err) != "metering.egress_unavailable" {
		t.Fatalf("enforce: checkpoint lỗi → metering.egress_unavailable: %v", err)
	}
	ok := &fakeCP{d: port.Decision{Allowed: true}}
	if err := port.Guard(ctx, ok, port.ModeEnforce, e, nil); err != nil || ok.n != 1 {
		t.Fatalf("cho qua: %v (gọi %d lần)", err, ok.n)
	}
}

func TestParseMode_ADR0024(t *testing.T) {
	for in, want := range map[string]port.Mode{"": port.ModeObserve, "observe": port.ModeObserve, " enforce ": port.ModeEnforce} {
		if got, err := port.ParseMode(in); err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	if _, err := port.ParseMode("block"); err == nil {
		t.Fatal("giá trị lạ phải lỗi (không lặng lẽ observe)")
	}
	if port.EgressParam(port.ParamEgressDailyQuotaFmt, port.ChannelValve) != "metering.egress.valve.daily_records_quota" {
		t.Fatal("khóa tham số kênh sai")
	}
}

// A5 (03/10/2026): xuất nội bộ (Internal) luôn observe — enforce + vượt hạn mức hoặc checkpoint lỗi vẫn cho qua,
// checkpoint vẫn được gọi (ghi egress_event); kênh ngoài (Internal = false) vẫn bị chặn.
func TestGuardInternalNeverDenied_A5(t *testing.T) {
	ctx := context.Background()
	in := port.Egress{Channel: port.ChannelReportExport, Internal: true}
	deny := &fakeCP{d: port.Decision{Allowed: false, Reason: "quota_exceeded"}}
	if err := port.Guard(ctx, deny, port.ModeEnforce, in, nil); err != nil || deny.n != 1 {
		t.Fatalf("nội bộ ở enforce không được từ chối: %v (gọi %d lần)", err, deny.n)
	}
	if err := port.Guard(ctx, &fakeCP{err: errors.New("db down")}, port.ModeEnforce, in, nil); err != nil {
		t.Fatalf("nội bộ: checkpoint lỗi vẫn tiếp tục: %v", err)
	}
	out := in
	out.Internal = false
	if err := port.Guard(ctx, deny, port.ModeEnforce, out, nil); code(err) != "metering.egress_quota_exceeded" {
		t.Fatalf("kênh ngoài vẫn phải bị chặn ở enforce: %v", err)
	}
	if in.EffectiveMode(port.ModeEnforce) != port.ModeObserve || out.EffectiveMode(port.ModeEnforce) != port.ModeEnforce {
		t.Fatal("EffectiveMode")
	}
}
