# Idle Timeout Cleanup Testing Guide

This document explains how to test the idle timeout cleanup mechanism in `url-proxy`.

## 1. Testing Principle

To verify that `url-proxy` can correctly clean up hung connections, we need to simulate a "faulty" upstream server:
- Upon receiving a request, the server returns the HTTP Headers and a small initial chunk of data.
- Afterwards, the server goes into an indefinite sleep, **sending no more data and never closing the TCP connection**.

If `url-proxy`'s cleanup mechanism works correctly, it should actively sever the connection to the server, clean up local temporary files, and log the event after the configured `-idle-time` is reached.

## 2. Setting Up the Test Environment

We provide a simple Go program `hang_server.go` in the `tests/idle_timeout` directory to simulate the faulty server described above.

### Step 2.1: Start the simulated faulty server
Open a terminal and run the following command to start the server:
```bash
cd tests/idle_timeout
go run hang_server.go
```
*Expected output: `Test hang server listening on :9999`*

### Step 2.2: Start url-proxy (with a very short timeout)
To avoid waiting too long, we set `-idle-time` to a short duration (e.g., 10 seconds) when starting `url-proxy`.
Open a **second** terminal and run the following in the project root:
```bash
go run url-proxy.go -addr :8888 -idle-time 10s
```
*Expected output: `watchdog started: idle-time=10s, internal-check-time=3.333333333s`*

## 3. Executing the Test

In a **third** terminal, use `curl` to request the faulty server through `url-proxy`:

```bash
curl http://localhost:8888/http://localhost:9999/hang
```

## 4. Observation and Verification

### 4.1 Behavior in the `curl` terminal
You will see `curl` output the initial data:
```
Hello, this is the start of the data...
```
Then `curl` will hang, waiting for the remaining 99MB of data.

### 4.2 Behavior in the `url-proxy` terminal (Core Verification)
Observe the log output of `url-proxy`. You should see a flow similar to the following:

1. **Request received and download starts**:
   ```text
   [1] req: http://localhost:9999/hang
   [1] job leader: http://localhost:9999/hang
   [1] job download: http://localhost:9999/hang
   ```
2. **After about 10 seconds, the Watchdog triggers cleanup**:
   ```text
   watchdog: killing idle job: http://localhost:9999/hang, inactive for 10.002s
   ```
3. **Underlying connection is forcefully cancelled, triggering cleanup logic**:
   ```text
   [1] job cancelled due to idle timeout: http://localhost:9999/hang
   [1] job finished: http://localhost:9999/hang
   [1] rename skipped: tmp file does not exist... (or rename fails because the file is incomplete)
   ```

### 4.3 Final behavior in the `curl` terminal
After `url-proxy` forcefully drops the connection, your `curl` command will also exit with an error because the connection was reset by the server (proxy):
```text
curl: (18) transfer closed with 99999961 bytes remaining to read
```

## 5. Conclusion
Through this test, you can clearly observe that `url-proxy` keenly detects the hung state of the upstream server and uses `context.Cancel` to cleanly sever the connection and reclaim resources, preventing the proxy server from exhausting its memory due to zombie connections.
