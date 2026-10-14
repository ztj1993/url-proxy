package proxy

import (
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type closeWriter interface {
	CloseWrite() error
}

type idleTimeoutConn struct {
	net.Conn
	idleTimeout time.Duration
}

func (c *idleTimeoutConn) Read(b []byte) (int, error) {
	if c.idleTimeout > 0 {
		_ = c.Conn.SetReadDeadline(time.Now().Add(c.idleTimeout))
	}
	return c.Conn.Read(b)
}

func (c *idleTimeoutConn) Write(b []byte) (int, error) {
	if c.idleTimeout > 0 {
		_ = c.Conn.SetWriteDeadline(time.Now().Add(c.idleTimeout))
	}
	return c.Conn.Write(b)
}

func (c *idleTimeoutConn) CloseWrite() error {
	if cw, ok := c.Conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return nil
}

// handleConnect 处理 HTTPS CONNECT 隧道代理 (Feature 335)
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request, reqID uint64) {
	log.Printf("[%d] connect tunnel: %s", reqID, r.Host)
	destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		log.Printf("[%d] connect dial err: %v", reqID, err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer destConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Printf("[%d] connect hijack not supported", reqID)
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("[%d] connect hijack err: %v", reqID, err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer clientConn.Close()

	s.trackTunnel(clientConn, destConn)
	defer s.untrackTunnel(clientConn, destConn)

	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		log.Printf("[%d] connect write 200 err: %v", reqID, err)
		return
	}

	timeout := s.TunnelTimeout()

	wrappedClient := &idleTimeoutConn{Conn: clientConn, idleTimeout: timeout}
	wrappedDest := &idleTimeoutConn{Conn: destConn, idleTimeout: timeout}

	var wg sync.WaitGroup
	wg.Add(2)

	relay := func(dst net.Conn, src net.Conn, dir string) {
		defer wg.Done()
		_, copyErr := io.Copy(dst, src)
		if copyErr == nil {
			// 正常传输结束，尝试执行半关闭优雅传递 TCP FIN
			if cw, ok := dst.(closeWriter); ok {
				_ = cw.CloseWrite()
				return
			}
		}
		// 若发生超时或网络错误，关闭双端以快速唤醒对端阻塞读取
		_ = dst.Close()
		_ = src.Close()
	}

	go relay(wrappedDest, wrappedClient, "client->dest")
	go relay(wrappedClient, wrappedDest, "dest->client")

	wg.Wait()
	log.Printf("[%d] connect tunnel closed: %s", reqID, r.Host)
}