package nutreader

import (
	"context"
	"fmt"
	"strings"

	"powercheck/internal/core"
	"powercheck/internal/readonlyexec"
)

type Reading struct {
	Target    string            `json:"target"`
	Status    core.NUTStatus    `json:"status"`
	RawStatus string            `json:"raw_status,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
}

type Client struct {
	Runner readonlyexec.Runner
	Target string
}

func (c Client) Read(ctx context.Context) (Reading, error) {
	reading := Reading{
		Target:    c.Target,
		Status:    core.NUTUnreachable,
		Variables: make(map[string]string),
	}
	if c.Runner == nil {
		return reading, fmt.Errorf("NUT command runner is required")
	}
	output, err := c.Runner.Run(ctx, "upsc", c.Target)
	if err != nil {
		return reading, err
	}

	for lineNumber, line := range strings.Split(string(output.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return reading, fmt.Errorf("invalid upsc output on line %d", lineNumber+1)
		}
		reading.Variables[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	rawStatus, found := reading.Variables["ups.status"]
	if !found {
		return reading, fmt.Errorf("upsc output does not contain ups.status")
	}
	reading.RawStatus = rawStatus
	status, err := parseStatus(rawStatus)
	if err != nil {
		return reading, err
	}
	reading.Status = status
	return reading, nil
}

func parseStatus(raw string) (core.NUTStatus, error) {
	tokens := strings.Fields(raw)
	for _, token := range tokens {
		if token == "LB" {
			return core.NUTLowBattery, nil
		}
	}
	for _, token := range tokens {
		if token == "OB" {
			return core.NUTOnBattery, nil
		}
	}
	for _, token := range tokens {
		if token == "OL" {
			return core.NUTOnline, nil
		}
	}
	return core.NUTUnreachable, fmt.Errorf("unsupported NUT status %q", raw)
}
