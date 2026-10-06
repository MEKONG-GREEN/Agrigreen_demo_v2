// Diễn tập phục hồi cụm pf: gốc lắp ráp vùng nền tảng — không nối mkg/*.
package main

import (
	_ "example.com/ag/internal/mkg/gamma" // want "R-03"
	_ "example.com/ag/internal/platform/beta/port"
)

func main() {}
