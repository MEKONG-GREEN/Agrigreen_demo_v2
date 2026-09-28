// Package mail là bộ gửi email qua SMTP có TLS bắt buộc cho lịch gửi báo cáo
// (US-DASH-03). Không gửi đính kèm; mỗi người nhận một email riêng (không lộ
// địa chỉ của nhau). Không ghi địa chỉ email vào log (R-14).
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Chế độ TLS.
const (
	TLSStartTLS = "starttls" // cổng 587: STARTTLS bắt buộc
	TLSImplicit = "tls"      // cổng 465: TLS ngay từ đầu
)

// Config là cấu hình SMTP.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string `pii:"true"` // bí mật — không ghi log
	From     string
	TLS      string
}

// ErrNotConfigured: chưa đặt AG_SMTP_HOST.
var ErrNotConfigured = errors.New("mail: chưa cấu hình AG_SMTP_HOST")

// ConfigFromEnv đọc AG_SMTP_HOST, AG_SMTP_PORT, AG_SMTP_USERNAME, AG_SMTP_PASSWORD,
// AG_SMTP_FROM, AG_SMTP_TLS (starttls | tls; mặc định starttls). Không có chế độ bản rõ.
func ConfigFromEnv() (Config, error) {
	c := Config{
		Host: os.Getenv("AG_SMTP_HOST"), Username: os.Getenv("AG_SMTP_USERNAME"), Password: os.Getenv("AG_SMTP_PASSWORD"),
		From: os.Getenv("AG_SMTP_FROM"), TLS: strings.ToLower(os.Getenv("AG_SMTP_TLS")),
	}
	if c.Host == "" {
		return c, ErrNotConfigured
	}
	if c.TLS == "" {
		c.TLS = TLSStartTLS
	}
	if c.TLS != TLSStartTLS && c.TLS != TLSImplicit {
		return c, errors.New("mail: AG_SMTP_TLS phải là starttls hoặc tls (không hỗ trợ bản rõ)")
	}
	c.Port = 587
	if c.TLS == TLSImplicit {
		c.Port = 465
	}
	if p := os.Getenv("AG_SMTP_PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return c, errors.New("mail: AG_SMTP_PORT không hợp lệ")
		}
		c.Port = n
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return c, errors.New("mail: AG_SMTP_FROM không phải địa chỉ email hợp lệ")
	}
	return c, nil
}

// SMTP gửi email bằng net/smtp với TLS ≥ 1.2.
type SMTP struct {
	Cfg   Config
	Clock clock.Clock
}

var _ port.Mailer = (*SMTP)(nil)

// Send gửi một email.
func (s *SMTP) Send(ctx context.Context, m port.Message) error {
	if _, err := mail.ParseAddress(m.To); err != nil {
		return errors.New("mail: địa chỉ người nhận không hợp lệ")
	}
	body, err := Compose(s.Cfg.From, m, s.Clock.Now())
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(s.Cfg.Host, strconv.Itoa(s.Cfg.Port))
	tcfg := &tls.Config{ServerName: s.Cfg.Host, MinVersion: tls.VersionTLS12}
	d := &net.Dialer{Timeout: 20 * time.Second}
	var conn net.Conn
	if s.Cfg.TLS == TLSImplicit {
		conn, err = (&tls.Dialer{NetDialer: d, Config: tcfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mail: kết nối SMTP: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	}
	c, err := smtp.NewClient(conn, s.Cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("mail: mở phiên SMTP: %w", err)
	}
	defer func() { _ = c.Close() }()
	if s.Cfg.TLS == TLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("mail: máy chủ không hỗ trợ STARTTLS — từ chối gửi bản rõ")
		}
		if err := c.StartTLS(tcfg); err != nil {
			return fmt.Errorf("mail: STARTTLS: %w", err)
		}
	}
	if s.Cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Cfg.Username, s.Cfg.Password, s.Cfg.Host)); err != nil {
			return errors.New("mail: xác thực SMTP thất bại")
		}
	}
	from, _ := mail.ParseAddress(s.Cfg.From)
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	to, _ := mail.ParseAddress(m.To)
	if err := c.Rcpt(to.Address); err != nil {
		return errors.New("mail: máy chủ từ chối người nhận")
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("mail: ghi nội dung: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: kết thúc DATA: %w", err)
	}
	return c.Quit()
}

// Compose dựng thông điệp RFC 5322 UTF-8 (tiêu đề mã hóa Q, thân base64), không đính kèm.
func Compose(from string, m port.Message, now time.Time) ([]byte, error) {
	if strings.ContainsAny(m.Subject, "\r\n") || strings.ContainsAny(m.To, "\r\n") {
		return nil, errors.New("mail: tiêu đề hoặc người nhận chứa ký tự xuống dòng")
	}
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	domainPart := "agrigreen.local"
	if a, err := mail.ParseAddress(from); err == nil {
		if i := strings.LastIndex(a.Address, "@"); i >= 0 {
			domainPart = a.Address[i+1:]
		}
	}
	var b bytes.Buffer
	hdr := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	hdr("From", from)
	hdr("To", m.To)
	hdr("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	hdr("Date", now.In(clock.Zone()).Format(time.RFC1123Z))
	hdr("Message-ID", "<"+hex.EncodeToString(id)+"@"+domainPart+">")
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", "text/plain; charset=utf-8")
	hdr("Content-Transfer-Encoding", "base64")
	hdr("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	enc := base64.StdEncoding.EncodeToString([]byte(m.Text))
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return b.Bytes(), nil
}
