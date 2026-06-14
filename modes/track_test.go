package modes

import (
	"testing"
	"time"
)

func TestTrackerBasic(t *testing.T) {
	tracker := NewTracker()

	// Decode a real DF17 message
	ModesChecksumInit(2)
	msgHex := "8D4840D6202CC371C32CE0576098"
	msg := mustDecodeHex(msgHex)

	mm, err := DecodeModesMessage(msg)
	if err != 0 {
		t.Fatalf("DecodeModesMessage() failed: %d", err)
	}

	// First update - should create aircraft
	a := tracker.UpdateFromMessage(mm)
	if a == nil {
		t.Fatal("UpdateFromMessage() returned nil")
	}

	if a.Addr != 0x4840D6 {
		t.Errorf("Aircraft addr = 0x%06X, want 0x4840D6", a.Addr)
	}

	if a.Messages != 1 {
		t.Errorf("Messages = %d, want 1", a.Messages)
	}

	// Second update - should update existing aircraft
	mm2, _ := DecodeModesMessage(msg)
	a2 := tracker.UpdateFromMessage(mm2)

	if a2.Addr != a.Addr {
		t.Error("Second update returned different aircraft")
	}

	if a2.Messages != 2 {
		t.Errorf("Messages = %d, want 2", a2.Messages)
	}

	// Verify we can retrieve the aircraft
	retrieved := tracker.GetAircraft(0x4840D6)
	if retrieved == nil {
		t.Fatal("GetAircraft() returned nil")
	}

	if retrieved.Addr != 0x4840D6 {
		t.Errorf("Retrieved aircraft addr = 0x%06X, want 0x4840D6", retrieved.Addr)
	}
}

func TestTrackerCallsignUpdate(t *testing.T) {
	tracker := NewTracker()
	ModesChecksumInit(2)

	// DF17 message with callsign
	msgHex := "8D4840D6202CC371C32CE0576098"
	msg := mustDecodeHex(msgHex)

	mm, err := DecodeModesMessage(msg)
	if err != 0 {
		t.Fatalf("DecodeModesMessage() failed: %d", err)
	}

	a := tracker.UpdateFromMessage(mm)

	if !mm.CallsignValid {
		t.Fatal("Callsign should be valid")
	}

	if !dataValid(&a.CallsignValid) {
		t.Error("Aircraft callsign validity not set")
	}

	// Verify callsign was stored
	callsign := string(a.Callsign[:8])
	t.Logf("Decoded callsign: %q", callsign)
}

func TestTrackerAltitudeUpdate(t *testing.T) {
	tracker := NewTracker()
	ModesChecksumInit(2)

	// DF17 airborne position message (synthetic - may not decode)
	msgHex := "8D4840D6580B90000000000000F0"
	msg := mustDecodeHex(msgHex)

	mm, err := DecodeModesMessage(msg)
	if err != 0 {
		t.Skipf("Test message failed to decode (expected for synthetic message): %d", err)
		return
	}

	a := tracker.UpdateFromMessage(mm)

	if mm.AltitudeValid {
		if !dataValid(&a.AltitudeValid) {
			t.Error("Aircraft altitude validity not set")
		}

		t.Logf("Altitude: %d feet", a.Altitude)
	}
}

func TestTrackerDataValidity(t *testing.T) {
	now := uint64(time.Now().UnixMilli())

	var v DataValidity

	// Initially invalid
	if dataValid(&v) {
		t.Error("DataValidity should be invalid initially")
	}

	// Accept data from ADS-B
	if !acceptData(&v, SOURCE_ADSB, now) {
		t.Error("Should accept ADS-B data")
	}

	if !dataValid(&v) {
		t.Error("DataValidity should be valid after accept")
	}

	if v.Source != SOURCE_ADSB {
		t.Errorf("Source = %v, want SOURCE_ADSB", v.Source)
	}

	// Try to update with lower priority source (MODE_S) while data is fresh
	if acceptData(&v, SOURCE_MODE_S, now+1000) {
		t.Error("Should not accept lower priority data while fresh")
	}

	// After data goes stale, should accept lower priority
	if !acceptData(&v, SOURCE_MODE_S, now+70000) {
		t.Error("Should accept lower priority data after stale")
	}

	if v.Source != SOURCE_MODE_S {
		t.Errorf("Source = %v, want SOURCE_MODE_S", v.Source)
	}
}

