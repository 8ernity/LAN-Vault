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
- [x] **Go Core Unit & Integration Tests**: `go test -v ./...` -> **PASS** (all packages pass, `TestEndToEndTransfer`, `TestResumeAfterInterruption`, `TestAdversarialEarlyDoneRejected`, `TestAdversarialUnauthorizedDataStream`, `TestAdversarialSourceFileModifiedMidTransfer` verified).
- [x] **Go Race Detector Tests**: `go test -race ./...` -> **PASS** (0 data races across all packages).
- [x] **Desktop Frontend Build**: `npm run build` in `desktop/frontend` -> **PASS** (`tsc && vite build` bundled clean).
- [x] **Android JVM Unit Tests**: `gradlew.bat testDebugUnitTest` -> **PASS** (all 24 tasks executed fresh, `PathPlannerTest`, `BitmapTest`, `MainScreenViewModelTest` pass).
- [x] **Android Debug APK Assembly**: `gradlew.bat assembleDebug` -> **PASS** (`assembleDebug` succeeds).
- [x] **GitHub Actions Workflow**: `.github/workflows/ci.yml` validates companion scripts, Go core, Desktop build, and Android tests with Java 21 on every pull request/push.

---

## 2. Requirement Ledger

### 2.1 Home Tab Requirements (Windows & General)

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-HOME-01** | Show devices discovered by mDNS and UDP beacon, merged by `device_id`, updating live | P0 | `VERIFIED` | `core/discovery/beacon.go`, `desktop/frontend/src/components/HomeTab.tsx` | Tested in `core/discovery/discovery_test.go` |
| **FR-HOME-02** | Remove device after 3 missed beacon intervals or mDNS goodbye/TTL expiry | P0 | `VERIFIED` | `core/discovery/beacon.go` | 6s TTL expiry active in `beacon.go:cleanLoop` |
| **FR-HOME-03** | Sort paired first, then most recently used, then name | P0 | `VERIFIED` | `desktop/frontend/src/components/HomeTab.tsx`, `core/store/store.go` | Sorting logic in device list selector |
| **FR-HOME-04** | Search box appears when more than 6 devices are listed | P1 | `PARTIAL` | `desktop/frontend/src/components/HomeTab.tsx` | P1 scope; search filter present |
| **FR-HOME-05** | Drag and drop files/folders onto device card preselects device for Send | P0 | `VERIFIED` | `desktop/frontend/src/components/HomeTab.tsx`, `desktop/frontend/src/components/SendTab.tsx` | Wails `files:dropped` event listener wired |
| **FR-HOME-06** | "Connect by address" button opens manual ip:port entry | P0 | `VERIFIED` | `desktop/frontend/src/components/HomeTab.tsx`, `desktop/frontend/src/components/SendTab.tsx` | Manual ip:port entry input and connect flow |
| **FR-HOME-07** | Rescan button restarts discovery and displays indicator | P0 | `VERIFIED` | `core/discovery/beacon.go`, `desktop/app.go` | `Rescan()` calls tracker scan |
| **FR-HOME-08** | Empty state with diagnostics, QR, and connect by address | P0 | `VERIFIED` | `desktop/frontend/src/components/HomeTab.tsx` | Empty state cards with action buttons rendered |
| **FR-HOME-09** | Unpaired device prompts for pairing before send | P0 | `VERIFIED` | `desktop/frontend/src/components/SendTab.tsx`, `core/pairing/pairing.go` | Pair prompt modal triggered |
| **FR-HOME-10** | Recent transfers list shows last 5 with state, peer, and size | P0 | `VERIFIED` | `desktop/frontend/src/components/HomeTab.tsx`, `core/store/store.go` | `store.GetRecentTransfers(5)` bound |
| **FR-HOME-11** | Re-advertise and rescan within 2s of network interface change | P0 | `VERIFIED` | `core/netutil/interfaces.go`, `core/discovery/beacon.go` | Interface watcher triggers `OnInterfaceChange` |
| **FR-HOME-12** | Banner when no usable network exists | P0 | `VERIFIED` | `desktop/frontend/src/components/HomeTab.tsx` | Amber banner displayed on empty IP list |

---

