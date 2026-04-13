// Copyright 2015 geOps
// Authors: patrick.brosi@geops.de
//
// Use of this source code is governed by a GPL v2
// license that can be found in the LICENSE file

package gtfsparser

import (
	"bufio"
	"encoding/csv"
	"io"
	"strings"
)

type HeaderIdx map[string]int

func (d HeaderIdx) GetFldId(key string, def int) (result int) {
	if v, ok := d[key]; ok {
		return v
	} else {
		return def
	}
}

// CsvParser is a wrapper around csv.Reader
type CsvParser struct {
	header      []string
	headeridx   HeaderIdx
	ret         map[string]string
	reader      *csv.Reader
	Curline     int
	silentfail  bool
	assumeclean bool
	scanner     *bufio.Scanner
	record      []string
}

// NewCsvParser creates a new CsvParser
func NewCsvParser(file io.Reader, silentfail bool, assumeclean bool) CsvParser {
	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	p := CsvParser{reader: reader}
	p.assumeclean = assumeclean

	if p.assumeclean {
		p.scanner = bufio.NewScanner(file)
		p.record = make([]string, 999)
	}

	p.parseHeader()
	p.silentfail = silentfail

	if p.assumeclean {
		p.record = make([]string, len(p.header))
	}

	return p
}

func (c *CsvParser) GetHeader() []string {
	return c.header
}

// ParseRecord reads a single line into a map
func (p *CsvParser) ParseRecord() map[string]string {
	l := p.ParseCsvLine()

	if l == nil {
		return nil
	}

	for i, e := range p.header {
		if i >= len(l) {
			p.ret[e] = ""
		} else {
			p.ret[e] = l[i]
		}
	}

	return p.ret
}

func (p *CsvParser) ParseCsvLine() []string {
	// TODO: this does not capture empty CSV lines and comments, as they are skipped
	// automatically by the CSV reader, and the internal line counter of the CSV reader
	// is not accessible.
	p.Curline++

	if p.assumeclean {
		have := p.scanner.Scan()

		if !have {
			return nil
		} else if p.scanner.Err() != nil {
			if p.silentfail {
				return nil
			} else {
				panic(p.scanner.Err())
			}
		}

		return strings.Split(p.scanner.Text(), ",")
	}

	record, err := p.reader.Read()

	// handle byte order marks
	if len(record) > 0 {
		a := len(record[0])
		// utf 8
		if a > 2 && record[0][0] == 239 && record[0][1] == 187 && record[0][2] == 191 {
			record[0] = record[0][3:]

			// utf 16 BE
		} else if a > 1 && record[0][0] == 254 && record[0][1] == 255 {
			record[0] = record[0][2:]

			// utf 16 LE
		} else if a > 1 && record[0][0] == 255 && record[0][1] == 254 {
			record[0] = record[0][2:]
		}
	}

	if err == io.EOF {
		return nil
	} else if err != nil {
		if p.silentfail {
			return nil
		} else {
			panic(err)
		}
	}

	// trim
	for i, r := range record {
		if len(record[i]) > 0 {
			record[i] = strings.TrimSpace(r)
		}
	}

	return record
}

func (p *CsvParser) parseHeader() {
	rec := p.ParseCsvLine()
	p.header = make([]string, len(rec))
	p.headeridx = make(HeaderIdx, len(rec))
	p.ret = make(map[string]string, len(rec))
	copy(p.header, rec)

	for i, header := range rec {
		header = strings.Trim(header, `"`)
		p.header[i] = header
		p.ret[header] = ""
		p.headeridx[header] = i
	}
}
