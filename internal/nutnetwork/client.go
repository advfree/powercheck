package nutnetwork

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var safeUPSName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type Status struct {
	Address                  string    `json:"address"`
	UPSName                  string    `json:"ups_name"`
	Description              string    `json:"description,omitempty"`
	Manufacturer             string    `json:"manufacturer,omitempty"`
	Model                    string    `json:"model,omitempty"`
	UPSStatus                string    `json:"ups_status"`
	UPSLoadPercent           *float64  `json:"ups_load_percent,omitempty"`
	UPSRealPowerWatts        *float64  `json:"ups_realpower_watts,omitempty"`
	UPSRealPowerNominalWatts *float64  `json:"ups_realpower_nominal_watts,omitempty"`
	UPSPowerVA               *float64  `json:"ups_power_va,omitempty"`
	UPSPowerNominalVA        *float64  `json:"ups_power_nominal_va,omitempty"`
	InputVoltage             *float64  `json:"input_voltage,omitempty"`
	BatteryCharge            *float64  `json:"battery_charge,omitempty"`
	BatteryRuntimeSeconds    *int      `json:"battery_runtime_seconds,omitempty"`
	BatteryVoltage           *float64  `json:"battery_voltage,omitempty"`
	BatteryVoltageNominal    *float64  `json:"battery_voltage_nominal,omitempty"`
	BatteryCapacityAH        *float64  `json:"battery_capacity_ah,omitempty"`
	BatteryCapacityNominalAH *float64  `json:"battery_capacity_nominal_ah,omitempty"`
	BatteryTemperature       *float64  `json:"battery_temperature,omitempty"`
	BatteryPacksBad          *int      `json:"battery_packs_bad,omitempty"`
	BatteryType              string    `json:"battery_type,omitempty"`
	BatteryStatus            string    `json:"battery_status,omitempty"`
	BatteryManufacturedDate  string    `json:"battery_manufactured_date,omitempty"`
	BatteryReplacementDate   string    `json:"battery_replacement_date,omitempty"`
	UPSManufacturedDate      string    `json:"ups_manufactured_date,omitempty"`
	SelfTestResult           string    `json:"self_test_result,omitempty"`
	CheckedAt                time.Time `json:"checked_at"`
}

type Client struct {
	Address     string
	UPSName     string
	Timeout     time.Duration
	dialContext func(context.Context, string, string) (net.Conn, error)
}

func (c Client) Read(ctx context.Context) (Status, error) {
	if err := c.validate(); err != nil {
		return Status{}, err
	}
	dial := c.dialContext
	if dial == nil {
		dialer := net.Dialer{Timeout: c.timeout()}
		dial = dialer.DialContext
	}
	connection, err := dial(ctx, "tcp", c.Address)
	if err != nil {
		return Status{}, fmt.Errorf("connect to NUT %s: %w", c.Address, err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(c.timeout())); err != nil {
		return Status{}, fmt.Errorf("set NUT deadline: %w", err)
	}

	reader := bufio.NewScanner(connection)
	writer := bufio.NewWriter(connection)
	name := c.UPSName
	description := ""
	if name == "" {
		name, description, err = discoverUPS(reader, writer)
		if err != nil {
			return Status{}, err
		}
	}
	variables, err := listVariables(reader, writer, name)
	if err != nil {
		return Status{}, err
	}
	upsStatus := variables["ups.status"]
	if upsStatus == "" {
		return Status{}, fmt.Errorf("NUT did not provide required variable ups.status")
	}

	return Status{
		Address:                  c.Address,
		UPSName:                  name,
		Description:              description,
		Manufacturer:             firstValue(variables, "device.mfr", "ups.mfr"),
		Model:                    firstValue(variables, "device.model", "ups.model"),
		UPSStatus:                upsStatus,
		UPSLoadPercent:           optionalFloat(variables["ups.load"]),
		UPSRealPowerWatts:        optionalFloat(variables["ups.realpower"]),
		UPSRealPowerNominalWatts: optionalFloat(variables["ups.realpower.nominal"]),
		UPSPowerVA:               optionalFloat(variables["ups.power"]),
		UPSPowerNominalVA:        optionalFloat(variables["ups.power.nominal"]),
		InputVoltage:             optionalFloat(variables["input.voltage"]),
		BatteryCharge:            optionalFloat(variables["battery.charge"]),
		BatteryRuntimeSeconds:    optionalInt(variables["battery.runtime"]),
		BatteryVoltage:           optionalFloat(variables["battery.voltage"]),
		BatteryVoltageNominal:    optionalFloat(variables["battery.voltage.nominal"]),
		BatteryCapacityAH:        optionalFloat(variables["battery.capacity"]),
		BatteryCapacityNominalAH: optionalFloat(variables["battery.capacity.nominal"]),
		BatteryTemperature:       optionalFloat(variables["battery.temperature"]),
		BatteryPacksBad:          optionalInt(variables["battery.packs.bad"]),
		BatteryType:              variables["battery.type"],
		BatteryStatus:            variables["battery.status"],
		BatteryManufacturedDate:  variables["battery.mfr.date"],
		BatteryReplacementDate:   variables["battery.date"],
		UPSManufacturedDate:      variables["ups.mfr.date"],
		SelfTestResult:           variables["ups.test.result"],
		CheckedAt:                time.Now().UTC(),
	}, nil
}

func (c Client) validate() error {
	host, port, err := net.SplitHostPort(c.Address)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("NUT address must use host:port")
	}
	if c.UPSName != "" && !safeUPSName.MatchString(c.UPSName) {
		return fmt.Errorf("invalid NUT UPS name")
	}
	return nil
}

