---
inclusion: fileMatch
fileMatchPattern: "crypto/*.kt|protocol/*.kt"
---

# Security Requirements for LANVault

## Cryptographic Standards

### Identity (ECDSA)

- **Algorithm**: ECDSA with P-256 curve (secp256r1)
- **Key Storage**: Android Keystore (hardware-backed when available)
- **Certificate**: Self-signed X.509v3, 10-year validity
- **Fingerprint**: SHA-256 of DER-encoded certificate

```kotlin
// Key generation
KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore")
    .apply {
        initialize(
            KeyGenParameterSpec.Builder(alias, PURPOSE_SIGN or PURPOSE_VERIFY)
                .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
                .setDigests(KeyProperties.DIGEST_SHA256)
                .build()
        )
    }
    .generateKeyPair()
```

### Transport (TLS 1.3)

- **Version**: TLS 1.3 ONLY (reject TLS 1.2 and older)
- **Mode**: Mutual authentication (both sides present certificates)
- **Cipher Suites**: System default (TLS 1.3 suites are secure)
- **Hostname Verification**: DISABLED (we use fingerprint pinning)
- **CA Validation**: DISABLED (self-signed certificates)

```kotlin
val sslContext = SSLContext.getInstance("TLSv1.3")
val socket = sslContext.socketFactory.createSocket() as SSLSocket
socket.enabledProtocols = arrayOf("TLSv1.3")
socket.needClientAuth = true

// Verify peer fingerprint
val peerCert = socket.session.peerCertificates[0] as X509Certificate
val peerFingerprint = calculateFingerprint(peerCert)
if (peerFingerprint != expectedFingerprint) {
    socket.close()
    throw SecurityException("Fingerprint mismatch")
}
```

### Data Integrity (SHA-256)

- **Per-Chunk Hashing**: SHA-256 of each 4 MiB chunk
- **Verification**: Before writing to disk and setting bitmap bit
- **Manifest Hash**: SHA-256 of canonical manifest representation
- **Resume State**: HMAC-SHA-256 tag on persisted state

```kotlin
val digest = MessageDigest.getInstance("SHA-256")
val chunkHash = digest.digest(chunkBytes)

// Constant-time comparison
fun constantTimeEquals(a: ByteArray, b: ByteArray): Boolean {
    if (a.size != b.size) return false
    var result = 0
    for (i in a.indices) {
        result = result or (a[i].toInt() xor b[i].toInt())
    }
    return result == 0
}

if (!constantTimeEquals(chunkHash, expectedHash)) {
    // Re-request chunk with CHUNK_NEED
}
```

## Trust Model

### Pairing (QR Code)

QR code establishes trust via out-of-band verification:

1. **Displayer** generates random 16-byte token
2. **QR contains**: fingerprint, IP, port, token
3. **Scanner** connects via TLS, verifies certificate fingerprint matches QR
4. **Scanner** sends PAIR_REQUEST with token
5. **Displayer** verifies token (constant-time), adds scanner to trust store
6. **Scanner** adds displayer to trust store

**Token Requirements**:
- 16 bytes (128 bits) of cryptographically random data
- Single-use (invalidated after one successful pair or on expiry)
- Time-to-live: 120 seconds
- Constant-time comparison to prevent timing attacks

```kotlin
val token = ByteArray(16)
SecureRandom().nextBytes(token)

// Store with timestamp
val pairRequest = PairRequest(
    token = token,
    createdAt = System.currentTimeMillis(),
    attempts = 0
)

// Verification
fun verifyToken(submitted: ByteArray): Boolean {
    if (System.currentTimeMillis() - pairRequest.createdAt > 120_000) {
        return false // Expired
    }
    if (pairRequest.attempts >= 3) {
        return false // Too many failed attempts
    }
    
    val valid = constantTimeEquals(submitted, pairRequest.token)
    if (!valid) {
        pairRequest.attempts++
    }
    return valid
}
```

### Trust Store

- **Storage**: Encrypted shared preferences or Room database
- **Schema**: `fingerprint -> {name, platform, pairedAt}`
- **Verification**: On every connection, verify TLS peer certificate fingerprint is in store
- **Revocation**: User can remove devices; removed fingerprints are immediately untrusted

### TOFU (Trust On First Use)

Optional convenience feature (OFF by default):

- Receiver allows unpaired senders
- On first transfer, receiver shows: sender name + short fingerprint (first 8 hex chars)
- User can "Trust this device" to add to store
- UI MUST label as weaker than QR pairing

## Threat Model

### In Scope

✅ **Eavesdropping**: Mitigated by TLS 1.3 encryption  
✅ **MITM on pairing**: Mitigated by QR code out-of-band fingerprint verification  
✅ **Replay attacks**: Mitigated by TLS sequence numbers + random tokens  
✅ **Data corruption**: Mitigated by per-chunk SHA-256  
✅ **Resume state tampering**: Mitigated by HMAC-SHA-256 tag  
✅ **Unpaired sender**: Receiver explicitly accepts or rejects  

### Out of Scope

