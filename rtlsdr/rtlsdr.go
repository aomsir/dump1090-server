//go:build cgo
// +build cgo

// Package rtlsdr provides Go bindings to librtlsdr for RTL-SDR USB devices.
//
// This package wraps the C library librtlsdr to provide access to RTL2832U
// based DVB-T receivers for software defined radio applications.
//
// Build requirements:
//   - librtlsdr development files (librtlsdr-dev on Debian/Ubuntu)
//   - pkg-config
package rtlsdr

/*
#cgo pkg-config: librtlsdr

#include <stdlib.h>
#include <rtl-sdr.h>

// Callback wrapper for async reading
extern void goAsyncCallback(unsigned char *buf, uint32_t len, void *ctx);

static inline int rtlsdr_read_async_wrapper(rtlsdr_dev_t *dev, void *ctx, uint32_t buf_num, uint32_t buf_len) {
    return rtlsdr_read_async(dev, (rtlsdr_read_async_cb_t)goAsyncCallback, ctx, buf_num, buf_len);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// Common errors
var (
	ErrNotFound     = errors.New("rtlsdr: device not found")
	ErrAccessDenied = errors.New("rtlsdr: access denied")
	ErrDeviceBusy   = errors.New("rtlsdr: device busy")
	ErrInvalidParam = errors.New("rtlsdr: invalid parameter")
	ErrNotSupported = errors.New("rtlsdr: not supported")
	ErrNoMemory     = errors.New("rtlsdr: no memory")
	ErrLibUSB       = errors.New("rtlsdr: libusb error")
	ErrUnknown      = errors.New("rtlsdr: unknown error")
)

// TunerType represents the tuner chip type
type TunerType int

const (
	TunerUnknown TunerType = C.RTLSDR_TUNER_UNKNOWN
	TunerE4000   TunerType = C.RTLSDR_TUNER_E4000
	TunerFC0012  TunerType = C.RTLSDR_TUNER_FC0012
	TunerFC0013  TunerType = C.RTLSDR_TUNER_FC0013
	TunerFC2580  TunerType = C.RTLSDR_TUNER_FC2580
	TunerR820T   TunerType = C.RTLSDR_TUNER_R820T
	TunerR828D   TunerType = C.RTLSDR_TUNER_R828D
)

func (t TunerType) String() string {
	switch t {
	case TunerE4000:
		return "E4000"
	case TunerFC0012:
		return "FC0012"
	case TunerFC0013:
		return "FC0013"
	case TunerFC2580:
		return "FC2580"
	case TunerR820T:
		return "R820T"
	case TunerR828D:
		return "R828D"
	default:
		return "Unknown"
	}
}

// Device represents an RTL-SDR device
type Device struct {
	dev      *C.rtlsdr_dev_t
	index    int
	closed   bool
	mu       sync.Mutex
	callback AsyncCallback
}

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
	TunerGains []int // Available gain values in tenths of dB
}

// Global callback registry for async reading
var (
	callbackMu   sync.RWMutex
	callbackMap  = make(map[uintptr]*Device)
	callbackNext uintptr
)

// GetDeviceCount returns the number of RTL-SDR devices found
func GetDeviceCount() int {
	return int(C.rtlsdr_get_device_count())
}

// GetDeviceName returns the name of a device by index
func GetDeviceName(index int) string {
	name := C.rtlsdr_get_device_name(C.uint32_t(index))
	if name == nil {
		return ""
	}
	return C.GoString(name)
}

// GetDeviceUSBStrings returns USB descriptor strings for a device
func GetDeviceUSBStrings(index int) (vendor, product, serial string, err error) {
	var cVendor, cProduct, cSerial [256]C.char

	ret := C.rtlsdr_get_device_usb_strings(
		C.uint32_t(index),
		&cVendor[0],
		&cProduct[0],
		&cSerial[0],
	)

	if ret != 0 {
		return "", "", "", translateError(int(ret))
	}

	return C.GoString(&cVendor[0]),
		C.GoString(&cProduct[0]),
		C.GoString(&cSerial[0]),
		nil
}

// GetIndexBySerial finds a device index by exact serial number.
func GetIndexBySerial(serial string) (int, error) {
	cSerial := C.CString(serial)
	defer C.free(unsafe.Pointer(cSerial))

	idx := C.rtlsdr_get_index_by_serial(cSerial)
	if idx < 0 {
		return -1, translateError(int(idx))
	}

	return int(idx), nil
}

// FindDeviceBySerial finds a device index by serial number.
// Supports exact match, prefix match (serial ends with '-'), and suffix match (serial starts with '-').
// Returns the device index or -1 if not found.
func FindDeviceBySerial(serial string) (int, error) {
	count := GetDeviceCount()
	if count == 0 {
		return -1, ErrNotFound
	}

	// Try exact match first
	idx, err := GetIndexBySerial(serial)
	if err == nil {
		return idx, nil
	}

	// Check for prefix/suffix patterns
	isPrefix := len(serial) > 0 && serial[len(serial)-1] == '-'
	isSuffix := len(serial) > 0 && serial[0] == '-'

	if isPrefix || isSuffix {
		pattern := serial
		if isPrefix {
			pattern = serial[:len(serial)-1]
		} else {
			pattern = serial[1:]
		}

		for i := 0; i < count; i++ {
			_, _, devSerial, err := GetDeviceUSBStrings(i)
			if err != nil {
				continue
			}
			if isPrefix && len(devSerial) >= len(pattern) && devSerial[:len(pattern)] == pattern {
				return i, nil
			}
			if isSuffix && len(devSerial) >= len(pattern) && devSerial[len(devSerial)-len(pattern):] == pattern {
				return i, nil
			}
		}
	}

	return -1, ErrNotFound
}

// NearestGain finds the nearest supported gain value to the requested gain.
// Returns the nearest gain in tenths of dB, or the requested gain if no gains are available.
func NearestGain(dev *Device, requestedGain int) (int, error) {
	gains, err := dev.GetTunerGains()
	if err != nil {
		return requestedGain, err
	}
	if len(gains) == 0 {
		return requestedGain, nil
	}

	nearest := gains[0]
	minDiff := abs(gains[0] - requestedGain)

	for _, g := range gains[1:] {
		diff := abs(g - requestedGain)
		if diff < minDiff {
			minDiff = diff
			nearest = g
		}
	}

	return nearest, nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ListDevices returns information about all connected RTL-SDR devices
func ListDevices() []DeviceInfo {
	count := GetDeviceCount()
	devices := make([]DeviceInfo, 0, count)

	for i := 0; i < count; i++ {
		info := DeviceInfo{
			Index: i,
			Name:  GetDeviceName(i),
		}

		vendor, product, serial, err := GetDeviceUSBStrings(i)
		if err == nil {
			info.Vendor = vendor
			info.Product = product
			info.Serial = serial
		}

		devices = append(devices, info)
	}

	return devices
}

// Open opens an RTL-SDR device by index
func Open(index int) (*Device, error) {
	var dev *C.rtlsdr_dev_t

	ret := C.rtlsdr_open(&dev, C.uint32_t(index))
	if ret != 0 {
		return nil, translateError(int(ret))
	}

	return &Device{
		dev:   dev,
		index: index,
	}, nil
}

// Close closes the device
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil
	}

	ret := C.rtlsdr_close(d.dev)
	d.closed = true
	d.dev = nil

	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// SetCenterFreq sets the center frequency in Hz
func (d *Device) SetCenterFreq(freq uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	ret := C.rtlsdr_set_center_freq(d.dev, C.uint32_t(freq))
	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// GetCenterFreq returns the current center frequency in Hz
func (d *Device) GetCenterFreq() uint32 {
	d.mu.Lock()
	defer d.mu.Unlock()

	return uint32(C.rtlsdr_get_center_freq(d.dev))
}

// SetFreqCorrection sets the frequency correction in PPM
func (d *Device) SetFreqCorrection(ppm int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	ret := C.rtlsdr_set_freq_correction(d.dev, C.int(ppm))
	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// GetFreqCorrection returns the current frequency correction in PPM
func (d *Device) GetFreqCorrection() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return int(C.rtlsdr_get_freq_correction(d.dev))
}

// GetTunerType returns the tuner chip type
func (d *Device) GetTunerType() TunerType {
	d.mu.Lock()
	defer d.mu.Unlock()

	return TunerType(C.rtlsdr_get_tuner_type(d.dev))
}

// GetTunerGains returns the available tuner gains in tenths of dB
func (d *Device) GetTunerGains() ([]int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// First call to get count
	count := int(C.rtlsdr_get_tuner_gains(d.dev, nil))
	if count < 0 {
		return nil, translateError(count)
	}

	if count == 0 {
		return nil, nil
	}

	// Second call to get gains
	gains := make([]C.int, count)
	ret := C.rtlsdr_get_tuner_gains(d.dev, &gains[0])
	if int(ret) != count {
		return nil, ErrUnknown
	}

	result := make([]int, count)
	for i, g := range gains {
		result[i] = int(g)
	}
	return result, nil
}

// SetTunerGain sets the tuner gain in tenths of dB
func (d *Device) SetTunerGain(gain int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	ret := C.rtlsdr_set_tuner_gain(d.dev, C.int(gain))
	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// GetTunerGain returns the current tuner gain in tenths of dB
func (d *Device) GetTunerGain() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return int(C.rtlsdr_get_tuner_gain(d.dev))
}

// SetTunerGainMode sets the gain mode (0=auto, 1=manual)
func (d *Device) SetTunerGainMode(manual bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	mode := 0
	if manual {
		mode = 1
	}

	ret := C.rtlsdr_set_tuner_gain_mode(d.dev, C.int(mode))
	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// SetAGCMode enables or disables RTL2832 AGC
func (d *Device) SetAGCMode(enable bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	mode := 0
	if enable {
		mode = 1
	}

	ret := C.rtlsdr_set_agc_mode(d.dev, C.int(mode))
	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// SetSampleRate sets the sample rate in Hz
func (d *Device) SetSampleRate(rate uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	ret := C.rtlsdr_set_sample_rate(d.dev, C.uint32_t(rate))
	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// GetSampleRate returns the current sample rate in Hz
func (d *Device) GetSampleRate() uint32 {
	d.mu.Lock()
	defer d.mu.Unlock()

	return uint32(C.rtlsdr_get_sample_rate(d.dev))
}

// SetBiasTee enables or disables the bias tee on GPIO pin 0
func (d *Device) SetBiasTee(enable bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	mode := 0
	if enable {
		mode = 1
	}

	ret := C.rtlsdr_set_bias_tee(d.dev, C.int(mode))
	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// ResetBuffer resets the internal buffer
func (d *Device) ResetBuffer() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	ret := C.rtlsdr_reset_buffer(d.dev)
	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// ReadSync reads samples synchronously
func (d *Device) ReadSync(buf []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(buf) == 0 {
		return 0, nil
	}

	var nRead C.int
	ret := C.rtlsdr_read_sync(d.dev, unsafe.Pointer(&buf[0]), C.int(len(buf)), &nRead)
	if ret != 0 {
		return int(nRead), translateError(int(ret))
	}
	return int(nRead), nil
}

// ReadAsync starts async reading with the given callback
// This function blocks until CancelAsync is called or an error occurs
func (d *Device) ReadAsync(callback AsyncCallback, bufNum, bufLen uint32) error {
	d.mu.Lock()
	d.callback = callback

	// Register callback
	callbackMu.Lock()
	callbackNext++
	id := callbackNext
	callbackMap[id] = d
	callbackMu.Unlock()

	d.mu.Unlock()

	// Start async reading (this blocks)
	ret := C.rtlsdr_read_async_wrapper(d.dev, unsafe.Pointer(id), C.uint32_t(bufNum), C.uint32_t(bufLen))

	// Clean up callback registration
	callbackMu.Lock()
	delete(callbackMap, id)
	callbackMu.Unlock()

	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// CancelAsync cancels async reading
func (d *Device) CancelAsync() error {
	ret := C.rtlsdr_cancel_async(d.dev)
	if ret != 0 {
		return translateError(int(ret))
	}
	return nil
}

// goAsyncCallback is called from C for async reading
//
//export goAsyncCallback
func goAsyncCallback(buf *C.uchar, length C.uint32_t, ctx unsafe.Pointer) {
	id := uintptr(ctx)

	callbackMu.RLock()
	dev, ok := callbackMap[id]
	callbackMu.RUnlock()

	if !ok || dev.callback == nil {
		return
	}

	// Create Go slice from C buffer (without copying)
	goSlice := unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(length))
	dev.callback(goSlice)
}

// translateError converts librtlsdr error codes to Go errors
func translateError(code int) error {
	switch code {
	case 0:
		return nil
	case -1:
		return ErrLibUSB
	case -2:
		return ErrInvalidParam
	case -3:
		return ErrAccessDenied
	case -4:
		return ErrNotFound
	case -5:
		return ErrDeviceBusy
	case -6:
		return ErrNoMemory
	case -7:
		return ErrNotSupported
	default:
		return fmt.Errorf("rtlsdr: error code %d", code)
	}
}

// DeviceConfig holds configuration for opening and setting up a device
type DeviceConfig struct {
	Index         int
	Frequency     uint32 // Center frequency in Hz (default: 1090MHz)
	SampleRate    uint32 // Sample rate in Hz (default: 2400000)
	Gain          int    // Gain in tenths of dB, -1 for auto
	PPMCorrection int    // Frequency correction in PPM
	EnableAGC     bool   // Enable RTL2832 AGC
	EnableBiasTee bool   // Enable bias tee
	DeviceSerial  string // Select device by serial number (or prefix/suffix)
}

// DefaultConfig returns the default configuration for ADS-B reception
func DefaultConfig() DeviceConfig {
	return DeviceConfig{
		Index:         0,
		Frequency:     1090000000, // 1090 MHz
		SampleRate:    2400000,    // 2.4 MHz
		Gain:          -1,         // Auto
		PPMCorrection: 0,
		EnableAGC:     false,
		EnableBiasTee: false,
	}
}

// OpenWithConfig opens and configures a device with the given settings
func OpenWithConfig(cfg DeviceConfig) (*Device, error) {
	// Find device by serial if specified
	index := cfg.Index
	if cfg.DeviceSerial != "" {
		foundIdx, err := FindDeviceBySerial(cfg.DeviceSerial)
		if err != nil {
			return nil, fmt.Errorf("device with serial %q not found: %w", cfg.DeviceSerial, err)
		}
		index = foundIdx
	}

	dev, err := Open(index)
	if err != nil {
		return nil, fmt.Errorf("failed to open device %d: %w", index, err)
	}

	// Set gain mode
	if cfg.Gain < 0 {
		// Auto gain
		if err := dev.SetTunerGainMode(false); err != nil {
			dev.Close()
			return nil, fmt.Errorf("failed to set auto gain mode: %w", err)
		}
	} else {
		// Manual gain - find nearest supported gain
		nearestGain, err := NearestGain(dev, cfg.Gain)
		if err != nil {
			dev.Close()
			return nil, fmt.Errorf("failed to get tuner gains: %w", err)
		}

		if err := dev.SetTunerGainMode(true); err != nil {
			dev.Close()
			return nil, fmt.Errorf("failed to set manual gain mode: %w", err)
		}
		if err := dev.SetTunerGain(nearestGain); err != nil {
			dev.Close()
			return nil, fmt.Errorf("failed to set gain to %d: %w", nearestGain, err)
		}
	}

	// Set frequency correction
	if cfg.PPMCorrection != 0 {
		if err := dev.SetFreqCorrection(cfg.PPMCorrection); err != nil {
			dev.Close()
			return nil, fmt.Errorf("failed to set PPM correction: %w", err)
		}
	}

	// Enable AGC if requested (default off)
	if cfg.EnableAGC {
		if err := dev.SetAGCMode(true); err != nil {
			dev.Close()
			return nil, fmt.Errorf("failed to enable AGC: %w", err)
		}
	}

	// Set frequency
	if err := dev.SetCenterFreq(cfg.Frequency); err != nil {
		dev.Close()
		return nil, fmt.Errorf("failed to set frequency to %d Hz: %w", cfg.Frequency, err)
	}

	// Set sample rate
	if err := dev.SetSampleRate(cfg.SampleRate); err != nil {
		dev.Close()
		return nil, fmt.Errorf("failed to set sample rate to %d Hz: %w", cfg.SampleRate, err)
	}

	// Reset buffer
	if err := dev.ResetBuffer(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("failed to reset buffer: %w", err)
	}

	// Enable bias tee if requested
	if cfg.EnableBiasTee {
		if err := dev.SetBiasTee(true); err != nil {
			// Non-fatal, some devices don't support it
		}
	}

	return dev, nil
}