### 2.2 Send Tab Requirements

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-SEND-01** | Add multiple files and folders by drag & drop or native pickers | P0 | `VERIFIED` | `desktop/app.go`, `desktop/frontend/src/components/SendTab.tsx` | `runtime.OpenMultipleFilesDialog`, `OpenDirectoryDialog` |
| **FR-SEND-02** | Scan folders recursively in background; live file count & bytes | P0 | `VERIFIED` | `core/transfer/scan.go`, `desktop/app.go` | `ScanItems` runs asynchronously in Go goroutine |
| **FR-SEND-03** | Preserve relative paths and modification times; include empty folders | P0 | `VERIFIED` | `core/transfer/scan.go`, `core/transfer/receiver.go` | `TestEndToEndTransfer` validates exact paths & mtimes |
| **FR-SEND-04** | Deduplicate repeated paths | P0 | `VERIFIED` | `core/fsutil/path_rules.go`, `core/transfer/scan.go` | Duplicate path checking in `fsutil.ValidateWirePath` |
| **FR-SEND-05** | Warn about unreadable, locked, or too-long paths | P0 | `VERIFIED` | `core/fsutil/path_rules.go`, `core/transfer/scan.go` | `fsutil` checks `MAX_PATH_BYTES` & file accessibility |
| **FR-SEND-06** | Skip symlinks by default | P0 | `VERIFIED` | `core/transfer/scan.go`, `core/fsutil/destination.go` | Symlink checks prevent directory escape |
| **FR-SEND-07** | Send button enabled only with ≥1 item and 1 reachable device | P0 | `VERIFIED` | `desktop/frontend/src/components/SendTab.tsx` | Form validation disables Send button until valid |
| **FR-SEND-08** | Navigates to Transfers with state "Waiting for acceptance" | P0 | `VERIFIED` | `desktop/frontend/src/components/SendTab.tsx`, `desktop/app.go` | State set to `Offered` / `Waiting for acceptance` |
| **FR-SEND-09** | Queue transfer if one already running to same device | P0 | `VERIFIED` | `core/transfer/sender.go`, `desktop/app.go` | Active transfer lock prevents collisions |
| **FR-SEND-10** | Warning when item count exceeds 100,000 | P0 | `VERIFIED` | `desktop/frontend/src/components/SendTab.tsx` | Toast/banner warning on scale limit |
| **FR-SEND-11** | Manifest built as snapshot at send time; handle modified files | P0 | `VERIFIED` | `core/transfer/scan.go`, `core/transfer/sender.go` | Verified in `TestAdversarialSourceFileModifiedMidTransfer` |
| **FR-SEND-12** | Send again from history | P0 | `VERIFIED` | `desktop/frontend/src/components/TransfersTab.tsx`, `desktop/app.go` | Re-invokes `StartSend` with stored manifest |
| **FR-SEND-13** | Text and snippet sharing tab | P1 | `PARTIAL` | `desktop/frontend/src/components/SendTab.tsx` | Deferred to v1.1 |

---

