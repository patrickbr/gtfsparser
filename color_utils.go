package gtfsparser

import (
	"fmt"
	"log"
	"math"
	"strconv"
)

// Converts a hex color (e.g., "AABBCC") to normalized RGB (0..1)
func hexToRGB(hex string) (r, g, b float64, err error) {
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("invalid hex color: %s", hex)
	}
	parse := func(s string) (float64, error) {
		val, err := strconv.ParseInt(s, 16, 64)
		if err != nil {
			return 0, err
		}
		return float64(val) / 255.0, nil
	}

	r, err = parse(hex[0:2])
	if err != nil {
		return
	}
	g, err = parse(hex[2:4])
	if err != nil {
		return
	}
	b, err = parse(hex[4:6])
	if err != nil {
		return
	}
	return
}

// Computes relative luminance using WCAG formula
func relativeLuminance(r, g, b float64) float64 {
	convert := func(c float64) float64 {
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*convert(r) + 0.7152*convert(g) + 0.0722*convert(b)
}

// Computes contrast ratio between two luminances
func contrastRatio(l1, l2 float64) float64 {
	light := math.Max(l1, l2)
	dark := math.Min(l1, l2)
	return (light + 0.05) / (dark + 0.05)
}

// Checks and prints WCAG contrast evaluation
// See https://www.w3.org/WAI/WCAG21/Understanding/contrast-minimum.html
// "The visual presentation of text and images of text has a contrast ratio of at least 4.5:1""

func hasValidContrast(fgHex, bgHex string) bool {
	fgR, fgG, fgB, err := hexToRGB(fgHex)
	if err != nil {
		log.Fatalf("Invalid foreground color: %v", err)
	}
	bgR, bgG, bgB, err := hexToRGB(bgHex)
	if err != nil {
		log.Fatalf("Invalid background color: %v", err)
	}

	fgLum := relativeLuminance(fgR, fgG, fgB)
	bgLum := relativeLuminance(bgR, bgG, bgB)
	ratio := contrastRatio(fgLum, bgLum)

	return ratio >= 4.5
}
