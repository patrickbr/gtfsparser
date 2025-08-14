package gtfsparser

import (
	"math"
	"testing"
)

func TestHasValidContrast(t *testing.T) {
	tests := []struct {
		name     string
		fg       string
		bg       string
		expected bool
	}{
		{
			name:     "High contrast: black on white",
			fg:       "000000",
			bg:       "FFFFFF",
			expected: true,
		},
		{
			name:     "High contrast: white on black",
			fg:       "FFFFFF",
			bg:       "000000",
			expected: true,
		},
		{
			name:     "Low contrast: grey on slightly lighter grey",
			fg:       "777777",
			bg:       "AAAAAA",
			expected: false,
		},
		{
			name:     "Invalid contrast: blue on red",
			fg:       "0000FF",
			bg:       "FF0000",
			expected: false,
		},
		{
			name:     "Edge case: same color",
			fg:       "123456",
			bg:       "123456",
			expected: false,
		},
		{
			name:     "Pass: yellow on black",
			fg:       "FFFF00",
			bg:       "000000",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasValidContrast(tt.fg, tt.bg)
			if result != tt.expected {
				t.Errorf("hasValidContrast(%q, %q) = %v; want %v", tt.fg, tt.bg, result, tt.expected)
			}
		})
	}
}

func almostEqual(a, b float64) bool {
	const epsilon = 1e-6
	return math.Abs(a-b) < epsilon
}

func TestHexToRGB(t *testing.T) {
	tests := []struct {
		hex      string
		expected [3]float64
		wantErr  bool
	}{
		{"FFFFFF", [3]float64{1.0, 1.0, 1.0}, false},
		{"000000", [3]float64{0.0, 0.0, 0.0}, false},
		{"FF0000", [3]float64{1.0, 0.0, 0.0}, false},
		{"00FF00", [3]float64{0.0, 1.0, 0.0}, false},
		{"0000FF", [3]float64{0.0, 0.0, 1.0}, false},
		{"123456", [3]float64{0x12 / 255.0, 0x34 / 255.0, 0x56 / 255.0}, false},
		{"ZZZZZZ", [3]float64{}, true},
		{"ABC", [3]float64{}, true},
	}

	for _, tt := range tests {
		r, g, b, err := hexToRGB(tt.hex)
		if tt.wantErr && err == nil {
			t.Errorf("hexToRGB(%q) expected error, got none", tt.hex)
		}
		if !tt.wantErr {
			if err != nil {
				t.Errorf("hexToRGB(%q) unexpected error: %v", tt.hex, err)
			} else if !(almostEqual(r, tt.expected[0]) && almostEqual(g, tt.expected[1]) && almostEqual(b, tt.expected[2])) {
				t.Errorf("hexToRGB(%q) = (%f, %f, %f); want (%f, %f, %f)", tt.hex, r, g, b, tt.expected[0], tt.expected[1], tt.expected[2])
			}
		}
	}
}

func TestRelativeLuminance(t *testing.T) {
	tests := []struct {
		r, g, b float64
		want    float64
	}{
		{1.0, 1.0, 1.0, 1.0},      // white
		{0.0, 0.0, 0.0, 0.0},      // black
		{1.0, 0.0, 0.0, 0.2126},   // red
		{0.0, 1.0, 0.0, 0.7152},   // green
		{0.0, 0.0, 1.0, 0.0722},   // blue
		{0.5, 0.5, 0.5, 0.214041}, // mid-gray approx
	}

	for _, tt := range tests {
		got := relativeLuminance(tt.r, tt.g, tt.b)
		if !almostEqual(got, tt.want) {
			t.Errorf("relativeLuminance(%f, %f, %f) = %f; want %f", tt.r, tt.g, tt.b, got, tt.want)
		}
	}
}

func TestContrastRatio(t *testing.T) {
	tests := []struct {
		l1, l2 float64
		want   float64
	}{
		{1.0, 0.0, 21.0}, // max contrast
		{0.5, 0.5, 1.0},  // no contrast
		{0.8, 0.2, (0.8 + 0.05) / (0.2 + 0.05)},
		{0.7, 0.3, (0.7 + 0.05) / (0.3 + 0.05)},
	}

	for _, tt := range tests {
		got := contrastRatio(tt.l1, tt.l2)
		if !almostEqual(got, tt.want) {
			t.Errorf("contrastRatio(%f, %f) = %f; want %f", tt.l1, tt.l2, got, tt.want)
		}
	}
}
