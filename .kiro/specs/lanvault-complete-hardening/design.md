# LANVault Complete Hardening - Design
## Technical Architecture & Fix Strategies

**Project**: LANVault Production Hardening  
**Phase**: Design & Root Cause Analysis  
**Date**: November 2026

---

## 1. System Architecture Overview

### 1.1 Current Architecture (Verified Working)

```
┌─────────────────────────────────────────────────────────────────┐
│                     Windows Desktop (Wails)                      │
├──────────────┬─────────────────────┬────────────────────────────┤
│   React UI   │   Wails Bridge      │   Go Backend               │
│   (TypeScript│   (JS ↔ Go)         │   - Identity               │
│    + Vite)   │                     │   - Discovery (UDP Beacon) │
│              │                     │   - Pairing Manager        │
│              │                     │   - Transfer Engine        │
│              │                     │   - Store (SQLite)         │
└──────────────┴─────────────────────┴────────────────────────────┘
                         │
                         │ TCP/TLS 1.3 (Port 47800)
                         │ UDP Broadcast (Port 47801)
                         │
┌─────────────────────────────────────────────────────────────────┐
│                    Android App (Kotlin)                          │
├──────────────┬─────────────────────┬────────────────────────────┤
│ Jetpack      │ TransferForeground  │  Transfer Engine           │
│ Compose UI   │ Service (dataSync)  │  - Identity (Keystore)     │
│ (Material 3) │                     │  - Discovery (NsdManager?) │
│              │                     │  - Protocol Handler        │
│              │                     │  - PathPlanner             │
│              │                     │  - VaultDatabase (Room)    │
└──────────────┴─────────────────────┴────────────────────────────┘
```

### 1.2 Protocol Stack (Wire Protocol v1.1)

```
Application Layer: OFFER → MANIFEST → ACCEPT → DATA → DONE
    ↓
Control Frames (JSON): HELLO, OFFER, ACCEPT, REJECT, ERROR, etc.
Data Frames (Binary): [file_idx][chunk_idx][data][SHA-256]
    ↓
Transport Layer: Multiple TCP connections over mutual TLS 1.3
    - 1 control connection (bidirectional)
    - N data connections (default 4)
    ↓
Security: ECDSA P-256 self-signed certificates + fingerprint pinning
    ↓
Network: Local WiFi, Hotspot, mDNS/UDP discovery
```

---

## 2. Root Cause Analysis of Identified Issues

### Issue PC-01: Android Transfer History Persistence Race

**Root Cause**:
- `TransferForegroundService.kt` has three async callbacks: `onProgress`, `onCompleted`, `onError`
- Each calls `db.updateTransferProgress()` or `db.recordTransfer()` without synchronization
- Android Room doesn't automatically synchronize writes from different coroutine contexts
- Rapid completion events (e.g., small file transfer finishing in <100ms) may cause:
  1. `onProgress` writes partial state
  2. `onCompleted` writes final state
  3. Database transaction ordering not guaranteed
  4. Final persisted state may show 0% progress despite completion

**Fix Strategy**:
1. **Single Source of Truth**: Transfer state managed by `TransferEngine`, not service
2. **Atomic State Transitions**: Use `@Transaction` annotation on Room DAO methods
3. **State Machine**: Enforce valid state transitions (Pending → Active → Completed)
4. **Synchronization**: Protect database writes with coroutine mutex
5. **Idempotence**: Completion callbacks idempotent (safe to call multiple times)

**Implementation**:
```kotlin
// VaultDatabase.kt
class TransferStateManager(private val db: VaultDatabase) {
    private val mutex = Mutex()
    
    @Transaction
    suspend fun transitionTo(transferId: String, newState: String, updates: TransferUpdate) = mutex.withLock {
        val current = db.getTransfer(transferId)
        if (!isValidTransition(current?.state, newState)) {
            throw IllegalStateException("Invalid transition: ${current?.state} → $newState")
        }
        db.updateTransfer(transferId, newState, updates)
    }
    
    private fun isValidTransition(from: String?, to: String): Boolean {
        return when (from to to) {
            null to "pending",
            "pending" to "active",
            "active" to "paused",
            "active" to "completed",
            "active" to "failed",
            "paused" to "active",
            "paused" to "cancelled" -> true
            else -> false
        }
    }
}
```

