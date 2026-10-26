package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// fakeParams ghi lại khóa, thời điểm tra và tenant của ctx; trả giá trị theo khóa.
type fakeParams struct {
	vals    map[string]string
	err     error
	keys    []string
	ats     []time.Time
	tenants []uuid.UUID
}

func (f *fakeParams) At(ctx context.Context, key string, at time.Time) (params.ParamValue, error) {
	tid, _ := tenant.FromContext(ctx)
	f.keys, f.ats, f.tenants = append(f.keys, key), append(f.ats, at), append(f.tenants, tid)
	if f.err != nil {
		return params.ParamValue{}, f.err
	}
	v, ok := f.vals[key]
	if !ok {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return params.ParamValue{Key: key, Value: decimal.RequireFromString(v)}, nil
}

// KN-PARAMS-CNTS-DIARY-OWNER (#85): hạn sửa / hạn lưu GPS đọc phiên bản đã duyệt có hiệu lực ở NGÀY NGHIỆP VỤ (giờ Việt
// Nam) của thời điểm sự việc; chưa có giá trị → mặc định 7/365 theo hợp đồng; lỗi hệ thống hoặc giá trị sai → 503, không
// âm thầm rơi về mặc định.
func TestDiaryParams_VersionAtBusinessDay(t *testing.T) {
	tid := uuid.New()
	ctx := tenant.WithID(context.Background(), tid)
	// 00:30 Thứ Tư 20/10/2027 giờ Việt Nam = 17:30 UTC ngày 19/10.
	at := time.Date(2027, 10, 19, 17, 30, 0, 0, time.UTC)

	if got, err := (&Service{}).editDays(ctx, at); err != nil || got != EditDays {
		t.Fatalf("không nối tham số → mặc định %d: %d %v", EditDays, got, err)
	}
	if got, err := (&Service{}).fixRetentionDays(ctx, at); err != nil || got != FixRetentionDays {
		t.Fatalf("không nối tham số → mặc định %d: %d %v", FixRetentionDays, got, err)
	}

	empty := &fakeParams{}
	s := &Service{Params: empty}
	if got, err := s.editDays(ctx, at); err != nil || got != 7 {
		t.Fatalf("chưa có phiên bản đã duyệt → 7: %d %v", got, err)
	}
	if got, err := s.fixRetentionDays(ctx, at); err != nil || got != 365 {
		t.Fatalf("chưa có phiên bản đã duyệt → 365: %d %v", got, err)
	}

	p := &fakeParams{vals: map[string]string{knport.ParamCNTSDiaryEditWindowDays: "3", knport.ParamCNTSDiaryGPSRetentionDays: "90"}}
	s = &Service{Params: p}
	if got, err := s.editDays(ctx, at); err != nil || got != 3 {
		t.Fatalf("hạn sửa đã duyệt 3: %d %v", got, err)
	}
	if got, err := s.fixRetentionDays(ctx, at); err != nil || got != 90 {
		t.Fatalf("hạn lưu đã duyệt 90: %d %v", got, err)
	}
	wantAt := clock.BusinessDay(at)
	if !wantAt.Equal(time.Date(2027, 10, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("ngày nghiệp vụ VN: %v", wantAt)
	}
	for i, k := range []string{knport.ParamCNTSDiaryEditWindowDays, knport.ParamCNTSDiaryGPSRetentionDays} {
		if p.keys[i] != k || !p.ats[i].Equal(wantAt) || p.tenants[i] != tid {
			t.Fatalf("tra %d: khóa %s tại %v tenant %v — muốn %s tại %v tenant %v", i, p.keys[i], p.ats[i], p.tenants[i], k, wantAt, tid)
		}
	}

	boom := &Service{Params: &fakeParams{err: errors.New("pool cạn")}}
	for name, fn := range map[string]func(context.Context, time.Time) (int, error){"edit": boom.editDays, "retention": boom.fixRetentionDays} {
		if _, err := fn(ctx, at); !isDependency(err) {
			t.Fatalf("%s: lỗi hệ thống phải là 503 dependency_unavailable, không rơi về mặc định: %v", name, err)
		}
	}

	for _, tc := range []struct {
		key, value string
	}{
		{knport.ParamCNTSDiaryEditWindowDays, "2.5"},
		{knport.ParamCNTSDiaryEditWindowDays, "-1"},
		{knport.ParamCNTSDiaryEditWindowDays, "31"},
		{knport.ParamCNTSDiaryGPSRetentionDays, "0"},
		{knport.ParamCNTSDiaryGPSRetentionDays, "3651"},
		{knport.ParamCNTSDiaryGPSRetentionDays, "1e12"},
	} {
		s := &Service{Params: &fakeParams{vals: map[string]string{tc.key: tc.value}}}
		fn := s.editDays
		if tc.key == knport.ParamCNTSDiaryGPSRetentionDays {
			fn = s.fixRetentionDays
		}
		if _, err := fn(ctx, at); !isDependency(err) {
			t.Fatalf("%s=%s phải bị từ chối (503), không cắt/làm tròn: %v", tc.key, tc.value, err)
		}
	}
	// Biên hợp lệ.
	s = &Service{Params: &fakeParams{vals: map[string]string{knport.ParamCNTSDiaryEditWindowDays: "0", knport.ParamCNTSDiaryGPSRetentionDays: "3650"}}}
	if got, err := s.editDays(ctx, at); err != nil || got != 0 {
		t.Fatalf("hạn sửa 0 (chỉ trong ngày ghi): %d %v", got, err)
	}
	if got, err := s.fixRetentionDays(ctx, at); err != nil || got != 3650 {
		t.Fatalf("hạn lưu 3650: %d %v", got, err)
	}
}

// isDependency: lỗi là 503 extension.dependency_unavailable (ErrDependency, có thể kèm chi tiết/nguyên nhân).
func isDependency(err error) bool {
	var pb *problem.Problem
	return errors.As(err, &pb) && pb.Code == ErrDependency.Code && pb.Status == ErrDependency.Status
}
