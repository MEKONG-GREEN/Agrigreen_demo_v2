package ingest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

const vectorsFile = "../../../../contracts/edge/batch.v1.vectors.json"

type vectors struct {
	Valid []struct {
		Name          string          `json:"name"`
		JSON          json.RawMessage `json:"json"`
		Canonical     string          `json:"canonical"`
		PayloadSHA256 string          `json:"payload_sha256_hex"`
	} `json:"valid"`
	Invalid []struct {
		Name string          `json:"name"`
		JSON json.RawMessage `json:"json"`
		Code string          `json:"code"`
	} `json:"invalid"`
}

func loadVectors(t *testing.T) vectors {
	t.Helper()
	raw, err := os.ReadFile(vectorsFile)
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func countRows(t *testing.T, r *rig, tid uuid.UUID, device string) (batches, readings int) {
	t.Helper()
	ctx := context.Background()
	if err := tenant.WithTenantTx(ctx, r.store.Reader, tid, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM ts_weigh.reading_batch WHERE device_id = $1", device).Scan(&batches); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM ts_weigh.raw_reading WHERE device_id = $1", device).Scan(&readings)
	}); err != nil {
		t.Fatal(err)
	}
	return
}

// rebuild tái dựng lô từ cột ts_weigh (ADR-0017/0019: dạng chuẩn byte-cho-byte từ DB).
func rebuild(t *testing.T, r *rig, tid uuid.UUID, device string, epoch int, from int64) (edgewire.Batch, []byte) {
	t.Helper()
	ctx := context.Background()
	b := edgewire.Batch{Format: edgewire.Format, DeviceID: device, Epoch: epoch, CounterFrom: from}
	var sha, sig []byte
	var batchID uuid.UUID
	if err := tenant.WithTenantTx(ctx, r.store.Reader, tid, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id, counter_to, key_id, ntp_offset_ms, payload_sha256, signature FROM ts_weigh.reading_batch
		  WHERE device_id = $1 AND epoch = $2 AND counter_from = $3`, device, epoch, from).
			Scan(&batchID, &b.CounterTo, &b.KeyID, &b.NTPOffsetMs, &sha, &sig); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT counter, device_time, value_kg::text, stable, unit, raw FROM ts_weigh.raw_reading
		  WHERE batch_id = $1 ORDER BY counter`, batchID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var rd edgewire.Reading
			var dt time.Time
			var raw []byte
			if err := rows.Scan(&rd.Counter, &dt, &rd.ValueKg, &rd.Stable, &rd.Unit, &raw); err != nil {
				return err
			}
			rd.DeviceTime = edgewire.FormatDeviceTime(dt)
			if len(raw) > 0 {
				rd.Raw = base64.StdEncoding.EncodeToString(raw)
			}
			b.Readings = append(b.Readings, rd)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	b.Signature = base64.StdEncoding.EncodeToString(sig)
	return b, sha
}

// Vector batch.v1.vectors.json qua đầy đủ đường nhận (g3-conventions §7: xác minh chữ
// ký vector ở mọi hiện thực nhận lô): hợp lệ → ghi và tái dựng byte-cho-byte; hỏng → đúng mã edge.*.
func TestIngestEdgeVectorsEndToEnd(t *testing.T) {
	r := newRig(t)
	v := loadVectors(t)
	tid := ids.New()
	pub := vectorKey().Public().(ed25519.PublicKey)
	r.enroll(tid, "scale-hub01-in", 1, pub)
	r.enroll(tid, "scale-hub01-out", 2, pub)
	for _, vec := range v.Valid {
		var b edgewire.Batch
		_ = json.Unmarshal(vec.JSON, &b)
		code, ack := r.post(b.DeviceID, vec.JSON)
		if code != http.StatusOK || ack["high_water_counter"] != json.Number(strconv.FormatInt(b.CounterTo, 10)) || ack["device_id"] != b.DeviceID || ack["epoch"] != json.Number(strconv.Itoa(b.Epoch)) {
			t.Fatalf("%s: %d %v", vec.Name, code, ack)
		}
		got, sha := rebuild(t, r, tid, b.DeviceID, b.Epoch, b.CounterFrom)
		c, err := got.Canonical()
		if err != nil || string(c) != vec.Canonical {
			t.Fatalf("%s: dạng chuẩn tái dựng từ DB khác vector (%v):\n%q\n%q", vec.Name, err, c, vec.Canonical)
		}
		if hex.EncodeToString(sha) != vec.PayloadSHA256 {
			t.Fatalf("%s: payload_sha256 lưu sai", vec.Name)
		}
		if err := edgewire.Verify(pub, got); err != nil {
			t.Fatalf("%s: chữ ký phải xác minh lại được từ DB: %v", vec.Name, err)
		}
	}
	_, before := countRows(t, r, tid, "scale-hub01-in")
	for _, vec := range v.Invalid {
		var probe struct {
			DeviceID string `json:"device_id"`
		}
		_ = json.Unmarshal(vec.JSON, &probe)
		name := probe.DeviceID
		if name == "" || name != "scale-hub01-in" {
			name = "scale-hub01-in"
		}
		code, p := r.post(name, vec.JSON)
		if p["code"] != vec.Code || code < 400 || code >= 500 {
			t.Errorf("%s: mong %s, nhận %d %v", vec.Name, vec.Code, code, p["code"])
		}
	}
	if _, after := countRows(t, r, tid, "scale-hub01-in"); after != before {
		t.Fatalf("lô hỏng không được ghi gì: %d → %d", before, after)
	}
}

// ADR-0019 §3: gửi lặp cùng lô → cùng Ack, không ghi thêm (kể cả gửi đồng thời).
func TestIngestReplayIdempotent(t *testing.T) {
	r := newRig(t)
	tid, key := ids.New(), vectorKey()
	r.enroll(tid, "scale-replay", 1, key.Public().(ed25519.PublicKey))
	body := mustJSON(t, batch(t, key, "scale-replay", 1, 1, "10.000", true, "10.000", true, "10.000", true))
	var wg sync.WaitGroup
	acks := make([]map[string]any, 6)
	codes := make([]int, 6)
	for i := range acks {
		wg.Add(1)
		go func(i int) { defer wg.Done(); codes[i], acks[i] = r.post("scale-replay", body) }(i)
	}
	wg.Wait()
	for i := range acks {
		if codes[i] != http.StatusOK || acks[i]["high_water_counter"] != json.Number("3") || len(acks[i]) != 3 {
			t.Fatalf("lần %d: %d %v", i, codes[i], acks[i])
		}
	}
	if b, n := countRows(t, r, tid, "scale-replay"); b != 1 || n != 3 {
		t.Fatalf("phải đúng 1 lô, 3 số đo: %d %d", b, n)
	}
}

// ADR-0019 §3: cùng khóa khác nội dung → báo động; chồng lấn/lùi → từ chối; khoảng trống → nhận.
func TestIngestConflictRegressionAndGap(t *testing.T) {
	r := newRig(t)
	tid, key := ids.New(), vectorKey()
	dev := "scale-mono"
	r.enroll(tid, dev, 1, key.Public().(ed25519.PublicKey))
	if code, _ := r.post(dev, mustJSON(t, batch(t, key, dev, 1, 1, "1.000", true, "1.000", true, "1.000", true, "1.000", true))); code != 200 {
		t.Fatal(code)
	}
	cases := []struct {
		name string
		b    edgewire.Batch
		code string
	}{
		{"cùng counter_from khác giá trị", batch(t, key, dev, 1, 1, "2.000", true, "1.000", true, "1.000", true, "1.000", true), "ingest.batch_conflict"},
		{"cùng counter_from khác counter_to", batch(t, key, dev, 1, 1, "1.000", true), "ingest.batch_conflict"},
		{"chồng lấn", batch(t, key, dev, 1, 3, "1.000", true, "1.000", true), "ingest.counter_regression"},
		{"lùi", batch(t, key, dev, 1, 2, "1.000", true), "ingest.counter_regression"},
	}
	for _, c := range cases {
		code, p := r.post(dev, mustJSON(t, c.b))
		if code != http.StatusConflict || p["code"] != c.code {
			t.Fatalf("%s: %d %v", c.name, code, p)
		}
	}
	if code, ack := r.post(dev, mustJSON(t, batch(t, key, dev, 1, 10, "1.000", true))); code != 200 || ack["high_water_counter"] != json.Number("10") {
		t.Fatalf("khoảng trống phải được nhận: %d %v", code, ack)
	}
	if b, n := countRows(t, r, tid, dev); b != 2 || n != 5 {
		t.Fatalf("chỉ lô 1..4 và 10: %d %d", b, n)
	}
}

// ADR-0019 §1–2, §5: kênh mTLS + sổ thiết bị đã duyệt + khóa nguồn đã đăng ký.
func TestIngestDeviceAuthentication(t *testing.T) {
	r := newRig(t)
	tid, key := ids.New(), vectorKey()
	pub := key.Public().(ed25519.PublicKey)
	r.enroll(tid, "scale-auth", 1, pub)
	r.enroll(tid, "scale-auth", 2, pub)
	r.enroll(tid, "scale-rev", 1, pub)
	r.reg.setStatus("scale-rev", 1, port.DeviceRevoked)
	r.reg.setStatus("scale-auth", 1, port.DeviceSuperseded)
	good := mustJSON(t, batch(t, key, "scale-auth", 2, 1, "1.000", true))

	_, otherPriv, _ := ed25519.GenerateKey(nil)
	wrongKey := mustJSON(t, batch(t, otherPriv, "scale-auth", 2, 1, "1.000", true))
	forged := batch(t, key, "scale-auth", 2, 1, "1.000", true)
	sig, _ := forged.SignatureBytes()
	sig[0] ^= 0xff
	forged.Signature = base64.StdEncoding.EncodeToString(sig)

	// Chứng thư cùng CN nhưng không phải chứng thư đã duyệt (vd cấp lại trái phép).
	_, stolen := r.ca.leaf(t, "scale-auth", false)
	reqWith := func(c *x509.Certificate, body []byte) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, BatchesPath, bytes.NewReader(body))
		req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{c}}
		w := httptest.NewRecorder()
		r.engine.ServeHTTP(w, req)
		var p map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		return w.Code, p
	}
	cases := []struct {
		name   string
		do     func() (int, map[string]any)
		status int
		code   string
	}{
		{"không chứng thư client", func() (int, map[string]any) { return r.post("", good) }, 401, "ingest.client_cert_required"},
		{"chứng thư thiết bị khác", func() (int, map[string]any) { return r.post("scale-rev", good) }, 403, "ingest.device_mismatch"},
		{"thiết bị chưa đăng ký", func() (int, map[string]any) {
			return r.post("scale-nobody", mustJSON(t, batch(t, key, "scale-nobody", 1, 1, "1.000", true)))
		}, 403, "ingest.unknown_device"},
		{"chứng thư không khớp đăng ký", func() (int, map[string]any) { return reqWith(stolen, good) }, 403, "ingest.cert_mismatch"},
		{"thiết bị thu hồi", func() (int, map[string]any) {
			return r.post("scale-rev", mustJSON(t, batch(t, key, "scale-rev", 1, 1, "1.000", true)))
		}, 403, "ingest.device_revoked"},
		{"kỳ cũ đã thay", func() (int, map[string]any) {
			return r.post("scale-auth", mustJSON(t, batch(t, key, "scale-auth", 1, 1, "1.000", true)))
		}, 409, "ingest.epoch_superseded"},
		{"ký bằng khóa khác khóa đăng ký", func() (int, map[string]any) { return r.post("scale-auth", wrongKey) }, 422, "edge.key_id"},
		{"chữ ký giả", func() (int, map[string]any) { return r.post("scale-auth", mustJSON(t, forged)) }, 422, "edge.signature"},
	}
	for _, c := range cases {
		code, p := c.do()
		if code != c.status || p["code"] != c.code {
			t.Errorf("%s: mong %d %s, nhận %d %v", c.name, c.status, c.code, code, p["code"])
		}
	}
	if b, _ := countRows(t, r, tid, "scale-auth"); b != 0 {
		t.Fatalf("không lô nào được ghi: %d", b)
	}
	if code, _ := r.post("scale-auth", good); code != 200 {
		t.Fatalf("lô đúng vẫn phải nhận: %d", code)
	}
}

