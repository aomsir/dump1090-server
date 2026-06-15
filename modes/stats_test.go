package modes

import (
	"testing"
)

// clock is a test helper that provides a controllable time source.
type clock struct {
	now uint64 // Unix milliseconds
}

func (c *clock) Now() uint64 {
	return c.now
}

func (c *clock) Advance(ms uint64) {
	c.now += ms
}

// newTestStatsCollector creates a StatsCollector with injected clock for testing.
func newTestStatsCollector(initialMs uint64) (*StatsCollector, *clock) {
	clk := &clock{now: initialMs}
	sc := &StatsCollector{
		latestIndex: STATS_HISTORY_SIZE - 1,
		nextUpdate:  initialMs + 60000,
		clock:       clk.Now,
	}
	sc.current.Start = initialMs
	sc.current.End = initialMs
	sc.allTime.Start = initialMs
	return sc, clk
}

func TestStatsLatestIsCurrentWindow(t *testing.T) {
	sc, clk := newTestStatsCollector(1000000)

	// Add messages to the current (not-yet-rotated) window
	sc.AddMessage()
	sc.AddMessage()
	sc.AddMessage()

	// latest should reflect current window counters
	latest := sc.GetLatest()
	if latest.MessagesTotal != 3 {
		t.Errorf("latest.MessagesTotal = %d, want 3 (current window)", latest.MessagesTotal)
	}

	// Advance time but NOT past the 60s rotation boundary
	clk.Advance(30000) // 30s
	sc.Update()

	latest = sc.GetLatest()
	if latest.MessagesTotal != 3 {
		t.Errorf("latest.MessagesTotal after 30s = %d, want 3 (still current window)", latest.MessagesTotal)
	}
}

func TestStatsLast1MinIsMostRecentCompleteWindow(t *testing.T) {
	sc, clk := newTestStatsCollector(1000000)

	// Add messages in first interval
	sc.AddMessage()
	sc.AddMessage()

	// Rotate to next interval (advance past 60s)
	clk.Advance(61000)
	sc.Update()

	// last1min should reflect the rotated (now complete) interval
	last1min := sc.GetLast1Min()
	if last1min.MessagesTotal != 2 {
		t.Errorf("last1min.MessagesTotal = %d, want 2 (first rotated interval)", last1min.MessagesTotal)
	}

	// latest should now reflect the NEW current window (empty)
	latest := sc.GetLatest()
	if latest.MessagesTotal != 0 {
		t.Errorf("latest.MessagesTotal = %d, want 0 (new empty current window)", latest.MessagesTotal)
	}

	// Add messages in second interval
	sc.AddMessage()
	sc.AddMessage()
	sc.AddMessage()

	// Rotate again
	clk.Advance(61000)
	sc.Update()

	// last1min should now be the second interval (3 messages)
	last1min = sc.GetLast1Min()
	if last1min.MessagesTotal != 3 {
		t.Errorf("last1min.MessagesTotal after second rotation = %d, want 3", last1min.MessagesTotal)
	}
}

func TestStatsTotalIncludesCurrentWindow(t *testing.T) {
	sc, clk := newTestStatsCollector(1000000)

	// Add messages in first interval
	sc.AddMessage()
	sc.AddMessage()
	sc.AddMessage()

	// total should include current window
	total := sc.GetAllTime()
	if total.MessagesTotal != 3 {
		t.Errorf("total.MessagesTotal (before rotation) = %d, want 3", total.MessagesTotal)
	}

	// Rotate
	clk.Advance(61000)
	sc.Update()

	// Add more in second interval
	sc.AddMessage()
	sc.AddMessage()

	// total should include both rotated (3) and current (2)
	total = sc.GetAllTime()
	if total.MessagesTotal != 5 {
		t.Errorf("total.MessagesTotal (after rotation) = %d, want 5 (3 rotated + 2 current)", total.MessagesTotal)
	}

	// Rotate again
	clk.Advance(61000)
	sc.Update()

	// total should include all: 3 + 2 = 5
	total = sc.GetAllTime()
	if total.MessagesTotal != 5 {
		t.Errorf("total.MessagesTotal (after second rotation) = %d, want 5", total.MessagesTotal)
	}
}

func TestStatsCollectorTracksMessagesRemoteAndAircraft(t *testing.T) {
	sc, clk := newTestStatsCollector(1000000)

	// Test message counter
	sc.AddMessage()
	sc.AddMessage()
	if sc.current.MessagesTotal != 2 {
		t.Errorf("MessagesTotal = %d, want 2", sc.current.MessagesTotal)
	}

	// Test remote counters
	sc.AddRemoteMessage(false, 0, false) // Mode S, 0-bit accepted
	sc.AddRemoteMessage(false, 1, false) // Mode S, 1-bit accepted
	sc.AddRemoteMessage(true, 0, false)  // Mode AC — counted separately, not in accepted/rejected
	sc.AddRemoteMessage(false, 0, true)  // Mode S, unknown ICAO
	if sc.current.RemoteReceivedModes != 3 {
		t.Errorf("RemoteReceivedModes = %d, want 3", sc.current.RemoteReceivedModes)
	}
	if sc.current.RemoteReceivedModeAC != 1 {
		t.Errorf("RemoteReceivedModeAC = %d, want 1", sc.current.RemoteReceivedModeAC)
	}
	if sc.current.RemoteAccepted[0] != 1 {
		t.Errorf("RemoteAccepted[0] = %d, want 1 (only Mode S 0-bit-error)", sc.current.RemoteAccepted[0])
	}
	if sc.current.RemoteAccepted[1] != 1 {
		t.Errorf("RemoteAccepted[1] = %d, want 1", sc.current.RemoteAccepted[1])
	}
	if sc.current.RemoteRejectedUnknownICAO != 1 {
		t.Errorf("RemoteRejectedUnknownICAO = %d, want 1", sc.current.RemoteRejectedUnknownICAO)
	}

	// Test aircraft counters
	sc.AddUniqueAircraft()
	sc.AddUniqueAircraft()
	sc.AddSingleMessageAircraft()
	if sc.current.UniqueAircraft != 2 {
		t.Errorf("UniqueAircraft = %d, want 2", sc.current.UniqueAircraft)
	}
	if sc.current.SingleMessageAircraft != 1 {
		t.Errorf("SingleMessageAircraft = %d, want 1", sc.current.SingleMessageAircraft)
	}

	// Verify these flow through to total
	clk.Advance(61000)
	sc.Update()
	total := sc.GetAllTime()
	if total.MessagesTotal != 2 {
		t.Errorf("total.MessagesTotal = %d, want 2", total.MessagesTotal)
	}
	if total.UniqueAircraft != 2 {
		t.Errorf("total.UniqueAircraft = %d, want 2", total.UniqueAircraft)
	}
	if total.SingleMessageAircraft != 1 {
		t.Errorf("total.SingleMessageAircraft = %d, want 1", total.SingleMessageAircraft)
	}
	if total.RemoteReceivedModes != 3 {
		t.Errorf("total.RemoteReceivedModes = %d, want 3", total.RemoteReceivedModes)
	}
}
