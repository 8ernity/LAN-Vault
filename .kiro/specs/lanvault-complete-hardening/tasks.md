# LANVault Complete Hardening - Implementation Tasks
## Atomic Task Breakdown with Priorities

**Project**: LANVault Production Hardening  
**Sprint**: Complete Audit & Bug Fixing  
**Estimated Effort**: 40-60 hours  
**Date**: November 2026

---

## Task Notation

- **Priority**: P0 (critical) | P1 (important) | P2 (nice-to-have)
- **Status**: 🔴 NOT_STARTED | 🟡 IN_PROGRESS | ✅ DONE | ⏸️ BLOCKED
- **Effort**: Hours estimate
- **Dependencies**: Task IDs that must complete first

---

## Phase A: Critical Bug Fixes (16 hours)

### Task PC-01-A: Android Transfer State Synchronization
**Priority**: P0  
**Effort**: 4h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Create `TransferStateManager.kt` with mutex-protected state transitions
2. Add `@Transaction` annotations to Room DAO methods
3. Replace direct `db.updateTransferProgress()` calls with `stateManager.transitionTo()`
4. Add `isValidTransition()` state machine validation
5. Write unit test: `testRapidCompletionPreservesMetadata()`

**Files to Modify**:
- `android/app/src/main/kotlin/com/example/lanvault/data/VaultDatabase.kt` (new class)
- `android/app/src/main/kotlin/com/example/lanvault/service/TransferForegroundService.kt`
- `android/app/src/test/kotlin/com/example/lanvault/data/TransferStateTest.kt` (new)

**Verification**:
```kotlin
// Run test
./gradlew testDebugUnitTest --tests TransferStateTest
// Should PASS without race conditions
```

---

### Task PC-02-A: QR Token Lifecycle Management (Desktop)
**Priority**: P0  
**Effort**: 3h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Add `GetPairingQRWithTTL()` method to `desktop/app.go`
2. Return structured response: `{uri, ttl_seconds, created_at}`
3. Frontend: Poll `GetPairingQRWithTTL()` every second
4. Auto-regenerate QR when TTL < 10s
5. Display countdown ring UI element

**Files to Modify**:
- `desktop/app.go`
- `desktop/frontend/src/components/ReceiveTab.tsx`

**Verification**:
- Manual: Open Receive tab, watch QR refresh at 120s mark
- Console log should show: "QR regenerated at TTL=9s"

---

### Task PC-02-B: QR Token Lifecycle Management (Android)
**Priority**: P0  
**Effort**: 3h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Create `PairingViewModel.kt` with token lifecycle management
2. Add `remainingSeconds` StateFlow updated every second
3. Auto-regenerate QR when TTL < 10s
4. Bind countdown UI to `remainingSeconds`
5. Add manual refresh button

**Files to Modify**:
- `android/app/src/main/kotlin/com/example/lanvault/ui/PairingViewModel.kt` (new)
- `android/app/src/main/kotlin/com/example/lanvault/ui/main/MainScreen.kt`

**Verification**:
- Manual: Open Receive screen, watch countdown reach 0 and QR refresh

---

### Task PC-03: Windows Event Type Safety
**Priority**: P1  
**Effort**: 2h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Define `TransferEventType` enum in `desktop/app.go`
2. Create `TransferEvent` struct with type, message, metadata
3. Update `acceptLoop()` to emit structured events
4. Update frontend to handle typed events
5. Add integration test: `TestAcceptLoopEmitsCorrectEvents()`

**Files to Modify**:
- `desktop/app.go`
- `desktop/frontend/src/App.tsx` (event listeners)
- `desktop/app_test.go` (new test)

**Verification**:
```bash
cd desktop
go test -v -run TestAcceptLoopEmitsCorrectEvents
# Should PASS
```

---

### Task PC-04: Android Foreground Service Timeout Handling
**Priority**: P1  
**Effort**: 2h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Implement `onTimeout()` notification with Resume action
2. Add `ACTION_RESUME_TRANSFER` intent handler
3. Track foreground vs background duration
4. Show proactive warning at 5.5 hours (optional)
5. Document limitation in README

