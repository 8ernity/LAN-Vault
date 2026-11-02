# LANVault Protocol Specification — v1.1 (draft)

Wire protocol shared by the Go core (Windows) and the Kotlin client (Android).
Both implementations MUST follow this document. Where code and spec disagree, the spec wins and the code is the bug.

**Keywords:** MUST, SHOULD, MAY as in RFC 2119.
**Byte order:** all integers are big-endian. **Strings:** UTF-8, NFC-normalized.

---

## 1. Constants (single source of truth)

Every number below lives in one file, `docs/protocol-constants.json`. Both codebases generate their constants from it (a small script emits `constants.go` and `Constants.kt`), and `tools/verify_companions.py` fails if this table, the frame table in section 5.3 or the error table in section 12 disagree with the JSON. Nobody hardcodes these values in app code.

| Name | Value | Notes |
|---|---|---|
| `PROTOCOL_MAJOR` | 1 | Incompatible wire changes only |
| `PROTOCOL_MINOR` | 1 | Backward-compatible additions |
| `DEFAULT_PORT` | 47800 | tcp port — Always advertised, never assumed |
| `BEACON_PORT` | 47801 | udp port — Discovery fallback |
| `BEACON_INTERVAL_MS` | 2000 | ms |
| `CHUNK_SIZE_DEFAULT` | 4194304 | bytes — 4 MiB |
| `CHUNK_SIZE_MIN` | 65536 | bytes |
| `CHUNK_SIZE_MAX` | 16777216 | bytes |
| `DATA_FRAME_OVERHEAD` | 41 | bytes — 1 type + 8 header + 32 hash |
| `MAX_DATA_FRAME` | 16777257 | bytes — CHUNK_SIZE_MAX + DATA_FRAME_OVERHEAD |
| `MAX_CONTROL_FRAME` | 1048576 | bytes — 1 MiB |
| `DATA_STREAMS_DEFAULT` | 4 |  |
| `DATA_STREAMS_MAX` | 8 |  |
| `WINDOW_CHUNKS_PER_STREAM` | 8 | chunks |
| `MANIFEST_PART_MAX_ENTRIES` | 1000 | entries |
| `MAX_MANIFEST_ENTRIES` | 1000000 | entries — Larger offers are rejected with PATH_INVALID |
| `MAX_PATH_BYTES` | 4096 | bytes — UTF-8 |
| `MAX_SEGMENT_BYTES` | 255 | bytes — UTF-8 |
| `MAX_CONTROL_CONNECTIONS` | 16 | connections — Concurrent, per device |
| `HELLO_TIMEOUT_MS` | 10000 | ms — Close a TLS connection that sends no HELLO |
| `PING_INTERVAL_MS` | 5000 | ms — When idle |
| `DEAD_PEER_TIMEOUT_MS` | 15000 | ms |
| `PAIR_TOKEN_TTL_S` | 120 | s |
| `PAIR_TOKEN_BYTES` | 16 | bytes — Random |
| `PAIR_MAX_FAILED_ATTEMPTS` | 3 | attempts — Token invalidated after this many failures |
| `PARTIAL_RETENTION_DAYS` | 7 | days — Receiver resume state |
| `OFFER_RESPONSE_TIMEOUT_S` | 300 | s — Receiver user has this long to answer an OFFER |
| `PAUSED_SERVICE_LIMIT_S` | 600 | s — Android: a paused transfer releases its service after this |
| `HASH_ALGO` | SHA-256 | Chunk hashing and fingerprints |
| `MAX_NEED_PAIRS_PER_FRAME` | 1024 | pairs — CHUNK_NEED limit per frame |

---

## 2. Identity and trust

### 2.1 Device certificate
- On first run each device generates an **ECDSA P-256** key pair and a self-signed X.509 certificate (10-year validity, CN = random UUID; the CN carries no meaning).
- The private key stays in the OS keystore where available (Android Keystore, Windows DPAPI-protected file).
- **Fingerprint** = lowercase hex of SHA-256 over the certificate's DER encoding (64 chars).
- **`device_id`** = first 32 hex chars of the fingerprint. It is *derived*, never chosen, so it cannot be spoofed independently of the certificate. Receivers MUST verify that a claimed `device_id` equals the prefix of the fingerprint in the TLS session.

### 2.2 Trust store
Each device keeps `fingerprint -> { name, platform, paired_at }`. A peer is **paired** iff its fingerprint is in the store. Fingerprints are compared byte-for-byte; certificate chains, hostnames and expiry are NOT used for trust.