// Giới hạn thân (edgewire.MaxBodyBytes) và tốc độ theo thiết bị.
func TestIngestBodyAndRateLimit(t *testing.T) {
	r := newRig(t)
	tid, key := ids.New(), vectorKey()
	r.enroll(tid, "scale-rate", 1, key.Public().(ed25519.PublicKey))
	big := bytes.Repeat([]byte(" "), edgewire.MaxBodyBytes+10)
	if code, p := r.post("scale-rate", big); code != http.StatusRequestEntityTooLarge || p["code"] != "payload_too_large" {
		t.Fatalf("thân quá lớn: %d %v", code, p)
	}
	r.rv.Limiter = NewRateLimiter(r.clk, 60, 2)
	for i := int64(1); i <= 2; i++ {
		if code, _ := r.post("scale-rate", mustJSON(t, batch(t, key, "scale-rate", 1, i, "1.000", true))); code != 200 {
			t.Fatalf("lô %d trong burst: %d", i, code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, BatchesPath, bytes.NewReader(mustJSON(t, batch(t, key, "scale-rate", 1, 3, "1.000", true))))
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{r.certs["scale-rate"]}}
	w := httptest.NewRecorder()
	r.engine.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("vượt tốc độ: %d %q", w.Code, w.Header().Get("Retry-After"))
	}
	r.clk.Advance(time.Second)
	if code, _ := r.post("scale-rate", mustJSON(t, batch(t, key, "scale-rate", 1, 3, "1.000", true))); code != 200 {
		t.Fatalf("sau 1 s có token mới: %d", code)
	}
}

