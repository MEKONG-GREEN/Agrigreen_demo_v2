package app

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

func TestRegisteredSourceBuilderBindsJobAndCopies(t *testing.T) {
	tenant := uuid.New()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	job := domain.Job{TenantID: tenant, RequestedAt: at, ScopeUnits: []string{"89"}, Def: domain.Definition{Source: "example.measure", Title: "Example report", Columns: []domain.Column{{Key: "amount", Title: "Quantity"}}}, Params: domain.Params{Level: "province", UnitCode: "89", Period: "2026-09"}}
	value := decimal.RequireFromString("7.125")
	data := port.SourceData{Rows: []port.SourceRow{{"amount": {Dec: &value}}}, Total: port.SourceRow{"amount": {Dec: &value}}, Notes: []string{"source note"}, Provisional: true}
	registrations := []port.SourceBuilderRegistration{{Source: "example.measure", Build: func(_ context.Context, q port.SourceBuildQuery) (port.SourceData, error) {
		if q.TenantID != tenant || !q.At.Equal(at) || q.Source != job.Def.Source || q.Unit != "89" || len(q.Periods) != 1 || q.Periods[0] != "2026-09" || q.Scope.Units[0] != "89" {
			t.Fatalf("job identity/scope/query changed: %+v", q)
		}
		q.Scope.Units[0] = "escaped"
		q.Columns[0].Key = "escaped"
		return data, nil
	}}}
	registry := NewSourceBuilderRegistry(registrations...)
	registrations[0].Build = nil
	svc := Service{Builders: registry, Catalog: NewCatalogRegistry(testCatalogRegistration())}
	got, err := svc.buildDataset(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if job.ScopeUnits[0] != "89" || job.Def.Columns[0].Key != "amount" || !got.Provisional || got.Rows[0][0].Dec.String() != "7.125" {
		t.Fatalf("source projection or aliases: %+v", got)
	}
	value = decimal.Zero
	data.Rows[0]["amount"] = port.SourceCell{Text: "mutated"}
	data.Notes[0] = "mutated"
	if got.Rows[0][0].Dec.String() != "7.125" || got.Notes[0] != "source note" {
		t.Fatal("result aliases escaped")
	}
}
func TestRegisteredSourceBuilderErrorIsFinal(t *testing.T) {
	sentinel := errors.New("source failed")
	svc := Service{Catalog: NewCatalogRegistry(testCatalogRegistration()), Builders: NewSourceBuilderRegistry(port.SourceBuilderRegistration{Source: "example.measure", Build: func(context.Context, port.SourceBuildQuery) (port.SourceData, error) {
		return port.SourceData{}, sentinel
	}})}
	if _, err := svc.buildDataset(context.Background(), domain.Job{TenantID: uuid.New(), Def: domain.Definition{Source: "example.measure"}}); !errors.Is(err, sentinel) {
		t.Fatalf("source error fell back: %v", err)
	}
}
func TestSourceBuilderRegistryRejectsInvalid(t *testing.T) {
	for _, registrations := range [][]port.SourceBuilderRegistration{
		{{Source: "missing"}},
		{{Source: "duplicate", Build: func(context.Context, port.SourceBuildQuery) (port.SourceData, error) { return port.SourceData{}, nil }}, {Source: "duplicate", Build: func(context.Context, port.SourceBuildQuery) (port.SourceData, error) { return port.SourceData{}, nil }}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid/duplicate builder accepted")
				}
			}()
			NewSourceBuilderRegistry(registrations...)
		}()
	}
}
