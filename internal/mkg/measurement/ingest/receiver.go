// Package ingest là dịch vụ nhận số đo cân của mkg/measurement (cmd/ingest; tài
// liệu kiến trúc mục 13.2, 14, 17; ADR-0017, ADR-0019):
//
//   - Receiver: POST /v1/ingest/batches qua mTLS (vùng biên OT → ingest, mục 17).
//     Chứng thư client → sổ thiết bị ĐÃ DUYỆT (port.DeviceRegistry), chữ ký nguồn
//     Ed25519 kiểm bằng khóa đã đăng ký, rồi MỘT giao dịch ghi ts_weigh.reading_batch
//   - ts_weigh.raw_reading (vai ingest_ts). Idempotent theo (device_id, epoch, counter_from).
//   - Loop: tích phân các phiên cân đã đóng (SessionSource) bằng Integrator
//     (weigh.integrate.v1, vai ts_reader) rồi port.TicketWriter.Record (vai ingest_mkg).
//     R-70: CHỈ ingest tích phân luồng thô thành phiếu cân. R-71: số đo thô không bao giờ xóa.
//
// Đồng hồ thiết bị và ntp_offset_ms chỉ được đo và gắn cờ, KHÔNG BAO GIỜ dùng làm thứ tự.
package ingest

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// BatchesPath là đường nhận lô.
const BatchesPath = "/v1/ingest/batches"

// DefaultSkewFlagMs là ngưỡng gắn cờ độ lệch đồng hồ mặc định (tạm, chờ khách/vận hành):
// |ntp_offset_ms| hoặc thiết bị đi trước hệ quá ngưỡng này → cờ.
const DefaultSkewFlagMs = 2000

// Problem ổn định của ingest (docs/errors.md, mục mkg/measurement — ingest).
var (
	ProblemClientCert        = problem.New(http.StatusUnauthorized, "ingest.client_cert_required", "Cần chứng thư mTLS client của thiết bị")
	ProblemRateLimited       = problem.New(http.StatusTooManyRequests, "ingest.rate_limited", "Thiết bị gửi lô quá nhanh")
	ProblemDeviceMismatch    = problem.New(http.StatusForbidden, "ingest.device_mismatch", "device_id của lô không khớp chứng thư client")
	ProblemUnknownDevice     = problem.New(http.StatusForbidden, "ingest.unknown_device", "Thiết bị chưa đăng ký hoặc chưa duyệt")
	ProblemDeviceRevoked     = problem.New(http.StatusForbidden, "ingest.device_revoked", "Thiết bị đã bị thu hồi")
	ProblemCertMismatch      = problem.New(http.StatusForbidden, "ingest.cert_mismatch", "Chứng thư client không khớp đăng ký của kỳ")
	ProblemEpochSuperseded   = problem.New(http.StatusConflict, "ingest.epoch_superseded", "Kỳ đăng ký đã được thay — đăng ký kỳ mới")
	ProblemBatchConflict     = problem.New(http.StatusConflict, "ingest.batch_conflict", "Lô trùng khóa nhưng khác nội dung đã nhận")
	ProblemCounterRegression = problem.New(http.StatusConflict, "ingest.counter_regression", "Bộ đếm chồng lấn hoặc lùi so với lô đã nhận")
)

// Ack là phản hồi nhận lô. high_water_counter = counter_to của lô đã commit:
// agent xóa khỏi đệm mọi số đo ≤ giá trị này của (device_id, epoch). Gửi lặp cùng
// lô trả đúng Ack cũ.
type Ack struct {
	DeviceID         string `json:"device_id"`
	Epoch            int    `json:"epoch"`
	HighWaterCounter int64  `json:"high_water_counter"`
}

// Receiver nhận lô số đo.
type Receiver struct {
	Devices    port.DeviceRegistry
	Store      BatchStore
	Clock      clock.Clock
	Log        *slog.Logger
	Metrics    *Metrics
	Limiter    *RateLimiter // nil = không giới hạn
	SkewFlagMs int64        // ≤ 0 → DefaultSkewFlagMs
}

// Register gắn route vào engine (bề mặt mTLS).
func (rv *Receiver) Register(r gin.IRoutes) {
	r.POST(BatchesPath, rv.postBatch)
}

