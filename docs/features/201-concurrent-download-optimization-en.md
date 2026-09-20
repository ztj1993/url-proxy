# Request Coalescing Optimization Plan

## 1. Optimization Goal
In scenarios where multiple users simultaneously request the same large file, the goal is to reduce redundant requests to the upstream server, save bandwidth, and improve the concurrent download experience.

## 2. Core Mechanism
We adopt a strategy combining **Request Coalescing** and **Shared Streaming**.

- **Request Coalescing**: When multiple users request the same resource, only the first user (Leader) initiates a remote download request.
- **Shared Streaming**: Subsequent users (Followers) directly read data from the temporary file being downloaded by the Leader, achieving data reuse.

## 3. Technical Solution

### 3.1 State Management (`DownloadJob`)
Introduce a `DownloadJob` struct to manage the state of concurrent download tasks:

```go
type DownloadJob struct {
    Uri        string
    FilePath   string       // Local temporary file path
    Header     http.Header  // Response headers
    HeaderDone chan struct{} // Signal that headers are ready
    Done       chan struct{} // Signal that download is complete
    Cond       *sync.Cond    // Broadcast "new data written"
    Mu         sync.Mutex    // Used with Cond
    Readers    int32         // Number of Followers currently reading
    RenameMu   sync.Mutex    // Protects the rename operation
    FinalName  string        // Final cache file name
    TmpName    string        // Temporary file name
}
```

### 3.2 Real-time Broadcasting (`sync.Cond`)
Utilize Go's `sync.Cond` to implement an efficient "Publish/Subscribe" pattern:
- **Publisher (Leader)**: Calls `Cond.Broadcast()` to wake up all waiters every time a chunk of data is written to the temporary file.
- **Subscriber (Follower)**: After reading all currently available data, calls `Cond.Wait()` to sleep and wait for new data to arrive.

### 3.3 File System Buffer
Use the local file system as a buffer to enable **Catch-up**:
- **Leader**: Appends data to the temporary file.
- **Follower**: Opens the same temporary file and starts reading from the beginning (Offset 0).
    - **Catch-up Phase**: Reads the already downloaded part (e.g., first 50%) as fast as disk IO allows.
    - **Sync Phase**: After reaching the End of File (EOF), automatically switches to `Wait` mode to follow the Leader's write progress in real-time.

### 3.4 Resolving Windows File Locking Issues (Reference Counting)
On Windows, `os.Rename` fails if the temporary file is still open for reading by any Follower. This is solved by introducing **Reference Counting**:
- **Increment**: When a client (Leader or Follower) opens the file for streaming, the `Readers` counter is incremented by 1.
- **Decrement**: When the file is closed (download completes or client disconnects), `Readers` is decremented by 1 in a `defer` block.
- **Delayed Renaming (`tryRename`)**:
    - The Leader attempts to rename upon download completion. If `Readers > 0`, it skips the rename.
    - The last Follower to close the file (when `Readers` reaches 0) checks if the Leader has finished downloading. If so, it performs the rename operation on behalf of the Leader.

### 3.5 Observability and Log Tracing
To accurately track request states in high-concurrency scenarios:
- **Request ID (`reqID`)**: Introduced a global atomic counter `requestID uint64` to assign a unique ID to every incoming HTTP request.
- **Log Correlation**: All relevant logs are prefixed with `[reqID]` (e.g., `[1] req: ...`), providing a clear trace of the entire lifecycle: request arrival, role assignment (Leader/Follower), streaming, and file renaming.

## 4. Workflow

### Scenario Demo: Downloading a 1GB File
1.  **T=0 (Leader)**: User A initiates a request `[reqID=1]`.
    -   System creates a `DownloadJob`.
    -   Starts downloading and writing to `temp.dat`.
2.  **T=5 (Follower)**: User B initiates the same request `[reqID=2]`.
    -   At this point, `temp.dat` has 500MB of data.
    -   System detects an existing `DownloadJob`, B joins as a Follower.
    -   B opens `temp.dat`, `Readers` increments by 1, and B **instantly reads** the first 500MB.
3.  **T=5.1 (Sync)**: B finishes reading 500MB and catches up with A's progress.
    -   B enters `Cond.Wait()` state.
    -   As A writes every 32KB, B receives a notification and sends data to the client.
4.  **T=10 (Finish)**: Download completes.
    -   A closes the `Done` channel.
    -   A attempts to rename `temp.dat`, but since B is still reading (`Readers=1`), the rename is skipped.
    -   B receives the final data and closes the connection, `Readers` decrements to 0.
    -   B triggers `tryRename` and successfully renames `temp.dat` to the final cache file.

## 5. Benefits
1.  **Bandwidth Savings**: Regardless of the number of concurrent downloads, upstream bandwidth consumption remains constant at 1 unit.
2.  **Instant Response**: Late-joining users receive downloaded data immediately without waiting.
3.  **Cross-Platform Compatibility**: Perfectly resolves the Windows file-in-use issue, ensuring cache files are correctly persisted.
4.  **Strong Traceability**: The `reqID`-based logging system makes concurrent states and abnormal behaviors clear at a glance.
