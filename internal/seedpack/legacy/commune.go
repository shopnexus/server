package legacy

import (
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/shopnexus/server/migrations"
)

// Commune is one of the 2025 list, at its point.
type Commune struct {
	Code, Province, Name string
	Lat, Lng             float64
}

var communeRow = regexp.MustCompile(`\('(\d+)', '(\d+)', '([^']+)', ([\d.]+), ([\d.]+)\)`)

// Communes reads the commune list from the regions migration, the same rows
// the server's communes table holds.
func Communes() ([]Commune, error) {
	raw, err := migrations.FS.ReadFile("0005_regions.sql")
	if err != nil {
		return nil, fmt.Errorf("read the regions migration: %w", err)
	}
	var out []Commune
	for _, m := range communeRow.FindAllStringSubmatch(string(raw), -1) {
		lat, _ := strconv.ParseFloat(m[4], 64)
		lng, _ := strconv.ParseFloat(m[5], 64)
		out = append(out, Commune{Code: m[1], Province: m[2], Name: m[3], Lat: lat, Lng: lng})
	}
	if len(out) < 3000 {
		return nil, fmt.Errorf("the regions migration has %d communes; its format changed", len(out))
	}
	return out, nil
}

// Nearest returns the commune whose point is closest to lat, lng.
func Nearest(cs []Commune, lat, lng float64) Commune {
	best, bestD := cs[0], math.Inf(1)
	k := math.Cos(lat * math.Pi / 180)
	for _, c := range cs {
		dy, dx := c.Lat-lat, (c.Lng-lng)*k
		if d := dy*dy + dx*dx; d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

// provinceWeight is roughly where second-hand goods change hands: the two
// big cities first, then the other cities; every other province shares
// the rest by its commune count.
var provinceWeight = map[string]float64{
	"79": 34, "01": 24, "48": 4, "31": 3.5, "75": 3.5, "92": 2.5, "24": 2, "22": 1.5, "56": 1.5, "46": 1.2,
}

const otherProvinces = 22.3

// Picker draws a person's commune: a province by weight, then a commune in
// it, a ward (phường) four times in five in a city province.
type Picker struct {
	byProvince map[string][]Commune
	provinces  []string
	weights    []float64
}

// NewPicker groups cs by province.
func NewPicker(cs []Commune) *Picker {
	p := &Picker{byProvince: map[string][]Commune{}}
	for _, c := range cs {
		p.byProvince[c.Province] = append(p.byProvince[c.Province], c)
	}
	rest := 0
	for code, list := range p.byProvince {
		if _, ok := provinceWeight[code]; !ok {
			rest += len(list)
		}
	}
	p.provinces = slices.Sorted(maps.Keys(p.byProvince))
	for _, code := range p.provinces {
		list := p.byProvince[code]
		w, ok := provinceWeight[code]
		if !ok {
			w = otherProvinces * float64(len(list)) / float64(rest)
		}
		p.weights = append(p.weights, w)
	}
	return p
}

// Pick draws with u1 for the province, u2 for ward or not, u3 for the
// commune, each in [0, 1).
func (p *Picker) Pick(u1, u2, u3 float64) Commune {
	total := 0.0
	for _, w := range p.weights {
		total += w
	}
	at, code := u1*total, p.provinces[len(p.provinces)-1]
	for i, w := range p.weights {
		if at < w {
			code = p.provinces[i]
			break
		}
		at -= w
	}
	list := p.byProvince[code]
	if _, city := provinceWeight[code]; city && u2 < 0.8 {
		var wards []Commune
		for _, c := range list {
			if strings.HasPrefix(c.Name, "Phường ") {
				wards = append(wards, c)
			}
		}
		if len(wards) > 0 {
			list = wards
		}
	}
	return list[int(u3*float64(len(list)))]
}
