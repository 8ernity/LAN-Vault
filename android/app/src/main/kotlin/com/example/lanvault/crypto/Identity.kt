package com.example.lanvault.crypto

import android.content.Context
import java.net.Socket
import java.security.*
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import java.util.*
import javax.net.ssl.*

data class DeviceIdentity(
    val deviceId: String,
    val certificate: X509Certificate,
    val privateKey: PrivateKey,
    val fingerprint: String,
    val shortFingerprint: String
)

object IdentityManager {
    private const val PREFS_NAME = "lanvault_identity"
    private const val KEY_DEVICE_ID = "device_id"

    @Volatile
    private var cachedIdentity: DeviceIdentity? = null

    fun getOrCreateIdentity(context: Context): DeviceIdentity {
        cachedIdentity?.let { return it }

        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        var devId = prefs.getString(KEY_DEVICE_ID, null)
        if (devId == null) {
            devId = UUID.randomUUID().toString()
            prefs.edit().putString(KEY_DEVICE_ID, devId).apply()
        }

        val (cert, key) = SelfSignedCertHelper.getOrGenerateKeyStoreIdentity(devId)
        val fp = computeFingerprint(cert)
        val ident = DeviceIdentity(devId, cert, key, fp, fp.take(8))
        cachedIdentity = ident
        return ident
    }

    fun computeFingerprint(cert: X509Certificate): String {
        val md = MessageDigest.getInstance("SHA-256")
        val digest = md.digest(cert.encoded)
        val sb = StringBuilder(digest.size * 2)
        for (b in digest) {
            sb.append(String.format("%02x", b))
        }
        return sb.toString()
    }

    fun createSSLContext(identity: DeviceIdentity): SSLContext {
        val keyManager = object : X509ExtendedKeyManager() {
            override fun getClientAliases(keyType: String?, issuers: Array<out Principal>?): Array<String> = arrayOf("lanvault")
            override fun chooseClientAlias(keyType: Array<out String>?, issuers: Array<out Principal>?, socket: Socket?): String = "lanvault"
            override fun getServerAliases(keyType: String?, issuers: Array<out Principal>?): Array<String> = arrayOf("lanvault")
            override fun chooseServerAlias(keyType: String?, issuers: Array<out Principal>?, socket: Socket?): String = "lanvault"
            override fun chooseEngineClientAlias(keyType: Array<out String>?, issuers: Array<out Principal>?, engine: SSLEngine?): String = "lanvault"
            override fun chooseEngineServerAlias(keyType: String?, issuers: Array<out Principal>?, engine: SSLEngine?): String = "lanvault"
            override fun getCertificateChain(alias: String?): Array<X509Certificate> = arrayOf(identity.certificate)
            override fun getPrivateKey(alias: String?): PrivateKey = identity.privateKey
        }

        val trustManager = object : X509TrustManager {
            override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) {
                if (chain.isNullOrEmpty()) throw CertificateException("Missing client certificate")
                // Accept any valid certificate at TLS layer; verification happens in app layer against trust store
            }

            override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {
                if (chain.isNullOrEmpty()) throw CertificateException("Missing server certificate")
                // Accept any valid certificate at TLS layer; verification happens in app layer against trust store
            }

            override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
        }

        val sslContext = SSLContext.getInstance("TLSv1.3")
        sslContext.init(arrayOf(keyManager), arrayOf(trustManager), SecureRandom())
        return sslContext
    }
}
