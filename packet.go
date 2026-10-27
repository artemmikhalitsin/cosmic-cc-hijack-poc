package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	mapleVersion       = 83
	maxMaplePacketSize = 0xFFFF

	recvLoginPassword   = 0x0001
	recvCharlistRequest = 0x0005
	recvAfterLogin      = 0x0009
	recvCharSelect      = 0x0013
	recvPlayerLoggedIn  = 0x0014
	recvPong            = 0x0018
	recvCharSelectPic   = 0x001E

	sendLoginStatus   = 0x0000
	sendCheckPincode  = 0x0006
	sendServerIP      = 0x000C
	sendChangeChannel = 0x0010
	sendPing          = 0x0011
	sendSetField      = 0x007D
	sendSpawnPlayer   = 0x00A0
	sendRemovePlayer  = 0x00A1
)

type packetWriter struct {
	bytes []byte
}

func (w *packetWriter) u8(value byte) {
	w.bytes = append(w.bytes, value)
}

func (w *packetWriter) u16(value uint16) {
	w.bytes = binary.LittleEndian.AppendUint16(w.bytes, value)
}

func (w *packetWriter) u32(value uint32) {
	w.bytes = binary.LittleEndian.AppendUint32(w.bytes, value)
}

func (w *packetWriter) i32(value int32) {
	w.u32(uint32(value))
}

func (w *packetWriter) u64(value uint64) {
	w.bytes = binary.LittleEndian.AppendUint64(w.bytes, value)
}

func (w *packetWriter) raw(value []byte) {
	w.bytes = append(w.bytes, value...)
}

func (w *packetWriter) string(value string) {
	w.u16(uint16(len(value)))
	w.raw([]byte(value))
}

func packetWithOpcode(opcode uint16) *packetWriter {
	w := &packetWriter{}
	w.u16(opcode)
	return w
}

type packetReader struct {
	data []byte
	off  int
	err  error
}

func newPacketReader(data []byte) *packetReader {
	return &packetReader{data: data}
}

func (r *packetReader) take(length int) []byte {
	if r.err != nil {
		return nil
	}
	if length < 0 || length > len(r.data)-r.off {
		r.err = io.ErrUnexpectedEOF
		return nil
	}
	value := r.data[r.off : r.off+length]
	r.off += length
	return value
}

func (r *packetReader) skip(length int) {
	r.take(length)
}

func (r *packetReader) u8() byte {
	value := r.take(1)
	if value == nil {
		return 0
	}
	return value[0]
}

func (r *packetReader) u16() uint16 {
	value := r.take(2)
	if value == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(value)
}

func (r *packetReader) u32() uint32 {
	value := r.take(4)
	if value == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(value)
}

func (r *packetReader) i32() int32 {
	return int32(r.u32())
}

func (r *packetReader) u64() uint64 {
	value := r.take(8)
	if value == nil {
		return 0
	}
	return binary.LittleEndian.Uint64(value)
}

func (r *packetReader) string() string {
	length := int(r.u16())
	return string(r.take(length))
}

func (r *packetReader) fixedString(length int) string {
	return strings.TrimRight(string(r.take(length)), "\x00")
}

func packetOpcode(body []byte) (uint16, error) {
	if len(body) < 2 {
		return 0, io.ErrUnexpectedEOF
	}
	return binary.LittleEndian.Uint16(body), nil
}

type mapleConn struct {
	conn    net.Conn
	send    *mapleCipher
	receive *mapleCipher
	readMu  sync.Mutex
	writeMu sync.Mutex
}

func dialMaple(host string, port int, timeout time.Duration) (*mapleConn, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)), timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	hello := make([]byte, 16)
	if _, err := io.ReadFull(conn, hello); err != nil {
		conn.Close()
		return nil, fmt.Errorf("read hello: %w", err)
	}
	r := newPacketReader(hello)
	if r.u16() != 0x000E || r.u16() != mapleVersion {
		conn.Close()
		return nil, errors.New("unexpected MapleStory hello")
	}
	r.u16()
	r.u8()
	receiveIV := append([]byte(nil), r.take(4)...)
	sendIV := append([]byte(nil), r.take(4)...)
	r.u8()
	if r.err != nil {
		conn.Close()
		return nil, r.err
	}
	sendCipher, err := newMapleCipher(receiveIV, mapleVersion)
	if err != nil {
		conn.Close()
		return nil, err
	}
	receiveCipher, err := newMapleCipher(sendIV, 0xFFFF-mapleVersion)
	if err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return &mapleConn{conn: conn, send: sendCipher, receive: receiveCipher}, nil
}

func (c *mapleConn) Close() error {
	return c.conn.Close()
}

func (c *mapleConn) setDeadline(deadline time.Time) error {
	return c.conn.SetDeadline(deadline)
}

func (c *mapleConn) readPacket() ([]byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	var headerBytes [4]byte
	if _, err := io.ReadFull(c.conn, headerBytes[:]); err != nil {
		return nil, err
	}
	header := binary.BigEndian.Uint32(headerBytes[:])
	if !c.receive.validHeader(header) {
		return nil, fmt.Errorf("invalid packet header %08x", header)
	}
	length := maplePacketLength(header)
	if length < 0 || length > maxMaplePacketSize {
		return nil, fmt.Errorf("invalid packet length %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.conn, body); err != nil {
		return nil, err
	}
	c.receive.crypt(body)
	mapleDecryptData(body)
	return body, nil
}

func (c *mapleConn) writePacket(body []byte) error {
	if len(body) > maxMaplePacketSize {
		return fmt.Errorf("packet body too large: %d", len(body))
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	payload := append([]byte(nil), body...)
	header := c.send.packetHeader(len(payload))
	mapleEncryptData(payload)
	c.send.crypt(payload)
	if err := writeAll(c.conn, header[:]); err != nil {
		return err
	}
	return writeAll(c.conn, payload)
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		data = data[written:]
	}
	return nil
}

func (c *mapleConn) writePong() error {
	return c.writePacket(packetWithOpcode(recvPong).bytes)
}
