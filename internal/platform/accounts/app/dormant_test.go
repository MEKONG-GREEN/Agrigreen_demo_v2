package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

type fakeParams map[string]string

func (f fakeParams) At(ctx context.Context, key string, _ time.Time) (params.ParamValue, error) {
	if _, err := tenant.FromContext(ctx); err != nil {
		return params.ParamValue{}, err
	}
	v, ok := f[key]
	if !ok {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	if v == "err" {
		return params.ParamValue{}, errors.New("db down")
	}
	return params.ParamValue{Key: key, Value: decimal.RequireFromString(v)}, nil
}

// D24: ngưỡng là tham số có phiên bản (QĐ-4 "mọi giá trị là tham số có phiên bản"); thiếu hoặc sai miền → 38/45.
func TestDormantPolicy_D24(t *testing.T) {
	ctx := context.Background()
	tid := uuid.New()
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	w, d := mdport.ParamAccountsDormantWarnDays, mdport.ParamAccountsDormantDisableDays
	cases := []struct {
		name string
		p    params.Params
		want DormantPolicy
		err  bool
	}{
		{"không nối tham số", nil, DormantPolicy{38, 45}, false},
		{"chưa có tham số", fakeParams{}, DormantPolicy{38, 45}, false},
		{"đã duyệt 23/30", fakeParams{w: "23", d: "30"}, DormantPolicy{23, 30}, false},
		{"báo trước không nhỏ hơn khóa", fakeParams{w: "45", d: "45"}, DormantPolicy{38, 45}, false},
		{"số lẻ", fakeParams{w: "37.5"}, DormantPolicy{38, 45}, false},
		{"ngoài miền", fakeParams{d: "400"}, DormantPolicy{38, 45}, false},
		{"lỗi tra cứu không lặng lẽ dùng mặc định", fakeParams{w: "err"}, DormantPolicy{}, true},
	}
	for _, c := range cases {
		s := &Service{Params: c.p}
		got, err := s.dormantPolicy(ctx, tid, now)
		if (err != nil) != c.err || (!c.err && got != c.want) {
			t.Errorf("%s: %+v %v", c.name, got, err)
		}
	}
	if got := IdleDays(now.Add(-38*24*time.Hour+time.Minute), now); got != 37 {
		t.Errorf("IdleDays làm tròn xuống: %d", got)
	}
}
