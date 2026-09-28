package channel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

// SMTP gửi email qua máy chủ SMTP TRONG LÃNH THỔ (ADR-0007). Mặc định bắt buộc
// STARTTLS; cổng 465 dùng TLS ngay từ đầu.
type SMTP struct {
	Host, Username, Password, From string
	Port                           int
	// AllowPlain cho phép gửi không mã hóa — CHỈ Local/Dev (hộp thư giả như MailHog).
	AllowPlain bool
	Timeout    time.Duration
}

// SMTPFromEnv đọc AG_SMTP_HOST, AG_SMTP_PORT (mặc định 587), AG_SMTP_USERNAME,
// AG_SMTP_PASSWORD, AG_SMTP_FROM, AG_SMTP_ALLOW_PLAIN. Chưa đặt AG_SMTP_HOST → nil
// (tắt email tóm tắt).
func SMTPFromEnv() (*SMTP, error) {
	host := os.Getenv("AG_SMTP_HOST")
	if host == "" {
		return nil, nil
	}
	port := 587
	if p := os.Getenv("AG_SMTP_PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("channel: AG_SMTP_PORT không hợp lệ")
		}
		port = n
	}
	from := os.Getenv("AG_SMTP_FROM")
	if from == "" {
		return nil, errors.New("channel: thiếu AG_SMTP_FROM")
	}
	return &SMTP{Host: host, Port: port, Username: os.Getenv("AG_SMTP_USERNAME"), Password: os.Getenv("AG_SMTP_PASSWORD"),
		From: from, AllowPlain: os.Getenv("AG_SMTP_ALLOW_PLAIN") == "true"}, nil
}

// DigestRecipientsFromEnv đọc AG_SMTP_DIGEST_TO: danh sách hộp thư CÔNG VỤ (vd
// hộp thư chung của TTKN tỉnh), phân tách bằng dấu phẩy. Không dùng email cá nhân:
// email cá nhân là dữ liệu cá nhân, chỉ nằm trong kho định danh (R-04).
func DigestRecipientsFromEnv() []string {
	var out []string
	for _, s := range strings.Split(os.Getenv("AG_SMTP_DIGEST_TO"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Send thực hiện Mailer.
func (s *SMTP) Send(ctx context.Context, to []string, subject, body string) error {
	if len(to) == 0 {
		return errors.New("channel: email không có người nhận")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	d := net.Dialer{Timeout: timeout}
	var conn net.Conn
	var err error
	tlsCfg := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	if s.Port == 465 {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("channel: nối SMTP: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("channel: SMTP: %w", err)
	}
	defer func() { _ = c.Close() }()
	if s.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("channel: STARTTLS: %w", err)
			}
		} else if !s.AllowPlain {
			return errors.New("channel: máy chủ SMTP không hỗ trợ STARTTLS")
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("channel: xác thực SMTP: %w", err)
		}
	}
	if err := c.Mail(s.From); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(BuildMessage(s.From, to, subject, body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// BuildMessage dựng thư văn bản UTF-8 (tiêu đề mã hóa theo RFC 2047).
func BuildMessage(from string, to []string, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return []byte(b.String())
}

// LogMailer ghi log thay vì gửi (Local/Dev).
type LogMailer struct{ Log *slog.Logger }

// Send thực hiện Mailer — chỉ ghi số người nhận và tiêu đề.
func (m LogMailer) Send(ctx context.Context, to []string, subject, _ string) error {
	if m.Log != nil {
		m.Log.InfoContext(ctx, "email (dev, không gửi)", "event", "notify.email", "recipients", len(to), "subject", subject, "outcome", "logged")
	}
	return nil
}

// PusherFromEnv chọn kênh đẩy: có AG_FCM_PROJECT_ID hoặc AG_FCM_CREDENTIALS_FILE →
// FCM HTTP v1; không → LogPusher (Local/Dev).
func PusherFromEnv(log *slog.Logger) (Pusher, error) {
	if os.Getenv("AG_FCM_PROJECT_ID") == "" && os.Getenv("AG_FCM_CREDENTIALS_FILE") == "" {
		return LogPusher{Log: log}, nil
	}
	return FCMFromEnv()
}