---

### Issue PC-02: QR Pairing Token Regeneration Timing

**Root Cause**:
- Desktop `GetPairingQR()` creates fresh token on every call (correct for security)
- Android caches QR bitmap and shows 120s countdown
- Android countdown is UI-only timer, disconnected from actual token expiry in `PairingManager`
- If Android screen rotates or app is backgrounded, countdown resets but QR token unchanged
- Token expires server-side before UI countdown reaches zero

**Fix Strategy**:
1. **Token Lifecycle Management**: Pairing manager owns token and expiry timestamp
2. **Synchronized Refresh**: UI polls pairing manager for remaining TTL
3. **Auto-Regeneration**: When TTL <10s, automatically generate new token and QR
4. **Event-Driven**: Pairing manager emits events on token creation/expiry
5. **Atomic QR Generation**: QR contains token + expiry timestamp; backend validates both

**Implementation**:
```kotlin
// Android: PairingViewModel.kt
class PairingViewModel : ViewModel() {
    private val pairingManager = PairingManager()
    val qrUri = MutableStateFlow<String?>(null)
    val remainingSeconds = MutableStateFlow(0L)
    
    init {
        viewModelScope.launch {
            while (isActive) {
                val ttl = pairingManager.getRemainingTTL()
                remainingSeconds.value = ttl
                if (ttl < 10) {
                    regenerateQR()
                }
                delay(1000)
            }
        }
    }
    
    private suspend fun regenerateQR() {
        val token = pairingManager.createNewToken()
        qrUri.value = generatePairingURI(token)
    }
}
```

**Desktop (Go)**:
```go
// desktop/app.go
func (a *App) GetPairingQRWithTTL() (map[string]any, error) {
    tok, err := a.pairingMgr.CreateNewToken()
    if err != nil {
        return nil, err
    }
    ips := netutil.GetLocalIPStrings()
    uri := pairing.GenerateURI(protocol.ProtocolMajor, a.id.Fingerprint, ips, protocol.DefaultPort, a.deviceName, tok)
    return map[string]any{
        "uri": uri,
        "ttl_seconds": protocol.PairTokenTTLSec,
        "created_at": time.Now().Unix(),
    }, nil
}
```

---

### Issue PC-03: Windows Event Emission Verification

**Root Cause** (partially fixed by F16):
- `acceptLoop()` returns `nil` error for successful pairing (now sends `pairing:success`)
- Returns `ErrTransferDeclined` for rejected offers (now sends `transfer:declined`)
- Returns `ErrTransferCancelled` for cancellations (now sends `transfer:cancelled`)
- Generic `transfer:completed` event may still fire in edge cases

**Fix Strategy**:
1. **Event Type Enum**: Define explicit event types instead of error-based routing
2. **Type Safety**: Each event carries structured data, not just strings
3. **Testing**: Add integration test that verifies correct event for each scenario
4. **Documentation**: Document event contract for frontend developers

