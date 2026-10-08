package main

// Lệnh con của kịch bản cổng tích hợp (W5.9):
//
//	go run ./tools/loadgen gateway-provision -admin https://admin.staging.<miền> \
//	   -admin-token-file proposer.jwt -approver-token-file approver.jwt -partners 200 -noisy 1 -out partners.tsv
//	go run ./tools/loadgen gateway -gateway https://partners.staging.<miền> -partners partners.tsv \
//	   -gw-rps 0.5 -gw-quota-rpm 60 -baseline 2m -duration 2m -rounds 2 -json w59.json -md w59.md
//
// Chạy cục bộ trên cổng trong tiến trình + PostgreSQL cục bộ: kiểm thử TestGatewayNoisyPartnerIsolation_W59
// (gateway_integration_test.go, biến AG_LOADGEN_GW_*), xem docs/runbooks/load-drill.md mục kiểm thử tải cổng.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// Mặc định của kịch bản cổng (§42): hạn mức gói 60 lượt/phút, đối tác thường dùng một nửa, đối tác ồn gấp 10.
const (
	defaultGWFeed     = apiclientport.FeedHarvestProgress
	defaultGWContract = "roll_count_statement"
	defaultGWMix      = "feed=6,checksums=1,batch=2,file=1"
)

func readSecretFile(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // tệp do người vận hành truyền
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("%s: rỗng", path)
	}
	return s, nil
}

