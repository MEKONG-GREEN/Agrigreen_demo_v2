package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// MKGConfig là cấu hình một lần chạy kế hoạch MKG (-plan mkg) trên API nội bộ MKG
// (cmd/api-internal) và cmd/ingest đang chạy — không đi tắt vào DB.
type MKGConfig struct {
	AppURL, AdminURL string // api-internal: bề mặt ứng dụng (:8081) và quản trị MFA (:8083)
	IngestURL        string // cmd/ingest listener mTLS (:8443)
	// Operator: token tenant MKG có field_supervisor, field_collector, scale_supervisor,
	// reconciliation_officer (MFA — bề mặt quản trị). Approver: token của NGƯỜI KHÁC có
	// field_supervisor + device_approver (R-56: người duyệt ≠ người đề xuất).
	Operator, Approver string `json:"-"`
	// ClientCert: chứng thư mTLS client (DER) có CN/SAN = cả hai mã thiết bị; nó được đăng ký
	// làm cert_sha256 của thiết bị. IngestHTTP phải trình chứng thư này.
	ClientCert []byte
	IngestHTTP *http.Client
	HTTP       *http.Client
	DeviceIn   string // mã thiết bị HUB_IN (mặc định theo kế hoạch)
	DeviceOut  string
	Seed       uint64
	Scale      string
	OverPct    int
	Env        string
	Today      time.Time
	Wait       time.Duration // chờ ingest tích phân phiếu sau khi đóng phiên (mặc định 2 phút)
	Log        *slog.Logger
	// AfterClose (kiểm thử): gọi sau khi đóng mỗi phiên cân thay cho việc chờ vòng tích phân
	// của cmd/ingest đang chạy (vd chạy IngestLoop.RunOnce trong tiến trình).
	AfterClose func(ctx context.Context) error
}

// MKGResult là thống kê một lần chạy.
type MKGResult struct {
	Plan            MKGPlan
	OrderIDs        []string // lệnh gom theo thứ tự lô (id do fieldops cấp)
	Orders, Counts  int
	Batches         int
	Sessions        int
	Imports         int
	Reconciliations int
}

type mkgSeeder struct {
	cfg      MKGConfig
	op, appr *api
	plan     MKGPlan
	res      *MKGResult
}

func (s *mkgSeeder) key(kind string, parts ...any) string {
	k := fmt.Sprintf("syn-%d-%s-mkg-%s", s.cfg.Seed, s.cfg.Scale, kind)
	for _, p := range parts {
		k += fmt.Sprintf("-%v", p)
	}
	if len(k) > 200 {
		k = k[:200]
	}
	return k
}

