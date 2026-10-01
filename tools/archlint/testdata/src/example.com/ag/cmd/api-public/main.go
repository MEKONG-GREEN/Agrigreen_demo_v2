package main

import (
	_ "example.com/ag/internal/mkg/gamma/port" // want "R-03"
	_ "example.com/ag/internal/platform/beta"
	_ "example.com/ag/internal/platform/beta/port"
	_ "example.com/ag/internal/vault/identity/client" // want "RB-3"
)

func main() {}
