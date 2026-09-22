package pipeline

import "math"

const nanValue = math.NaN()

func expmath(x float64) float64 { return math.Exp(x) }
func isNaN(x float64) bool     { return math.IsNaN(x) }
func fround(x, digits float64) float64 {
	m := math.Pow(10, digits)
	return math.Round(x*m) / m
}
