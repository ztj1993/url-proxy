#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.yml"

# 结果收集数组 (纯文本，不包含 ANSI 颜色代码)
declare -a RESULTS_ECO=()
declare -a RESULTS_MODE=()
declare -a RESULTS_CMD=()
declare -a RESULTS_TIME=()
declare -a RESULTS_CACHE=()
declare -a RESULTS_STATUS=()

cleanup() {
    echo ""
    echo "[INFO] Cleaning up and stopping test containers..."
    docker compose -f "${COMPOSE_FILE}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

echo "=================================================================================================="
echo "                     url-proxy Multi-Ecosystem Dual-Mode E2E Test Suite                           "
echo "=================================================================================================="

# 1. 基础环境校验
if ! command -v docker >/dev/null 2>&1; then
    echo "[ERROR] Docker is not installed or not running in the current environment!"
    exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
    echo "[ERROR] Docker Compose is not installed or unavailable!"
    exit 1
fi

# 2. 清理历史容器与网络，重置纯净运行环境
docker compose -f "${COMPOSE_FILE}" down -v --remove-orphans >/dev/null 2>&1 || true

# 3. 启动并构建 url-proxy 核心服务
echo "[1/3] Building and starting url-proxy service..."
docker compose -f "${COMPOSE_FILE}" up -d --force-recreate --build url-proxy

echo -n "[INFO] Waiting for url-proxy to be ready..."
READY=0
for i in $(seq 1 30); do
    STATUS=$(docker compose -f "${COMPOSE_FILE}" ps --format '{{.Health}}' url-proxy 2>/dev/null || true)
    if [ "$STATUS" = "healthy" ]; then
        READY=1
        break
    fi
    sleep 1
    echo -n "."
done
echo ""

if [ "$READY" -ne 1 ]; then
    echo "[ERROR] url-proxy service failed to become healthy within 30 seconds! Container logs:"
    docker compose -f "${COMPOSE_FILE}" logs url-proxy
    exit 1
fi
echo "[OK] url-proxy is up and healthy!"

# 确保容器内部 /cache 处于初始干净状态
docker exec url-proxy sh -c "rm -rf /cache/*" >/dev/null 2>&1 || true

# 测试调度与审计函数
run_test() {
    local eco="$1"
    local mode="$2"
    local cmd_desc="$3"
    local service_name="$4"
    local cache_pattern="$5"
    local expected_cache_desc="$6"

    echo ""
    echo "--------------------------------------------------------------------------------------------------"
    echo "Running test: [${eco}] - ${mode} (${cmd_desc})"

    local start_ts
    start_ts=$(date +%s%N)
    local status="PASS"

    if docker compose -f "${COMPOSE_FILE}" run --rm "${service_name}"; then
        status="PASS"
    else
        status="FAIL"
    fi

    local end_ts
    end_ts=$(date +%s%N)
    local elapsed_ms=$(( (end_ts - start_ts) / 1000000 ))
    local elapsed_sec
    elapsed_sec=$(awk "BEGIN {printf \"%.2fs\", ${elapsed_ms}/1000}")

    local cache_msg="None"
    if [ "$cache_pattern" = "tunnel" ] || [ "$cache_pattern" = "custom" ]; then
        cache_msg="${expected_cache_desc}"
    elif [ "$cache_pattern" = "anti-404" ]; then
        if docker exec url-proxy find /cache -name "*fake-artifact-404*" 2>/dev/null | grep -q .; then
            cache_msg="Contaminated"
            status="FAIL"
        else
            cache_msg="${expected_cache_desc}"
        fi
    elif [ "$cache_pattern" = "head-guard" ]; then
        local f
        f=$(docker exec url-proxy find /cache -path "*ubuntu*jammy*InRelease*" 2>/dev/null | head -n 1)
        if [ -n "$f" ]; then
            local sz
            sz=$(docker exec url-proxy stat -c%s "$f" 2>/dev/null || echo 0)
            if [ "$sz" -gt 0 ]; then
                cache_msg="${expected_cache_desc}"
            else
                cache_msg="Corrupted (${sz}B)"
                status="FAIL"
            fi
        else
            cache_msg="Not Cached"
            status="FAIL"
        fi
    elif [ "$cache_pattern" = "disk-ttl" ]; then
        if docker exec url-proxy find /cache -path "*ubuntu*noble*InRelease*" 2>/dev/null | grep -q .; then
            cache_msg="${expected_cache_desc}"
        else
            cache_msg="Not Cached"
            status="FAIL"
        fi
    elif [ -n "$cache_pattern" ]; then
        # 直接在 url-proxy 容器内部检查 /cache 目录中是否存在对应文件
        if docker exec url-proxy find /cache -path "${cache_pattern}" 2>/dev/null | grep -q .; then
            cache_msg="${expected_cache_desc}"
        else
            cache_msg="Not Found"
        fi
    fi

    RESULTS_ECO+=("${eco}")
    RESULTS_MODE+=("${mode}")
    RESULTS_CMD+=("${cmd_desc}")
    RESULTS_TIME+=("${elapsed_sec}")
    RESULTS_CACHE+=("${cache_msg}")
    RESULTS_STATUS+=("${status}")

    if [ "$status" = "PASS" ]; then
        echo "[PASS] ${eco} (${mode}) passed | Duration: ${elapsed_sec} | Cache / Status: ${cache_msg}"
    else
        echo "[FAIL] ${eco} (${mode}) failed!"
    fi
}

echo ""
echo "[2/3] Executing dual-mode test matrix across all ecosystems..."

# 1. APT Tests (Independent packages: tree for Prefix, ed for Proxy)
run_test "APT" "Prefix Mode" "apt download tree" "client-apt-prefix" "*/tree*.deb" ".deb cached (tree)"
run_test "APT" "HTTP Proxy" "apt download ed" "client-apt-proxy" "*/ed*.deb" ".deb cached (ed)"

# 2. Alpine Tests (Independent packages: tree for Prefix, sed for Proxy)
run_test "Alpine" "Prefix Mode" "apk fetch tree" "client-alpine-prefix" "*/tree*.apk" ".apk cached (tree)"
run_test "Alpine" "HTTP Proxy" "apk fetch sed" "client-alpine-proxy" "*/sed*.apk" ".apk cached (sed)"

# 3. PyPI Tests (Independent packages: six for Prefix, certifi for Proxy)
run_test "PyPI" "Prefix Mode" "pip install six" "client-pypi-prefix" "*six*.whl" ".whl cached (six)"
run_test "PyPI" "HTTP Proxy" "pip install certifi" "client-pypi-proxy" "tunnel" "HTTPS Tunnel"

# 4. npm Tests (Independent packages: is-sorted for Prefix, is-number for Proxy)
run_test "npm" "Prefix Mode" "npm install is-sorted" "client-npm-prefix" "*is-sorted*.tgz" ".tgz cached (is-sorted)"
run_test "npm" "HTTP Proxy" "npm install is-number" "client-npm-proxy" "tunnel" "HTTPS Tunnel"

# 5. Go Tests (Independent packages: pkg/errors for GOPROXY, google/uuid for Proxy)
run_test "Go" "GOPROXY Mode" "go download pkg/errors" "client-go-goproxy" "*errors*" ".zip/.mod cached (errors)"
run_test "Go" "HTTP Proxy" "go download google/uuid" "client-go-proxy" "*uuid*" ".zip/.mod cached (uuid)"

# 6. Maven Central Test (.jar file caching)
run_test "Maven" "Central Proxy" "fetch slf4j-api.jar" "client-maven" "*slf4j-api*.jar" ".jar cached (slf4j)"

# 7. Rust Cargo Test (config.json rewrite & .crate caching)
run_test "Cargo" "Sparse & Crates" "config rewrite & serde" "client-cargo" "*serde*.crate" "Rewritten & .crate cached"

# 8. Range 206 Partial Content Test
run_test "Range" "HTTP 206 Status" "curl range 0-499" "client-range" "custom" "206 Partial Content"

# 9. Conditional 304 Not Modified Test
run_test "Conditional" "HTTP 304 Status" "curl conditional 304" "client-304" "custom" "304 Not Modified"

# ==============================================================================================
# Extended Stability & Governance Test Suite (Plans 330 & 340)
# ==============================================================================================
# 10. Cache HIT & Data Consistency (Second Download Test - Feature 342)
run_test "Cache-HIT" "Second Download" "verify HIT & identical" "client-cache-hit" "custom" "HIT & Bit-Identical"

# 11. Non-200 404 Anti-Pollution Test (Feature 331)
run_test "Anti-Pollute" "404 Zero-Disk" "curl 404 fake-artifact" "client-404-guard" "anti-404" "Zero-Disk (Clean)"

# 12. HEAD Method Conversion & Pre-warming Test (Feature 344)
run_test "HEAD-Guard" "Method Conversion" "HEAD probe & GET warm" "client-head-guard" "head-guard" "Pre-warmed (>0 Bytes)"

# 13. Response Rewrite Sanitization & Content-Length Test (Feature 342)
run_test "Rewrite" "Header Cleansing" "strip ETag/Digest & len" "client-rewrite-guard" "custom" "Sanitized & Length OK"

# 14. Disk TTL Fresh HIT & Client ETag Isolation Test (Features 332 & 343)
run_test "Disk-TTL" "Fresh HIT & Guard" "TTL hit & ETag isolate" "client-ttl-guard" "disk-ttl" "HIT-DISK-TTL (Isolated)"

# 4. 打印报告 (纯英文字符表格，无 ANSI 颜色)
echo ""
echo "[3/3] Generating final inspection report..."
echo "=================================================================================================="
echo "                                url-proxy E2E Inspection Report                                   "
echo "=================================================================================================="
printf "%-12s %-16s %-26s %-10s %-26s %s\n" "Ecosystem" "Mode" "Command" "Duration" "Cache / Status" "Result"
echo "--------------------------------------------------------------------------------------------------"

TOTAL=${#RESULTS_ECO[@]}
PASSED=0
FAILED=0

for ((i=0; i<TOTAL; i++)); do
    eco="${RESULTS_ECO[$i]}"
    mode="${RESULTS_MODE[$i]}"
    cmd="${RESULTS_CMD[$i]}"
    t="${RESULTS_TIME[$i]}"
    c="${RESULTS_CACHE[$i]}"
    s="${RESULTS_STATUS[$i]}"

    if [ "$s" = "PASS" ]; then
        PASSED=$((PASSED + 1))
        status_disp="[PASS]"
    else
        FAILED=$((FAILED + 1))
        status_disp="[FAIL]"
    fi

    printf "%-12s %-16s %-26s %-10s %-26s %s\n" "$eco" "$mode" "$cmd" "$t" "$c" "$status_disp"
done

echo "--------------------------------------------------------------------------------------------------"
if [ "$FAILED" -eq 0 ]; then
    echo "Total: ${TOTAL} | Passed: ${PASSED} | Failed: 0 | Overall Health: HEALTHY"
else
    echo "Total: ${TOTAL} | Passed: ${PASSED} | Failed: ${FAILED} | Overall Health: UNHEALTHY"
fi
echo "=================================================================================================="

if [ "$FAILED" -ne 0 ]; then
    exit 1
fi
exit 0
