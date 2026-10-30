package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
)

func TestPinnedDefinitionNeverRebindsOrFallsBack(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	d := Seed("mau_1")
	raw, hash, err := d.BytesHash()
	if err != nil {
		t.Fatal(err)
	}
	original := port.KHTCTemplateSnapshot{Mode: "published", Rule: RuleVersion, ID: uuid.New(), Key: d.Key, Version: 1, Hash: hash, Definition: raw, ApplicableFrom: start}
	if got, err := PinnedDefinition(original, d.Key, start); err != nil || got.Key != d.Key {
		t.Fatalf("valid pin: %v", err)
	}
	for name, change := range map[string]func(*port.KHTCTemplateSnapshot){
		"mode":     func(p *port.KHTCTemplateSnapshot) { p.Mode = "" },
		"rule":     func(p *port.KHTCTemplateSnapshot) { p.Rule = "future-v2" },
		"version":  func(p *port.KHTCTemplateSnapshot) { p.Version = 0 },
		"identity": func(p *port.KHTCTemplateSnapshot) { p.ID = uuid.Nil },
		"hash":     func(p *port.KHTCTemplateSnapshot) { p.Hash = "tampered" },
		"key":      func(p *port.KHTCTemplateSnapshot) { p.Key = "mau_2" },
		"future":   func(p *port.KHTCTemplateSnapshot) { p.ApplicableFrom = start.AddDate(0, 1, 0) },
		"extra": func(p *port.KHTCTemplateSnapshot) {
			p.Definition = append(append(json.RawMessage{}, p.Definition...), []byte(" {}")...)
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := original
			change(&bad)
			if _, err := PinnedDefinition(bad, d.Key, start); err == nil {
				t.Fatal("bad pin accepted; no fallback is allowed")
			}
		})
	}
	// Input is an immutable copy: a later caller's edits cannot change this revision.
	first, err := PinnedDefinition(original, d.Key, start)
	if err != nil {
		t.Fatal(err)
	}
	first.Nodes[0].Title = "changed"
	second, err := PinnedDefinition(original, d.Key, start)
	if err != nil || second.Nodes[0].Title == "changed" {
		t.Fatal("shared snapshot mutation", err)
	}
}
