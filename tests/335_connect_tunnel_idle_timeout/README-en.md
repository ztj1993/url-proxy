# Feature 335: CONNECT Tunnel Idle Timeout & Half-Close Recovery Test Suite

This directory provides the standalone test environment and automated regression test suite for Feature 335 (CONNECT Tunnel Idle Timeout & Half-Close Recovery).

---

## 1. Directory Layout

- `proxy_test.conf`: Test configuration declaring service address and forwarding rules.
- `mock_server.go`: Mock TCP target server supporting bidirectional echo, delayed responses, and TCP half-close interaction.
- `connect_tunnel_test.go`: Automated test suite covering silent zombie tunnel idle timeout reclamation, active transfer deadline renewal, and unidirectional half-close data integrity.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation.

---

## 2. Automated Test Execution

Run from repository root:
```bash
go test -v -count=1 ./tests/335_connect_tunnel_idle_timeout/...
```

---

## 3. Manual Verification Steps

### Step 1: Start Mock Target Server
```bash
go run tests/335_connect_tunnel_idle_timeout/mock_server.go
```

### Step 2: Start url-proxy Instance
```bash
go run url-proxy.go -conf tests/335_connect_tunnel_idle_timeout/proxy_test.conf -addr :8888
```

### Step 3: Verification Scenarios
1. **CONNECT Tunnel Handshake**:
   Test HTTPS tunneling using curl:
   ```bash
   curl -v -p -x http://127.0.0.1:8888 https://httpbin.org/get
   ```
2. **Idle Timeout Reclamation**:
   Establish a tunnel and remain idle; verify the proxy reclaims sockets and routines upon deadline expiry.