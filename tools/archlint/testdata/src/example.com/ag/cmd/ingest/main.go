package main

import (
	_ "example.com/ag/internal/mkg/measurement/port"
	_ "example.com/ag/internal/mkg/measurement/port/edgewire"
	_ "example.com/ag/internal/platform/beta" // want "R-03"
	_ "example.com/ag/internal/shared/db"
)

func main() {}
