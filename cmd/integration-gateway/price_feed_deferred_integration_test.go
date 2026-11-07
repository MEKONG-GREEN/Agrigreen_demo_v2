package main

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A disabled KN gateway policy denies existing stored data too. The shared
// gateway's nil policy and unrelated HTX feeds keep their existing contracts.
func TestPriceFeedDeferredGatewayPolicyPreservesStoredPublication(t *testing.T) {
	e := newIntEnv(t)
	publisher := ids.New()
	src := &priceStub{recs: map[uuid.UUID][]map[string]any{publisher: {priceRec("MH00001", "8000", "10000", 1, false)}}}
	e.mod.UsePublishedFeedSources(map[string]apiclientport.PublishedSource{feedPrice: src},
		map[string][]apiclientport.Publisher{feedPrice: {{Tenant: publisher, Code: "89"}}})
	e.buildFeeds()
	_, key := e.onboardPriceFeed()
	_, otherKey := e.onboardFeed()
	d := e.deps
	d.AllowUnapproved = true
	shared := httptest.NewServer(gateway.NewHandler(e.mod, d, nil, e.log))
	t.Cleanup(shared.Close)
	get := func(server, path, key string) intResp {
		return doReq(t, http.MethodGet, server+path,
			map[string]string{apiclientport.KeyHeader: key, "X-Profile": "other", "X-AG-Profile": "other"}, nil)
	}
	pricePath := "/v1/feeds/" + feedPrice
	before := get(shared.URL, pricePath, key)
	want(t, before, 200, "shared gateway before disabled policy")
	if items, ok := before.body["items"].([]any); !ok || len(items) != 1 {
		t.Fatalf("missing stored publication: %s", before.raw)
	}
	want(t, get(shared.URL, "/v1/snapshots/"+feedPrice+"/1", key), 200, "shared snapshot remains usable")
	want(t, get(shared.URL, "/v1/feeds/"+feedPrice+"/checksums", key), 200, "shared checksum remains usable")
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	open := func(context.Context, string) (*pgxpool.Pool, error) {
		return e.pg.Pool(t, migrations.PF, dbadmin.AppPublic), nil
	}
	mod, policy, err := assembly.Build(context.Background(), open, assembly.Config{Operator: e.operator, Peppers: peppers, Replicas: 1,
		AllowUnapproved: true, Clock: clock.System{}, Log: e.log, DisabledFeeds: map[string]bool{feedPrice: true}})
	if err != nil {
		t.Fatal(err)
	}
	blocked := httptest.NewServer(gateway.NewHandler(mod, policy, nil, e.log))
	t.Cleanup(blocked.Close)
	for _, path := range []string{pricePath, pricePath + "?contract_version=1&profile=other", pricePath + "/checksums", "/v1/snapshots/" + feedPrice + "/1"} {
		r := get(blocked.URL, path, key)
		if r.status != 403 || r.code() != apiclientport.ErrFeedDisabled.Code {
			t.Fatalf("disabled %s: %d %s", path, r.status, r.raw)
		}
		if r.body["items"] != nil || r.body["url"] != nil || r.body["versions"] != nil {
			t.Fatalf("disabled policy leaked stored payload: %s", r.raw)
		}
	}
	want(t, get(blocked.URL, "/v1/feeds/"+feedHP, otherKey), 200, "unrelated HTX feed remains available")
	mod.UsePublishedFeedSources(map[string]apiclientport.PublishedSource{feedPrice: src},
		map[string][]apiclientport.Publisher{feedPrice: {{Tenant: publisher, Code: "89"}}})
	if _, err := mod.Service().BuildPublished(context.Background(), feedPrice); problem.From(err).Code != apiclientport.ErrFeedDisabled.Code {
		t.Fatalf("disabled builder: %v", err)
	}
	after := get(shared.URL, pricePath, key)
	want(t, after, 200, "stored publication after denied access and build")
	if string(before.raw) != string(after.raw) {
		t.Fatalf("disabled policy rewrote stored publication\nbefore=%s\nafter=%s", before.raw, after.raw)
	}
}
