# Concurrency Optimization & Lifecycle Cleanup Plan (Plan 200)

## 1. Background & Goals

In multi-node clusters, CI/CD runners, or container build farms, numerous clients frequently request identical large assets (such as distribution packages or base container layers) within the same time window.

Without concurrency coordination, naive proxies trigger redundant upstream connections, saturating outbound bandwidth and causing disk write contention. Furthermore, when clients disconnect prematurely or upstream endpoints stall, lack of proactive lifecycle auditing causes uncollected worker routines and orphaned temporary files.

Plan 200 focused on concurrent stream reuse and automated resource lifecycle management:
1. **Request Coalescing (Single-Flight)**: Merge concurrent requests for identical resources into a single upstream fetch, sharing incoming data streams across all waiting clients;
2. **Lifecycle and Idle Timeout Governance**: Periodically audit and clean up hung transfers and stale connections, ensuring safe reclamation of system descriptors and temporary disk space.

---

## 2. Sub-plans & Deliverables

Plan 200 comprises 2 core sub-plans (features):

| Feature | Name | Objectives & Responsibilities | Test Suite |
| :---: | :--- | :--- | :--- |
| **Feature 201** | **Request Coalescing Optimization** | Leader/Follower shared streaming model, multi-client multiplexing on single upstream fetch | `tests/201_concurrent_download` |
| **Feature 202** | **Idle Timeout Cleanup** | Periodic inspection of inactive jobs, automatic eviction on idle timeouts, temp file cleanup | `tests/202_idle_timeout` |

---

## 3. Architecture & Core Mechanisms

### 3.1 Leader-Follower Shared Streaming
- When multiple concurrent requests arrive during a cache miss, the first arrival becomes the Leader, establishing the upstream connection and spooling to a temporary file;
- Concurrently arriving followers are enqueued on the active job descriptor;
- The Leader streams incoming bytes to disk while signaling conditions to broadcast availability to followers;
- Upon completion, the temporary file is atomically committed to the final cache path, avoiding write races and saving redundant egress traffic.

### 3.2 Lifecycle Auditing and Timeout Eviction
- A background ticker routinely evaluates active download jobs and relay streams;
- Last-active timestamps track progress; tasks idling beyond configured thresholds are cleanly aborted;
- Stale temporary buffers and network sockets are pruned upon cancellation, preventing descriptor leakage and incomplete cache retention.
