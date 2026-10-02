package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

func decodeB64(raw []byte) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.NewReplacer("\r", "", "\n", "").Replace(string(raw)))
}

// KN v7.7 FN-TI-02.06: báo cáo Chi cục gửi từ hệ thống qua SMTP (máy chủ giả có STARTTLS) tới hộp thư đơn vị ngoài hệ
// thống, có CC, ĐÍNH KÈM tệp Excel/PDF (multipart/mixed, tên tệp tiếng Việt RFC 2231), nội dung đọc lại đúng từng byte.
func TestFN_TI_02_06_SMTPAttachmentAndCC(t *testing.T) {
	cert, pool := newCA(t)
	f := startFake(t, cert, false, true)
	xlsx := []byte("PK\x03\x04 nội dung xlsx giả " + strings.Repeat("x", 300))
	pdf := []byte("%PDF-1.4 giả")
	m := port.Message{To: "Chi cục Trồng trọt và BVTV <trongtrot.chicuc@angiang.gia.test>", CC: []string{"vanthu@ttkn.angiang.gia.test"},
		Subject: "Báo cáo tuần 36 — Phòng Trồng trọt", Text: "Kính gửi Chi cục,\nĐính kèm báo cáo tổng hợp (Tạm tính).",
		Attachments: []port.Attachment{
			{Name: "T01_Tiến độ xuống giống_Tuần 36.xlsx", ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Data: xlsx},
			{Name: "T01_Tuần 36.pdf", ContentType: "application/pdf", Data: pdf},
		}}
	if err := sender(f, TLSStartTLS, pool).Send(context.Background(), m); err != nil {
		t.Fatalf("gửi: %v", err)
	}
	got := f.snapshot()
	if got.delivered != 1 || !got.tlsBefore {
		t.Fatalf("giao %d, TLS trước = %v", got.delivered, got.tlsBefore)
	}
	if !slices.Equal(got.rcpts, []string{"TO:<trongtrot.chicuc@angiang.gia.test>", "TO:<vanthu@ttkn.angiang.gia.test>"}) {
		t.Fatalf("RCPT người nhận + CC: %v", got.rcpts)
	}
	msg, err := mail.ReadMessage(strings.NewReader(got.data))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Cc") != "vanthu@ttkn.angiang.gia.test" {
		t.Fatalf("Cc: %q", msg.Header.Get("Cc"))
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("Content-Type: %v %v", mt, err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var names []string
	var datas [][]byte
	for i := 0; ; i++ {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(p)
		dec, err := decodeB64(raw)
		if err != nil {
			t.Fatalf("phần %d: %v", i, err)
		}
		if i == 0 {
			if string(dec) != m.Text {
				t.Fatalf("thân: %q", dec)
			}
			continue
		}
		names = append(names, p.FileName())
		datas = append(datas, dec)
	}
	if !slices.Equal(names, []string{m.Attachments[0].Name, m.Attachments[1].Name}) {
		t.Fatalf("tên tệp: %v", names)
	}
	if !bytes.Equal(datas[0], xlsx) || !bytes.Equal(datas[1], pdf) {
		t.Fatal("nội dung đính kèm sai")
	}
	// Chặn chèn tiêu đề qua CC / tên tệp.
	if _, err := Compose("a@b.vn", port.Message{To: "a@b.vn", CC: []string{"x@y.vn\r\nBcc: z@w"}, Subject: "x"}, f.cert.Leaf.NotBefore); err == nil {
		t.Fatal("chặn CC chứa xuống dòng")
	}
	if _, err := Compose("a@b.vn", port.Message{To: "a@b.vn", Subject: "x", Attachments: []port.Attachment{{Name: "a\r\nb", Data: []byte("1")}}},
		f.cert.Leaf.NotBefore); err == nil {
		t.Fatal("chặn tên tệp chứa xuống dòng")
	}
}
