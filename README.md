# LANVault companion specifications (revision 1.2)

| Path | Purpose |
|---|---|
| docs/PROTOCOL.md | Wire protocol v1.1 (draft); application v1.0 is manual-accept only |
| docs/protocol-constants.json | Single source of truth for protocol numbers, frame types and error codes |
| docs/design-tokens.json | Colors, typography, spacing, motion, and the contrast pairs CI must enforce |
| docs/settings-schema.json | Every setting with options, default, platform and priority |
| testvectors/ | Shared fixtures that the Go and Kotlin tests must both assert |
| testvectors/security/path-corpus.json | Hand-authored path and manifest cases for the security acceptance tests |
| tools/ref/path_rules.py | Reference oracle for the path rules (not production code) |
| tools/verify_companions.py | Cross-checks all of the above |

Run: `python3 tools/verify_companions.py --self-test`
Optional: export the PRD as Markdown and add `--prd PRD.md` to compare its token and settings tables.

The self-test builds deliberately broken variants (a changed constant, a flipped frame byte, a disabled path rule, a low-contrast token) and fails unless every one is detected.
The test certificate in testvectors/identity.json is for fixtures only; its private key is not published.

## Implementation notes for v1.2 reconciliation

- **Safety:** All names are validated across the entire manifest after local normalization, sanitization and case-folding. File/directory aliasing rejects the offer; file/file aliases get unique names. The path oracle is a contract reference, not production filesystem security code.
- **Release vs wire version:** Application v1.0 implements **wire protocol 1.1**. Auto-accept is deferred to application v1.1/P1; it never applies to unpaired peers.
- **Android:** Active approved transfers may continue under a permitted foreground lifecycle; passive standby listening is P1 and is not promised as perpetual background reception. Android network mode is auto (local Wi-Fi/hotspot only).
- **Manifest frames:** Each part must obey both entry count and the actual serialized UTF-8 frame-byte cap.
- **Validation:** `python3 tools/verify_companions.py --self-test --prd docs/PRD.md` must pass. The test suite covers platform-specific path collisions, oversized manifest batching, version consistency and PRD tables.
