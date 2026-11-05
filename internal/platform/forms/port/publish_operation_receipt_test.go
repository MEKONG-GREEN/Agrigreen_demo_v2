package port

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func validPublish224() PublishOperationRequest {
	zero := 0
	return PublishOperationRequest{OperationID: uuid.New(), TenantID: uuid.New(), Input: BatchInput{Profile: "fx", TemplateID: uuid.New(), UnitKind: UnitCommune, Units: []string{"X1", "X2"}, MaxSubmissions: &zero, Periods: []PeriodDef{{Label: "Kỳ A", Kind: "week", Key: "2026-W41", From: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), DueAt: time.Date(2026, 10, 14, 1, 2, 3, 123456789, time.UTC), Rule: "v1"}}}}
}
func TestPublish224CanonicalOwnedPrecisionAndBindings(t *testing.T) {
	in := validPublish224()
	original := in.Input.Periods[0].DueAt
	normalized, err := NormalizePublishOperation(in)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Input.Periods[0].DueAt.Nanosecond() != 123456000 || in.Input.Periods[0].DueAt != original {
		t.Fatal("precision or caller mutation")
	}
	normalized.Input.Units[0] = "changed"
	normalized.Input.Periods[0].Label = "changed"
	*normalized.Input.MaxSubmissions = 3
	if in.Input.Units[0] != "X1" || in.Input.Periods[0].Label != "Kỳ A" || *in.Input.MaxSubmissions != 0 {
		t.Fatal("mutable input alias")
	}
	first, err := PublishOperationDigest(in)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*PublishOperationRequest){
		"operation": func(r *PublishOperationRequest) { r.OperationID = uuid.New() }, "tenant": func(r *PublishOperationRequest) { r.TenantID = uuid.New() }, "profile": func(r *PublishOperationRequest) { r.Input.Profile = "other" }, "template": func(r *PublishOperationRequest) { r.Input.TemplateID = uuid.New() },
		"order": func(r *PublishOperationRequest) {
			r.Input.Units[0], r.Input.Units[1] = r.Input.Units[1], r.Input.Units[0]
		}, "maxNilVsZero": func(r *PublishOperationRequest) { r.Input.MaxSubmissions = nil }, "key": func(r *PublishOperationRequest) { r.Input.Periods[0].Key = "2026-W42" }, "rule": func(r *PublishOperationRequest) { r.Input.Periods[0].Rule = "v2" }, "label": func(r *PublishOperationRequest) { r.Input.Periods[0].Label = "Kỳ B" }, "due": func(r *PublishOperationRequest) { r.Input.Periods[0].DueAt = r.Input.Periods[0].DueAt.Add(time.Second) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			copy, err := NormalizePublishOperation(in)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&copy)
			next, err := PublishOperationDigest(copy)
			if err != nil || next == first {
				t.Fatalf("full command binding: err=%v equal=%t", err, next == first)
			}
		})
	}
	same, err := NormalizePublishOperation(in)
	if err != nil {
		t.Fatal(err)
	}
	same.Input.Periods[0].DueAt = same.Input.Periods[0].DueAt.In(time.FixedZone("offset", 7*3600))
	digest, err := PublishOperationDigest(same)
	if err != nil || digest != first {
		t.Fatal("same UTC instant differs")
	}
}
func TestPublish224BoundsBeforeHashAndUnavailable(t *testing.T) {
	cases := map[string]func(*PublishOperationRequest){
		"noOperation": func(r *PublishOperationRequest) { r.OperationID = uuid.Nil }, "noTenant": func(r *PublishOperationRequest) { r.TenantID = uuid.Nil }, "noTemplate": func(r *PublishOperationRequest) { r.Input.TemplateID = uuid.Nil }, "longLabel": func(r *PublishOperationRequest) { r.Input.Periods[0].Label = strings.Repeat("9", 100000) }, "nul": func(r *PublishOperationRequest) { r.Input.Periods[0].Rule = "v\x00x" }, "invalidUTF8": func(r *PublishOperationRequest) { r.Input.Profile = string([]byte{0xff}) }, "negativeMax": func(r *PublishOperationRequest) { n := -1; r.Input.MaxSubmissions = &n }, "hugeMax": func(r *PublishOperationRequest) { n := math.MaxInt; r.Input.MaxSubmissions = &n }, "duplicateUnits": func(r *PublishOperationRequest) { r.Input.Units = []string{"X1", "X1"} }, "duplicatePeriod": func(r *PublishOperationRequest) { r.Input.Periods = append(r.Input.Periods, r.Input.Periods[0]) }, "noPeriods": func(r *PublishOperationRequest) { r.Input.Periods = nil }, "tooMany": func(r *PublishOperationRequest) { r.Input.Periods = make([]PeriodDef, MaxPublishOperationPeriods+1) }, "parents": func(r *PublishOperationRequest) { r.Input.UnitParents = []string{"X1"} }, "hugeYear": func(r *PublishOperationRequest) {
			r.Input.Periods[0].From = time.Date(100000, 1, 1, 0, 0, 0, 0, time.UTC)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := validPublish224()
			mutate(&in)
			if _, err := PublishOperationDigest(in); err == nil {
				t.Fatal("invalid command accepted")
			}
		})
	}
	got, err := ExecutePublishOperation(context.Background(), nil, validPublish224())
	if !errors.Is(err, ErrPublishOperationUnavailable) || !reflect.DeepEqual(got, PublishOperationReceipt{Outcome: PublishOperationUnknown}) {
		t.Fatal("nil capability not opaque failclosed")
	}
}