func runProvision(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("loadgen gateway-provision", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var p GWProvision
	var adminTok, approverTok, out string
	var allowProd bool
	fs.StringVar(&p.Admin, "admin", "", "URL gốc Admin API (api-platform) của môi trường staging/dev")
	fs.StringVar(&adminTok, "admin-token-file", "", "tệp bearer MFA của người đề xuất (vai api_client_admin, tenant đơn vị vận hành)")
	fs.StringVar(&approverTok, "approver-token-file", "", "tệp bearer MFA của người duyệt (vai api_client_approver, người khác)")
	fs.IntVar(&p.Partners, "partners", 200, "số đối tác tổng hợp")
	fs.IntVar(&p.Noisy, "noisy", 1, "số đối tác ồn (cuối danh sách)")
	fs.StringVar(&p.Feed, "feed", defaultGWFeed, "feed đăng ký (scope feed:<tên>:read)")
	fs.StringVar(&p.Contract, "contract", defaultGWContract, "hợp đồng nạp đăng ký (scope ingest:<hợp đồng>:write)")
	fs.Int64Var(&p.QuotaRPM, "quota-rpm", 60, "requests_per_minute của gói mỗi đối tác")
	fs.Int64Var(&p.DailyUnits, "daily-units", 1_000_000, "daily_units của gói (đủ lớn để không chạm hạn mức ngày trong lượt đo)")
	fs.StringVar(&p.Label, "label", "", "tiền tố display_name (mặc định loadgen-w59-<thời điểm>) — để thu hồi sau lượt đo")
	fs.IntVar(&p.Parallel, "parallel", 4, "số lời gọi Admin API song song")
	fs.StringVar(&out, "out", "partners.tsv", "tệp TSV kết quả (0600, chứa bí mật khóa thử)")
	fs.BoolVar(&allowProd, "i-know-this-is-not-production", false, "bắt buộc khi URL không chứa 'staging', 'dev', 'localhost' hoặc '127.0.0.1'")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if !allowProd && !looksNonProd(p.Admin) {
		outln(stderr, "loadgen: "+p.Admin+" không giống môi trường staging/dev — chỉ cấp đối tác tổng hợp ngoài production (R-95)")
		return 2
	}
	var err error
	if p.AdminToken, err = readSecretFile(adminTok); err == nil {
		p.ApproverToken, err = readSecretFile(approverTok)
	}
	if err != nil {
		outln(stderr, "loadgen:", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ps, err := Provision(ctx, p)
	if err != nil {
		outln(stderr, "loadgen:", err)
		return 1
	}
	if err := WritePartners(out, ps); err != nil {
		outln(stderr, "loadgen:", err)
		return 2
	}
	outf(stdout, "loadgen: đã cấp %d đối tác tổng hợp (%d ồn) → %s\n", len(ps), p.Noisy, out)
	return 0
}

func runGateway(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("loadgen gateway", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfg GWConfig
	var r GWRun
	var partners, mix, jsonOut, mdOut string
	var allowProd bool
	fs.StringVar(&cfg.Base, "gateway", "", "URL gốc cổng tích hợp (vd https://partners.staging.<miền>)")
	fs.StringVar(&partners, "partners", "", "tệp TSV partner_id<TAB>api_key<TAB>class (từ gateway-provision)")
	fs.StringVar(&cfg.Feed, "feed", defaultGWFeed, "feed đọc (GET /v1/feeds/{feed}, /checksums)")
	fs.StringVar(&cfg.Contract, "contract", defaultGWContract, "hợp đồng nạp (POST /v1/batches|files/{contract})")
	fs.Float64Var(&cfg.PartnerRPS, "gw-rps", 0.5, "tốc độ mỗi đối tác thường (lượt/giây), phải dưới hạn mức")
	fs.Int64Var(&cfg.QuotaRPM, "gw-quota-rpm", 60, "requests_per_minute của gói (như lúc cấp)")
	fs.Float64Var(&cfg.NoisyFactor, "gw-noisy-factor", 10, "đối tác ồn gửi gấp bao nhiêu lần hạn mức")
	fs.StringVar(&mix, "gw-mix", defaultGWMix, "trọng số thao tác feed,checksums,batch,file")
	fs.IntVar(&cfg.BatchItems, "gw-batch-items", 20, "bản ghi mỗi lô (≤ max_batch 500)")
	fs.IntVar(&cfg.FileRecords, "gw-file-records", 50, "bản ghi mỗi tệp NDJSON")
	fs.IntVar(&cfg.MaxInFlight, "max-inflight", 16, "lượt đang bay tối đa mỗi đối tác")
	fs.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "hạn mỗi yêu cầu HTTP")
	fs.DurationVar(&r.Warmup, "warmup", 30*time.Second, "lượt khởi động (không đo, không có đối tác ồn)")
	fs.DurationVar(&r.Baseline, "baseline", 2*time.Minute, "lượt nền mỗi vòng (không có đối tác ồn); 0 = bỏ")
	fs.DurationVar(&r.Noisy, "duration", 2*time.Minute, "lượt có đối tác ồn mỗi vòng")
	fs.IntVar(&r.Rounds, "rounds", 2, "số vòng nền/ồn xen kẽ (gộp mẫu)")
	fs.Float64Var(&r.BaselineNoisyFactor, "baseline-noisy-factor", 0, "đối tác ồn trong lượt nền gửi bao nhiêu lần hạn mức (0 = vắng mặt, đúng §42; 1 = dùng đúng hạn mức — cho quy mô nhỏ)")
	fs.Float64Var(&r.MaxIncrease, "max-p95-increase", 0.10, "tăng p95 tối đa của đối tác thường (§42: 10%)")
	fs.DurationVar(&r.Slack, "p95-slack", 0, "dung sai tuyệt đối cộng thêm (0 ở lượt đo chính thức)")
	fs.Float64Var(&r.MaxErrRate, "max-error-rate", 0.01, "tỷ lệ lỗi tối đa (0..1)")
	fs.StringVar(&jsonOut, "json", "", "ghi báo cáo JSON")
	fs.StringVar(&mdOut, "md", "", "ghi báo cáo markdown")
	fs.BoolVar(&allowProd, "i-know-this-is-not-production", false, "bắt buộc khi URL không chứa 'staging', 'dev', 'localhost' hoặc '127.0.0.1'")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if cfg.Base == "" || partners == "" {
		outln(stderr, "loadgen gateway: cần -gateway và -partners")
		return 2
	}
	if !allowProd && !looksNonProd(cfg.Base) {
		outln(stderr, "loadgen: "+cfg.Base+" không giống môi trường staging/dev — kiểm thử tải chỉ chạy trên dữ liệu tổng hợp (R-95)")
		return 2
	}
	var err error
	if cfg.Mix, err = ParseMix(mix); err != nil {
		outln(stderr, "loadgen: -gw-mix:", err)
		return 2
	}
	ps, err := LoadPartners(partners)
	if err != nil {
		outln(stderr, "loadgen:", err)
		return 2
	}
	g, err := NewGateway(cfg, ps)
	if err != nil {
		outln(stderr, "loadgen:", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	outf(stdout, "loadgen gateway: %d đối tác, khởi động %s, %d vòng × (nền %s + ồn %s)\n", len(ps), r.Warmup, r.Rounds, r.Baseline, r.Noisy)
	rep := g.Compare(ctx, r)
	md := rep.Markdown()
	outln(stdout, md)
	if jsonOut != "" {
		b, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.WriteFile(jsonOut, b, 0o600); err != nil {
			outln(stderr, "loadgen:", err)
			return 2
		}
	}
	if mdOut != "" {
		if err := os.WriteFile(mdOut, []byte(md), 0o600); err != nil {
			outln(stderr, "loadgen:", err)
			return 2
		}
	}
	if len(rep.Violations) > 0 {
		return 1
	}
	return 0
}
