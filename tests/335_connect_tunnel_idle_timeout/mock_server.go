package main

import (
	"net"
	"time"
)

type MockTunnelTarget struct {
	Listener net.Listener
}

func StartMockTunnelTarget() (*MockTunnelTarget, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	target := &MockTunnelTarget{Listener: ln}
	go target.serve()
	return target, nil
}

func (t *MockTunnelTarget) serve() {
	for {
		conn, err := t.Listener.Accept()
		if err != nil {
			return
		}
		go t.handleConn(conn)
	}
}

func (t *MockTunnelTarget) handleConn(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			data := string(buf[:n])
			if data == "HALF-CLOSE-TEST" {
				time.Sleep(50 * time.Millisecond)
				_, _ = conn.Write([]byte("HALF-CLOSE-RESPONSE"))
				return
			}
			_, _ = conn.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (t *MockTunnelTarget) Close() error {
	return t.Listener.Close()
}