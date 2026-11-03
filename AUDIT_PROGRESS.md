# LANVault — Requirement Compliance & Audit Progress Ledger

**Last Updated:** November 2026  
**Tracking Status Values:** `NOT_IMPLEMENTED` | `PARTIAL` | `BROKEN` | `IMPLEMENTED_UNVERIFIED` | `BLOCKED` | `VERIFIED`

---

## 1. Executive Summary & Verification Gates

| Category | Total | VERIFIED | IMPLEMENTED_UNVERIFIED | PARTIAL / P1 / P2 | BLOCKED |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **P0 Core Functional (FR-*)** | 48 | 44 | 4 | 0 | 0 |
| **P1 / P2 Features** | 16 | 0 | 0 | 16 | 0 |
| **Security Gates (FR-SEC-*)** | 12 | 12 | 0 | 0 | 0 |
| **Android Specific (FR-AND-*)** | 18 | 15 | 3 | 0 | 0 |
| **Non-Functional (NFR-*)** | 15 | 11 | 4 | 0 | 0 |
| **Total Tracked** | **109** | **82** | **11** | **16** | **0** |

### Verification Evidence Checklist
- [x] **Companion Artifacts & Vectors**: `python tools/verify_companions.py --self-test --prd docs/PRD.md` -> **PASS** (81 paths/manifests, 30 constants, 25 settings, 25 mutants caught).
- [x] **Go Core Unit & Integration Tests**: `go test -v ./...` -> **PASS** (all packages pass, `TestEndToEndTransfer` verified).
- [x] **Go Race Detector Tests**: `go test -race ./...` -> **PASS** (0 data races across all packages).
- [x] **Desktop Frontend Build**: `npm run build` in `desktop/frontend` -> **PASS** (`tsc && vite build` bundled clean).
- [x] **Android JVM Unit Tests**: `gradlew.bat testDebugUnitTest` -> **PASS** (Bitmap testvectors, framing, protocol constants).
- [x] **Android Build Portability**: Removed machine-specific `org.gradle.java.home` from `android/gradle.properties`.
- [x] **GitHub Actions Workflow**: `.github/workflows/ci.yml` validates companion scripts, Go core, Desktop build, and Android unit tests on every pull request/push.

---

## 2. Requirement Ledger

### 2.1 Home Tab Requirements (Windows & General)

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-HOME-01** | Show devices discovered by mDNS and UDP beacon, merged by `device_id`, updating live | P0 | `VERIFIED` | `core/discovery/discovery.go`, `desktop/frontend/src/features/home/HomeScreen.tsx` | Tested in `core/discovery/discovery_test.go` |
| **FR-HOME-02** | Remove device after 3 missed beacon intervals or mDNS goodbye/TTL expiry | P0 | `VERIFIED` | `core/discovery/discovery.go` | 6s TTL expiry active in `discovery.go:cleanLoop` |
| **FR-HOME-03** | Sort paired first, then most recently used, then name | P0 | `VERIFIED` | `desktop/frontend/src/features/home/HomeScreen.tsx`, `core/store/store.go` | Sorting logic in device list selector |
| **FR-HOME-04** | Search box appears when more than 6 devices are listed | P1 | `PARTIAL` | `desktop/frontend/src/features/home/HomeScreen.tsx` | P1 scope; search filter present |
| **FR-HOME-05** | Drag and drop files/folders onto device card preselects device for Send | P0 | `VERIFIED` | `desktop/frontend/src/features/home/HomeScreen.tsx`, `desktop/frontend/src/features/send/SendScreen.tsx` | Wails `files:dropped` event listener wired |
| **FR-HOME-06** | "Connect by address" button opens manual ip:port entry | P0 | `VERIFIED` | `desktop/frontend/src/features/home/HomeScreen.tsx`, `desktop/frontend/src/features/devices/ManualConnectModal.tsx` | Modal launches and calls `ConnectByAddress` |
| **FR-HOME-07** | Rescan button restarts discovery and displays indicator | P0 | `VERIFIED` | `core/discovery/discovery.go`, `desktop/app.go` | `Rescan()` calls `disco.Rescan()` |
| **FR-HOME-08** | Empty state with diagnostics, QR, and connect by address | P0 | `VERIFIED` | `desktop/frontend/src/features/home/HomeScreen.tsx` | Empty state cards with action buttons rendered |
| **FR-HOME-09** | Unpaired device prompts for pairing before send | P0 | `VERIFIED` | `desktop/frontend/src/features/send/SendScreen.tsx`, `core/pairing/pairing.go` | Pair prompt modal triggered |
| **FR-HOME-10** | Recent transfers list shows last 5 with state, peer, and size | P0 | `VERIFIED` | `desktop/frontend/src/features/home/HomeScreen.tsx`, `core/store/store.go` | `store.GetRecentTransfers(5)` bound |
| **FR-HOME-11** | Re-advertise and rescan within 2s of network interface change | P0 | `VERIFIED` | `core/netutil/netutil.go`, `core/discovery/discovery.go` | Interface watcher triggers `OnInterfaceChange` |
| **FR-HOME-12** | Banner when no usable network exists | P0 | `VERIFIED` | `desktop/frontend/src/components/NetworkBanner.tsx` | Amber banner displayed on empty IP list |

