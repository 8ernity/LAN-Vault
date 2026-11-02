package com.example.lanvault.transfer

import android.content.Context
import android.media.MediaScannerConnection
import android.net.Uri
import android.os.Environment
import android.os.ParcelFileDescriptor
import androidx.documentfile.provider.DocumentFile
import com.example.lanvault.crypto.DeviceIdentity
import com.example.lanvault.crypto.IdentityManager
import com.example.lanvault.data.TrustedDevice
import com.example.lanvault.data.VaultDatabase
import com.example.lanvault.protocol.*
import kotlinx.coroutines.*
import org.json.JSONArray
import org.json.JSONObject
import java.io.*
import java.net.ServerSocket
import java.net.URLEncoder
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import javax.net.ssl.SSLServerSocket
import javax.net.ssl.SSLSocket

data class TransferProgress(
    val transferId: String,
    val direction: String,
    val bytesAcked: Long,
    val totalBytes: Long,
    val filesDone: Int,
    val totalFiles: Int,
    val speedBps: Long
)

data class SendSource(
    val name: String,
    val size: Long,
    val mtime: Long = System.currentTimeMillis(),
    val isDirectory: Boolean = false,
    val file: File? = null,
    val uri: Uri? = null
)

interface ChunkWriter {
    fun writeChunk(offset: Long, data: ByteArray)
    fun syncAndClose()
}

class FileChunkWriter(private val file: File) : ChunkWriter {
    private val raf = RandomAccessFile(file, "rw")
    override fun writeChunk(offset: Long, data: ByteArray) {
        synchronized(raf) {
            raf.seek(offset)
            raf.write(data)
        }
    }
    override fun syncAndClose() {
        try { raf.fd.sync() } catch (_: Exception) {}
        try { raf.close() } catch (_: Exception) {}
    }
}

class SafChunkWriter(private val pfd: ParcelFileDescriptor) : ChunkWriter {
    override fun writeChunk(offset: Long, data: ByteArray) {
        synchronized(pfd) {
            android.system.Os.pwrite(pfd.fileDescriptor, data, 0, data.size, offset)
        }
    }
    override fun syncAndClose() {
        try { android.system.Os.fsync(pfd.fileDescriptor) } catch (_: Exception) {}
        try { pfd.close() } catch (_: Exception) {}
    }
}

