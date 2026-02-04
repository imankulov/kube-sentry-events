package dedup

import (
	"testing"
	"time"
)

func TestDeduplicator_FirstEventIsNew(t *testing.T) {
	d := New(5 * time.Minute)

	isNew, count, _, _ := d.Check("default", "my-pod", "OOMKilled")

	if !isNew {
		t.Error("expected first event to be new")
	}
	if count != 1 {
		t.Errorf("expected count to be 1, got %d", count)
	}
}

func TestDeduplicator_DuplicateWithinWindow(t *testing.T) {
	d := New(5 * time.Minute)

	// First event
	isNew1, _, _, _ := d.Check("default", "my-pod", "OOMKilled")
	if !isNew1 {
		t.Error("expected first event to be new")
	}

	// Same event again (should be duplicate)
	isNew2, count2, _, _ := d.Check("default", "my-pod", "OOMKilled")
	if isNew2 {
		t.Error("expected second event to be duplicate")
	}
	if count2 != 2 {
		t.Errorf("expected count to be 2, got %d", count2)
	}

	// Third occurrence
	isNew3, count3, _, _ := d.Check("default", "my-pod", "OOMKilled")
	if isNew3 {
		t.Error("expected third event to be duplicate")
	}
	if count3 != 3 {
		t.Errorf("expected count to be 3, got %d", count3)
	}
}

func TestDeduplicator_DifferentEvents(t *testing.T) {
	d := New(5 * time.Minute)

	// Different pod
	isNew1, _, _, _ := d.Check("default", "pod-1", "OOMKilled")
	isNew2, _, _, _ := d.Check("default", "pod-2", "OOMKilled")

	if !isNew1 || !isNew2 {
		t.Error("expected different pods to be treated as new events")
	}

	// Different namespace
	isNew3, _, _, _ := d.Check("production", "pod-1", "OOMKilled")
	if !isNew3 {
		t.Error("expected different namespace to be treated as new event")
	}

	// Different reason
	isNew4, _, _, _ := d.Check("default", "pod-1", "CrashLoopBackOff")
	if !isNew4 {
		t.Error("expected different reason to be treated as new event")
	}
}

func TestDeduplicator_ExpiredEntry(t *testing.T) {
	// Use a very short window for testing
	d := New(10 * time.Millisecond)

	// First event
	isNew1, _, _, _ := d.Check("default", "my-pod", "OOMKilled")
	if !isNew1 {
		t.Error("expected first event to be new")
	}

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Same event after expiration should be new again
	isNew2, count2, _, _ := d.Check("default", "my-pod", "OOMKilled")
	if !isNew2 {
		t.Error("expected event after expiration to be new")
	}
	if count2 != 1 {
		t.Errorf("expected count to reset to 1, got %d", count2)
	}
}

func TestDeduplicator_GetStats(t *testing.T) {
	d := New(5 * time.Minute)

	// No entry yet
	_, _, _, exists := d.GetStats("default", "my-pod", "OOMKilled")
	if exists {
		t.Error("expected no stats for non-existent entry")
	}

	// Create entry
	d.Check("default", "my-pod", "OOMKilled")
	d.Check("default", "my-pod", "OOMKilled")

	count, firstSeen, lastSeen, exists := d.GetStats("default", "my-pod", "OOMKilled")
	if !exists {
		t.Error("expected stats to exist")
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
	if firstSeen.IsZero() || lastSeen.IsZero() {
		t.Error("expected timestamps to be set")
	}
	if lastSeen.Before(firstSeen) {
		t.Error("expected lastSeen to be >= firstSeen")
	}
}

func TestDeduplicator_Size(t *testing.T) {
	d := New(5 * time.Minute)

	if d.Size() != 0 {
		t.Errorf("expected initial size 0, got %d", d.Size())
	}

	d.Check("default", "pod-1", "OOMKilled")
	d.Check("default", "pod-2", "OOMKilled")
	d.Check("default", "pod-3", "OOMKilled")

	if d.Size() != 3 {
		t.Errorf("expected size 3, got %d", d.Size())
	}

	// Duplicate shouldn't increase size
	d.Check("default", "pod-1", "OOMKilled")
	if d.Size() != 3 {
		t.Errorf("expected size still 3, got %d", d.Size())
	}
}

func TestDeduplicator_MaxEntries(t *testing.T) {
	d := New(5 * time.Minute)

	// Add more than MaxEntries
	for i := 0; i < MaxEntries+100; i++ {
		d.Check("default", "pod-"+string(rune(i)), "OOMKilled")
	}

	if d.Size() > MaxEntries {
		t.Errorf("expected size <= %d, got %d", MaxEntries, d.Size())
	}
}

func TestDeduplicator_TimestampTracking(t *testing.T) {
	d := New(5 * time.Minute)

	before := time.Now()
	d.Check("default", "my-pod", "OOMKilled")
	time.Sleep(10 * time.Millisecond)
	d.Check("default", "my-pod", "OOMKilled")
	after := time.Now()

	_, firstSeen, lastSeen, _ := d.GetStats("default", "my-pod", "OOMKilled")

	if firstSeen.Before(before) || firstSeen.After(after) {
		t.Error("firstSeen timestamp out of expected range")
	}
	if lastSeen.Before(firstSeen) {
		t.Error("lastSeen should be >= firstSeen")
	}
}
