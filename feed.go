// Copyright 2023 Patrick Brosi
// Authors: info@patrickbrosi.de
//
// Use of this source code is governed by a GPL v2
// license that can be found in the LICENSE file

package gtfsparser

import (
	// "archive/zip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	opath "path"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/patrickbr/gtfsparser/gtfs"
)

// Holds the original column ordering
type ColOrders struct {
	Agencies           []string
	Stops              []string
	Routes             []string
	Trips              []string
	StopTimes          []string
	Frequencies        []string
	Calendar           []string
	CalendarDates      []string
	FareAttributes     []string
	FareAttributeRules []string
	Shapes             []string
	Levels             []string
	Pathways           []string
	Transfers          []string
	FeedInfos          []string
	Attributions       []string
}

type Polygon struct {
	OuterRing  [][2]float64
	InnerRings [][][2]float64
	ll         [2]float64
	ur         [2]float64
}

// NewPolygon creates a new Polygon from an outer ring
func NewPolygon(outer [][2]float64, inners [][][2]float64) Polygon {
	poly := Polygon{outer, inners, [2]float64{math.MaxFloat64, math.MaxFloat64}, [2]float64{-math.MaxFloat64, -math.MaxFloat64}}

	for _, p := range outer {
		if p[0] < poly.ll[0] {
			poly.ll[0] = p[0]
		}
		if p[1] < poly.ll[1] {
			poly.ll[1] = p[1]
		}
		if p[0] > poly.ur[0] {
			poly.ur[0] = p[0]
		}
		if p[1] > poly.ur[1] {
			poly.ur[1] = p[1]
		}
	}

	for _, inner := range inners {
		for _, p := range inner {
			if p[0] < poly.ll[0] {
				poly.ll[0] = p[0]
			}
			if p[1] < poly.ll[1] {
				poly.ll[1] = p[1]
			}
			if p[0] > poly.ur[0] {
				poly.ur[0] = p[0]
			}
			if p[1] > poly.ur[1] {
				poly.ur[1] = p[1]
			}
		}
	}

	return poly
}

// A ParseOptions object holds options for parsing a the feed
type ParseOptions struct {
	UseDefValueOnError           bool
	DropErroneous                bool
	DryRun                       bool
	CheckNullCoordinates         bool
	EmptyStringRepl              string
	ZipFix                       bool
	ShowWarnings                 bool
	ShowWarningsExpensive        bool
	DropShapes                   bool
	KeepAddFlds                  bool
	DateFilterStart              gtfs.Date
	DateFilterEnd                gtfs.Date
	PolygonFilter                []Polygon
	UseStandardRouteTypes        bool
	MOTFilter                    map[int16]bool
	MOTFilterNeg                 map[int16]bool
	AssumeCleanCsv               bool
	RemoveFillers                bool
	UseGoogleSupportedRouteTypes bool
	DropSingleStopTrips          bool
}

type ErrStats struct {
	DroppedAgencies           int
	DroppedStops              int
	DroppedRoutes             int
	DroppedTrips              int
	DroppedStopTimes          int
	DroppedFrequencies        int
	DroppedServices           int
	DroppedFareAttributes     int
	DroppedFareAttributeRules int
	DroppedAttributions       int
	DroppedShapes             int
	DroppedLevels             int
	DroppedPathways           int
	DroppedTransfers          int
	DroppedFeedInfos          int
	DroppedTranslations       int
	NumTranslations           int
}

// Feed represents a single GTFS feed
type Feed struct {
	Agencies       map[string]*gtfs.Agency
	Stops          map[string]*gtfs.Stop
	Routes         map[string]*gtfs.Route
	Trips          map[string]*gtfs.Trip
	Services       map[string]*gtfs.Service
	FareAttributes map[string]*gtfs.FareAttribute
	Shapes         map[string]*gtfs.Shape
	Levels         map[string]*gtfs.Level
	Pathways       map[string]*gtfs.Pathway
	Transfers      map[gtfs.TransferKey]gtfs.TransferVal
	FeedInfos      []*gtfs.FeedInfo
	ZoneIds        map[string]bool

	StopsAddFlds          map[string]map[string]string
	AgenciesAddFlds       map[string]map[string]string
	RoutesAddFlds         map[string]map[string]string
	TripsAddFlds          map[string]map[string]string
	StopTimesAddFlds      map[string]map[string]map[int]string
	FrequenciesAddFlds    map[string]map[string]map[*gtfs.Frequency]string
	ShapesAddFlds         map[string]map[string]map[int]string
	FareRulesAddFlds      map[string]map[string]map[*gtfs.FareAttributeRule]string
	LevelsAddFlds         map[string]map[string]string
	PathwaysAddFlds       map[string]map[string]string
	FareAttributesAddFlds map[string]map[string]string
	TransfersAddFlds      map[string]map[gtfs.TransferKey]string
	FeedInfosAddFlds      map[string]map[*gtfs.FeedInfo]string
	AttributionsAddFlds   map[string]map[*gtfs.Attribution]string
	TranslationsAddFlds   map[string]map[*gtfs.Translation]string

	// this only holds feed-wide attributions
	Attributions []*gtfs.Attribution

	ErrorStats   ErrStats
	warnCounts   map[string]int
	NumShpPoints int
	NumStopTimes int

	ColOrders ColOrders

	lastTrip  *gtfs.Trip
	lastShape *gtfs.Shape

	zipFileCloser *zip.ReadCloser
	curFileHandle *os.File

	lastString  *string
	emptyString string

	fastParsePossible bool

	opts ParseOptions
}

