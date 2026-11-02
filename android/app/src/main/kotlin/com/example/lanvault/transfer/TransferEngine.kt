package com.example.lanvault.transfer

import android.content.Context
import android.media.MediaScannerConnection
import android.net.Uri
import android.os.Environment
import androidx.documentfile.provider.DocumentFile
import com.example.lanvault.crypto.DeviceIdentity
import com.example.lanvault.crypto.IdentityManager
import com.example.lanvault.data.VaultDatabase
import com.example.lanvault.protocol.*
import kotlinx.coroutines.*
import org.json.JSONArray
import org.json.JSONObject
import java.io.*
import java.net.ServerSocket
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
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

class TransferEngine(
    private val context: Context,
    private val identity: DeviceIdentity
) {
    private var serverJob: Job? = null
    private var serverSocket: ServerSocket? = null

    private data class IncomingTransferSession(
        val transferId: String,
        val peerName: String,
        val peerFp: String,
        val entries: List<ManifestEntry>,
        val totalBytes: Long,
        val targetFiles: Map<Int, File> = emptyMap(),
        val partFiles: Map<Int, File> = emptyMap(),
        val safPartDocs: Map<Int, DocumentFile> = emptyMap(),
        val safFinalNames: Map<Int, String> = emptyMap(),
        val safParentDocs: Map<Int, DocumentFile> = emptyMap(),
        val openFiles: Map<Int, OutputStream>,
        val controlOutput: OutputStream,
        var bytesReceived: Long = 0L,
        val startTime: Long = System.currentTimeMillis()
    ) {
        @Volatile
        private var isCompleted = false

        fun completeTransfer(context: Context) {
            if (isCompleted) return
            isCompleted = true
            for ((_, stream) in openFiles) {
                try { stream.close() } catch (_: Exception) {}
            }
            for ((idx, partDoc) in safPartDocs) {
                val finalName = safFinalNames[idx] ?: continue
                val parentDoc = safParentDocs[idx]
                try {
                    parentDoc?.findFile(finalName)?.delete()
                    partDoc.renameTo(finalName)
                } catch (_: Exception) {}
            }
            val completedPaths = mutableListOf<String>()
            for ((idx, part) in partFiles) {
                val target = targetFiles[idx]
                if (part.exists() && target != null) {
                    try {
                        part.renameTo(target)
                        completedPaths.add(target.absolutePath)
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
            for ((_, stream) in openFiles) {
                try { stream.close() } catch (_: Exception) {}
            }
            for ((_, partDoc) in safPartDocs) {
                try { partDoc.delete() } catch (_: Exception) {}
            }
            for ((_, part) in partFiles) {
                try { if (part.exists()) part.delete() } catch (_: Exception) {}
            }
        }
    }

    private val activeIncoming = ConcurrentHashMap<String, IncomingTransferSession>()

    var onIncomingOffer: ((transferId: String, peerName: String, peerFp: String, fileCount: Int, totalBytes: Long, acceptCallback: (Boolean) -> Unit) -> Unit)? = null
    var onProgress: ((TransferProgress) -> Unit)? = null
    var onCompleted: ((transferId: String) -> Unit)? = null
    var onError: ((transferId: String, error: String) -> Unit)? = null

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
            } catch (e: Exception) {
                // Log and allow clean recovery
            }
        }
    }

    fun stopServer() {
        serverJob?.cancel()
        serverJob = null
        try {
            serverSocket?.close()
        } catch (_: Exception) {}
        for ((_, session) in activeIncoming) {
            for ((_, fos) in session.openFiles) {
                try { fos.close() } catch (_: Exception) {}
            }
        }
        activeIncoming.clear()
    }

    private suspend fun handleIncomingConnection(socket: SSLSocket) = withContext(Dispatchers.IO) {
        try {
            socket.startHandshake()
            val certs = socket.session.peerCertificates
            val peerCert = certs.firstOrNull() as? java.security.cert.X509Certificate
                ?: throw IOException("No peer certificate")
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
                // Handle DATA stream
                handleDataStream(dataTransferId, input, output)
                return@withContext
            }

            // 2. Control Stream: Read OFFER
            val offerFrame = Framing.readFrame(input)
            if (offerFrame.type != Protocol.FRAME_OFFER) {
                throw IOException("Expected OFFER frame (0x10), got ${offerFrame.type}")
            }
            val offerJson = JSONObject(offerFrame.payloadAsString())
            val transferId = offerJson.getString("transfer_id")
            val totalBytes = offerJson.optLong("total_bytes", 0L)
            val fileCount = offerJson.optInt("file_count", 0)

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
                    put("code", 1)
                    put("message", "Declined by user")
                }.toString()
                Framing.writeJSONFrame(output, Protocol.FRAME_REJECT, rejectJson)
                socket.close()
                return@withContext
            }

            // 5. Pre-create save directories and files (SAF or File fallback)
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
            val openFiles = mutableMapOf<Int, OutputStream>()

            if (safRoot != null) {
                // User-selected SAF folder
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
                        val outStream = context.contentResolver.openOutputStream(partDoc.uri, "wt")
                            ?: throw IOException("Failed to open stream for $fileName")

                        safPartDocs[entry.index] = partDoc
                        safFinalNames[entry.index] = fileName
                        safParentDocs[entry.index] = parentDoc
                        openFiles[entry.index] = outStream
                    }
                }
            } else {
                // Fallback: Downloads/LANVault
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
                        targetFiles[entry.index] = target
                        partFiles[entry.index] = part
                        openFiles[entry.index] = FileOutputStream(part)
                    }
                }
            }

            val session = IncomingTransferSession(
                transferId = transferId,
                peerName = peerName,
                peerFp = peerFp,
                entries = entries,
                totalBytes = totalBytes,
                targetFiles = targetFiles,
                partFiles = partFiles,
                safPartDocs = safPartDocs,
                safFinalNames = safFinalNames,
                safParentDocs = safParentDocs,
                openFiles = openFiles,
                controlOutput = output
            )
            activeIncoming[transferId] = session

            // Send ACCEPT
            val streamToken = java.util.UUID.randomUUID().toString().replace("-", "").take(16)
            val acceptJson = JSONObject().apply {
                put("transfer_id", transferId)
                put("resume", false)
                put("stream_token", streamToken)
                put("data_streams", 1)
            }.toString()
            Framing.writeJSONFrame(output, Protocol.FRAME_ACCEPT, acceptJson)

            // Read control connection frames (e.g. DONE, CANCEL) until transfer finishes
            while (activeIncoming.containsKey(transferId)) {
                val ctrlFrame = try {
                    Framing.readFrame(input)
                } catch (_: Exception) {
                    break
                }
                if (ctrlFrame.type == Protocol.FRAME_DONE) {
                    session.completeTransfer(context)
                    val doneOkJson = JSONObject().apply {
                        put("transfer_id", session.transferId)
                        put("files_completed", session.entries.size)
                        put("bytes", session.bytesReceived)
                    }.toString()
                    try {
                        Framing.writeJSONFrame(output, Protocol.FRAME_DONE_OK, doneOkJson)
                    } catch (_: Exception) {}
                    activeIncoming.remove(session.transferId)
                    onCompleted?.invoke(session.transferId)
                    break
                } else if (ctrlFrame.type == Protocol.FRAME_ERROR) {
                    session.cleanupTransfer()
                    activeIncoming.remove(session.transferId)
                    break
                }
            }
        } catch (e: Exception) {
            onError?.invoke("", e.message ?: "Transfer error")
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

    private fun handleDataStream(transferId: String, input: InputStream, output: OutputStream) {
        val session = activeIncoming[transferId] ?: activeIncoming.values.firstOrNull()
        if (session == null) {
            return
        }

        try {
            while (true) {
                val frame = Framing.readFrame(input)
                if (frame.type == Protocol.FRAME_DONE) {
                    session.completeTransfer(context)

                    // Send DONE_OK to control and data streams
                    val doneOkJson = JSONObject().apply {
                        put("transfer_id", session.transferId)
                        put("files_completed", session.entries.size)
                        put("bytes", session.bytesReceived)
                    }.toString()
                    try {
                        Framing.writeJSONFrame(session.controlOutput, Protocol.FRAME_DONE_OK, doneOkJson)
                    } catch (_: Exception) {}
                    try {
                        Framing.writeJSONFrame(output, Protocol.FRAME_DONE_OK, doneOkJson)
                    } catch (_: Exception) {}

                    activeIncoming.remove(session.transferId)
                    onCompleted?.invoke(session.transferId)
                    break
                }

                if (frame.type == Protocol.FRAME_DATA) {
                    // Payload format: fileIndex (u32, 4) + chunkIndex (u32, 4) + data (N) + sha256 (32)
                    val p = frame.payload
                    if (p.size >= 40) {
                        val bb = ByteBuffer.wrap(p, 0, 8).order(ByteOrder.BIG_ENDIAN)
                        val fileIdx = bb.int
                        val chunkIdx = bb.int
                        val dataLen = p.size - 8 - 32
                        val data = p.copyOfRange(8, 8 + dataLen)
                        val expectedHash = p.copyOfRange(8 + dataLen, p.size)

                        val actualHash = MessageDigest.getInstance("SHA-256").digest(data)
                        if (actualHash.contentEquals(expectedHash)) {
                            val fos = session.openFiles[fileIdx]
                            if (fos != null) {
                                fos.write(data)
                                fos.flush()
                                session.bytesReceived += dataLen

                                val elapsed = (System.currentTimeMillis() - session.startTime) / 1000L
                                val speed = if (elapsed > 0) session.bytesReceived / elapsed else 0L

                                onProgress?.invoke(
                                    TransferProgress(
                                        transferId = session.transferId,
                                        direction = "receive",
                                        bytesAcked = session.bytesReceived,
                                        totalBytes = session.totalBytes,
                                        filesDone = session.entries.size,
                                        totalFiles = session.entries.size,
                                        speedBps = speed
                                    )
                                )
                            }
                        }
                    }
                }
            }
        } catch (_: Exception) {
            session.cleanupTransfer()
        } finally {
            session.cleanupTransfer()
            activeIncoming.remove(session.transferId)
        }
    }

    suspend fun startSend(
        targetIp: String,
        targetPort: Int,
        files: List<File>,
        peerFingerprint: String? = null
    ): String = withContext(Dispatchers.IO) {
        val transferId = java.util.UUID.randomUUID().toString().replace("-", "").take(16)
        val sslContext = IdentityManager.createSSLContext(identity)
        val sf = sslContext.socketFactory
        val socket = sf.createSocket(targetIp, targetPort) as SSLSocket
        socket.startHandshake()

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
            files.forEachIndexed { index, file ->
                totalBytes += file.length()
                entries.add(
                    ManifestEntry(
                        index = index,
                        type = if (file.isDirectory) "dir" else "file",
                        path = file.name,
                        size = file.length(),
                        mtimeMs = file.lastModified()
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

            // 6. Open parallel Data Connection
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

            // 7. Stream DATA chunks
            var bytesAcked = 0L
            val chunkSize = Protocol.CHUNK_SIZE_DEFAULT
            val buffer = ByteArray(chunkSize)
            val startTime = System.currentTimeMillis()

            for ((fileIdx, file) in files.withIndex()) {
                if (file.isDirectory || file.length() == 0L) continue
                val fis = FileInputStream(file)
                var chunkIdx = 0
                try {
                    while (true) {
                        val n = fis.read(buffer)
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
                                filesDone = fileIdx,
                                totalFiles = files.size,
                                speedBps = speed
                            )
                        )
                    }
                } finally {
                    fis.close()
                }
            }

            // 8. Send DONE frame on both data and control connections
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
