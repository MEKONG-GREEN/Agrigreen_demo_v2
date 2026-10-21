package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
)

func auditPrincipal() authz.Principal {
	return authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), MFA: true, Roles: []string{authz.RolePlatformAdmin}}
}
func auditTestRow(at time.Time) port.AuditRecord {
	return port.AuditRecord{EventRef: ids.New(), EntityRef: ids.New(), ActorRef: ids.New(), OccurredAt: at, RecordedAt: at, Action: "created", Outcome: "unknown"}
}
func TestAuditCursorStableMergeBindingsAndReadReceipt(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	clk := clock.NewFixed(now)
	p := auditPrincipal()
	ctx := authz.WithPrincipal(context.Background(), p)
	in := AuditInput{From: now.Add(-time.Hour), To: now.Add(time.Hour), Limit: 1}
	rows := map[string][]port.AuditRecord{"a.events": {auditTestRow(now.Add(-time.Minute))}, "b.events": {auditTestRow(now.Add(-time.Minute)), auditTestRow(now.Add(-2 * time.Minute))}}
	calls := []string{}
	makeSource := func(key string) port.AuditSource {
		return port.AuditSource{Key: key, Module: "example", Read: func(got context.Context, tid uuid.UUID, q port.AuditReadQuery) ([]port.AuditRecord, error) {
			if tid != p.TenantID {
				t.Fatal("reader tenant changed")
			}
			identity, _ := authz.FromContext(got)
			if identity.ActorRef != p.ActorRef || identity.TenantID != p.TenantID {
				t.Fatal("identity changed")
			}
			calls = append(calls, key)
			out := []port.AuditRecord{}
			for _, r := range rows[key] {
				pos := port.AuditPosition{RecordedAt: r.RecordedAt, SourceKey: key, EventRef: r.EventRef}
				if !r.RecordedAt.After(q.AsOf) && (q.Before == nil || auditCompare(pos, *q.Before) < 0) {
					out = append(out, r)
				}
			}
			if len(out) > q.Limit {
				out = out[:q.Limit]
			}
			return out, nil
		}}
	}
	reg, err := port.NewAuditRegistry(makeSource("a.events"), makeSource("b.events"))
	if err != nil {
		t.Fatal(err)
	}
	receipts := []port.AuditReadReceipt{}
	svc := Service{Clock: clk, AuditRegistry: reg, AuditAccess: port.AuditAccess{Authorize: func(context.Context) error { calls = append(calls, "authorize"); return nil }, RecordRead: func(_ context.Context, r port.AuditReadReceipt) error {
		calls = append(calls, "record")
		receipts = append(receipts, r)
		return nil
	}}}
	page, err := svc.Audit(ctx, in)
	if err != nil || len(page.Items) != 1 || page.Items[0].Source != "b.events" || page.NextCursor == nil {
		t.Fatalf("first page %+v %v", page, err)
	}
	if !slices.Equal(calls, []string{"authorize", "a.events", "b.events", "record"}) || receipts[0].ActorRef != p.ActorRef || receipts[0].TenantID != p.TenantID || receipts[0].Returned[0].EventRef != page.Items[0].EventRef {
		t.Fatal("read receipt/order wrong", calls)
	}
	// New facts arriving after the watermark cannot change subsequent pages.
	clk.Advance(10 * time.Minute)
	rows["a.events"] = append([]port.AuditRecord{auditTestRow(now.Add(time.Minute))}, rows["a.events"]...)
	in.Cursor = *page.NextCursor
	second, err := svc.Audit(ctx, in)
	if err != nil || len(second.Items) != 1 || second.Items[0].Source != "a.events" || !second.AsOf.Equal(now) {
		t.Fatalf("second page %+v %v", second, err)
	}
	thirdInput := in
	thirdInput.Cursor = *second.NextCursor
	third, err := svc.Audit(ctx, thirdInput)
	if err != nil || len(third.Items) != 1 || third.NextCursor != nil || third.Items[0].Source != "b.events" {
		t.Fatalf("last page %+v %v", third, err)
	}
	changed := in
	changed.Action = "locked"
	if _, err = svc.Audit(ctx, changed); err == nil || problem.From(err).Code != problem.Validation.Code {
		t.Fatal("cursor allowed different filter", err)
	}
	changed = in
	changed.Limit = 2
	if _, err = svc.Audit(ctx, changed); err == nil || problem.From(err).Code != problem.Validation.Code {
		t.Fatal("cursor allowed different limit", err)
	}
	other := p
	other.TenantID = ids.New()
	if _, err = svc.Audit(authz.WithPrincipal(context.Background(), other), in); err == nil || problem.From(err).Code != problem.Validation.Code {
		t.Fatal("cursor crossed tenant", err)
	}
	data, _ := base64.RawURLEncoding.DecodeString(in.Cursor)
	var cur auditCursor
	_ = json.Unmarshal(data, &cur)
	cur.Before.SourceKey = "missing.events"
	data, _ = json.Marshal(cur)
	changed = in
	changed.Cursor = base64.RawURLEncoding.EncodeToString(data)
	if _, err = svc.Audit(ctx, changed); err == nil || problem.From(err).Code != problem.Validation.Code {
		t.Fatal("unknown cursor source accepted", err)
	}
}
func TestAuditAuthorizationRecorderAndSourceFailClosed(t *testing.T) {
	now := time.Now().UTC()
	p := auditPrincipal()
	ctx := authz.WithPrincipal(context.Background(), p)
	in := AuditInput{From: now.Add(-time.Hour), To: now.Add(time.Hour)}
	readCalls, recordCalls := 0, 0
	src := port.AuditSource{Key: "example.events", Module: "example", Read: func(context.Context, uuid.UUID, port.AuditReadQuery) ([]port.AuditRecord, error) {
		readCalls++
		return []port.AuditRecord{auditTestRow(now.Add(-time.Minute))}, nil
	}}
	reg, _ := port.NewAuditRegistry(src)
	svc := Service{Clock: clock.NewFixed(now), AuditRegistry: reg}
	if page, err := svc.Audit(ctx, in); err == nil || page.Items != nil || readCalls != 0 {
		t.Fatal("missing callbacks released data")
	}
	svc.AuditAccess = port.AuditAccess{Authorize: func(context.Context) error { return problem.Forbidden }, RecordRead: func(context.Context, port.AuditReadReceipt) error { recordCalls++; return nil }}
	if _, err := svc.Audit(ctx, in); !errors.Is(err, problem.Forbidden) || readCalls != 0 {
		t.Fatal("owner denial fell back", err)
	}
	svc.AuditAccess.Authorize = func(context.Context) error { return nil }
	svc.AuditAccess.RecordRead = func(context.Context, port.AuditReadReceipt) error {
		recordCalls++
		return errors.New("durable sink down")
	}
	if page, err := svc.Audit(ctx, in); err == nil || page.Items != nil || readCalls != 1 || recordCalls != 1 {
		t.Fatal("failed recorder released page")
	}
	unverified := p
	unverified.MFA = false
	if _, err := svc.Audit(authz.WithPrincipal(context.Background(), unverified), in); !errors.Is(err, problem.MFARequired) || readCalls != 1 {
		t.Fatal("MFA bypass", err)
	}
	if _, err := svc.Audit(authz.WithPrincipal(tenant.WithID(context.Background(), ids.New()), p), in); !errors.Is(err, problem.Forbidden) {
		t.Fatal("tenant context bypass", err)
	}
	badSources := []port.AuditSource{
		{Key: src.Key, Module: src.Module, Read: func(context.Context, uuid.UUID, port.AuditReadQuery) ([]port.AuditRecord, error) {
			return nil, errors.New("source failed")
		}},
		{Key: src.Key, Module: src.Module, Read: func(context.Context, uuid.UUID, port.AuditReadQuery) ([]port.AuditRecord, error) {
			r := auditTestRow(now)
			r.Action = "person@example.test"
			return []port.AuditRecord{r}, nil
		}},
		{Key: src.Key, Module: src.Module, Read: func(context.Context, uuid.UUID, port.AuditReadQuery) ([]port.AuditRecord, error) {
			return []port.AuditRecord{auditTestRow(now.Add(-2 * time.Minute)), auditTestRow(now.Add(-time.Minute))}, nil
		}},
	}
	for i, source := range badSources {
		svc.AuditRegistry, _ = port.NewAuditRegistry(source)
		before := recordCalls
		if page, err := svc.Audit(ctx, in); err == nil || page.Items != nil || recordCalls != before {
			t.Fatalf("bad source%d released or recorded partial data", i)
		}
	}
	invalid := in
	invalid.To = invalid.From.Add(MaxAuditWindow + time.Second)
	if _, err := svc.Audit(ctx, invalid); err == nil || problem.From(err).Code != problem.Validation.Code {
		t.Fatal("unbounded window", err)
	}
}
