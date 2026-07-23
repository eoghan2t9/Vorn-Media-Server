package logging

import (
	"bytes"
	"testing"
	"time"
)

func TestBuffer_WritesThroughAndTrims(t *testing.T) {
	var underlying bytes.Buffer
	b := NewBuffer(&underlying, 2)

	b.Write([]byte("line1\n"))
	b.Write([]byte("line2\n"))
	b.Write([]byte("line3\n"))

	if underlying.String() != "line1\nline2\nline3\n" {
		t.Errorf("underlying writer got %q, want all three lines written through", underlying.String())
	}

	recent := b.Recent()
	if len(recent) != 2 || recent[0] != "line2" || recent[1] != "line3" {
		t.Errorf("Recent() = %v, want [line2 line3] (max=2 should trim oldest)", recent)
	}
}

func TestBuffer_Subscribe(t *testing.T) {
	var underlying bytes.Buffer
	b := NewBuffer(&underlying, 100)
	b.Write([]byte("before-subscribe\n"))

	ch, unsubscribe := b.Subscribe()
	defer unsubscribe()

	b.Write([]byte("after-subscribe\n"))

	select {
	case line := <-ch:
		if line != "after-subscribe" {
			t.Errorf("got %q, want after-subscribe", line)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscribed line")
	}
}

func TestBuffer_SubscribeDoesNotBlockOnSlowReader(t *testing.T) {
	var underlying bytes.Buffer
	b := NewBuffer(&underlying, 100)
	ch, unsubscribe := b.Subscribe()
	defer unsubscribe()

	// Fill the subscriber channel past capacity without draining it; Write
	// must not block even though the subscriber is stalled.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			b.Write([]byte("spam\n"))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked on a slow subscriber")
	}
	_ = ch
}
