package mail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"mime"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// fakeSMTP là máy chủ SMTP giả tối thiểu (RFC 5321 + STARTTLS RFC 3207 + AUTH PLAIN) để kiểm
// bộ gửi thư của lịch báo cáo (US-DASH-03) mà không cần máy chủ SMTP thật trên máy dev/CI.
type fakeSMTP struct {
	ln         net.Listener
	cert       tls.Certificate
	startTLS   bool // quảng bá STARTTLS
	rejectRcpt bool

	mu sync.Mutex
	seen
}

// seen là những gì máy chủ giả nhận được.
type seen struct {
	tlsBefore bool // mọi lệnh AUTH/MAIL đều sau khi đã TLS
	auth      string
	from, rcp string
	rcpts     []string // mọi RCPT (người nhận + CC)
	data      string
	delivered int
}

func newCA(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "smtp.gia.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// startFake mở máy chủ giả; implicit = TLS ngay từ đầu (cổng 465).
func startFake(t *testing.T, cert tls.Certificate, implicit, offerStartTLS bool, rejectRcpt ...bool) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if implicit {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	}
	f := &fakeSMTP{ln: ln, cert: cert, startTLS: offerStartTLS, rejectRcpt: len(rejectRcpt) > 0 && rejectRcpt[0], seen: seen{tlsBefore: true}}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c, implicit)
		}
	}()
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve(conn net.Conn, secure bool) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	tp := textproto.NewConn(conn)
	reply := func(s string) { _ = tp.PrintfLine("%s", s) }
	reply("220 smtp.gia.test ESMTP")
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			ext := []string{"250-smtp.gia.test"}
			if f.startTLS && !secure {
				ext = append(ext, "250-STARTTLS")
			}
			if secure {
				ext = append(ext, "250-AUTH PLAIN")
			}
			ext = append(ext, "250 8BITMIME")
			for _, e := range ext {
				reply(e)
			}
		case "STARTTLS":
			reply("220 sẵn sàng TLS")
			tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12})
			if err := tc.Handshake(); err != nil {
				return
			}
			conn, secure = tc, true
			tp = textproto.NewConn(tc)
			reply = func(s string) { _ = tp.PrintfLine("%s", s) }
		case "AUTH":
			f.mu.Lock()
			f.tlsBefore = f.tlsBefore && secure
			f.auth = arg
			f.mu.Unlock()
			reply("235 2.7.0 đã xác thực")
		case "MAIL":
			f.mu.Lock()
			f.tlsBefore = f.tlsBefore && secure
			f.from = arg
			f.mu.Unlock()
			reply("250 ok")
		case "RCPT":
			if f.rejectRcpt {
				reply("550 5.1.1 không có hộp thư")
				continue
			}
			f.mu.Lock()
			f.rcp = arg
			f.rcpts = append(f.rcpts, arg)
			f.mu.Unlock()
			reply("250 ok")
		case "DATA":
			reply("354 gửi nội dung")
			b, err := io.ReadAll(tp.DotReader())
			if err != nil {
				return
			}
			f.mu.Lock()
			f.data = string(b)
			f.delivered++
			f.mu.Unlock()
			reply("250 đã nhận")
		case "QUIT":
			reply("221 tạm biệt")
			return
		default:
			reply("502 không hỗ trợ")
		}
	}
}

func (f *fakeSMTP) snapshot() seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen
}

var msg = port.Message{To: "Cán bộ TTKN <canbo@ttkn.gia.test>", Subject: "Báo cáo tiến độ gieo sạ — Tháng 09/2026", Text: "Tải tệp trong 15 phút: https://agrigreen.gia.test/tai/abc"}

func sender(f *fakeSMTP, tlsMode string, pool *x509.CertPool) *SMTP {
	return &SMTP{
		Cfg:   Config{Host: "127.0.0.1", Port: f.port(), Username: "agrigreen", Password: "mat-khau-gia", From: "AgriGreen <no-reply@agrigreen.gia.test>", TLS: tlsMode},
		Clock: clock.NewFixed(time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC)), RootCAs: pool,
	}
}

