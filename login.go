package main

import (
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	loginPort = 8484
	pocMAC    = "02-00-00-00-00-01"
	pocHost   = "020000000001_01020304"
)

type loginOptions struct {
	host      string
	account   string
	password  string
	character string
	world     int
	channel   int
	timeout   time.Duration
}

type loginResult struct {
	conn      *mapleConn
	host      string
	port      int
	character characterStats
}

type authResult struct {
	accountID int32
	pinMode   byte
	picMode   byte
}

func loginObserver(options loginOptions) (*loginResult, error) {
	login, err := dialMaple(options.host, loginPort, options.timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to login server: %w", err)
	}
	defer login.Close()

	packet := packetWithOpcode(recvLoginPassword)
	packet.string(options.account)
	packet.string(options.password)
	packet.raw(make([]byte, 6))
	packet.raw([]byte{1, 2, 3, 4})
	if err := login.writePacket(packet.bytes); err != nil {
		return nil, err
	}
	body, err := readExpectedPacket(login, sendLoginStatus, options.timeout)
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}
	auth, err := parseAuthResult(body)
	if err != nil {
		return nil, err
	}
	if auth.pinMode == 0 {
		if err := completePIN(login, os.Getenv("POC_PIN"), options.timeout); err != nil {
			return nil, err
		}
	}

	packet = packetWithOpcode(recvCharlistRequest)
	packet.u8(0)
	packet.u8(byte(options.world))
	packet.u8(byte(options.channel - 1))
	if err := login.writePacket(packet.bytes); err != nil {
		return nil, err
	}
	body, err = readExpectedPacket(login, 0x000B, options.timeout)
	if err != nil {
		return nil, fmt.Errorf("request character list: %w", err)
	}
	entries, err := parseCharacterList(body)
	if err != nil {
		return nil, err
	}
	selected, err := findCharacter(entries, options.character)
	if err != nil {
		return nil, err
	}

	switch auth.picMode {
	case 1:
		pic := os.Getenv("POC_PIC")
		if pic == "" {
			return nil, errors.New("account requires a PIC; set POC_PIC")
		}
		packet = packetWithOpcode(recvCharSelectPic)
		packet.string(pic)
		packet.i32(selected.ID)
		packet.string(pocMAC)
		packet.string(pocHost)
	case 2:
		packet = packetWithOpcode(recvCharSelect)
		packet.i32(selected.ID)
		packet.string(pocMAC)
		packet.string(pocHost)
	default:
		return nil, errors.New("PIC registration is not supported; register a PIC with the development client first")
	}
	if err := login.writePacket(packet.bytes); err != nil {
		return nil, err
	}
	body, err = readExpectedPacket(login, sendServerIP, options.timeout)
	if err != nil {
		return nil, fmt.Errorf("select observer character: %w", err)
	}
	port, characterID, err := parseServerIP(body)
	if err != nil {
		return nil, err
	}

	channel, err := dialMaple(options.host, port, options.timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to observer channel: %w", err)
	}
	packet = packetWithOpcode(recvPlayerLoggedIn)
	packet.i32(characterID)
	if err := channel.writePacket(packet.bytes); err != nil {
		channel.Close()
		return nil, err
	}
	body, err = readExpectedPacket(channel, sendSetField, options.timeout)
	if err != nil {
		channel.Close()
		return nil, fmt.Errorf("enter world as observer: %w", err)
	}
	stats, err := parseSetField(body)
	if err != nil {
		channel.Close()
		return nil, err
	}
	if stats.ID != selected.ID {
		channel.Close()
		return nil, fmt.Errorf("observer entered as unexpected character %d", stats.ID)
	}
	return &loginResult{conn: channel, host: options.host, port: port, character: stats}, nil
}

func parseAuthResult(body []byte) (authResult, error) {
	r := newPacketReader(body)
	if r.u16() != sendLoginStatus {
		return authResult{}, fmt.Errorf("not a login-status packet")
	}
	result := r.i32()
	if result != 0 {
		return authResult{}, fmt.Errorf("login failed with reason %d", result)
	}
	r.u16()
	auth := authResult{accountID: r.i32()}
	r.skip(4)
	r.string()
	r.skip(1)
	r.skip(17)
	r.skip(4)
	auth.pinMode = r.u8()
	auth.picMode = r.u8()
	if r.err != nil {
		return authResult{}, fmt.Errorf("parse login status: %w", r.err)
	}
	return auth, nil
}

func completePIN(conn *mapleConn, pin string, timeout time.Duration) error {
	if pin == "" {
		return errors.New("account requires a PIN; set POC_PIN")
	}
	packet := packetWithOpcode(recvAfterLogin)
	packet.u8(1)
	packet.u8(1)
	if err := conn.writePacket(packet.bytes); err != nil {
		return err
	}
	body, err := readExpectedPacket(conn, sendCheckPincode, timeout)
	if err != nil {
		return err
	}
	r := newPacketReader(body)
	r.u16()
	if r.u8() != 4 {
		return errors.New("PIN registration is not supported; register a PIN with the development client first")
	}
	packet = packetWithOpcode(recvAfterLogin)
	packet.u8(1)
	packet.u8(0)
	packet.string(pin)
	if err := conn.writePacket(packet.bytes); err != nil {
		return err
	}
	body, err = readExpectedPacket(conn, sendCheckPincode, timeout)
	if err != nil {
		return err
	}
	r = newPacketReader(body)
	r.u16()
	if mode := r.u8(); mode != 0 {
		return fmt.Errorf("PIN rejected with mode %d", mode)
	}
	return nil
}

func parseServerIP(body []byte) (int, int32, error) {
	r := newPacketReader(body)
	if r.u16() != sendServerIP {
		return 0, 0, fmt.Errorf("not a server-IP packet")
	}
	r.skip(6)
	port := int(r.u16())
	characterID := r.i32()
	if r.err != nil {
		return 0, 0, r.err
	}
	return port, characterID, nil
}

func readExpectedPacket(conn *mapleConn, opcode uint16, timeout time.Duration) ([]byte, error) {
	_ = conn.setDeadline(time.Now().Add(timeout))
	defer conn.setDeadline(time.Time{})
	for {
		body, err := conn.readPacket()
		if err != nil {
			return nil, err
		}
		received, err := packetOpcode(body)
		if err != nil {
			return nil, err
		}
		switch received {
		case sendPing:
			if err := conn.writePong(); err != nil {
				return nil, err
			}
		case opcode:
			return body, nil
		case 0x0009:
			r := newPacketReader(body)
			r.u16()
			return nil, fmt.Errorf("login transition failed with reason %d", r.u16())
		}
	}
}