// NewFeed creates a new, empty feed
func NewFeed() *Feed {
	g := Feed{
		Agencies:              make(map[string]*gtfs.Agency),
		Stops:                 make(map[string]*gtfs.Stop),
		Routes:                make(map[string]*gtfs.Route),
		Trips:                 make(map[string]*gtfs.Trip),
		Services:              make(map[string]*gtfs.Service),
		FareAttributes:        make(map[string]*gtfs.FareAttribute),
		Shapes:                make(map[string]*gtfs.Shape),
		Levels:                make(map[string]*gtfs.Level),
		Pathways:              make(map[string]*gtfs.Pathway),
		Transfers:             make(map[gtfs.TransferKey]gtfs.TransferVal, 0),
		FeedInfos:             make([]*gtfs.FeedInfo, 0),
		ZoneIds:               make(map[string]bool, 0),
		StopsAddFlds:          make(map[string]map[string]string),
		StopTimesAddFlds:      make(map[string]map[string]map[int]string),
		FrequenciesAddFlds:    make(map[string]map[string]map[*gtfs.Frequency]string),
		ShapesAddFlds:         make(map[string]map[string]map[int]string),
		AgenciesAddFlds:       make(map[string]map[string]string),
		RoutesAddFlds:         make(map[string]map[string]string),
		TripsAddFlds:          make(map[string]map[string]string),
		LevelsAddFlds:         make(map[string]map[string]string),
		PathwaysAddFlds:       make(map[string]map[string]string),
		FareAttributesAddFlds: make(map[string]map[string]string),
		FareRulesAddFlds:      make(map[string]map[string]map[*gtfs.FareAttributeRule]string),
		TransfersAddFlds:      make(map[string]map[gtfs.TransferKey]string),
		FeedInfosAddFlds:      make(map[string]map[*gtfs.FeedInfo]string),
		AttributionsAddFlds:   make(map[string]map[*gtfs.Attribution]string),
		ErrorStats:            ErrStats{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		warnCounts:            make(map[string]int),
		NumShpPoints:          0,
		NumStopTimes:          0,
		fastParsePossible:     true,
		opts:                  ParseOptions{false, false, false, false, "", false, false, false, false, false, gtfs.Date{}, gtfs.Date{}, make([]Polygon, 0), false, make(map[int16]bool, 0), make(map[int16]bool, 0), false, false, false, false},
	}
	g.lastString = &g.emptyString

	return &g
}

// SetParseOpts sets the ParseOptions for this feed
func (feed *Feed) SetParseOpts(opts ParseOptions) {
	feed.opts = opts
	feed.opts.ShowWarnings = feed.opts.ShowWarnings || feed.opts.ShowWarningsExpensive
}

// Parse the GTFS data in the specified folder into the feed
func (feed *Feed) Parse(path string) error {
	return feed.PrefixParse(path, "")
}

// Parse the GTFS data in the specified folder into the feed, use
// and id prefix
func (feed *Feed) PrefixParse(path string, prefix string) error {
	var e error

	// holds stops that are dropped because of geometric filtering.
	// if these are referenced later, we quietly ignore the error like
	// with -De
	geofilteredStops := make(map[string]struct{}, 0)

	// holds routes that are dropped because of MOT filtering.
	// if these are referenced later, we quietly ignore the error like
	// with -De
	filteredRoutes := make(map[string]struct{}, 0)

	// holds trips that are dropped because of MOT filtering.
	// if these are referenced later, we quietly ignore the error like
	// with -De
	filteredTrips := make(map[string]struct{}, 0)

	// holds zones ids that are dropped because of geometric filtering.
	// if these are referenced later, we quietly ignore the error like
	// with -De
	geofilteredZones := make(map[string]struct{}, 0)

	hasBoardingArea := make(map[string]bool)

	e = feed.parseAgencies(path, prefix)
	if e == nil {
		e = feed.parseFeedInfos(path)
	}
	if e == nil {
		e = feed.parseLevels(path, prefix)
	}
	if e == nil {
		e = feed.parseStops(path, prefix, geofilteredStops, geofilteredZones)
	}
	if e == nil {
		e = feed.reserveShapes(path, prefix)
	}
	if e == nil {
		e = feed.parseShapes(path, prefix)
	}
	if e == nil {
		e = feed.parseRoutes(path, prefix, filteredRoutes)
	}
	if e == nil {
		e = feed.parseCalendar(path, prefix)
	}
	if e == nil {
		e = feed.parseCalendarDates(path, prefix)
	}
	if e == nil && len(feed.Services) == 0 {
		e = errors.New("missing_calendar_and_calendar_date_files: Neither calendar.txt nor calendar_dates.txt could be opened. At least one is required.")
	}
	if e == nil {
		e = feed.parseTrips(path, prefix, filteredRoutes, filteredTrips)
	}
	if e == nil {
		e = feed.reserveStopTimes(path, prefix, filteredTrips)
	}
	if e == nil {
		e = feed.parseStopTimes(path, prefix, geofilteredStops, filteredTrips)
	}
	if e == nil {
		// remove reservation markers
		for tripId, t := range feed.Trips {
			// might be nil on dry run
			if t != nil && t.Id != tripId {
				t.Id = tripId
				t.StopTimes = make(gtfs.StopTimes, 0)
			}
		}
	}
	if e == nil {
		e = feed.parseFareAttributes(path, prefix)
	}
	if e == nil {
		e = feed.parseFareAttributeRules(path, prefix, filteredRoutes, geofilteredZones)
	}
	if e == nil {
		e = feed.parseFrequencies(path, prefix, filteredTrips)
	}
	if e == nil {
		e = feed.parseTransfers(path, prefix, geofilteredStops, filteredRoutes)
	}
	if e == nil {
		if feed.opts.ShowWarnings || feed.opts.ShowWarningsExpensive {
			for _, s := range feed.Stops {
				if s.Location_type == 4 && s.Parent_station != nil {
					hasBoardingArea[s.Parent_station.Id] = true
				}
			}
		}

		e = feed.parsePathways(path, prefix, geofilteredStops, hasBoardingArea)
	}
	if e == nil {
		e = feed.parseAttributions(path, prefix, filteredRoutes, filteredTrips)
	}
	// if e == nil {
	// e = feed.parseTranslations(path, prefix)
	// }

	// close open readers
	if feed.zipFileCloser != nil {
		feed.zipFileCloser.Close()
		feed.zipFileCloser = nil
	}

	if feed.curFileHandle != nil {
		feed.curFileHandle.Close()
		feed.curFileHandle = nil
	}

	if !feed.opts.DateFilterStart.IsEmpty() || !feed.opts.DateFilterEnd.IsEmpty() {
		feed.filterServices(prefix)
	}

	if feed.opts.DropSingleStopTrips {
		for _, t := range feed.Trips {
			if len(t.StopTimes) < 2 {
				feed.DeleteTrip(t.Id)
			}
		}
	}

	if feed.opts.ShowWarnings {
		feed.warnDuplicateUrls()
		feed.warnAgencyLangConsistency()
		feed.warnUnusedStations()
		feed.warnExpiredCalendars()
	}

	if feed.opts.ShowWarningsExpensive {
		feed.warnPathwayReachability(hasBoardingArea)
		feed.warnUnusedShapesAndTripsAndStops()
		feed.warnBlockTrips()
	}

	return e
}

func (feed *Feed) filterServices(prefix string) {
	toDel := make([]*gtfs.Service, 0)
	for _, t := range feed.Trips {
		s := t.Service
		if (s.IsEmpty() && s.Start_date().IsEmpty() && s.End_date().IsEmpty()) || s.GetFirstActiveDate().IsEmpty() {
			feed.DeleteTrip(t.Id)
			toDel = append(toDel, s)
		}
	}

	for _, s := range toDel {
		delete(feed.Services, s.Id())
	}
}

func (feed *Feed) getFile(path string, name string) (io.Reader, error) {
	fileInfo, err := os.Stat(path)

	if err != nil {
		return nil, err
	}

	if fileInfo.IsDir() {
		if feed.curFileHandle != nil {
			// close previous handle
			feed.curFileHandle.Close()
		}

		return os.Open(opath.Join(path, name))
	}

	var e error
	if feed.zipFileCloser == nil {
		// reuse existing opened zip file
		feed.zipFileCloser, e = zip.OpenReader(path)
	}

	if e != nil {
		return nil, e
	}

	// check for any directory that is a ZIP file
	zipDir := feed.getGTFSDir(feed.zipFileCloser)

	if !feed.opts.ZipFix {
		zipDir = ""
	}

	for _, f := range feed.zipFileCloser.File {
		d, n := opath.Split(f.Name)
		if d == zipDir && n == name {
			return f.Open()
		}
	}

	return nil, errors.New("Not found")
}

func (feed *Feed) parseAgencies(path string, prefix string) (err error) {
	file, e := feed.getFile(path, "agency.txt")

	if e != nil {
		return errors.New("Could not open required file agency.txt")
	}

	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"agency.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := AgencyFields{
		agencyId:       reader.headeridx.GetFldId("agency_id", -1),
		agencyName:     reader.headeridx.GetFldId("agency_name", -2),
		agencyUrl:      reader.headeridx.GetFldId("agency_url", -3),
		agencyTimezone: reader.headeridx.GetFldId("agency_timezone", -4),
		agencyLang:     reader.headeridx.GetFldId("agency_lang", -5),
		agencyPhone:    reader.headeridx.GetFldId("agency_phone", -6),
		agencyFareUrl:  reader.headeridx.GetFldId("agency_fare_url", -7),
		agencyEmail:    reader.headeridx.GetFldId("agency_email", -8),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}
	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		agency, e := createAgency(record, flds, feed, prefix)
		if e == nil {
			if _, ok := feed.Agencies[agency.Id]; ok {
				e = errors.New("ID collision, agency_id '" + agency.Id + "' already used.")
			}
		}

		if e == nil {
			existingAgId := ""

			for k := range feed.Agencies {
				existingAgId = k
				break
			}

			if len(existingAgId) > 0 && !feed.Agencies[existingAgId].Timezone.Equals(agency.Timezone) {
				e = fmt.Errorf("Agency '%s' has a different timezone (%s) than existing agencies (%s). All agencies must have the same timezone.", agency.Id, agency.Timezone.GetTzString(), feed.Agencies[existingAgId].Timezone.GetTzString())
			}
		}

		if e != nil {
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedAgencies++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}

		feed.Agencies[agency.Id] = agency
		if feed.opts.ShowWarnings && !isValidId(agency.Id) {

			feed.warnLimited("non_ascii_or_non_printable_char", fmt.Errorf("non_ascii_or_non_printable_char: agency_id '%s' contains non-ASCII or non-printable characters", agency.Id))
		}

		for _, i := range addFlds {
			if i < len(record) {
				if _, ok := feed.AgenciesAddFlds[reader.header[i]]; !ok {
					feed.AgenciesAddFlds[reader.header[i]] = make(map[string]string)
				}

				feed.AgenciesAddFlds[reader.header[i]][agency.Id] = record[i]
			}
		}
	}

	feed.ColOrders.Agencies = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseStops(path string, prefix string, geofilteredStops map[string]struct{}, geofilteredZones map[string]struct{}) (err error) {
	file, e := feed.getFile(path, "stops.txt")

	if e != nil {
		return errors.New("Could not open required file stops.txt")
	}

	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"stops.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := StopFields{
		stopId:             reader.headeridx.GetFldId("stop_id", -1),
		stopCode:           reader.headeridx.GetFldId("stop_code", -2),
		locationType:       reader.headeridx.GetFldId("location_type", -3),
		stopName:           reader.headeridx.GetFldId("stop_name", -4),
		stopDesc:           reader.headeridx.GetFldId("stop_desc", -5),
		stopLat:            reader.headeridx.GetFldId("stop_lat", -6),
		stopLon:            reader.headeridx.GetFldId("stop_lon", -7),
		zoneId:             reader.headeridx.GetFldId("zone_id", -8),
		stopUrl:            reader.headeridx.GetFldId("stop_url", -9),
		parentStation:      reader.headeridx.GetFldId("parent_station", -10),
		stopTimezone:       reader.headeridx.GetFldId("stop_timezone", -11),
		levelId:            reader.headeridx.GetFldId("level_id", -12),
		platformCode:       reader.headeridx.GetFldId("platform_code", -13),
		wheelchairBoarding: reader.headeridx.GetFldId("wheelchair_boarding", -14),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	zoneSeen := make(map[string]int)
	zoneKept := make(map[string]int)

	parentStopIds := make(map[string]string, 0)
	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		stop, parentId, e := createStop(record, flds, feed, prefix)
		if e == nil {
			if _, ok := feed.Stops[stop.Id]; ok {
				e = errors.New("ID collision, stop_id '" + stop.Id + "' already used.")
			}
		}
		if e != nil {
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedStops++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}

		// check if any defined PolygonFilter contains the stop
		contains := true
		for _, poly := range feed.opts.PolygonFilter {
			contains = false
			if poly.PolyContains(float64(stop.Lon), float64(stop.Lat)) {
				contains = true
				break
			}
		}

		if stop.Zone_id != "" {
			zoneSeen[stop.Zone_id]++
		}

		if !contains {
			geofilteredStops[stop.Id] = struct{}{}
			continue
		}

		if stop.Zone_id != "" {
			zoneKept[stop.Zone_id]++
			feed.ZoneIds[stop.Zone_id] = true
		}

		if len(parentId) > len(prefix) {
			parentStopIds[stop.Id] = parentId
		}

		feed.Stops[stop.Id] = stop

		if feed.opts.ShowWarnings {
			if !isValidId(stop.Id) {
				feed.warnLimited("non_ascii_or_non_printable_char", fmt.Errorf("non_ascii_or_non_printable_char: stop_id '%s' contains non-ASCII or non-printable characters", stop.Id))
			}

			if len(stop.Desc) > 0 && strings.EqualFold(stop.Desc, stop.Name) {
				feed.warnLimited("same_name_and_description_for_stop", fmt.Errorf("same_name_and_description_for_stop: stop '%s' has the same description as its name ('%s')", stop.Id, stop.Name))
			}
		}

		for _, i := range addFlds {
			if i < len(record) {
				if _, ok := feed.StopsAddFlds[reader.header[i]]; !ok {
					feed.StopsAddFlds[reader.header[i]] = make(map[string]string)
				}

				feed.StopsAddFlds[reader.header[i]][stop.Id] = record[i]
			}
		}
	}

	for zoneID, seen := range zoneSeen {
		if seen > 0 && zoneKept[zoneID] == 0 {
			geofilteredZones[zoneID] = struct{}{}
		}
	}

	feed.ColOrders.Stops = append([]string(nil), reader.header...)

	// write the parent stop ids
	for id, pid := range parentStopIds {
		pstop, ok := feed.Stops[pid]
		if !ok {
			locErr := errors.New("(for stop id " + id + ") No station with id " + pid + " found, cannot use as parent station here.")
			_, wasFiltered := geofilteredStops[pid]

			// note: if type >= 2, a parent Id is *required*
			if wasFiltered && feed.Stops[id].Location_type < 2 {
				// continue, the default value "nil" has already be written above
				continue
			} else if feed.opts.UseDefValueOnError && feed.Stops[id].Location_type < 2 {
				// continue, the default value "nil" has already be written above
				feed.warn(locErr)
				continue
			} else if feed.opts.DropErroneous {
				// delete the erroneous entry
				delete(feed.Stops, id)
				feed.ErrorStats.DroppedStops++
				feed.warn(locErr)
				continue
			} else {
				return locErr
			}
		}

		if (feed.Stops[id].Location_type == 0 || feed.Stops[id].Location_type == 2 || feed.Stops[id].Location_type == 3) && pstop.Location_type != 1 {
			locErr := fmt.Errorf("(for stop id %s) Station with id %s has location_type=%d, cannot use as parent station here for stop with location_type=%d (must be 1).", id, pid, pstop.Location_type, feed.Stops[id].Location_type)
			if feed.opts.UseDefValueOnError && !(feed.Stops[id].Location_type == 2 || feed.Stops[id].Location_type == 3) {
				// continue, the default value "nil" has already be written above
				feed.warn(locErr)
				continue
			} else if feed.opts.DropErroneous {
				// delete the erroneous entry
				delete(feed.Stops, id)
				feed.ErrorStats.DroppedStops++
				feed.warn(locErr)
				continue
			} else {
				return (locErr)
			}
		}

		if feed.Stops[id].Location_type == 4 && pstop.Location_type != 0 {
			locErr := fmt.Errorf("(for stop id %s) Station with id %s has location_type=%d, cannot use as parent station here for stop with location_type=4 (boarding area), which expects a parent station with location_type=0 (stop/platform).", id, pid, pstop.Location_type)
			if feed.opts.DropErroneous {
				// delete the erroneous entry
				delete(feed.Stops, id)
				feed.ErrorStats.DroppedStops++
				feed.warn(locErr)
				continue
			} else {
				panic(locErr)
			}
		}

		feed.Stops[id].Parent_station = pstop
	}

	return e
}