⚠️ **Device compromise**: If private key is stolen, attacker can impersonate device  
⚠️ **QR code phishing**: User scans malicious QR (educate users)  
⚠️ **Post-transfer file security**: Files are plaintext on disk (OS handles encryption at rest)  
⚠️ **Network-level DoS**: Can be overwhelmed by connection floods (rate-limit connections)  
⚠️ **Physical access**: Attacker with device access can extract files  

## Secure Coding Practices

### Input Validation

ALL peer-provided data must be validated:

```kotlin
// Frame length validation
if (frameLength > MAX_CONTROL_FRAME) {
    throw ProtocolException("Frame too large")
}

// Index bounds checking
if (fileIndex >= manifest.entries.size) {
    throw ProtocolException("Invalid file index")
}

if (chunkIndex >= file.chunkCount) {
    throw ProtocolException("Invalid chunk index")
}

// Path validation (PROTOCOL.md section 10.2)
fun validatePath(path: String): Boolean {
    if (path.isEmpty() || path.startsWith("/")) return false
    if (path.contains("..") || path.contains("\\")) return false
    if (path.any { it.code < 0x20 }) return false // Control chars
    if (path.toByteArray(Charsets.UTF_8).size > 4096) return false
    return true
}
```

### Memory Safety

Prevent buffer overflows and DoS:

```kotlin
// ❌ Don't allocate before checking size
val buffer = ByteArray(frameLength) // Could be malicious large value!

// ✅ Do validate first
if (frameLength > MAX_CONTROL_FRAME) {
    throw ProtocolException("Frame too large")
}
val buffer = ByteArray(frameLength)

// ❌ Don't trust peer count
val entries = List(peerProvidedCount) { readEntry() }

// ✅ Do enforce limits
if (peerProvidedCount > MAX_MANIFEST_ENTRIES) {
    throw ProtocolException("Too many entries")
}
val entries = List(peerProvidedCount) { readEntry() }
```

### Timing Attacks

Use constant-time comparison for secrets:

```kotlin
// ❌ Don't use ==
if (submittedToken == expectedToken) { ... }

// ✅ Do use constant-time
if (constantTimeEquals(submittedToken, expectedToken)) { ... }
```

### Random Numbers

Always use `SecureRandom` for cryptographic purposes:

```kotlin
// ❌ Don't use Random()
val token = Random().nextBytes(16)

// ✅ Do use SecureRandom
val token = ByteArray(16)
SecureRandom().nextBytes(token)
```

## Privacy

### No Telemetry

- Zero outbound connections to non-local addresses
- No analytics, crash reporting, or usage tracking
- No advertising identifiers
- Packet capture verification required before release

### Data Minimization

- Certificates contain no personal information (CN = random UUID)
- Device name is user-chosen (default: "Android Device")
- Transfer history is local only (never synced)
- Discovery broadcasts minimal info: device_id, name, platform

### User Consent

- All transfers require explicit accept (except opted-in auto-accept for paired devices)
- Receiver always sees: sender identity, file count, total size, destination
- Files never opened or executed automatically

## Code Review Checklist

Before merging crypto/protocol code:

- [ ] TLS 1.3 enforced (no fallback to 1.2)
- [ ] Certificate fingerprints verified before trust
- [ ] All input validated (bounds, types, lengths)
- [ ] Secrets compared constant-time
- [ ] Random values from SecureRandom
- [ ] Frame sizes checked before allocation
- [ ] Chunk hashes verified before persisting
- [ ] Resume state HMAC validated
- [ ] No hardcoded keys, tokens, or secrets
- [ ] Errors don't leak sensitive info

## Testing

### Security Tests

```kotlin
@Test
fun `reject TLS 1_2 connections`() {
    val socket = createSocket()
    socket.enabledProtocols = arrayOf("TLSv1.2")
    
    assertThrows<SSLHandshakeException> {
        socket.startHandshake()
    }
}

@Test
fun `reject connection from unpaired peer`() {
    val unpairedFingerprint = "1234..."
    
    val result = connectionHandler.handle(unpairedFingerprint)
    
    assertTrue(result is ProtocolError)
    assertEquals(ErrorCode.NOT_PAIRED, result.code)
}

@Test
fun `constant time comparison takes same time for match and mismatch`() {
    val token1 = SecureRandom().nextBytes(16)
    val token2 = SecureRandom().nextBytes(16)
    
    val time1 = measureNanoTime {
        repeat(1000) { constantTimeEquals(token1, token1) }
    }
    
    val time2 = measureNanoTime {
        repeat(1000) { constantTimeEquals(token1, token2) }
    }
    
    // Allow 10% variance
    assertTrue(abs(time1 - time2).toDouble() / time1 < 0.1)
}
```

## Compliance

- **TLS 1.3**: RFC 8446
- **ECDSA P-256**: FIPS 186-4
- **SHA-256**: FIPS 180-4
- **X.509 Certificates**: RFC 5280
- **Mutual TLS**: RFC 8446 Section 4.4.2

#[[file:../../docs/PROTOCOL.md]]