**Files to Modify**:
- `android/app/src/main/kotlin/com/example/lanvault/service/TransferForegroundService.kt`
- `README.md` (Known Limitations section)

**Verification**:
- Manual: Cannot simulate 6-hour timeout in test
- Code review: Verify notification and resume logic

---

### Task PC-05: Path Collision Temp File Reservation
**Priority**: P1  
**Effort**: 2h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Add `reserveDestination()` method to `PathPlanner.kt`
2. Check both final name and `.lvpart` variant
3. Update collision resolution to check temp files
4. Add test: `testPathPlannerReservesBothFinalAndTemp()`
5. Verify with Go path_rules.go for parity

**Files to Modify**:
- `android/app/src/main/kotlin/com/example/lanvault/transfer/PathPlanner.kt`
- `core/fsutil/path_rules.go` (verify parity)
- `android/app/src/test/kotlin/com/example/lanvault/transfer/PathPlannerTest.kt`

**Verification**:
```bash
./gradlew testDebugUnitTest --tests PathPlannerTest
# testPathPlannerReservesBothFinalAndTemp should PASS
```

---

## Phase B: P1 Feature Implementation (24 hours)

### Task P1-01-A: Pause/Resume UI - Desktop
**Priority**: P1  
**Effort**: 4h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Add `PauseTransfer(transferID string)` method to `desktop/app.go`
2. Add `ResumeTransfer(transferID string)` method
3. Send PAUSE/RESUME frames to peer
4. Add Pause/Resume buttons to `TransfersTab.tsx`
5. Show "Paused" badge on paused transfers

**Files to Modify**:
- `desktop/app.go`
- `desktop/frontend/src/components/TransfersTab.tsx`
- `core/transfer/sender.go` (pause/resume support)

**Verification**:
- Manual: Start transfer, click Pause, verify PAUSE frame sent
- Click Resume, verify transfer continues

---

### Task P1-01-B: Pause/Resume UI - Android
**Priority**: P1  
**Effort**: 4h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Expose `pauseTransfer()` and `resumeTransfer()` in `TransferEngine.kt`
2. Add FloatingActionButton with pause/resume icons to transfer progress screen
3. Add notification action buttons for pause/resume
4. Update progress UI to show "Paused" state
5. Persist paused state in database

**Files to Modify**:
- `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt`
- `android/app/src/main/kotlin/com/example/lanvault/ui/main/MainScreen.kt`
- `android/app/src/main/kotlin/com/example/lanvault/service/TransferForegroundService.kt`

**Verification**:
- Manual: Start transfer, tap Pause FAB, verify chunks stop
- Tap Resume, verify chunks continue

---

### Task P1-02-A: Cancel with Cleanup Dialog - Desktop
**Priority**: P1  
**Effort**: 3h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Add `CancelTransfer(transferID string, keepPartials bool)` to `desktop/app.go`
2. Implement cleanup logic (delete `.lvpart` if !keepPartials)
3. Add cancel button to `TransfersTab.tsx`
4. Show dialog: "Keep partial files for resume?"
5. Update transfer state based on choice

**Files to Modify**:
- `desktop/app.go`
- `desktop/frontend/src/components/TransfersTab.tsx`
- `core/transfer/sender.go` (cleanup logic)

**Verification**:
- Manual: Cancel transfer, choose Delete, verify `.lvpart` removed
- Cancel again, choose Keep, verify `.lvpart` preserved

---

### Task P1-02-B: Cancel with Cleanup Dialog - Android
**Priority**: P1  
**Effort**: 3h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Add `cancelTransfer(keepPartials: Boolean)` to `TransferEngine.kt`
2. Implement cleanup for both File and SAF modes
3. Add cancel button to transfer progress screen
4. Show AlertDialog with cleanup options
5. Update notification to show cancellation

**Files to Modify**:
- `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt`
- `android/app/src/main/kotlin/com/example/lanvault/ui/main/MainScreen.kt`

**Verification**:
- Manual: Cancel transfer, choose options, verify behavior

---