func (feed *Feed) parseRoutes(path string, prefix string, filtered map[string]struct{}) (err error) {
	file, e := feed.getFile(path, "routes.txt")

	if e != nil {
		return errors.New("Could not open required file routes.txt")
	}

	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"routes.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := RouteFields{
		routeId:           reader.headeridx.GetFldId("route_id", -1),
		agencyId:          reader.headeridx.GetFldId("agency_id", -2),
		routeShortName:    reader.headeridx.GetFldId("route_short_name", -3),
		routeLongName:     reader.headeridx.GetFldId("route_long_name", -4),
		routeDesc:         reader.headeridx.GetFldId("route_desc", -5),
		routeType:         reader.headeridx.GetFldId("route_type", -6),
		routeUrl:          reader.headeridx.GetFldId("route_url", -7),
		routeColor:        reader.headeridx.GetFldId("route_color", -8),
		routeTextColor:    reader.headeridx.GetFldId("route_text_color", -9),
		routeSortOrder:    reader.headeridx.GetFldId("route_sort_order", -10),
		continuousDropOff: reader.headeridx.GetFldId("continuous_drop_off", -11),
		continuousPickup:  reader.headeridx.GetFldId("continuous_pickup", -12),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		route, e := createRoute(record, flds, feed, prefix)
		if e == nil {
			if _, ok := feed.Routes[route.Id]; ok {
				e = errors.New("ID collision, route_id '" + route.Id + "' already used.")
			}
		}
		if e != nil {
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedRoutes++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}

		if feed.opts.UseStandardRouteTypes {
			route.Type = gtfs.GetTypeFromExtended(route.Type)
		}

		if feed.opts.UseGoogleSupportedRouteTypes {
			route.Type = gtfs.GetSupportedExtendedTypeFromExtended(route.Type)
		}

		if len(feed.opts.MOTFilter) != 0 {
			if _, ok := feed.opts.MOTFilter[route.Type]; !ok {
				filtered[route.Id] = struct{}{}
				continue
			}
		}

		if len(feed.opts.MOTFilterNeg) != 0 {
			if _, ok := feed.opts.MOTFilterNeg[route.Type]; ok {
				filtered[route.Id] = struct{}{}
				continue
			}
		}

		if feed.opts.DryRun {
			feed.Routes[route.Id] = route
		} else {
			feed.Routes[route.Id] = route

			for _, i := range addFlds {
				if i < len(record) {
					if _, ok := feed.RoutesAddFlds[reader.header[i]]; !ok {
						feed.RoutesAddFlds[reader.header[i]] = make(map[string]string)
					}

					feed.RoutesAddFlds[reader.header[i]][route.Id] = record[i]
				}
			}
		}

		if feed.opts.ShowWarnings {
			if route.Short_name == "" && route.Long_name == "" {
				feed.warnLimited("route_both_short_and_long_name_missing", fmt.Errorf("route_both_short_and_long_name_missing: route '%s' has neither route_short_name nor route_long_name", route.Id))
			}
		}

		if feed.opts.ShowWarnings && !isValidId(route.Id) {
			feed.warnLimited("non_ascii_or_non_printable_char", fmt.Errorf("non_ascii_or_non_printable_char: route_id '%s' contains non-ASCII or non-printable characters", route.Id))
		}

		if feed.opts.ShowWarnings {
			if len(route.Short_name) > 12 {
				feed.warnLimited("route_short_name_too_long", fmt.Errorf("route_short_name_too_long: route '%s' has a short name longer than 12 characters ('%s')", route.Id, route.Short_name))
			}
			if len(route.Short_name) > 0 && len(route.Long_name) > 0 &&
				containsAsWord(route.Long_name, route.Short_name) {
				feed.warnLimited("route_long_name_contains_short_name", fmt.Errorf("route_long_name_contains_short_name: route '%s' long name ('%s') contains short name ('%s')",
					route.Id, route.Long_name, route.Short_name))
			}
			if len(route.Desc) > 0 &&
				(strings.EqualFold(route.Desc, route.Short_name) || strings.EqualFold(route.Desc, route.Long_name)) {
				feed.warnLimited("same_name_and_description_for_route", fmt.Errorf("same_name_and_description_for_route: route '%s' has the same description as its short or long name", route.Id))
			}
		}
	}

	feed.ColOrders.Routes = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseCalendar(path string, prefix string) (err error) {
	file, e := feed.getFile(path, "calendar.txt")

	if e != nil {
		return nil
	}

	// reader := NewCsvParser(file, feed.opts.DropErroneous, feed.opts.AssumeCleanCsv && !feed.opts.KeepAddFlds)
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"calendar.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := CalendarFields{
		serviceId: reader.headeridx.GetFldId("service_id", -1),
		monday:    reader.headeridx.GetFldId("monday", -2),
		tuesday:   reader.headeridx.GetFldId("tuesday", -3),
		wednesday: reader.headeridx.GetFldId("wednesday", -4),
		thursday:  reader.headeridx.GetFldId("thursday", -5),
		friday:    reader.headeridx.GetFldId("friday", -6),
		saturday:  reader.headeridx.GetFldId("saturday", -7),
		sunday:    reader.headeridx.GetFldId("sunday", -8),
		startDate: reader.headeridx.GetFldId("start_date", -9),
		endDate:   reader.headeridx.GetFldId("end_date", -10),
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		service, e := createServiceFromCalendar(record, flds, feed, prefix)

		if e != nil {
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedServices++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}

		// if service was parsed in-place, nil was returned
		if service != nil {
			if feed.opts.DryRun {
				feed.Services[service.Id()] = nil
			} else {
				feed.Services[service.Id()] = service

				// check if service is completely out of range
				if !feed.opts.DateFilterStart.IsEmpty() && service.End_date().GetTime().Before(feed.opts.DateFilterStart.GetTime()) || !feed.opts.DateFilterEnd.IsEmpty() && service.Start_date().GetTime().After(feed.opts.DateFilterEnd.GetTime()) {
					service.SetRawDaymap(0)
				} else {
					// we overlap, there are now two cases:

					// 1. A start date is defined, and the service starts before the start time. Set the start time to the new start time
					if !feed.opts.DateFilterStart.IsEmpty() && service.Start_date().GetTime().Before(feed.opts.DateFilterStart.GetTime()) {
						service.SetStart_date(feed.opts.DateFilterStart)
						// note: because of the check above, End_date is guaranteed to >= DateFilterStart, so our service remains valid
					}

					// 2. An end date is defined, and the service ends after the start time. Set the end  time to the new end time
					if !feed.opts.DateFilterEnd.IsEmpty() && service.End_date().GetTime().After(feed.opts.DateFilterEnd.GetTime()) {
						service.SetEnd_date(feed.opts.DateFilterEnd)
						// note: because of the check above, Start_date is guaranteed to <= DateFilterEnd, so our service remains valid
					}
				}
			}

			if feed.opts.ShowWarnings {
				if !service.Start_date().IsEmpty() && !service.End_date().IsEmpty() &&
					service.Start_date().GetTime().After(service.End_date().GetTime()) {
					feed.warnLimited("start_and_end_range_out_of_order", fmt.Errorf("start_and_end_range_out_of_order: service '%s' has start_date (%s) after end_date (%s)",
						service.Id(),
						service.Start_date().GetTime().Format("20060102"),
						service.End_date().GetTime().Format("20060102")))
				}
			}
		}
	}

	feed.ColOrders.Calendar = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseCalendarDates(path string, prefix string) (err error) {
	file, e := feed.getFile(path, "calendar_dates.txt")

	if e != nil {
		return nil
	}

	// reader := NewCsvParser(file, feed.opts.DropErroneous, feed.opts.AssumeCleanCsv && !feed.opts.KeepAddFlds)
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"calendar_dates.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := CalendarDatesFields{
		serviceId:     reader.headeridx.GetFldId("service_id", -1),
		exceptionType: reader.headeridx.GetFldId("exception_type", -2),
		date:          reader.headeridx.GetFldId("date", -3),
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		service, e := createServiceFromCalendarDates(record, flds, feed, feed.opts.DateFilterStart, feed.opts.DateFilterEnd, prefix)

		if e != nil {
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedServices++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}

		// if service was parsed in-place, nil was returned
		if service != nil {
			if feed.opts.DryRun {
				feed.Services[service.Id()] = nil
			} else {
				feed.Services[service.Id()] = service
			}
		}
	}

	feed.ColOrders.CalendarDates = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseTrips(path string, prefix string, filteredRoutes map[string]struct{}, filteredTrips map[string]struct{}) (err error) {
	file, e := feed.getFile(path, "trips.txt")

	if e != nil {
		return errors.New("Could not open required file trips.txt")
	}

	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"trips.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := TripFields{
		tripId:               reader.headeridx.GetFldId("trip_id", -1),
		routeId:              reader.headeridx.GetFldId("route_id", -2),
		serviceId:            reader.headeridx.GetFldId("service_id", -3),
		tripHeadsign:         reader.headeridx.GetFldId("trip_headsign", -4),
		tripShortName:        reader.headeridx.GetFldId("trip_short_name", -5),
		directionId:          reader.headeridx.GetFldId("direction_id", -6),
		blockId:              reader.headeridx.GetFldId("block_id", -7),
		shapeId:              reader.headeridx.GetFldId("shape_id", -8),
		wheelchairAccessible: reader.headeridx.GetFldId("wheelchair_accessible", -9),
		bikesAllowed:         reader.headeridx.GetFldId("bikes_allowed", -10),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		trip, e := createTrip(record, flds, feed, prefix)

		tripId := ""

		if e == nil {
			tripId = trip.Id
			trip.Id = ""
			dummy := gtfs.StopTime{}
			dummy.SetSequence(0)
			trip.StopTimes = append(trip.StopTimes, dummy)
			if _, ok := feed.Trips[tripId]; ok {
				e = errors.New("ID collision, trip_id '" + tripId + "' already used.")
			}
		} else {
			routeNotFoundErr, routeNotFound := e.(*RouteNotFoundErr)
			wasFiltered := false
			if routeNotFound {
				_, wasFiltered = filteredRoutes[routeNotFoundErr.RouteId()]
			}

			if wasFiltered {
				filteredTrips[routeNotFoundErr.PayloadId()] = struct{}{}
				continue
			} else if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedTrips++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}
		feed.Trips[tripId] = trip
		if feed.opts.ShowWarnings {
			if !isValidId(tripId) {
				feed.warnLimited("non_ascii_or_non_printable_char", fmt.Errorf("non_ascii_or_non_printable_char: trip_id '%s' contains non-ASCII or non-printable characters", tripId))
			}
			if trip.Route != nil && gtfs.GetTypeFromExtended(trip.Route.Type) == 4 && trip.Bikes_allowed == 0 {
				feed.warnLimited("missing_bike_allowance", fmt.Errorf("missing_bike_allowance: ferry trip '%s' does not specify bikes_allowed", tripId))
			}
		}

		for _, i := range addFlds {
			if i < len(record) {
				if _, ok := feed.TripsAddFlds[reader.header[i]]; !ok {
					feed.TripsAddFlds[reader.header[i]] = make(map[string]string)
				}

				feed.TripsAddFlds[reader.header[i]][tripId] = record[i]
			}
		}
	}

	feed.ColOrders.Trips = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) reserveShapes(path string, prefix string) (err error) {
	if feed.opts.DropShapes {
		return
	}
	file, e := feed.getFile(path, "shapes.txt")

	if e != nil {
		return nil
	}

	reader := NewCsvParser(file, feed.opts.DropErroneous, feed.opts.AssumeCleanCsv && !feed.opts.KeepAddFlds)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"shapes.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := ShapeFields{
		shapeId:           reader.headeridx.GetFldId("shape_id", -1),
		shapeDistTraveled: reader.headeridx.GetFldId("shape_dist_traveled", -2),
		shapePtLat:        reader.headeridx.GetFldId("shape_pt_lat", -3),
		shapePtLon:        reader.headeridx.GetFldId("shape_pt_lon", -4),
		shapePtSequence:   reader.headeridx.GetFldId("shape_pt_sequence", -5),
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		e := reserveShapePoint(record, flds, feed, prefix)
		if e != nil {
			if feed.opts.DropErroneous {
				continue
			} else {
				panic(e)
			}
		}
	}

	return e
}