// RunMKG sinh dữ liệu MKG theo kế hoạch tất định. AG_ENV=production → từ chối (R-95).
func RunMKG(ctx context.Context, cfg MKGConfig) (*MKGResult, error) {
	if e := strings.ToLower(strings.TrimSpace(cfg.Env)); e == "production" || e == "prod" {
		return nil, ErrProduction
	}
	sc, ok := mkgScales[cfg.Scale]
	if !ok {
		return nil, fmt.Errorf("-scale phải là small hoặc medium, nhận %q", cfg.Scale)
	}
	if cfg.OverPct < 0 || cfg.OverPct > 100 {
		return nil, fmt.Errorf("-mkg-over-pct phải trong 0..100")
	}
	if cfg.Operator == "" || cfg.Approver == "" {
		return nil, errors.New("thiếu -mkg-token hoặc -mkg-approver-token (hai người khác nhau, R-56)")
	}
	if len(cfg.ClientCert) == 0 || cfg.IngestHTTP == nil {
		return nil, errors.New("thiếu chứng thư mTLS client (-mkg-client-cert/-mkg-client-key)")
	}
	if cfg.Log == nil {
		cfg.Log = telemetry.NewLogger("seed-synthetic", io.Discard)
	}
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	if cfg.Wait <= 0 {
		cfg.Wait = 2 * time.Minute
	}
	today := cfg.Today
	if today.IsZero() {
		today = time.Now().In(clock.Zone())
	}
	plan := BuildMKGPlan(cfg.Seed, sc, cfg.OverPct, today)
	if cfg.DeviceIn != "" {
		plan.Devices[0].ID = cfg.DeviceIn
	}
	if cfg.DeviceOut != "" {
		plan.Devices[1].ID = cfg.DeviceOut
	}
	s := &mkgSeeder{cfg: cfg, plan: plan, res: &MKGResult{Plan: plan},
		op:   &api{app: cfg.AppURL, admin: cfg.AdminURL, token: cfg.Operator, hc: hc},
		appr: &api{app: cfg.AppURL, admin: cfg.AdminURL, token: cfg.Approver, hc: hc}}
	if err := s.devices(ctx); err != nil {
		return s.res, err
	}
	for i := range plan.Lots {
		if err := s.lot(ctx, i, &plan.Lots[i]); err != nil {
			return s.res, fmt.Errorf("lô %d: %w", i, err)
		}
	}
	cfg.Log.Info("đã sinh dữ liệu tổng hợp MKG", "event", "seed.mkg.done", "seed", cfg.Seed, "scale", sc.Name, "lots", len(plan.Lots),
		"batches", s.res.Batches, "reconciliations", s.res.Reconciliations, "outcome", "ok")
	return s.res, nil
}

// devices đăng ký (operator) và duyệt (approver) hai thiết bị cân tổng hợp.
func (s *mkgSeeder) devices(ctx context.Context) error {
	sum := sha256.Sum256(s.cfg.ClientCert)
	for _, d := range s.plan.Devices {
		body := map[string]any{"device_id": d.ID, "facility_ref": d.FacilityRef.String(), "point": d.Point,
			"public_key": base64.StdEncoding.EncodeToString([]byte(d.Key().Public().(ed25519.PublicKey))), "cert_sha256": hex.EncodeToString(sum[:]),
			"scale_model": "synthetic-weighbridge (C-6 tạm)"}
		r, err := s.op.do(ctx, adminAPI, http.MethodPost, "/v1/measurement/devices", s.key("device", d.ID), body)
		if err != nil {
			return err
		}
		switch {
		case r.status == http.StatusCreated || r.status == http.StatusOK:
		case r.status == http.StatusConflict && (r.code() == "measurement.pending_exists" || r.code() == "measurement.device_taken"):
			continue // lần chạy trước đã đăng ký
		default:
			return &errStatus{what: "đăng ký thiết bị " + d.ID, r: r}
		}
		ep, _ := r.body["epoch"].(float64)
		if ep < 1 {
			ep = 1
		}
		if _, err := s.appr.expect(ctx, adminAPI, http.MethodPost, fmt.Sprintf("/v1/measurement/devices/%s/epochs/%d/decision", d.ID, int(ep)),
			s.key("device-decision", d.ID), map[string]any{"decision": "approved"}, http.StatusCreated, http.StatusOK, http.StatusConflict); err != nil {
			return err
		}
	}
	return nil
}