**Implementation**:
```go
type TransferEventType string

const (
    EventTransferStarted   TransferEventType = "transfer:started"
    EventTransferProgress  TransferEventType = "transfer:progress"
    EventTransferCompleted TransferEventType = "transfer:completed"
    EventTransferFailed    TransferEventType = "transfer:failed"
    EventTransferCancelled TransferEventType = "transfer:cancelled"
    EventTransferDeclined  TransferEventType = "transfer:declined"
    EventPairingSuccess    TransferEventType = "pairing:success"
    EventPairingFailed     TransferEventType = "pairing:failed"
)

type TransferEvent struct {
    Type      TransferEventType `json:"type"`
    TransferID string           `json:"transfer_id,omitempty"`
    Message    string           `json:"message"`
    Metadata   map[string]any   `json:"metadata,omitempty"`
}

// In acceptLoop:
if errors.Is(err, transfer.ErrPairingSuccess) {
    runtime.EventsEmit(a.ctx, string(EventPairingSuccess), TransferEvent{
        Type: EventPairingSuccess,
        Message: "Device paired successfully",
        Metadata: map[string]any{"peer_fp": peerFP},
    })
} else if errors.Is(err, transfer.ErrTransferDeclined) {
    runtime.EventsEmit(a.ctx, string(EventTransferDeclined), TransferEvent{
        Type: EventTransferDeclined,
        Message: "Transfer declined by user",
    })
} else if err != nil {
    runtime.EventsEmit(a.ctx, string(EventTransferFailed), TransferEvent{
        Type: EventTransferFailed,
        Message: err.Error(),
    })
} else {
    // Only emit transfer:completed if we actually completed a transfer
    // (nil error with no sentinel means HandleIncomingControl returned normally)
    runtime.EventsEmit(a.ctx, string(EventTransferCompleted), TransferEvent{
        Type: EventTransferCompleted,
        Message: "Transfer completed successfully",
    })
}
```

---

### Issue PC-04: Android Foreground Service API 35+ Timeout

**Root Cause**:
- Android 15 (API 35) introduces 6-hour rolling limit for `dataSync` foreground services when app is backgrounded
- `onTimeout()` implemented but not tested with actual timeout
- User may not understand why service stopped mid-transfer
- No UI notification or recovery guidance

**Fix Strategy**:
1. **User-Initiated Transfers**: Exempt from timeout if user manually starts/resumes
2. **Service Lifecycle Tracking**: Log time spent in foreground vs background
3. **Proactive Warning**: Show notification at 5.5 hours warning of impending timeout
4. **Graceful Degradation**: Save state, show persistent notification with "Resume" action
5. **Documentation**: README explains limitation and workarounds

**Implementation**:
```kotlin
// TransferForegroundService.kt
private var backgroundStartTime: Long? = null
private var foregroundDuration = 0L

override fun onTimeout(startId: Int, fgsType: Int) {
    // Save progress and notify user
    activeProgress?.let { progress ->
        db.updateTransferProgress(progress.transferId, progress.filesDone, progress.bytesAcked, "paused_timeout")
        
        // Show persistent notification
        val resumeIntent = Intent(this, TransferForegroundService::class.java).apply {
            action = ACTION_RESUME_TRANSFER
            putExtra("transfer_id", progress.transferId)
        }
        val resumePending = PendingIntent.getService(this, 3, resumeIntent, PendingIntent.FLAG_IMMUTABLE)
        
        val notif = NotificationCompat.Builder(this, CHANNEL_SERVICE)
            .setContentTitle("Transfer Paused")
            .setContentText("Android limited background transfer time. Tap Resume to continue.")
            .setSmallIcon(android.R.drawable.stat_sys_warning)
            .addAction(android.R.drawable.ic_media_play, "Resume", resumePending)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .build()
        
        val nm = getSystemService(NotificationManager::class.java)
        nm.notify(NOTIFICATION_ID_TIMEOUT, notif)
    }
    
    stopSelf(startId)
}

// Track foreground vs background time
override fun onTaskRemoved(rootIntent: Intent?) {
    backgroundStartTime = System.currentTimeMillis()
    super.onTaskRemoved(rootIntent)
}

override fun onTaskAdded() {
    backgroundStartTime?.let {
        foregroundDuration += System.currentTimeMillis() - it
        backgroundStartTime = null
    }
}
```

---

### Issue PC-05: Destination Path Collision with Temporary Files

**Root Cause**:
- `PathPlanner` checks for collisions among final names: `file.txt`, `file (1).txt`, etc.
- Does not check for existing `.lvpart` temporary files
- Scenario:
  1. Transfer `file.txt` → creates `file.txt.abc123.lvpart`
  2. Transfer interrupted
  3. Second transfer of different `file.txt` → planner assigns `file (1).txt`
  4. But `file (1).txt.abc123.lvpart` might also exist from a third transfer
  5. Finalization may fail or overwrite wrong temp file