func (c Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 5 * time.Second
}

func discoverUPS(reader *bufio.Scanner, writer *bufio.Writer) (string, string, error) {
	if err := writeCommand(writer, "LIST UPS"); err != nil {
		return "", "", err
	}
	if !reader.Scan() {
		return "", "", scanError(reader, "read NUT UPS list")
	}
	if line := reader.Text(); line != "BEGIN LIST UPS" {
		return "", "", fmt.Errorf("NUT LIST UPS rejected: %s", line)
	}
	var name, description string
	for reader.Scan() {
		line := reader.Text()
		if line == "END LIST UPS" {
			switch {
			case name == "":
				return "", "", fmt.Errorf("NUT did not advertise an UPS")
			default:
				return name, description, nil
			}
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "UPS" || !safeUPSName.MatchString(fields[1]) {
			return "", "", fmt.Errorf("invalid NUT UPS list response")
		}
		if name != "" {
			return "", "", fmt.Errorf("NUT advertised multiple UPS devices; configure one explicitly")
		}
		parsedDescription, err := strconv.Unquote(strings.Join(fields[2:], " "))
		if err != nil {
			return "", "", fmt.Errorf("decode NUT UPS description: %w", err)
		}
		name = fields[1]
		description = parsedDescription
	}
	return "", "", scanError(reader, "read NUT UPS list")
}

func listVariables(reader *bufio.Scanner, writer *bufio.Writer, upsName string) (map[string]string, error) {
	if err := writeCommand(writer, "LIST VAR "+upsName); err != nil {
		return nil, err
	}
	if !reader.Scan() {
		return nil, scanError(reader, "read NUT variable list")
	}
	if line := reader.Text(); line != "BEGIN LIST VAR "+upsName {
		return nil, fmt.Errorf("NUT LIST VAR rejected: %s", line)
	}
	variables := make(map[string]string)
	for reader.Scan() {
		line := reader.Text()
		if line == "END LIST VAR "+upsName {
			return variables, nil
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "VAR" || fields[1] != upsName {
			return nil, fmt.Errorf("invalid NUT variable list response")
		}
		if len(variables) >= 512 {
			return nil, fmt.Errorf("NUT variable list exceeds 512 entries")
		}
		value, err := strconv.Unquote(strings.Join(fields[3:], " "))
		if err != nil {
			return nil, fmt.Errorf("decode NUT variable %s: %w", fields[2], err)
		}
		variables[fields[2]] = value
	}
	return nil, scanError(reader, "read NUT variable list")
}

func getVariable(
	reader *bufio.Scanner,
	writer *bufio.Writer,
	upsName string,
	variable string,
	required bool,
) (string, error) {
	if err := writeCommand(writer, "GET VAR "+upsName+" "+variable); err != nil {
		return "", err
	}
	if !reader.Scan() {
		return "", scanError(reader, "read NUT variable "+variable)
	}
	line := reader.Text()
	if strings.HasPrefix(line, "ERR ") {
		if !required && (line == "ERR VAR-NOT-SUPPORTED" || line == "ERR UNKNOWN-VAR") {
			return "", nil
		}
		return "", fmt.Errorf("NUT rejected %s: %s", variable, line)
	}
	fields := strings.Fields(line)
	if len(fields) < 4 ||
		fields[0] != "VAR" ||
		fields[1] != upsName ||
		fields[2] != variable {
		return "", fmt.Errorf("invalid NUT response for %s", variable)
	}
	value, err := strconv.Unquote(strings.Join(fields[3:], " "))
	if err != nil {
		return "", fmt.Errorf("decode NUT variable %s: %w", variable, err)
	}
	return value, nil
}

func writeCommand(writer *bufio.Writer, command string) error {
	if _, err := writer.WriteString(command + "\n"); err != nil {
		return fmt.Errorf("write NUT command: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush NUT command: %w", err)
	}
	return nil
}

func scanError(reader *bufio.Scanner, action string) error {
	if err := reader.Err(); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: connection closed", action)
}

func optionalInt(value string) *int {
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func optionalFloat(value string) *float64 {
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func firstValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := values[key]; value != "" {
			return value
		}
	}
	return ""
}
