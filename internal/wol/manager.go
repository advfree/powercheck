package wol

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrDeviceNotFound = errors.New("WOL device not found")
	ErrDeviceDisabled = errors.New("WOL device is disabled")
	ErrJobRunning     = errors.New("WOL job is already running for this device")
)

type Job struct {
	DeviceID      string     `json:"device_id"`
	State         string     `json:"state"`
	Attempts      int        `json:"attempts"`
	PacketsSent   int        `json:"packets_sent"`
	TotalAttempts int        `json:"total_attempts"`
	StartedAt     time.Time  `json:"started_at"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

type DeviceStatus struct {
	Device
	Job *Job `json:"job,omitempty"`
}

type Status struct {
	DurationSeconds int            `json:"duration_seconds"`
	IntervalSeconds int            `json:"interval_seconds"`
	Devices         []DeviceStatus `json:"devices"`
}

type Manager struct {
	context context.Context
	config  Config
	sender  Sender
	mu      sync.Mutex
	jobs    map[string]Job
}

func NewManager(ctx context.Context, config Config, sender Sender) (*Manager, error) {
	if ctx == nil {
		return nil, fmt.Errorf("WOL context is required")
	}
	if sender == nil {
		return nil, fmt.Errorf("WOL sender is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Manager{
		context: ctx,
		config:  config,
		sender:  sender,
		jobs:    make(map[string]Job),
	}, nil
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	devices := make([]DeviceStatus, 0, len(m.config.Devices))
	for _, device := range m.config.Devices {
		status := DeviceStatus{Device: device}
		if current, ok := m.jobs[device.ID]; ok {
			copy := current
			status.Job = &copy
		}
		devices = append(devices, status)
	}
	return Status{
		DurationSeconds: m.config.DurationSeconds,
		IntervalSeconds: m.config.IntervalSeconds,
		Devices:         devices,
	}
}

func (m *Manager) Wake(deviceID string) (Job, error) {
	m.mu.Lock()
	device, found := m.device(deviceID)
	if !found {
		m.mu.Unlock()
		return Job{}, ErrDeviceNotFound
	}
	if !device.Enabled {
		m.mu.Unlock()
		return Job{}, ErrDeviceDisabled
	}
	if current, exists := m.jobs[deviceID]; exists && current.State == "running" {
		m.mu.Unlock()
		return Job{}, ErrJobRunning
	}
	offsets, err := SendOffsets(m.config.Duration(), m.config.Interval())
	if err != nil {
		m.mu.Unlock()
		return Job{}, err
	}
	job := Job{
		DeviceID:      deviceID,
		State:         "running",
		TotalAttempts: len(offsets),
		StartedAt:     time.Now().UTC(),
	}
	m.jobs[deviceID] = job
	m.mu.Unlock()

	go m.run(device, offsets)
	return job, nil
}

func (m *Manager) run(device Device, offsets []time.Duration) {
	started := time.Now()
	for index, offset := range offsets {
		if index > 0 {
			wait := time.Until(started.Add(offset))
			if wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-m.context.Done():
					timer.Stop()
					m.finishCancelled(device.ID, m.context.Err())
					return
				case <-timer.C:
				}
			}
		}

		sendContext, cancel := context.WithTimeout(m.context, 5*time.Second)
		err := m.sender.Send(sendContext, device)
		cancel()
		m.recordAttempt(device.ID, err)
	}
	m.finish(device.ID)
}

func (m *Manager) recordAttempt(deviceID string, sendError error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[deviceID]
	now := time.Now().UTC()
	job.Attempts++
	job.LastAttemptAt = &now
	if sendError == nil {
		job.PacketsSent++
	} else {
		job.LastError = sendError.Error()
	}
	m.jobs[deviceID] = job
}

func (m *Manager) finish(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[deviceID]
	now := time.Now().UTC()
	job.CompletedAt = &now
	if job.PacketsSent > 0 {
		job.State = "completed"
	} else {
		job.State = "failed"
	}
	m.jobs[deviceID] = job
}

func (m *Manager) finishCancelled(deviceID string, cancellation error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[deviceID]
	now := time.Now().UTC()
	job.CompletedAt = &now
	job.State = "cancelled"
	if cancellation != nil {
		job.LastError = cancellation.Error()
	}
	m.jobs[deviceID] = job
}

func (m *Manager) device(deviceID string) (Device, bool) {
	for _, device := range m.config.Devices {
		if device.ID == deviceID {
			return device, true
		}
	}
	return Device{}, false
}