**Fix Strategy**:
1. **Comprehensive Namespace Check**: Reserve both `{name}` and `{name}.{tid}.lvpart`
2. **Conflict Resolution Order**:
   - Check final name exists
   - Check any `.lvpart` variant exists
   - Increment number until both are free
3. **Atomic Reservation**: Mark name as reserved before any file I/O
4. **Test Case**: Concurrent transfers of same-named files

**Implementation**:
```kotlin
// PathPlanner.kt
fun reserveDestination(baseName: String, transferId: String, existingFiles: Set<String>, existingParts: Set<String>): String {
    var candidate = baseName
    var n = 0
    
    while (true) {
        val finalName = if (n == 0) candidate else numberedName(candidate, n)
        val partName = "$finalName.$transferId.lvpart"
        
        if (!existingFiles.contains(finalName) && !existingParts.contains(partName)) {
            // Reserve both names
            existingFiles.add(finalName)
            existingParts.add(partName)
            return finalName
        }
        n++
        if (n > 9999) throw IOException("Cannot reserve unique name for $baseName after 10000 attempts")
    }
}
```

---

### Issue PC-06: Android Discovery Visibility Parity

**Root Cause**:
- Windows `BeaconBroadcaster` now respects visibility setting (F17)
- Android `Discovery.kt` always broadcasts regardless of visibility
- Inconsistent behavior: Android "hidden" but still discoverable

**Fix Strategy**:
1. **Visibility State Management**: Android tracks visibility setting from DB
2. **Conditional Broadcast**: Suppress mDNS/beacon when visibility = "hidden"
3. **Listener Remains Active**: Can still receive incoming connections (listening != broadcasting)
4. **UI Consistency**: Android and Windows settings have same effect

**Implementation**:
```kotlin
// Discovery.kt (Android)
class DiscoveryService(
    private val context: Context,
    private val deviceId: String,
    private val deviceNameProvider: () -> String,
    private val visibilityProvider: () -> String
) {
    fun start() {
        updateVisibility()
    }
    
    fun updateVisibility() {
        val visibility = visibilityProvider()
        when (visibility.lowercase()) {
            "hidden" -> {
                stopBroadcasting()
            }
            "contacts_only", "everyone" -> {
                startBroadcasting()
            }
        }
    }
    
    private fun startBroadcasting() {
        // mDNS registration
        // UDP beacon
    }
    
    private fun stopBroadcasting() {
        // Stop mDNS
        // Stop UDP beacon
        // Keep listener active for incoming
    }
}
```

---

## 3. P1 Feature Design

### 3.1 Pause/Resume UI Controls (FR-P1-01)

**State Machine**:
```
Active ──[user clicks Pause]──> Paused ──[user clicks Resume]──> Active
   │                              │
   └──[connection lost]──> Interrupted ──[auto-reconnect]──> Active (resumed)
```

**UI Components**:
- **Desktop**: Pause/Resume buttons in TransfersTab transfer row
- **Android**: FloatingActionButton on transfer progress screen
- **Notification**: Android notification action buttons

**Protocol Interaction**:
```
User clicks Pause
  → UI sends pause request to backend
  → Backend sends PAUSE frame to peer
  → Sender stops sending new chunks
  → In-flight chunks complete
  → UI shows "Paused" state

User clicks Resume
  → UI sends resume request
  → Backend sends RESUME frame
  → Sender resumes from window state
  → UI shows "Active" state
```

**Implementation Locations**:
- Desktop: `desktop/frontend/src/components/TransfersTab.tsx` (add buttons)
- Desktop: `desktop/app.go` (add `PauseTransfer()`, `ResumeTransfer()` methods)
- Android: `MainScreen.kt` (add FAB)
- Android: `TransferEngine.kt` (expose pause/resume methods)

---

### 3.2 Cancel Transfer with Cleanup (FR-P1-02)

