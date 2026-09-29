package main

import (
	_ "crypto/ed25519"
	_ "fmt"

	_ "example.com/ag/internal/mkg/measurement/port/edgewire"
	_ "example.com/ag/internal/shared/clock"

	_ "example.com/ag/internal/mkg/measurement/port" // want "ADR-0019"
	_ "example.com/ag/internal/platform/beta/port"   // want "ADR-0019"
	_ "example.com/ag/internal/shared/db"            // want "ADR-0019"
	_ "example.org/pgx"                              // want "ADR-0019"
)

func main() {}