func (s *mkgSeeder) lot(ctx context.Context, i int, l *MKGLot) error {
	// 1. Lệnh gom: lập (operator) + duyệt (approver).
	plots := make([]string, 0, len(l.Plots))
	for _, p := range l.Plots {
		plots = append(plots, p.Ref.String())
	}
	day := l.At.Format("2006-01-02")
	r, err := s.op.expect(ctx, appAPI, http.MethodPost, "/v1/fieldops/orders", s.key("order", i), map[string]any{
		"htx_ref": l.HTXRef.String(), "window_from": day, "window_to": l.At.AddDate(0, 0, 5).Format("2006-01-02"),
		"plot_refs": plots}, http.StatusCreated, http.StatusOK)
	if err != nil {
		return err
	}
	order := str(obj(r.body, "order"), "order_id")
	if order == "" {
		return &errStatus{what: "lập lệnh gom (thiếu order_id)", r: r}
	}
	s.res.OrderIDs = append(s.res.OrderIDs, order)
	if _, err := s.appr.expect(ctx, appAPI, http.MethodPost, "/v1/fieldops/orders/"+order+"/decision", s.key("order-decision", i),
		map[string]any{"decision": "approved"}, http.StatusCreated, http.StatusOK); err != nil {
		return err
	}
	s.res.Orders++
	// 2. Đếm cuộn theo công đoạn (FM-04).
	for k, p := range l.Plots {
		for j, st := range []struct {
			stage  string
			rolls  int
			reason string
		}{{"bale", p.Bale, ""}, {"gather", p.Gather, p.GatherReason}, {"boat_load", p.BoatLoad, p.LoadReason}} {
			at := l.At.Add(time.Duration(j-3) * time.Hour)
			body := map[string]any{"plot_ref": p.Ref.String(), "stage": st.stage, "rolls": st.rolls, "event_time": at.Format(time.RFC3339)}
			if st.reason != "" {
				body["discrepancy_reason"] = st.reason
			}
			if _, err := s.op.expect(ctx, appAPI, http.MethodPost, "/v1/fieldops/orders/"+order+"/roll-counts", s.key("count", i, k, st.stage),
				body, http.StatusCreated, http.StatusOK); err != nil {
				return err
			}
			s.res.Counts++
		}
	}
	lot := l.LotRef.String()
	// 3. Cách tính giá (tạm tới G4 orders).
	if _, err := s.op.expect(ctx, appAPI, http.MethodPost, "/v1/measurement/lots/"+lot+"/terms", s.key("terms", i),
		map[string]any{"pricing_basis": l.Basis}, http.StatusCreated, http.StatusOK); err != nil {
		return err
	}
	// 4. Cân HUB_IN, HUB_OUT (edge) — lô HUB; lô DIRECT gắn lệnh gom tay.
	if l.Route == "HUB" {
		for pi, w := range []*MKGWeighing{l.HubIn, l.HubOut} {
			if err := s.weigh(ctx, i, pi, l, w, order); err != nil {
				return err
			}
		}
	} else if _, err := s.op.expect(ctx, appAPI, http.MethodPost, "/v1/measurement/lots/"+lot+"/collection", s.key("collection", i),
		map[string]any{"collection_order_ref": order}, http.StatusCreated, http.StatusOK); err != nil {
		return err
	}
	// 5. Số cân của khách (C-5 tạm: kênh api) → đối soát tự động.
	ir, err := s.op.expect(ctx, appAPI, http.MethodPost, "/v1/measurement/customer-weight-imports", s.key("import", i), map[string]any{
		"channel": "api", "customer_ref": s.plan.CustomerRef.String(), "rows": []map[string]any{{
			"lot_ref": lot, "route": l.Route, "mass_kg": kgStr(l.CustomerInKg), "moisture_pct": l.CustomerMoist,
			"weighed_at": l.At.Add(6 * time.Hour).Format(time.RFC3339), "customer_ticket_no": fmt.Sprintf("SYN-PX-%d-%03d", s.cfg.Seed, i)}},
	}, http.StatusCreated, http.StatusOK)
	if err != nil {
		return err
	}
	s.res.Imports++
	if rs, _ := ir.body["reconciliations"].([]any); len(rs) > 0 {
		s.res.Reconciliations += len(rs)
	}
	return nil
}