func TestTrackerExpiration(t *testing.T) {
	tracker := NewTracker()
	ModesChecksumInit(2)

	msgHex := "8D4840D6202CC371C32CE0576098"
	msg := mustDecodeHex(msgHex)

	mm, _ := DecodeModesMessage(msg)
	a := tracker.UpdateFromMessage(mm)

	// Callsign should be valid
	if !dataValid(&a.CallsignValid) {
		t.Fatal("Callsign should be valid")
	}

	// Manually expire the field
	now := uint64(time.Now().UnixMilli())
	a.CallsignValid.Expires = now - 1000 // Expired 1 second ago

	tracker.expireField(&a.CallsignValid, now)

	if dataValid(&a.CallsignValid) {
		t.Error("Callsign should be invalid after expiration")
	}
}

func TestTrackerRemoveStale(t *testing.T) {
	tracker := NewTracker()
	ModesChecksumInit(2)

	msgHex := "8D4840D6202CC371C32CE0576098"
	msg := mustDecodeHex(msgHex)

	mm, _ := DecodeModesMessage(msg)
	a := tracker.UpdateFromMessage(mm)

	// Manually set seen time to long ago
	a.Seen = uint64(time.Now().UnixMilli()) - TRACK_AIRCRAFT_TTL - 1000

	// Update tracker's internal state (this is a bit hacky for testing)
	tracker.mu.Lock()
	tracker.aircraft[a.Addr] = a
	tracker.mu.Unlock()

	// Run periodic update
	tracker.PeriodicUpdate()

	// Aircraft should be removed
	retrieved := tracker.GetAircraft(0x4840D6)
	if retrieved != nil {
		t.Error("Stale aircraft should be removed")
	}
}

func TestTrackerOneHitRemoval(t *testing.T) {
	tracker := NewTracker()
	ModesChecksumInit(2)

	msgHex := "8D4840D6202CC371C32CE0576098"
	msg := mustDecodeHex(msgHex)

	mm, _ := DecodeModesMessage(msg)
	a := tracker.UpdateFromMessage(mm)

	// One message, aged out
	if a.Messages != 1 {
		t.Fatalf("Messages = %d, want 1", a.Messages)
	}

	a.Seen = uint64(time.Now().UnixMilli()) - TRACK_AIRCRAFT_ONEHIT_TTL - 1000

	tracker.mu.Lock()
	tracker.aircraft[a.Addr] = a
	tracker.mu.Unlock()

	tracker.PeriodicUpdate()

	retrieved := tracker.GetAircraft(0x4840D6)
	if retrieved != nil {
		t.Error("One-hit aircraft should be removed after timeout")
	}
}

func TestTrackerGetAllAircraft(t *testing.T) {
	tracker := NewTracker()
	ModesChecksumInit(2)

	// For this test, we'll manually create aircraft since we can't easily
	// generate valid Mode S messages with different addresses
	tracker.mu.Lock()
	tracker.aircraft[0x4840D6] = &Aircraft{Addr: 0x4840D6, Messages: 1}
	tracker.aircraft[0x4840D7] = &Aircraft{Addr: 0x4840D7, Messages: 1}
	tracker.aircraft[0x4840D8] = &Aircraft{Addr: 0x4840D8, Messages: 1}
	tracker.mu.Unlock()

	all := tracker.GetAllAircraft()
	if len(all) != 3 {
		t.Errorf("GetAllAircraft() returned %d aircraft, want 3", len(all))
	}

	// Verify addresses are present
	addresses := make(map[uint32]bool)
	for _, a := range all {
		addresses[a.Addr] = true
	}

	expected := []uint32{0x4840D6, 0x4840D7, 0x4840D8}
	for _, addr := range expected {
		if !addresses[addr] {
			t.Errorf("Expected aircraft 0x%06X not found", addr)
		}
	}
}

