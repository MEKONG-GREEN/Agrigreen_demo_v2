package domain

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func frozenJob() Job {
	j := Job{ID: uuid.New(), TenantID: uuid.New(), RequestedBy: uuid.New(), RequestedAt: time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC),
		TemplateKey: "example.layout", Format: "xlsx", Params: Params{Level: "province"}, Def: Definition{Source: "example.measure"}}
	j.SourceArgs = &FrozenSourceArgs{Source: j.Def.Source, Version: 1, JobID: j.ID, TenantID: j.TenantID, RequestedBy: j.RequestedBy,
		RequestedAt: j.RequestedAt, TemplateKey: j.TemplateKey, TemplateVersion: j.TemplateVersion, Format: j.Format, Args: json.RawMessage(`{"as_of":"2026-10-21T03:00:00Z","filter":"private-marker"}`)}
	return j
}
func TestSourceArgsCodecNativeAndPrivate(t *testing.T) {
	j := frozenJob()
	native := j
	native.SourceArgs = nil
	want, _ := json.Marshal(native.Params)
	got, err := MarshalJobParams(native)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("native bytes changed: %s %v", got, err)
	}
	encoded, err := MarshalJobParams(j)
	if err != nil {
		t.Fatal(err)
	}
	restored := j
	restored.SourceArgs = nil
	restored.Params = Params{}
	if err = UnmarshalJobParams(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Params != j.Params || !bytes.Equal(restored.SourceArgs.Args, j.SourceArgs.Args) {
		t.Fatal("private roundtrip changed")
	}
	public, _ := json.Marshal(j)
	if bytes.Contains(public, []byte("private-marker")) || bytes.Contains(public, []byte("_reports_source")) {
		t.Fatalf("public Job leaked private args: %s", public)
	}
	restored.SourceArgs.Args[2] = 'X'
	if !bytes.Contains(j.SourceArgs.Args, []byte("filter")) {
		t.Fatal("codec aliased input")
	}
	// The private envelope is bound to every persisted identity/snapshot field.
	for _, mutate := range []func(*Job){
		func(j *Job) { j.ID = uuid.New() }, func(j *Job) { j.TenantID = uuid.New() }, func(j *Job) { j.RequestedBy = uuid.New() },
		func(j *Job) { j.RequestedAt = j.RequestedAt.Add(time.Second) }, func(j *Job) { j.Def.Source = "other" },
		func(j *Job) { j.TemplateKey = "other" }, func(j *Job) { j.TemplateVersion++ }, func(j *Job) { j.Format = "pdf" },
	} {
		tampered := j
		mutate(&tampered)
		if err = UnmarshalJobParams(encoded, &tampered); err == nil {
			t.Fatal("moved envelope accepted")
		}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &fields)
	var env map[string]json.RawMessage
	_ = json.Unmarshal(fields["_reports_source"], &env)
	env["unexpected"] = json.RawMessage("true")
	fields["_reports_source"], _ = json.Marshal(env)
	bad, _ := json.Marshal(fields)
	if err = UnmarshalJobParams(bad, &j); err == nil {
		t.Fatal("unknown envelope field accepted")
	}
}
func TestSourceArgsRejectMalformedAndCanonicalize(t *testing.T) {
	for _, raw := range []string{
		"", "null", "[]", `{"a":1,"a":2}`, `{"nested":{"a":1,"a":2}}`, `{"_reports_source":{}}`,
		`{"nested":[{"_reports_source":{}}]}`, `{} {}`, `{"n":NaN}`, `{"s":"` + strings.Repeat("x", MaxSourceArgsBytes) + `"}`,
		`{"a":` + strings.Repeat("[", 18) + "0" + strings.Repeat("]", 18) + "}",
	} {
		if _, err := CanonicalSourceArgs([]byte(raw)); err == nil {
			t.Fatalf("malformed args accepted (len %d)", len(raw))
		}
	}
	if _, err := CanonicalSourceArgs([]byte{'{', '"', 'x', '"', ':', '"', 255, '"', '}'}); err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
	a, err := CanonicalSourceArgs([]byte(`{"z":2,"a":{"y":3,"x":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != `{"a":{"x":1,"y":3},"z":2}` {
		t.Fatalf("not canonical: %s", a)
	}
}
