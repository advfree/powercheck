package wol

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type senderStub struct {
	mu      sync.Mutex
	devices []Device
	block   <-chan struct{}
	err     error
}

func (s *senderStub) Send(ctx context.Context, device Device) error {
	if s.block != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.block:
		}
	}
	s.mu.Lock()
	s.devices = append(s.devices, device)
	s.mu.Unlock()
	return s.err
}

func testConfig() Config {
	return Config{
		DurationSeconds: 1,
		IntervalSeconds: 1,
		Devices: []Device{{
			ID:        "pve-one",
			Name:      "PVE One",
			IP:        "192.168.1.66",
			MAC:       "7C:C3:85:BE:65:CC",
			Broadcast: "192.168.1.255",
			Port:      9,
			Enabled:   true,
		}},
	}
}

func TestManagerRunsConfiguredDeviceOnly(t *testing.T) {
	t.Parallel()
	sender := &senderStub{}
	manager, err := NewManager(context.Background(), testConfig(), sender)
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.Wake("pve-one")
	if err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if job.State != "running" || job.TotalAttempts != 1 {
		t.Fatalf("unexpected initial job: %#v", job)
	}
	deadline := time.Now().Add(time.Second)
	for {
		current := manager.Status().Devices[0].Job
		if current != nil && current.State == "completed" {
			if current.Attempts != 1 || current.PacketsSent != 1 {
				t.Fatalf("unexpected completed job: %#v", current)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("WOL job did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := manager.Wake("missing"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("missing device error=%v", err)
	}
}

func TestManagerRejectsConcurrentJobForSameDevice(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	manager, err := NewManager(context.Background(), testConfig(), &senderStub{block: release})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Wake("pve-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Wake("pve-one"); !errors.Is(err, ErrJobRunning) {
		t.Fatalf("second Wake error=%v, want ErrJobRunning", err)
	}
	close(release)
}