// US-DASH-03: email lịch báo cáo đi qua SMTP thật (máy chủ giả trong tiến trình) — STARTTLS
// bắt buộc trước AUTH/MAIL, người nhận đúng, tiêu đề tiếng Việt mã hóa Q, thân base64 đúng nội dung.
func TestSMTPDeliversOverSTARTTLS(t *testing.T) {
	cert, pool := newCA(t)
	f := startFake(t, cert, false, true)
	if err := sender(f, TLSStartTLS, pool).Send(context.Background(), msg); err != nil {
		t.Fatalf("gửi: %v", err)
	}
	got := f.snapshot()
	if got.delivered != 1 || !got.tlsBefore {
		t.Fatalf("đã giao %d, TLS trước AUTH/MAIL = %v", got.delivered, got.tlsBefore)
	}
	if cred, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(got.auth, "PLAIN ")); string(cred) != "\x00agrigreen\x00mat-khau-gia" {
		t.Fatalf("AUTH PLAIN: %q", cred)
	}
	if !strings.HasPrefix(got.from, "FROM:<no-reply@agrigreen.gia.test>") || got.rcp != "TO:<canbo@ttkn.gia.test>" {
		t.Fatalf("phong bì: %q %q", got.from, got.rcp)
	}
	r := textproto.NewReader(bufio.NewReader(strings.NewReader(got.data)))
	hdr, err := r.ReadMIMEHeader()
	if err != nil {
		t.Fatal(err)
	}
	subj, err := new(mime.WordDecoder).DecodeHeader(hdr.Get("Subject"))
	if err != nil || subj != msg.Subject || hdr.Get("To") != msg.To || hdr.Get("Auto-Submitted") != "auto-generated" {
		t.Fatalf("tiêu đề: %q %v %v", subj, err, hdr)
	}
	rest, _ := io.ReadAll(r.R)
	body, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(string(rest)), "\r\n", ""))
	if err != nil || string(body) != msg.Text {
		t.Fatalf("thân thư: %q %v", body, err)
	}
}

func TestSMTPImplicitTLS(t *testing.T) {
	cert, pool := newCA(t)
	f := startFake(t, cert, true, false)
	if err := sender(f, TLSImplicit, pool).Send(context.Background(), msg); err != nil {
		t.Fatalf("gửi qua TLS ngầm định (465): %v", err)
	}
	if got := f.snapshot(); got.delivered != 1 || got.rcp != "TO:<canbo@ttkn.gia.test>" {
		t.Fatalf("giao: %+v", got)
	}
}

// Không bao giờ gửi bản rõ, không chấp nhận chứng chỉ không tin cậy; máy chủ từ chối người nhận → lỗi.
func TestSMTPRefusesInsecureOrRejected(t *testing.T) {
	cert, pool := newCA(t)
	noTLS := startFake(t, cert, false, false)
	if err := sender(noTLS, TLSStartTLS, pool).Send(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("máy chủ không có STARTTLS phải bị từ chối: %v", err)
	}
	if got := noTLS.snapshot(); got.from != "" || got.delivered != 0 {
		t.Fatalf("không được gửi MAIL khi chưa TLS: %+v", got)
	}
	f := startFake(t, cert, false, true)
	if err := sender(f, TLSStartTLS, x509.NewCertPool()).Send(context.Background(), msg); err == nil {
		t.Fatal("chứng chỉ không tin cậy phải bị từ chối")
	}
	if got := f.snapshot(); got.auth != "" || got.delivered != 0 {
		t.Fatalf("lộ mật khẩu/thư khi chứng chỉ sai: %+v", got)
	}
	rej := startFake(t, cert, false, true, true)
	if err := sender(rej, TLSStartTLS, pool).Send(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "từ chối người nhận") {
		t.Fatalf("người nhận bị từ chối: %v", err)
	}
	if got := rej.snapshot(); got.delivered != 0 {
		t.Fatalf("không được giao: %+v", got)
	}
}
