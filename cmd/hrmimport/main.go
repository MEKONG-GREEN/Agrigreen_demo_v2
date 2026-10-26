// Lệnh hrmimport chuyển đổi một lần dữ liệu nhân sự hiện có của Phòng HCTH từ Excel/CSV vào hồ sơ nhân sự pf/hrm
// (W11.30, D3 H10; BRD HCTH v3.0 mục phạm vi: "Mekong Green chuyển đổi dữ liệu một lần trước khi vận hành, Phòng HCTH
// xác nhận", giả định A2). Đọc tệp qua shared/tabular, chuẩn hóa theo tệp ánh xạ cột, báo cáo lỗi theo dòng.
//
//	go run ./cmd/hrmimport -template mau-nhan-su.xlsx                     # tệp mẫu gửi HCTH
//	go run ./cmd/hrmimport -dump-mapping anh-xa.yaml                      # ánh xạ mặc định để sửa theo tệp thật
//	go run ./cmd/hrmimport -in nhan-su.xlsx -mapping anh-xa.yaml -report bao-cao.xlsx           # chạy thử (không ghi)
//	go run ./cmd/hrmimport -in nhan-su.xlsx -mapping anh-xa.yaml -report bao-cao.xlsx -commit   # ghi
//
// Máy chủ: -api (Application API, vd https://api.example/v1) hoặc AG_HRM_IMPORT_API; token Bearer của tài khoản Cấp
// Tỉnh - HCTH/Admin qua biến môi trường AG_HRM_IMPORT_TOKEN (không nhận qua cờ để không lọt vào lịch sử lệnh); ngữ cảnh
// phiên (X-AG-Context) qua AG_HRM_IMPORT_CONTEXT. Mọi lần ghi đi qua POST /v1/hrm/staff (lớp app của pf/hrm: quyền, danh
// mục, sổ chỉ ghi thêm, dữ liệu cá nhân vào kho định danh). Chạy lại an toàn: người đã có (trùng SĐT ở kho định danh)
// báo "Đã có", không ghi lần hai.
//
// Mã thoát: 0 xong không lỗi dòng; 2 có dòng lỗi (xem báo cáo); 1 lỗi cả tệp/máy chủ.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// Config là tham số một lần chạy.
type Config struct {
	In          string // tệp nguồn .xlsx/.csv
	Mapping     string // tệp ánh xạ (rỗng = mặc định)
	Report      string // tệp báo cáo .csv/.xlsx ("" hoặc "-" = stdout CSV)
	AsOf        string // ngày chốt (ghi đè defaults.as_of)
	Commit      bool   // ghi thật
	SkipErrors  bool   // khi ghi: bỏ qua dòng lỗi, ghi các dòng hợp lệ
	API         string // gốc Application API (…/v1)
	Token       string // Bearer
	Context     string // X-AG-Context
	HTTP        *http.Client
	Out, ErrOut io.Writer
}

// errRowErrors: còn dòng lỗi (mã thoát 2).
var errRowErrors = errors.New("có dòng lỗi — xem báo cáo")

// maxRows là số dòng tối đa một tệp (Trung tâm có khoảng 500 người).
const maxRows = 5000

func main() {
	cfg := Config{Out: os.Stdout, ErrOut: os.Stderr, API: os.Getenv("AG_HRM_IMPORT_API"), Token: os.Getenv("AG_HRM_IMPORT_TOKEN"),
		Context: os.Getenv("AG_HRM_IMPORT_CONTEXT")}
	flag.StringVar(&cfg.In, "in", "", "tệp nguồn .xlsx hoặc .csv (sheet đầu tiên, dòng đầu là tiêu đề)")
	flag.StringVar(&cfg.Mapping, "mapping", "", "tệp ánh xạ cột YAML/JSON (rỗng = ánh xạ mặc định)")
	flag.StringVar(&cfg.Report, "report", "", "tệp báo cáo theo dòng .csv hoặc .xlsx (rỗng = in CSV ra stdout)")
	flag.StringVar(&cfg.AsOf, "as-of", "", "ngày chốt dữ liệu YYYY-MM-DD (ghi đè defaults.as_of)")
	flag.BoolVar(&cfg.Commit, "commit", false, "ghi vào hệ thống (mặc định chỉ chạy thử, không ghi)")
	flag.BoolVar(&cfg.SkipErrors, "skip-errors", false, "khi -commit: ghi các dòng hợp lệ, bỏ qua dòng lỗi")
	flag.StringVar(&cfg.API, "api", cfg.API, "gốc Application API, vd https://api.example/v1 (hoặc AG_HRM_IMPORT_API)")
	tpl := flag.String("template", "", "chỉ ghi tệp mẫu .xlsx gửi Phòng HCTH")
	dump := flag.String("dump-mapping", "", "chỉ ghi tệp ánh xạ mặc định ra đường dẫn này")
	flag.Parse()
	var err error
	switch {
	case *tpl != "":
		err = writeFile(*tpl, func(w io.Writer) error { return WriteTemplate(w) })
	case *dump != "":
		err = os.WriteFile(filepath.Clean(*dump), defaultMappingYAML, 0o600)
	default:
		_, err = Run(context.Background(), cfg)
	}
	switch {
	case errors.Is(err, errRowErrors):
		fmt.Fprintln(os.Stderr, "hrmimport:", err)
		os.Exit(2)
	case err != nil:
		fmt.Fprintln(os.Stderr, "hrmimport:", err)
		os.Exit(1)
	}
}

