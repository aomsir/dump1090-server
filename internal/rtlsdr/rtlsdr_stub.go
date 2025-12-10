// +build !cgo

// Package rtlsdr provides Go bindings to librtlsdr for RTL-SDR USB devices.
//
// This is a stub implementation for systems without cgo support.
// All functions return errors indicating that RTL-SDR is not available.
package rtlsdr

import "errors"

// ErrNoCGO is returned when the package is compiled without cgo support
var ErrNoCGO = errors.New("rtlsdr: compiled without cgo support")

// Common errors
var (
	ErrNotFound      = errors.New("rtlsdr: device not found")
	ErrAccessDenied  = errors.New("rtlsdr: access denied")
	ErrDeviceBusy    = errors.New("rtlsdr: device busy")
	ErrInvalidParam  = errors.New("rtlsdr: invalid parameter")
	ErrNotSupported  = errors.New("rtlsdr: not supported")
	ErrNoMemory      = errors.New("rtlsdr: no memory")
	ErrLibUSB        = errors.New("rtlsdr: libusb error")
	ErrUnknown       = errors.New("rtlsdr: unknown error")
)

// TunerType represents the tuner chip type
type TunerType int

const (
	TunerUnknown TunerType = 0
	TunerE4000   TunerType = 1
	TunerFC0012  TunerType = 2
	TunerFC0013  TunerType = 3
	TunerFC2580  TunerType = 4
	TunerR820T   TunerType = 5
	TunerR828D   TunerType = 6
)

func (t TunerType) String() string {
	return "Unknown (no cgo)"
}

// Device represents an RTL-SDR device
type Device struct{}

// AsyncCallback is the function type for async read callbacks
type AsyncCallback func(buf []byte)

// DeviceInfo contains information about an RTL-SDR device
type DeviceInfo struct {
	Index      int
	Name       string
	Vendor     string
	Product    string
	Serial     string
	TunerType  TunerType
	TunerGains []int
}

// GetDeviceCount returns the number of RTL-SDR devices found
func GetDeviceCount() int { return 0 }

// GetDeviceName returns the name of a device by index
func GetDeviceName(index int) string { return "" }

// GetDeviceUSBStrings returns USB descriptor strings for a device
func GetDeviceUSBStrings(index int) (vendor, product, serial string, err error) {
	return "", "", "", ErrNoCGO
}

// GetIndexBySerial finds a device index by serial number
func GetIndexBySerial(serial string) (int, error) { return -1, ErrNoCGO }

// ListDevices returns information about all connected RTL-SDR devices
func ListDevices() []DeviceInfo { return nil }

// Open opens an RTL-SDR device by index
func Open(index int) (*Device, error) { return nil, ErrNoCGO }

// Close closes the device
func (d *Device) Close() error { return ErrNoCGO }

// SetCenterFreq sets the center frequency in Hz
func (d *Device) SetCenterFreq(freq uint32) error { return ErrNoCGO }

// GetCenterFreq returns the current center frequency in Hz
func (d *Device) GetCenterFreq() uint32 { return 0 }

// SetFreqCorrection sets the frequency correction in PPM
func (d *Device) SetFreqCorrection(ppm int) error { return ErrNoCGO }

// GetFreqCorrection returns the current frequency correction in PPM
func (d *Device) GetFreqCorrection() int { return 0 }

// GetTunerType returns the tuner chip type
func (d *Device) GetTunerType() TunerType { return TunerUnknown }

// GetTunerGains returns the available tuner gains in tenths of dB
func (d *Device) GetTunerGains() ([]int, error) { return nil, ErrNoCGO }

// SetTunerGain sets the tuner gain in tenths of dB
func (d *Device) SetTunerGain(gain int) error { return ErrNoCGO }

// GetTunerGain returns the current tuner gain in tenths of dB
func (d *Device) GetTunerGain() int { return 0 }

// SetTunerGainMode sets the gain mode (0=auto, 1=manual)
func (d *Device) SetTunerGainMode(manual bool) error { return ErrNoCGO }

// SetAGCMode enables or disables RTL2832 AGC
func (d *Device) SetAGCMode(enable bool) error { return ErrNoCGO }

// SetSampleRate sets the sample rate in Hz
func (d *Device) SetSampleRate(rate uint32) error { return ErrNoCGO }

// GetSampleRate returns the current sample rate in Hz
func (d *Device) GetSampleRate() uint32 { return 0 }

// SetBiasTee enables or disables the bias tee on GPIO pin 0
func (d *Device) SetBiasTee(enable bool) error { return ErrNoCGO }

// ResetBuffer resets the internal buffer
func (d *Device) ResetBuffer() error { return ErrNoCGO }

// ReadSync reads samples synchronously
func (d *Device) ReadSync(buf []byte) (int, error) { return 0, ErrNoCGO }

// ReadAsync starts async reading with the given callback
func (d *Device) ReadAsync(callback AsyncCallback, bufNum, bufLen uint32) error {
	return ErrNoCGO
}

// CancelAsync cancels async reading
func (d *Device) CancelAsync() error { return ErrNoCGO }

// DeviceConfig holds configuration for opening and setting up a device
type DeviceConfig struct {
	Index          int
	Frequency      uint32
	SampleRate     uint32
	Gain           int
	PPMCorrection  int
	EnableAGC      bool
	EnableBiasTee  bool
}

// DefaultConfig returns the default configuration for ADS-B reception
func DefaultConfig() DeviceConfig {
	return DeviceConfig{
		Index:         0,
		Frequency:     1090000000,
		SampleRate:    2400000,
		Gain:          -1,
		PPMCorrection: 0,
		EnableAGC:     true,
		EnableBiasTee: false,
	}
}

// OpenWithConfig opens and configures a device with the given settings
func OpenWithConfig(cfg DeviceConfig) (*Device, error) {
	return nil, ErrNoCGO
}