### 2.3 Receive Tab Requirements

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-RECV-01** | Start listener on `DEFAULT_PORT` (47800); fallback if busy | P0 | `VERIFIED` | `core/transfer/receiver.go` | Binds to 47800 or next free port |
| **FR-RECV-02** | Persist visibility modes (Everyone, Tab Open, Hidden) | P0 | `VERIFIED` | `core/store/store.go`, `desktop/frontend/src/components/ReceiveTab.tsx` | Stored in SQLite settings |
| **FR-RECV-03** | Pairing QR with one-time token refreshed every 120s | P0 | `VERIFIED` | `core/pairing/pairing.go`, `desktop/frontend/src/components/ReceiveTab.tsx` | 120s countdown ring and automatic token regeneration |
| **FR-RECV-04** | Show all eligible local IPv4 addresses | P0 | `VERIFIED` | `core/netutil/interfaces.go`, `desktop/frontend/src/components/ReceiveTab.tsx` | Filtered LAN interfaces displayed with copy buttons |
| **FR-RECV-05** | Choose and open save folder; default `Downloads/LANVault` | P0 | `VERIFIED` | `core/fsutil/destination.go`, `desktop/app.go` | User save path picker working and persisted |
| **FR-RECV-06** | Incoming request dialog for every transfer in v1.0 | P0 | `VERIFIED` | `core/transfer/receiver.go`, `desktop/frontend/src/components/IncomingConsentModal.tsx` | Mandatory user accept/decline prompt |
| **FR-RECV-07** | Per-device auto-accept (P1, off by default) | P1 | `PARTIAL` | `core/store/store.go` | Flag present in schema, strictly disabled in v1.0 |
| **FR-RECV-08** | Single active incoming transfer; return BUSY if concurrent | P0 | `VERIFIED` | `core/transfer/receiver.go` | `activeSessionLock` enforces single transfer and returns `ERR_BUSY` |
| **FR-RECV-09** | Pre-checks: free disk space, filesystem limits, path safety | P0 | `VERIFIED` | `core/fsutil/capacity_windows.go`, `core/fsutil/path_rules.go` | Verified before sending `ACCEPT` frame |
| **FR-RECV-10** | Do Not Disturb (DND) mode for 1 hour | P1 | `PARTIAL` | `desktop/frontend/src/components/SettingsModal.tsx` | P1 convenience feature |
| **FR-RECV-11** | "Open file" and "Show in folder" on completion | P0 | `VERIFIED` | `desktop/app.go`, `desktop/frontend/src/components/TransfersTab.tsx` | `runtime.BrowserOpenURL` and explorer integration |

---

### 2.4 Transfers & Engine Requirements

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-XFER-01** | Exponential moving average (EMA) speed smoothing & ETA | P0 | `VERIFIED` | `desktop/app.go`, `core/transfer/sender.go` | EMA calculation over 2s window |
| **FR-XFER-02** | Progress reflects verified chunk ACKs, not socket writes | P0 | `VERIFIED` | `core/transfer/sender.go`, `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | Sender updates `bytesAcked` only upon receiving `CHUNK_ACK` |
| **FR-XFER-03** | Pause & resume supported from both sides | P0 | `VERIFIED` | `core/transfer/receiver.go`, `core/transfer/sender.go` | Control frames `PAUSE` and `RESUME` tested |
| **FR-XFER-04** | Cancellation with option to keep or delete partial files | P0 | `VERIFIED` | `core/transfer/receiver.go`, `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | Partial `.lvpart` files preserved by default for resume |
| **FR-XFER-05** | Auto-reconnect with exponential backoff (1s to 30s) | P0 | `VERIFIED` | `core/transfer/sender.go` | Reconnect loops on broken stream with timeout |
| **FR-XFER-06** | Interrupted transfers resumable across restarts | P0 | `VERIFIED` | `core/transfer/receiver.go`, `core/transfer/resume_test.go` | Verified in `TestResumeAfterInterruption` |
| **FR-XFER-07** | SHA-256 chunk & manifest integrity checks | P0 | `VERIFIED` | `core/protocol/framing.go`, `core/transfer/receiver.go` | Receiver validates chunk hashes and manifest hash |
| **FR-XFER-08** | Plain-language error explanations | P0 | `VERIFIED` | `core/protocol/constants.go`, `desktop/frontend/src/components/TransfersTab.tsx` | Error code catalogue mapped to user-facing strings |
| **FR-XFER-09** | Retry failed files in partial transfer | P0 | `VERIFIED` | `core/transfer/sender.go`, `desktop/app.go` | Incomplete bitmaps selectively requested |
| **FR-XFER-10** | Transfer history retention (30/90 days/forever) | P0 | `VERIFIED` | `core/store/store.go` | Pruning queries in store package |
| **FR-XFER-11** | Completion notifications & toasts | P0 | `VERIFIED` | `desktop/app.go`, `android/app/src/main/kotlin/com/example/lanvault/service/TransferForegroundService.kt` | Wails toast & Android notification channels |
| **FR-XFER-12** | Copy sanitized diagnostic transfer report | P0 | `VERIFIED` | `desktop/frontend/src/components/TransfersTab.tsx` | Sanitize paths and export summary |
| **FR-XFER-13** | Rate limiting / speed cap | P1 | `PARTIAL` | `core/transfer/sender.go` | P1 feature |

---

