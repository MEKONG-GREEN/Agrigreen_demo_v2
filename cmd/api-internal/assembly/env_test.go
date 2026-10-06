package assembly

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// M15: biến job đã đặt nhưng sai cú pháp → lỗi khởi động; chưa đặt → mặc định (không lỗi).
func TestEnvUUID(t *testing.T) {
	const name = "AG_TEST_ENV_UUID"
	want := uuid.MustParse("6f1c2a4e-1b7d-4c39-9e0a-2f5b8d7c1a11")
	cases := []struct {
		val     string
		want    uuid.UUID
		wantErr bool
	}{
		{"", uuid.Nil, false},
		{"   ", uuid.Nil, false},
		{want.String(), want, false},
		{" " + want.String() + " ", want, false},
		{"6f1c2a4e-1b7d-4c39-9e0a-2f5b8d7c1a1", uuid.Nil, true}, // thiếu một ký tự
		{"mkg", uuid.Nil, true},
		{uuid.Nil.String(), uuid.Nil, true},
	}
	for _, c := range cases {
		t.Setenv(name, c.val)
		got, err := EnvUUID(name)
		if (err != nil) != c.wantErr || got != c.want {
			t.Fatalf("EnvUUID(%q) = %v, %v; muốn %v, lỗi=%v", c.val, got, err, c.want, c.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), name) {
			t.Fatalf("lỗi phải nêu tên biến: %v", err)
		}
	}
}

func TestEnvDuration(t *testing.T) {
	const name = "AG_TEST_ENV_DURATION"
	cases := []struct {
		val     string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"5m", 5 * time.Minute, false},
		{" 1h ", time.Hour, false},
		{"5", 0, true}, // thiếu đơn vị
		{"5 phút", 0, true},
		{"0s", 0, true},
		{"-1m", 0, true},
	}
	for _, c := range cases {
		t.Setenv(name, c.val)
		got, err := EnvDuration(name)
		if (err != nil) != c.wantErr || got != c.want {
			t.Fatalf("EnvDuration(%q) = %v, %v; muốn %v, lỗi=%v", c.val, got, err, c.want, c.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), name) {
			t.Fatalf("lỗi phải nêu tên biến: %v", err)
		}
	}
}

// Jobs trả lỗi trước khi chạm module khi biến của job được chọn sai cú pháp.
func TestJobsRejectsInvalidEnv(t *testing.T) {
	for _, c := range []struct{ job, env, val string }{
		{"fieldops_htx_responses", "AG_MKG_TENANT_ID", "not-a-uuid"},
		{"fieldops_harvest_dates", "AG_FIELDOPS_HARVEST_POLL_INTERVAL", "1 giờ"},
		{"logistics_alerts", "AG_LOGISTICS_ALERT_INTERVAL", "15"},
	} {
		t.Run(c.job, func(t *testing.T) {
			t.Setenv("AG_MKG_TENANT_ID", "")
			t.Setenv(c.env, c.val)
			jobs, err := Jobs(Modules{}, nil, c.job, nil)
			if err == nil || !strings.Contains(err.Error(), c.env) {
				t.Fatalf("Jobs(%s) với %s=%q: lỗi = %v, jobs = %d; muốn lỗi nêu %s", c.job, c.env, c.val, err, len(jobs), c.env)
			}
		})
	}
}
