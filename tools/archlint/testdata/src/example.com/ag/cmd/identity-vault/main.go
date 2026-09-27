// Gốc lắp ráp của vùng vault: được import gốc module vault và client, không import lớp trong.
package main

import (
	_ "example.com/ag/internal/vault/identity"
	_ "example.com/ag/internal/vault/identity/client"
	_ "example.com/ag/internal/vault/identity/store" // want "R-02"
)

func main() {}
