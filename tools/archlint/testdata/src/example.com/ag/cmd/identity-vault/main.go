// Gốc lắp ráp của vùng vault: được import gốc module vault và client, không import lớp trong,
// không nối platform/* hay mkg/* (mục 4, RB-3).
package main

import (
	_ "example.com/ag/internal/mkg/gamma/port"     // want "R-03"
	_ "example.com/ag/internal/platform/beta/port" // want "R-03"
	_ "example.com/ag/internal/vault/identity"
	_ "example.com/ag/internal/vault/identity/client"
	_ "example.com/ag/internal/vault/identity/store" // want "R-02"
)

func main() {}
