package main

import (
	_ "example.com/ag/internal/platform/beta"
	_ "example.com/ag/internal/platform/beta/app" // want "R-02"
	_ "example.com/ag/internal/platform/beta/port"
)

func main() {}
