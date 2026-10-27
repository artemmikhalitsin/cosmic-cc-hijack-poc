package main

type packetEvent struct {
	body []byte
	err  error
}

func streamPackets(conn *mapleConn, events chan<- packetEvent) {
	for {
		body, err := conn.readPacket()
		events <- packetEvent{body: body, err: err}
		if err != nil {
			return
		}
	}
}

func handlePing(conn *mapleConn, body []byte) (bool, error) {
	opcode, err := packetOpcode(body)
	if err != nil {
		return false, err
	}
	if opcode != sendPing {
		return false, nil
	}
	return true, conn.writePong()
}