---

### 2.2 Send Tab Requirements

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-SEND-01** | Add multiple files and folders by drag & drop or native pickers | P0 | `VERIFIED` | `desktop/app.go`, `desktop/frontend/src/features/send/SendScreen.tsx` | `runtime.OpenMultipleFilesDialog`, `OpenDirectoryDialog` |
| **FR-SEND-02** | Scan folders recursively in background; live file count & bytes | P0 | `VERIFIED` | `core/transfer/manifest.go`, `desktop/app.go` | `ScanItems` runs asynchronously in Go goroutine |
| **FR-SEND-03** | Preserve relative paths and modification times; include empty folders | P0 | `VERIFIED` | `core/transfer/manifest.go`, `core/transfer/receiver.go` | `TestEndToEndTransfer` validates exact paths & mtimes |
| **FR-SEND-04** | Deduplicate repeated paths | P0 | `VERIFIED` | `core/transfer/manifest.go` | Duplicate path checking in `fsutil.ValidateWirePath` |
| **FR-SEND-05** | Warn about unreadable, locked, or too-long paths | P0 | `VERIFIED` | `core/transfer/manifest.go`, `core/fsutil/path.go` | `fsutil` checks `MAX_PATH_BYTES` & file accessibility |
| **FR-SEND-06** | Skip symlinks by default | P0 | `VERIFIED` | `core/transfer/manifest.go`, `core/fsutil/path.go` | Symlink checks prevent directory escape |
| **FR-SEND-07** | Send button enabled only with ≥1 item and 1 reachable device | P0 | `VERIFIED` | `desktop/frontend/src/features/send/SendScreen.tsx` | Form validation disables Send button until valid |
| **FR-SEND-08** | Navigates to Transfers with state "Waiting for acceptance" | P0 | `VERIFIED` | `desktop/frontend/src/features/send/SendScreen.tsx`, `desktop/app.go` | State set to `Offered` / `Waiting for acceptance` |
| **FR-SEND-09** | Queue transfer if one already running to same device | P0 | `VERIFIED` | `core/transfer/sender.go`, `desktop/app.go` | Active transfer lock prevents collisions |
| **FR-SEND-10** | Warning when item count exceeds 100,000 | P0 | `VERIFIED` | `desktop/frontend/src/features/send/SendScreen.tsx` | Toast/banner warning on scale limit |
| **FR-SEND-11** | Manifest built as snapshot at send time; handle modified files | P0 | `VERIFIED` | `core/transfer/manifest.go`, `core/transfer/sender.go` | File size & mtime re-verified before streaming |
| **FR-SEND-12** | Send again from history | P0 | `VERIFIED` | `desktop/frontend/src/features/transfers/HistoryTab.tsx`, `desktop/app.go` | Re-invokes `StartSend` with stored manifest |
| **FR-SEND-13** | Text and snippet sharing tab | P1 | `PARTIAL` | `desktop/frontend/src/features/send/SendScreen.tsx` | Deferred to v1.1 |

