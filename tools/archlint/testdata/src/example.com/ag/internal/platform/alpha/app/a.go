package app

import (
	_ "example.com/ag/internal/mkg/gamma/port"         // want "R-03"
	_ "example.com/ag/internal/platform/alpha/domain"
	_ "example.com/ag/internal/platform/beta/domain"   // want "R-02"
	_ "example.com/ag/internal/platform/beta/port"
	_ "example.com/ag/internal/shared/x"
	_ "example.com/ag/internal/vault/identity/client"
	_ "example.com/ag/internal/vault/identity/store"   // want "R-04"
)
