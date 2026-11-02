---
inclusion: fileMatch
fileMatchPattern: "protocol/*.kt"
---

# Protocol Implementation Guide

When implementing LANVault protocol components in Kotlin:

## Reference Documents

1. **Primary Spec**: Always reference `docs/PROTOCOL.md` as the source of truth
2. **Constants**: All numeric values come from `docs/protocol-constants.json` ONLY
3. **Test Vectors**: Verify implementation against `testvectors/*.json`

## Frame Encoding Rules

- **Byte Order**: Always use BIG_ENDIAN (network byte order)
  ```kotlin
  ByteBuffer.allocate(size).order(ByteOrder.BIG_ENDIAN)
  ```

- **Frame Format**: 
  ```
  [length: u32][type: u8][payload: variable]
  ```
  Length includes type byte + payload, NOT the length field itself

- **String Encoding**: UTF-8, NFC-normalized
  ```kotlin
  val bytes = str.normalize(Normalizer.Form.NFC).toByteArray(Charsets.UTF_8)
  ```

## Protocol Constants

Never hardcode these values - import from generated Constants.kt:

- `PROTOCOL_MAJOR = 1`
- `PROTOCOL_MINOR = 1`
- `DEFAULT_PORT = 47800`
- `BEACON_PORT = 47801`
- `CHUNK_SIZE_DEFAULT = 4194304` (4 MiB)
- `DATA_STREAMS_DEFAULT = 4`
- `MAX_CONTROL_FRAME = 1048576` (1 MiB)
- `MAX_DATA_FRAME = 16777257`

## Error Handling

Return proper error codes from `docs/protocol-constants.json`:

1. `UNSUPPORTED_VERSION` - Major version mismatch
2. `NOT_PAIRED` - Peer not in trust store
3. `REJECTED` - User declined
4. `DISK_FULL` - Insufficient space
5. `PATH_INVALID` - Manifest validation failed
6. `HASH_MISMATCH` - Chunk verification failed
7. `PROTOCOL_ERROR` - Malformed frame
8. `CANCELLED` - User or peer cancelled
9. `TIMEOUT` - Dead peer (no frame for 15s)
10. `FILE_TOO_LARGE_FOR_FS` - FAT32 limit etc.

## Security Requirements

- **Mutual TLS 1.3 only**: No older versions
- **Certificate Pinning**: Compare fingerprints byte-for-byte
- **Constant-Time Comparison**: For pairing tokens
  ```kotlin
  fun constantTimeEquals(a: ByteArray, b: ByteArray): Boolean {
      if (a.size != b.size) return false
      var result = 0
      for (i in a.indices) result = result or (a[i].toInt() xor b[i].toInt())
      return result == 0
  }
  ```

## Chunk Integrity

- Hash algorithm: SHA-256
- Hash per chunk (not per file)
- Verify on receive BEFORE marking in bitmap
- Re-request with CHUNK_NEED on mismatch (max 3 attempts)

## Test Verification

Before committing protocol changes:

1. Run unit tests against test vectors
2. Verify frame hex matches `testvectors/frames.json`
3. Check manifest hash against `testvectors/manifest_hash.json`
4. Test bitmap encoding with `testvectors/bitmap.json`

## Common Pitfalls

❌ **Don't**: Hardcode port numbers or timeouts  
✅ **Do**: Import from Constants

❌ **Don't**: Use platform-default byte order  
✅ **Do**: Explicitly set BIG_ENDIAN

❌ **Don't**: Assume strings are ASCII  
✅ **Do**: Use UTF-8 and normalize to NFC

❌ **Don't**: Skip frame length validation  
✅ **Do**: Check against MAX_CONTROL_FRAME before allocating

❌ **Don't**: Trust peer-provided indices without bounds checking  
✅ **Do**: Validate file_index and chunk_index against manifest

## Flow Control

- Window size: `WINDOW_CHUNKS_PER_STREAM = 8` chunks per stream
- Max outstanding: `window * data_streams` chunks
- Send CHUNK_ACK after verify + persist (batch ~16 chunks or 200ms)
- Respect TCP backpressure on top of chunk window

## Resume State Integrity

- Bind to `(transfer_id, sender_fingerprint, manifest_hash, chunk_size)`
- HMAC-SHA-256 tag on persisted state (key from device keystore)
- Discard tampered state silently, treat as fresh transfer
- Re-verify chunks from earlier sessions before finalization

#[[file:../../docs/PROTOCOL.md]]
#[[file:../../docs/protocol-constants.json]]