func (feed *Feed) parseShapes(path string, prefix string) (err error) {
	if feed.opts.DropShapes {
		return
	}
	file, e := feed.getFile(path, "shapes.txt")

	if e != nil {
		return nil
	}

	reader := NewCsvParser(file, feed.opts.DropErroneous, feed.opts.AssumeCleanCsv && !feed.opts.KeepAddFlds)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"shapes.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := ShapeFields{
		shapeId:           reader.headeridx.GetFldId("shape_id", -1),
		shapeDistTraveled: reader.headeridx.GetFldId("shape_dist_traveled", -2),
		shapePtLat:        reader.headeridx.GetFldId("shape_pt_lat", -3),
		shapePtLon:        reader.headeridx.GetFldId("shape_pt_lon", -4),
		shapePtSequence:   reader.headeridx.GetFldId("shape_pt_sequence", -5),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	i := 0

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		i += 1

		shape, sp, e := createShapePoint(record, flds, feed, prefix)

		if e != nil {
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedShapes++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		} else if sp != nil {
			if feed.opts.ShowWarnings {
				if !isValidId(shape.Id) {
					feed.warnLimited("non_ascii_or_non_printable_char", fmt.Errorf("non_ascii_or_non_printable_char: shape_id '%s' contains non-ASCII or non-printable characters", shape.Id))
				}
			}

			for _, i := range addFlds {
				if i < len(record) {
					if _, ok := feed.ShapesAddFlds[reader.header[i]]; !ok {
						feed.ShapesAddFlds[reader.header[i]] = make(map[string]map[int]string)
					}
					if _, ok := feed.ShapesAddFlds[reader.header[i]][shape.Id]; !ok {
						feed.ShapesAddFlds[reader.header[i]][shape.Id] = make(map[int]string)
					}

					feed.ShapesAddFlds[reader.header[i]][shape.Id][int(sp.Sequence)] = record[i]
				}
			}
		}
	}

	feed.ColOrders.Shapes = append([]string(nil), reader.header...)

	if e == nil {
		// sort points in shapes, drop empty shapes
		for id, shape := range feed.Shapes {
			if len(shape.Points) == 0 {
				loce := fmt.Errorf("Shape #%s has no points", id)
				if feed.opts.DropErroneous || len(feed.opts.PolygonFilter) > 0 {
					// dont warn here, because this can only happen if a shape point
					// has been deleted before
					delete(feed.Shapes, id)
					continue
				} else {
					panic(loce)
				}
			}
			sort.Sort(shape.Points)

			if feed.opts.ShowWarnings && len(shape.Points) == 1 {
				feed.warn(fmt.Errorf("single_shape_point: shape '%s' contains only a single point", id))
			}

			e = feed.checkShapeMeasure(shape, &feed.opts)
			feed.NumShpPoints += len(shape.Points)
			if e != nil {
				break
			}
		}
		if feed.opts.DryRun {
			// clear space
			for id := range feed.Shapes {
				feed.Shapes[id] = nil
			}
		}
	}

	return e
}

func (feed *Feed) reserveStopTimes(path string, prefix string, filteredTrips map[string]struct{}) (err error) {
	file, e := feed.getFile(path, "stop_times.txt")

	if e != nil {
		return errors.New("Could not open required file stop_times.txt")
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"stop_times.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := StopTimeFields{
		tripId:            reader.headeridx.GetFldId("trip_id", -1),
		stopId:            reader.headeridx.GetFldId("stop_id", -2),
		arrivalTime:       reader.headeridx.GetFldId("arrival_time", -3),
		departureTime:     reader.headeridx.GetFldId("departure_time", -4),
		stopSequence:      reader.headeridx.GetFldId("stop_sequence", -5),
		stopHeadsign:      reader.headeridx.GetFldId("stop_headsign", -6),
		pickupType:        reader.headeridx.GetFldId("pickup_type", -7),
		dropOffType:       reader.headeridx.GetFldId("drop_off_type", -8),
		continuousDropOff: reader.headeridx.GetFldId("continuous_drop_off", -9),
		continuousPickup:  reader.headeridx.GetFldId("continuous_pickup", -10),
		shapeDistTraveled: reader.headeridx.GetFldId("shape_dist_traveled", -11),
		timepoint:         reader.headeridx.GetFldId("timepoint", -12),
	}

	file, e = feed.getFile(path, "stop_times.txt")

	if e != nil {
		return errors.New("Could not open required file stop_times.txt")
	}

	reader = NewCsvParser(file, feed.opts.DropErroneous, feed.opts.AssumeCleanCsv && flds.stopHeadsign < 0 && !feed.opts.KeepAddFlds)

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		reserveStopTime(record, flds, feed, prefix)
	}

	return e
}

func (feed *Feed) parseStopTimes(path string, prefix string, geofiltered map[string]struct{}, filteredTrips map[string]struct{}) (err error) {
	file, e := feed.getFile(path, "stop_times.txt")

	if e != nil {
		return errors.New("Could not open required file stop_times.txt")
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, feed.opts.AssumeCleanCsv && !feed.opts.KeepAddFlds)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"stop_times.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := StopTimeFields{
		tripId:            reader.headeridx.GetFldId("trip_id", -1),
		stopId:            reader.headeridx.GetFldId("stop_id", -2),
		arrivalTime:       reader.headeridx.GetFldId("arrival_time", -3),
		departureTime:     reader.headeridx.GetFldId("departure_time", -4),
		stopSequence:      reader.headeridx.GetFldId("stop_sequence", -5),
		stopHeadsign:      reader.headeridx.GetFldId("stop_headsign", -6),
		pickupType:        reader.headeridx.GetFldId("pickup_type", -7),
		dropOffType:       reader.headeridx.GetFldId("drop_off_type", -8),
		continuousDropOff: reader.headeridx.GetFldId("continuous_drop_off", -9),
		continuousPickup:  reader.headeridx.GetFldId("continuous_pickup", -10),
		shapeDistTraveled: reader.headeridx.GetFldId("shape_dist_traveled", -11),
		timepoint:         reader.headeridx.GetFldId("timepoint", -12),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	file, e = feed.getFile(path, "stop_times.txt")

	if e != nil {
		return errors.New("Could not open required file stop_times.txt")
	}

	reader = NewCsvParser(file, feed.opts.DropErroneous, feed.opts.AssumeCleanCsv && flds.stopHeadsign < 0)

	i := 0

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		i += 1

		trip, stopTimeSeq, e := createStopTime(record, &flds, feed, prefix)

		if e != nil {
			wasFiltered := false
			stopNotFoundErr, stopNotFound := e.(*StopNotFoundErr)
			if stopNotFound {
				_, wasFiltered = geofiltered[stopNotFoundErr.StopId()]
			}

			tripNotFoundErr, tripNotFound := e.(*TripNotFoundErr)
			if tripNotFound {
				_, wasFiltered = filteredTrips[tripNotFoundErr.TripId()]
			}

			if wasFiltered {
				continue
			} else if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedStopTimes++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		} else {
			for _, i := range addFlds {
				if i < len(record) {
					if _, ok := feed.StopTimesAddFlds[reader.header[i]]; !ok {
						feed.StopTimesAddFlds[reader.header[i]] = make(map[string]map[int]string)
					}
					if _, ok := feed.StopTimesAddFlds[reader.header[i]][trip.Id]; !ok {
						feed.StopTimesAddFlds[reader.header[i]][trip.Id] = make(map[int]string)
					}

					feed.StopTimesAddFlds[reader.header[i]][trip.Id][stopTimeSeq] = record[i]
				}
			}
		}
	}

	feed.ColOrders.StopTimes = append([]string(nil), reader.header...)

	if e == nil {
		// sort stoptimes in trips
		for _, trip := range feed.Trips {
			sort.Sort(trip.StopTimes)
			e = feed.checkStopTimeMeasure(trip, &feed.opts)
			feed.NumStopTimes += len(trip.StopTimes)

			if feed.opts.ShowWarnings && len(trip.StopTimes) > 0 {
				first := trip.StopTimes[0]
				last := trip.StopTimes[len(trip.StopTimes)-1]
				if first.Arrival_time().Empty() || first.Departure_time().Empty() {
					feed.warnLimited("missing_trip_edge", fmt.Errorf("missing_trip_edge: trip '%s' first stop (seq=%d) is missing arrival_time or departure_time", trip.Id, first.Sequence()))
				}
				if len(trip.StopTimes) > 1 && (last.Arrival_time().Empty() || last.Departure_time().Empty()) {
					feed.warnLimited("missing_trip_edge", fmt.Errorf("missing_trip_edge: trip '%s' last stop (seq=%d) is missing arrival_time or departure_time", trip.Id, last.Sequence()))
				}
			}

			if e != nil {
				break
			}
		}
	}

	return e
}

