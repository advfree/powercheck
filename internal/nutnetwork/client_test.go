package nutnetwork

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestClientDiscoversAndReadsUPS(t *testing.T) {
	clientConnection, serverConnection := net.Pipe()
	defer clientConnection.Close()
	go serveNUT(t, serverConnection)

	client := Client{
		Address: "192.0.2.10:3493",
		Timeout: time.Second,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return clientConnection, nil
		},
	}
	status, err := client.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.UPSName != "ups" ||
		status.Description != "Synology UPS" ||
		status.UPSStatus != "OL" ||
		status.UPSLoadPercent == nil ||
		*status.UPSLoadPercent != 38.5 ||
		status.UPSRealPowerNominalWatts == nil ||
		*status.UPSRealPowerNominalWatts != 390 ||
		status.BatteryCharge == nil ||
		*status.BatteryCharge != 100 ||
		status.BatteryRuntimeSeconds == nil ||
		*status.BatteryRuntimeSeconds != 1800 {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestClientRejectsAccessDenied(t *testing.T) {
	clientConnection, serverConnection := net.Pipe()
	defer clientConnection.Close()
	go func() {
		defer serverConnection.Close()
		reader := bufio.NewReader(serverConnection)
		_, _ = reader.ReadString('\n')
		_, _ = serverConnection.Write([]byte("ERR ACCESS-DENIED\n"))
	}()
	client := Client{
		Address: "192.0.2.10:3493",
		Timeout: time.Second,
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return clientConnection, nil
		},
	}
	if _, err := client.Read(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "ACCESS-DENIED") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func serveNUT(t *testing.T, connection net.Conn) {
	t.Helper()
	defer connection.Close()
	reader := bufio.NewScanner(connection)
	for reader.Scan() {
		switch reader.Text() {
		case "LIST UPS":
			_, _ = connection.Write([]byte("BEGIN LIST UPS\nUPS ups \"Synology UPS\"\nEND LIST UPS\n"))
		case "LIST VAR ups":
			_, _ = connection.Write([]byte(
				"BEGIN LIST VAR ups\n" +
					"VAR ups device.mfr \"APC\"\n" +
					"VAR ups device.model \"Back-UPS\"\n" +
					"VAR ups ups.status \"OL\"\n" +
					"VAR ups ups.load \"38.5\"\n" +
					"VAR ups ups.realpower.nominal \"390\"\n" +
					"VAR ups battery.charge \"100\"\n" +
					"VAR ups battery.runtime \"1800\"\n" +
					"VAR ups battery.voltage \"13.5\"\n" +
					"VAR ups battery.voltage.nominal \"12\"\n" +
					"END LIST VAR ups\n",
			))
		default:
			t.Errorf("unexpected NUT command: %s", reader.Text())
			return
		}
	}
}