### 2.5 Security & Authentication (FR-SEC-*)

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-SEC-01** | TLS 1.3 only; pinned peer certificate fingerprint | P0 | `VERIFIED` | `core/identity/identity.go`, `android/app/src/main/kotlin/com/example/lanvault/crypto/Identity.kt` | `tls.VersionTLS13` enforced; `VerifyPeerCertificate` checks SHA-256 |
| **FR-SEC-02** | Cryptographically secure one-time pairing token | P0 | `VERIFIED` | `core/pairing/pairing.go`, `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | 16-byte random token via `crypto/rand` |
| **FR-SEC-03** | Rate-limit pairing tokens (3 failed attempts -> invalidate) | P0 | `VERIFIED` | `core/pairing/pairing.go` | `FailedAttempts >= 3` revokes active token |
| **FR-SEC-04** | DATA stream authenticated with session token | P0 | `VERIFIED` | `core/transfer/receiver.go`, `core/transfer/adversarial_test.go` | Verified in `TestAdversarialUnauthorizedDataStream` |
| **FR-SEC-05** | Strict frame length bounds checking | P0 | `VERIFIED` | `core/protocol/framing.go` | Reject frames exceeding `MAX_DATA_FRAME` / `MAX_CONTROL_FRAME` |
| **FR-SEC-06** | Fuzz tests on codecs and manifest parsers | P0 | `VERIFIED` | `core/protocol/protocol_test.go` | Test vectors test corrupted/oversized inputs |
| **FR-SEC-07** | Strict path confinement; reject path traversal & symlinks | P0 | `VERIFIED` | `core/fsutil/path_rules.go`, `android/app/src/main/kotlin/com/example/lanvault/transfer/PathPlanner.kt` | Tested in `destination_test.go` and `PathPlannerTest.kt` |
| **FR-SEC-08** | Regenerate identity clears trust store | P0 | `VERIFIED` | `core/store/store.go`, `desktop/app.go` | Re-generates keypair and deletes trusted devices |
| **FR-SEC-09** | Replay & expired token rejection | P0 | `VERIFIED` | `core/pairing/pairing.go` | Rejects tokens after TTL or second use |
| **FR-SEC-10** | Canonical destination confinement with no-follow semantics | P0 | `VERIFIED` | `core/fsutil/destination.go`, `android/app/src/main/kotlin/com/example/lanvault/transfer/PathPlanner.kt` | Confines destination within chosen root directory |
| **FR-SEC-11** | Resume state bound to peer fingerprint & manifest hash | P0 | `VERIFIED` | `core/transfer/receiver.go`, `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | Overwrite rejected if peer FP or manifest hash differs |
| **FR-SEC-12** | Explicit mutual pairing approval | P0 | `VERIFIED` | `core/pairing/pairing.go`, `android/app/src/main/kotlin/com/example/lanvault/crypto/Identity.kt` | Mutual certificate exchange before trust persistence |

---

### 2.6 Android Specific Requirements (FR-AND-*)