// RB-2: số đo tenant A không đọc được dưới tenant B — cả bảng ts lẫn cửa sổ tích phân.
func TestIngestTenantIsolationTS(t *testing.T) {
	r := newRig(t)
	a, b, key := ids.New(), ids.New(), vectorKey()
	pub := key.Public().(ed25519.PublicKey)
	r.enroll(a, "scale-ten-a", 1, pub)
	r.enroll(b, "scale-ten-b", 1, pub)
	for _, d := range []string{"scale-ten-a", "scale-ten-b"} {
		if code, _ := r.post(d, mustJSON(t, batch(t, key, d, 1, 1, "5.000", true, "5.000", true))); code != 200 {
			t.Fatal(code)
		}
	}
	if bt, n := countRows(t, r, b, "scale-ten-a"); bt != 0 || n != 0 {
		t.Fatalf("tenant B thấy số đo của A: %d %d", bt, n)
	}
	if bt, n := countRows(t, r, a, "scale-ten-a"); bt != 1 || n != 2 {
		t.Fatalf("tenant A phải thấy số đo của mình: %d %d", bt, n)
	}
	in := &Integrator{Windows: r.store}
	w := port.Window{DeviceID: "scale-ten-a", Epoch: 1, CounterFrom: 1, CounterTo: 2}
	if _, err := in.Integrate(context.Background(), b, w); !errors.Is(err, port.ErrWindowIncomplete) {
		t.Fatalf("tích phân dưới tenant khác phải thiếu số đo: %v", err)
	}
	if res, err := in.Integrate(context.Background(), a, w); err != nil || res.NetKg.String() != "5.000" {
		t.Fatalf("tenant đúng: %v %v", res.NetKg, err)
	}
}

