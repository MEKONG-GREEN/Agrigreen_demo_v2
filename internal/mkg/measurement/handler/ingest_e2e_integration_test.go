package handler_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// R-70 đầu–cuối với cổng THẬT của measurement: đăng ký + duyệt thiết bị (R-56) →
// agent đẩy lô ký Ed25519 qua mTLS → ingest ghi ts_weigh → phiên cân đóng (bì,
// ẩm) → vòng tích phân → sổ phiếu cân (vai ingest_mkg) → khối lượng lô 96,8 t
// (proto seed: 138 400 − 41 600). Chạy lại → không phiếu mới (phiên hết chờ).
func TestIngestEndToEndIntoTicketLedger(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	devID := "scale-e2e-in"
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cert := selfCert(t, devID)
	sum := sha256.Sum256(cert.Raw)
	sup := f.supervisor()
	reg := sup.adm(http.MethodPost, "/v1/measurement/devices", map[string]any{"device_id": devID, "facility_ref": ids.New().String(),
		"point": "HUB_IN", "public_key": base64.StdEncoding.EncodeToString(pub), "cert_sha256": hex.EncodeToString(sum[:]), "scale_model": "e2e"})
	mustStatus(t, reg, http.StatusCreated, "đề xuất")
	mustStatus(t, f.approver().adm(http.MethodPost, "/v1/measurement/devices/"+devID+"/epochs/1/decision", map[string]any{"decision": "approved"}),
		http.StatusCreated, "duyệt")

	ports := measurement.NewIngestPorts(f.ingestPool, f.clk)
	if err := ports.Validate(); err != nil {
		t.Fatal(err)
	}
	store := &measurement.IngestTSStore{Writer: f.env.Pool(t, migrations.TS, port.IngestTSRole), Reader: f.env.Pool(t, migrations.TS, port.TSReaderRole)}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := measurement.NewIngestMetrics(nil)
	rv := &measurement.IngestReceiver{Devices: ports.Devices, Store: store, Clock: f.clk, Log: log, Metrics: metrics}
	eng := rv.NewEngine()
	send := func(from int64, vals ...any) {
		t.Helper()
		b := edgeBatch(t, priv, devID, from, vals...)
		raw, _ := json.Marshal(b)
		req := httptest.NewRequest(http.MethodPost, "/v1/ingest/batches", bytes.NewReader(raw))
		req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
		w := httptest.NewRecorder()
		eng.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("lô %d: %d %s", from, w.Code, w.Body.String())
		}
	}
	send(1, "12000.000", false, "98000.000", false, "138420.000", false, "138400.000", true)
	send(5, "138380.000", true, "138400.000", true, "138410.000", true, "138400.000", true, "150000.000", false)

	lot := ids.New()
	op := sup.app(http.MethodPost, "/v1/measurement/sessions", map[string]any{"lot_ref": lot.String(), "device_id": devID, "epoch": 1, "counter_from": 1})
	mustStatus(t, op, http.StatusCreated, "mở phiên")
	sid := str(op.body, "id")
	loop := &measurement.IngestLoop{Sessions: ports.Sessions, Integrator: &measurement.IngestIntegrator{Windows: store, Tare: ports.Tare},
		Tickets: ports.Tickets, Clock: f.clk, Log: log, Metrics: metrics}
	if st, err := loop.RunOnce(ctx); err != nil || st.Recorded != 0 {
		t.Fatalf("phiên mở không được tích phân: %+v %v", st, err)
	}
	mustStatus(t, sup.app(http.MethodPost, "/v1/measurement/sessions/"+sid+"/close",
		map[string]any{"counter_to": 9, "tare_kg": "41600", "moisture_pct": "18"}), http.StatusCreated, "đóng phiên")

	st, err := loop.RunOnce(ctx)
	if err != nil || st.Recorded != 1 || st.Retrying != 0 {
		t.Fatalf("lượt tích phân: %+v %v", st, err)
	}
	lt := sup.app(http.MethodGet, "/v1/measurement/lots/"+lot.String()+"/tickets", nil)
	ts := arr(lt.body, "tickets")
	if len(ts) != 1 {
		t.Fatalf("phiếu: %s", lt.raw)
	}
	tk := ts[0]
	w := obj(tk, "window")
	if str(tk, "mass_kg") != "96800.000" || str(tk, "source") != "edge" || str(tk, "point") != "HUB_IN" || str(tk, "moisture_pct") != "18.00" ||
		str(tk, "rule_version") != "weigh.integrate.v1" || w["counter_from"] != json.Number("1") || w["counter_to"] != json.Number("9") ||
		len(str(w, "readings_sha256")) != 64 || len(w["batch_ids"].([]any)) != 2 {
		t.Fatalf("phiếu edge: %s", lt.raw)
	}
	// Chạy lại: phiên đã có phiếu ở HUB_IN → không còn chờ.
	if st, err := loop.RunOnce(ctx); err != nil || st.Recorded != 0 {
		t.Fatalf("chạy lại: %+v %v", st, err)
	}
	// Tích phân lại (tranh chấp) cùng bì từ sổ phiên → cùng khối lượng và băm.
	in := &measurement.IngestIntegrator{Windows: store, Tare: ports.Tare}
	again, err := in.Integrate(ctx, f.tenant, port.Window{DeviceID: devID, Epoch: 1, CounterFrom: 1, CounterTo: 9})
	if err != nil || again.NetKg.String() != "96800.000" || again.ReadingsSHA256 != str(w, "readings_sha256") {
		t.Fatalf("tích phân lại: %+v %v", again, err)
	}
}

func selfCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn}, DNSNames: []string{cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// edgeBatch dựng và ký lô [from, …]; vals là (value_kg, stable) xen kẽ.
func edgeBatch(t *testing.T, key ed25519.PrivateKey, device string, from int64, vals ...any) edgewire.Batch {
	t.Helper()
	off := 7
	b := edgewire.Batch{Format: edgewire.Format, DeviceID: device, Epoch: 1, CounterFrom: from, NTPOffsetMs: &off}
	t0 := time.Date(2026, 1, 5, 0, 14, 22, 0, time.UTC)
	for i := 0; i < len(vals)/2; i++ {
		b.Readings = append(b.Readings, edgewire.Reading{
			Counter: from + int64(i), DeviceTime: edgewire.FormatDeviceTime(t0.Add(time.Duration(from+int64(i)) * 500 * time.Millisecond)),
			ValueKg: vals[2*i].(string), Stable: vals[2*i+1].(bool), Unit: edgewire.UnitKg,
		})
	}
	b.CounterTo = from + int64(len(b.Readings)) - 1
	if err := edgewire.Sign(key, &b); err != nil {
		t.Fatal(err)
	}
	return b
}