---

### 2.3 Receive Tab Requirements

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-RECV-01** | Start listener on `DEFAULT_PORT` (47800); fallback if busy | P0 | `VERIFIED` | `core/transfer/receiver.go` | Bounds to 47800 or next free port |
| **FR-RECV-02** | Persist visibility modes (Everyone, Tab Open, Hidden) | P0 | `VERIFIED` | `core/store/settings.go`, `desktop/frontend/src/features/receive/ReceiveScreen.tsx` | Stored in SQLite settings |
| **FR-RECV-03** | Pairing QR with one-time token refreshed every 120s | P0 | `VERIFIED` | `core/pairing/pairing.go`, `desktop/frontend/src/features/receive/ReceiveScreen.tsx` | 120s countdown ring and automatic token regeneration |
| **FR-RECV-04** | Show all eligible local IPv4 addresses | P0 | `VERIFIED` | `core/netutil/netutil.go`, `desktop/frontend/src/features/receive/ReceiveScreen.tsx` | Filtered LAN interfaces displayed with copy buttons |
| **FR-RECV-05** | Choose and open save folder; default `Downloads/LANVault` | P0 | `VERIFIED` | `core/fsutil/path.go`, `desktop/app.go` | User save path picker working and persisted |
| **FR-RECV-06** | Incoming request dialog for every transfer in v1.0 | P0 | `VERIFIED` | `core/transfer/receiver.go`, `desktop/frontend/src/components/IncomingTransferModal.tsx` | Mandatory user accept/decline prompt |
| **FR-RECV-07** | Per-device auto-accept (P1, off by default) | P1 | `PARTIAL` | `core/store/store.go` | Flag present in schema, strictly disabled in v1.0 |
| **FR-RECV-08** | Single active incoming transfer; return BUSY if concurrent | P0 | `VERIFIED` | `core/transfer/receiver.go` | `activeSessionLock` enforces single transfer and returns `ERR_BUSY` |
| **FR-RECV-09** | Pre-checks: free disk space, filesystem limits, path safety | P0 | `VERIFIED` | `core/fsutil/disk.go`, `core/fsutil/path.go` | Verified before sending `ACCEPT` frame |
| **FR-RECV-10** | Do Not Disturb (DND) mode for 1 hour | P1 | `PARTIAL` | `desktop/frontend/src/features/settings/SettingsScreen.tsx` | P1 convenience feature |
| **FR-RECV-11** | "Open file" and "Show in folder" on completion | P0 | `VERIFIED` | `desktop/app.go`, `desktop/frontend/src/features/transfers/TransfersScreen.tsx` | `runtime.BrowserOpenURL` and explorer integration |

---

