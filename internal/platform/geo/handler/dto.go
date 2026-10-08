// Package handler: HTTP của pf/geo (contracts/openapi/application.yaml và admin.yaml,
// mục "# --- pf/geo ---").
package handler

import (
	"encoding/base64"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func refOrNil(u uuid.UUID) any {
	if u == uuid.Nil {
		return nil
	}
	return u.String()
}

func strOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func decOrNil(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.StringFixed(4)
}

func encodeCursor(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString(u[:])
}

func decodeCursor(c *gin.Context) (uuid.UUID, error) {
	var after uuid.UUID
	cur := c.Query("cursor")
	if cur == "" {
		return after, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil || len(b) != 16 {
		return after, problem.Validation.WithField("cursor", "không hợp lệ")
	}
	copy(after[:], b)
	return after, nil
}