func TestGreatcircle(t *testing.T) {
	// Cambridge UK to London
	// Cambridge: 52.2, 0.12
	// London: 51.5, -0.12
	distance := greatcircle(52.2, 0.12, 51.5, -0.12)

	// Distance should be approximately 78-82 km (haversine is more accurate)
	if distance < 75e3 || distance > 85e3 {
		t.Errorf("greatcircle() = %.0f meters, expected ~80km", distance)
	}

	t.Logf("Cambridge to London: %.2f km", distance/1000)
}

func TestGreatcircleSmallDistance(t *testing.T) {
	// Very close points (100 meters apart)
	lat1, lon1 := 52.0, 0.0
	lat2, lon2 := 52.0009, 0.0 // Approximately 100m north

	distance := greatcircle(lat1, lon1, lat2, lon2)

	// Should be approximately 100 meters
	if distance < 90 || distance > 110 {
		t.Errorf("greatcircle() = %.1f meters, expected ~100m", distance)
	}
}

func TestCombineValidity(t *testing.T) {
	now := uint64(time.Now().UnixMilli())

	v1 := DataValidity{
		Source:  SOURCE_ADSB,
		Updated: now,
		Stale:   now + 60000,
		Expires: now + 70000,
	}

	v2 := DataValidity{
		Source:  SOURCE_MODE_S,
		Updated: now + 5000,
		Stale:   now + 50000,
		Expires: now + 60000,
	}

	var result DataValidity
	combineValidity(&result, &v1, &v2)

	// Should take worst source
	if result.Source != SOURCE_MODE_S {
		t.Errorf("Combined source = %v, want MODE_S (worst)", result.Source)
	}

	// Should take later updated time
	if result.Updated != now+5000 {
		t.Errorf("Combined updated = %d, want %d", result.Updated, now+5000)
	}

	// Should take earlier stale time
	if result.Stale != now+50000 {
		t.Errorf("Combined stale = %d, want %d", result.Stale, now+50000)
	}

	// Should take earlier expires time
	if result.Expires != now+60000 {
		t.Errorf("Combined expires = %d, want %d", result.Expires, now+60000)
	}
}

func TestCompareValidity(t *testing.T) {
	now := uint64(time.Now().UnixMilli())

	// Better source, fresh
	v1 := DataValidity{
		Source:  SOURCE_ADSB,
		Updated: now,
		Stale:   now + 60000,
	}

	// Worse source, fresh
	v2 := DataValidity{
		Source:  SOURCE_MODE_S,
		Updated: now,
		Stale:   now + 60000,
	}

	if compareValidity(&v1, &v2, now) != 1 {
		t.Error("Better source should win")
	}

	if compareValidity(&v2, &v1, now) != -1 {
		t.Error("Worse source should lose")
	}

	// Same source, different update times
	v3 := DataValidity{
		Source:  SOURCE_ADSB,
		Updated: now + 5000,
		Stale:   now + 65000,
	}

	if compareValidity(&v3, &v1, now) != 1 {
		t.Error("More recent data should win when sources equal")
	}
}

func TestTrackerConcurrency(t *testing.T) {
	tracker := NewTracker()
	ModesChecksumInit(2)

	msgHex := "8D4840D6202CC371C32CE0576098"
	msg := mustDecodeHex(msgHex)

	// Spawn multiple goroutines updating the same aircraft
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				mm, _ := DecodeModesMessage(msg)
				tracker.UpdateFromMessage(mm)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have exactly 1 aircraft with 1000 messages
	a := tracker.GetAircraft(0x4840D6)
	if a == nil {
		t.Fatal("Aircraft not found")
	}

	if a.Messages != 1000 {
		t.Errorf("Messages = %d, want 1000", a.Messages)
	}
}

