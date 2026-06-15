// Package modes defines core data structures for Mode S message decoding.
//
// This file contains types translated from dump1090-mutability's dump1090.h,
// maintaining compatibility with the C implementation.
//
// Original Copyright (c) 2014-2016 Oliver Jowett <oliver@mutability.co.uk>
// and Copyright (C) 2012 Salvatore Sanfilippo <antirez@gmail.com>
// Licensed under GPL v2+ / BSD
package modes

import "time"

// Message lengths and constants
const (
	MODES_SHORT_MSG_BYTES = 7
	MODES_LONG_MSG_BYTES  = 14

	MODES_PREAMBLE_US      = 8
	MODES_PREAMBLE_SAMPLES = MODES_PREAMBLE_US * 2

	MODES_LONG_MSG_SAMPLES  = MODES_LONG_MSG_BITS * 2
	MODES_SHORT_MSG_SAMPLES = MODES_SHORT_MSG_BITS * 2

	INVALID_ALTITUDE = -9999

	MODES_NON_ICAO_ADDRESS = 1 << 24 // Set on addresses to indicate they are not ICAO addresses

	// MLAT timestamp magic value - 'MLAT' as a 48-bit timestamp
	// If a message's TimestampMsg field equals this, it's an MLAT message
	MAGIC_MLAT_TIMESTAMP = 0xFF004D4C4154
)

// DataSource indicates where a piece of data came from (in order of increasing priority)
type DataSource int

const (
	SOURCE_INVALID        DataSource = iota // data is not valid
	SOURCE_MLAT                             // derived from mlat
	SOURCE_MODE_S                           // data from a Mode S message, no full CRC
	SOURCE_MODE_S_CHECKED                   // data from a Mode S message with full CRC
	SOURCE_TISB                             // data from a TIS-B extended squitter message
	SOURCE_ADSB                             // data from a ADS-B extended squitter message
)

// AddrType indicates what sort of address this is and who sent it
// (Earlier values are higher priority)
type AddrType int

const (
	ADDR_ADSB_ICAO    AddrType = iota // Mode S or ADS-B, ICAO address, transponder sourced
	ADDR_ADSB_ICAO_NT                 // ADS-B, ICAO address, non-transponder
	ADDR_ADSR_ICAO                    // ADS-R, ICAO address
	ADDR_TISB_ICAO                    // TIS-B, ICAO address

	ADDR_ADSB_OTHER     // ADS-B, other address format
	ADDR_ADSR_OTHER     // ADS-R, other address format
	ADDR_TISB_TRACKFILE // TIS-B, Mode A code + track file number
	ADDR_TISB_OTHER     // TIS-B, other address format

	ADDR_UNKNOWN // unknown address format
)

// AltitudeUnit represents the unit used for altitude
type AltitudeUnit int

const (
	UNIT_FEET AltitudeUnit = iota
	UNIT_METERS
)

// AltitudeSource represents the source of altitude information
type AltitudeSource int

const (
	ALTITUDE_BARO AltitudeSource = iota
	ALTITUDE_GNSS
)

// AirGround represents air/ground state
type AirGround int

const (
	AG_INVALID AirGround = iota
	AG_GROUND
	AG_AIRBORNE
	AG_UNCERTAIN
)

// SpeedSource represents the source of speed information
type SpeedSource int

const (
	SPEED_GROUNDSPEED SpeedSource = iota
	SPEED_IAS
	SPEED_TAS
)

// HeadingSource represents the source of heading information
type HeadingSource int

const (
	HEADING_TRUE HeadingSource = iota
	HEADING_MAGNETIC
)

// SILType represents SIL (Source Integrity Level) measurement type
type SILType int

const (
	SIL_PER_SAMPLE SILType = iota
	SIL_PER_HOUR
)

// CPRType represents the type of CPR encoding used
type CPRType int

const (
	CPR_SURFACE CPRType = iota
	CPR_AIRBORNE
	CPR_COARSE
)

