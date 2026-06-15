// Package modes implements Mode S message decoding and output formats.
//
// json_schema.go: shared JSON schema generation for HTTP and file output
//
// This provides a single source of truth for receiver.json and stats.json
// generation used by both HTTP handlers and the file writer.
package modes

import (
	"fmt"
	"math"
	"time"
)

// ReceiverJSONParams holds parameters for generating receiver.json.
type ReceiverJSONParams struct {
	Version          string
	RefreshMs        int // refresh interval in milliseconds
	History          int
	Lat              float64
	Lon              float64
	LocationAccuracy int // 0=none, 1=rough (2dp), 2=exact
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

// GenerateStatsJSON generates stats.json content with the full schema
// (latest, last1min, last5min, last15min, total). If sc is nil, produces
// a minimal skeleton with empty blocks. This is the shared generator used
// by both HTTP handlers and the file writer.
func GenerateStatsJSON(sc *StatsCollector) []byte {
	if sc == nil {
		now := time.Now()
		var buf []byte
		buf = append(buf, fmt.Sprintf("{ \"latest\" : { \"start\" : %.1f, \"end\" : %.1f }",
			float64(now.Unix()), float64(now.Unix()))...)
		buf = append(buf, fmt.Sprintf(", \"last1min\" : { \"start\" : %.1f, \"end\" : %.1f }",
			float64(now.Unix()), float64(now.Unix()))...)
		buf = append(buf, ", \"last5min\" : { \"start\" : 0, \"end\" : 0 }"...)
		buf = append(buf, ", \"last15min\" : { \"start\" : 0, \"end\" : 0 }"...)
		buf = append(buf, ", \"total\" : { \"start\" : 0, \"end\" : 0 }"...)
		buf = append(buf, "\n}\n"...)
		return buf
	}

	latest := sc.GetLatest()
	last1min := sc.GetLast1Min()
	last5min := sc.GetLast5Min()
	last15min := sc.GetLast15Min()
	total := sc.GetAllTime()

	buf := make([]byte, 0, 4096)
	buf = append(buf, "{\n"...)
	buf = append(buf, "  \"latest\" : "...)
	buf = append(buf, formatStatsBlock(&latest, true)...)
	buf = append(buf, ",\n"...)
	buf = append(buf, "  \"last1min\" : "...)
	buf = append(buf, formatStatsBlock(&last1min, true)...)
	buf = append(buf, ",\n"...)
	buf = append(buf, "  \"last5min\" : "...)
	buf = append(buf, formatStatsBlock(&last5min, false)...)
	buf = append(buf, ",\n"...)
	buf = append(buf, "  \"last15min\" : "...)
	buf = append(buf, formatStatsBlock(&last15min, false)...)
	buf = append(buf, ",\n"...)
	buf = append(buf, "  \"total\" : "...)
	buf = append(buf, formatStatsBlock(&total, false)...)
	buf = append(buf, "\n}\n"...)
	return buf
}

// GenerateStatsJSONWithMessages is a convenience wrapper for callers that
// only have a total message count and no StatsCollector. It produces a minimal
// full-schema stats JSON with the message count embedded in each block.
func GenerateStatsJSONWithMessages(totalMessages uint64) []byte {
	now := time.Now()
	var buf []byte
	buf = append(buf, fmt.Sprintf("{ \"latest\" : { \"start\" : %.1f, \"end\" : %.1f, \"messages\" : %d }",
		float64(now.Unix()), float64(now.Unix()), totalMessages)...)
	buf = append(buf, fmt.Sprintf(", \"last1min\" : { \"start\" : %.1f, \"end\" : %.1f, \"messages\" : %d }",
		float64(now.Unix()), float64(now.Unix()), totalMessages)...)
	buf = append(buf, fmt.Sprintf(", \"last5min\" : { \"start\" : 0, \"end\" : 0, \"messages\" : %d }", totalMessages)...)
	buf = append(buf, fmt.Sprintf(", \"last15min\" : { \"start\" : 0, \"end\" : 0, \"messages\" : %d }", totalMessages)...)
	buf = append(buf, fmt.Sprintf(", \"total\" : { \"start\" : 0, \"end\" : 0, \"messages\" : %d }", totalMessages)...)
	buf = append(buf, "\n}\n"...)
	return buf
}