func TestSpeedCheck(t *testing.T) {
	tracker := NewTracker()

	// Start time: 0
	startTime := uint64(0)

	a := &Aircraft{
		Addr:          0x4840D6,
		Lat:           52.0,
		Lon:           0.0,
		Speed:         400, // 400 knots
		PositionValid: DataValidity{Source: SOURCE_ADSB, Updated: startTime},
		SpeedValid:    DataValidity{Source: SOURCE_ADSB, Updated: startTime},
	}

	now := uint64(10000) // 10 seconds later

	// New position 5 km away
	// At 400 knots (~741 km/h), in 10 seconds aircraft can travel ~2 km
	// But speed check adds margin (4/3) and base distance (500m airborne)
	// So max range = 500m + (10+1)sec * (400*4/3)kts * 1.852km/h / 3600
	// = 500m + 11 * 533.3 * 1.852 / 3.6 = 500m + 3025m = 3.5km
	// So 5km should fail, but let's test with a closer position first
	newLat := 52.02 // ~2.2 km north
	newLon := 0.0

	if !tracker.speedCheck(a, newLat, newLon, now, false) {
		t.Error("Speed check should pass for reasonable movement (~2km)")
	}

	// New position 20 km away - should definitely fail
	newLat2 := 52.18
	newLon2 := 0.0

	if tracker.speedCheck(a, newLat2, newLon2, now, false) {
		t.Error("Speed check should fail for impossible movement (~20km)")
	}
}

func TestTrackerSetReceiverLocation(t *testing.T) {
	tracker := NewTracker()

	if tracker.receiverLatLon {
		t.Error("Receiver location should not be set initially")
	}

	tracker.SetReceiverLocation(52.2, 0.12)

	if !tracker.receiverLatLon {
		t.Error("Receiver location should be set")
	}

	if tracker.receiverLat != 52.2 || tracker.receiverLon != 0.12 {
		t.Errorf("Receiver location = (%f, %f), want (52.2, 0.12)",
			tracker.receiverLat, tracker.receiverLon)
	}
}

func TestDataAge(t *testing.T) {
	now := uint64(time.Now().UnixMilli())

	v := DataValidity{
		Source:  SOURCE_ADSB,
		Updated: now - 5000,
	}

	age := dataAge(&v, now)
	if age != 5000 {
		t.Errorf("dataAge() = %d, want 5000", age)
	}

	// Invalid data should have maximum age
	vInvalid := DataValidity{Source: SOURCE_INVALID}
	age = dataAge(&vInvalid, now)
	if age != ^uint64(0) {
		t.Errorf("dataAge() for invalid = %d, want max", age)
	}
}

func BenchmarkTrackerUpdate(b *testing.B) {
	tracker := NewTracker()
	ModesChecksumInit(2)

	msgHex := "8D4840D6202CC371C32CE0576098"
	msg := mustDecodeHex(msgHex)

	mm, _ := DecodeModesMessage(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.UpdateFromMessage(mm)
	}
}

func TestModeACPeriodicRematch(t *testing.T) {
	tracker := NewTracker()
	ModesChecksumInit(2)

	now := uint64(time.Now().UnixMilli())

	// Create a Mode S aircraft with known squawk and altitude.
	tracker.mu.Lock()
	modeSAddr := uint32(0x4840D6)
	modeS := &Aircraft{
		Addr:     modeSAddr,
		AddrType: ADDR_ADSB_ICAO,
		Seen:     now,
		Messages: 10,
		SquawkValid: DataValidity{
			Source:  SOURCE_ADSB,
			Updated: now,
			Stale:   now + 60000,
			Expires: now + 70000,
		},
		Squawk: 0x1620,
		AltitudeValid: DataValidity{
			Source:  SOURCE_ADSB,
			Updated: now,
			Stale:   now + 60000,
			Expires: now + 70000,
		},
		Altitude:      100,
		AltitudeModeC: 1,
	}
	for i := range modeS.SignalLevel {
		modeS.SignalLevel[i] = 1e-5
	}
	tracker.aircraft[modeSAddr] = modeS
	tracker.mu.Unlock()

	// Create a Mode A/C message with matching squawk and altitude.
	// Mode A code 0x1620: A1(0x1000)+B4(0x0400)+B2(0x0200)+C2(0x0020)
	modeACCode := 0x1620
	mm := DecodeModeAMessage(modeACCode)
	if mm == nil {
		t.Fatal("DecodeModeAMessage returned nil")
	}

	tracker.UpdateFromModeAC(mm)

	// Verify the Mode A/C synthetic aircraft was created
	tracker.mu.RLock()
	var modeACAircraft *Aircraft
	for _, a := range tracker.aircraft {
		if a.ModeACFlags&MODEAC_MSG_FLAG != 0 {
			modeACAircraft = a
			break
		}
	}
	tracker.mu.RUnlock()

	if modeACAircraft == nil {
		t.Fatal("Mode A/C synthetic aircraft not created")
	}

	// PeriodicUpdate should rematch Mode A/C with Mode S aircraft
	tracker.PeriodicUpdate()

	tracker.mu.RLock()
	updatedModeS := tracker.aircraft[modeSAddr]
	tracker.mu.RUnlock()

	if updatedModeS == nil {
		t.Fatal("Mode S aircraft should still exist after PeriodicUpdate")
	}

	// Squawk matches (0x1620 == 0x1620), MODEA_HIT should be set.
	if updatedModeS.ModeACFlags&MODEAC_MSG_MODEA_HIT == 0 {
		t.Error("PeriodicUpdate should rematch Mode A/C records with Mode S aircraft (MODEA_HIT not set)")
	}
}

