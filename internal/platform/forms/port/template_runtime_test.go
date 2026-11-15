package port

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeDefinitionBoundsAndDetachedJSON(t *testing.T) {
	in := &RuntimeDefinition{Schema: RuntimeDefinitionV1, Contract: "sample.runtime/v1", Config: json.RawMessage(`{"frequency":"daily","gps":true,"photo":false,"limit":123.456,"groups":["one","two"]}`)}
	got, err := NormalizeRuntime(in)
	if err != nil {
		t.Fatal(err)
	}
	original := string(got.Config)
	in.Config[2] = 'x'
	if string(got.Config) != original {
		t.Fatal("normalized config aliases caller memory")
	}
	cases := []string{`null`, `[]`, `{"x":1,"x":2}`, `{"x":` + strings.Repeat("[", 18) + `1` + strings.Repeat("]", 18) + `}`, `{"x":"` + strings.Repeat("s", 4097) + `"}`, `{"x":` + strings.Repeat("9", 70000) + `}`}
	for _, number := range []string{"1e100000", "1e-100000", strings.Repeat("9", 20000), "1234567890123456789", "0.1234567"} {
		cases = append(cases, `{"number":`+number+`}`)
	}
	for _, raw := range cases {
		t.Run(raw[:min(len(raw), 40)], func(t *testing.T) {
			if _, err := NormalizeRuntime(&RuntimeDefinition{Schema: RuntimeDefinitionV1, Contract: "sample.runtime/v1", Config: json.RawMessage(raw)}); err == nil {
				t.Fatal("invalid runtime accepted")
			}
		})
	}
	for _, schema := range []string{"", "forms.runtime/v2"} {
		if _, err := NormalizeRuntime(&RuntimeDefinition{Schema: schema, Contract: "sample.runtime/v1", Config: json.RawMessage(`{}`)}); err == nil {
			t.Fatal("unsupported envelope accepted")
		}
	}
	for _, raw := range []string{`{"schema":"forms.runtime/v1","contract":"sample.runtime/v1","config":{},"validated":true}`, `{"schema":"forms.runtime/v1","contract":"sample.runtime/v1","config":{}} {}`} {
		var out RuntimeDefinition
		if json.Unmarshal([]byte(raw), &out) == nil {
			t.Fatal("unknown/trailing metadata discarded")
		}
	}
}
