package costing

var Price float64 = 1.5 // want "R-10"

func Half(n int) int { return n / 2 }

func Ratio(a, b int) float32 { return float32(a) / float32(b) } // want "R-10"