// ---- vòng tích phân ----

type fakeSessions struct {
	mu   sync.Mutex
	list []Session
	done map[uuid.UUID]bool
}

func (f *fakeSessions) PendingSessions(_ context.Context, limit int) ([]Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Session
	for _, s := range f.list {
		if !f.done[s.ID] && len(out) < limit {
			out = append(out, s)
		}
	}
	return out, nil
}

type fakeTickets struct {
	mu       sync.Mutex
	byKey    map[string]port.Ticket
	sessions *fakeSessions
	calls    int
}

func (f *fakeTickets) Record(_ context.Context, tid uuid.UUID, d port.TicketDraft) (port.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if d.Source != port.SourceEdge || d.Integration == nil || d.Integration.RuleVersion == "" || d.ActorRef == uuid.Nil {
		return port.Ticket{}, port.ErrInvalidTicket
	}
	if t, ok := f.byKey[d.IdemKey]; ok {
		return t, nil
	}
	if f.byKey == nil {
		f.byKey = map[string]port.Ticket{}
	}
	t := port.Ticket{TicketDraft: d, Seq: int64(len(f.byKey) + 1)}
	f.byKey[d.IdemKey] = t
	_ = tid
	return t, nil
}

type fixedTare map[string]string

func (f fixedTare) TareKg(_ context.Context, _ uuid.UUID, w port.Window) (mass.Kg, error) {
	return mass.ParseKg(f[w.DeviceID+":"+strconv.FormatInt(w.CounterFrom, 10)])
}