**User Flow**:
```
1. User clicks "Cancel" on active transfer
2. Dialog appears:
   "Cancel transfer?"
   "Keep partial files for later resume?"
   [Keep Files] [Delete Files] [Continue Transfer]
3. If [Delete Files]:
   - Send CANCEL frame to peer
   - Delete .lvpart files
   - Delete chunk bitmaps from DB
   - Mark transfer as "Cancelled (cleaned)"
4. If [Keep Files]:
   - Send CANCEL frame to peer
   - Preserve .lvpart files
   - Keep chunk bitmaps
   - Mark transfer as "Cancelled (resumable)"
```

**Implementation**:
```typescript
// Desktop: TransfersTab.tsx
async function handleCancel(transferId: string) {
    const keepFiles = await showDialog({
        title: "Cancel Transfer",
        message: "Do you want to keep partial files for later resume?",
        buttons: ["Delete Files", "Keep Files", "Continue Transfer"]
    });
    
    if (keepFiles === 2) return; // Continue Transfer
    
    await CancelTransfer(transferId, keepFiles === 1);
}
```

```go
// Desktop: app.go
func (a *App) CancelTransfer(transferID string, keepPartials bool) error {
    // Send CANCEL frame
    // If !keepPartials, delete .lvpart files
    // Update DB state
}
```

---

### 3.3 Manual Retry After Interruption (FR-P1-03)

**UI Indication**:
- Interrupted/Failed transfers show "Retry" button in history
- Button disabled if source files no longer exist
- Tooltip: "Resume this transfer from where it stopped"

**Implementation Strategy**:
1. **Retry Button Condition**: `state IN ("Interrupted", "Failed") AND manifest_hash != NULL`
2. **Source Validation**: Before retry, verify source files unchanged (size + mtime)
3. **Resume Logic**: Reuse existing transfer_id, retrieve saved bitmap
4. **UI Feedback**: Show "Retrying..." spinner during reconnection

---

## 4. Testing Strategy

### 4.1 Automated Testing Enhancement

**New Test Suites Needed**:

1. **Android Transfer State Machine Test** (`TransferStateTest.kt`):
```kotlin
@Test
fun `rapid completion does not lose metadata`() {
    val db = VaultDatabase(context)
    val stateManager = TransferStateManager(db)
    
    // Simulate rapid state changes
    runBlocking {
        launch { stateManager.transitionTo("t1", "active", updates1) }
        launch { stateManager.transitionTo("t1", "completed", updates2) }
    }
    
    // Verify final state
    val final = db.getTransfer("t1")
    assertEquals("completed", final.state)
    assertEquals(updates2.completedBytes, final.completedBytes)
}
```

2. **Desktop Event Emission Test** (`app_test.go`):
```go
func TestAcceptLoopEmitsCorrectEvents(t *testing.T) {
    // Mock transfer scenarios
    // Verify correct event type for each
}
```

3. **Path Collision with Temp Files Test**:
```kotlin
@Test
fun `path planner reserves both final and temp names`() {
    val existing = setOf("file.txt.abc.lvpart")
    val planned = planDestination("file.txt", "def", existing)
    assertEquals("file (1).txt", planned)
}
```

### 4.2 Physical Device Test Matrix

**Required Hardware**: 1 Windows PC + 1 Android device (API 29+)

**Test Scenarios** (10 required):

| # | Scenario | Network | Size | Expected Result |
|---|----------|---------|------|-----------------|
| 1 | Small text file | WiFi router | 1 KB | Complete in <1s, SHA-256 match |
| 2 | Photo (JPEG) | WiFi router | 5 MB | Complete, metadata preserved |
| 3 | Video (MP4) | WiFi router | 100 MB | Complete, playable |
| 4 | Large file | WiFi router | 1 GB | >85% TCP throughput, ETA accurate |
| 5 | Folder (nested) | WiFi router | 50 MB | Structure preserved exactly |
| 6 | Multiple files | WiFi router | 100 files | All transferred, correct order |
| 7 | Interrupted transfer | WiFi → disconnect → WiFi | 500 MB | Resume from last chunk |
| 8 | Hotspot transfer | Phone hotspot | 50 MB | Complete without internet |
| 9 | Bidirectional | WiFi router | 10 MB each | Both transfers succeed |
| 10 | Duplicate filename | WiFi router | 1 MB | Second file named `file (1).txt` |

