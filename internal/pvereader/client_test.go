package pvereader

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"powercheck/internal/readonlyexec"
)

type runnerFunc func(context.Context, string, ...string) (readonlyexec.Output, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (readonlyexec.Output, error) {
	return f(ctx, name, args...)
}

func TestListGuestsParsesAndSortsPVEResources(t *testing.T) {
	client := Client{Node: "pve", Runner: runnerFunc(func(_ context.Context, name string, args ...string) (readonlyexec.Output, error) {
		if name != "pvesh" {
			t.Fatalf("unexpected command %q", name)
		}
		return readonlyexec.Output{Stdout: []byte(`[
			{"vmid":300,"name":"template","type":"qemu","status":"stopped","node":"pve","template":1},
			{"vmid":200,"name":"dns","type":"lxc","status":"stopped","node":"pve","tags":"infra;dns"},
			{"vmid":100,"name":"windows","type":"qemu","status":"running","node":"pve","template":0},
			{"vmid":400,"name":"other-node","type":"qemu","status":"running","node":"pve2"},
			{"type":"node","status":"online","node":"pve"}
		]`)}, nil
	})}

	guests, err := client.ListGuests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := []int{guests[0].VMID, guests[1].VMID, guests[2].VMID}
	if want := []int{100, 200, 300}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("guest IDs are %v, want %v", gotIDs, want)
	}
	if guests[1].Type != GuestLXC || !reflect.DeepEqual(guests[1].Tags, []string{"infra", "dns"}) {
		t.Fatalf("unexpected LXC resource: %#v", guests[1])
	}
	if !guests[2].Template {
		t.Fatalf("template flag was not parsed: %#v", guests[2])
	}
	if AllGuestsStopped(guests) {
		t.Fatal("running VM was incorrectly considered stopped")
	}
	guests[0].Status = "stopped"
	if !AllGuestsStopped(guests) {
		t.Fatal("stopped guests plus a template should be considered stopped")
	}
}

func TestAgentPingResults(t *testing.T) {
	success := Client{Runner: runnerFunc(func(_ context.Context, name string, args ...string) (readonlyexec.Output, error) {
		if name != "qm" || !reflect.DeepEqual(args, []string{"agent", "100", "ping"}) {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return readonlyexec.Output{}, nil
	})}
	if result, err := success.TestAgent(context.Background(), 100); err != nil || result != AgentSuccess {
		t.Fatalf("success result=%q err=%v", result, err)
	}

	failure := Client{Runner: runnerFunc(func(_ context.Context, _ string, _ ...string) (readonlyexec.Output, error) {
		return readonlyexec.Output{Stderr: []byte("guest agent is not running")}, errors.New("exit status 255")
	})}
	if result, err := failure.TestAgent(context.Background(), 100); err == nil || result != AgentFailure {
		t.Fatalf("failure result=%q err=%v", result, err)
	}
}