// E2E proto seed: agent đẩy lô (edgewire) → ingest ghi ts → phiên cân đóng → phiếu 96,8 t
// với rule_version, khoảng bộ đếm, băm số đo, reading_batch đã dùng (R-70). Chạy lại →
// idempotent. Phiên có khoảng trống → thử lại, báo động một lần sau ngưỡng.
func TestIngestIntegrationLoopEndToEnd(t *testing.T) {
	r := newRig(t)
	tid, key := ids.New(), vectorKey()
	dev := "scale-hub09-in"
	r.enroll(tid, dev, 1, key.Public().(ed25519.PublicKey))
	send := func(b edgewire.Batch) {
		t.Helper()
		if code, p := r.post(dev, mustJSON(t, b)); code != 200 {
			t.Fatalf("gửi lô %d: %d %v", b.CounterFrom, code, p)
		}
	}
	send(batch(t, key, dev, 1, 1, "12000.000", false, "98000.000", false, "138420.000", false, "138400.000", true))
	send(batch(t, key, dev, 1, 5, "138380.000", true, "138400.000", true, "138410.000", true, "138400.000", true, "150000.000", false))
	// Phiên 2 có khoảng trống: lô 10..11 rồi 14..15 (12, 13 mất vĩnh viễn — bộ đếm không lùi).
	send(batch(t, key, dev, 1, 10, "5000.000", true, "5000.000", true))
	send(batch(t, key, dev, 1, 14, "5000.000", true, "5000.000", true))

	ok := Session{ID: ids.New(), TenantID: tid, LotRef: ids.New(), Point: port.PointHubIn, Route: port.RouteHub,
		Window: port.Window{DeviceID: dev, Epoch: 1, CounterFrom: 1, CounterTo: 9}, ActorRef: ids.New(), ClosedAt: r.clk.Now()}
	gap := Session{ID: ids.New(), TenantID: tid, LotRef: ids.New(), Point: port.PointHubIn, Route: port.RouteHub,
		Window: port.Window{DeviceID: dev, Epoch: 1, CounterFrom: 10, CounterTo: 15}, ActorRef: ids.New(), ClosedAt: r.clk.Now()}
	src := &fakeSessions{list: []Session{ok, gap}, done: map[uuid.UUID]bool{}}
	tickets := &fakeTickets{sessions: src}
	loop := &Loop{
		Sessions: src, Tickets: tickets, Clock: r.clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Metrics: NewMetrics(nil),
		Integrator: &Integrator{Windows: r.store, Tare: fixedTare{dev + ":1": "41600.000", dev + ":10": "0.000"}},
		StuckAfter: 10 * time.Minute,
	}
	ctx := context.Background()
	st, err := loop.RunOnce(ctx)
	if err != nil || st.Recorded != 1 || st.Retrying != 1 || st.Alerted != 0 {
		t.Fatalf("lượt 1: %+v %v", st, err)
	}
	tk := tickets.byKey[IdemKey(ok.ID, RuleV1)]
	in := tk.Integration
	if tk.MassKg.String() != "96800.000" || in == nil || in.RuleVersion != RuleV1 || in.Window != ok.Window ||
		len(in.ReadingsSHA256) != 64 || len(in.BatchIDs) != 2 || in.ReadingsUsed != 5 || in.ReadingsTotal != 9 ||
		tk.Point != port.PointHubIn || tk.LotRef != ok.LotRef || tk.EventTime.IsZero() {
		t.Fatalf("phiếu sai: %+v %+v", tk.TicketDraft, in)
	}
	// Tái tích phân qua port.Integrator (tranh chấp) → cùng kết quả byte-cho-byte.
	again, err := loop.Integrator.(*Integrator).Integrate(ctx, tid, ok.Window)
	if err != nil || again.ReadingsSHA256 != in.ReadingsSHA256 || again.NetKg.String() != "96800.000" {
		t.Fatalf("tính lại khác: %+v %v", again, err)
	}
	// Nguồn trả lại phiên đã có phiếu (chưa kịp đánh dấu) → Record idempotent, không phiếu mới.
	if st, _ := loop.RunOnce(ctx); st.Recorded != 1 || len(tickets.byKey) != 1 {
		t.Fatalf("chạy lại phải idempotent: %+v %d", st, len(tickets.byKey))
	}
	src.done[ok.ID] = true
	r.clk.Advance(11 * time.Minute)
	if st, _ := loop.RunOnce(ctx); st.Alerted != 1 || st.Retrying != 1 {
		t.Fatalf("phiên thiếu lô quá ngưỡng phải báo động: %+v", st)
	}
	if st, _ := loop.RunOnce(ctx); st.Alerted != 0 || st.Retrying != 1 {
		t.Fatalf("báo động chỉ một lần: %+v", st)
	}
	if _, err := loop.Integrator.(*Integrator).Integrate(ctx, tid, gap.Window); !errors.Is(err, port.ErrWindowIncomplete) {
		t.Fatalf("khoảng trống: %v", err)
	}
	if len(tickets.byKey) != 1 {
		t.Fatal("không bao giờ ghi phiếu cho cửa sổ thiếu")
	}
}