### Task P1-03-A: Manual Retry Button - Desktop
**Priority**: P1  
**Effort**: 3h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Add "Retry" button to failed/interrupted transfers in history
2. Disable if source files no longer exist
3. Reuse existing `StartSend()` with saved transfer_id and manifest
4. Show "Retrying..." spinner during reconnection
5. Auto-navigate to Transfers tab on retry

**Files to Modify**:
- `desktop/frontend/src/components/TransfersTab.tsx`
- `desktop/app.go` (no changes needed, existing logic works)

**Verification**:
- Manual: Interrupt transfer, click Retry, verify resume from last chunk

---

### Task P1-03-B: Manual Retry Button - Android
**Priority**: P1  
**Effort**: 3h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Add "Retry" button to failed transfers in history list
2. Validate source URIs still accessible
3. Call `startSendSources()` with existing transfer_id
4. Show SnackBar: "Retrying transfer..."
5. Update progress indicator

**Files to Modify**:
- `android/app/src/main/kotlin/com/example/lanvault/ui/main/MainScreen.kt`
- `android/app/src/main/kotlin/com/example/lanvault/transfer/TransferEngine.kt`

**Verification**:
- Manual: Interrupt transfer, tap Retry, verify resume

---

### Task P1-06: Verify mDNS Implementation Status
**Priority**: P1  
**Effort**: 4h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. **Inspect Go code**: Check if mDNS library is used (e.g., `github.com/hashicorp/mdns`)
2. **Inspect Android code**: Check if `NsdManager` is actually configured
3. **Protocol validation**: Verify `_lanvault._tcp.local.` service registration
4. If mDNS NOT implemented:
   - Update README.md to say "UDP discovery only"
   - Update AUDIT_PROGRESS.md FR-HOME-01 to clarify
   - Add mDNS to P2 roadmap
5. If mDNS IS implemented:
   - Write integration test
   - Verify mDNS and UDP beacon are merged by device_id

**Files to Inspect**:
- `core/discovery/beacon.go` (look for mDNS imports)
- `android/app/src/main/kotlin/com/example/lanvault/discovery/Discovery.kt` (look for NsdManager)

**Verification**:
```bash
# If mDNS implemented
avahi-browse -r _lanvault._tcp
# Should show LANVault services
```

**Decision Point**: Document actual status accurately

---

## Phase C: Production Polish (12 hours)

### Task PROD-01: Windows Installer Creation
**Priority**: P1  
**Effort**: 6h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Configure Wails build with NSIS/WiX
2. Bundle WebView2 Evergreen Runtime
3. Create installer script with:
   - Welcome screen
   - License (if applicable)
   - Install directory selection
   - Firewall rule registration
   - Start menu + desktop shortcuts
4. Test on clean Windows 10 VM
5. Test on Windows 11
6. Document installer creation in `docs/BUILD.md`

**Files to Create**:
- `desktop/build/windows/installer.nsi` or `installer.wxs`
- `docs/BUILD.md`

**Build Command**:
```bash
cd desktop
wails build -clean -platform windows/amd64 -nsis
```

**Verification**:
- Install on clean VM
- Verify all shortcuts created
- Verify firewall rule present
- Uninstall, verify clean removal

---

### Task PROD-02: Android Release APK Configuration
**Priority**: P1  
**Effort**: 3h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: None

**Subtasks**:
1. Configure `signingConfigs.release` in `build.gradle.kts`
2. Create `proguard-rules.pro` with Keep rules
3. Enable `minifyEnabled` and `shrinkResources`
4. Generate release keystore (not committed to repo)
5. Document signing process in `android/README.md`
6. Build and test release APK

**Files to Modify**:
- `android/app/build.gradle.kts`
- `android/app/proguard-rules.pro` (create)
- `android/README.md` (build instructions)

**Build Command**:
```bash
export KEYSTORE_PATH=../release.keystore
export KEYSTORE_PASSWORD=<password>
export KEY_ALIAS=lanvault
export KEY_PASSWORD=<password>
cd android
./gradlew assembleRelease
```

