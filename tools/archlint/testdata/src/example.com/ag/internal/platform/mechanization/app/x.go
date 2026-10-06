package app

import "time"

// Diện tích, giờ máy: decimal — không float trong mã không phải kiểm thử.
var Area float64 // want "R-10"

func Days(from, to time.Time) int { return int(to.Sub(from).Hours() / 24) } // want "R-10"