// NewEngine dựng engine của listener ingest: trace, log, recovery, giới hạn thân
// edgewire.MaxBodyBytes (vượt → 413 payload_too_large).
func (rv *Receiver) NewEngine() *gin.Engine {
	r := httpx.NewEngine(rv.Log, edgewire.MaxBodyBytes)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "surface": "ingest"}) })
	rv.Register(r)
	return r
}

// CertSHA256 là SHA-256 (hex thường) của chứng thư DER — khóa đối chiếu với sổ thiết bị.
func CertSHA256(c *x509.Certificate) string {
	h := sha256.Sum256(c.Raw)
	return hex.EncodeToString(h[:])
}

// certNames trả CN và mọi DNS SAN của chứng thư (ADR-0019: CN/SAN = device_id).
func certHas(c *x509.Certificate, deviceID string) bool {
	if c.Subject.CommonName == deviceID {
		return true
	}
	for _, n := range c.DNSNames {
		if n == deviceID {
			return true
		}
	}
	return false
}

func edgeProblem(err error) *problem.Problem {
	code := edgewire.CodeOf(err)
	if code == "" {
		return problem.Internal.WithCause(err)
	}
	status := http.StatusUnprocessableEntity
	if code == "edge.decode" {
		status = http.StatusBadRequest
	}
	return problem.New(status, code, "Lô số đo sai hợp đồng agrigreen.edge.batch.v1").WithDetail(err.Error())
}

func registryProblem(err error) *problem.Problem {
	switch {
	case errors.Is(err, port.ErrUnknownDevice), errors.Is(err, port.ErrNotFound):
		return ProblemUnknownDevice
	case errors.Is(err, port.ErrDeviceRevoked):
		return ProblemDeviceRevoked
	case errors.Is(err, port.ErrEpochSuperseded):
		return ProblemEpochSuperseded
	case errors.Is(err, port.ErrCertMismatch):
		return ProblemCertMismatch
	}
	return problem.Internal.WithCause(err)
}

func (rv *Receiver) reject(c *gin.Context, p *problem.Problem, deviceID string) {
	ctx := c.Request.Context()
	rv.Metrics.batch(ctx, "rejected", p.Code)
	level := slog.LevelWarn
	if p.Status >= 500 {
		level = slog.LevelError
	}
	rv.Log.Log(ctx, level, "từ chối lô số đo", "event", "ingest.batch", "outcome", "rejected", "code", p.Code, "device_id", deviceID)
	httpx.Fail(c, p)
}

