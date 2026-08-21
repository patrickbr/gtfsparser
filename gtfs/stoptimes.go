// Copyright 2023 Patrick Brosi
// Authors: info@patrickbrosi.de
//
// Use of this source code is governed by a GPL v2
// license that can be found in the LICENSE file

package gtfs

import (
	"math"
	"time"
)

const MaxStopSequence = (1 << 23) - 1

const (
	seqMask   = uint32(1<<23) - 1 // stop_sequence
	noTpBit   = uint32(1) << 23   // set if NOT a timepoint
	pudoShift = 24                // PU/DO
)

// A StopTime is a single stop with times on a trip
type StopTime struct {
	arrival_time        Time
	departure_time      Time
	seqpudo             uint32
	shape_dist_traveled float32
	stop                *Stop
	headsign            *string
}

func (st *StopTime) pudo() uint8 {
	return uint8(st.seqpudo >> pudoShift)
}

func (st *StopTime) setPudo(v uint8) {
	st.seqpudo = (st.seqpudo & ^(uint32(0xff) << pudoShift)) | (uint32(v) << pudoShift)
}

// StopTimes group multiple StopTime objects
type StopTimes []StopTime

// Time is a simple GTFS time type
type Time struct {
	Hour   int16
	Minute int8
	Second int8
}

func (st *StopTime) Arrival_time() Time {
	return st.arrival_time
}

func (st *StopTime) SetArrival_time(t Time) {
	st.arrival_time = t
}

func (st *StopTime) Departure_time() Time {
	return st.departure_time
}

func (st *StopTime) SetDeparture_time(t Time) {
	st.departure_time = t
}

func (st *StopTime) Stop() *Stop {
	return st.stop
}

func (st *StopTime) SetStop(s *Stop) {
	st.stop = s
}

func (st *StopTime) Sequence() int {
	return int(st.seqpudo & seqMask)
}

// SetSequence sets the stop_sequence. seq must be in [0, MaxStopSequence].
func (st *StopTime) SetSequence(seq int) {
	st.seqpudo = (st.seqpudo & ^seqMask) | (uint32(seq) & seqMask)
}

func (st *StopTime) Headsign() *string {
	return st.headsign
}

func (st *StopTime) SetHeadsign(hs *string) {
	st.headsign = hs
}

func (st *StopTime) Pickup_type() uint8 {
	return (st.pudo() & uint8(3))
}

func (st *StopTime) SetPickup_type(put uint8) {
	st.setPudo(st.pudo() | put)
}

func (st *StopTime) Drop_off_type() uint8 {
	return ((st.pudo() & (uint8(3) << 2)) >> 2)
}

func (st *StopTime) SetDrop_off_type(dot uint8) {
	st.setPudo(st.pudo() | (dot << 2))
}

func (st *StopTime) Continuous_pickup() uint8 {
	return ((st.pudo() & (uint8(3) << 4)) >> 4)
}

func (st *StopTime) SetContinuous_pickup(cp uint8) {
	st.setPudo(st.pudo() | (cp << 4))
}

func (st *StopTime) Continuous_drop_off() uint8 {
	return ((st.pudo() & (uint8(3) << 6)) >> 6)
}

func (st *StopTime) SetContinuous_drop_off(cdo uint8) {
	st.setPudo(st.pudo() | (cdo << 6))
}

func (st *StopTime) Shape_dist_traveled() float32 {
	return st.shape_dist_traveled
}

func (st *StopTime) SetShape_dist_traveled(d float32) {
	st.shape_dist_traveled = d
}

func (st *StopTime) Timepoint() bool {
	return st.seqpudo&noTpBit == 0
}

func (st *StopTime) SetTimepoint(tp bool) {
	if tp {
		st.seqpudo &= ^noTpBit
	} else {
		st.seqpudo |= noTpBit
	}
}

// Minus subtracts one time from another
func (a Time) Minus(b Time) int {
	return a.SecondsSinceMidnight() - b.SecondsSinceMidnight()
}

func (stopTimes StopTimes) Len() int {
	return len(stopTimes)
}

func (stopTimes StopTimes) Less(i, j int) bool {
	return stopTimes[i].Sequence() < stopTimes[j].Sequence()
}

func (stopTimes StopTimes) Swap(i, j int) {
	stopTimes[i], stopTimes[j] = stopTimes[j], stopTimes[i]
}

// Empty returns true if the Time is 'empty' - null
func (a Time) Empty() bool {
	return a.Hour == -1 && a.Minute == -1 && a.Second == -1
}

// Equals returns true if two Time objects are exactly the same
func (a Time) Equals(b Time) bool {
	return a.Hour == b.Hour && a.Minute == b.Minute && a.Second == b.Second
}

// SecondsSinceMidnight returns the number of seconds since midnight
func (a Time) SecondsSinceMidnight() int {
	return int(a.Hour)*3600 + int(a.Minute)*60 + int(a.Second)
}

// GetLocationTime returns the time.Time of the gtfs time on a certain
// date, for a certain agency (which itself holds a timezone)
func (a Time) GetLocationTime(d Date, agency *Agency) time.Time {
	loc := agency.Timezone.GetLocation()
	if loc == nil {
		panic("Don't know timezone " + agency.Timezone.GetTzString())
	}

	return time.Date(int(d.Year()), time.Month(d.Month()), int(d.Day()), int(a.Hour), int(a.Minute), int(a.Second), 0, loc)
}

// HasDistanceTraveled returns true if this ShapePoint has a measurement
func (s StopTime) HasDistanceTraveled() bool {
	return !math.IsNaN(float64(s.Shape_dist_traveled()))
}
