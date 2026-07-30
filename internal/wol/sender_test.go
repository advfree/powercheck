package wol

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func TestMagicPacket(t *testing.T) {
	t.Parallel()
	hardware, err := net.ParseMAC("7C:C3:85:BE:65:CC")
	if err != nil {
		t.Fatal(err)
	}
	packet := MagicPacket(hardware)
	if len(packet) != 102 {
		t.Fatalf("packet length=%d, want 102", len(packet))
	}
	if !bytes.Equal(packet[:6], bytes.Repeat([]byte{0xff}, 6)) {
		t.Fatal("packet does not start with six FF bytes")
	}
	for offset := 6; offset < len(packet); offset += len(hardware) {
		if !bytes.Equal(packet[offset:offset+len(hardware)], hardware) {
			t.Fatalf("MAC repetition is invalid at offset %d", offset)
		}
	}
}

func TestUDPSenderWritesMagicPacket(t *testing.T) {
	t.Parallel()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.LocalAddr().(*net.UDPAddr).Port

	err = (UDPSender{Timeout: time.Second}).Send(context.Background(), Device{
		MAC:       "7C:C3:85:BE:65:CC",
		Broadcast: "127.0.0.1",
		Port:      port,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := listener.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 128)
	read, _, err := listener.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if read != 102 {
		t.Fatalf("received %d bytes, want 102", read)
	}
}