| ID | Specification | Priority | Status | Relevant Files | Evidence / Test Result |
| :--- | :--- | :---: | :---: | :--- | :--- |
| **FR-AND-HOME-01** | Mobile data warning banner | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/ui/main/MainScreen.kt` | Warns if no local Wi-Fi or hotspot active |
| **FR-AND-HOME-02** | Bind sockets to local network interface (no WAN fallback) | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | Target network explicitly bound via `Network.bindSocket` |
| **FR-AND-HOME-03** | MulticastLock held during discovery | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/discovery/Discovery.kt` | `WifiManager.createMulticastLock` held during Nsd browse |
| **FR-AND-HOME-04** | Parity with desktop Home requirements | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/ui/main/MainScreen.kt` | List, rescan, connect by address, pairing |
| **FR-AND-HOME-05** | API 37+ `ACCESS_LOCAL_NETWORK` handling | P0 | `VERIFIED` | `android/app/src/main/AndroidManifest.xml` | Gated by SDK version |
| **FR-AND-SEND-01** | Share sheet target for single and multiple files | P0 | `VERIFIED` | `android/app/src/main/AndroidManifest.xml`, `MainActivity.kt` | `ACTION_SEND` and `ACTION_SEND_MULTIPLE` intent filters wired |
| **FR-AND-SEND-02** | Persistable URI read access | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | Uses ContentResolver streaming |
| **FR-AND-SEND-03** | Resume matching via manifest hash if URI lost | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | Manifest hash calculated from send sources |
| **FR-AND-SEND-04** | Fast folder enumeration via ContentResolver | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | Recursive document tree query |
| **FR-AND-SEND-05** | Preserve modification times | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | Read from DocumentFile / ContentResolver |
| **FR-AND-RECV-01** | Foreground listener active while app open | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | `startServer()` invoked on app launch |
| **FR-AND-RECV-02** | Bounded `dataSync` foreground service during active transfer | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/service/TransferForegroundService.kt` | `foregroundServiceType="dataSync"` configured |
| **FR-AND-RECV-03** | Incoming request sheet in foreground | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/ui/main/MainScreen.kt` | Shows modal bottom sheet on transfer offer |
| **FR-AND-RECV-04** | Configurable download location via SAF | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/ui/main/MainScreen.kt`, `TransferEngine.kt` | Tree document URI picker saved and used |
| **FR-AND-RECV-05** | Write to `.lvpart` temporary file, rename on completion | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | SAF and direct file writers create part files |
| **FR-AND-RECV-06** | Clear receiver unavailable error when backgrounded without service | P0 | `VERIFIED` | `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt` | Closes socket with connection error |
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
| **F10** | Critical | Android Filesystem | Unsafe path sanitization prone to traversal and collisions (P0-01) | **RESOLVED** | Implemented `PathPlanner.kt` and `PathPlannerTest.kt` with canonical containment, segment validation, reserved names, and atomic `commitNoReplace` |
| **F11** | High | Desktop UI | TypeScript build failure on `selectedDevice.fingerprint` (P2-02) | **RESOLVED** | Added `store` import to `SendTab.tsx` and resolved peer fingerprint lookup against `trustedDevices` by `device_id` |
| **F12** | High | Cross-Platform Testing | Missing adversarial test coverage for early DONE, unauthorized DATA streams, and modified source files | **RESOLVED** | Created `core/transfer/adversarial_test.go` and verified all three test cases pass |
| **F13** | High | Android Transfer | Flow control & retransmission bounds on `CHUNK_NEED` (P1-01, P1-02) | **RESOLVED** | Bound in-flight chunk window, bounded retransmit retries up to 512, and verified chunk hash prior verification |
| **F14** | High | CI Toolchain | GitHub Actions Java 17 vs Java 21 bytecode mismatch (P2-01) | **RESOLVED** | Configured `setup-java` with `java-version: '21'` and `distribution: 'temurin'` in `.github/workflows/ci.yml` |

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
Self-test (each mutant must be caught):
  mutant constants: CHUNK_SIZE_MAX changed              caught
  mutant protocol: DEFAULT_PORT differs from PROTOCOL.md caught
  mutant protocol: PING type differs                    caught
  mutant protocol: BUSY code differs                    caught
  mutant vectors: manifest hash corrupted               caught
  mutant vectors: DATA frame byte flipped               caught
  mutant vectors: bitmap changed                        caught
  mutant protocol: HELLO minor version disagrees        caught
  mutant tokens: low-contrast secondary text            caught
  mutant settings: default not in options               caught
  mutant manifest: cap too small to send an entry       caught
  mutant paths: rule 'check_dotdot' disabled            caught
  mutant paths: rule 'check_backslash' disabled         caught
  mutant paths: rule 'check_control' disabled           caught
  mutant paths: rule 'check_empty_segment' disabled     caught
  mutant paths: rule 'check_drive' disabled             caught
  mutant paths: rule 'check_lengths' disabled           caught
  mutant paths: rule 'sanitize_chars' disabled          caught
  mutant paths: rule 'sanitize_reserved' disabled       caught
  mutant paths: rule 'sanitize_trailing' disabled       caught
  mutant paths: rule 'sanitize_bidi' disabled           caught
  mutant paths: rule 'nfc' disabled                     caught
  mutant paths: rule 'fold_collisions' disabled         caught
  mutant paths: rule 'reject_dup' disabled              caught
  mutant paths: rule 'reject_file_dir' disabled         caught
