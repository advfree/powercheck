package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"powercheck/internal/buildinfo"
	"powercheck/internal/managerweb"
	"powercheck/internal/nutnetwork"
	"powercheck/internal/upshistory"
	"powercheck/internal/wol"
)

func main() {
	var (
		listen      = flag.String("listen", "0.0.0.0:8765", "manager web listen address")
		pveURL      = flag.String("pve-url", "", "PVE backend base URL")
		webRoot     = flag.String("web-root", "/usr/local/share/powercheck-manager/web", "directory containing the manager web console")
		nutAddress  = flag.String("nut-address", "", "NUT server address in host:port form")
		nutUPS      = flag.String("nut-ups", "", "NUT UPS name; empty discovers a single advertised UPS")
		nutHistory  = flag.String("nut-history-file", "", "path to persistent NUT history in JSONL format")
		eventFile   = flag.String("event-file", "", "path to persistent Manager event history in JSONL format")
		upsSpecPath = flag.String("ups-spec", "", "path to UPS and battery specification JSON")
		wolConfig   = flag.String("wol-config", "", "path to the WOL device configuration")
		versionOnly = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *versionOnly {
		fmt.Println(buildinfo.String("powercheck-manager"))
		return
	}
	events, err := managerweb.NewEventStore(*eventFile, 24*time.Hour)
	if err != nil {
		exitError(err)
	}
	upstream, err := managerweb.ParseUpstream(*pveURL)
	if err != nil {
		exitError(err)
	}
	var nutReader managerweb.NUTReader
	var historyReader managerweb.UPSHistoryReader
	if *nutAddress != "" {
		nutClient := nutnetwork.Client{
			Address: *nutAddress,
			UPSName: *nutUPS,
			Timeout: 5 * time.Second,
		}
		spec := upshistory.Spec{}
		if *upsSpecPath != "" {
			spec, err = upshistory.LoadSpec(*upsSpecPath)
			if err != nil {
				exitError(err)
			}
		}
		collector, collectorErr := upshistory.NewCollector(
			nutClient,
			*nutHistory,
			30*time.Second,
			24*time.Hour,
			spec,
		)
		if collectorErr != nil {
			exitError(collectorErr)
		}
		nutReader = collector
		historyReader = collector
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if collector, ok := nutReader.(*upshistory.Collector); ok {
		collector.Start(ctx)
	}

	var wolController managerweb.WOLController
	if *wolConfig != "" {
		config, err := wol.LoadConfig(*wolConfig)
		if err != nil {
			exitError(err)
		}
		wolController, err = wol.NewManager(ctx, config, wol.UDPSender{})
		if err != nil {
			exitError(err)
		}
	}
	server := managerweb.Server{
		Upstream:   upstream,
		WebRoot:    *webRoot,
		NUT:        nutReader,
		UPSHistory: historyReader,
		WOL:        wolController,
		Events:     events,
		Logger:     log.New(os.Stdout, "powercheck-manager ", log.LstdFlags|log.LUTC),
	}
	fmt.Printf("PowerCheck Manager listening on http://%s and proxying API requests to %s\n", *listen, upstream)
	if err := server.ListenAndServe(ctx, *listen); err != nil {
		exitError(err)
	}
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, "powercheck manager:", err)
	os.Exit(1)
}
