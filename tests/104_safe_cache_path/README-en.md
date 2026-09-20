# Safe Cache Path Generation Test Guide

This document describes how to test the Safe Local Cache Path Generation mechanism (`getSafeUrlPath`) in `url-proxy`.

## 1. Test Principle

Verify how `getSafeUrlPath` maps complex URLs onto the local filesystem:
1. **Host Port Colon Replacement**: When the URL is `http://localhost:9995/...`, the top-level cache folder becomes `http-localhost-9995`, replacing the colon `:` with a hyphen `-` for Windows compatibility.
2. **Query Parameter Isolation & Escaping**: Distinct requests to `/api/item?v=1` and `/api/item?v=2` produce isolated files (e.g. `item%3Fv%3D1` and `item%3Fv%3D2`) without collision.
3. **Trailing Slash Normalization**: A request to `/folder/` is saved locally with the filename `index`.

## 2. Setting Up the Test Environment

We provide a simple Go program `path_test_server.go` in the current directory.

### Step 2.1: Start the simulated upstream server
Open a terminal and run the following command to start the server:
```bash
go run path_test_server.go
```
*Expected output: `测试用安全路径验证服务器已启动，监听端口 :9995`*

### Step 2.2: Start url-proxy (with a custom test cache directory)
Open a **second** terminal and run the following in the project root:
```bash
mkdir -p .test_path_cache
go run url-proxy.go -addr :8888 -cache ./.test_path_cache
```
*Expected output: `cache dir: ./.test_path_cache`*

## 3. Running the Test

Open a **third** terminal and execute three distinct requests:

1. **Test Parameterized Request 1**:
   ```bash
   curl -s http://localhost:8888/http://localhost:9995/api/item?v=1
   ```

2. **Test Parameterized Request 2 (Same path, different query)**:
   ```bash
   curl -s http://localhost:8888/http://localhost:9995/api/item?v=2
   ```

3. **Test Directory Trailing Slash Request**:
   ```bash
   curl -s http://localhost:8888/http://localhost:9995/folder/
   ```

## 4. Observations and Verification

Inspect the directory structure inside `.test_path_cache`:
```bash
find .test_path_cache -type f
```

### Expected File Structure:
```text
.test_path_cache/http-localhost-9995/api/item%3Fv%3D1
.test_path_cache/http-localhost-9995/api/item%3Fv%3D2
.test_path_cache/http-localhost-9995/folder/index
```

*Key checks*:
1. **Windows Compatibility**: Top-level directory is `http-localhost-9995`, with the colon before the port replaced by `-`.
2. **Query Isolation**: `item?v=1` and `item?v=2` are stored as distinct, independent cache files.
3. **Default Filename**: The `/folder/` request is saved as `index`.

## 5. Conclusion
Through this test, you can verify that `url-proxy` reliably maps and isolates complex URLs into safe, cross-platform cache paths.
