package pvereader

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"powercheck/internal/readonlyexec"
)

type GuestType string

const (
	GuestQEMU GuestType = "qemu"
	GuestLXC  GuestType = "lxc"
)

type Guest struct {
	VMID     int       `json:"vmid"`
	Name     string    `json:"name"`
	Type     GuestType `json:"type"`
	Status   string    `json:"status"`
	Node     string    `json:"node"`
	Template bool      `json:"template"`
	Tags     []string  `json:"tags,omitempty"`
}

type AgentTestResult string

const (
	AgentSuccess AgentTestResult = "success"
	AgentFailure AgentTestResult = "failure"
	AgentTimeout AgentTestResult = "timeout"
)

type Client struct {
	Runner readonlyexec.Runner
	Node   string
}

func (c Client) ListGuests(ctx context.Context) ([]Guest, error) {
	if c.Runner == nil {
		return nil, fmt.Errorf("PVE command runner is required")
	}
	output, err := c.Runner.Run(
		ctx,
		"pvesh",
		"get", "/cluster/resources",
		"--type", "vm",
		"--output-format", "json",
	)
	if err != nil {
		return nil, err
	}

	var resources []resource
	if err := json.Unmarshal(output.Stdout, &resources); err != nil {
		return nil, fmt.Errorf("decode PVE guest list: %w", err)
	}

	guests := make([]Guest, 0, len(resources))
	for _, item := range resources {
		kind := GuestType(item.Type)
		if kind != GuestQEMU && kind != GuestLXC {
			continue
		}
		if c.Node != "" && item.Node != c.Node {
			continue
		}
		guests = append(guests, Guest{
			VMID:     item.VMID,
			Name:     item.Name,
			Type:     kind,
			Status:   item.Status,
			Node:     item.Node,
			Template: flexibleBool(item.Template),
			Tags:     splitTags(item.Tags),
		})
	}
	sort.Slice(guests, func(i, j int) bool {
		return guests[i].VMID < guests[j].VMID
	})
	return guests, nil
}

func (c Client) TestAgent(ctx context.Context, vmid int) (AgentTestResult, error) {
	if c.Runner == nil {
		return AgentFailure, fmt.Errorf("PVE command runner is required")
	}
	output, err := c.Runner.Run(ctx, "qm", "agent", strconv.Itoa(vmid), "ping")
	if err != nil {
		if ctx.Err() != nil {
			return AgentTimeout, ctx.Err()
		}
		return AgentFailure, fmt.Errorf("QEMU Guest Agent ping for VM %d failed: %w; stderr: %s", vmid, err, strings.TrimSpace(string(output.Stderr)))
	}
	return AgentSuccess, nil
}

func AllGuestsStopped(guests []Guest) bool {
	for _, guest := range guests {
		if guest.Template {
			continue
		}
		if guest.Status != "stopped" {
			return false
		}
	}
	return true
}

type resource struct {
	VMID     int             `json:"vmid"`
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Status   string          `json:"status"`
	Node     string          `json:"node"`
	Template json.RawMessage `json:"template"`
	Tags     string          `json:"tags"`
}

func flexibleBool(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value == "1" || value == "true" || value == `"1"`
}

func splitTags(raw string) []string {
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == ','
	})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if value := strings.TrimSpace(field); value != "" {
			result = append(result, value)
		}
	}
	return result
}