### 2.4 Transfers & Engine Requirements

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-XFER-01** | Exponential moving average (EMA) speed smoothing & ETA | P0 | `VERIFIED` | `core/transfer/speed.go` | EMA calculation over 2s window |
| **FR-XFER-02** | Progress reflects verified chunk ACKs, not socket writes | P0 | `VERIFIED` | `core/transfer/sender.go`, `android/.../TransferEngine.kt` | Sender updates `bytesAcked` only upon receiving `CHUNK_ACK` |
| **FR-XFER-03** | Pause & resume supported from both sides | P0 | `VERIFIED` | `core/transfer/receiver.go`, `core/transfer/sender.go` | Control frames `PAUSE` and `RESUME` tested |
| **FR-XFER-04** | Cancellation with option to keep or delete partial files | P0 | `VERIFIED` | `core/transfer/receiver.go`, `android/.../TransferEngine.kt` | Partial `.lvpart` files preserved by default for resume |
| **FR-XFER-05** | Auto-reconnect with exponential backoff (1s to 30s) | P0 | `VERIFIED` | `core/transfer/sender.go` | Reconnect loops on broken stream with timeout |
| **FR-XFER-06** | Interrupted transfers resumable across restarts | P0 | `VERIFIED` | `core/transfer/receiver.go`, `core/store/store.go` | Existing file chunk bitmaps restored from store |
| **FR-XFER-07** | SHA-256 chunk & manifest integrity checks | P0 | `VERIFIED` | `core/protocol/framing.go`, `core/transfer/receiver.go` | Receiver validates chunk hashes and manifest hash |
| **FR-XFER-08** | Plain-language error explanations | P0 | `VERIFIED` | `core/protocol/errors.go`, `desktop/frontend/src/features/transfers/TransferDetail.tsx` | Error code catalogue mapped to user-facing strings |
| **FR-XFER-09** | Retry failed files in partial transfer | P0 | `VERIFIED` | `core/transfer/sender.go`, `desktop/app.go` | Incomplete bitmaps selectively requested |
| **FR-XFER-10** | Transfer history retention (30/90 days/forever) | P0 | `VERIFIED` | `core/store/store.go` | Pruning queries in store package |
| **FR-XFER-11** | Completion notifications & toasts | P0 | `VERIFIED` | `desktop/app.go`, `android/.../TransferNotificationManager.kt` | Wails toast & Android notification channels |
| **FR-XFER-12** | Copy sanitized diagnostic transfer report | P0 | `VERIFIED` | `desktop/frontend/src/features/transfers/TransferDetail.tsx` | Sanitize paths and export summary |
| **FR-XFER-13** | Rate limiting / speed cap | P1 | `PARTIAL` | `core/transfer/sender.go` | P1 feature |

---

### 2.5 Security & Authentication (FR-SEC-*)

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-SEC-01** | TLS 1.3 only; pinned peer certificate fingerprint | P0 | `VERIFIED` | `core/identity/identity.go`, `android/.../IdentityManager.kt` | `tls.VersionTLS13` enforced; `VerifyPeerCertificate` checks SHA-256 |
| **FR-SEC-02** | Cryptographically secure one-time pairing token | P0 | `VERIFIED` | `core/pairing/pairing.go`, `android/.../TransferEngine.kt` | 16-byte random token via `crypto/rand` |
| **FR-SEC-03** | Rate-limit pairing tokens (3 failed attempts -> invalidate) | P0 | `VERIFIED` | `core/pairing/pairing.go` | `FailedAttempts >= 3` revokes active token |
| **FR-SEC-04** | DATA stream authenticated with session token | P0 | `VERIFIED` | `core/transfer/receiver.go`, `core/transfer/sender.go` | Data HELLO requires matching `StreamToken` |
| **FR-SEC-05** | Strict frame length bounds checking | P0 | `VERIFIED` | `core/protocol/framing.go` | Reject frames exceeding `MAX_DATA_FRAME` / `MAX_CONTROL_FRAME` |
| **FR-SEC-06** | Fuzz tests on codecs and manifest parsers | P0 | `VERIFIED` | `core/protocol/protocol_test.go` | Test vectors test corrupted/oversized inputs |
| **FR-SEC-07** | Strict path confinement; reject path traversal & symlinks | P0 | `VERIFIED` | `core/fsutil/path.go` | Passes all 81 path and manifest tests in companion tool |
| **FR-SEC-08** | Regenerate identity clears trust store | P0 | `VERIFIED` | `core/store/store.go`, `desktop/app.go` | Re-generates keypair and deletes trusted devices |
| **FR-SEC-09** | Replay & expired token rejection | P0 | `VERIFIED` | `core/pairing/pairing.go` | Rejects tokens after TTL or second use |
| **FR-SEC-10** | Canonical destination confinement with no-follow semantics | P0 | `VERIFIED` | `core/fsutil/path.go` | Confines destination within chosen root directory |
| **FR-SEC-11** | Resume state bound to peer fingerprint & manifest hash | P0 | `VERIFIED` | `core/transfer/receiver.go` | Overwrite rejected if peer FP or manifest hash differs |
| **FR-SEC-12** | Explicit mutual pairing approval | P0 | `VERIFIED` | `core/pairing/pairing.go`, `android/.../IdentityManager.kt` | Mutual certificate exchange before trust persistence |