func writeFile(path string, fn func(io.Writer) error) error {
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Run chạy thử hoặc ghi; luôn ghi báo cáo khi đọc được tệp. Trả tổng kết; errRowErrors khi còn dòng lỗi.
func Run(ctx context.Context, cfg Config) (Summary, error) {
	if cfg.In == "" {
		return Summary{}, errors.New("thiếu -in")
	}
	if cfg.API == "" {
		return Summary{}, errors.New("thiếu -api (hoặc AG_HRM_IMPORT_API): chạy thử cũng đọc danh mục, đơn vị, bảng hệ số từ máy chủ")
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 60 * time.Second}
	}
	if cfg.Out == nil {
		cfg.Out = io.Discard
	}
	m, err := LoadMapping(cfg.Mapping)
	if err != nil {
		return Summary{}, err
	}
	data, err := os.ReadFile(filepath.Clean(cfg.In))
	if err != nil {
		return Summary{}, err
	}
	lim := tabular.Limits{MaxBytes: 20 << 20, MaxRows: maxRows, MaxCols: 128}
	t, err := tabular.ReadBytes(data, lim)
	if err != nil {
		return Summary{}, errors.New(tabular.Message(err, lim))
	}
	a := &api{base: cfg.API, token: cfg.Token, kctx: cfg.Context, hc: cfg.HTTP}
	refs, err := a.loadRefs(ctx)
	if err != nil {
		return Summary{}, err
	}
	rows, col, err := Prepare(t, m, refs, cfg.AsOf)
	if err != nil {
		return Summary{}, err
	}
	pre := summarize(rows)
	var runErr error
	if cfg.Commit {
		if pre.Errors > 0 && !cfg.SkipErrors {
			runErr = fmt.Errorf("%d dòng lỗi — không ghi dòng nào; sửa tệp rồi chạy lại, hoặc thêm -skip-errors để ghi các dòng hợp lệ", pre.Errors)
		} else {
			sum := sha256.Sum256(data)
			commit(ctx, a, rows, "hrmimport:"+hex.EncodeToString(sum[:8]))
		}
	}
	s := summarize(rows)
	if err := writeReport(cfg.Report, cfg.Out, reportRows(rows, t.Header, col)); err != nil {
		return s, err
	}
	mode := "Chạy thử (không ghi)"
	if cfg.Commit {
		mode = "Ghi"
	}
	if cfg.ErrOut != nil {
		_, _ = fmt.Fprintf(cfg.ErrOut, "%s — %s\n", mode, s)
	}
	if runErr != nil {
		return s, runErr
	}
	if s.Errors > 0 {
		return s, errRowErrors
	}
	return s, nil
}

// commit ghi tuần tự theo thứ tự dòng (mã hồ sơ cấp theo thứ tự tệp). Khóa Idempotency-Key = tệp + dòng (không chứa dữ
// liệu cá nhân): chạy lại cùng tệp trong thời hạn khóa nhận lại phản hồi cũ; ngoài thời hạn hoặc tệp đã sửa, kho định
// danh chặn trùng SĐT → "Đã có".
func commit(ctx context.Context, a *api, rows []*Row, idemPrefix string) {
	for i, r := range rows {
		if r.Status != StatusValid {
			continue
		}
		res, replayed, err := a.createStaff(ctx, *r.Req, fmt.Sprintf("%s:%d", idemPrefix, r.Line))
		var ae *apiError
		switch {
		case err == nil:
			r.Status, r.Code = StatusCreated, res.Code
			if replayed {
				r.Status = StatusExists
				r.add(LevelWarn, "", "Đã tạo ở lần chạy trước (phản hồi gọi lặp)")
			}
			for _, w := range res.Warnings {
				r.add(LevelWarn, "", "Máy chủ cảnh báo: %s", w)
			}
		case errors.As(err, &ae) && ae.P.Code == "hrm.duplicate_person":
			r.Status = StatusExists
			r.add(LevelWarn, "phone", "Số điện thoại đã thuộc một hồ sơ (nạp ở lần chạy trước hoặc nhập tay) — không ghi lại")
		case errors.As(err, &ae):
			r.Status = StatusError
			r.add(LevelError, "", "Máy chủ từ chối (%s): %s", ae.P.Code, ae.P.String())
		default:
			// Lỗi mạng sau khi đã thử lại: dừng, các dòng sau "Chưa ghi" — chạy lại an toàn.
			r.Status = StatusError
			r.add(LevelError, "", "Lỗi kết nối máy chủ — dừng ghi; chạy lại lệnh sau khi kiểm tra kết nối")
			for _, rest := range rows[i+1:] {
				if rest.Status == StatusValid {
					rest.Status = StatusSkipped
				}
			}
			return
		}
	}
}
