package dryrun

import (
	"context"
	"fmt"
	"sync"
	"time"

	"powercheck/internal/core"
	"powercheck/internal/nutreader"
	"powercheck/internal/pvereader"
	"powercheck/internal/reachability"
)

type PVEReader interface {
	ListGuests(context.Context) ([]pvereader.Guest, error)
}

type NUTReader interface {
	Read(context.Context) (nutreader.Reading, error)
}

type NetworkProber interface {
	Probe(context.Context, string) reachability.Result
}

type Issue struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

type Report struct {
	Mode             string                `json:"mode"`
	CollectedAt      time.Time             `json:"collected_at"`
	CollectionTimeMS int64                 `json:"collection_time_ms"`
	Snapshot         core.Snapshot         `json:"snapshot"`
	NUT              nutreader.Reading     `json:"nut"`
	PVEAvailable     bool                  `json:"pve_available"`
	Guests           []pvereader.Guest     `json:"guests"`
	LAN              []reachability.Result `json:"lan"`
	WAN              []reachability.Result `json:"wan"`
	Issues           []Issue               `json:"issues,omitempty"`
	PlannedActions   []PlannedAction       `json:"planned_actions,omitempty"`
	SingleSampleNote string                `json:"single_sample_note,omitempty"`
}

type Collector struct {
	Config Config
	PVE    PVEReader
	NUT    NUTReader
	Ping   NetworkProber
	Now    func() time.Time
}

func NewCollector(config Config, pve PVEReader, nut NUTReader, ping NetworkProber) (*Collector, error) {
	switch {
	case pve == nil:
		return nil, fmt.Errorf("PVE reader is required")
	case nut == nil:
		return nil, fmt.Errorf("NUT reader is required")
	case ping == nil:
		return nil, fmt.Errorf("network prober is required")
	}
	return &Collector{Config: config, PVE: pve, NUT: nut, Ping: ping}, nil
}

type pveResult struct {
	guests []pvereader.Guest
	err    error
}

type nutResult struct {
	reading nutreader.Reading
	err     error
}

func (c Collector) Collect(parent context.Context) Report {
	started := time.Now()
	now := c.Now
	if now == nil {
		now = time.Now
	}
	report := Report{
		Mode:        "dry-run",
		CollectedAt: now(),
		LAN:         make([]reachability.Result, len(c.Config.LANTargets)),
		WAN:         make([]reachability.Result, len(c.Config.WANTargets)),
	}

	ctx, cancel := context.WithTimeout(parent, c.Config.RoundTimeout)
	defer cancel()

	pveChannel := make(chan pveResult, 1)
	nutChannel := make(chan nutResult, 1)
	go func() {
		pveContext, pveCancel := context.WithTimeout(ctx, c.Config.PVECommandTimeout)
		defer pveCancel()
		guests, err := c.PVE.ListGuests(pveContext)
		pveChannel <- pveResult{guests: guests, err: err}
	}()
	go func() {
		reading, err := c.NUT.Read(ctx)
		nutChannel <- nutResult{reading: reading, err: err}
	}()

	pveData := <-pveChannel
	nutData := <-nutChannel
	if nutData.err != nil {
		var wait sync.WaitGroup
		for index, target := range c.Config.LANTargets {
			wait.Add(1)
			go func(i int, value string) {
				defer wait.Done()
				report.LAN[i] = c.Ping.Probe(ctx, value)
			}(index, target)
		}
		for index, target := range c.Config.WANTargets {
			wait.Add(1)
			go func(i int, value string) {
				defer wait.Done()
				report.WAN[i] = c.Ping.Probe(ctx, value)
			}(index, target)
		}
		wait.Wait()
	}

	report.Guests = pveData.guests
	report.PVEAvailable = pveData.err == nil
	if pveData.err != nil {
		report.Issues = append(report.Issues, Issue{Source: "pve", Message: pveData.err.Error()})
	}
	report.NUT = nutData.reading
	if nutData.err != nil {
		report.Issues = append(report.Issues, Issue{Source: "nut", Message: nutData.err.Error()})
		report.NUT.Status = core.NUTUnreachable
	}
	for _, result := range report.LAN {
		if result.Error != "" {
			report.Issues = append(report.Issues, Issue{Source: "lan:" + result.Target, Message: result.Error})
		}
	}
	for _, result := range report.WAN {
		if result.Error != "" {
			report.Issues = append(report.Issues, Issue{Source: "wan:" + result.Target, Message: result.Error})
		}
	}

	lanReachable := true
	wanReachable := true
	if nutData.err != nil {
		lanReachable = anyReachable(report.LAN)
		wanReachable = anyReachable(report.WAN)
	}
	report.Snapshot = core.Snapshot{
		NUT:              report.NUT.Status,
		LANReachable:     lanReachable,
		WANReachable:     wanReachable,
		AllGuestsStopped: report.PVEAvailable && pvereader.AllGuestsStopped(report.Guests),
	}
	report.CollectionTimeMS = time.Since(started).Milliseconds()
	return report
}

func anyReachable(results []reachability.Result) bool {
	for _, result := range results {
		if result.Reachable {
			return true
		}
	}
	return false
}
