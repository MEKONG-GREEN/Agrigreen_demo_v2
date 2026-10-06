// worker-platform: gốc lắp ráp vùng nền tảng — không nối mkg/*.
package main

import (
	_ "example.com/ag/internal/mkg/gamma/port" // want "R-03"
	_ "example.com/ag/internal/platform/beta"
)

func main() {}