// Message represents a decoded Mode S message.
// This struct matches the C struct modesMessage.
type Message struct {
	// Generic fields
	Msg       [MODES_LONG_MSG_BYTES]byte // Binary message
	Verbatim  [MODES_LONG_MSG_BYTES]byte // Binary message, as originally received before correction
	MsgBits   int                        // Number of bits in message
	MsgType   int                        // Downlink format #
	CRC       uint32                     // Message CRC
	Corrected int                        // No. of bits corrected

	Addr     uint32   // Address announced
	AddrType AddrType // address format / source

	Timestamp    uint64    // Timestamp of the message (12MHz clock)
	SysTimestamp time.Time // Timestamp of the message (system time)
	TimestampMsg uint64    // Timestamp from message (for MLAT detection)

	Remote      bool    // If true, this message is from a remote station
	SignalLevel float64 // RSSI, in the range [0..1], as a fraction of full-scale power
	Score       int     // Scoring from scoreModesMessage, if used

	Source DataSource // Characterizes the overall message source

	// Raw data fields (directly from message)
	// The names reflect field names in ICAO Annex 4
	IID uint32 // extracted from CRC of DF11s
	AA  uint32
	AC  uint32
	CA  uint32
	CC  uint32
	CF  uint32
	DR  uint32
	FS  uint32
	ID  uint32
	KE  uint32
	ND  uint32
	RI  uint32
	SL  uint32
	UM  uint32
	VS  uint32

	MB [7]byte  // DF17/18 MB field
	MD [10]byte // DF20/21 MD field
	ME [7]byte  // DF17/18 ME field
	MV [7]byte  // DF17/18 MV field

	// Decoded data (validity indicated by flags)
	AltitudeValid   bool
	HeadingValid    bool
	SpeedValid      bool
	VertRateValid   bool
	SquawkValid     bool
	CallsignValid   bool
	EWVelocityValid bool
	NSVelocityValid bool
	CPRValid        bool
	CPROdd          bool
	CPRDecoded      bool
	CPRRelative     bool
	CategoryValid   bool
	GNSSDeltaValid  bool
	FromMLAT        bool
	FromTISB        bool
	SPIValid        bool
	SPI             bool
	AlertValid      bool
	Alert           bool

	METype uint32 // DF17/18 ME type
	MESub  uint32 // DF17/18 ME subtype

	// Valid if AltitudeValid
	Altitude       int            // Altitude in either feet or meters
	AltitudeUnit   AltitudeUnit   // the unit used for altitude
	AltitudeSource AltitudeSource // whether the altitude is barometric or GNSS

	// Valid if GNSSDeltaValid
	GNSSDelta int // difference between GNSS and baro alt

	// Valid if HeadingValid
	Heading       uint32        // Reported by aircraft, or computed from velocity
	HeadingSource HeadingSource // what "heading" is measuring

	// Valid if SpeedValid
	Speed       uint32      // in kts, reported or computed from velocity
	SpeedSource SpeedSource // what "speed" is measuring

	// Valid if VertRateValid
	VertRate       int            // vertical rate in feet/minute
	VertRateSource AltitudeSource // the altitude source used for vert_rate

	// Valid if SquawkValid
	Squawk uint32 // 13 bits identity (Squawk), encoded as 4 hex digits

	// Valid if CallsignValid
	Callsign [9]byte // 8 chars flight number + null terminator

	// Valid if CategoryValid
	Category uint32 // A0 - D7 encoded as a single hex byte

	// Valid if CPRValid
	CPRType CPRType // The encoding type used
	CPRLat  uint32  // Non decoded latitude
	CPRLon  uint32  // Non decoded longitude
	CPRNUCP uint32  // NUCp/NIC value implied by message type

	AirGround AirGround // air/ground state

	// Valid if CPRDecoded
	DecodedLat float64
	DecodedLon float64

	// Operational Status
	OpStatus struct {
		Valid   bool
		Version uint32

		// Operational Mode
		OMACASRa bool
		OMIdent  bool
		OMATC    bool
		OMSAF    bool
		OMSDA    uint32

		// Capability Class
		CCACAS       bool
		CCCDTI       bool
		CC1090In     bool
		CCARV        bool
		CCTS         bool
		CCTC         uint32
		CCUATIn      bool
		CCPOA        bool
		CCB2Low      bool
		CCNACv       uint32
		CCNICSupp    bool
		CCLWValid    bool
		CCLW         uint32
		CCAntennaOff uint32

		NICSupp uint32
		NACp    uint32
		GVA     uint32
		SIL     uint32
		NICBaro bool

		SILType    SILType
		TrackAngle uint32 // ANGLE_HEADING or ANGLE_TRACK
		HRD        HeadingSource
	}

	// Target State & Status (ADS-B V2 only)
	TSS struct {
		Valid           bool
		AltitudeValid   bool
		BaroValid       bool
		HeadingValid    bool
		ModeValid       bool
		ModeAutopilot   bool
		ModeVNAV        bool
		ModeAltHold     bool
		ModeApproach    bool
		ACASOperational bool
		NACp            uint32
		NICBaro         bool
		SIL             uint32

		SILType      SILType
		AltitudeType uint32 // TSS_ALTITUDE_MCP or TSS_ALTITUDE_FMS
		Altitude     uint32
		Baro         float32
		Heading      uint32
	}
}

// MagBuf represents one magnitude buffer (converted from IQ samples)
type MagBuf struct {
	Data            []uint16  // Magnitude data (includes trailing overlap from previous block)
	Length          uint32    // Number of valid samples after overlap
	SampleTimestamp uint64    // Clock timestamp of the start of this block, 12MHz clock
	SysTimestamp    time.Time // Estimated system time at start of block
	Dropped         uint32    // Number of dropped samples preceding this buffer
	TotalPower      float64   // Sum of per-sample input power, or 0 if not measured
}

// DataValidity tracks the validity and freshness of a piece of data.
// Each field in Aircraft has an associated DataValidity to track when
// it was last updated and from what source.
type DataValidity struct {
	Source  DataSource // Where the data came from
	Updated uint64     // Timestamp (milliseconds) when data arrived
	Stale   uint64     // Timestamp when data becomes stale
	Expires uint64     // Timestamp when data expires
}

