# Idle Timeout and Cleanup Optimization Plan

## 1. Optimization Goal
Address the issue of permanent hangs in concurrent downloads caused by network anomalies (e.g., upstream server stops responding but connection remains open). Implement a robust mechanism to detect stalled downloads, abort them gracefully, and clean up associated resources to prevent memory and connection leaks.

## 2. Core Mechanism
Adopt the **"Idle Timeout Watchdog + Context Cancellation"** strategy.

- **Idle Timeout**: Instead of a global timeout (which is unsuitable for large files), monitor the time elapsed since the last byte was received.
- **Watchdog Routine**: A background goroutine periodically scans all active `DownloadJob` instances to check for staleness.
- **Context Cancellation**: Use Go's `context.Context` to forcefully terminate the hung HTTP request, triggering immediate cleanup in the Leader's download loop.

## 3. Technical Solution

### 3.1 State Management (`DownloadJob` Updates)
Extend the `DownloadJob` struct to track activity and support cancellation:

```go
type DownloadJob struct {
    // ... existing fields ...
    LastActive time.Time          // Timestamp of the last successful read
    Cancel     context.CancelFunc // Function to abort the upstream request
}
```

### 3.2 Leader Request with Context
When the Leader initiates the download, it wraps the request in a cancellable context:

1.  Create a context: `ctx, cancel := context.WithCancel(context.Background())`.
2.  Store `cancel` in the `DownloadJob`.
3.  Execute the request: `req, _ := http.NewRequestWithContext(ctx, "GET", targetUri, nil)`.

### 3.3 Activity Tracking (Heartbeat)
Inside the Leader's `resp.Body.Read` loop, update `LastActive` every time data is successfully read:

```go
n, err := resp.Body.Read(buf)
if n > 0 {
    job.Mu.Lock()
    job.LastActive = time.Now()
    job.Mu.Unlock()
    // ... process data ...
}
```

### 3.4 Background Watchdog
Introduce a global `init` or `main` level goroutine to monitor all jobs:

1.  Use `time.NewTicker` (e.g., every 1 minute).
2.  Iterate through the global `jobs` map.
3.  If `time.Since(job.LastActive) > IdleTimeoutThreshold` (e.g., 5 minutes):
    - Call `job.Cancel()`.
    - Log the forceful termination.

### 3.5 Cleanup & Error Handling Flow
When `job.Cancel()` is invoked or a network error occurs:
1.  The Leader's `resp.Body.Read` immediately returns a `context.Canceled` or other network error.
2.  The Leader records the error state (`job.Err = err`), breaks out of the loop, and executes its `defer` block.
3.  The `defer` block safely:
    - Removes the job from the global map (`removeJob`).
    - Wakes up all waiting Followers (`job.Cond.Broadcast()`) and closes the `Done` channel.
    - **Corrupted Data Cleanup**: During the final `tryRename` phase, if an error is detected (`job.Err != nil`), the Leader **strictly skips renaming**. Instead, it directly deletes the incomplete temporary file using `os.Remove(job.TmpName)`.
    - **Follower Notification**: Awakened Followers reading EOF will check for `job.Err != nil`. If an error exists, they will immediately drop the client connection, preventing the client from receiving corrupted cache data.

## 4. Expected Benefits
1.  **High Reliability**: Effectively prevents "zombie" jobs from permanently occupying memory and file handles.
2.  **Data Consistency**: If a download fails mid-way (e.g., 9GB out of 10GB), the system automatically deletes the incomplete temporary file and drops all current Follower connections. Subsequent requests will act as a new Leader and download from scratch, completely eliminating the risk of serving corrupted files as legitimate cache.
3.  **Large File Friendly**: By focusing on *idle time* rather than *total time*, slow but steady downloads of massive files will never be incorrectly terminated.
4.  **Clean Shutdown**: Leverages Go's native `context` to ensure TCP connections are dropped and all goroutine locks/channels are safely released.