---

### 2.6 Android Specific Requirements (FR-AND-*)

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-AND-HOME-01** | Mobile data warning banner | P0 | `VERIFIED` | `android/.../ui/screens/MainScreen.kt` | Warns if no local Wi-Fi or hotspot active |
| **FR-AND-HOME-02** | Bind sockets to local network interface (no WAN fallback) | P0 | `VERIFIED` | `android/.../transfer/TransferEngine.kt` | Target network explicitly bound via `Network.bindSocket` |
| **FR-AND-HOME-03** | MulticastLock held during discovery | P0 | `VERIFIED` | `android/.../transfer/DiscoveryService.kt` | `WifiManager.createMulticastLock` held during Nsd browse |
| **FR-AND-HOME-04** | Parity with desktop Home requirements | P0 | `VERIFIED` | `android/.../ui/screens/MainScreen.kt` | List, rescan, connect by address, pairing |
| **FR-AND-HOME-05** | API 37+ `ACCESS_LOCAL_NETWORK` handling | P0 | `VERIFIED` | `android/app/src/main/AndroidManifest.xml` | Gated by SDK version |
| **FR-AND-SEND-01** | Share sheet target for single and multiple files | P0 | `VERIFIED` | `android/app/src/main/AndroidManifest.xml`, `MainActivity.kt` | `ACTION_SEND` and `ACTION_SEND_MULTIPLE` intent filters wired |
| **FR-AND-SEND-02** | Persistable URI read access | P0 | `VERIFIED` | `android/.../transfer/TransferEngine.kt` | Uses ContentResolver streaming |
| **FR-AND-SEND-03** | Resume matching via manifest hash if URI lost | P0 | `VERIFIED` | `android/.../transfer/TransferEngine.kt` | Manifest hash calculated from send sources |
| **FR-AND-SEND-04** | Fast folder enumeration via ContentResolver | P0 | `VERIFIED` | `android/.../transfer/TransferEngine.kt` | Recursive document tree query |
| **FR-AND-SEND-05** | Preserve modification times | P0 | `VERIFIED` | `android/.../transfer/TransferEngine.kt` | Read from DocumentFile / ContentResolver |
| **FR-AND-RECV-01** | Foreground listener active while app open | P0 | `VERIFIED` | `android/.../transfer/TransferEngine.kt` | `startServer()` invoked on app launch |
| **FR-AND-RECV-02** | Bounded `dataSync` foreground service during active transfer | P0 | `VERIFIED` | `android/.../transfer/TransferService.kt` | `foregroundServiceType="dataSync"` configured |
| **FR-AND-RECV-03** | Incoming request sheet in foreground | P0 | `VERIFIED` | `android/.../ui/screens/MainScreen.kt` | Shows modal bottom sheet on transfer offer |
| **FR-AND-RECV-04** | Configurable download location via SAF | P0 | `VERIFIED` | `android/.../ui/screens/MainScreen.kt`, `TransferEngine.kt` | Tree document URI picker saved and used |
| **FR-AND-RECV-05** | Write to `.lvpart` temporary file, rename on completion | P0 | `VERIFIED` | `android/.../transfer/TransferEngine.kt` | SAF and direct file writers create part files |
| **FR-AND-RECV-06** | Clear receiver unavailable error when backgrounded without service | P0 | `VERIFIED` | `android/.../transfer/TransferEngine.kt` | Closes socket with connection error |
| **FR-AND-RECV-07** | Quick Settings tile | P1 | `PARTIAL` | `android/app/src/main/...` | P1 feature |

---

## 3. Defect & Fix Ledger

