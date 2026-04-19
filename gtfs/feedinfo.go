// Copyright 2015 geOps
// Authors: patrick.brosi@geops.de
//
// Use of this source code is governed by a GPL v2
// license that can be found in the LICENSE file

package gtfs

import (
	mail "net/mail"
	url "net/url"
)

// FeedInfo holds general information about a GTFS feed
type FeedInfo struct {
	Publisher_name string
	Publisher_url  *url.URL
	Lang           LanguageISO6391
	Start_date     Date
	End_date       Date
	Version        string
	Contact_email  *mail.Address
	Contact_url    *url.URL
}