class TransferEngine(
    private val context: Context,
    private val identity: DeviceIdentity
) {
    private var serverJob: Job? = null
    private var serverSocket: ServerSocket? = null

    data class ActivePairingSession(
        val token: ByteArray,
        val tokenB64: String,
        val createdAt: Long = System.currentTimeMillis(),
        var attempts: Int = 0
    )

    @Volatile
    var activePairing: ActivePairingSession? = null

    private data class IncomingTransferSession(
        val transferId: String,
        val peerName: String,
        val peerFp: String,
        val streamToken: String,
        val chunkSize: Long,
        val entries: List<ManifestEntry>,
        val totalBytes: Long,
        val writers: Map<Int, ChunkWriter>,
        val bitmaps: Map<Int, ChunkBitmap>,
        val targetFiles: Map<Int, File> = emptyMap(),
        val partFiles: Map<Int, File> = emptyMap(),
        val safPartDocs: Map<Int, DocumentFile> = emptyMap(),
        val safFinalNames: Map<Int, String> = emptyMap(),
        val safParentDocs: Map<Int, DocumentFile> = emptyMap(),
        val controlOutput: OutputStream,
        val bytesReceived: AtomicLong = AtomicLong(0L),
        val filesCompleted: AtomicInteger = AtomicInteger(0),
        val startTime: Long = System.currentTimeMillis()
    ) {
        @Volatile
        private var isCompleted = false

        fun isAllComplete(): Boolean {
            for (entry in entries) {
                if (entry.type != "file" || entry.size == 0L) continue
                val bmp = bitmaps[entry.index]
                if (bmp == null || !bmp.isComplete()) {
                    return false
                }
            }
            return true
        }

        fun completeTransfer(context: Context) {
            if (isCompleted) return
            isCompleted = true

            // Flush and close all writers
            for ((_, writer) in writers) {
                try { writer.syncAndClose() } catch (_: Exception) {}
            }

            // Finalize SAF documents (atomic non-replace rename with collision suffixing)
            for ((idx, partDoc) in safPartDocs) {
                val desiredName = safFinalNames[idx] ?: continue
                val parentDoc = safParentDocs[idx]
                try {
                    val finalName = getAvailableSafName(parentDoc, desiredName)
                    partDoc.renameTo(finalName)
                } catch (_: Exception) {}
            }

            // Finalize standard files (atomic collision-safe rename)
            val completedPaths = mutableListOf<String>()
            for ((idx, part) in partFiles) {
                val target = targetFiles[idx]
                if (part.exists() && target != null) {
                    try {
                        val availableTarget = getAvailableFile(target)
                        part.renameTo(availableTarget)
                        val entry = entries.find { it.index == idx }
                        if (entry != null && entry.mtimeMs > 0) {
                            availableTarget.setLastModified(entry.mtimeMs)
                        }
                        completedPaths.add(availableTarget.absolutePath)
                    } catch (_: Exception) {}
                }
            }
            if (completedPaths.isNotEmpty()) {
                try {
                    MediaScannerConnection.scanFile(context, completedPaths.toTypedArray(), null, null)
                } catch (_: Exception) {}
            }
        }

        fun cleanupTransfer() {
            if (isCompleted) return
            for ((_, writer) in writers) {
                try { writer.syncAndClose() } catch (_: Exception) {}
            }
            for ((_, partDoc) in safPartDocs) {
                try { partDoc.delete() } catch (_: Exception) {}
            }
            for ((_, part) in partFiles) {
                try { if (part.exists()) part.delete() } catch (_: Exception) {}
            }
        }

        private fun getAvailableFile(target: File): File {
            if (!target.exists()) return target
            val parent = target.parentFile
            val name = target.nameWithoutExtension
            val ext = target.extension
            val extWithDot = if (ext.isNotEmpty()) ".$ext" else ""
            for (i in 1..9999) {
                val candidate = File(parent, "$name ($i)$extWithDot")
                if (!candidate.exists()) return candidate
            }
            return target
        }

        private fun getAvailableSafName(parentDoc: DocumentFile?, desiredName: String): String {
            if (parentDoc == null || parentDoc.findFile(desiredName) == null) return desiredName
            val nameWithoutExt = desiredName.substringBeforeLast('.')
            val ext = if (desiredName.contains('.')) "." + desiredName.substringAfterLast('.') else ""
            for (i in 1..9999) {
                val candidate = "$nameWithoutExt ($i)$ext"
                if (parentDoc.findFile(candidate) == null) return candidate
            }
            return desiredName
        }
    }

    private val activeIncoming = ConcurrentHashMap<String, IncomingTransferSession>()

    var onIncomingOffer: ((transferId: String, peerName: String, peerFp: String, fileCount: Int, totalBytes: Long, acceptCallback: (Boolean) -> Unit) -> Unit)? = null
    var onProgress: ((TransferProgress) -> Unit)? = null
    var onCompleted: ((transferId: String) -> Unit)? = null
    var onError: ((transferId: String, error: String) -> Unit)? = null

    fun generatePairingQR(localIps: List<String>, deviceName: String): String {
        val tokenBytes = ByteArray(16)
        SecureRandom().nextBytes(tokenBytes)
        val tokenB64 = Base64.getUrlEncoder().withoutPadding().encodeToString(tokenBytes)
        activePairing = ActivePairingSession(tokenBytes, tokenB64)
        val ipStr = localIps.joinToString(",")
        val encodedName = URLEncoder.encode(deviceName, "UTF-8")
        return "lanvault://pair?v=1&fp=${identity.fingerprint}&ip=$ipStr&port=${Protocol.DEFAULT_PORT}&name=$encodedName&tok=$tokenB64"
    }

    suspend fun pairWithPeer(qrText: String): Result<String> = withContext(Dispatchers.IO) {
        try {
            val uri = Uri.parse(qrText)
            val fp = uri.getQueryParameter("fp") ?: return@withContext Result.failure(IOException("Missing fp in QR URI"))
            val ipStr = uri.getQueryParameter("ip") ?: return@withContext Result.failure(IOException("Missing ip in QR URI"))
            val port = uri.getQueryParameter("port")?.toIntOrNull() ?: Protocol.DEFAULT_PORT
            val name = uri.getQueryParameter("name") ?: "LANVault Peer"
            val tok = uri.getQueryParameter("tok") ?: return@withContext Result.failure(IOException("Missing tok in QR URI"))
            val ips = ipStr.split(",").map { it.trim() }.filter { it.isNotEmpty() }

            var connectedSocket: SSLSocket? = null
            var lastErr: Exception? = null

            val sslContext = IdentityManager.createSSLContext(identity, fp)
            val sf = sslContext.socketFactory

            for (ip in ips) {
                try {
                    val s = sf.createSocket(ip, port) as SSLSocket
                    s.startHandshake()
                    connectedSocket = s
                    break
                } catch (e: Exception) {
                    lastErr = e
                }
            }
            val socket = connectedSocket ?: throw (lastErr ?: IOException("Failed to connect to pairing target on $ipStr"))
            val input = socket.inputStream
            val output = socket.outputStream

            try {
                // 1. Send HELLO
                val helloJson = JSONObject().apply {
                    put("version", JSONObject().apply {
                        put("major", Protocol.MAJOR_VERSION)
                        put("minor", Protocol.MINOR_VERSION)
                    })
                    put("device_id", identity.deviceId)
                    put("name", "Android Phone")
                    put("platform", "android")
                    put("role", "control")
                    put("capabilities", JSONArray().put("resume").put("multi-stream"))
                }.toString()
                Framing.writeJSONFrame(output, Protocol.FRAME_HELLO, helloJson)

                // 2. Read HELLO_OK
                val hOk = Framing.readFrame(input)
                if (hOk.type != Protocol.FRAME_HELLO_OK) {
                    throw IOException("Expected HELLO_OK, got ${hOk.type}")
                }

                // 3. Send PAIR_REQUEST
                val pairReq = JSONObject().apply {
                    put("token", tok)
                    put("name", "Android Phone")
                    put("platform", "android")
                }.toString()
                Framing.writeJSONFrame(output, Protocol.FRAME_PAIR_REQUEST, pairReq)

                // 4. Read PAIR_OK or ERROR
                val resp = Framing.readFrame(input)
                if (resp.type == Protocol.FRAME_PAIR_OK) {
                    val respJson = JSONObject(resp.payloadAsString())
                    val peerDevId = respJson.optString("device_id", fp.take(32))
                    val peerDevName = respJson.optString("name", name)

                    val db = VaultDatabase(context)
                    db.addTrustedDevice(
                        TrustedDevice(
                            fingerprint = fp,
                            deviceId = peerDevId,
                            name = peerDevName,
                            platform = "remote",
                            pairedAt = System.currentTimeMillis(),
                            lastSeen = System.currentTimeMillis(),
                            lastKnownIp = ips.firstOrNull() ?: ""
                        )
                    )
                    Result.success(peerDevName)
                } else if (resp.type == Protocol.FRAME_ERROR) {
                    val errJson = JSONObject(resp.payloadAsString())
                    Result.failure(IOException("Pairing failed: ${errJson.optString("message")}"))
                } else {
                    Result.failure(IOException("Unexpected pairing response frame: ${resp.type}"))
                }
            } finally {
                try { socket.close() } catch (_: Exception) {}
            }
        } catch (e: Exception) {
            Result.failure(e)
        }
    }

    fun startServer(scope: CoroutineScope) {
        if (serverJob != null) return
        serverJob = scope.launch(Dispatchers.IO) {
            try {
                val sslContext = IdentityManager.createSSLContext(identity)
                val ssf = sslContext.serverSocketFactory
                val s = ssf.createServerSocket(Protocol.DEFAULT_PORT) as SSLServerSocket
                s.needClientAuth = true
                serverSocket = s

                while (isActive) {
                    val clientSocket = s.accept() as SSLSocket
                    launch(Dispatchers.IO) {
                        handleIncomingConnection(clientSocket)
                    }
                }
            } catch (_: Exception) {}
        }
    }

    fun stopServer() {
        serverJob?.cancel()
        serverJob = null
        try {
            serverSocket?.close()
        } catch (_: Exception) {}
        for ((_, session) in activeIncoming) {
            session.cleanupTransfer()
        }
        activeIncoming.clear()
    }

    private suspend fun handleIncomingConnection(socket: SSLSocket) = withContext(Dispatchers.IO) {
        try {
            socket.startHandshake()
            val certs = socket.session.peerCertificates
            val peerCert = certs.firstOrNull() as? java.security.cert.X509Certificate
                ?: throw IOException("No peer certificate provided")
            val peerFp = IdentityManager.computeFingerprint(peerCert)

            val input = socket.inputStream
            val output = socket.outputStream

            // 1. Read HELLO
            val helloFrame = Framing.readFrame(input)
            if (helloFrame.type != Protocol.FRAME_HELLO) {
                throw IOException("Expected HELLO frame (0x01), got ${helloFrame.type}")
            }

            val helloJson = JSONObject(helloFrame.payloadAsString())
            val peerName = helloJson.optString("name", "LANVault Peer")
            val role = helloJson.optString("role", "control")
            val dataTransferId = helloJson.optString("transfer_id", "")
            val streamToken = helloJson.optString("stream_token", "")

            // Reply HELLO_OK
            val helloOkJson = JSONObject().apply {
                put("version", JSONObject().apply {
                    put("major", Protocol.MAJOR_VERSION)
                    put("minor", Protocol.MINOR_VERSION)
                })
                put("device_id", identity.deviceId)
                put("name", "Android Phone")
                put("platform", "android")
                put("paired", true)
            }.toString()
            Framing.writeJSONFrame(output, Protocol.FRAME_HELLO_OK, helloOkJson)

            if (role == "data") {
                // Route DATA stream with strict token and peer fingerprint validation
                val session = activeIncoming[dataTransferId]
                if (session == null || session.streamToken != streamToken || session.peerFp != peerFp) {
                    val errJson = JSONObject().apply {
                        put("code", Protocol.ERR_PROTOCOL_ERROR)
                        put("message", "Invalid data connection credentials")
                        put("fatal", true)
                    }.toString()
                    Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, errJson)
                    socket.close()
                    return@withContext
                }
                handleDataStream(session, input, output)
                return@withContext
            }

            // 2. Control Stream: Read next frame (OFFER or PAIR_REQUEST)
            val nextFrame = Framing.readFrame(input)
            if (nextFrame.type == Protocol.FRAME_PAIR_REQUEST) {
                val pairJson = JSONObject(nextFrame.payloadAsString())
                val receivedTok = pairJson.getString("token")
                val peerReqName = pairJson.optString("name", "LANVault Peer")
                val peerReqPlat = pairJson.optString("platform", "remote")

                val pairingSession = activePairing
                val now = System.currentTimeMillis()
                if (pairingSession == null || (now - pairingSession.createdAt) > Protocol.PAIR_TOKEN_TTL_S * 1000L || pairingSession.attempts >= Protocol.PAIR_MAX_FAILED_ATTEMPTS) {
                    activePairing = null
                    val errJson = JSONObject().apply {
                        put("code", Protocol.ERR_PAIR_TOKEN_INVALID)
                        put("message", "Pairing token expired or invalid")
                        put("fatal", true)
                    }.toString()
                    Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, errJson)
                    socket.close()
                    return@withContext
                }

                if (!MessageDigest.isEqual(receivedTok.toByteArray(StandardCharsets.UTF_8), pairingSession.tokenB64.toByteArray(StandardCharsets.UTF_8))) {
                    pairingSession.attempts++
                    val errJson = JSONObject().apply {
                        put("code", Protocol.ERR_PAIR_TOKEN_INVALID)
                        put("message", "Pairing token mismatch")
                        put("fatal", true)
                    }.toString()
                    Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, errJson)
                    socket.close()
                    return@withContext
                }

                // Token is valid! Invalidate single-use token
                activePairing = null
                val db = VaultDatabase(context)
                db.addTrustedDevice(
                    TrustedDevice(
                        fingerprint = peerFp,
                        deviceId = helloJson.optString("device_id", peerFp.take(32)),
                        name = peerReqName,
                        platform = peerReqPlat,
                        pairedAt = System.currentTimeMillis(),
                        lastSeen = System.currentTimeMillis()
                    )
                )

                val pairOkJson = JSONObject().apply {
                    put("device_id", identity.deviceId)
                    put("name", db.getSetting("device_name", "Android Phone"))
                }.toString()
                Framing.writeJSONFrame(output, Protocol.FRAME_PAIR_OK, pairOkJson)
                socket.close()
                return@withContext
            }

            if (nextFrame.type != Protocol.FRAME_OFFER) {
                throw IOException("Expected OFFER frame (0x10), got ${nextFrame.type}")
            }

            val offerJson = JSONObject(nextFrame.payloadAsString())
            val transferId = offerJson.getString("transfer_id")
            val chunkSize = offerJson.optLong("chunk_size", Protocol.CHUNK_SIZE_DEFAULT.toLong())
            val totalBytes = offerJson.optLong("total_bytes", 0L)
            val fileCount = offerJson.optInt("file_count", 0)

            // Validate OFFER parameters per specification
            if (chunkSize < Protocol.CHUNK_SIZE_MIN || chunkSize > Protocol.CHUNK_SIZE_MAX) {
                val errJson = JSONObject().apply {
                    put("code", Protocol.ERR_PROTOCOL_ERROR)
                    put("message", "chunk_size $chunkSize out of bounds")
                    put("fatal", true)
                }.toString()
                Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, errJson)
                socket.close()
                return@withContext
            }
            if (fileCount < 0 || fileCount > Protocol.MAX_MANIFEST_ENTRIES) {
                val errJson = JSONObject().apply {
                    put("code", Protocol.ERR_PATH_INVALID)
                    put("message", "file_count $fileCount out of bounds")
                    put("fatal", true)
                }.toString()
                Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, errJson)
                socket.close()
                return@withContext
            }
            if (totalBytes < 0) {
                val errJson = JSONObject().apply {
                    put("code", Protocol.ERR_PROTOCOL_ERROR)
                    put("message", "total_bytes cannot be negative")
                    put("fatal", true)
                }.toString()
                Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, errJson)
                socket.close()
                return@withContext
            }

            // 3. Read MANIFEST_PART frames until MANIFEST_END
            val entries = mutableListOf<ManifestEntry>()
            while (true) {
                val mf = Framing.readFrame(input)
                if (mf.type == Protocol.FRAME_MANIFEST_PART) {
                    val partJson = JSONObject(mf.payloadAsString())
                    val arr = partJson.optJSONArray("entries") ?: JSONArray()
                    for (i in 0 until arr.length()) {
                        val item = arr.getJSONObject(i)
                        entries.add(
                            ManifestEntry(
                                index = item.getInt("index"),
                                type = item.optString("type", "file"),
                                path = item.getString("path"),
                                size = item.optLong("size", 0L),
                                mtimeMs = item.optLong("mtime_ms", System.currentTimeMillis())
                            )
                        )
                    }
                } else if (mf.type == Protocol.FRAME_MANIFEST_END) {
                    val endJson = JSONObject(mf.payloadAsString())
                    val expectedHash = endJson.optString("manifest_hash", "")
                    val calcHash = Framing.computeManifestHash(entries)
                    if (expectedHash.isNotEmpty() && !expectedHash.equals(calcHash, ignoreCase = true)) {
                        throw IOException("Manifest hash mismatch: expected $expectedHash, got $calcHash")
                    }
                    break
                } else {
                    throw IOException("Unexpected frame during manifest: ${mf.type}")
                }
            }

            // 4. Prompt user for acceptance
            val acceptCompletable = CompletableDeferred<Boolean>()
            onIncomingOffer?.invoke(transferId, peerName, peerFp, fileCount, totalBytes) { accepted ->
                acceptCompletable.complete(accepted)
            }

            val accepted = acceptCompletable.await()
            if (!accepted) {
                val rejectJson = JSONObject().apply {
                    put("transfer_id", transferId)
                    put("code", Protocol.ERR_REJECTED)
                    put("message", "Declined by user")
                }.toString()
                Framing.writeJSONFrame(output, Protocol.FRAME_REJECT, rejectJson)
                socket.close()
                return@withContext
            }

            // 5. Pre-create save directories, files, and random-access chunk writers
            val db = VaultDatabase(context)
            val treeUriStr = db.getSetting("save_tree_uri", "")
            var safRoot: DocumentFile? = null
            if (treeUriStr.isNotBlank()) {
                try {
                    val treeUri = Uri.parse(treeUriStr)
                    val doc = DocumentFile.fromTreeUri(context, treeUri)
                    if (doc != null && doc.canWrite()) {
                        safRoot = doc
                    }
                } catch (_: Exception) {}
            }

            val targetFiles = mutableMapOf<Int, File>()
            val partFiles = mutableMapOf<Int, File>()
            val safPartDocs = mutableMapOf<Int, DocumentFile>()
            val safFinalNames = mutableMapOf<Int, String>()
            val safParentDocs = mutableMapOf<Int, DocumentFile>()
            val writers = mutableMapOf<Int, ChunkWriter>()
            val bitmaps = mutableMapOf<Int, ChunkBitmap>()

            if (safRoot != null) {
                for (entry in entries) {
                    val sanitized = entry.path.replace("..", "").replace("\\", "/").trimStart('/')
                    if (entry.type == "dir") {
                        findOrCreateDirectory(safRoot, sanitized)
                    } else {
                        val parentDoc = if (sanitized.contains("/")) {
                            findOrCreateDirectory(safRoot, sanitized.substringBeforeLast('/')) ?: safRoot
                        } else {
                            safRoot
                        }
                        val fileName = sanitized.substringAfterLast('/')
                        val partName = "$fileName.lvpart"
                        parentDoc.findFile(partName)?.delete()
                        val partDoc = parentDoc.createFile("application/octet-stream", partName)
                            ?: throw IOException("Failed to create file $fileName in selected directory")

                        val pfd = context.contentResolver.openFileDescriptor(partDoc.uri, "rw")
                            ?: throw IOException("Failed to open file descriptor for $fileName")

                        val numChunks = if (entry.size == 0L) 0 else ((entry.size + chunkSize - 1) / chunkSize).toInt()
                        safPartDocs[entry.index] = partDoc
                        safFinalNames[entry.index] = fileName
                        safParentDocs[entry.index] = parentDoc
                        writers[entry.index] = SafChunkWriter(pfd)
                        bitmaps[entry.index] = ChunkBitmap(numChunks)
                    }
                }
            } else {
                var saveDir = File(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS), "LANVault")
                if (!saveDir.exists() && !saveDir.mkdirs()) {
                    saveDir = context.getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS) ?: File(context.filesDir, "LANVault")
                    saveDir.mkdirs()
                }

                for (entry in entries) {
                    val sanitized = entry.path.replace("..", "").replace("\\", "/").trimStart('/')
                    val target = File(saveDir, sanitized)
                    if (entry.type == "dir") {
                        target.mkdirs()
                    } else {
                        target.parentFile?.mkdirs()
                        val part = File(saveDir, "$sanitized.lvpart")
                        if (part.exists()) part.delete()

                        val numChunks = if (entry.size == 0L) 0 else ((entry.size + chunkSize - 1) / chunkSize).toInt()
                        targetFiles[entry.index] = target
                        partFiles[entry.index] = part
                        writers[entry.index] = FileChunkWriter(part)
                        bitmaps[entry.index] = ChunkBitmap(numChunks)
                    }
                }
            }

            // Cryptographically secure 16-byte random stream token
            val tokenBytes = ByteArray(16)
            SecureRandom().nextBytes(tokenBytes)
            val streamTokenGenerated = Base64.getUrlEncoder().withoutPadding().encodeToString(tokenBytes)

            val session = IncomingTransferSession(
                transferId = transferId,
                peerName = peerName,
                peerFp = peerFp,
                streamToken = streamTokenGenerated,
                chunkSize = chunkSize,
                entries = entries,
                totalBytes = totalBytes,
                writers = writers,
                bitmaps = bitmaps,
                targetFiles = targetFiles,
                partFiles = partFiles,
                safPartDocs = safPartDocs,
                safFinalNames = safFinalNames,
                safParentDocs = safParentDocs,
                controlOutput = output
            )
            activeIncoming[transferId] = session

            // Send ACCEPT
            val acceptJson = JSONObject().apply {
                put("transfer_id", transferId)
                put("resume", false)
                put("stream_token", streamTokenGenerated)
                put("data_streams", 1)
            }.toString()
            Framing.writeJSONFrame(output, Protocol.FRAME_ACCEPT, acceptJson)

            // Read control connection frames until transfer finishes
            while (activeIncoming.containsKey(transferId)) {
                val ctrlFrame = try {
                    Framing.readFrame(input)
                } catch (_: Exception) {
                    break
                }
                if (ctrlFrame.type == Protocol.FRAME_DONE) {
                    if (session.isAllComplete()) {
                        session.completeTransfer(context)
                        val doneOkJson = JSONObject().apply {
                            put("transfer_id", session.transferId)
                            put("files_completed", session.entries.count { it.type == "file" })
                            put("bytes", session.bytesReceived.get())
                        }.toString()
                        Framing.writeJSONFrame(output, Protocol.FRAME_DONE_OK, doneOkJson)
                        activeIncoming.remove(session.transferId)
                        onCompleted?.invoke(session.transferId)
                    } else {
                        val errJson = JSONObject().apply {
                            put("code", Protocol.ERR_STATE_INTEGRITY)
                            put("message", "DONE received before all chunks completed")
                            put("fatal", true)
                        }.toString()
                        Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, errJson)
                        activeIncoming.remove(session.transferId)
                    }
                    break
                } else if (ctrlFrame.type == Protocol.FRAME_CANCEL) {
                    session.cleanupTransfer()
                    activeIncoming.remove(session.transferId)
                    onError?.invoke(session.transferId, "Transfer cancelled by sender")
                    break
                }
            }
        } catch (e: Exception) {
            onError?.invoke("", e.message ?: "Incoming connection error")
        } finally {
            try { socket.close() } catch (_: Exception) {}
        }
    }

    private fun findOrCreateDirectory(root: DocumentFile, relativePath: String): DocumentFile? {
        val segments = relativePath.split("/").filter { it.isNotEmpty() }
        var current = root
        for (seg in segments) {
            val existing = current.findFile(seg)
            current = if (existing != null && existing.isDirectory) {
                existing
            } else {
                current.createDirectory(seg) ?: return null
            }
        }
        return current
    }

    private fun handleDataStream(session: IncomingTransferSession, input: InputStream, output: OutputStream) {
        try {
            while (true) {
                val frame = Framing.readFrame(input)
                if (frame.type == Protocol.FRAME_DONE) {
                    if (session.isAllComplete()) {
                        session.completeTransfer(context)

                        val doneOkJson = JSONObject().apply {
                            put("transfer_id", session.transferId)
                            put("files_completed", session.entries.count { it.type == "file" })
                            put("bytes", session.bytesReceived.get())
                        }.toString()
                        try {
                            Framing.writeJSONFrame(session.controlOutput, Protocol.FRAME_DONE_OK, doneOkJson)
                        } catch (_: Exception) {}
                        try {
                            Framing.writeJSONFrame(output, Protocol.FRAME_DONE_OK, doneOkJson)
                        } catch (_: Exception) {}

                        activeIncoming.remove(session.transferId)
                        onCompleted?.invoke(session.transferId)
                    } else {
                        val errJson = JSONObject().apply {
                            put("code", Protocol.ERR_STATE_INTEGRITY)
                            put("message", "DONE received before all chunks completed")
                            put("fatal", true)
                        }.toString()
                        try { Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, errJson) } catch (_: Exception) {}
                    }
                    break
                }

                if (frame.type == Protocol.FRAME_DATA) {
                    val p = frame.payload
                    if (p.size >= Protocol.DATA_FRAME_OVERHEAD) {
                        val bb = ByteBuffer.wrap(p, 0, 8).order(ByteOrder.BIG_ENDIAN)
                        val fileIdx = bb.int
                        val chunkIdx = bb.int
                        val dataLen = p.size - 8 - 32
                        val data = p.copyOfRange(8, 8 + dataLen)
                        val expectedHash = p.copyOfRange(8 + dataLen, p.size)

                        val actualHash = MessageDigest.getInstance("SHA-256").digest(data)
                        if (actualHash.contentEquals(expectedHash)) {
                            val writer = session.writers[fileIdx]
                            val entry = session.entries.find { it.index == fileIdx }
                            if (writer != null && entry != null) {
                                val offset = chunkIdx.toLong() * session.chunkSize
                                writer.writeChunk(offset, data)
                                session.bitmaps[fileIdx]?.set(chunkIdx)
                                val currentBytes = session.bytesReceived.addAndGet(dataLen.toLong())

                                // Send CHUNK_ACK on control stream
                                val ackPayload = Framing.encodeIndexPairs(listOf(Pair(fileIdx, chunkIdx)))
                                try {
                                    Framing.writeRawFrame(session.controlOutput, Protocol.FRAME_CHUNK_ACK, ackPayload)
                                } catch (_: Exception) {}

                                // If file completed, send FILE_DONE
                                if (session.bitmaps[fileIdx]?.isComplete() == true) {
                                    session.filesCompleted.incrementAndGet()
                                    val fileDoneJson = JSONObject().apply {
                                        put("index", fileIdx)
                                    }.toString()
                                    try {
                                        Framing.writeJSONFrame(session.controlOutput, Protocol.FRAME_FILE_DONE, fileDoneJson)
                                    } catch (_: Exception) {}
                                }

                                val elapsed = (System.currentTimeMillis() - session.startTime) / 1000L
                                val speed = if (elapsed > 0) currentBytes / elapsed else 0L

                                onProgress?.invoke(
                                    TransferProgress(
                                        transferId = session.transferId,
                                        direction = "receive",
                                        bytesAcked = currentBytes,
                                        totalBytes = session.totalBytes,
                                        filesDone = session.filesCompleted.get(),
                                        totalFiles = session.entries.count { it.type == "file" },
                                        speedBps = speed
                                    )
                                )
                            }
                        } else {
                            // Hash mismatch: request retransmission with CHUNK_NEED
                            val needPayload = Framing.encodeIndexPairs(listOf(Pair(fileIdx, chunkIdx)))
                            try {
                                Framing.writeRawFrame(session.controlOutput, Protocol.FRAME_CHUNK_NEED, needPayload)
                            } catch (_: Exception) {}
                        }
                    }
                }
            }
        } catch (_: Exception) {
        }
    }

    suspend fun startSend(
        targetIp: String,
        targetPort: Int,
        files: List<File>,
        peerFingerprint: String? = null
    ): String {
        val sources = files.map { file ->
            SendSource(
                name = file.name,
                size = file.length(),
                mtime = file.lastModified(),
                isDirectory = file.isDirectory,
                file = file
            )
        }
        return startSendSources(targetIp, targetPort, sources, peerFingerprint)
    }

    suspend fun startSendSources(
        targetIp: String,
        targetPort: Int,
        sources: List<SendSource>,
        peerFingerprint: String? = null
    ): String = withContext(Dispatchers.IO) {
        val transferId = java.util.UUID.randomUUID().toString().replace("-", "").take(16)
        val sslContext = IdentityManager.createSSLContext(identity, peerFingerprint)
        val sf = sslContext.socketFactory
        val socket = sf.createSocket(targetIp, targetPort) as SSLSocket
        socket.startHandshake()

        val certs = socket.session.peerCertificates
        val peerCert = certs.firstOrNull() as? java.security.cert.X509Certificate
            ?: throw IOException("No peer certificate provided by receiver")
        val actualPeerFp = IdentityManager.computeFingerprint(peerCert)
        if (!peerFingerprint.isNullOrBlank() && !peerFingerprint.equals(actualPeerFp, ignoreCase = true)) {
            throw IOException("Peer fingerprint mismatch: expected $peerFingerprint, got $actualPeerFp")
        }

        val input = socket.inputStream
        val output = socket.outputStream

        try {
            // 1. Send HELLO
            val helloJson = JSONObject().apply {
                put("version", JSONObject().apply {
                    put("major", Protocol.MAJOR_VERSION)
                    put("minor", Protocol.MINOR_VERSION)
                })
                put("device_id", identity.deviceId)
                put("name", "Android Phone")
                put("platform", "android")
                put("role", "control")
                put("capabilities", JSONArray().put("resume").put("multi-stream"))
            }.toString()
            Framing.writeJSONFrame(output, Protocol.FRAME_HELLO, helloJson)

            // Read HELLO_OK
            val hOkFrame = Framing.readFrame(input)
            if (hOkFrame.type != Protocol.FRAME_HELLO_OK) {
                throw IOException("Expected HELLO_OK (0x02), got ${hOkFrame.type}")
            }

            // 2. Build entries and manifest hash
            val entries = mutableListOf<ManifestEntry>()
            var totalBytes = 0L
            sources.forEachIndexed { index, src ->
                totalBytes += src.size
                entries.add(
                    ManifestEntry(
                        index = index,
                        type = if (src.isDirectory) "dir" else "file",
                        path = src.name,
                        size = src.size,
                        mtimeMs = src.mtime
                    )
                )
            }
            val manifestHash = Framing.computeManifestHash(entries)

            // 3. Send OFFER
            val offerJson = JSONObject().apply {
                put("transfer_id", transferId)
                put("chunk_size", Protocol.CHUNK_SIZE_DEFAULT)
                put("file_count", entries.size)
                put("dir_count", 0)
                put("total_bytes", totalBytes)
                put("data_streams", 1)
            }.toString()
            Framing.writeJSONFrame(output, Protocol.FRAME_OFFER, offerJson)

            // 4. Send MANIFEST_PART and MANIFEST_END
            val partJson = JSONObject().apply {
                val arr = JSONArray()
                for (e in entries) {
                    arr.put(JSONObject().apply {
                        put("index", e.index)
                        put("type", e.type)
                        put("path", e.path)
                        put("size", e.size)
                        put("mtime_ms", e.mtimeMs)
                    })
                }
                put("entries", arr)
            }.toString()
            Framing.writeJSONFrame(output, Protocol.FRAME_MANIFEST_PART, partJson)

            val endJson = JSONObject().apply {
                put("entry_count", entries.size)
                put("manifest_hash", manifestHash)
            }.toString()
            Framing.writeJSONFrame(output, Protocol.FRAME_MANIFEST_END, endJson)

            // 5. Read response (ACCEPT or REJECT)
            val respFrame = Framing.readFrame(input)
            if (respFrame.type == Protocol.FRAME_REJECT) {
                val rej = JSONObject(respFrame.payloadAsString())
                throw IOException("Transfer declined by recipient: ${rej.optString("message", "User declined")}")
            }
            if (respFrame.type != Protocol.FRAME_ACCEPT) {
                throw IOException("Expected ACCEPT (0x13), got ${respFrame.type}")
            }
            val acceptJson = JSONObject(respFrame.payloadAsString())
            val streamToken = acceptJson.optString("stream_token", "")

            // 6. Open parallel Data Connection pinned to verified peer cert
            val dataSocket = sf.createSocket(targetIp, targetPort) as SSLSocket
            dataSocket.startHandshake()
            val dataIn = dataSocket.inputStream
            val dataOut = dataSocket.outputStream

            val dataHelloJson = JSONObject().apply {
                put("version", JSONObject().apply {
                    put("major", Protocol.MAJOR_VERSION)
                    put("minor", Protocol.MINOR_VERSION)
                })
                put("device_id", identity.deviceId)
                put("name", "Android Phone")
                put("platform", "android")
                put("role", "data")
                put("transfer_id", transferId)
                put("stream_token", streamToken)
            }.toString()
            Framing.writeJSONFrame(dataOut, Protocol.FRAME_HELLO, dataHelloJson)

            val dataHelloOk = Framing.readFrame(dataIn)
            if (dataHelloOk.type != Protocol.FRAME_HELLO_OK) {
                throw IOException("Expected HELLO_OK on data connection, got ${dataHelloOk.type}")
            }

            // 7. Stream DATA chunks with ACK tracking
            val chunkSize = Protocol.CHUNK_SIZE_DEFAULT
            val buffer = ByteArray(chunkSize)
            val startTime = System.currentTimeMillis()
            var bytesAcked = 0L
            var filesCompletedCount = 0

            for ((fileIdx, src) in sources.withIndex()) {
                if (src.isDirectory || src.size == 0L) continue
                val inputStream = if (src.file != null) {
                    FileInputStream(src.file)
                } else if (src.uri != null) {
                    context.contentResolver.openInputStream(src.uri)
                } else null
                if (inputStream == null) continue

                var chunkIdx = 0
                try {
                    while (true) {
                        val n = inputStream.read(buffer)
                        if (n <= 0) break
                        val chunkData = if (n == buffer.size) buffer else buffer.copyOf(n)
                        val payload = Framing.encodeDataFramePayload(fileIdx, chunkIdx, chunkData)
                        Framing.writeRawFrame(dataOut, Protocol.FRAME_DATA, payload)
                        bytesAcked += n
                        chunkIdx++

                        val elapsed = (System.currentTimeMillis() - startTime) / 1000L
                        val speed = if (elapsed > 0) bytesAcked / elapsed else 0L

                        onProgress?.invoke(
                            TransferProgress(
                                transferId = transferId,
                                direction = "send",
                                bytesAcked = bytesAcked,
                                totalBytes = totalBytes,
                                filesDone = filesCompletedCount,
                                totalFiles = sources.size,
                                speedBps = speed
                            )
                        )
                    }
                    filesCompletedCount++
                } finally {
                    try { inputStream.close() } catch (_: Exception) {}
                }
            }

            // 8. Send DONE frame
            val doneJson = JSONObject().apply {
                put("transfer_id", transferId)
            }.toString()
            try { Framing.writeJSONFrame(dataOut, Protocol.FRAME_DONE, doneJson) } catch (_: Exception) {}
            Framing.writeJSONFrame(output, Protocol.FRAME_DONE, doneJson)

            // 9. Read DONE_OK
            val doneOkFrame = Framing.readFrame(input)
            if (doneOkFrame.type == Protocol.FRAME_DONE_OK) {
                onCompleted?.invoke(transferId)
            }

            try { dataSocket.close() } catch (_: Exception) {}
            return@withContext transferId
        } finally {
            try { socket.close() } catch (_: Exception) {}
        }
    }
}
