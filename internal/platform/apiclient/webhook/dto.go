package webhook

import (
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
)

func rfc(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// DTO là một webhook trong phản hồi (Public và Admin API) — không bao giờ có bí mật hay nonce.
func DTO(v View) map[string]any {
	b := v.Breaker
	var lastErr any
	if b.LastError != "" {
		lastErr = b.LastError
	}
	cursors := make([]map[string]any, 0, len(v.Cursors))
	for _, c := range v.Cursors {
		var nc any
		if c.NotifiedSeq > 0 {
			nc = domain.EncodeCursor(c.NotifiedSeq)
		}
		cursors = append(cursors, map[string]any{"feed": c.Feed, "notified_cursor": nc, "pending_attempts": c.Attempts,
			"next_attempt_at": rfc(c.NextAttemptAt), "last_attempt_at": rfc(c.LastAttemptAt)})
	}
	out := map[string]any{"id": v.ID.String(), "url": v.URL, "feeds": v.Feeds, "status": v.Status, "failure_threshold": v.Threshold,
		"secret_version": v.SecretVersion, "previous_secret_valid_until": rfc(v.PrevValidUntil), "created_at": v.CreatedAt.UTC().Format(time.RFC3339Nano),
		"breaker": map[string]any{"state": b.State, "consecutive_failures": b.Failures, "opened_at": rfc(b.OpenedAt), "next_probe_at": rfc(b.NextProbeAt),
			"last_success_at": rfc(b.LastSuccess), "last_failure_at": rfc(b.LastFailure), "last_error": lastErr},
		"cursors": cursors}
	if v.StatusReason != nil {
		out["status_reason"] = *v.StatusReason
	}
	if v.Deliveries != nil {
		ds := make([]map[string]any, 0, len(v.Deliveries))
		for _, d := range v.Deliveries {
			var cls any
			if d.ErrorClass != "" {
				cls = d.ErrorClass
			}
			ds = append(ds, map[string]any{"id": d.ID.String(), "feed": d.Feed, "cursor": domain.EncodeCursor(d.CursorSeq), "attempt": d.Attempt,
				"probe": d.Probe, "outcome": d.Outcome, "http_status": d.HTTPStatus, "error_class": cls, "breaker_state": d.BreakerState,
				"at": d.At.UTC().Format(time.RFC3339Nano)})
		}
		out["recent_deliveries"] = ds
	}
	return out
}
