package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/google/uuid"
	"time"
)

// Tenant is pinned by a trusted server/owner instance, never selected from the
// opaque token or a client header. No public/HRM assembly is installed in W10.18's first slice.
type Config struct {
	Enabled                                            bool
	Tenant                                             uuid.UUID
	Key                                                []byte `json:"-"`
	LinkTTL, OTPTTL, SessionTTL, Cooldown, RateWindow  time.Duration
	MaxOpens, MaxAttempts, MaxSends, MaxIP, MaxSubject int
}

func defaults(c Config) Config {
	if c.LinkTTL == 0 {
		c.LinkTTL = 24 * time.Hour
	}
	if c.OTPTTL == 0 {
		c.OTPTTL = 5 * time.Minute
	}
	if c.SessionTTL == 0 {
		c.SessionTTL = 15 * time.Minute
	}
	if c.Cooldown == 0 {
		c.Cooldown = time.Minute
	}
	if c.RateWindow == 0 {
		c.RateWindow = 15 * time.Minute
	}
	if c.MaxOpens == 0 {
		c.MaxOpens = 10
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 5
	}
	if c.MaxSends == 0 {
		c.MaxSends = 3
	}
	if c.MaxIP == 0 {
		c.MaxIP = 30
	}
	if c.MaxSubject == 0 {
		c.MaxSubject = 30
	}
	return c
}
func (c Config) validate() error {
	if !c.Enabled {
		return nil
	}
	zero := true
	for _, x := range c.Key {
		if x != 0 {
			zero = false
		}
	}
	if c.Tenant == uuid.Nil || len(c.Key) != 32 || zero || c.LinkTTL < time.Minute || c.LinkTTL > 24*time.Hour || c.OTPTTL < time.Minute || c.OTPTTL > 10*time.Minute || c.SessionTTL < time.Minute || c.SessionTTL > 30*time.Minute || c.Cooldown < 30*time.Second || c.Cooldown > 10*time.Minute || c.RateWindow < time.Minute || c.RateWindow > time.Hour || c.MaxOpens < 1 || c.MaxOpens > 10 || c.MaxAttempts < 1 || c.MaxAttempts > 5 || c.MaxSends < 1 || c.MaxSends > 5 || c.MaxIP < 1 || c.MaxIP > 100 || c.MaxSubject < 1 || c.MaxSubject > 100 {
		return port.ErrInvalid
	}
	return nil
}

func (c Config) String() string   { return "guestlink.Config{redacted}" }
func (c Config) GoString() string { return c.String() }
