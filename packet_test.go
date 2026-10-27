package main

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestPacketPrimitives(t *testing.T) {
	w := &packetWriter{}
	w.u8(0x12)
	w.u16(0x3456)
	w.u32(0x789ABCDE)
	w.string("alice")
	w.raw([]byte{0xFF})
	r := newPacketReader(w.bytes)
	if value := r.u8(); value != 0x12 {
		t.Fatalf("u8 = %x", value)
	}
	if value := r.u16(); value != 0x3456 {
		t.Fatalf("u16 = %x", value)
	}
	if value := r.u32(); value != 0x789ABCDE {
		t.Fatalf("u32 = %x", value)
	}
	if value := r.string(); value != "alice" {
		t.Fatalf("string = %q", value)
	}
	if value := r.u8(); value != 0xFF {
		t.Fatalf("raw = %x", value)
	}
	if r.err != nil {
		t.Fatal(r.err)
	}
}

func TestFramedPacketRoundTrip(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	receiveIV := []byte{0x46, 0x72, 0x7A, 0x11}
	sendIV := []byte{0x52, 0x30, 0x78, 0x22}
	clientSend, _ := newMapleCipher(receiveIV, mapleVersion)
	clientReceive, _ := newMapleCipher(sendIV, 0xFFFF-mapleVersion)
	serverSend, _ := newMapleCipher(sendIV, 0xFFFF-mapleVersion)
	serverReceive, _ := newMapleCipher(receiveIV, mapleVersion)
	client := &mapleConn{conn: left, send: clientSend, receive: clientReceive}
	server := &mapleConn{conn: right, send: serverSend, receive: serverReceive}

	original := []byte{0x14, 0x00, 0xE9, 0x03, 0x00, 0x00}
	_ = server.setDeadline(time.Now().Add(time.Second))
	go func() {
		_ = client.writePacket(original)
	}()
	received, err := server.readPacket()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, original) {
		t.Fatalf("received %x, want %x", received, original)
	}
}

func TestLoopbackOnly(t *testing.T) {
	if err := requireLoopback("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := requireLoopback("localhost"); err != nil {
		t.Fatal(err)
	}
	if err := requireLoopback("192.0.2.1"); err == nil {
		t.Fatal("expected non-loopback address to be rejected")
	}
}
