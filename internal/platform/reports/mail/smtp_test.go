package mail

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

func TestComposeUTF8NoAttachment(t *testing.T) {
	b, err := Compose("AgriGreen <no-reply@agrigreen.vn>", port.Message{To: "a@b.vn", Subject: "Báo cáo tiến độ gieo sạ — Tháng 09/2026", Text: "Tải tệp: https://x/y"},
		time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	head, body, _ := strings.Cut(s, "\r\n\r\n")
	if !strings.Contains(head, "Subject: =?utf-8?q?") || !strings.Contains(head, "Content-Type: text/plain; charset=utf-8") ||
		strings.Contains(strings.ToLower(head), "multipart") || !strings.Contains(head, "Date: Mon, 28 Sep 2026 10:00:00 +0700") {
		t.Fatalf("tiêu đề thư: %s", head)
	}
	dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", ""))
	if err != nil || string(dec) != "Tải tệp: https://x/y" {
		t.Fatalf("thân: %q %v", dec, err)
	}
	if _, err := Compose("a@b.vn", port.Message{To: "a@b.vn\r\nBcc: x@y", Subject: "x"}, time.Now()); err == nil {
		t.Fatal("chặn chèn tiêu đề")
	}
}

func TestConfigFromEnvRequiresTLS(t *testing.T) {
	t.Setenv("AG_SMTP_HOST", "")
	if _, err := ConfigFromEnv(); !errors.Is(err, ErrNotConfigured) {
		t.Fatal("chưa cấu hình")
	}
	t.Setenv("AG_SMTP_HOST", "smtp.test")
	t.Setenv("AG_SMTP_FROM", "no-reply@agrigreen.vn")
	t.Setenv("AG_SMTP_TLS", "none")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("không cho bản rõ")
	}
	t.Setenv("AG_SMTP_TLS", "tls")
	c, err := ConfigFromEnv()
	if err != nil || c.Port != 465 {
		t.Fatalf("TLS ngầm định cổng 465: %+v %v", c, err)
	}
}
