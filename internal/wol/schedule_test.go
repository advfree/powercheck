package wol

import (
	"reflect"
	"testing"
	"time"
)

func TestDefaultRetryWindow(t *testing.T) {
	got, err := SendOffsets(2*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{
		0,
		30 * time.Second,
		60 * time.Second,
		90 * time.Second,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRetryOffsetsRejectsInvalidInput(t *testing.T) {
	if _, err := SendOffsets(0, time.Second); err == nil {
		t.Fatal("expected zero duration to fail")
	}
	if _, err := SendOffsets(time.Minute, 0); err == nil {
		t.Fatal("expected zero interval to fail")
	}
}
