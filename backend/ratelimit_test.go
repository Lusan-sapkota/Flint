package main

import (
	"testing"
	"time"
)

func TestRateLimiter_BlocksAfterLimit(t *testing.T) {
	rl := newRateLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("expected attempt %d to be allowed", i+1)
		}
	}
	if rl.allow("1.2.3.4") {
		t.Fatal("expected the 4th attempt within the window to be blocked")
	}
}

func TestRateLimiter_TracksKeysIndependently(t *testing.T) {
	rl := newRateLimiter(1, time.Minute)

	if !rl.allow("1.1.1.1") {
		t.Fatal("expected first attempt for 1.1.1.1 to be allowed")
	}
	if rl.allow("1.1.1.1") {
		t.Fatal("expected second attempt for 1.1.1.1 to be blocked")
	}
	if !rl.allow("2.2.2.2") {
		t.Fatal("expected a different key to have its own independent limit")
	}
}

func TestRateLimiter_AllowsAgainAfterWindowExpires(t *testing.T) {
	rl := newRateLimiter(1, 50*time.Millisecond)

	if !rl.allow("3.3.3.3") {
		t.Fatal("expected first attempt to be allowed")
	}
	if rl.allow("3.3.3.3") {
		t.Fatal("expected second immediate attempt to be blocked")
	}

	time.Sleep(60 * time.Millisecond)

	if !rl.allow("3.3.3.3") {
		t.Fatal("expected attempt after the window expired to be allowed again")
	}
}