**Verification for Each**:
```bash
# On receiver after completion
sha256sum <received_file>
# Compare with sender's hash
```

---

## 5. Deployment Architecture

### 5.1 Windows Installer Design

**Technology**: Wails bundled with NSIS or WiX Toolset

**Installer Flow**:
1. Welcome screen with app icon and branding
2. License agreement (if required)
3. Installation directory selection (default: `C:\Program Files\LANVault`)
4. WebView2 runtime check:
   - If missing, bundle Microsoft Evergreen Standalone Installer
   - Install WebView2 silently
5. Create shortcuts: Start Menu + Desktop
6. Firewall rule registration (Windows Firewall):
   - Rule name: "LANVault File Transfer"
   - Allow inbound TCP 47800 on Private networks
   - Require user confirmation on Public networks
7. Installation progress
8. Finish screen with "Launch LANVault" checkbox

**Build Command**:
```bash
cd desktop
wails build -clean -platform windows/amd64 -nsis
```

**Post-Build**:
- Sign executable with code signing certificate (optional but recommended)
- Test on clean Windows 10 and Windows 11 VMs
- Verify uninstaller removes all files and registry keys

---

### 5.2 Android Release Build

**Configuration** (`android/app/build.gradle.kts`):
```kotlin
android {
    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            signingConfig = signingConfigs.getByName("release")
        }
    }
    
    signingConfigs {
        create("release") {
            // Use environment variables or local.properties
            storeFile = file(System.getenv("KEYSTORE_PATH") ?: "../release.keystore")
            storePassword = System.getenv("KEYSTORE_PASSWORD")
            keyAlias = System.getenv("KEY_ALIAS")
            keyPassword = System.getenv("KEY_PASSWORD")
        }
    }
}
```

**ProGuard Rules** (`proguard-rules.pro`):
```
# Keep Room database
-keep class * extends androidx.room.RoomDatabase
-keep @androidx.room.Entity class *
-dontwarn androidx.room.**

# Keep Kotlin coroutines
-dontwarn kotlinx.coroutines.**

# Keep protocol classes (JSON serialization)
-keep class com.example.lanvault.protocol.** { *; }

# Keep identity certificate classes
-keep class com.example.lanvault.crypto.** { *; }
```

**Build Command**:
```bash
cd android
./gradlew assembleRelease
```

**Output**: `android/app/build/outputs/apk/release/app-release.apk`

**Distribution**: Google Play Store or direct APK download

---

## 6. Performance Optimization Strategies

### 6.1 Throughput Optimization

**Current**: 4 parallel streams, 4 MiB chunks, window of 8 chunks per stream

**Tuning Parameters**:
- **Chunk Size**: Test 1 MiB, 2 MiB, 4 MiB, 8 MiB on different network conditions
- **Stream Count**: Test 2, 4, 6, 8 streams
- **Window Size**: Test window of 4, 8, 16 chunks

**Profiling**:
```go
// Add metrics to sender.go
var (
    totalChunksSent     atomic.Int64
    totalBytesOnWire    atomic.Int64
    totalAckWaitTime    atomic.Int64
    sendDuration        time.Duration
)

// Log at end:
fmt.Printf("Throughput: %.2f MB/s\n", float64(totalBytesOnWire.Load()) / sendDuration.Seconds() / 1e6)
fmt.Printf("Avg ACK wait: %d ms\n", totalAckWaitTime.Load() / totalChunksSent.Load())
```

### 6.2 Discovery Latency Optimization

**Current**: UDP beacon every 2s, peer expiry after 3 missed intervals (6s)

**Improvements**:
- **Initial Burst**: Send 3 beacons in first second after app launch
- **Adaptive Interval**: Increase to 5s after first peer found
- **Background Mode**: Reduce to 10s when app backgrounded

---

## 7. Error Recovery Patterns

### 7.1 Network Interruption Recovery

**Scenario**: WiFi disconnects during transfer

