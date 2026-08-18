package nutreader

import (
	"context"
	"errors"
	"testing"

	"powercheck/internal/core"
	"powercheck/internal/readonlyexec"
)

type runnerFunc func(context.Context, string, ...string) (readonlyexec.Output, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (readonlyexec.Output, error) {
	return f(ctx, name, args...)
}

func TestReadParsesNUTVariables(t *testing.T) {
	client := Client{
		Target: "ups@nas",
		Runner: runnerFunc(func(_ context.Context, name string, args ...string) (readonlyexec.Output, error) {
			if name != "upsc" || len(args) != 1 || args[0] != "ups@nas" {
				t.Fatalf("unexpected command: %s %v", name, args)
			}
			return readonlyexec.Output{Stdout: []byte(
				"battery.charge: 87\nbattery.runtime: 1234\nups.status: OL\n",
			)}, nil
		}),
	}

	reading, err := client.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reading.Status != core.NUTOnline || reading.Variables["battery.charge"] != "87" {
		t.Fatalf("unexpected reading: %#v", reading)
	}
}

func TestNUTStatusPriority(t *testing.T) {
	tests := []struct {
		raw  string
		want core.NUTStatus
	}{
		{"OL", core.NUTOnline},
		{"OB", core.NUTOnBattery},
		{"OB LB", core.NUTLowBattery},
	}
	for _, test := range tests {
		got, err := parseStatus(test.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("status %q parsed as %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestCommandFailureBecomesUnreachable(t *testing.T) {
	client := Client{
		Target: "ups@nas",
		Runner: runnerFunc(func(context.Context, string, ...string) (readonlyexec.Output, error) {
			return readonlyexec.Output{}, errors.New("connection refused")
		}),
	}
	reading, err := client.Read(context.Background())
	if err == nil {
		t.Fatal("expected upsc failure")
	}
	if reading.Status != core.NUTUnreachable {
		t.Fatalf("failure status is %q", reading.Status)
	}
}
