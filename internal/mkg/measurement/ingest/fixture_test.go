package ingest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// vectorKey là khóa của contracts/edge/batch.v1.vectors.json (seed 00..1f) — CHỈ kiểm thử.
func vectorKey() ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	return ed25519.NewKeyFromSeed(seed)
}

// ---- chứng thư ----

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

var serial int64 = 100

func nextSerial() *big.Int { serial++; return big.NewInt(serial) }

func newCA(t *testing.T) *testCA {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: nextSerial(), Subject: pkix.Name{CommonName: "ingest-test-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return &testCA{cert: c, key: k, pem: pemEncode("CERTIFICATE", der)}
}

func pemEncode(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

// leaf cấp chứng thư lá (CN = SAN = name).
func (ca *testCA) leaf(t *testing.T, name string, server bool) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	usage := x509.ExtKeyUsageClientAuth
	if server {
		usage = x509.ExtKeyUsageServerAuth
	}
	tpl := &x509.Certificate{
		SerialNumber: nextSerial(), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
	}
	if ip := net.ParseIP(name); ip != nil {
		tpl.IPAddresses, tpl.DNSNames = []net.IP{ip}, nil
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.cert, &k.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k, Leaf: c}, c
}

// ---- sổ thiết bị giả (hiện thực thật thuộc mkg/measurement) ----

type fakeRegistry struct {
	mu   sync.Mutex
	devs map[string]port.Device // khóa device|epoch
}

func devKey(id string, epoch int) string { return fmt.Sprintf("%s|%d", id, epoch) }

func (r *fakeRegistry) add(d port.Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.devs == nil {
		r.devs = map[string]port.Device{}
	}
	r.devs[devKey(d.DeviceID, d.Epoch)] = d
}

func (r *fakeRegistry) setStatus(id string, epoch int, s port.DeviceStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.devs[devKey(id, epoch)]
	d.Status = s
	r.devs[devKey(id, epoch)] = d
}

func (r *fakeRegistry) Resolve(_ context.Context, id string, epoch int, certSHA string) (port.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devs[devKey(id, epoch)]
	switch {
	case !ok:
		return port.Device{}, port.ErrUnknownDevice
	case d.Status == port.DeviceRevoked:
		return port.Device{}, port.ErrDeviceRevoked
	case d.Status == port.DeviceSuperseded:
		return port.Device{}, port.ErrEpochSuperseded
	case d.CertSHA256 != certSHA:
		return port.Device{}, port.ErrCertMismatch
	}
	return d, nil
}

// ---- môi trường ----

type rig struct {
	t        *testing.T
	ca       *testCA
	reg      *fakeRegistry
	store    *TSStore
	clk      *clock.Fixed
	engine   *gin.Engine
	rv       *Receiver
	certs    map[string]*x509.Certificate
	tenantOf map[string]uuid.UUID
}

// measurementDB: vai nhóm của module chủ ts_weigh (gói ingest không import gốc mkg/measurement).
var measurementDB = shareddb.Module("mkg/measurement")

func newRig(t *testing.T) *rig {
	t.Helper()
	env := pgtest.Get(t)
	r := &rig{
		t: t, ca: newCA(t), reg: &fakeRegistry{}, clk: clock.NewFixed(time.Date(2026, 12, 3, 0, 20, 0, 0, time.UTC)),
		// W2.6: vai đăng nhập app_ingest, mọi giao dịch SET LOCAL ROLE r_measurement (như cmd/ingest).
		store: &TSStore{Writer: shareddb.ForModule(env.Pool(t, migrations.TS, dbadmin.AppIngest), measurementDB),
			Reader: shareddb.ForModule(env.Pool(t, migrations.TS, dbadmin.AppIngest), measurementDB)},
		certs:    map[string]*x509.Certificate{},
		tenantOf: map[string]uuid.UUID{},
	}
	r.rv = &Receiver{Devices: r.reg, Store: r.store, Clock: r.clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Metrics: NewMetrics(nil)}
	r.engine = r.rv.NewEngine()
	return r
}

// enroll đăng ký (device, epoch) với khóa pub cho tenant; chứng thư mTLS mới cho device nếu chưa có.
func (r *rig) enroll(tid uuid.UUID, device string, epoch int, pub ed25519.PublicKey) {
	c, ok := r.certs[device]
	if !ok {
		_, c = r.ca.leaf(r.t, device, false)
		r.certs[device] = c
	}
	r.tenantOf[device] = tid
	r.reg.add(port.Device{
		TenantID: tid, DeviceID: device, Epoch: epoch, FacilityRef: ids.New(), Point: port.PointHubIn,
		PublicKey: pub, KeyID: edgewire.KeyID(pub), CertSHA256: CertSHA256(c), ScaleModel: "test", Status: port.DeviceActive,
	})
}

// post gửi thân với chứng thư của certName (nil = không chứng thư).
func (r *rig) post(certName string, body []byte) (int, map[string]any) {
	req := httptest.NewRequest(http.MethodPost, BatchesPath, bytes.NewReader(body))
	if certName != "" {
		c := r.certs[certName]
		if c == nil {
			_, c = r.ca.leaf(r.t, certName, false)
			r.certs[certName] = c
		}
		req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{c}}
	}
	w := httptest.NewRecorder()
	r.engine.ServeHTTP(w, req)
	var out map[string]any
	dec := json.NewDecoder(w.Body)
	dec.UseNumber() // số JSON → json.Number (package measurement cấm float, R-10)
	_ = dec.Decode(&out)
	return w.Code, out
}

// batch dựng và ký lô [from, from+len(vals)-1]; vals là (value_kg, stable) xen kẽ.
func batch(t *testing.T, key ed25519.PrivateKey, device string, epoch int, from int64, vals ...any) edgewire.Batch {
	t.Helper()
	off := 7
	b := edgewire.Batch{Format: edgewire.Format, DeviceID: device, Epoch: epoch, CounterFrom: from, NTPOffsetMs: &off}
	t0 := time.Date(2026, 1, 5, 0, 14, 22, 0, time.UTC) // quá khứ so với giờ DB — không tự gắn cờ device_ahead
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

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
