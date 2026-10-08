package public_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/public"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
)

// fakeAuth: khóa → bên gọi (tenant bên gọi chỉ đến từ bản ghi khóa).
type fakeAuth struct{ callers map[string]port.Client }

func (f fakeAuth) Authenticate(_ context.Context, presented string, at time.Time) (port.Caller, error) {
	c, ok := f.callers[presented]
	if !ok {
		return port.Caller{}, port.ErrUnauthenticated
	}
	prefix, _ := port.ParseKey(presented)
	return port.Caller{Client: c, KeyID: ids.New(), KeyPrefix: prefix, AuthMethod: port.AuthAPIKey, RequestID: ids.New(), ReceivedAt: at}, nil
}

type fakePlans struct{ plan port.Plan }

func (f fakePlans) PlanFor(_ context.Context, _, clientID uuid.UUID, _ time.Time) (port.Plan, error) {
	p := f.plan
	p.ClientID = clientID
	return p, nil
}

// fakeMetering mô phỏng pf/metering: Record tính đơn vị bằng UnitsV1, vượt hạn mức tháng → dòng sổ
// status 429 units 0 + ErrQuotaExceeded; Remaining đọc cùng bộ đếm.
type fakeMetering struct {
	mu      sync.Mutex
	calls   []meter.Call
	units   []int64
	monthly int64
	used    int64
	err     error
}

func (f *fakeMetering) Record(_ context.Context, _ uuid.UUID, c meter.Call) (meter.Recorded, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return meter.Recorded{}, f.err
	}
	u := meter.UnitsV1(c)
	if u > 0 && f.used+u > f.monthly {
		c.Status, c.ProblemCode = http.StatusTooManyRequests, meter.ErrQuotaExceeded.Code
		f.calls, f.units = append(f.calls, c), append(f.units, 0)
		return meter.Recorded{}, meter.ErrQuotaExceeded
	}
	f.used += u
	f.calls, f.units = append(f.calls, c), append(f.units, u)
	return meter.Recorded{EventID: ids.New(), Units: u, RuleVersion: meter.RuleUnitsV1, MonthlyRemaining: f.monthly - f.used,
		DailyRemaining: f.monthly - f.used}, nil
}

func (f *fakeMetering) Remaining(context.Context, uuid.UUID, uuid.UUID, string, time.Time) (meter.Remaining, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return meter.Remaining{DailyUnits: f.monthly - f.used, MonthlyUnits: f.monthly - f.used}, nil
}

func (f *fakeMetering) UsageOf(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) ([]meter.UsageDay, error) {
	return []meter.UsageDay{{Date: "2026-10-01", Product: port.ProductCollectionVolume, Calls: 2, Units: 2}}, nil
}

func (f *fakeMetering) snapshot() ([]meter.Call, int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var sum int64
	for _, u := range f.units {
		sum += u
	}
	return append([]meter.Call(nil), f.calls...), sum
}

// fakeGrants: cấp quyền (bên cấp → bên nhận, loại). Ghi lại grantee đã tra.
type fakeGrants struct {
	mu       sync.Mutex
	grants   map[uuid.UUID]map[string]uuid.UUID // grantee → kind → grantor
	grantees []uuid.UUID
}

func (f *fakeGrants) ActiveGrant(_ context.Context, grantee, grantor uuid.UUID, kind string, at time.Time) (partyport.ShareGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grantees = append(f.grantees, grantee)
	g, ok := f.grants[grantee][kind]
	if !ok || (grantor != uuid.Nil && grantor != g) {
		return partyport.ShareGrant{}, partyport.ErrShareGrantMissing
	}
	return partyport.ShareGrant{ID: ids.New(), Grantor: g, Grantee: grantee, Kinds: []string{kind}, ValidFrom: at.Add(-time.Hour), ValidTo: at.Add(time.Hour)}, nil
}

func (f *fakeGrants) revoke(grantee uuid.UUID, kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.grants[grantee], kind)
}

type fakeCollection struct {
	items []port.CollectionVolumeV1
	last  port.CollectionQuery
}

func (f *fakeCollection) CollectionCells(_ context.Context, q port.CollectionQuery) (port.CollectionPage, error) {
	f.last = q
	return port.CollectionPage{Items: f.items, Suppressed: 3}, nil
}

func cell(commune string) port.CollectionVolumeV1 {
	return port.CollectionVolumeV1{ProvinceCode: "87", CommuneCode: commune, SeasonCode: "2026-HT", PeriodMonth: "2026-08", RollsCollected: 1200,
		AreaHaCollected: "35.40", KApplied: 10, AsOf: "2026-09-30T10:00:00Z"}
}

type env struct {
	t          *testing.T
	router     *gin.Engine
	meter      *fakeMetering
	grants     *fakeGrants
	coll       *fakeCollection
	clk        *clock.Fixed
	key        string
	client     port.Client
	grantor    uuid.UUID
	thirdParty uuid.UUID
	logs       *bytes.Buffer
}

var testKey = "agk_abcdefghijkl_" + strings.Repeat("A", 43)

type opt func(*public.Deps)

func newEnv(t *testing.T, opts ...opt) *env {
	t.Helper()
	tp, grantor := ids.New(), ids.New()
	client := port.Client{ID: ids.New(), OperatorTenant: ids.New(), ClientTenantID: tp, DisplayName: "Nhà máy VFT", Status: port.ClientActive,
		AuthMethods: []port.AuthMethod{port.AuthAPIKey}, Scopes: []string{"dp:collection_volume:read", "dp:collection_volume:export", port.ScopeUsageRead}}
	e := &env{t: t, meter: &fakeMetering{monthly: 1000}, grants: &fakeGrants{grants: map[uuid.UUID]map[string]uuid.UUID{tp: {
		partyport.KindDPCollectionVolume: grantor, partyport.KindDPHubCapacity: grantor}}},
		coll: &fakeCollection{items: []port.CollectionVolumeV1{cell("30001"), cell("30002")}}, clk: clock.NewFixed(time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)),
		key: testKey, client: client, grantor: grantor, thirdParty: tp, logs: &bytes.Buffer{}}
	d := public.Deps{Auth: fakeAuth{callers: map[string]port.Client{testKey: client}},
		Plans:   fakePlans{plan: port.Plan{RequestsPerMinute: 600, DailyUnits: 1000, MonthlyUnits: 1000, MaxExportRows: 1000}},
		Limiter: public.NewMemory(1), Ledger: e.meter, Quotas: e.meter, Usage: e.meter, Grants: e.grants, Collection: e.coll,
		OperatorTenant: client.OperatorTenant, Clock: e.clk, Log: slog.New(slog.NewJSONHandler(e.logs, nil))}
	for _, o := range opts {
		o(&d)
	}
	e.router = public.NewRouter(d)
	return e
}

type resp struct {
	status int
	header http.Header
	raw    []byte
	body   map[string]any
}

func (e *env) do(method, path, key string, body []byte, hdr ...string) resp {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if key != "" {
		req.Header.Set(port.KeyHeader, key)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	out := resp{status: w.Code, header: w.Header(), raw: w.Body.Bytes()}
	_ = json.Unmarshal(out.raw, &out.body)
	return out
}

func (r resp) code() string {
	s, _ := r.body["code"].(string)
	return s
}

var errBoom = errors.New("metering: DB không sẵn sàng")
