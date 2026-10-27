package main

import (
	"testing"
)

func TestParseAuthResult(t *testing.T) {
	w := packetWithOpcode(sendLoginStatus)
	w.i32(0)
	w.u16(0)
	w.i32(42)
	w.u8(0)
	w.u8(0)
	w.u8(0)
	w.u8(0)
	w.string("testuser")
	w.u8(0)
	w.u8(0)
	w.raw(make([]byte, 16))
	w.u32(1)
	w.u8(0)
	w.u8(1)

	auth, err := parseAuthResult(w.bytes)
	if err != nil {
		t.Fatal(err)
	}
	if auth.accountID != 42 || auth.pinMode != 0 || auth.picMode != 1 {
		t.Fatalf("unexpected auth result: %+v", auth)
	}
}

func TestParseLoginFailure(t *testing.T) {
	w := packetWithOpcode(sendLoginStatus)
	w.u8(4)
	w.u8(0)
	w.u32(0)
	if _, err := parseAuthResult(w.bytes); err == nil {
		t.Fatal("expected login failure")
	}
}

func TestParseServerIP(t *testing.T) {
	w := packetWithOpcode(sendServerIP)
	w.u16(0)
	w.raw([]byte{127, 0, 0, 1})
	w.u16(7575)
	w.i32(1001)
	w.raw(make([]byte, 5))
	port, id, err := parseServerIP(w.bytes)
	if err != nil || port != 7575 || id != 1001 {
		t.Fatalf("port=%d id=%d err=%v", port, id, err)
	}
}