**Verification**:
- Install release APK on device
- Verify app functions correctly
- Check APK size (should be smaller than debug)

---

### Task PROD-03: Documentation Update
**Priority**: P1  
**Effort**: 3h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: P1-06 (mDNS status)

**Subtasks**:
1. Update `README.md`:
   - System requirements
   - Installation instructions
   - Quick start guide
   - Known limitations (Android 15 timeout, FAT32)
2. Create `docs/USER_GUIDE.md`:
   - How to pair devices
   - How to send/receive files
   - Troubleshooting common issues
3. Update `AUDIT_PROGRESS.md`:
   - Mark PC-01 through PC-06 as RESOLVED
   - Update test results
4. Verify `docs/PRD.md` and `docs/PROTOCOL.md` accuracy

**Files to Modify**:
- `README.md`
- `docs/USER_GUIDE.md` (create)
- `AUDIT_PROGRESS.md`

**Verification**:
- Technical review: all claims match implementation
- No fabricated benchmarks or test results

---

## Phase D: Physical Device Testing (8 hours)

### Task TEST-01: End-to-End Transfer Test Matrix
**Priority**: P0  
**Effort**: 8h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: PROD-01, PROD-02
**Blocker**: Requires physical Windows PC + Android device

**Test Scenarios** (10 required):

| # | Test Case | Network | File | Expected | Actual Result |
|---|-----------|---------|------|----------|---------------|
| 1 | Small file | WiFi | text.txt (1 KB) | <1s, SHA-256 match | 🔴 NOT RUN |
| 2 | Photo | WiFi | photo.jpg (5 MB) | Complete, metadata | 🔴 NOT RUN |
| 3 | Video | WiFi | video.mp4 (100 MB) | Complete, playable | 🔴 NOT RUN |
| 4 | Large file | WiFi | file.bin (1 GB) | >85% TCP throughput | 🔴 NOT RUN |
| 5 | Nested folder | WiFi | project/ (50 MB) | Structure preserved | 🔴 NOT RUN |
| 6 | Multiple files | WiFi | 100 files | All transferred | 🔴 NOT RUN |
| 7 | Resume | WiFi → disconnect | 500 MB | Resume from chunk | 🔴 NOT RUN |
| 8 | Hotspot | Phone hotspot | 50 MB | No internet needed | 🔴 NOT RUN |
| 9 | Bidirectional | WiFi | 10 MB each | Both succeed | 🔴 NOT RUN |
| 10 | Collision | WiFi | file.txt (twice) | file (1).txt created | 🔴 NOT RUN |

**For Each Test**:
1. Record start time
2. Transfer file(s)
3. Record completion time and speed
4. Compute SHA-256 of sent and received files
5. Compare hashes
6. Verify file metadata (mtime, size)
7. Document result in `docs/VERIFICATION_REPORT.md`

**Verification Report Template**:
```markdown
## Test 1: Small File Transfer
- **Date**: YYYY-MM-DD
- **Windows Version**: Windows 11 Pro 23H2
- **Android Version**: Android 14 (Pixel 6)
- **Network**: Home WiFi (802.11ac, 5 GHz)
- **File**: test.txt (1024 bytes)
- **Duration**: 0.8 seconds
- **Speed**: 10 Kbps (overhead dominated)
- **SHA-256 Match**: ✅ YES
- **Result**: ✅ PASS

Sent: `sha256sum test.txt` → a3b5c7...
Received: `sha256sum test.txt` → a3b5c7...
```

---

## Phase E: Final Verification (8 hours)

### Task VERIFY-01: Automated Test Suite Revalidation
**Priority**: P0  
**Effort**: 2h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: All Phase A, B, C tasks

**Subtasks**:
1. Run Go core tests: `go test -v ./...`
2. Run Go race detector: `go test -race ./...`
3. Run Android unit tests: `./gradlew testDebugUnitTest`
4. Run protocol validation: `python tools/verify_companions.py --self-test --prd docs/PRD.md`
5. Run desktop build: `npm run build` in `desktop/frontend`
6. Document results in `docs/FINAL_TEST_REPORT.md`

