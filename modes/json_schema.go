// Package modes implements Mode S message decoding and output formats.
//
// json_schema.go: shared JSON schema generation for HTTP and file output
//
// This provides a single source of truth for receiver.json and stats.json
// generation used by both HTTP handlers and the file writer.
package modes

import (
	"math"
)

// ReceiverJSONParams holds parameters for generating receiver.json.
type ReceiverJSONParams struct {
	Version          string
	RefreshMs        int     // refresh interval in milliseconds
	History          int
	Lat              float64
	Lon              float64
	LocationAccuracy int     // 0=none, 1=rough (2dp), 2=exact
}

// GenerateReceiverJSONWithAccuracy generates receiver.json with refresh in
// milliseconds and location accuracy applied. This is the shared generator
// used by both HTTP handlers and the file writer.
func GenerateReceiverJSONWithAccuracy(params ReceiverJSONParams) *ReceiverJSON {
	r := &ReceiverJSON{
		Version: params.Version,
		Refresh: float64(params.RefreshMs),
		History: params.History,
	}

	if params.LocationAccuracy > 0 && (params.Lat != 0 || params.Lon != 0) {
		if params.LocationAccuracy == 1 {
			r.Lat = math.Round(params.Lat*100) / 100
			r.Lon = math.Round(params.Lon*100) / 100
		} else {
			r.Lat = params.Lat
			r.Lon = params.Lon
		}
	}

	return r
}