**Recovery Strategy**:
1. **Detection**: TCP read timeout after 15s (DEAD_PEER_TIMEOUT_MS)
2. **Sender Action**:
   - Close all connections
   - Enter "Retrying..." state
   - Exponential backoff: 1s, 2s, 4s, 8s, ... up to 30s
   - Retry for up to 5 minutes
3. **Receiver Action**:
   - Flush and close all writers
   - Persist current bitmap state
   - Close connections
   - Wait for reconnection (passive)
4. **Reconnection**:
   - Sender initiates new control connection
   - Sends same OFFER + MANIFEST
   - Receiver responds with updated `have` bitmap
   - Transfer continues from last ACK

**UI Indication**:
- Desktop: "Connection lost, retrying..." with countdown
- Android: Persistent notification: "Transfer paused - waiting for connection"

---

### 7.2 Source File Changed Recovery

**Scenario**: User modifies source file during transfer

**Detection**:
```go
// sender.go: openSource()
info, err := f.Stat()
if info.Size() != it.Entry.Size {
    return nil, fmt.Errorf("source file size changed for %s", it.Entry.Path)
}
if it.Entry.MtimeMs != 0 && info.ModTime().UnixMilli() != it.Entry.MtimeMs {
    return nil, fmt.Errorf("source file mtime changed for %s", it.Entry.Path)
}
```

**Recovery**:
- Send ERROR frame with code `FILE_CHANGED`
- Mark transfer as "Failed"
- UI message: "Source file was modified during transfer. Please retry with current file version."
- User must initiate new transfer (new manifest hash)

---

## 8. Documentation Updates

### 8.1 README.md Enhancements

**Add Sections**:
- System Requirements (Windows 10+, Android 10+)
- Installation Instructions (installer steps)
- Quick Start Guide (pair → send → receive)
- Troubleshooting (common issues)
- Known Limitations (Android 15 timeout, FAT32 limits)

### 8.2 User Guide

**Create** `docs/USER_GUIDE.md`:
- How to pair devices
- How to send files and folders
- How to pause and resume transfers
- Understanding transfer status indicators
- Managing trusted devices
- Privacy and security FAQ

### 8.3 Developer Documentation

**Create** `docs/DEVELOPER_GUIDE.md`:
- Building from source (Windows + Android)
- Running tests
- Protocol specification reference
- Contributing guidelines
- Code review checklist

---

## 9. Success Metrics

### 9.1 Quantitative Metrics

- **Defect Density**: <0.5 defects per KLOC (currently 19 fixed / ~7 KLOC = 0.27, acceptable)
- **Test Coverage**: >80% line coverage on core packages (identity, protocol, transfer)
- **Crash Rate**: <0.1% of sessions (target: 0 crashes in beta)
- **Throughput**: >85% of raw TCP on same link
- **Discovery Time**: <3s median on home WiFi

### 9.2 Qualitative Metrics

- **User Feedback**: "Easy to use," "Fast," "Reliable"
- **First-Time Success**: >90% complete first transfer within 5 minutes
- **Error Clarity**: Users understand and resolve errors without support
- **Trust**: Users feel confident files are secure and private

---

## 10. Risk Mitigation

### Risk R-01: Physical Testing Unavailable
**Impact**: High (cannot verify real-world performance)  
**Mitigation**: 
- Thorough automated integration tests
- Emulator testing (Android Emulator + localhost)
- Public beta with telemetry-free feedback form

### Risk R-02: Code Signing Certificate Cost
**Impact**: Medium (unsigned Windows app shows SmartScreen warning)  
**Mitigation**:
- Document workaround for users
- Consider open-source code signing services
- Build reputation over time (warning disappears after downloads threshold)

### Risk R-03: Android Fragmentation
**Impact**: Medium (different behavior on OEM skins)  
**Mitigation**:
- Target vanilla Android (AOSP)
- Test on Samsung, Xiaomi, Pixel if possible
- Document known OEM-specific issues

---

**Document Status**: COMPLETE  
**Next**: Create `tasks.md` with atomic implementation checklist
