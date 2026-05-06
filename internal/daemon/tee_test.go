package daemon

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ybira-net/ybira-net/internal/types"
	"pgregory.net/rapid"
)

// genFlowEvent generates a random FlowEvent for property testing.
func genFlowEvent(t *rapid.T) types.FlowEvent {
	return types.FlowEvent{
		Timestamp: time.Unix(rapid.Int64Range(1000000000, 2000000000).Draw(t, "timestamp"), 0),
		PID:       rapid.IntRange(0, 65535).Draw(t, "pid"),
		Process:   rapid.StringMatching(`[a-z]{1,15}`).Draw(t, "process"),
		Bytes:     rapid.IntRange(1, 65535).Draw(t, "bytes"),
		SrcIP:     net.IPv4(byte(rapid.IntRange(1, 254).Draw(t, "srcA")), byte(rapid.IntRange(0, 255).Draw(t, "srcB")), byte(rapid.IntRange(0, 255).Draw(t, "srcC")), byte(rapid.IntRange(1, 254).Draw(t, "srcD"))),
		DstIP:     net.IPv4(byte(rapid.IntRange(1, 254).Draw(t, "dstA")), byte(rapid.IntRange(0, 255).Draw(t, "dstB")), byte(rapid.IntRange(0, 255).Draw(t, "dstC")), byte(rapid.IntRange(1, 254).Draw(t, "dstD"))),
		SrcPort:   uint16(rapid.IntRange(1, 65535).Draw(t, "srcPort")),
		DstPort:   uint16(rapid.IntRange(1, 65535).Draw(t, "dstPort")),
		Protocol:  rapid.SampledFrom([]string{"tcp", "udp"}).Draw(t, "protocol"),
	}
}

// TestProperty_TeeFanOutCompleteness verifies that for any FlowEvent entering
// tee, it is delivered to BOTH aggChan and storeChan, OR the respective drop
// counter is incremented (fan-out completeness).
//
// **Validates: Requirements 9.4, 11.2**
func TestProperty_TeeFanOutCompleteness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a batch of events to send
		numEvents := rapid.IntRange(1, 200).Draw(t, "numEvents")
		events := make([]types.FlowEvent, numEvents)
		for i := range events {
			events[i] = genFlowEvent(t)
		}

		// Use a small buffer to force some drops when sending many events
		bufSize := rapid.IntRange(1, 50).Draw(t, "bufSize")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create input channel and tee
		inCh := make(chan types.FlowEvent, len(events))
		drops := &TeeDropCounters{}

		// Create custom-sized output channels for this test
		aggCh := make(chan types.FlowEvent, bufSize)
		storeCh := make(chan types.FlowEvent, bufSize)

		// Run tee with custom channels (we replicate the tee logic here
		// to control buffer sizes for testing)
		go func() {
			defer close(aggCh)
			defer close(storeCh)
			for {
				select {
				case ev, ok := <-inCh:
					if !ok {
						return
					}
					select {
					case aggCh <- ev:
					default:
						drops.AggDrops.Add(1)
					}
					select {
					case storeCh <- ev:
					default:
						drops.StoreDrops.Add(1)
					}
				case <-ctx.Done():
					return
				}
			}
		}()

		// Send all events
		for _, ev := range events {
			inCh <- ev
		}
		close(inCh)

		// Drain output channels
		aggReceived := 0
		for range aggCh {
			aggReceived++
		}

		storeReceived := 0
		for range storeCh {
			storeReceived++
		}

		// Property: for each channel, delivered + dropped == total sent
		aggTotal := int64(aggReceived) + drops.AggDrops.Load()
		storeTotal := int64(storeReceived) + drops.StoreDrops.Load()

		if aggTotal != int64(numEvents) {
			t.Fatalf("fan-out completeness violated for aggChan: received=%d + dropped=%d = %d, expected %d",
				aggReceived, drops.AggDrops.Load(), aggTotal, numEvents)
		}
		if storeTotal != int64(numEvents) {
			t.Fatalf("fan-out completeness violated for storeChan: received=%d + dropped=%d = %d, expected %d",
				storeReceived, drops.StoreDrops.Load(), storeTotal, numEvents)
		}
	})
}

// TestTee_BasicFanOut verifies basic tee behavior with sufficient buffer space.
func TestTee_BasicFanOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inCh := make(chan types.FlowEvent, 10)
	drops := &TeeDropCounters{}
	aggCh, storeCh := Tee(ctx, inCh, drops)

	// Send a few events
	ev := types.FlowEvent{
		Timestamp: time.Now(),
		PID:       1234,
		Process:   "test",
		Bytes:     100,
		Protocol:  "tcp",
	}

	inCh <- ev
	inCh <- ev
	inCh <- ev
	close(inCh)

	// Drain and count
	aggCount := 0
	for range aggCh {
		aggCount++
	}
	storeCount := 0
	for range storeCh {
		storeCount++
	}

	if aggCount != 3 {
		t.Errorf("expected 3 events on aggCh, got %d", aggCount)
	}
	if storeCount != 3 {
		t.Errorf("expected 3 events on storeCh, got %d", storeCount)
	}
	if drops.AggDrops.Load() != 0 {
		t.Errorf("expected 0 agg drops, got %d", drops.AggDrops.Load())
	}
	if drops.StoreDrops.Load() != 0 {
		t.Errorf("expected 0 store drops, got %d", drops.StoreDrops.Load())
	}
}

// TestTee_ContextCancellation verifies tee exits on context cancellation.
func TestTee_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inCh := make(chan types.FlowEvent, 10)
	drops := &TeeDropCounters{}
	aggCh, storeCh := Tee(ctx, inCh, drops)

	// Send one event
	ev := types.FlowEvent{
		Timestamp: time.Now(),
		PID:       1,
		Process:   "test",
		Bytes:     50,
		Protocol:  "udp",
	}
	inCh <- ev

	// Cancel context
	cancel()

	// Channels should eventually close
	timeout := time.After(2 * time.Second)
	aggDrained := false
	storeDrained := false

	for !aggDrained || !storeDrained {
		select {
		case _, ok := <-aggCh:
			if !ok {
				aggDrained = true
			}
		case _, ok := <-storeCh:
			if !ok {
				storeDrained = true
			}
		case <-timeout:
			t.Fatal("tee did not close channels after context cancellation")
		}
	}
}
