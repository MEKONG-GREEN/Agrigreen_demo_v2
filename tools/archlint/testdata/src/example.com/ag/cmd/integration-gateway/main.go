package main

// W5.2 (ADR-0033): cổng tích hợp — cùng luật với api-public (R-03, RB-3).
import (
	_ "example.com/ag/internal/mkg/gamma/port" // want "R-03"
	_ "example.com/ag/internal/platform/beta"
	_ "example.com/ag/internal/vault/identity/client" // want "RB-3"
)

func main() {}