func TestModeACStaleFlagClearing(t *testing.T) {
	tracker := NewTracker()
	ModesChecksumInit(2)

	now := uint64(time.Now().UnixMilli())

	// Create Mode S aircraft with squawk 0x1620, altitude 100ft (ModeC=1).
	tracker.mu.Lock()
	modeSAddr := uint32(0x4840D6)
	tracker.aircraft[modeSAddr] = &Aircraft{
		Addr:     modeSAddr,
		AddrType: ADDR_ADSB_ICAO,
		Seen:     now,
		Messages: 10,
		SquawkValid: DataValidity{
			Source:  SOURCE_ADSB,
			Updated: now,
			Stale:   now + 60000,
			Expires: now + 70000,
		},
		Squawk: 0x1620,
		AltitudeValid: DataValidity{
			Source:  SOURCE_ADSB,
			Updated: now,
			Stale:   now + 60000,
			Expires: now + 70000,
		},
		Altitude:      100,
		AltitudeModeC: 1,
	}
	tracker.mu.Unlock()

	// Create a Mode A/C record with matching squawk 0x1620.
	mm := DecodeModeAMessage(0x1620)
	tracker.UpdateFromModeAC(mm)

	// First PeriodicUpdate: establishes the match.
	tracker.PeriodicUpdate()

	tracker.mu.RLock()
	modeS := tracker.aircraft[modeSAddr]
	hasModeAHit := modeS.ModeACFlags&MODEAC_MSG_MODEA_HIT != 0
	tracker.mu.RUnlock()

	if !hasModeAHit {
		t.Fatal("Precondition: MODEA_HIT should be set after initial match")
	}

	// Now change the Mode A/C synthetic aircraft's squawk so it no longer
	// matches Mode S aircraft. Direct mutation isolates PeriodicUpdate rematch
	// behavior without going through the full UpdateFromModeAC path.
	tracker.mu.Lock()
	for _, a := range tracker.aircraft {
		if a.ModeACFlags&MODEAC_MSG_FLAG != 0 {
			a.Squawk = 0x7700 // different squawk, no longer matches
			break
		}
	}
	tracker.mu.Unlock()

	// Second PeriodicUpdate: should clear stale flags on Mode S aircraft.
	tracker.PeriodicUpdate()

	tracker.mu.RLock()
	modeS = tracker.aircraft[modeSAddr]
	staleModeAHit := modeS.ModeACFlags&MODEAC_MSG_MODEA_HIT != 0
	tracker.mu.RUnlock()

	if staleModeAHit {
		t.Error("PeriodicUpdate should clear stale MODEA_HIT on Mode S aircraft when match no longer applies")
	}
}

func BenchmarkGreatcircle(b *testing.B) {
	lat1, lon1 := 52.2, 0.12
	lat2, lon2 := 51.5, -0.12

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = greatcircle(lat1, lon1, lat2, lon2)
	}
}
