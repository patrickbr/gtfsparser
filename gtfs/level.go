// Copyright 2019 Patrick Brosi
// Authors: info@patrickbrosi.de
//
// Use of this source code is governed by a GPL v2
// license that can be found in the LICENSE file

package gtfs

// A Pathway represents a graph of the station layout
type Level struct {
	Id    string
	Index float32
	Name  string
	// Elevation is used in the example on developers.google.com, but missing in the reference
}
