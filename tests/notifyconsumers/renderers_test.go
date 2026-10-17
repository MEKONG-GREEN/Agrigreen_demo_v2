package notifyconsumers_test

import (
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestCNTSOwnedRenderersMatchLegacy(t *testing.T) {
	var inputs []struct {
		Name, Event, Payload string
		At                   time.Time
	}
	var expected []struct {
		Name   string
		Drafts []notifyport.NotificationDraft
		Error  string
	}
	for path, target := range map[string]any{"../golden/kn-notify-cnts-inputs.json": &inputs, "../golden/kn-notify-cnts.json": &expected} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	if len(inputs) != 46 || len(expected) != len(inputs) {
		t.Fatal("missing legacy vectors")
	}
	renderers := map[string]notifyport.EventRenderer{}
	for _, r := range extension.NotificationRenderers() {
		for _, event := range r.Events {
			if renderers[event] != nil {
				t.Fatal("duplicate renderer")
			}
			renderers[event] = r.Render
		}
	}
	if len(renderers) != 3 {
		t.Fatalf("CNTS events: %d", len(renderers))
	}
	for i, in := range inputs {
		t.Run(in.Name, func(t *testing.T) {
			renderer := renderers[in.Event]
			var got []notifyport.NotificationDraft
			var err error
			if renderer != nil {
				got, err = renderer(in.Event, json.RawMessage(in.Payload), in.At)
			}
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			want := expected[i]
			if want.Name != in.Name || !reflect.DeepEqual(got, want.Drafts) || errText != want.Error {
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(want.Drafts)
				t.Fatalf("legacy mismatch %s: got %s / %q; want %s / %q", in.Name, a, errText, b, want.Error)
			}
		})
	}
}
