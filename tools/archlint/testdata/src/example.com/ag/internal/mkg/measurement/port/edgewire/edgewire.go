package edgewire

import (
	_ "crypto/ed25519"
	_ "encoding/json"

	_ "example.com/ag/internal/shared/clock" // want "ADR-0019"
	_ "example.org/pgx"                      // want "ADR-0019"
)