func (feed *Feed) parseFrequencies(path string, prefix string, filteredTrips map[string]struct{}) (err error) {
	file, e := feed.getFile(path, "frequencies.txt")

	if e != nil {
		return nil
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"frequencies.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := FrequencyFields{
		tripId:      reader.headeridx.GetFldId("trip_id", -1),
		exactTimes:  reader.headeridx.GetFldId("exact_times", -2),
		startTime:   reader.headeridx.GetFldId("start_time", -3),
		endTime:     reader.headeridx.GetFldId("end_time", -4),
		headwaySecs: reader.headeridx.GetFldId("headway_secs", -5),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		trip, freq, e := createFrequency(record, flds, feed, prefix)
		if e != nil {
			tripNotFoundErr, tripNotFound := e.(*TripNotFoundErr)
			wasFiltered := false
			if tripNotFound {
				_, wasFiltered = filteredTrips[tripNotFoundErr.TripId()]
			}

			if wasFiltered {
				continue
			} else if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedFrequencies++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}

		for _, i := range addFlds {
			if i < len(record) {
				if _, ok := feed.FrequenciesAddFlds[reader.header[i]]; !ok {
					feed.FrequenciesAddFlds[reader.header[i]] = make(map[string]map[*gtfs.Frequency]string)
				}
				if _, ok := feed.FrequenciesAddFlds[reader.header[i]][trip.Id]; !ok {
					feed.FrequenciesAddFlds[reader.header[i]][trip.Id] = make(map[*gtfs.Frequency]string)
				}

				feed.FrequenciesAddFlds[reader.header[i]][trip.Id][freq] = record[i]
			}
		}

		if feed.opts.ShowWarnings && freq != nil {
			start := freq.Start_time.SecondsSinceMidnight()
			end := freq.End_time.SecondsSinceMidnight()
			if start == end {
				feed.warn(fmt.Errorf("start_and_end_range_equal: frequency for trip '%s' has equal start_time and end_time (%02d:%02d:%02d)",
					trip.Id, freq.Start_time.Hour, freq.Start_time.Minute, freq.Start_time.Second))
			} else if start > end {
				feed.warnLimited("start_and_end_range_out_of_order", fmt.Errorf("start_and_end_range_out_of_order: frequency for trip '%s' has start_time (%02d:%02d:%02d) after end_time (%02d:%02d:%02d)",
					trip.Id, freq.Start_time.Hour, freq.Start_time.Minute, freq.Start_time.Second,
					freq.End_time.Hour, freq.End_time.Minute, freq.End_time.Second))
			}
		}
	}

	if feed.opts.ShowWarningsExpensive {
		// group frequencies by trip id
		type freqRange struct {
			start int
			end   int
		}
		tripFreqs := make(map[string][]freqRange)
		for _, trip := range feed.Trips {
			if trip.Frequencies != nil {
				for _, f := range *trip.Frequencies {
					tripFreqs[trip.Id] = append(tripFreqs[trip.Id], freqRange{
						start: f.Start_time.SecondsSinceMidnight(),
						end:   f.End_time.SecondsSinceMidnight(),
					})
				}
			}
		}

		for tripId, freqs := range tripFreqs {
			sort.Slice(freqs, func(i, j int) bool {
				return freqs[i].start < freqs[j].start
			})
			for i := 1; i < len(freqs); i++ {
				x, y := freqs[i-1], freqs[i]
				if y.start < x.end {
					feed.warn(fmt.Errorf("overlapping_frequency: trip '%s' has overlapping frequencies (%02d:%02d-%02d:%02d and %02d:%02d-%02d:%02d)",
						tripId,
						x.start/3600, (x.start%3600)/60,
						x.end/3600, (x.end%3600)/60,
						y.start/3600, (y.start%3600)/60,
						y.end/3600, (y.end%3600)/60))
				}
			}
		}
	}

	feed.ColOrders.Frequencies = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseFareAttributes(path string, prefix string) (err error) {
	file, e := feed.getFile(path, "fare_attributes.txt")

	if e != nil {
		return nil
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"fare_attributes.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := FareAttributeFields{
		fareId:           reader.headeridx.GetFldId("fare_id", -1),
		price:            reader.headeridx.GetFldId("price", -2),
		currencyType:     reader.headeridx.GetFldId("currency_type", -3),
		paymentMethod:    reader.headeridx.GetFldId("payment_method", -4),
		transfers:        reader.headeridx.GetFldId("transfers", -5),
		transferDuration: reader.headeridx.GetFldId("transfer_duration", -6),
		agencyId:         reader.headeridx.GetFldId("agency_id", -7),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		fa, e := createFareAttribute(record, flds, feed, prefix)
		if e != nil {
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedFareAttributes++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}
		feed.FareAttributes[fa.Id] = fa

		if feed.opts.ShowWarnings && !isValidId(fa.Id) {
			feed.warnLimited("non_ascii_or_non_printable_char", fmt.Errorf("non_ascii_or_non_printable_char: fare_id '%s' contains non-ASCII or non-printable characters", fa.Id))
		}

		for _, i := range addFlds {
			if i < len(record) {
				if _, ok := feed.FareAttributesAddFlds[reader.header[i]]; !ok {
					feed.FareAttributesAddFlds[reader.header[i]] = make(map[string]string)
				}

				feed.FareAttributesAddFlds[reader.header[i]][fa.Id] = record[i]
			}
		}
	}

	feed.ColOrders.FareAttributes = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseFareAttributeRules(path string, prefix string, filteredRoutes map[string]struct{}, geofilteredZones map[string]struct{}) (err error) {
	file, e := feed.getFile(path, "fare_rules.txt")

	if e != nil {
		return nil
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"fare_rules.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := FareRuleFields{
		fareId:        reader.headeridx.GetFldId("fare_id", -1),
		routeId:       reader.headeridx.GetFldId("route_id", -2),
		originId:      reader.headeridx.GetFldId("origin_id", -3),
		destinationId: reader.headeridx.GetFldId("destination_id", -4),
		containsId:    reader.headeridx.GetFldId("contains_id", -5),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		fare, rule, e := createFareRule(record, flds, feed, prefix, geofilteredZones)
		if e != nil {
			routeNotFoundErr, routeNotFound := e.(*RouteNotFoundErr)
			wasFiltered := false
			if routeNotFound {
				_, wasFiltered = filteredRoutes[routeNotFoundErr.RouteId()]
			}

			zoneNotFoundError, zoneNotFound := e.(*ZoneNotFoundError)
			if zoneNotFound {
				_, wasFiltered = geofilteredZones[zoneNotFoundError.ZoneId()]
			}

			if wasFiltered {
				// silently drop route-related rule
				continue
			} else if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedFareAttributeRules++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		} else {
			for _, i := range addFlds {
				if i < len(record) {
					if _, ok := feed.FareRulesAddFlds[reader.header[i]]; !ok {
						feed.FareRulesAddFlds[reader.header[i]] = make(map[string]map[*gtfs.FareAttributeRule]string)
					}
					if _, ok := feed.FareRulesAddFlds[reader.header[i]][fare.Id]; !ok {
						feed.FareRulesAddFlds[reader.header[i]][fare.Id] = make(map[*gtfs.FareAttributeRule]string)
					}

					feed.FareRulesAddFlds[reader.header[i]][fare.Id][rule] = record[i]
				}
			}

		}
	}

	feed.ColOrders.FareAttributeRules = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseTransfers(path string, prefix string, geofiltered map[string]struct{}, filteredRoutes map[string]struct{}) (err error) {
	file, e := feed.getFile(path, "transfers.txt")

	if e != nil {
		return nil
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"transfers.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := TransferFields{
		FromStopId:      reader.headeridx.GetFldId("from_stop_id", -1),
		ToStopId:        reader.headeridx.GetFldId("to_stop_id", -2),
		FromRouteId:     reader.headeridx.GetFldId("from_route_id", -3),
		ToRouteId:       reader.headeridx.GetFldId("to_route_id", -4),
		FromTripId:      reader.headeridx.GetFldId("from_trip_id", -5),
		ToTripId:        reader.headeridx.GetFldId("to_trip_id", -6),
		TransferType:    reader.headeridx.GetFldId("transfer_type", -7),
		MinTransferTime: reader.headeridx.GetFldId("min_transfer_time", -8),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}
	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		tk, tv, e := createTransfer(record, flds, feed, prefix)
		if e == nil {
			if _, ok := feed.Transfers[tk]; ok {
				e = errors.New("ID collision, transfer already defined.")
			}
		}
		if e != nil {
			stopNotFoundErr, stopNotFound := e.(*StopNotFoundErr)
			wasFiltered := false
			if stopNotFound {
				_, wasFiltered = geofiltered[stopNotFoundErr.StopId()]
			}

			if wasFiltered {
				continue
			} else if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedTransfers++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}

		feed.Transfers[tk] = tv

		if !feed.opts.DryRun {
			// add additional CSV fields
			for _, i := range addFlds {
				if i < len(record) {
					if _, ok := feed.TransfersAddFlds[reader.header[i]]; !ok {
						feed.TransfersAddFlds[reader.header[i]] = make(map[gtfs.TransferKey]string)
					}

					feed.TransfersAddFlds[reader.header[i]][tk] = record[i]
				}
			}
		}
	}

	if feed.opts.ShowWarnings {
		for tk, tv := range feed.Transfers {
			// transfer_distance_too_large
			if tk.From_stop != nil && tk.To_stop != nil {
				lat1 := float64(tk.From_stop.Lat)
				lon1 := float64(tk.From_stop.Lon)
				lat2 := float64(tk.To_stop.Lat)
				lon2 := float64(tk.To_stop.Lon)

				dist := haversineKm(lat1, lon1, lat2, lon2)
				if dist > 10.0 {
					feed.warn(fmt.Errorf("transfer_distance_too_large: transfer from stop '%s' to stop '%s' is %.2f km apart (max 10 km)",
						tk.From_stop.Id, tk.To_stop.Id, dist))
				}
			}

			// transfer_with_suspicious_mid_trip_in_seat
			// type 4 = in-seat transfer (passenger stays on vehicle)
			if tv.Transfer_type == 4 {
				if tk.From_trip != nil && tk.From_stop != nil {
					trip := tk.From_trip
					if len(trip.StopTimes) > 0 {
						lastSt := trip.StopTimes[len(trip.StopTimes)-1]
						if lastSt.Stop() != tk.From_stop {
							feed.warn(fmt.Errorf("transfer_with_suspicious_mid_trip_in_seat: in-seat transfer from trip '%s' references stop '%s' which is not the last stop in the trip",
								trip.Id, tk.From_stop.Id))
						}
					}
				}
				if tk.To_trip != nil && tk.To_stop != nil {
					trip := tk.To_trip
					if len(trip.StopTimes) > 0 {
						firstSt := trip.StopTimes[0]
						if firstSt.Stop() != tk.To_stop {
							feed.warn(fmt.Errorf("transfer_with_suspicious_mid_trip_in_seat: in-seat transfer to trip '%s' references stop '%s' which is not the first stop in the trip",
								trip.Id, tk.To_stop.Id))
						}
					}
				}
			}
		}
	}

	feed.ColOrders.Transfers = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parsePathways(path string, prefix string, geofiltered map[string]struct{}, hasBoardingArea map[string]bool) (err error) {
	file, e := feed.getFile(path, "pathways.txt")

	if e != nil {
		return nil
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"pathways.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := PathwayFields{
		pathwayId:            reader.headeridx.GetFldId("pathway_id", -1),
		fromStopId:           reader.headeridx.GetFldId("from_stop_id", -2),
		toStopId:             reader.headeridx.GetFldId("to_stop_id", -3),
		pathwayMode:          reader.headeridx.GetFldId("pathway_mode", -4),
		isBidirectional:      reader.headeridx.GetFldId("is_bidirectional", -5),
		length:               reader.headeridx.GetFldId("length", -6),
		traversalTime:        reader.headeridx.GetFldId("traversal_time", -7),
		stairCount:           reader.headeridx.GetFldId("stair_count", -8),
		maxSlope:             reader.headeridx.GetFldId("max_slope", -9),
		minWidth:             reader.headeridx.GetFldId("min_width", -10),
		signpostedAs:         reader.headeridx.GetFldId("signposted_as", -11),
		reversedSignpostedAs: reader.headeridx.GetFldId("reversed_signposted_as", -12),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		pw, e := createPathway(record, flds, feed, prefix)
		if e == nil {
			if _, ok := feed.Pathways[pw.Id]; ok {
				e = errors.New("ID collision, pathway_id '" + pw.Id + "' already used.")
			}
		}
		if e != nil {
			stopNotFoundErr, stopNotFound := e.(*StopNotFoundErr)
			wasFiltered := false
			if stopNotFound {
				_, wasFiltered = geofiltered[stopNotFoundErr.StopId()]
			}

			if wasFiltered {
				continue
			} else if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedPathways++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}
		feed.Pathways[pw.Id] = pw

		if feed.opts.ShowWarnings {
			if !isValidId(pw.Id) {
				feed.warnLimited("non_ascii_or_non_printable_char", fmt.Errorf("non_ascii_or_non_printable_char: pathway_id '%s' contains non-ASCII or non-printable characters", pw.Id))
			}
			for _, endpoint := range []*gtfs.Stop{pw.From_stop, pw.To_stop} {
				if endpoint == nil {
					continue
				}
				// pathway_to_wrong_location_type: endpoint must not be a station (location_type=1)
				if endpoint.Location_type == 1 {
					feed.warnLimited("pathway_to_wrong_location_type", fmt.Errorf("pathway_to_wrong_location_type: pathway '%s' has an endpoint stop '%s' which is a station (location_type=1); pathways must connect platforms, entrances, generic nodes or boarding areas",
						pw.Id, endpoint.Id))
				}
				// pathway_to_platform_with_boarding_areas: endpoint must not be a platform
				// that itself has boarding areas (location_type=0 with children of type 4)
				if endpoint.Location_type == 0 && hasBoardingArea[endpoint.Id] {
					feed.warnLimited("pathway_to_platform_with_boarding_areas", fmt.Errorf("pathway_to_platform_with_boarding_areas: pathway '%s' has an endpoint platform '%s' which has boarding areas; pathways should be assigned to the boarding areas instead",
						pw.Id, endpoint.Id))
					break
				}
			}

			if pw.From_stop != nil && pw.To_stop != nil && pw.From_stop == pw.To_stop {
				feed.warnLimited("pathway_loop", fmt.Errorf("pathway_loop: pathway '%s' starts and ends at the same location '%s'",
					pw.Id, pw.From_stop.Id))
			}
		}

		for _, i := range addFlds {
			if i < len(record) {
				if _, ok := feed.PathwaysAddFlds[reader.header[i]]; !ok {
					feed.PathwaysAddFlds[reader.header[i]] = make(map[string]string)
				}

				feed.PathwaysAddFlds[reader.header[i]][pw.Id] = record[i]
			}
		}
	}

	feed.ColOrders.Pathways = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseTranslations(path string, prefix string) (err error) {
	file, e := feed.getFile(path, "translations.txt")

	if e != nil {
		return nil
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"translations.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := TranslationFields{
		tableName:   reader.headeridx.GetFldId("table_name", -1),
		fieldName:   reader.headeridx.GetFldId("field_name", -2),
		language:    reader.headeridx.GetFldId("language", -3),
		translation: reader.headeridx.GetFldId("translation", -4),
		recordId:    reader.headeridx.GetFldId("record_id", -5),
		recordSubId: reader.headeridx.GetFldId("record_sub_id", -6),
		fieldValue:  reader.headeridx.GetFldId("field_value", -7),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		trans, e := createTranslation(record, flds, feed, prefix)
		if e != nil {
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedTranslations++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}

		feed.ErrorStats.NumTranslations++

		for _, i := range addFlds {
			if i < len(record) {
				if _, ok := feed.TranslationsAddFlds[reader.header[i]]; !ok {
					feed.TranslationsAddFlds[reader.header[i]] = make(map[*gtfs.Translation]string)
				}

				feed.TranslationsAddFlds[reader.header[i]][trans] = record[i]
			}
		}
	}

	feed.ColOrders.Attributions = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseAttributions(path string, prefix string, filteredRoutes map[string]struct{}, filteredTrips map[string]struct{}) (err error) {
	file, e := feed.getFile(path, "attributions.txt")

	if e != nil {
		return nil
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"attributions.txt", reader.Curline, r.(error).Error()}
		}
	}()

	ids := make(map[string]bool)

	var record []string
	flds := AttributionFields{
		attributionId:    reader.headeridx.GetFldId("attribution_id", -1),
		organizationName: reader.headeridx.GetFldId("organization_name", -2),
		isProducer:       reader.headeridx.GetFldId("is_producer", -3),
		isOperator:       reader.headeridx.GetFldId("is_operator", -4),
		isAuthority:      reader.headeridx.GetFldId("is_authority", -5),
		attributionUrl:   reader.headeridx.GetFldId("attribution_url", -6),
		attributionEmail: reader.headeridx.GetFldId("attribution_email", -7),
		attributionPhone: reader.headeridx.GetFldId("attribution_phone", -8),
		routeId:          reader.headeridx.GetFldId("route_id", -9),
		agencyId:         reader.headeridx.GetFldId("agency_id", -10),
		tripId:           reader.headeridx.GetFldId("trip_id", -11),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		attr, ag, route, trip, e := createAttribution(record, flds, feed, prefix)
		if e == nil {
			if len(attr.Id) == len(prefix) {
				attr.Id = ""
			}
			if _, ok := ids[attr.Id]; ok {
				e = errors.New("ID collision, attribution_id '" + attr.Id + "' already used.")
			}
			if len(attr.Id) > 0 {
				ids[attr.Id] = true
			}
		}

		if e != nil {
			routeNotFoundErr, routeNotFound := e.(*RouteNotFoundErr)
			wasFiltered := false
			if routeNotFound {
				_, wasFiltered = filteredRoutes[routeNotFoundErr.RouteId()]
			}

			tripNotFoundErr, tripNotFound := e.(*TripNotFoundErr)
			if tripNotFound {
				_, wasFiltered = filteredTrips[tripNotFoundErr.TripId()]
			}

			if wasFiltered {
				continue
			} else if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedAttributions++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}

		if ag != nil {
			ag.Attributions = append(ag.Attributions, attr)
		} else if route != nil {
			route.Attributions = append(route.Attributions, attr)
		} else if trip != nil {
			if trip.Attributions == nil {
				attrs := make([]*gtfs.Attribution, 0)
				trip.Attributions = &attrs
			}
			*trip.Attributions = append(*trip.Attributions, attr)
		} else {
			// if the attribution is not for a specific agency, route or trip,
			// add it to feed-wide
			feed.Attributions = append(feed.Attributions, attr)
		}

		for _, i := range addFlds {
			if i < len(record) {
				if _, ok := feed.AttributionsAddFlds[reader.header[i]]; !ok {
					feed.AttributionsAddFlds[reader.header[i]] = make(map[*gtfs.Attribution]string)
				}

				feed.AttributionsAddFlds[reader.header[i]][attr] = record[i]
			}
		}
	}

	feed.ColOrders.Attributions = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseLevels(path string, idprefix string) (err error) {
	file, e := feed.getFile(path, "levels.txt")

	if e != nil {
		return nil
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"levels.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := LevelFields{
		levelId:    reader.headeridx.GetFldId("level_id", -1),
		levelIndex: reader.headeridx.GetFldId("level_index", -2),
		levelName:  reader.headeridx.GetFldId("level_name", -3),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}
	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		lvl, e := createLevel(record, flds, feed, idprefix)
		if e == nil {
			if _, ok := feed.Levels[lvl.Id]; ok {
				e = errors.New("ID collision, level_id '" + lvl.Id + "' already used.")
			}
		}

		if e != nil {
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedLevels++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}
		feed.Levels[lvl.Id] = lvl

		if feed.opts.ShowWarnings && !isValidId(lvl.Id) {
			feed.warnLimited("non_ascii_or_non_printable_char", fmt.Errorf("non_ascii_or_non_printable_char: level_id '%s' contains non-ASCII or non-printable characters", lvl.Id))
		}

		for _, i := range addFlds {
			if i < len(record) {
				if _, ok := feed.LevelsAddFlds[reader.header[i]]; !ok {
					feed.LevelsAddFlds[reader.header[i]] = make(map[string]string)
				}

				feed.LevelsAddFlds[reader.header[i]][lvl.Id] = record[i]
			}
		}
	}

	feed.ColOrders.Levels = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) parseFeedInfos(path string) (err error) {
	file, e := feed.getFile(path, "feed_info.txt")

	if e != nil {
		return nil
	}
	reader := NewCsvParser(file, feed.opts.DropErroneous, false)

	defer func() {
		if r := recover(); r != nil {
			err = ParseError{"feed_info.txt", reader.Curline, r.(error).Error()}
		}
	}()

	var record []string
	flds := FeedInfoFields{
		feedPublisherName: reader.headeridx.GetFldId("feed_publisher_name", -1),
		feedPublisherUrl:  reader.headeridx.GetFldId("feed_publisher_url", -2),
		feedLang:          reader.headeridx.GetFldId("feed_lang", -3),
		feedStartDate:     reader.headeridx.GetFldId("feed_start_date", -4),
		feedEndDate:       reader.headeridx.GetFldId("feed_end_date", -5),
		feedVersion:       reader.headeridx.GetFldId("feed_version", -6),
		feedContactEmail:  reader.headeridx.GetFldId("feed_contact_email", -7),
		feedContactUrl:    reader.headeridx.GetFldId("feed_contact_url", -8),
	}

	addFlds := make([]int, 0)

	if feed.opts.KeepAddFlds {
		addFlds = addiFields(reader.header, flds)
	}

	for record = reader.ParseCsvLine(); record != nil; record = reader.ParseCsvLine() {
		fi, e := createFeedInfo(record, flds, feed)
		if e != nil {
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedFeedInfos++
				feed.warn(e)
				continue
			} else {
				panic(e)
			}
		}
		if !feed.opts.DryRun {
			for _, i := range addFlds {
				if i < len(record) {
					if _, ok := feed.FeedInfosAddFlds[reader.header[i]]; !ok {
						feed.FeedInfosAddFlds[reader.header[i]] = make(map[*gtfs.FeedInfo]string)
					}

					feed.FeedInfosAddFlds[reader.header[i]][fi] = record[i]
				}
			}
			feed.FeedInfos = append(feed.FeedInfos, fi)

			if feed.opts.ShowWarnings {
				if (fi.Start_date.IsEmpty()) != (fi.End_date.IsEmpty()) {
					feed.warnLimited("missing_feed_info_date", fmt.Errorf("missing_feed_info_date: feed_info has only one of feed_start_date / feed_end_date; both should be provided if either is set"))
				}
				if (fi.Contact_email == nil || fi.Contact_email.Address == "") &&
					(fi.Contact_url == nil || fi.Contact_url.String() == "") {
					feed.warnLimited("missing_feed_contact_email_and_url", fmt.Errorf("missing_feed_contact_email_and_url: feed_info provides neither feed_contact_email nor feed_contact_url"))
				}

				if !fi.Start_date.IsEmpty() && !fi.End_date.IsEmpty() &&
					fi.Start_date.GetTime().After(fi.End_date.GetTime()) {
					feed.warnLimited("start_and_end_range_out_of_order", fmt.Errorf("start_and_end_range_out_of_order: feed_info has feed_start_date (%s) after feed_end_date (%s)",
						fi.Start_date.GetTime().Format("20060102"),
						fi.End_date.GetTime().Format("20060102")))
				}
			}
		}
	}

	feed.ColOrders.FeedInfos = append([]string(nil), reader.header...)

	return e
}

func (feed *Feed) checkShapeMeasure(shape *gtfs.Shape, opt *ParseOptions) error {
	max := float32(math.Inf(-1))
	deleted := 0
	for j := 1; j < len(shape.Points)+deleted; j++ {
		i := j - deleted

		if shape.Points[i-1].Sequence == shape.Points[i].Sequence {
			e := fmt.Errorf("In shape '%s' for point with seq=%d: stop time sequence collision. Sequence has to increase along shape.", shape.Id, shape.Points[i].Sequence)
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedStopTimes++
				shape.Points = shape.Points[:i+copy(shape.Points[i:], shape.Points[i+1:])]
				feed.warn(e)
				deleted++
				continue
			} else {
				return e
			}
		}

		if shape.Points[i-1].HasDistanceTraveled() && shape.Points[i-1].Dist_traveled > max {
			max = shape.Points[i-1].Dist_traveled
		}

		if shape.Points[i].HasDistanceTraveled() && max > shape.Points[i].Dist_traveled {
			e := fmt.Errorf("In shape '%s' for point with seq=%d shape_dist_traveled does not increase along with stop_sequence (%f > %f)", shape.Id, shape.Points[i].Sequence, max, shape.Points[i].Dist_traveled)
			if opt.UseDefValueOnError {
				shape.Points[i].Dist_traveled = float32(math.NaN())
				feed.warn(e)
			} else if opt.DropErroneous {
				feed.ErrorStats.DroppedShapes++
				feed.warn(e)
				shape.Points = shape.Points[:i+copy(shape.Points[i:], shape.Points[i+1:])]
				deleted++
			} else {
				return e
			}
		}
	}
	return nil
}

func (feed *Feed) checkStopTimeMeasure(trip *gtfs.Trip, opt *ParseOptions) error {
	max := float32(math.Inf(-1))
	deleted := 0
	for j := 1; j < len(trip.StopTimes)+deleted; j++ {
		i := j - deleted

		if trip.StopTimes[i-1].Sequence() == trip.StopTimes[i].Sequence() {
			e := fmt.Errorf("In trip '%s' for stoptime with seq=%d: stop time sequence collision. Sequence has to increase along trip.", trip.Id, trip.StopTimes[i].Sequence())
			if feed.opts.DropErroneous {
				feed.ErrorStats.DroppedStopTimes++
				trip.StopTimes = trip.StopTimes[:i+copy(trip.StopTimes[i:], trip.StopTimes[i+1:])]
				feed.warn(e)
				deleted++
				continue
			} else {
				return e
			}
		}

		if !trip.StopTimes[i-1].Departure_time().Empty() && !trip.StopTimes[i].Arrival_time().Empty() && trip.StopTimes[i-1].Departure_time().SecondsSinceMidnight() > trip.StopTimes[i].Arrival_time().SecondsSinceMidnight() {
			e := fmt.Errorf("In trip '%s' for stoptime with seq=%d the arrival time is before the departure in the previous station", trip.Id, trip.StopTimes[i].Sequence())
			if opt.DropErroneous {
				feed.ErrorStats.DroppedStopTimes++
				trip.StopTimes = trip.StopTimes[:i+copy(trip.StopTimes[i:], trip.StopTimes[i+1:])]
				feed.warn(e)
				deleted++
				continue
			} else {
				return e
			}
		}

		if trip.StopTimes[i-1].HasDistanceTraveled() && trip.StopTimes[i-1].Shape_dist_traveled() > max {
			max = trip.StopTimes[i-1].Shape_dist_traveled()
		}

		if trip.StopTimes[i].HasDistanceTraveled() && max > trip.StopTimes[i].Shape_dist_traveled() {
			e := fmt.Errorf("In trip '%s' for stoptime with seq=%d shape_dist_traveled does not increase along with stop_sequence (%f > %f)", trip.Id, trip.StopTimes[i].Sequence(), max, trip.StopTimes[i].Shape_dist_traveled())
			if opt.UseDefValueOnError {
				trip.StopTimes[i].SetShape_dist_traveled(float32(math.NaN()))
				feed.warn(e)
			} else if opt.DropErroneous {
				trip.StopTimes = trip.StopTimes[:i+copy(trip.StopTimes[i:], trip.StopTimes[i+1:])]
				feed.ErrorStats.DroppedStopTimes++
				feed.warn(e)
				deleted++
				continue
			} else {
				return e
			}
		}
	}
	return nil
}

func (p *Polygon) PolyContains(x float64, y float64) bool {
	if len(p.OuterRing) == 0 {
		return false
	}

	// first check if contained in bounding box
	if x < p.ll[0] || x > p.ur[0] || y < p.ll[1] || y > p.ur[1] {
		return false
	}

	// see https://de.wikipedia.org/wiki/Punkt-in-Polygon-Test_nach_Jordan
	c := int8(-1)

	for i := 1; i < len(p.OuterRing); i++ {
		c *= polyContCheck(x, y, p.OuterRing[i-1][0], p.OuterRing[i-1][1], p.OuterRing[i][0], p.OuterRing[i][1])
		if c == 0 {
			return true
		}
	}

	c *= polyContCheck(x, y, p.OuterRing[len(p.OuterRing)-1][0], p.OuterRing[len(p.OuterRing)-1][1], p.OuterRing[0][0], p.OuterRing[0][1])

	if c < 0 {
		return false
	}

	for _, innerRing := range p.InnerRings {
		c = int8(-1)

		for i := 1; i < len(innerRing); i++ {
			c *= polyContCheck(x, y, innerRing[i-1][0], innerRing[i-1][1], innerRing[i][0], innerRing[i][1])
			if c == 0 {
				return false
			}
		}

		c *= polyContCheck(x, y, innerRing[len(innerRing)-1][0], innerRing[len(innerRing)-1][1], innerRing[0][0], innerRing[0][1])

		if c >= 0 {
			return false
		}
	}

	return true
}

func polyContCheck(ax float64, ay float64, bx float64, by float64, cx float64, cy float64) int8 {
	EPSILON := 0.00000001
	if ay == by && ay == cy {
		if !((bx <= ax && ax <= cx) ||
			(cx <= ax && ax <= bx)) {
			return 1
		}
		return 0
	}
	if math.Abs(ay-by) < EPSILON &&
		math.Abs(ax-by) < EPSILON {
		return 0
	}

	if by > cy {
		tmpx := bx
		tmpy := by
		bx = cx
		by = cy
		cx = tmpx
		cy = tmpy
	}

	if ay <= by || ay > cy {
		return 1
	}

	d := (bx-ax)*(cy-ay) -
		(by-ay)*(cx-ax)

	if d > 0 {
		return -1
	}
	if d < 0 {
		return 1
	}
	return 0
}

func (feed *Feed) getGTFSDir(zip *zip.ReadCloser) string {
	// count number of GTFS file occurances in folders,
	// return the folder with the most GTFS files

	pathm := make(map[string]int)
	files := map[string]bool{
		"agency.txt":          true,
		"stops.txt":           true,
		"routes.txt":          true,
		"trips.txt":           true,
		"stop_times.txt":      true,
		"calendar.txt":        true,
		"calendar_dates.txt":  true,
		"fare_attributes.txt": true,
		"fare_rules.txt":      true,
		"shapes.txt":          true,
		"frequencies.txt":     true,
		"transfers.txt":       true,
		"pathways.txt":        true,
		"levels.txt":          true,
		"feed_info.txt":       true,
	}

	for _, f := range feed.zipFileCloser.File {
		dir, name := opath.Split(f.Name)
		if files[name] {
			pathm[dir] = pathm[dir] + 1
		}
	}

	ret := ""
	max := 0
	for dir := range pathm {
		if pathm[dir] > max {
			max = pathm[dir]
			ret = dir
		}
	}

	return ret
}

const maxWarningsPerType = 20

func (feed *Feed) warnLimited(code string, e error) {
	feed.warnCounts[code]++
	n := feed.warnCounts[code]
	if n < maxWarningsPerType {
		feed.warn(e)
	} else if n == maxWarningsPerType {
		feed.warn(e)
		fmt.Fprintf(os.Stderr, "WARNING: further '%s' warnings suppressed (>= %d occurrences)\n", code, maxWarningsPerType)
	}
	// n > maxWarningsPerType: silently counted but not printed
}

func (feed *Feed) warn(e error) {
	if feed.opts.ShowWarnings {
		fmt.Fprintln(os.Stderr, "WARNING: "+e.Error())
	}
}

func (feed *Feed) DeletePathway(id string) {
	delete(feed.FareAttributes, id)

	// delete additional fields from CSV
	for k := range feed.PathwaysAddFlds {
		delete(feed.PathwaysAddFlds[k], id)
	}
}

func (feed *Feed) DeleteFareAttribute(id string) {
	delete(feed.FareAttributes, id)

	// delete additional fields from CSV
	for k := range feed.FareRulesAddFlds {
		delete(feed.FareRulesAddFlds[k], id)
	}

	for k := range feed.FareAttributesAddFlds {
		delete(feed.FareAttributesAddFlds[k], id)
	}
}

func (feed *Feed) DeleteTrip(id string) {
	if _, ok := feed.Trips[id]; ok {
		feed.NumStopTimes -= len(feed.Trips[id].StopTimes)
		delete(feed.Trips, id)
	}

	// delete additional fields from CSV
	for k := range feed.TripsAddFlds {
		delete(feed.TripsAddFlds[k], id)
	}

	for k := range feed.StopTimesAddFlds {
		delete(feed.StopTimesAddFlds[k], id)
	}

	for k := range feed.FrequenciesAddFlds {
		delete(feed.FrequenciesAddFlds[k], id)
	}
}

func (feed *Feed) DeleteShape(id string) {
	delete(feed.Shapes, id)

	// delete additional fields from CSV
	for k := range feed.ShapesAddFlds {
		delete(feed.ShapesAddFlds[k], id)
	}
}

func (feed *Feed) DeleteAgency(id string) {
	delete(feed.Agencies, id)

	// delete additional fields from CSV
	for k := range feed.AgenciesAddFlds {
		delete(feed.AgenciesAddFlds[k], id)
	}
}

func (feed *Feed) DeleteRoute(id string) {
	delete(feed.Routes, id)

	// delete additional fields from CSV
	for k := range feed.RoutesAddFlds {
		delete(feed.RoutesAddFlds[k], id)
	}
}

func (feed *Feed) DeleteLevel(id string) {
	delete(feed.Levels, id)

	// delete additional fields from CSV
	for k := range feed.LevelsAddFlds {
		delete(feed.LevelsAddFlds[k], id)
	}
}

func (feed *Feed) DeleteStop(id string) {
	delete(feed.Stops, id)

	// delete additional fields from CSV
	for k := range feed.StopsAddFlds {
		delete(feed.StopsAddFlds[k], id)
	}
}

func (feed *Feed) DeleteTransfer(tk gtfs.TransferKey) {
	delete(feed.Transfers, tk)

	// delete additional fields from CSV
	for k := range feed.TransfersAddFlds {
		delete(feed.TransfersAddFlds[k], tk)
	}
}

func (feed *Feed) CleanTransfers() {
	for tk := range feed.Transfers {
		if tk.From_stop != nil {
			if _, in := feed.Stops[tk.From_stop.Id]; !in {
				feed.DeleteTransfer(tk)
				continue
			}
		}
		if tk.To_stop != nil {
			if _, in := feed.Stops[tk.To_stop.Id]; !in {
				feed.DeleteTransfer(tk)
				continue
			}
		}
		if tk.From_route != nil {
			if _, in := feed.Routes[tk.From_route.Id]; !in {
				feed.DeleteTransfer(tk)
				continue
			}
		}
		if tk.To_route != nil {
			if _, in := feed.Routes[tk.To_route.Id]; !in {
				feed.DeleteTransfer(tk)
				continue
			}
		}

		if tk.From_trip != nil {
			if _, in := feed.Trips[tk.From_trip.Id]; !in {
				feed.DeleteTransfer(tk)
				continue
			}
		}

		if tk.To_trip != nil {
			if _, in := feed.Trips[tk.To_trip.Id]; !in {
				feed.DeleteTransfer(tk)
				continue
			}
		}
	}
}

func (feed *Feed) DeleteService(id string) {
	delete(feed.Services, id)
}

func (feed *Feed) warnDuplicateUrls() {
	agencyUrls := make(map[string]string)
	for _, ag := range feed.Agencies {
		if ag.Url != nil {
			agencyUrls[ag.Url.String()] = ag.Id
		}
	}

	routeUrls := make(map[string]string)
	for _, route := range feed.Routes {
		if route.Url != nil {
			s := route.Url.String()
			if agId, ok := agencyUrls[s]; ok {
				feed.warnLimited("same_route_and_agency_url", fmt.Errorf("same_route_and_agency_url: route '%s' has the same URL as agency '%s' ('%s')", route.Id, agId, s))
			}
			routeUrls[s] = route.Id
		}
	}

	for _, stop := range feed.Stops {
		if stop.Url != nil {
			s := stop.Url.String()
			if agId, ok := agencyUrls[s]; ok {
				feed.warnLimited("same_stop_and_agency_url", fmt.Errorf("same_stop_and_agency_url: stop '%s' has the same URL as agency '%s' ('%s')", stop.Id, agId, s))
			}
			if routeId, ok := routeUrls[s]; ok {
				feed.warnLimited("same_stop_and_route_url", fmt.Errorf("same_stop_and_route_url: stop '%s' has the same URL as route '%s' ('%s')", stop.Id, routeId, s))
			}
		}
	}
}

func (feed *Feed) warnAgencyLangConsistency() {
	langSet := make(map[string]struct{})
	for _, ag := range feed.Agencies {
		if ag.Lang.GetLangString() != "" {
			langSet[ag.Lang.GetLangString()] = struct{}{}
		}
	}

	if len(langSet) > 1 {
		langs := make([]string, 0, len(langSet))
		for l := range langSet {
			langs = append(langs, l)
		}
		sort.Strings(langs)
		// this is already caught when reading agency.txt, but i'll leave it here
		feed.warnLimited("inconsistent_agency_lang", fmt.Errorf("inconsistent_agency_lang: agencies have different languages: %s",
			strings.Join(langs, ", ")))
	}

	if len(feed.FeedInfos) == 0 {
		return
	}

	for _, fi := range feed.FeedInfos {
		if fi.Lang.GetLangString() == "" {
			continue
		}
		feedLang := fi.Lang.GetLangString()
		isMul := feedLang == "mul"

		if isMul && len(langSet) <= 1 {
			feed.warnLimited("feed_info_lang_and_agency_lang_mismatch", fmt.Errorf("feed_info_lang_and_agency_lang_mismatch: feed_lang is 'mul' but there are not multiple distinct agency languages"))
			continue
		}

		if !isMul {
			if len(langSet) > 1 {
				feed.warnLimited("feed_info_lang_and_agency_lang_mismatch", fmt.Errorf("feed_info_lang_and_agency_lang_mismatch: feed_lang is '%s' but there are multiple distinct agency languages; consider using 'mul'", feedLang))
				continue
			}
			for agLang := range langSet {
				if agLang != feedLang {
					feed.warnLimited("feed_info_lang_and_agency_lang_mismatch", fmt.Errorf("feed_info_lang_and_agency_lang_mismatch: feed_lang '%s' does not match agency_lang '%s'", feedLang, agLang))
				}
			}
		}
	}
}

func (feed *Feed) warnBlockTrips() {
	blockIdRouteType := make(map[string]int16)
	blocks := make(map[string][]*gtfs.Trip)

	for _, trip := range feed.Trips {
		if trip.Block_id != nil {
			bid := *trip.Block_id

			// inconsistent route types within a block
			if prevRt, ok := blockIdRouteType[bid]; ok {
				if prevRt != trip.Route.Type {
					feed.warnLimited("inconsistent_route_type_within_block", fmt.Errorf("inconsistent_route_type_within_block: inconsistent route types for block_id '%s': found %d and %d",
						bid, prevRt, trip.Route.Type))
				}
			} else {
				blockIdRouteType[bid] = trip.Route.Type
			}

			// accumulate for overlap check
			blocks[bid] = append(blocks[bid], trip)
		}
	}

	// block_trips_with_overlapping_stop_times
	for bid, trips := range blocks {
		for i := 0; i < len(trips); i++ {
			if len(trips[i].StopTimes) < 2 {
				continue
			}
			aFirst := trips[i].StopTimes[0].Arrival_time()
			aLast := trips[i].StopTimes[len(trips[i].StopTimes)-1].Departure_time()
			if aFirst.Empty() || aLast.Empty() {
				continue
			}

			for j := i + 1; j < len(trips); j++ {
				if len(trips[j].StopTimes) < 2 {
					continue
				}
				bFirst := trips[j].StopTimes[0].Arrival_time()
				bLast := trips[j].StopTimes[len(trips[j].StopTimes)-1].Departure_time()
				if bFirst.Empty() || bLast.Empty() {
					continue
				}

				aStart := aFirst.SecondsSinceMidnight()
				aEnd := aLast.SecondsSinceMidnight()
				bStart := bFirst.SecondsSinceMidnight()
				bEnd := bLast.SecondsSinceMidnight()

				if aStart < bEnd && bStart < aEnd {
					feed.warnLimited("block_trips_with_overlapping_stop_times", fmt.Errorf("block_trips_with_overlapping_stop_times: trips '%s' and '%s' in block '%s' have overlapping stop times (%02d:%02d-%02d:%02d and %02d:%02d-%02d:%02d)",
						trips[i].Id, trips[j].Id, bid,
						aFirst.Hour, aFirst.Minute, aLast.Hour, aLast.Minute,
						bFirst.Hour, bFirst.Minute, bLast.Hour, bLast.Minute))
				}
			}
		}
	}
}

func (feed *Feed) warnPathwayReachability(hasBoardingArea map[string]bool) {
	if len(feed.Pathways) == 0 {
		return
	}

	fwd := make(map[string][]string)
	bwd := make(map[string][]string)
	neighbours := make(map[string]map[string]struct{})

	for _, pw := range feed.Pathways {
		if pw.From_stop == nil || pw.To_stop == nil {
			continue
		}
		fid := pw.From_stop.Id
		tid := pw.To_stop.Id
		fwd[fid] = append(fwd[fid], tid)
		bwd[tid] = append(bwd[tid], fid)
		if pw.Is_bidirectional {
			fwd[tid] = append(fwd[tid], fid)
			bwd[fid] = append(bwd[fid], tid)
		}

		// accumulate neighbours for dangling generic node check
		for _, stop := range []*gtfs.Stop{pw.From_stop, pw.To_stop} {
			if stop.Location_type == 3 {
				if _, ok := neighbours[stop.Id]; !ok {
					neighbours[stop.Id] = make(map[string]struct{})
				}
			}
		}
		if pw.From_stop.Location_type == 3 {
			neighbours[pw.From_stop.Id][tid] = struct{}{}
		}
		if pw.To_stop.Location_type == 3 {
			neighbours[pw.To_stop.Id][fid] = struct{}{}
		}
	}

	// pathway_dangling_generic_node: catch generic nodes with no pathways at all
	for _, s := range feed.Stops {
		if s.Location_type == 3 {
			if _, ok := neighbours[s.Id]; !ok {
				neighbours[s.Id] = make(map[string]struct{})
			}
		}
	}
	for id, nbrs := range neighbours {
		if len(nbrs) <= 1 {
			feed.warnLimited("pathway_dangling_generic_node", fmt.Errorf("pathway_dangling_generic_node: generic node '%s' has only %d incident location(s) in the pathway graph and is therefore useless",
				id, len(nbrs)))
		}
	}

	entrances := make(map[string]struct{})
	candidates := make(map[string]*gtfs.Stop)

	for _, s := range feed.Stops {
		switch s.Location_type {
		case 2:
			entrances[s.Id] = struct{}{}
		case 0:
			if !hasBoardingArea[s.Id] {
				candidates[s.Id] = s
			}
		case 3, 4:
			candidates[s.Id] = s
		}
	}

	if len(entrances) == 0 {
		return
	}

	reachableFromEntrance := bfsReach(entrances, fwd)
	reachableToEntrance := bfsReach(entrances, bwd)

	for id, stop := range candidates {
		if _, ok := reachableFromEntrance[id]; !ok {
			feed.warnLimited("pathway_unreachable_location", fmt.Errorf("pathway_unreachable_location: stop '%s' (location_type=%d) is not reachable from any entrance",
				stop.Id, stop.Location_type))
		} else if _, ok := reachableToEntrance[id]; !ok {
			feed.warnLimited("pathway_unreachable_location", fmt.Errorf("pathway_unreachable_location: stop '%s' (location_type=%d) cannot reach any exit",
				stop.Id, stop.Location_type))
		}
	}
}

func bfsReach(seeds map[string]struct{}, graph map[string][]string) map[string]struct{} {
	visited := make(map[string]struct{}, len(seeds)*4)
	queue := make([]string, 0, len(seeds)*4)
	for id := range seeds {
		visited[id] = struct{}{}
		queue = append(queue, id)
	}
	for i := 0; i < len(queue); i++ {
		for _, next := range graph[queue[i]] {
			if _, seen := visited[next]; !seen {
				visited[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return visited
}

func (feed *Feed) warnUnusedStations() {
	referenced := make(map[string]struct{}, len(feed.Stops))
	stations := make([]*gtfs.Stop, 0)
	for _, s := range feed.Stops {
		if s.Parent_station != nil {
			referenced[s.Parent_station.Id] = struct{}{}
		}
		if s.Location_type == 1 {
			stations = append(stations, s)
		}
	}
	for _, s := range stations {
		if _, ok := referenced[s.Id]; !ok {
			feed.warnLimited("unused_station", fmt.Errorf("unused_station: stop '%s' has location_type=1 (station) but is not referenced as a parent_station by any stop", s.Id))
		}
	}
}
func (feed *Feed) warnUnusedShapesAndTripsAndStops() {
	referencedShapes := make(map[string]struct{})
	tripsWithStopTimes := make(map[string]struct{})
	referencedStops := make(map[string]struct{})

	for _, trip := range feed.Trips {
		if trip.Shape != nil {
			referencedShapes[trip.Shape.Id] = struct{}{}
		}
		for i := range trip.StopTimes {
			if s := trip.StopTimes[i].Stop(); s != nil {
				referencedStops[s.Id] = struct{}{}
			}
		}
		if len(trip.StopTimes) > 0 {
			tripsWithStopTimes[trip.Id] = struct{}{}
		}
	}

	for id := range feed.Shapes {
		if _, ok := referencedShapes[id]; !ok {
			feed.warnLimited("unused_shape", fmt.Errorf("unused_shape: shape '%s' is defined in shapes.txt but not referenced by any trip", id))
		}
	}
	for id := range feed.Trips {
		if _, ok := tripsWithStopTimes[id]; !ok {
			feed.warnLimited("unused_trip", fmt.Errorf("unused_trip: trip '%s' is not referenced by any stop time", id))
		}
	}
	for _, stop := range feed.Stops {
		if stop.Location_type != 0 {
			continue
		}
		if _, ok := referencedStops[stop.Id]; !ok {
			feed.warnLimited("stop_without_stop_time", fmt.Errorf("stop_without_stop_time: stop '%s' ('%s') is not referenced by any stop time",
				stop.Id, stop.Name))
		}
	}
}

func (feed *Feed) warnExpiredCalendars() {
	now := time.Now()

	for _, svc := range feed.Services {
		if svc == nil {
			continue
		}

		// check calendar.txt-style services with explicit date ranges
		if !svc.End_date().IsEmpty() && svc.End_date().GetTime().Before(now) {
			feed.warnLimited("expired_calendar", fmt.Errorf("expired_calendar: service '%s' end date %s is in the past",
				svc.Id(), svc.End_date().GetTime().Format("20060102")))
			continue
		}

		// check calendar_dates.txt-style services: warn if no active date is
		// in the future (GetFirstActiveDate returns empty if all dates are past)
		if svc.End_date().IsEmpty() {
			lastActive := svc.GetLastActiveDate()
			if !lastActive.IsEmpty() && lastActive.GetTime().Before(now) {
				feed.warnLimited("expired_calendar", fmt.Errorf("expired_calendar: service '%s' has no active dates in the future (last active: %s)",
					svc.Id(), lastActive.GetTime().Format("20060102")))
			}
		}
	}
}