func (rv *Receiver) postBatch(c *gin.Context) {
	ctx := c.Request.Context()
	if c.Request.TLS == nil || len(c.Request.TLS.PeerCertificates) == 0 {
		rv.reject(c, ProblemClientCert, "")
		return
	}
	cert := c.Request.TLS.PeerCertificates[0]
	certSHA := CertSHA256(cert)
	if ok, wait := rv.Limiter.Allow(certSHA); !ok {
		secs := int64(wait/time.Second) + 1
		c.Header("Retry-After", strconv.FormatInt(secs, 10))
		rv.reject(c, ProblemRateLimited, cert.Subject.CommonName)
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			rv.reject(c, problem.PayloadTooLarge, cert.Subject.CommonName)
			return
		}
		rv.reject(c, problem.BadRequest, cert.Subject.CommonName)
		return
	}
	b, err := edgewire.Decode(body)
	if err != nil {
		rv.reject(c, edgeProblem(err), cert.Subject.CommonName)
		return
	}
	if err := b.Validate(); err != nil {
		rv.reject(c, edgeProblem(err), cert.Subject.CommonName)
		return
	}
	// R-16: danh tính từ kênh (chứng thư), không từ lô.
	if !certHas(cert, b.DeviceID) {
		rv.reject(c, ProblemDeviceMismatch, cert.Subject.CommonName)
		return
	}
	dev, err := rv.Devices.Resolve(ctx, b.DeviceID, b.Epoch, certSHA)
	if err != nil {
		rv.reject(c, registryProblem(err), b.DeviceID)
		return
	}
	// Khóa lấy từ sổ ĐÃ DUYỆT, không bao giờ từ lô (ADR-0019 §2).
	if b.KeyID != dev.KeyID {
		rv.reject(c, edgeProblem(&edgewire.Error{Code: "edge.key_id", Msg: "key_id của lô không khớp khóa đã đăng ký của kỳ"}), b.DeviceID)
		return
	}
	if err := edgewire.Verify(dev.PublicKey, b); err != nil {
		rv.reject(c, edgeProblem(err), b.DeviceID)
		return
	}
	sha, err := b.PayloadSHA256()
	if err != nil {
		rv.reject(c, edgeProblem(err), b.DeviceID)
		return
	}
	sig, err := b.SignatureBytes()
	if err != nil {
		rv.reject(c, edgeProblem(err), b.DeviceID)
		return
	}
	st, err := rv.Store.Store(ctx, dev.TenantID, b, sha, sig)
	switch {
	case errors.Is(err, ErrBatchConflict):
		rv.Log.Error("lô trùng khóa khác nội dung — thiết bị hỏng hoặc bị giả", "event", "ingest.batch_conflict", "outcome", "alarm",
			"device_id", b.DeviceID, "epoch", b.Epoch, "counter_from", b.CounterFrom, "tenant_id", dev.TenantID.String())
		rv.reject(c, ProblemBatchConflict, b.DeviceID)
		return
	case errors.Is(err, ErrCounterRegression):
		rv.reject(c, ProblemCounterRegression, b.DeviceID)
		return
	case err != nil:
		rv.reject(c, problem.Internal.WithCause(err), b.DeviceID)
		return
	}
	ack := Ack{DeviceID: b.DeviceID, Epoch: b.Epoch, HighWaterCounter: st.CounterTo}
	if st.Duplicate {
		rv.Metrics.batch(ctx, "duplicate", "")
		rv.Log.Info("lô gửi lặp — trả xác nhận cũ", "event", "ingest.batch", "outcome", "duplicate",
			"device_id", b.DeviceID, "epoch", b.Epoch, "tenant_id", dev.TenantID.String())
		c.JSON(http.StatusOK, ack)
		return
	}
	rv.Metrics.batch(ctx, "accepted", "")
	rv.Metrics.readings.Add(ctx, int64(len(b.Readings)))
	if st.GapBefore > 0 {
		rv.Metrics.gaps.Add(ctx, st.GapBefore)
		rv.Log.Warn("khoảng trống bộ đếm trước lô — mất lô", "event", "ingest.counter_gap", "outcome", "flagged",
			"device_id", b.DeviceID, "epoch", b.Epoch, "missing", st.GapBefore, "tenant_id", dev.TenantID.String())
	}
	rv.observeSkew(ctx, b, st, dev)
	rv.Log.Info("nhận lô số đo", "event", "ingest.batch", "outcome", "accepted", "device_id", b.DeviceID, "epoch", b.Epoch,
		"counter_from", b.CounterFrom, "counter_to", b.CounterTo, "tenant_id", dev.TenantID.String())
	c.JSON(http.StatusOK, ack)
}

// observeSkew đo độ lệch đồng hồ và gắn cờ (không bao giờ dùng làm thứ tự).
func (rv *Receiver) observeSkew(ctx context.Context, b edgewire.Batch, st Stored, dev port.Device) {
	limit := rv.SkewFlagMs
	if limit <= 0 {
		limit = DefaultSkewFlagMs
	}
	var reasons []string
	if b.NTPOffsetMs == nil {
		reasons = append(reasons, "ntp_unsynced")
	} else {
		off := int64(*b.NTPOffsetMs)
		rv.Metrics.skewMs(ctx, "ntp_offset", off)
		if off > limit || off < -limit {
			reasons = append(reasons, "ntp_offset")
		}
	}
	if last, err := time.Parse(edgewire.DeviceTimeLayout, b.Readings[len(b.Readings)-1].DeviceTime); err == nil {
		d := st.ReceivedAt.Sub(last).Milliseconds()
		rv.Metrics.skewMs(ctx, "device_vs_received", d)
		// Dương lớn là bình thường khi agent xả đệm; thiết bị đi TRƯỚC giờ hệ thì không giải thích được bằng đệm.
		if d < -limit {
			reasons = append(reasons, "device_ahead")
		}
	}
	for _, r := range reasons {
		rv.Metrics.flagSkew(ctx, r)
		rv.Log.Warn("độ lệch đồng hồ thiết bị vượt ngưỡng", "event", "ingest.clock_skew", "outcome", "flagged", "reason", r,
			"device_id", b.DeviceID, "epoch", b.Epoch, "tenant_id", dev.TenantID.String())
	}
}
