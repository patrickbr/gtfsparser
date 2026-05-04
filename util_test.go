package gtfsparser

import (
	"testing"
)

func TestIsValidId(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", true},
		{"plain ascii", "abc123", true},
		{"ascii with allowed symbols", "trip_01-A.B", true},
		{"ascii space", "trip 01", true},
		{"non-ascii latin", "café", false},
		{"chinese character", "路线1", false},
		{"null byte", "abc\x00def", false},
		{"tab character", "abc\tdef", false},
		{"newline", "abc\ndef", false},
		{"del character", "abc\x7fdef", false},
		{"tilde boundary (valid)", "abc~def", true},
		{"byte above MaxASCII", "abc\x80def", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidId(tt.input); got != tt.want {
				t.Errorf("isValidId(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHaversineKm(t *testing.T) {
	tests := []struct {
		name         string
		lat1, lon1   float64
		lat2, lon2   float64
		wantApproxKm float64
		toleranceKm  float64
	}{
		{
			// same point
			name: "same point",
			lat1: 48.8566, lon1: 2.3522,
			lat2: 48.8566, lon2: 2.3522,
			wantApproxKm: 0.0,
			toleranceKm:  0.001,
		},
		{
			// Paris to Berlin, well-known ~878 km
			name: "Paris to Berlin",
			lat1: 48.8566, lon1: 2.3522,
			lat2: 52.5200, lon2: 13.4050,
			wantApproxKm: 878.0,
			toleranceKm:  5.0,
		},
		{
			// New York to London, ~5570 km
			name: "New York to London",
			lat1: 40.7128, lon1: -74.0060,
			lat2: 51.5074, lon2: -0.1278,
			wantApproxKm: 5570.0,
			toleranceKm:  20.0,
		},
		{
			// symmetry: A->B == B->A
			name: "symmetry Berlin to Paris",
			lat1: 52.5200, lon1: 13.4050,
			lat2: 48.8566, lon2: 2.3522,
			wantApproxKm: 878.0,
			toleranceKm:  5.0,
		},
		{
			// antipodal points should be ~20015 km (half Earth circumference)
			name: "antipodal",
			lat1: 0.0, lon1: 0.0,
			lat2: 0.0, lon2: 180.0,
			wantApproxKm: 20015.0,
			toleranceKm:  10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := haversineKm(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			diff := got - tt.wantApproxKm
			if diff < 0 {
				diff = -diff
			}
			if diff > tt.toleranceKm {
				t.Errorf("haversineKm(%v,%v -> %v,%v) = %.2f km, want ~%.2f km (tolerance %.2f km)",
					tt.lat1, tt.lon1, tt.lat2, tt.lon2, got, tt.wantApproxKm, tt.toleranceKm)
			}
		})
	}
}

func TestWarnNearOriginOrPole(t *testing.T) {
	tests := []struct {
		name      string
		lat, lon  float64
		wantNil   bool
		wantInErr string
	}{
		// near origin
		{"exact origin", 0.0, 0.0, false, "point_near_origin"},
		{"within origin threshold", 0.0005, 0.0005, false, "point_near_origin"},
		{"just outside origin threshold lat", 0.002, 0.0, true, ""},
		{"just outside origin threshold lon", 0.0, 0.002, true, ""},
		{"both just outside origin threshold", 0.002, 0.002, true, ""},

		// near north pole
		{"exact north pole", 90.0, 0.0, false, "point_near_pole"},
		{"just inside north pole threshold", 89.9995, 10.0, false, "point_near_pole"},
		{"just outside north pole threshold", 89.998, 10.0, true, ""},

		// near south pole
		{"exact south pole", -90.0, 0.0, false, "point_near_pole"},
		{"just inside south pole threshold", -89.9995, 10.0, false, "point_near_pole"},
		{"just outside south pole threshold", -89.998, 10.0, true, ""},

		// normal coordinates
		{"Berlin", 52.52, 13.405, true, ""},
		{"Sydney", -33.87, 151.21, true, ""},
		{"Null Island adjacent", 0.0, 1.0, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkNearOriginOrPole(tt.lat, tt.lon, "test")
			if tt.wantNil {
				if err != nil {
					t.Errorf("checkNearOriginOrPole(%v, %v) = %v, want nil", tt.lat, tt.lon, err)
				}
			} else {
				if err == nil {
					t.Errorf("checkNearOriginOrPole(%v, %v) = nil, want error containing %q", tt.lat, tt.lon, tt.wantInErr)
				} else if tt.wantInErr != "" {
					if !containsStr(err.Error(), tt.wantInErr) {
						t.Errorf("checkNearOriginOrPole(%v, %v) error = %q, want it to contain %q", tt.lat, tt.lon, err.Error(), tt.wantInErr)
					}
				}
			}
		})
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

func TestContainsAsWord(t *testing.T) {
	tests := []struct {
		name  string
		long  string
		short string
		want  bool
	}{
		// basic matches
		{"exact match", "E", "E", true},
		{"word at start", "E Linie nach Hause", "E", true},
		{"word at end", "Linie nach E", "E", true},
		{"word in middle", "Linie E nach Hause", "E", true},
		{"multi-char word match", "Route 14 downtown", "14", true},
		{"full word match", "Route Judah downtown", "Judah", true},

		// must not match substrings within words
		{"substring of word at start", "Neuenheim Technologiepark", "E", false},
		{"substring in middle of word", "Berliner Platz", "E", false},
		{"substring at end of word", "Straße", "E", false},
		{"short inside longer word", "Route", "out", false},
		{"number inside longer number", "Route 1414", "14", false},

		// real-world cases from the bug report
		{"Neuenheim long name", "Neuenheim Technologiepark - Handschuhsheim Hans-Thoma-Platz", "E", false},
		{"Berliner Platz long name", "Berliner Platz, Bf LU Mitte - Maudach Kleestraße", "E", false},
		{"Leuschnerstraße long name", "Hans-Warsch-Platz - Leuschnerstraße", "E", false},
		{"Mühlaustraße long name", "Mühlaustraße - Karl-Dillinger-Straße", "E", false},

		// good GTFS examples from spec
		{"Judah", "Judah", "N", false},
		{"ML King Jr Blvd", "ML King Jr Blvd", "6", false},
		{"Boulevard Saint Laurent", "Boulevard Saint Laurent", "55", false},

		// bad GTFS examples from spec - these should match
		{"604 in 604", "604", "604", true},
		{"Route 14 contains 14", "Route 14", "14", true},
		{"Route 2 contains 2", "Route 2: Bellows Falls In-Town", "2", true},

		// case insensitivity
		{"case insensitive match", "linie S nach hause", "s", true},
		{"case insensitive no match", "Straße", "s", false},

		// boundary conditions
		{"empty short", "anything", "", false},
		{"empty long", "", "E", false},
		{"both empty", "", "", false},
		{"short longer than long", "hi", "hello", false},
		{"hyphen boundary before", "Linie-E nach Hause", "E", true},
		{"hyphen boundary after", "Linie E-Bahn", "E", true},
		{"comma boundary", "Linie,E,Bahn", "E", true},
		{"space boundaries", " E ", "E", true},

		// multi-char short names
		{"RE at word boundary", "RE Schnellzug", "RE", true},
		{"RE inside word", "REgionalzug", "RE", false},
		{"ICE standalone", "ICE nach Berlin", "ICE", true},
		{"ICE inside word", "NICE city", "ICE", false},

		// unicode
		{"umlaut word match", "Straße nach Köln", "Köln", true},
		{"umlaut no substring", "Köln", "ln", false},
		{"umlaut boundary", "München-Köln", "Köln", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsAsWord(tt.long, tt.short)
			if got != tt.want {
				t.Errorf("containsAsWord(%q, %q) = %v, want %v", tt.long, tt.short, got, tt.want)
			}
		})
	}
}
