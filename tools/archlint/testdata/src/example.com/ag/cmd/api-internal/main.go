package main

import (
	_ "example.com/ag/internal/mkg/gamma"
	_ "example.com/ag/internal/mkg/gamma/port"
	_ "example.com/ag/internal/platform/beta"      // want "R-03"
	_ "example.com/ag/internal/platform/beta/port" // want "R-03"
)

func main() {}
