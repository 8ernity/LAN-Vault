package com.example.lanvault.crypto

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.math.BigInteger
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PrivateKey
import java.security.Signature
import java.security.cert.X509Certificate
import java.security.spec.ECGenParameterSpec
import java.util.Date
import javax.security.auth.x500.X500Principal

object SelfSignedCertHelper {
    private const val ANDROID_KEYSTORE = "AndroidKeyStore"
    private const val ALIAS = "lanvault_identity_v2"

    fun getOrGenerateKeyStoreIdentity(deviceId: String): Pair<X509Certificate, PrivateKey> {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }

        // Clean up any legacy incompatible keys
        try {
            if (keyStore.containsAlias("lanvault_ecdsa_identity")) {
                keyStore.deleteEntry("lanvault_ecdsa_identity")
            }
        } catch (_: Exception) {}

        if (keyStore.containsAlias(ALIAS)) {
            try {
                val cert = keyStore.getCertificate(ALIAS) as? X509Certificate
                val key = keyStore.getKey(ALIAS, null) as? PrivateKey
                if (cert != null && key != null) {
                    // Verify key is compatible with Conscrypt's raw digest signing (NONEwithECDSA)
                    val testSig = Signature.getInstance("NONEwithECDSA")
                    testSig.initSign(key)
                    testSig.update(ByteArray(32))
                    testSig.sign()
                    return Pair(cert, key)
                }
            } catch (_: Exception) {
                // If previous key cannot sign raw digests (lacks DIGEST_NONE), wipe and regenerate
                try { keyStore.deleteEntry(ALIAS) } catch (_: Exception) {}
            }
        }

        val kpg = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, ANDROID_KEYSTORE)
        val now = System.currentTimeMillis()
        val notBefore = Date(now - 60_000L)
        val notAfter = Date(now + 1000L * 60 * 60 * 24 * 365 * 10) // 10 years

        val spec = KeyGenParameterSpec.Builder(
            ALIAS,
            KeyProperties.PURPOSE_SIGN or KeyProperties.PURPOSE_VERIFY
        )
            .setDigests(
                KeyProperties.DIGEST_NONE,
                KeyProperties.DIGEST_SHA256,
                KeyProperties.DIGEST_SHA384,
                KeyProperties.DIGEST_SHA512
            )
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
            .setCertificateSubject(X500Principal("CN=LANVault-$deviceId"))
            .setCertificateSerialNumber(BigInteger.valueOf(now))
            .setCertificateNotBefore(notBefore)
            .setCertificateNotAfter(notAfter)
            .build()

        kpg.initialize(spec)
        kpg.generateKeyPair()

        val cert = keyStore.getCertificate(ALIAS) as X509Certificate
        val key = keyStore.getKey(ALIAS, null) as PrivateKey
        return Pair(cert, key)
    }

    fun createSelfSigned(keyPair: KeyPair, dn: String, notBefore: Date, notAfter: Date): X509Certificate {
        val (cert, _) = getOrGenerateKeyStoreIdentity("default")
        return cert
    }
}
