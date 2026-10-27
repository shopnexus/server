package domain

import (
	"math"
	"math/rand/v2"
)

// Point is a position in degrees.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// MaxGPSKm is how far a seller's GPS point may be from their commune's
// point. The communes table holds points, not boundaries, so this distance
// stands in for "inside the commune".
const MaxGPSKm = 30

const (
	earthKm = 6371.0
	// kmPerDegree is the length of a degree of latitude.
	kmPerDegree = 111.32
	// blurKm is the largest random offset Blur applies before rounding.
	blurKm = 0.5
)

// Distance is the great-circle distance between a and b in kilometres.
func Distance(a, b Point) float64 {
	la1, la2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dla, dlo := la2-la1, (b.Lng-a.Lng)*math.Pi/180
	h := math.Pow(math.Sin(dla/2), 2) + math.Cos(la1)*math.Cos(la2)*math.Pow(math.Sin(dlo/2), 2)
	return 2 * earthKm * math.Asin(math.Sqrt(h))
}

// Blur moves p by up to 500 m in a random direction, then rounds it to a
// 0.01° grid (about 1.1 km), so the stored point says which cell the seller
// is near and no more.
func Blur(p Point) Point {
	r := blurKm * math.Sqrt(rand.Float64()) //nolint:gosec // a blur offset, not a secret
	angle := 2 * math.Pi * rand.Float64()   //nolint:gosec // a blur offset, not a secret
	lat := p.Lat + r*math.Cos(angle)/kmPerDegree
	lng := p.Lng + r*math.Sin(angle)/(kmPerDegree*math.Cos(p.Lat*math.Pi/180))
	return Point{Lat: math.Round(lat*100) / 100, Lng: math.Round(lng*100) / 100}
}
