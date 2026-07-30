package wol

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

const (
	defaultDuration = 2 * time.Minute
	defaultInterval = 30 * time.Second
	defaultPort     = 9
)

type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Broadcast string `json:"broadcast"`
	Port      int    `json:"port"`
	Enabled   bool   `json:"enabled"`
	Note      string `json:"note,omitempty"`
}

type Config struct {
	DurationSeconds int      `json:"duration_seconds"`
	IntervalSeconds int      `json:"interval_seconds"`
	Devices         []Device `json:"devices"`
}

func LoadConfig(filePath string) (Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return Config{}, fmt.Errorf("open WOL config: %w", err)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode WOL config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c *Config) Validate() error {
	if c.DurationSeconds == 0 {
		c.DurationSeconds = int(defaultDuration.Seconds())
	}
	if c.IntervalSeconds == 0 {
		c.IntervalSeconds = int(defaultInterval.Seconds())
	}
	if c.DurationSeconds < 1 || c.DurationSeconds > 600 {
		return fmt.Errorf("WOL duration must be between 1 and 600 seconds")
	}
	if c.IntervalSeconds < 1 || c.IntervalSeconds > 600 {
		return fmt.Errorf("WOL interval must be between 1 and 600 seconds")
	}
	duration := time.Duration(c.DurationSeconds) * time.Second
	interval := time.Duration(c.IntervalSeconds) * time.Second
	offsets, err := SendOffsets(duration, interval)
	if err != nil {
		return fmt.Errorf("invalid WOL retry schedule: %w", err)
	}
	if len(offsets) > 20 {
		return fmt.Errorf("WOL retry schedule must not exceed 20 sends")
	}
	if len(c.Devices) == 0 {
		return fmt.Errorf("at least one WOL device is required")
	}

	seen := make(map[string]struct{}, len(c.Devices))
	for index := range c.Devices {
		device := &c.Devices[index]
		if err := validateDevice(device); err != nil {
			return fmt.Errorf("device %d: %w", index+1, err)
		}
		if _, exists := seen[device.ID]; exists {
			return fmt.Errorf("device %d: duplicate id %q", index+1, device.ID)
		}
		seen[device.ID] = struct{}{}
	}
	return nil
}

func (c Config) Duration() time.Duration {
	return time.Duration(c.DurationSeconds) * time.Second
}

func (c Config) Interval() time.Duration {
	return time.Duration(c.IntervalSeconds) * time.Second
}

func validateDevice(device *Device) error {
	device.ID = strings.TrimSpace(device.ID)
	device.Name = strings.TrimSpace(device.Name)
	device.IP = strings.TrimSpace(device.IP)
	device.MAC = strings.ToUpper(strings.TrimSpace(device.MAC))
	device.Broadcast = strings.TrimSpace(device.Broadcast)
	device.Note = strings.TrimSpace(device.Note)
	if !safeID(device.ID) {
		return fmt.Errorf("id must contain only letters, digits, dot, dash, or underscore")
	}
	if device.Name == "" || len(device.Name) > 80 {
		return fmt.Errorf("name must contain 1 to 80 characters")
	}
	if parsed := net.ParseIP(device.IP); parsed == nil || parsed.To4() == nil {
		return fmt.Errorf("ip must be an IPv4 address")
	}
	broadcast := net.ParseIP(device.Broadcast)
	if broadcast == nil || broadcast.To4() == nil || broadcast.IsUnspecified() || broadcast.IsMulticast() {
		return fmt.Errorf("broadcast must be a usable IPv4 broadcast address")
	}
	hardware, err := net.ParseMAC(device.MAC)
	if err != nil || len(hardware) != 6 {
		return fmt.Errorf("mac must be a 6-byte hardware address")
	}
	device.MAC = strings.ToUpper(hardware.String())
	if device.Port == 0 {
		device.Port = defaultPort
	}
	if device.Port < 1 || device.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func safeID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '-', character == '_':
		default:
			return false
		}
	}
	return true
}
