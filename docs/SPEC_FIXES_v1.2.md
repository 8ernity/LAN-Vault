# LANVault specification reconciliation — v1.2

**Date:** 1 November 2026  
**Status:** Companion specifications + Word/Markdown PRD updated; not a compiled application.

## Decisions and fixes

1. **File/directory collisions (security):** A receiver must normalize/sanitize/case-fold paths and reserve every explicit and implicit directory *before* assigning file-name suffixes. If any original file aliases a directory after those transformations, reject the whole OFFER (`PATH_INVALID`), regardless of entry order. File/file aliases remain uniquely numbered. Destination safety still requires no-follow filesystem operations and atomic no-replace finalization.
2. **Manual approval in application v1.0:** Every transfer, including from a paired device, requires explicit acceptance. Per-trusted-device auto-accept remains an optional P1 feature for application v1.1 and must default off. Unpaired senders always need consent. This resolves the conflict with the existing manual-accept wire requirements without expanding the initial release surface.
3. **Version distinction:** The draft wire **protocol version is 1.1**, while the initial **application release remains v1.0**. Examples, constants, PRD title, and README now agree. Adding application v1.1 features later does not automatically require a wire version change.
4. **Android background behavior:** Application v1.0 supports resuming/permitted foreground services for *active, user-approved transfers*, but does not promise unattended idle background discovery/listening. A bounded, user-initiated 15/60-minute standby receive window is P1 only and subject to OS policy and real-device verification.
5. **Android network setting:** Only `Network mode: Auto` for local Wi-Fi client or the phone's own hotspot exists in v1.0. No user switch that could route transfers through mobile data.
6. **Settings priority:** `Speed units` and `Completion sound` are P0 in both the Word PRD and settings schema. Settings table labels, platforms, and priorities agree.
7. **Manifest frame byte limit:** Every `MANIFEST_PART` must satisfy both the at-most-1,000-entry limit and the *actual* `MAX_CONTROL_FRAME` length (type byte plus UTF-8 JSON). This must be measured from serialized bytes, including escaping; receivers reject oversized frames before allocation. Test coverage exercises both thresholds.
8. **Validator strengthening:** New explicit Windows/posix cross-profile regression cases, namespace invariants, HELLO wire-version example checking, manifest-batching tests, and Markdown PRD table parsing (including escaped `#HEX` colors). The prior false-positive PRD color-token failures are eliminated.

## Acceptance checks

```bash
python3 tools/verify_companions.py --self-test --prd docs/PRD.md
```

Every check (including `prd` and `manifest_frames`) must report PASS; all negative mutation checks must be detected. Use Go and Kotlin implementations to test against the shared corpus before starting a UI-only milestone.

**Implementation status:** This repository contains the complete cross-platform LANVault implementation:
- **Go Core Engine:** Protocol v1.1 wire framing, cryptographic TLS fingerprint pinning, authenticated data stream tokens, root-confined path traversal safety, chunk-index seek-writes, in-flight windowing, and completion barrier.
- **Windows Desktop:** Wails Go backend with TypeScript/React frontend, tray integration, mutual pairing challenge/response, and transfer history.
- **Android Native:** Kotlin engine with direct SAF streaming (zero cache copy), mutual QR pairing, foreground notification service with lifecycle management, and Compose UI.
- **CLI & Test Harness:** Loopback CLI and shared cross-platform companion test suite.