**Expected Results**:
- All tests PASS
- 0 data races
- 0 build errors

---

### Task VERIFY-02: Code Review Checklist
**Priority**: P0  
**Effort**: 3h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: All implementation tasks

**Review Checklist**:
- [ ] All P0 bugs fixed (PC-01 through PC-06)
- [ ] No new security vulnerabilities introduced
- [ ] TLS 1.3 still enforced
- [ ] Certificate verification not weakened
- [ ] Path traversal protection intact
- [ ] Resume state binding preserved
- [ ] Error handling graceful (no crashes)
- [ ] UI strings clear and actionable
- [ ] No hardcoded secrets or keys
- [ ] Logging doesn't expose sensitive data

**Files to Review**:
- All modified files from Phase A, B, C
- Focus on security-critical code: `identity.go`, `Identity.kt`, `receiver.go`, `PathPlanner.kt`

---

### Task VERIFY-03: Documentation Accuracy Audit
**Priority**: P1  
**Effort**: 2h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: All documentation tasks

**Audit Checklist**:
- [ ] README claims match actual features
- [ ] PRD.md version 1.2 synchronized with code
- [ ] PROTOCOL.md test vectors correct
- [ ] Kiro steering files have valid `fileMatchPattern`
- [ ] AUDIT_PROGRESS.md accurately reflects status
- [ ] No fabricated performance numbers
- [ ] Known limitations documented
- [ ] Build instructions accurate

---

### Task VERIFY-04: Final Report Generation
**Priority**: P0  
**Effort**: 1h  
**Status**: 🔴 NOT_STARTED  
**Dependencies**: All verification tasks

**Deliverables**:
1. `docs/FINAL_CODE_AUDIT.md`:
   - Summary of issues found
   - Issues fixed
   - Remaining issues
   - Risk assessment

2. `docs/FIX_IMPLEMENTATION_LOG.md`:
   - PC-01 through PC-06 fix details
   - P1-01 through P1-03 implementation notes
   - Code changes and verification

3. `docs/VERIFICATION_REPORT.md`:
   - Automated test results
   - Physical device test results (if available)
   - Performance metrics (if measured)

4. Update `AUDIT_PROGRESS.md`:
   - Mark PC-01 through PC-06 as RESOLVED
   - Update requirements status
   - Update verification evidence

---

## Summary & Progress Tracking

### Overall Progress

| Phase | Tasks | Completed | In Progress | Not Started |
|-------|-------|-----------|-------------|-------------|
| Phase A: Critical Bugs | 6 | 0 | 0 | 6 |
| Phase B: P1 Features | 7 | 0 | 0 | 7 |
| Phase C: Production | 3 | 0 | 0 | 3 |
| Phase D: Physical Testing | 1 | 0 | 0 | 1 |
| Phase E: Verification | 4 | 0 | 0 | 4 |
| **Total** | **21** | **0** | **0** | **21** |

### Effort Summary

- **Phase A**: 16 hours
- **Phase B**: 24 hours
- **Phase C**: 12 hours
- **Phase D**: 8 hours (requires hardware)
- **Phase E**: 8 hours
- **Total**: 68 hours

### Critical Path

```
Phase A (Critical Bugs) → Phase B (Features) → Phase C (Production) → Phase D (Testing) → Phase E (Verification)
```

**Minimum for Production Release**:
- Phase A: ALL tasks (critical fixes)
- Phase B: P1-01 and P1-02 (pause/resume/cancel)
- Phase C: ALL tasks (installer + docs)
- Phase D: 🔴 BLOCKED (requires hardware) - optional but highly recommended
- Phase E: ALL tasks (verification mandatory)

---

## Next Actions

1. **Start**: Task PC-01-A (Android state synchronization)
2. **Prioritize**: Phase A (critical bugs) before new features
3. **Sequence**: Complete each task, verify, then proceed
4. **Document**: Update this file after each task completion
5. **Commit**: Atomic commits per task for reviewability

---

**Document Status**: COMPLETE  
**Ready for Implementation**: YES  
**Blockers**: Physical device testing requires hardware (Phase D)
