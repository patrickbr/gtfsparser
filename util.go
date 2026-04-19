package gtfsparser

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

func isValidId(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // Earth radius in km
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	lat1r := lat1 * math.Pi / 180.0
	lat2r := lat2 * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(lat1r)*math.Cos(lat2r)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func warnNearOriginOrPole(lat float64, lon float64, context string) error {
	if math.Abs(lat) < 0.001 && math.Abs(lon) < 0.001 {
		return fmt.Errorf("point_near_origin: %s point is too close to origin (0, 0)", context)
	} else if math.Abs(lat) > 89.999 {
		return fmt.Errorf("point_near_pole: %s point is too close to the North or South Pole", context)
	}
	return nil
}

func containsAsWord(long, short string) bool {
	lowerLong := []rune(strings.ToLower(long))
	lowerShort := []rune(strings.ToLower(short))

	if len(lowerShort) == 0 {
		return false
	}

	for i := 0; i <= len(lowerLong)-len(lowerShort); i++ {
		match := true
		for j := 0; j < len(lowerShort); j++ {
			if lowerLong[i+j] != lowerShort[j] {
				match = false
				break
			}
		}
		if !match {
			continue
		}

		before := i == 0 || (!unicode.IsLetter(lowerLong[i-1]) && !unicode.IsDigit(lowerLong[i-1]))
		end := i + len(lowerShort)
		after := end == len(lowerLong) || (!unicode.IsLetter(lowerLong[end]) && !unicode.IsDigit(lowerLong[end]))

		if before && after {
			return true
		}
	}
	return false
}
