// Package modes provides decoder configuration for ICAO-aware message decoding.
package modes

// DecodeConfig configures message decoding with ICAO filtering and metadata.
type DecodeConfig struct {
	ICAOFilter   *ICAOFilter
	Remote       bool
	TimestampMsg uint64
	SignalLevel  float64
}

// DecodeModesMessageWithConfig decodes a raw Mode S message using the provided config.
// It applies ICAO filtering and attaches Remote/Timestamp/Signal metadata to the result.
func DecodeModesMessageWithConfig(msg []byte, cfg DecodeConfig) (*Message, int) {
	mm, result := DecodeModesMessageWithFilter(msg, cfg.ICAOFilter)
	if mm == nil || result < 0 {
		return mm, result
	}
	mm.Remote = cfg.Remote
	mm.TimestampMsg = cfg.TimestampMsg
	mm.SignalLevel = cfg.SignalLevel
	if mm.Remote && mm.TimestampMsg == MAGIC_MLAT_TIMESTAMP {
		mm.Source = SOURCE_MLAT
	}
	return mm, result
}