// weigh đẩy các lô số đo ký Ed25519 tới ingest, mở + đóng phiên cân, chờ phiếu.
func (s *mkgSeeder) weigh(ctx context.Context, i, pi int, l *MKGLot, w *MKGWeighing, order string) error {
	dev := s.plan.Devices[w.Device]
	for _, b := range batchesOf(w) {
		if err := s.push(ctx, dev, w, b[0], b[1]); err != nil {
			return err
		}
		s.res.Batches++
	}
	body := map[string]any{"lot_ref": l.LotRef.String(), "device_id": dev.ID, "epoch": 1, "counter_from": w.CounterFrom}
	if pi == 0 {
		body["collection_order_ref"] = order // G3-2: liên kết lô cân ↔ lệnh gom
	}
	op, err := s.op.expect(ctx, appAPI, http.MethodPost, "/v1/measurement/sessions", s.key("session", i, dev.Point), body,
		http.StatusCreated, http.StatusOK)
	if err != nil {
		return err
	}
	sid := str(op.body, "id")
	if _, err := s.op.expect(ctx, appAPI, http.MethodPost, "/v1/measurement/sessions/"+sid+"/close", s.key("session-close", i, dev.Point),
		map[string]any{"counter_to": w.CounterTo, "tare_kg": kgStr(w.TareKg), "moisture_pct": w.MoisturePct},
		http.StatusCreated, http.StatusOK, http.StatusConflict); err != nil {
		return err
	}
	s.res.Sessions++
	if s.cfg.AfterClose != nil {
		if err := s.cfg.AfterClose(ctx); err != nil {
			return err
		}
	}
	return s.waitTicket(ctx, l.LotRef.String(), dev.Point)
}

// push gửi một lô edge [from, to) của phiên qua mTLS.
func (s *mkgSeeder) push(ctx context.Context, dev MKGDevice, w *MKGWeighing, from, to int) error {
	off := 0
	b := edgewire.Batch{Format: edgewire.Format, DeviceID: dev.ID, Epoch: 1, CounterFrom: w.CounterFrom + int64(from),
		CounterTo: w.CounterFrom + int64(to) - 1, NTPOffsetMs: &off}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) // đồng hồ thiết bị: ghi nhận, không dùng làm thứ tự (ADR-0019)
	for k := from; k < to; k++ {
		c := w.CounterFrom + int64(k)
		b.Readings = append(b.Readings, edgewire.Reading{Counter: c, DeviceTime: edgewire.FormatDeviceTime(t0.Add(time.Duration(c) * time.Second)),
			ValueKg: w.Readings[k].ValueKg, Stable: w.Readings[k].Stable, Unit: edgewire.UnitKg})
	}
	if err := edgewire.Sign(dev.Key(), &b); err != nil {
		return err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.cfg.IngestURL, "/")+"/v1/ingest/batches", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.cfg.IngestHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("ingest lô %s [%d..%d]: %d %s", dev.ID, b.CounterFrom, b.CounterTo, res.StatusCode, body)
	}
	return nil
}

// waitTicket chờ phiếu của điểm cân xuất hiện (vòng tích phân của cmd/ingest).
func (s *mkgSeeder) waitTicket(ctx context.Context, lot, point string) error {
	deadline := time.Now().Add(s.cfg.Wait)
	for {
		r, err := s.op.do(ctx, appAPI, http.MethodGet, "/v1/measurement/lots/"+lot+"/tickets", "", nil)
		if err != nil {
			return err
		}
		if ts, _ := r.body["tickets"].([]any); r.status == http.StatusOK {
			for _, t := range ts {
				if m, _ := t.(map[string]any); str(m, "point") == point {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("lô %s: chưa có phiếu %s sau %s (cmd/ingest có đang chạy vòng tích phân?)", lot, point, s.cfg.Wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// mkgIngestClient dựng client mTLS từ tệp PEM (chứng thư + khóa client, CA của ingest).
func mkgIngestClient(certFile, keyFile, caFile string) (*http.Client, []byte, error) {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("chứng thư client: %w", err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pemBytes, err := os.ReadFile(caFile) //nolint:gosec // đường dẫn do người vận hành truyền vào
		if err != nil {
			return nil, nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, nil, errors.New("-mkg-ingest-ca: không đọc được PEM")
		}
		cfg.RootCAs = pool
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg}}, pair.Certificate[0], nil
}
