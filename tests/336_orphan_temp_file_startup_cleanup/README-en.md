# Feature 336: Startup Orphan Temporary File Sweep & Auto-Cleanup Test Suite

This directory provides the standalone test environment and automated regression test suite for Feature 336 (Startup Orphan Temporary File Sweep & Auto-Cleanup).

---

## 1. Directory Layout

- `proxy_test.conf`: Test configuration declaring service address and forwarding rules.
- `mock_server.go`: Mock upstream server for artifact downloads.
- `orphan_cleanup_test.go`: Automated test suite covering exact temporary filename pattern recognition, selective stale orphan removal, and safety guards for active temporary files and valid artifacts.
- `README-zh.md`: Chinese documentation.
- `README-en.md`: English documentation.

---

## 2. Automated Test Execution

Run from repository root:
```bash
go test -v -count=1 ./tests/336_orphan_temp_file_startup_cleanup/...
```

---

## 3. Manual Verification Steps

### Step 1: Start Mock Upstream Server
```bash
go run tests/336_orphan_temp_file_startup_cleanup/mock_server.go
```

### Step 2: Create Stale Orphan Temporary File
Create a test temporary file in the cache directory and backdate its timestamp:
```bash
touch cache/test.deb.1700000000000000000
touch -d "2 hours ago" cache/test.deb.1700000000000000000
```

### Step 3: Start url-proxy Instance
```bash
go run url-proxy.go -conf tests/336_orphan_temp_file_startup_cleanup/proxy_test.conf -cache ./cache -addr :8888
```
Observe the startup log confirming `orphan temp file cleanup completed: removed 1 stale files`, and verify the stale temporary file is deleted.