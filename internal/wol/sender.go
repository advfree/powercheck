package wol

import (
	"context"
	"fmt"
	"net"
	"time"
)

type Sender interface {
	Send(context.Context, Device) error
}

type UDPSender struct {
	Timeout time.Duration
}

func (s UDPSender) Send(ctx context.Context, device Device) error {
	hardware, err := net.ParseMAC(device.MAC)
	if err != nil || len(hardware) != 6 {
		return fmt.Errorf("parse device MAC")
	}
	targetIP := net.ParseIP(device.Broadcast)
	if targetIP == nil || targetIP.To4() == nil {
		return fmt.Errorf("parse broadcast address")
	}

	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return fmt.Errorf("open UDP socket: %w", err)
	}
	defer connection.Close()
	if err := enableBroadcast(connection); err != nil {
		return fmt.Errorf("enable UDP broadcast: %w", err)
	}

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("set UDP deadline: %w", err)
	}

	packet := MagicPacket(hardware)
	target := &net.UDPAddr{IP: targetIP.To4(), Port: device.Port}
	written, err := connection.WriteToUDP(packet, target)
	if err != nil {
		return fmt.Errorf("send magic packet: %w", err)
	}
	if written != len(packet) {
		return fmt.Errorf("short magic packet write: %d of %d bytes", written, len(packet))
	}
	return nil
}

func MagicPacket(hardware net.HardwareAddr) []byte {
	packet := make([]byte, 6+16*len(hardware))
	for index := 0; index < 6; index++ {
		packet[index] = 0xff
	}
	offset := 6
	for repeat := 0; repeat < 16; repeat++ {
		copy(packet[offset:offset+len(hardware)], hardware)
		offset += len(hardware)
	}
	return packet
}
