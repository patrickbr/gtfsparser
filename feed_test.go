// Copyright 2016 Patrick Brosi
// Authors: info@patrickbrosi.de
//
// Use of this source code is governed by a GPL v2
// license that can be found in the LICENSE file

package gtfsparser

import (
	"testing"
)

func TestFeedParsing(t *testing.T) {
	feedCorA := NewFeed()
	feedCorA.SetParseOpts(ParseOptions{UseDefValueOnError: false, DropErroneous: false, DryRun: false})

	e := feedCorA.Parse("./testfeeds/correct/a")

	if e != nil {
		t.Error(e)
		return
	}

	feedFailA := NewFeed()
	feedFailA.SetParseOpts(ParseOptions{UseDefValueOnError: false, DropErroneous: false, DryRun: false})
	e = feedFailA.Parse("./testfeeds/fail/a")

	if e == nil {
		t.Error("Parse successful, but input feed was incorrect!")
		return
	}

	feedFailA = NewFeed()
	feedFailA.SetParseOpts(ParseOptions{UseDefValueOnError: true, DropErroneous: false, DryRun: false})
	e = feedFailA.Parse("./testfeeds/fail/a")

	if e == nil {
		t.Error("Parse successful, but input feed was incorrect - and unfixable via def value!")
		return
	}

	feedFailA = NewFeed()
	feedFailA.SetParseOpts(ParseOptions{UseDefValueOnError: false, DropErroneous: true, DryRun: false})
	e = feedFailA.Parse("./testfeeds/fail/a")

	if e != nil {
		t.Error(e)
		return
	}

	shp, _ := feedFailA.Shapes["C_shp"]

	for i, p := range shp.Points {
		if i > 0 && p.HasDistanceTraveled() && shp.Points[i-1].HasDistanceTraveled() && p.Dist_traveled <= shp.Points[i-1].Dist_traveled {
			t.Error(p.Dist_traveled, shp.Points[i-1].Dist_traveled)
			return
		}
	}

	if len(shp.Points) != 7 {
		t.Error(len(shp.Points))
	}

	feedCorB := NewFeed()
	feedCorB.SetParseOpts(ParseOptions{UseDefValueOnError: false, DropErroneous: false, DryRun: false})

	e = feedCorB.Parse("./testfeeds/correct/b")

	if e != nil {
		t.Error(e)
		return
	}

	feedCorAddFlds := NewFeed()
	feedCorAddFlds.SetParseOpts(ParseOptions{UseDefValueOnError: false, DropErroneous: false, DryRun: false, KeepAddFlds: true})

	e = feedCorAddFlds.Parse("./testfeeds/correct/addflds")

	if e != nil {
		t.Error(e)
		return
	}

	if len(feedCorAddFlds.Agencies) != 1 {
		t.Error("expected on agency")
		return
	}

	a := feedCorAddFlds.Agencies["DTA"]
	if feedCorAddFlds.AgenciesAddFlds["testfield"][a.Id] != "testvalue" {
		t.Error("Wrong value for <testfield>")
	}

	if feedCorAddFlds.ShapesAddFlds["testfield_shp"]["B_shp"][5] != "b" {
		t.Error("Wrong value for <testfield>")
	}
}

func TestBfsReach(t *testing.T) {
	tests := []struct {
		name  string
		seeds map[string]struct{}
		graph map[string][]string
		want  map[string]struct{}
	}{
		{
			name:  "empty seeds",
			seeds: map[string]struct{}{},
			graph: map[string][]string{"A": {"B"}},
			want:  map[string]struct{}{},
		},
		{
			name:  "empty graph",
			seeds: map[string]struct{}{"A": {}},
			graph: map[string][]string{},
			want:  map[string]struct{}{"A": {}},
		},
		{
			name:  "single node no edges",
			seeds: map[string]struct{}{"A": {}},
			graph: map[string][]string{"A": {}},
			want:  map[string]struct{}{"A": {}},
		},
		{
			name:  "linear chain",
			seeds: map[string]struct{}{"A": {}},
			graph: map[string][]string{
				"A": {"B"},
				"B": {"C"},
				"C": {"D"},
			},
			want: map[string]struct{}{"A": {}, "B": {}, "C": {}, "D": {}},
		},
		{
			name:  "seed not reachable to isolated node",
			seeds: map[string]struct{}{"A": {}},
			graph: map[string][]string{
				"A": {"B"},
				"C": {"D"}, // disconnected component
			},
			want: map[string]struct{}{"A": {}, "B": {}},
		},
		{
			name:  "multiple seeds",
			seeds: map[string]struct{}{"A": {}, "C": {}},
			graph: map[string][]string{
				"A": {"B"},
				"C": {"D"},
			},
			want: map[string]struct{}{"A": {}, "B": {}, "C": {}, "D": {}},
		},
		{
			name:  "cycle does not loop forever",
			seeds: map[string]struct{}{"A": {}},
			graph: map[string][]string{
				"A": {"B"},
				"B": {"C"},
				"C": {"A"}, // cycle back
			},
			want: map[string]struct{}{"A": {}, "B": {}, "C": {}},
		},
		{
			name:  "directed: unreachable in reverse direction",
			seeds: map[string]struct{}{"A": {}},
			graph: map[string][]string{
				"B": {"A"}, // B points to A, but A does not point to B
			},
			want: map[string]struct{}{"A": {}},
		},
		{
			name:  "diamond shape",
			seeds: map[string]struct{}{"A": {}},
			graph: map[string][]string{
				"A": {"B", "C"},
				"B": {"D"},
				"C": {"D"},
			},
			want: map[string]struct{}{"A": {}, "B": {}, "C": {}, "D": {}},
		},
		{
			name:  "seed already in graph edges",
			seeds: map[string]struct{}{"B": {}},
			graph: map[string][]string{
				"A": {"B"},
				"B": {"C"},
			},
			want: map[string]struct{}{"B": {}, "C": {}},
		},
		{
			name: "typical pathway graph: entrances reach platforms",
			seeds: map[string]struct{}{
				"entrance1": {},
				"entrance2": {},
			},
			graph: map[string][]string{
				"entrance1": {"node1"},
				"entrance2": {"node2"},
				"node1":     {"platform1", "platform2"},
				"node2":     {"platform2"},
			},
			want: map[string]struct{}{
				"entrance1": {}, "entrance2": {},
				"node1": {}, "node2": {},
				"platform1": {}, "platform2": {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bfsReach(tt.seeds, tt.graph)

			if len(got) != len(tt.want) {
				t.Errorf("bfsReach() returned %d nodes, want %d\n  got:  %v\n  want: %v",
					len(got), len(tt.want), keys(got), keys(tt.want))
				return
			}
			for k := range tt.want {
				if _, ok := got[k]; !ok {
					t.Errorf("bfsReach() missing expected node %q\n  got:  %v\n  want: %v",
						k, keys(got), keys(tt.want))
				}
			}
		})
	}
}

// keys returns the keys of a map as a sorted slice, for readable error output
func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