### 2.3 TLS
- TLS 1.3 only, **mutual**. The server requests a client certificate and MUST accept any syntactically valid certificate at the TLS layer (verification happens at the application layer against the trust store, see 2.2).
- No hostname checks; no CA validation.
- A connection from an unpaired peer is allowed to proceed only to `HELLO`, `PAIR_REQUEST`, or (if the receiver's "allow unpaired senders" setting is on) `OFFER`. Anything else gets `ERROR NOT_PAIRED` and a close.

---

## 3. Discovery

Discovery only produces `(device_id, name, platform, ip, port)`. It is never trusted. Identity is confirmed by the TLS fingerprint at connect time.

### 3.1 mDNS / DNS-SD (primary)
Service `_lanvault._tcp.local.`, port from the SRV record. TXT records:

| Key | Value |
|---|---|
| `v` | `1` (major version) |
| `id` | `device_id` |
| `name` | display name (UTF-8, ≤ 63 bytes) |
| `plat` | `windows` or `android` |

Android: acquire a `WifiManager.MulticastLock` while discovering or advertising.

### 3.2 UDP beacon (fallback)
Every `BEACON_INTERVAL_MS`, send a JSON datagram to the subnet broadcast address and `255.255.255.255` on `BEACON_PORT`:

```json
{"v":1,"id":"<device_id>","name":"Arpan's Phone","plat":"android","port":47800}
```

Devices listen on `BEACON_PORT` and merge entries with mDNS results by `device_id`. A peer is dropped after 3 missed intervals (beacon) or on mDNS goodbye/TTL expiry.

### 3.3 Manual
QR code (section 4.1) or typed `ip:port`. Always available, because many hotspots and routers block multicast and broadcast (client/AP isolation).

---

## 4. Pairing

### 4.1 QR pairing (preferred)
The *displaying* device shows a QR containing a URI:

```
lanvault://pair?v=1&fp=<64-hex fingerprint>&ip=<ip[,ip...]>&port=<port>&name=<urlencoded>&tok=<base64url 16 random bytes>
```

Flow:
1. Scanner connects to one of the IPs (TLS, mutual) and checks that the server certificate's fingerprint equals `fp` from the QR. Mismatch = abort. This authenticates the displayer out-of-band.
2. Scanner sends `PAIR_REQUEST { token, name, platform }`.
3. Displayer checks the token: single-use, constant-time compare, not older than `PAIR_TOKEN_TTL_S`. On success it adds the scanner's TLS fingerprint to its trust store and replies `PAIR_OK`. The token authenticates the scanner to the displayer.
4. Scanner stores the displayer's fingerprint. Both sides show "Paired with X".

The token is invalidated after one use or on expiry. Displaying a new QR invalidates the previous token.

### 4.2 Short-code pairing (optional, v1.1)
For devices that cannot scan: both sides compute a **SAS** from `SHA-256(min(fpA,fpB) || max(fpA,fpB))` and render the first 44 bits as 4 words from a fixed 2048-word list. Users confirm the words match on both screens. (A 6-digit code is NOT acceptable, since ~20 bits can be ground offline by a MITM.)

### 4.3 Unpaired quick-send (TOFU)
If the receiver allows unpaired senders, it prompts with the sender's name and a short fingerprint (first 8 hex chars, grouped `ab12 cd34`) on every transfer, with an optional "Trust this device" button. The UI MUST label this as weaker than pairing.

---

## 5. Transport and framing

### 5.1 Connections
- **Control connection:** one TLS connection from sender to receiver. Carries handshake, manifest, accept, acks and control messages.
- **Data connections:** `N` additional TLS connections (`N` = negotiated `data_streams`), opened by the sender after `ACCEPT`. Carry `DATA` frames only (plus `PING`/`PONG`/`ERROR`).
- Data connections authenticate with the same mutual TLS, then send `HELLO` with `role:"data"` carrying the `transfer_id` and the `stream_token` from `ACCEPT`.

### 5.2 Frame format

```
+----------------+--------+------------------+
| length  (u32)  | type   | payload          |
|                | (u8)   | (length-1 bytes) |
+----------------+--------+------------------+
```

`length` counts the type byte plus payload (it does not count itself). Control frame payloads are UTF-8 JSON objects. Unknown JSON fields MUST be ignored. Unknown frame types on the control connection: reply `ERROR PROTOCOL_ERROR` and close. A frame larger than `MAX_CONTROL_FRAME` (control types) or `MAX_DATA_FRAME` (type `0x20`) is a protocol error.

### 5.3 Frame types

| Type | Name | Direction | Payload |
|---|---|---|---|
| `0x01` | HELLO | initiator → responder | JSON |
| `0x02` | HELLO_OK | responder → initiator | JSON |
| `0x10` | OFFER | sender → receiver | JSON |
| `0x11` | MANIFEST_PART | sender → receiver | JSON |
| `0x12` | MANIFEST_END | sender → receiver | JSON |
| `0x13` | ACCEPT | receiver → sender | JSON |
| `0x14` | REJECT | receiver → sender | JSON |
| `0x20` | DATA | sender → receiver (data streams) | binary |
| `0x21` | CHUNK_ACK | receiver → sender (control) | binary |
| `0x22` | FILE_DONE | receiver → sender (control) | JSON |
| `0x23` | CHUNK_NEED | receiver → sender (control) | binary |
| `0x30` | PAUSE | either | JSON |
| `0x31` | RESUME | either | JSON |
| `0x32` | CANCEL | either | JSON |
| `0x40` | DONE | sender → receiver | JSON |
| `0x41` | DONE_OK | receiver → sender | JSON |
| `0x50` | PAIR_REQUEST | scanner → displayer | JSON |
| `0x51` | PAIR_OK | displayer → scanner | JSON |
| `0x7D` | PING | either | empty |
| `0x7E` | PONG | either | empty |
| `0x7F` | ERROR | either | JSON |

---

## 6. Session lifecycle

```
Sender                                          Receiver
  |-------- TLS 1.3 (mutual) ------------------->|
  |-- HELLO ------------------------------------>|
  |<------------------------------- HELLO_OK ----|
  |-- OFFER (summary) -------------------------->|
  |-- MANIFEST_PART x k ------------------------>|
  |-- MANIFEST_END ----------------------------->|
  |                  (receiver validates paths, disk space, filesystem limits,
  |                   then asks the user to accept; or finds resume state)
  |<--------------- ACCEPT {have, stream_token} -|
  |==== open N data connections: HELLO(role=data) ====>|
  |== DATA frames (parallel, windowed) ==========>|
  |<-------------------- CHUNK_ACK (batched) -----|
  |<-------------------- FILE_DONE (per file) ----|
  |-- DONE ------------------------------------->|
  |<----------------------------------- DONE_OK --|
```

A receiver that declines sends `REJECT` instead of `ACCEPT`.

---

## 7. Message definitions

### 7.1 HELLO / HELLO_OK

```json
{
  "version": {"major": 1, "minor": 1},
  "device_id": "3fa9...(32 hex)",
  "name": "Arpan's Phone",
  "platform": "android",
  "role": "control",
  "capabilities": ["resume", "multi-stream"]
}
```

For a data connection: `"role": "data"`, plus `"transfer_id"` and `"stream_token"`.

`HELLO_OK` has the same fields (without `role`) plus `"paired": true|false`. If majors differ, the responder sends `ERROR UNSUPPORTED_VERSION` and closes. Minor versions are backward compatible; features are gated by `capabilities`.

### 7.2 OFFER

```json
{
  "transfer_id": "c5a1c1de-8c5e-4b62-8b6e-1f0f3a6f9d11",
  "chunk_size": 4194304,
  "file_count": 120,
  "dir_count": 8,
  "total_bytes": 9876543210,
  "data_streams": 4
}
```

- `transfer_id` is a UUIDv4 generated by the sender and **persisted by the sender** so it can reuse it to resume.
- `chunk_size` applies to all files in the transfer. The last chunk of each file may be shorter.

### 7.3 MANIFEST_PART / MANIFEST_END

```json
{"entries": [
  {"index": 0, "type": "dir",  "path": "Photos",       "size": 0,   "mtime_ms": 1730000000000},
  {"index": 1, "type": "file", "path": "Photos/a.jpg", "size": 5,   "mtime_ms": 1730000001000}
]}
```

- `index` is assigned by the sender, sequentially from 0, across files and dirs, in send order. It is the identifier used in all later messages.
- `path` is relative, uses `/` as separator, and follows the rules in section 10. `mtime_ms` is Unix epoch milliseconds, UTC.
- Empty directories are preserved via `type:"dir"`. Zero-byte files have 0 chunks and are created by the receiver at finalization.
- At most `MANIFEST_PART_MAX_ENTRIES` per part **and** the encoded frame length (1 type byte + UTF-8 JSON payload) MUST be at most `MAX_CONTROL_FRAME`. Both limits apply independently: 1,000 long paths may require multiple parts.
- The sender MUST batch on **actual serialized UTF-8 byte length**, including JSON escaping, punctuation, and the frame type byte; never estimate from entry count or Unicode character count. A part MUST contain at least one entry. If one entry alone cannot fit, reject the OFFER with `PATH_INVALID` instead of sending an oversized frame.
- The receiver MUST check the declared frame length against `MAX_CONTROL_FRAME` **before allocating or reading** its payload, count entries in every part, validate sequential indices, and enforce the full-manifest count and path limits. A peer exceeding either bound gets `PROTOCOL_ERROR` (invalid frame) or `PATH_INVALID` (invalid manifest), without writing any file.
- Empty manifests send `MANIFEST_END` directly (where permitted by the OFFER); non-empty manifests send one or more `MANIFEST_PART` frames.

```json
{"entry_count": 3, "manifest_hash": "bc54297c...0da1"}
```

**`manifest_hash`** = SHA-256 over the concatenation, in index order, of one line per entry:
`<type>\t<path>\t<size>\t<mtime_ms>\n` (UTF-8, no other whitespace). See test vector 14.1. Used to detect that a resumed transfer still refers to the same files.

### 7.4 ACCEPT / REJECT

```json
{
  "transfer_id": "c5a1c1de-...",
  "resume": true,
  "stream_token": "base64url(16 random bytes)",
  "data_streams": 4,
  "have": {"1": "BwI=", "7": "/w=="}
}
```

- `data_streams` MUST be ≤ the offered value and ≤ `DATA_STREAMS_MAX`.
- `have` maps `file_index` to a **bitmap of chunks already verified and persisted**. Files not listed have no chunks. Absent or `{}` for a fresh transfer. Bitmap encoding (section 9.1) is base64 (standard alphabet, with padding).
- `resume` is informational (true if the receiver found prior state matching `transfer_id` and `manifest_hash`).

```json
{"transfer_id": "c5a1c1de-...", "code": 3, "message": "Declined by user"}
```

Valid `code` values are in section 12. The receiver SHOULD pre-check and reject with `DISK_FULL` or `FILE_TOO_LARGE_FOR_FS` (e.g. FAT32 destination and a file ≥ 4 GiB) before asking the user.

### 7.5 DONE / DONE_OK / FILE_DONE

```json
{"transfer_id": "c5a1c1de-..."}                      // DONE
{"transfer_id": "c5a1c1de-...", "files_completed": 120, "bytes": 9876543210}   // DONE_OK
{"index": 1}                                         // FILE_DONE: file verified, renamed to final name
```

`FILE_DONE` is sent after the receiver has finished every chunk of a file, flushed it, set its modification time from the manifest, and renamed it from its partial name to its final name.

### 7.6 PAUSE / RESUME / CANCEL

```json
{"transfer_id": "c5a1c1de-...", "reason": "user"}
```

- **PAUSE**: the sender stops starting new chunks. In-flight chunks complete and are acked. Connections stay open and keepalive continues. The receiver MAY also send it (e.g. user paused on the receiving side); the sender MUST honor it.
- **RESUME**: continue after PAUSE.
- **CANCEL**: abort. The receiver deletes or keeps partial state per its own setting; the sender stops. Both close.

### 7.7 PAIR_REQUEST / PAIR_OK

```json
{"token": "base64url...", "name": "Arpan's Phone", "platform": "android"}
{"device_id": "3fa9...", "name": "Arpan-PC"}
```

### 7.8 ERROR

```json
{"code": 6, "message": "Chunk hash mismatch", "file_index": 4, "chunk_index": 17, "fatal": false}
```

`fatal:true` means the sender closes the connection afterward. `file_index`/`chunk_index` are optional context.

---

## 8. Data transfer

### 8.1 DATA frame (type `0x20`)

```
+---------------+---------------+------------------+-------------------+
| file_index    | chunk_index   | chunk bytes      | SHA-256 of chunk  |
| u32           | u32           | (frame len - 41) | 32 bytes          |
+---------------+---------------+------------------+-------------------+
```

- Chunk data length is derived from the frame length: `length - 1 - 8 - 32`. No separate length field.
- The receiver computes the expected length from the manifest: `min(chunk_size, size - chunk_index * chunk_size)`. A mismatch is `ERROR PROTOCOL_ERROR`.
- The receiver verifies the SHA-256 of the chunk. On mismatch it discards the chunk and sends `CHUNK_NEED` for it; the sender re-sends that chunk. After 3 failed attempts for the same chunk the receiver sends `ERROR HASH_MISMATCH (fatal:true)`.
- Chunks may arrive in any order and across any data stream. The receiver writes at offset `chunk_index * chunk_size` of the partial file.
- Hashing per chunk (rather than per file in the manifest) means the sender never has to read a whole file before sending the first byte.

### 8.2 CHUNK_ACK (type `0x21`, control connection)

Payload is a sequence of `(file_index u32, chunk_index u32)` pairs, 8 bytes each. A chunk is acked **only after it has been verified, written, and recorded in the persisted bitmap**. Receivers SHOULD batch acks (≈ every 16 chunks or 200 ms, whichever comes first).

### 8.2a CHUNK_NEED (type `0x23`, control connection)

Payload is a sequence of `(file_index u32, chunk_index u32)` pairs, at most `MAX_NEED_PAIRS_PER_FRAME` per frame. The receiver sends it when a chunk failed verification or must be fetched again (section 9.5). The sender re-queues each listed chunk and re-reads it from the source file. The sender MUST ignore nothing silently: a pair with an unknown `file_index`, a `chunk_index` at or beyond the chunk count, or more than `4 * WINDOW_CHUNKS_PER_STREAM * data_streams` outstanding re-requests is `ERROR PROTOCOL_ERROR` (fatal).

### 8.3 Flow control
Each data stream may have at most `WINDOW_CHUNKS_PER_STREAM` chunks sent but not yet acked. The sender assigns the next needed chunk to whichever stream has window space. TCP provides byte-level backpressure on top of this. There are no per-file or per-chunk round trips beyond the windowed acks, so thousands of small files stream back-to-back.

### 8.4 Progress
Progress and speed are *local* computations (sender: acked bytes; receiver: persisted bytes), not protocol messages. Implementations SHOULD smooth speed with an exponential moving average over a ~2 second window. ETA = remaining bytes / smoothed speed.

---

## 9. Resume

### 9.1 Bitmap encoding
One bit per chunk, `chunk_index` 0 first. Chunk `i` is bit `i % 8` (LSB = 0) of byte `i / 8`. Length = `ceil(chunk_count / 8)` bytes; unused high bits are 0. See test vector 14.3.

### 9.2 Receiver state
For each `transfer_id` the receiver persists (format is local, not on the wire):
- the manifest and `manifest_hash`, `chunk_size`, and sender fingerprint;
- a per-file chunk bitmap, flushed after chunk writes (at least once per second and on pause/close);
- partial file data under a temporary name (e.g. `name.lvpart`).

State is kept for `PARTIAL_RETENTION_DAYS`, then deleted.

### 9.3 Resume handshake
After an interruption (connection loss, app restart, or deliberate stop) the sender reconnects and runs the normal flow with the **same `transfer_id`**:
1. Sender sends `OFFER` + manifest.
2. Receiver finds state for that `transfer_id`:
   - same `sender fingerprint` **and** same `manifest_hash` → `ACCEPT` with `resume:true` and the persisted `have` bitmaps (no user prompt required, if the user already accepted this transfer);
   - otherwise → discard the old state and treat it as a new transfer (`resume:false`, user is asked again).
3. Sender skips chunks marked in `have`, sends the rest.

If a source file changed (size or mtime differs), `manifest_hash` changes, so the sender MUST create a new `transfer_id`. Partial chunks from a changed file are never mixed with the new version.

### 9.4 Finalization
A file is complete when every chunk bit is set. The receiver then: flushes and closes the file, sets mtime from the manifest, renames the partial file to its final name (applying collision handling, section 10), and sends `FILE_DONE`. A file is never visible under its final name before it is complete.

### 9.5 Resume state integrity

- **Binding.** State is bound to `(transfer_id, sender fingerprint, manifest_hash, chunk_size)`. An `OFFER` from any other fingerprint carrying the same `transfer_id` is a new transfer: the receiver returns no `have` data and leaves the existing state untouched.
- **Authenticated state.** The persisted record (manifest summary, per-file bitmaps, and the SHA-256 of every persisted chunk) carries an HMAC-SHA-256 tag. The key is a per-install secret kept with the device key (DPAPI on Windows, Android Keystore on Android). A record that fails the tag check is discarded as if it did not exist; the receiver replies `ACCEPT` with `resume:false` and an empty `have`, and logs local error `STATE_TAMPERED`.
- **Bitmap sanity.** Every set bit must refer to bytes inside the partial file's current length and below `chunk_count`. A file whose bitmap violates this has its bitmap cleared and its chunks re-requested.
- **Verification before finalization.** Before a file is renamed to its final name, the receiver re-reads every chunk written in an earlier session (chunks hashed and fsynced in the current session are exempt) and compares each with its stored SHA-256. Any mismatch clears that bit and sends `CHUNK_NEED`; the file stays under its temporary name until every chunk verifies. The UI state during this step is Verifying.
- **Sender-side validation of `have`.** A sender rejects an `ACCEPT` whose bitmap is not valid base64, has the wrong length for the file's chunk count, has bits set at or beyond the chunk count, or names an unknown file index: `ERROR PROTOCOL_ERROR` (fatal). A lying receiver can only lose its own data.

---

## 10. Paths and filesystem rules

### 10.1 Sender
Paths in the manifest are relative to the shared root, use `/`, and preserve the original name (NFC-normalized). The sender MUST NOT send symlink targets or special files; symlinks are skipped unless the user opts in to following them.

### 10.2 Receiver validation (reject the whole OFFER with `PATH_INVALID` on any violation)
- Not empty, not absolute, no leading `/`, no drive designator as the first segment (`C:`), no backslashes.
- No `.` or `..` segments, no empty segments (`a//b`).
- No NUL or control characters (U+0000–U+001F).
- Segment length ≤ 255 UTF-8 bytes; total path ≤ 4096 bytes.
- `index` values are unique and sequential; every file's parent directories exist in the manifest or are implied.

### 10.3 Receiver sanitization (local, not negotiated)
A path that is valid on the wire may still be illegal on the receiving filesystem. The receiver rewrites it locally and never tells the sender:
- Windows: replace `< > : " | ? *` and trailing dots/spaces; escape reserved names (`CON`, `PRN`, `AUX`, `NUL`, `COM1–9`, `LPT1–9`, with or without extension) by prefixing `_`.
- Enforce the OS path-length limit (use extended-length `\\?\` paths on Windows).
- On name collision, append ` (1)`, ` (2)`, … before the extension. Never overwrite silently.

### 10.4 Destination checks
Before ACCEPT, check free space (`total_bytes` minus already-held resume bytes) and the destination filesystem's max file size. If a file cannot fit (e.g. FAT32 ≥ 4 GiB), reject with `FILE_TOO_LARGE_FOR_FS` and name the file in `message`.

### 10.5 Manifest-level rules

These are checked on the whole manifest, after the per-path rules in 10.2.

- A path that is a drive designator (a first segment of one ASCII letter and a colon, such as `C:`) is rejected. Other colons are sanitized on Windows (10.3).
- Two entries of the same type with byte-identical paths: `PATH_INVALID`.
- A path used as both a file and a directory, or as a file and the parent of another entry: `PATH_INVALID`. This check applies again **after** receiver-side NFC normalization, filename sanitization, and case folding, across explicit **and implicit** parent directories, regardless of manifest entry order. Thus file `A` + directory `a`, or file `A` + file `a/x`, is rejected on Windows even though the wire paths differ. If one profile rejects and another accepts, the decision is receiver-local.
- More than `MAX_MANIFEST_ENTRIES` entries: `PATH_INVALID`, decided from the OFFER summary before any entry is stored.
- Paths are normalized to NFC before any comparison. Collisions after NFC, after sanitization, or (on Windows and other case-insensitive destinations) after case folding are resolved by renaming the later **file** entry: the number is inserted before the last dot (`REPORT.TAR (1).GZ`), or appended when there is no extension or only a leading dot (`.ENV (1)`), using the first free number. Directories with colliding names are merged. File-to-directory conflicts are **never** resolved by merging or overwriting: they invalidate the entire OFFER. The receiver MUST reserve the namespace of all explicit and implied directories **before** assigning file collision suffixes, so a suffix such as `A (1)` cannot alias a directory that appears later. Reject if sanitized or suffixed output exceeds OS path/segment limits; do not truncate silently.
- Bidirectional control characters (U+202A to U+202E and U+2066 to U+2069) are replaced with `_` on Windows.
- The receiver profile is `windows` (sanitize, case-insensitive) or `posix` (no sanitization, case-sensitive). A receiver on a case-insensitive filesystem such as FAT or exFAT applies the `windows` collision rules.

`testvectors/security/path-corpus.json` lists the expected result of every rule above for both profiles; `tools/ref/path_rules.py` is the reference oracle.

### 10.6 Safe writing and finalization

- Temporary files are created with exclusive-create semantics inside the destination.
- Before writing into a directory, the receiver resolves it and confirms that the resolved location is inside the destination root. Symlinks, junctions and reparse points inside the destination are never followed for writes; an entry that would pass through one fails with `PATH_INVALID`.
- Finalization MUST NOT replace an existing file. Use an atomic no-replace operation (`renameat2` with `RENAME_NOREPLACE` or link-then-unlink on POSIX, `MoveFileEx` without `MOVEFILE_REPLACE_EXISTING` on Windows, creating a new uniquely named document through SAF on Android). If the final name appeared after validation, pick the next free numbered name and try again.
- Received files are never opened or executed automatically.

---

## 11. Keepalive and failure handling

- Send `PING` on any connection that has been idle for `PING_INTERVAL_MS`; the peer replies `PONG`.
- No frame received for `DEAD_PEER_TIMEOUT_MS` = connection dead. The sender then retries the full resume handshake (section 9.3) with backoff (1 s, 2 s, 4 s, … capped at 30 s) while the transfer is not cancelled.
- If a data connection dies but the control connection is alive, the sender re-opens it (same `stream_token`) and re-sends un-acked chunks from that stream.
- If the control connection dies, all data connections are closed and the session is treated as interrupted.

---

## 12. Error codes

| Code | Name | Meaning |
|---|---|---|
| 1 | `UNSUPPORTED_VERSION` | Major version mismatch |
| 2 | `NOT_PAIRED` | Peer is not trusted for this operation |
| 3 | `REJECTED` | Receiver user declined |
| 4 | `DISK_FULL` | Not enough free space |
| 5 | `PATH_INVALID` | Manifest failed validation |
| 6 | `HASH_MISMATCH` | Chunk failed verification (retryable) |
| 7 | `FILE_CHANGED` | Source changed during transfer |
| 8 | `BUSY` | Receiver already handling a transfer |
| 9 | `PROTOCOL_ERROR` | Malformed or unexpected frame |
| 10 | `CANCELLED` | Transfer cancelled by a peer |
| 11 | `TIMEOUT` | Dead peer |
| 12 | `FILE_TOO_LARGE_FOR_FS` | Destination filesystem cannot hold a file |
| 13 | `PAIR_TOKEN_INVALID` | Pairing token wrong, used, or expired |
| 14 | `STATE_INTEGRITY` | Resume state failed its integrity check; the transfer restarts from nothing |

These codes are also defined in `protocol-constants.json`.

---

## 13. Versioning

`PROTOCOL_MAJOR` changes only for incompatible wire changes. New optional fields and capabilities increment `PROTOCOL_MINOR`. A peer MUST ignore unknown JSON fields and unknown capability strings.

---

## 14. Test vectors

Both the Go and Kotlin test suites MUST assert these exact values, and the vectors live in `testvectors/` as files, not only in this document.

### 14.1 `manifest_hash`
Entries:

| index | type | path | size | mtime_ms |
|---|---|---|---|---|
| 0 | dir | `Photos` | 0 | 1730000000000 |
| 1 | file | `Photos/a.jpg` | 5 | 1730000001000 |
| 2 | file | `notes.txt` | 0 | 1730000002000 |

Canonical bytes (`\t` = tab, `\n` = newline):

```
dir\tPhotos\t0\t1730000000000\n
file\tPhotos/a.jpg\t5\t1730000001000\n
file\tnotes.txt\t0\t1730000002000\n
```

```
manifest_hash = bc54297ce83d2fd925c50840eb7fb243f20c727919e1999ee6b7172d1c2f0da1
```

### 14.2 Frames (hex)

`PING`:
```
000000017d
```

`DATA` for `file_index=0`, `chunk_index=0`, chunk bytes `"hello"` (SHA-256 `2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824`), length field = 46:
```
0000002e20 00000000 00000000 68656c6c6f 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
```
(spaces added for readability; the real frame has none)

`CHUNK_ACK` for `(file 3, chunk 7)`:
```
00000009210000000300000007
```

### 14.3 Bitmap
Chunks `{0, 1, 2, 9}` completed → bytes `07 02` → base64 `BwI=`.

### 14.4 Authoritative vector files

The inline vectors above are copies. The files in `testvectors/` are authoritative and are checked by `tools/verify_companions.py`:

| File | Content |
|---|---|
| `manifest_hash.json` | Manifest entries and the expected hash |
| `frames.json` | PING, DATA, CHUNK_ACK and CHUNK_NEED frames in hex |
| `bitmap.json` | Valid and invalid bitmaps |
| `identity.json` | A test certificate with its fingerprint, `device_id` and short form |
| `pairing_uri.json` | A pairing URI and its parsed fields |
| `security/path-corpus.json` | Path and manifest rules, expected results for the `windows` and `posix` profiles |

`CHUNK_NEED` for `(3,7)` and `(3,8)`:
```
00000011230000000300000007 0000000300000008
```
(spaces added for readability; the real frame has none)

---

## 15. Security considerations

- Encryption and authentication come from mutual TLS 1.3 with pinned fingerprints. The protocol adds no custom cryptography. The per-chunk SHA-256 detects corruption and truncation; it is not a substitute for TLS.
- A compromised endpoint is out of scope. Data is plaintext on both devices.
- Discovery data (name, device_id) is visible to anyone on the LAN, even though transfers are encrypted.
- LANVault **application v1.0** MUST require explicit user acceptance for every transfer, including paired devices. Per-trusted-device auto-accept is a P1 feature planned for **application v1.1** and MUST remain off by default; unpaired senders always require explicit acceptance. Protocol v1.1 is a distinct **wire-version number**, not a promise that application v1.1 features ship in application v1.0.
- Receivers MUST NOT write outside the chosen destination directory. Validate and sanitize all paths (section 10) before touching the filesystem.
- Bound all allocation by the negotiated limits (`chunk_size`, frame caps, manifest entry counts) before reading peer-controlled lengths into memory.
- Rate-limit failed pairing attempts and invalidate the QR token after 3 failures.

---

## 16. Conformance requirements

Application release versions and protocol versions are independent. A v1.0 Windows/Android app may implement **wire protocol 1.1** and is still required to request approval for every transfer. Auto-accept is an application v1.1 (P1) feature.

### 16.1 Transfer authorization binding

| ID | Requirement |
|---|---|
| A-1 | A transfer authorization record `{transfer_id, sender fingerprint, stream_token, expiry}` is created only after explicit user acceptance in **application v1.0**. In application v1.1, an explicitly enabled per-paired-device auto-accept exception may create the same record after validating the peer certificate and stored consent. Unpaired senders NEVER auto-accept. |
| A-2 | `DATA`, `CHUNK_*`, `FILE_DONE`, `DONE` and `PAUSE`/`RESUME` frames received before the record exists are `ERROR PROTOCOL_ERROR`; the connection closes and no bytes are written. |
| A-3 | A data connection is accepted only if its TLS peer fingerprint equals the record's sender fingerprint, its `stream_token` matches, and the transfer is not in a terminal state. |
| A-4 | The `stream_token` becomes invalid when the transfer reaches Completed, Failed or Cancelled. A new attempt gets a new token. |
| A-5 | An `OFFER` from an unpaired fingerprint while unpaired senders are disallowed is answered `NOT_PAIRED` before any manifest part is read or stored. |
| A-6 | A connection that sends no `HELLO` within `HELLO_TIMEOUT_MS` is closed. At most `MAX_CONTROL_CONNECTIONS` control connections are open at once; further ones are refused. |
| A-7 | A second `OFFER` while a transfer is active is answered `BUSY`. |

### 16.2 Executable conformance

An implementation conforms only if it passes the security acceptance tests in the PRD (section 18.4) against the built binaries. Machine-readable inputs are in `testvectors/`; `tools/verify_companions.py` checks that the vectors, the constants and this document agree. Compiling, or passing unit tests that do not exercise a rule, is not evidence of conformance.

## 17. Implementation notes (non-normative)

- **Go:** `tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert, InsecureSkipVerify: true, VerifyPeerCertificate: pinCheck}` where `pinCheck` computes the SHA-256 of `rawCerts[0]` and consults the trust store.
- **Kotlin:** generate the certificate with BouncyCastle (`bcpkix`); supply a custom `X509TrustManager` and `KeyManager` to `SSLContext`; verify the fingerprint after the handshake.
- **Android storage:** write through `ParcelFileDescriptor` from SAF/`ContentResolver`; keep resume bitmaps in app-private storage, since SAF files cannot hold side files reliably. Run the transfer inside a foreground service with a `WifiLock`.
- **Many small files:** keep manifest parts streaming and avoid per-file `fsync`. Flush bitmaps on a timer instead.