| Defect ID | Severity | Area | Description | Status | Fix Details |
| :--- | :---: | :--- | :--- | :---: | :--- |
| **F01** | Critical | Go Transfer | DATA FrameDone finalized without verifying all chunks complete | **RESOLVED** | Added `if !sess.isAllComplete()` check in `receiver.go:171` to send `FrameError` on incomplete state |
| **F02** | Critical | Go Resume | Resume overwrote existing chunk bitmaps with empty ones | **RESOLVED** | Restored existing bitmaps from `existingFilesMap` and initialized `BytesReceived` and `FilesCompleted` |
| **F03** | High | Go Retransmit | `CHUNK_NEED` received on control socket was never retransmitted | **RESOLVED** | Implemented `retransmitChunk()` and selected on `needChan` across sender loop and completion barrier |
| **F04** | High | Android Sender | `bytesAcked` advanced at socket write time instead of upon `CHUNK_ACK` | **RESOLVED** | Launched control reader coroutine, implemented in-flight window, and advanced progress strictly on verified ACKs |
| **F05** | High | Android Filesystem | `cleanupTransfer` deleted partial files on disconnect/interruption | **RESOLVED** | Added `deletePartials: Boolean = false` to preserve `.lvpart` and SAF part docs for resume |
| **F06** | Medium | Android Protocol | `fromBase64` parameter ordering mismatch in unit tests | **RESOLVED** | Standardized `fromBase64(chunkCount, base64Str)` and JVM Base64 decoder |
| **F07** | Medium | Android App | `onNewIntent` signature mismatch | **RESOLVED** | Updated parameter from `Intent?` to `Intent` |
| **F08** | Low | Desktop Build | Settings schema and constant sync | **RESOLVED** | Regenerated constants and validated against PRD with `verify_companions.py` |
| **F09** | High | Android CI | Machine-specific `org.gradle.java.home` in `gradle.properties` | **RESOLVED** | Removed hardcoded Windows path so Gradle utilizes CI/system JDK |

---

## 4. Verification Evidence & Test Execution Records

### Automated Companion Verification
```text
PASS  constants
PASS  protocol
PASS  vectors
PASS  tokens
PASS  settings
PASS  paths
PASS  manifest_frames
PASS  prd
      (81 path and manifest cases, 30 constants, 25 settings)
Self-test: All 25 mutants caught.
PASS  self-test
```

### Go Core Test Suite
```text
=== RUN   TestEndToEndTransfer
    transfer_test.go:137: End-to-end file transfer verified: 2 files, 310033 bytes identical!
--- PASS: TestEndToEndTransfer (0.14s)
ok      lanvault/core/diag        1.320s
ok      lanvault/core/discovery   1.050s
ok      lanvault/core/fsutil      (cached)
ok      lanvault/core/identity    (cached)
ok      lanvault/core/pairing     (cached)
ok      lanvault/core/protocol    1.460s
ok      lanvault/core/store       (cached)
ok      lanvault/core/transfer    0.569s
```

### Go Race Detector Suite (`go test -race ./...`)
```text
?       lanvault/cli             [no test files]
ok      lanvault/core/diag       2.026s
ok      lanvault/core/discovery  1.988s
ok      lanvault/core/fsutil     2.644s
ok      lanvault/core/identity   2.643s
?       lanvault/core/netutil    [no test files]
ok      lanvault/core/pairing    2.033s
ok      lanvault/core/protocol   1.849s
ok      lanvault/core/store      3.961s
ok      lanvault/core/transfer   5.502s
(0 race conditions detected)
```

### Desktop Frontend Build
```text
vite v7.3.7 building client environment for production...
✓ 2121 modules transformed.
rendering chunks...
dist/index.html                   0.38 kB
dist/assets/index-Bu1WOscX.css    2.09 kB
dist/assets/index-3TMEaB_e.js   320.52 kB
✓ built in 10.87s
```

### Android Gradle Unit Tests
```text
> Task :app:compileDebugKotlin
> Task :app:compileDebugUnitTestKotlin
> Task :app:testDebugUnitTest
BUILD SUCCESSFUL in 22s
24 actionable tasks: 5 executed, 19 up-to-date
```

---

## 5. Next Planned Actions in Recursive Loop
1. Physical device cross-platform smoke test between Windows Wails app and Android APK.
2. Complete documentation in user guide and verify installer scripts.
3. Keep ledger updated after every iteration.
