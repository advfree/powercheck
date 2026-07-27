package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"powercheck/internal/buildinfo"
	"powercheck/internal/configfile"
	"powercheck/internal/core"
	"powercheck/internal/sim"
)

func main() {
	var (
		runAll       = flag.Bool("all", false, "run all JSON scenarios in the scenario directory")
		scenarioPath = flag.String("scenario", "", "run one scenario JSON file")
		scenarioDir  = flag.String("dir", "scenarios", "scenario directory used with -all")
		configPath   = flag.String("config", "", "optional JSON timing configuration")
		versionOnly  = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *versionOnly {
		fmt.Println(buildinfo.String("powercheck-sim"))
		return
	}

	config := core.DefaultConfig()
	if *configPath != "" {
		var err error
		config, err = configfile.Load(*configPath, config)
		if err != nil {
			exitError(err)
		}
	}

	var paths []string
	switch {
	case *runAll:
		matches, err := filepath.Glob(filepath.Join(*scenarioDir, "*.json"))
		if err != nil {
			exitError(err)
		}
		paths = matches
	case *scenarioPath != "":
		paths = []string{*scenarioPath}
	default:
		fmt.Fprintln(os.Stderr, "use -all or -scenario <file>")
		flag.Usage()
		os.Exit(2)
	}

	sort.Strings(paths)
	if len(paths) == 0 {
		exitError(fmt.Errorf("no scenarios found"))
	}

	failed := false
	for _, path := range paths {
		scenario, err := sim.LoadScenario(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", path, err)
			failed = true
			continue
		}
		result, err := sim.RunScenario(scenario, config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", scenario.Name, err)
			failed = true
			continue
		}

		status := "PASS"
		if !result.Passed {
			status = "FAIL"
			failed = true
		}
		fmt.Printf("%s %s\n", status, result.Scenario)
		for _, action := range result.Actions {
			fmt.Printf("  T+%-3ds %-28s %s\n", int64(action.At/time.Second), action.Kind, action.Reason)
		}
		for _, event := range result.PVEEvents {
			target := ""
			if event.GuestID != 0 {
				target = fmt.Sprintf(" guest=%d/%s", event.GuestID, event.Guest)
			}
			method := ""
			if event.Method != "" {
				method = " method=" + event.Method
			}
			fmt.Printf(
				"  T+%-3ds PVE %-26s %-8s%s%s command=%q\n",
				int64(event.At/time.Second),
				event.Kind,
				event.Outcome,
				target,
				method,
				event.Command,
			)
		}
		if result.Failure != "" {
			fmt.Printf("  %s\n", result.Failure)
		}
	}

	if failed {
		os.Exit(1)
	}
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
