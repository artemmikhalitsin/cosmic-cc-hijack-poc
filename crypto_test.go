package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestCustomEncryptionRoundTrip(t *testing.T) {
	for _, length := range []int{0, 1, 15, 16, 255, 256, 1456, 3000} {
		original := make([]byte, length)
		for i := range original {
			original[i] = byte(i*37 + 11)
		}
		transformed := append([]byte(nil), original...)
		mapleEncryptData(transformed)
		mapleDecryptData(transformed)
		if !bytes.Equal(transformed, original) {
			t.Fatalf("round trip failed for length %d", length)
		}
	}
}

func TestMaplePacketRoundTrip(t *testing.T) {
	iv := []byte{0x46, 0x72, 0x7A, 0x11}
	sender, err := newMapleCipher(iv, mapleVersion)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := newMapleCipher(iv, mapleVersion)
	if err != nil {
		t.Fatal(err)
	}
	for _, length := range []int{2, 64, 0x5B0, 0x5B4, 0x5B0 + 0x5B4 + 31} {
		original := make([]byte, length)
		for i := range original {
			original[i] = byte(i*13 + 7)
		}
		header := sender.packetHeader(length)
		headerInt := binary.BigEndian.Uint32(header[:])
		if !receiver.validHeader(headerInt) {
			t.Fatalf("receiver rejected header for length %d", length)
		}
		if maplePacketLength(headerInt) != length {
			t.Fatalf("wrong decoded length for %d", length)
		}
		payload := append([]byte(nil), original...)
		mapleEncryptData(payload)
		sender.crypt(payload)
		receiver.crypt(payload)
		mapleDecryptData(payload)
		if !bytes.Equal(payload, original) {
			t.Fatalf("packet round trip failed for length %d", length)
		}
		if sender.iv != receiver.iv {
			t.Fatalf("cipher IVs diverged for length %d", length)
		}
	}
}

func TestRollingIVChanges(t *testing.T) {
	old := [4]byte{0x46, 0x72, 0x7A, 0x11}
	next := nextMapleIV(old)
	if next == old {
		t.Fatal("IV did not change")
	}
	if nextMapleIV(old) != next {
		t.Fatal("IV update is not deterministic")
	}
}

func TestJavaCipherVector(t *testing.T) {
	cipher, err := newMapleCipher([]byte{0x46, 0x72, 0x7A, 0x20}, mapleVersion)
	if err != nil {
		t.Fatal(err)
	}
	header := cipher.packetHeader(64)
	if hex.EncodeToString(header[:]) != "29206920" {
		t.Fatalf("header = %x", header)
	}
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i)
	}
	cipher.crypt(data)
	expected := "b3cbf3c976aeede7bace7736ffd27e4077e1b5f8405aa4073ece32344d" +
		"df51941dafc96d8231cfe0d56f6a7a442f7ca8c00c19c8520b73045c8121dbd68bdecb"
	if hex.EncodeToString(data) != expected {
		t.Fatalf("ciphertext = %x", data)
	}
	if cipher.iv != [4]byte{0xF8, 0x79, 0xC6, 0xBC} {
		t.Fatalf("updated IV = %x", cipher.iv)
	}
}
