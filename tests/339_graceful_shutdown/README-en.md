# Feature 339: Graceful Shutdown & Process Lifecycle Governance Test Suite

This directory provides the standalone local test environment and automated test suite for Feature 339 (Graceful Shutdown & Process Lifecycle Governance Plan).

---

## 1. Directory Structure

- `proxy_test.conf`: Configuration file declaring listener and TTL rules for Feature 339 tests.
- `mock_server.go`: Mock upstream server providing slow chunked streaming, fast file delivery, and request metrics.
- `graceful_shutdown_test.go`: Core automated test suite covering in-flight HTTP request draining, active job cancellation with temp file cleanup, CONNECT hijacked tunnel termination, and lifecycle idempotence.
- `README-zh.md`: Chinese test documentation.
- `README-en.md`: English test documentation.

---

## 2. Automated Test Execution

Execute from the workspace root:
```bash
go test -v -count=1 ./tests/339_graceful_shutdown/...
```

---

## 3. Test Coverage Scenarios

1. **In-Flight HTTP Request Draining (`TestGracefulShutdownHTTPDrain`)**:
   - Trigger `server.Shutdown` while a slow streaming response is actively being transferred;
   - Verify that new incoming requests are rejected immediately while active in-flight requests complete cleanly.
2. **Active Job Cancellation & Temp File Cleanup (`TestGracefulShutdownJobCancellationAndTempFileCleanup`)**:
   - Call `downMgr.Shutdown()` while a coalesced upstream download is in-flight;
   - Verify that the job context receives cancellation, `job.Err` records `context.Canceled`;
   - Verify that the incomplete `.tmp` file is immediately deleted and never promoted to a cache file.
3. **CONNECT Tunnel Teardown & Transport Pool Release (`TestGracefulShutdownConnectTunnelCleanup`)**:
   - Establish a bidirectional CONNECT tunnel and call `proxyServer.Close()`;
   - Verify that hijacked sockets are forcibly closed and relay routines exit cleanly;
   - Verify that idle TCP connections in the outbound transport pool are closed.
4. **Lifecycle Method Idempotence (`TestGracefulShutdownManagerIdempotence`)**:
   - Call `downMgr.StopWatchdog()` and `downMgr.Shutdown()` multiple times;
   - Verify that synchronization guards prevent channel closing panics and deadlocks.