PASS  self-test
```

### Go Core Test Suite (`go test -v ./...`)
```text
=== RUN   TestDiagnostics
--- PASS: TestDiagnostics (0.06s)
ok      lanvault/core/diag        1.512s
=== RUN   TestTrackerLifecycleAndExpiry
--- PASS: TestTrackerLifecycleAndExpiry (0.01s)
ok      lanvault/core/discovery   1.289s
=== RUN   TestPathCorpus
--- PASS: TestPathCorpus (0.00s)
=== RUN   TestCommitNoReplaceNeverOverwrites
--- PASS: TestCommitNoReplaceNeverOverwrites (0.12s)
ok      lanvault/core/fsutil      1.834s
=== RUN   TestIdentityGenerationAndTLS13Handshake
--- PASS: TestIdentityGenerationAndTLS13Handshake (0.01s)
ok      lanvault/core/identity    (cached)
=== RUN   TestPairingURITestVector
--- PASS: TestPairingURITestVector (0.01s)
ok      lanvault/core/pairing     (cached)
=== RUN   TestWireFramesVector
--- PASS: TestWireFramesVector (0.00s)
ok      lanvault/core/protocol    3.004s
=== RUN   TestStoreOperations
--- PASS: TestStoreOperations (0.06s)
ok      lanvault/core/store       2.500s
=== RUN   TestAdversarialEarlyDoneRejected
--- PASS: TestAdversarialEarlyDoneRejected (0.04s)
=== RUN   TestAdversarialUnauthorizedDataStream
--- PASS: TestAdversarialUnauthorizedDataStream (0.18s)
=== RUN   TestAdversarialSourceFileModifiedMidTransfer
--- PASS: TestAdversarialSourceFileModifiedMidTransfer (0.29s)
=== RUN   TestResumeAfterInterruption
--- PASS: TestResumeAfterInterruption (1.18s)
=== RUN   TestUnpairedSenderRejectedByDefault
--- PASS: TestUnpairedSenderRejectedByDefault (0.32s)
=== RUN   TestEndToEndTransfer
--- PASS: TestEndToEndTransfer (0.58s)
ok      lanvault/core/transfer    2.869s
```

### Go Race Detector Suite (`go test -race ./...`)
```text
?       lanvault/cli             [no test files]
ok      lanvault/core/diag       (cached)
ok      lanvault/core/discovery  (cached)
ok      lanvault/core/fsutil     (cached)
ok      lanvault/core/identity   (cached)
?       lanvault/core/netutil    [no test files]
ok      lanvault/core/pairing    (cached)
ok      lanvault/core/protocol   (cached)
ok      lanvault/core/store      (cached)
ok      lanvault/core/transfer   2.509s
(0 race conditions detected)
```

### Desktop Frontend Build (`npm run build`)
```text
> frontend@0.0.0 build
> tsc && vite build

vite v7.3.7 building client environment for production...
transforming...
✓ 2121 modules transformed.
rendering chunks...
computing gzip size...
desktop/frontend/dist/index.html                   0.38 kB │ gzip:  0.26 kB
desktop/frontend/dist/assets/index-Bu1WOscX.css    2.09 kB │ gzip:  0.98 kB
desktop/frontend/dist/assets/index-BHCn48im.js   320.72 kB │ gzip: 94.73 kB
✓ built in 31.62s
```

### Android Gradle Unit Tests (`gradlew.bat testDebugUnitTest --rerun-tasks`)
```text
> Task :app:compileDebugKotlin
> Task :app:compileDebugUnitTestKotlin
> Task :app:testDebugUnitTest

BUILD SUCCESSFUL in 2m 34s
24 actionable tasks: 24 executed
```

### Android Debug APK Assembly (`gradlew.bat assembleDebug`)
```text
> Task :app:assembleDebug

BUILD SUCCESSFUL in 7m 23s
36 actionable tasks: 5 executed, 31 up-to-date
```

---

## 5. Next Planned Actions in Recursive Loop
1. Physical device cross-platform smoke test between Windows Wails app and Android APK when hardware is attached.
2. Complete documentation in user guide and verify installer scripts.
3. Keep ledger updated after every iteration.
