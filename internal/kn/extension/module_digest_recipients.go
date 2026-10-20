package extension

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/mail"
	"slices"
	"strings"

	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/google/uuid"
)

// ErrDigestRecipientsConfig never includes configuration values or mailbox addresses.
var ErrDigestRecipientsConfig = errors.New("KN digest recipients: invalid startup configuration")

// ErrDigestRecipientsMissing leaves the tenant's pending digest queue untouched.
var ErrDigestRecipientsMissing = errors.New("KN digest recipients: tenant mapping missing")

// NewDigestRecipients freezes a server-managed tenant-to-service-mailbox mapping.
// Empty configuration is a deny resolver, never a request to use a global mailbox.
// A mailbox belongs to only one tenant; the same recipient cannot receive two Centers' queues.
func NewDigestRecipients(raw string) (notifyport.DigestRecipients, error) {
	entries := map[uuid.UUID][]string{}
	used := map[string]bool{}
	if strings.TrimSpace(raw) != "" {
		if len(raw) > 1<<20 {
			return nil, ErrDigestRecipientsConfig
		}
		dec := json.NewDecoder(strings.NewReader(raw))
		token, err := dec.Token()
		if err != nil || token != json.Delim('[') {
			return nil, ErrDigestRecipientsConfig
		}
		for dec.More() {
			token, err = dec.Token()
			if err != nil || token != json.Delim('{') {
				return nil, ErrDigestRecipientsConfig
			}
			var id string
			var to []string
			keys := map[string]bool{}
			for dec.More() {
				token, err = dec.Token()
				if err != nil {
					return nil, ErrDigestRecipientsConfig
				}
				key, ok := token.(string)
				if !ok || keys[key] {
					return nil, ErrDigestRecipientsConfig
				}
				keys[key] = true
				switch key {
				case "tenant_id":
					err = dec.Decode(&id)
				case "to":
					err = dec.Decode(&to)
				default:
					return nil, ErrDigestRecipientsConfig
				}
				if err != nil {
					return nil, ErrDigestRecipientsConfig
				}
			}
			token, err = dec.Token()
			if err != nil || token != json.Delim('}') {
				return nil, ErrDigestRecipientsConfig
			}
			tid, err := uuid.Parse(id)
			if err != nil || tid == uuid.Nil || tid.String() != strings.ToLower(id) || !keys["tenant_id"] || !keys["to"] || len(to) == 0 || len(to) > 50 || len(entries) >= 1024 {
				return nil, ErrDigestRecipientsConfig
			}
			if _, exists := entries[tid]; exists {
				return nil, ErrDigestRecipientsConfig
			}
			for _, addr := range to {
				parsed, err := mail.ParseAddress(addr)
				if err != nil || parsed.Name != "" || parsed.Address != addr || strings.TrimSpace(addr) != addr || strings.ContainsAny(addr, "\r\n") || len(addr) > 254 {
					return nil, ErrDigestRecipientsConfig
				}
				canonical := strings.ToLower(addr)
				if used[canonical] {
					return nil, ErrDigestRecipientsConfig
				}
				used[canonical] = true
			}
			entries[tid] = slices.Clone(to)
		}
		token, err = dec.Token()
		if err != nil || token != json.Delim(']') {
			return nil, ErrDigestRecipientsConfig
		}
		var trailing any
		if err := dec.Decode(&trailing); err != io.EOF {
			return nil, ErrDigestRecipientsConfig
		}
	}
	return func(ctx context.Context, tid uuid.UUID) ([]string, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		to, ok := entries[tid]
		if !ok || tid == uuid.Nil {
			return nil, ErrDigestRecipientsMissing
		}
		return slices.Clone(to), nil
	}, nil
}