// Độ lệch đồng hồ: đo và gắn cờ, không ảnh hưởng nhận lô (ADR-0019 §4).
func TestIngestClockSkewFlagged(t *testing.T) {
	r := newRig(t)
	reader := sdkmetric.NewManualReader()
	r.rv.Metrics = NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	tid, key := ids.New(), vectorKey()
	r.enroll(tid, "scale-skew", 1, key.Public().(ed25519.PublicKey))
	b := batch(t, key, "scale-skew", 1, 1, "1.000", true)
	off := 5000
	b.NTPOffsetMs = &off
	_ = edgewire.Sign(key, &b)
	if code, _ := r.post("scale-skew", mustJSON(t, b)); code != 200 {
		t.Fatalf("lô lệch đồng hồ vẫn được nhận: %d", code)
	}
	b2 := batch(t, key, "scale-skew", 1, 2, "1.000", true)
	b2.NTPOffsetMs = nil
	b2.Readings[0].DeviceTime = edgewire.FormatDeviceTime(time.Now().Add(time.Hour)) // thiết bị đi trước giờ hệ
	_ = edgewire.Sign(key, &b2)
	if code, _ := r.post("scale-skew", mustJSON(t, b2)); code != 200 {
		t.Fatalf("lô chưa NTP vẫn được nhận: %d", code)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	flags := map[string]int64{}
	var accepted int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				switch m.Name {
				case MetricClockSkewFlagged:
					v, _ := dp.Attributes.Value("reason")
					flags[v.AsString()] += dp.Value
				case MetricBatches:
					if v, _ := dp.Attributes.Value("outcome"); v.AsString() == "accepted" {
						accepted += dp.Value
					}
				}
			}
		}
	}
	if flags["ntp_offset"] != 1 || flags["ntp_unsynced"] != 1 || flags["device_ahead"] != 1 || accepted != 2 {
		t.Fatalf("cờ lệch đồng hồ: %v, accepted %d", flags, accepted)
	}
}

// Listener mTLS thật: không chứng thư client → bắt tay thất bại; chứng thư CA khác → thất bại;
// chứng thư thiết bị đã duyệt → nhận lô.
func TestIngestMTLSListener(t *testing.T) {
	r := newRig(t)
	tid, key := ids.New(), vectorKey()
	dev := "scale-mtls"
	dir := t.TempDir()
	srvCert, _ := r.ca.leaf(t, "127.0.0.1", true)
	devCert, devX := r.ca.leaf(t, dev, false)
	r.certs[dev] = devX
	r.enroll(tid, dev, 1, key.Public().(ed25519.PublicKey))
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(srvCert.PrivateKey)
	cfg, err := ServerTLS(write("srv.crt", pemEncode("CERTIFICATE", srvCert.Certificate[0])), write("srv.key", pemEncode("PRIVATE KEY", keyDER)), write("ca.crt", r.ca.pem))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ServerTLS("", "", ""); err == nil {
		t.Fatal("thiếu tệp phải lỗi")
	}
	srv := httptest.NewUnstartedServer(r.engine)
	srv.TLS = cfg
	srv.StartTLS()
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(r.ca.cert)
	client := func(certs ...tls.Certificate) *http.Client {
		return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: certs, ServerName: "127.0.0.1",
		}}}
	}
	body := mustJSON(t, batch(t, key, dev, 1, 1, "1.000", true))
	postTo := func(c *http.Client) (*http.Response, error) {
		return c.Post(srv.URL+BatchesPath, "application/json", bytes.NewReader(body)) //nolint:noctx
	}
	if resp, err := postTo(client()); err == nil {
		_ = resp.Body.Close()
		t.Fatal("không chứng thư client phải bị từ chối ở bắt tay")
	}
	other := newCA(t)
	foreign, _ := other.leaf(t, dev, false)
	if resp, err := postTo(client(foreign)); err == nil {
		_ = resp.Body.Close()
		t.Fatal("chứng thư CA lạ phải bị từ chối")
	}
	resp, err := postTo(client(devCert))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var ack Ack
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil || resp.StatusCode != 200 || ack.HighWaterCounter != 1 {
		t.Fatalf("thiết bị đã duyệt: %d %+v %v", resp.StatusCode, ack, err)
	}
}