// Aircraft tracking constants
const (
	TRACK_AIRCRAFT_TTL        = 300000 // Maximum age of tracked aircraft (5 minutes)
	TRACK_AIRCRAFT_ONEHIT_TTL = 60000  // Maximum age for aircraft with only 1 message (1 minute)
)

// Mode A/C flags
const (
	MODEAC_MSG_FLAG       = 1 << 0 // Synthetic ICAO from Mode A/C
	MODEAC_MSG_MODES_HIT  = 1 << 1 // This Mode A/C matches a known Mode S
	MODEAC_MSG_MODEA_HIT  = 1 << 2 // This Mode S matches a known Mode A
	MODEAC_MSG_MODEC_HIT  = 1 << 3 // This Mode S matches a known Mode C
	MODEAC_MSG_MODEA_ONLY = 1 << 4 // This Mode A/C is from a Mode A only roll call
	MODEAC_MSG_MODEC_OLD  = 1 << 5 // This Mode A/C has previously been Mode C matched
)

// Aircraft represents the state of one tracked aircraft.
// This struct parallels the C struct aircraft in track.h.
type Aircraft struct {
	Addr     uint32   // ICAO address
	AddrType AddrType // Highest priority address type seen

	Seen     uint64 // Time (millis) at which last packet was received
	Messages int64  // Number of Mode S messages received

	SignalLevel [8]float64 // Last 8 signal amplitudes (RSSI)
	SignalNext  int        // Next index in signalLevel to use

	// Callsign
	CallsignValid DataValidity
	Callsign      [9]byte // Flight number

	// Barometric altitude
	AltitudeValid DataValidity
	Altitude      int    // Altitude (feet, barometric)
	AltitudeModeC uint32 // Altitude as Mode C value (100ft increments)

	// GNSS altitude
	AltitudeGNSSValid DataValidity
	AltitudeGNSS      int // Altitude (feet, GNSS)

	// GNSS delta
	GNSSDeltaValid DataValidity
	GNSSDelta      int // Difference between GNSS and baro altitude

	// Speed
	SpeedValid    DataValidity
	Speed         uint32 // Ground speed (kts)
	SpeedIASValid DataValidity
	SpeedIAS      uint32 // Indicated airspeed
	SpeedTASValid DataValidity
	SpeedTAS      uint32 // True airspeed

	// Heading
	HeadingValid         DataValidity
	Heading              uint32 // True track/heading
	HeadingMagneticValid DataValidity
	HeadingMagnetic      uint32 // Magnetic heading

	// Vertical rate
	VertRateValid  DataValidity
	VertRate       int            // Vertical rate (feet/min)
	VertRateSource AltitudeSource // Source (baro or GNSS)

	// Squawk
	SquawkValid DataValidity
	Squawk      uint32 // 4-digit squawk code

	// Category
	CategoryValid DataValidity
	Category      uint32 // Aircraft category (A0-D7)

	// Air/ground state
	AirGroundValid DataValidity
	AirGround      AirGround

	// CPR odd frame
	CPROddValid DataValidity
	CPROddType  CPRType
	CPROddLat   uint32
	CPROddLon   uint32
	CPROddNUC   uint32

	// CPR even frame
	CPREvenValid DataValidity
	CPREvenType  CPRType
	CPREvenLat   uint32
	CPREvenLon   uint32
	CPREvenNUC   uint32

	// Decoded position
	PositionValid DataValidity
	Lat           float64 // Latitude
	Lon           float64 // Longitude
	PosNUC        uint32  // NUCp of last computed position

	// Mode A/C correlation
	ModeACount  int64 // Mode A squawk hit count
	ModeCCount  int64 // Mode C altitude hit count
	ModeACFlags int   // Flags for Mode A/C recognition

	// First message received (for deferred output)
	FirstMessage Message

	// FATSV (FlightAware TSV) output tracking
	// These fields track the last emitted values to avoid redundant updates
	FATSVLastEmitted         uint64    // Time (millis) aircraft was last FATSV emitted
	FATSVEmittedAltitude     int       // Last emitted barometric altitude
	FATSVEmittedAltitudeGNSS int       // Last emitted GNSS altitude
	FATSVEmittedHeading      uint32    // Last emitted true heading
	FATSVEmittedHeadingMag   uint32    // Last emitted magnetic heading
	FATSVEmittedSpeed        uint32    // Last emitted ground speed
	FATSVEmittedSpeedIAS     uint32    // Last emitted IAS
	FATSVEmittedSpeedTAS     uint32    // Last emitted TAS
	FATSVEmittedAirGround    AirGround // Last emitted air/ground state
	FATSVEmittedBDS10        [7]byte   // Last emitted BDS 1,0 message
	FATSVEmittedBDS30        [7]byte   // Last emitted BDS 3,0 message
	FATSVEmittedESStatus     [7]byte   // Last emitted ES operational status message
	FATSVEmittedESTarget     [7]byte   // Last emitted ES target status message
	FATSVEmittedESACASRA     [7]byte   // Last emitted ES ACAS RA report message
}
