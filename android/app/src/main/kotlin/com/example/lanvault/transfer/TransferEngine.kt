package com.example.lanvault.transfer

import android.content.Context
import android.media.MediaScannerConnection
import android.net.Uri
import android.os.Environment
import android.os.ParcelFileDescriptor
import androidx.documentfile.provider.DocumentFile
import com.example.lanvault.crypto.DeviceIdentity
import com.example.lanvault.crypto.IdentityManager
import com.example.lanvault.data.StoredFileState
import com.example.lanvault.data.TransferRecord
import com.example.lanvault.data.TrustedDevice
import com.example.lanvault.data.VaultDatabase
import com.example.lanvault.protocol.*
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
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
import java.util.UUID
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
    fun sync()
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

    override fun sync() {
        synchronized(raf) {
            raf.fd.sync()
        }
    }

    override fun syncAndClose() {
        try { synchronized(raf) { raf.fd.sync() } } finally {
            try { raf.close() } catch (_: Exception) {}
        }
    }
}

class SafChunkWriter(private val pfd: ParcelFileDescriptor) : ChunkWriter {
    override fun writeChunk(offset: Long, data: ByteArray) {
        synchronized(pfd) {
            var written = 0
            while (written < data.size) {
                val n = android.system.Os.pwrite(pfd.fileDescriptor, data, written, data.size - written, offset + written)
                if (n <= 0) throw IOException("pwrite returned $n")
                written += n
            }
        }
    }

    override fun sync() {
        synchronized(pfd) {
            android.system.Os.fsync(pfd.fileDescriptor)
        }
    }

    override fun syncAndClose() {
        try { synchronized(pfd) { android.system.Os.fsync(pfd.fileDescriptor) } } finally {
            try { pfd.close() } catch (_: Exception) {}
        }
    }
}

private sealed class FinalizeOutcome {
    object Done : FinalizeOutcome()
    object Retry : FinalizeOutcome()
    data class Failed(val reason: String) : FinalizeOutcome()
}

private enum class DestinationMode { FILE, SAF }

private class IncomingTransferSession(
    val context: Context,
    val db: VaultDatabase,
    val transferId: String,
    val peerName: String,
    val peerFp: String,
    val streamToken: String,
    val chunkSize: Long,
    val entries: List<ManifestEntry>,
    val plannedPaths: List<String>,
    val totalBytes: Long,
    val mode: DestinationMode,
    val fileRoot: File?,
    val safRoot: DocumentFile?,
    val controlOutput: OutputStream,
    val createdAt: Long,
    val onProgress: ((TransferProgress) -> Unit)?,
    val onCompleted: ((String) -> Unit)?,
    val onError: ((String, String) -> Unit)?
) {
    val entriesByIndex: Map<Int, ManifestEntry> = entries.associateBy { it.index }
    val writers = HashMap<Int, ChunkWriter>()
    val bitmaps = HashMap<Int, ChunkBitmap>()
    val targetFiles = HashMap<Int, File>()
    val partFiles = HashMap<Int, File>()
    val safPartDocs = HashMap<Int, DocumentFile>()
    val safFinalDocs = HashMap<Int, DocumentFile>()
    val safFinalNames = HashMap<Int, String>()
    val safParentDocs = HashMap<Int, DocumentFile>()
    val finalized = HashSet<Int>()
    val alreadyFinal = HashSet<Int>()
    val failed = HashMap<Int, String>()
    val priorChunks = HashMap<Int, MutableSet<Int>>()
    val needAttempts = HashMap<String, Int>()
    private val hashCache = HashMap<Int, Map<Int, ByteArray>>()
    val bytesReceived = AtomicLong(0L)
    val filesCompleted = AtomicInteger(0)
    private val lock = Any()

    private var doneReceived = false
    private var donePending = false
    private var completing = false
    @Volatile
    private var terminal = false
    private var cancelled = false
    private val startTime = System.currentTimeMillis()

    fun isTerminal(): Boolean = terminal

    fun chunkCount(entry: ManifestEntry): Int =
        if (entry.size <= 0L) 0 else ((entry.size + chunkSize - 1) / chunkSize).toInt()

    fun chunkLength(entry: ManifestEntry, chunkIndex: Int): Long {
        val offset = chunkIndex.toLong() * chunkSize
        val remaining = entry.size - offset
        return if (remaining < chunkSize) remaining else chunkSize
    }

    private fun loadHashes(fileIndex: Int): Map<Int, ByteArray> =
        hashCache.getOrPut(fileIndex) { db.getChunkHashes(transferId, fileIndex) }

    fun initialize(existing: Map<Int, StoredFileState>, isResume: Boolean): Map<Int, ChunkBitmap> {
        val have = HashMap<Int, ChunkBitmap>()
        for (entry in entries) {
            val planned = plannedPaths[entry.index]
            if (entry.type == "dir") {
                try {
                    if (mode == DestinationMode.SAF) {
                        findOrCreateDirectory(safRoot!!, planned)
                    } else {
                        val dir = PathPlanner.safeJoin(fileRoot!!, planned)
                        if (!dir.exists() && !dir.mkdirs()) throw IOException("cannot create directory ${dir.absolutePath}")
                    }
                    db.upsertTransferFile(
                        StoredFileState(
                            transferId, entry.index, "dir", entry.path, planned, 0, entry.mtimeMs, 0,
                            ByteArray(0), true, null
                        )
                    )
                } catch (e: Exception) {
                    throw IOException("create directory ${entry.path} failed: ${e.message}", e)
                }
                continue
            }

            val count = chunkCount(entry)
            var bitmap = ChunkBitmap(count)
            val stored = existing[entry.index]
            if (isResume && stored != null && stored.size == entry.size && stored.chunkCount == count) {
                bitmap = try {
                    ChunkBitmap.fromBytes(count, stored.chunkBitmap)
                } catch (_: Exception) {
                    ChunkBitmap(count)
                }
            }

            val fileName = planned.substringAfterLast('/')
            val finalExists: Boolean
            if (mode == DestinationMode.SAF) {
                val parent = findOrCreateDirectory(safRoot!!, planned.substringBeforeLast('/', "")) ?: safRoot
                safParentDocs[entry.index] = parent
                val existingFinal = parent.findFile(fileName)
                finalExists = existingFinal != null
                if (existingFinal != null) safFinalDocs[entry.index] = existingFinal
                val partName = "$fileName.${transferId.take(8)}.lvpart"
                val partDoc = parent.findFile(partName) ?: parent.createFile("application/octet-stream", partName)
                    ?: throw IOException("cannot create partial file for ${entry.path}")
                safPartDocs[entry.index] = partDoc
                safFinalNames[entry.index] = fileName
                val hashes = if (isResume) loadHashes(entry.index) else emptyMap()
                if (finalExists && bitmap.isComplete() && count > 0 && hashes.size == count) {
                    alreadyFinal.add(entry.index)
                    priorChunks[entry.index] = (0 until count).toMutableSet()
                    bytesReceived.addAndGet(entry.size)
                    have[entry.index] = bitmap
                    db.upsertTransferFile(
                        StoredFileState(
                            transferId, entry.index, "file", entry.path,
                            safFinalDocs[entry.index]?.uri?.toString() ?: "", entry.size, entry.mtimeMs, count,
                            bitmap.getBytes(), true, null
                        )
                    )
                    continue
                }
                if (!isResume) bitmap = ChunkBitmap(count)
                sanitizeResumeBits(entry, bitmap, partDoc.length(), hashes)
                val pfd = context.contentResolver.openFileDescriptor(partDoc.uri, "rw")
                    ?: throw IOException("cannot open partial file for ${entry.path}")
                writers[entry.index] = SafChunkWriter(pfd)
            } else {
                val full = PathPlanner.safeJoin(fileRoot!!, planned)
                full.parentFile?.mkdirs()
                targetFiles[entry.index] = full
                // PC-05-A: Use collision-safe temp file name
                val part = PathPlanner.getTempFileName(full, transferId)
                partFiles[entry.index] = part
                val hashes = if (isResume) loadHashes(entry.index) else emptyMap()
                if (full.exists() && bitmap.isComplete() && count > 0 && hashes.size == count) {
                    alreadyFinal.add(entry.index)
                    priorChunks[entry.index] = (0 until count).toMutableSet()
                    bytesReceived.addAndGet(entry.size)
                    have[entry.index] = bitmap
                    db.upsertTransferFile(
                        StoredFileState(
                            transferId, entry.index, "file", entry.path, full.absolutePath, entry.size,
                            entry.mtimeMs, count, bitmap.getBytes(), true, null
                        )
                    )
                    continue
                }
                if (!isResume) bitmap = ChunkBitmap(count)
                val partLength = if (part.exists()) part.length() else 0L
                sanitizeResumeBits(entry, bitmap, partLength, hashes)
                writers[entry.index] = FileChunkWriter(part)
            }

            bitmaps[entry.index] = bitmap
            var held = 0L
            for (c in 0 until count) if (bitmap.isSet(c)) held += chunkLength(entry, c)
            bytesReceived.addAndGet(held)
            if (bitmap.completedCount() > 0) have[entry.index] = bitmap
            db.upsertTransferFile(
                StoredFileState(
                    transferId, entry.index, "file", entry.path,
                    if (mode == DestinationMode.SAF) safFinalDocs[entry.index]?.uri?.toString() ?: "" else targetFiles[entry.index]?.absolutePath ?: "",
                    entry.size, entry.mtimeMs, count, bitmap.getBytes(), false, null
                )
            )
        }
        return have
    }

    private fun sanitizeResumeBits(
        entry: ManifestEntry,
        bitmap: ChunkBitmap,
        partLength: Long,
        hashes: Map<Int, ByteArray>
    ) {
        if (bitmap.completedCount() == 0) return
        if (hashes.isEmpty()) {
            for (c in 0 until bitmap.chunkCount) if (bitmap.isSet(c)) bitmap.clear(c)
            return
        }
        for (c in 0 until bitmap.chunkCount) {
            if (!bitmap.isSet(c)) continue
            val end = (c + 1).toLong() * chunkSize
            val expectedEnd = if (end > entry.size) entry.size else end
            if (expectedEnd > partLength || !hashes.containsKey(c)) {
                bitmap.clear(c)
            }
        }
    }

    fun handleDataFrame(payload: ByteArray): Boolean {
        if (payload.size < 8 + 32) {
            return protocolFailure("malformed DATA frame")
        }
        val bb = ByteBuffer.wrap(payload, 0, 8).order(ByteOrder.BIG_ENDIAN)
        val fileIndex = bb.int
        val chunkIndex = bb.int
        val dataLen = payload.size - 8 - 32
        val data = payload.copyOfRange(8, 8 + dataLen)
        val expectedHash = payload.copyOfRange(8 + dataLen, payload.size)
        val entry = entriesByIndex[fileIndex]
        if (entry == null || entry.type != "file") {
            return protocolFailure("DATA names unknown file $fileIndex")
        }
        val count = chunkCount(entry)
        if (chunkIndex < 0 || chunkIndex >= count) {
            return protocolFailure("chunk $chunkIndex out of range for file $fileIndex")
        }
        val expectedLen = chunkLength(entry, chunkIndex)
        if (dataLen.toLong() != expectedLen) {
            return protocolFailure("chunk length $dataLen does not match expected $expectedLen")
        }
        val actualHash = MessageDigest.getInstance("SHA-256").digest(data)
        if (!actualHash.contentEquals(expectedHash)) {
            val key = "$fileIndex:$chunkIndex"
            val attempts = synchronized(lock) { needAttempts.merge(key, 1, Int::plus) ?: 1 }
            if (attempts >= 3) {
                return protocolFailure(
                    "chunk $chunkIndex of file $fileIndex failed hash verification $attempts times",
                    Constants.ERR_HASH_MISMATCH
                )
            }
            sendNeed(listOf(Pair(fileIndex, chunkIndex)))
            return true
        }

        var completeNow = false
        var retryCompletion = false
        synchronized(lock) {
            if (terminal) return false
            if (finalized.contains(fileIndex)) {
                sendAck(fileIndex, chunkIndex)
                return true
            }
            val bitmap = bitmaps[fileIndex]
            if (bitmap == null) {
                // Already finalized before this session (resume).
                sendAck(fileIndex, chunkIndex)
                return true
            }
            if (bitmap.isSet(chunkIndex)) {
                sendAck(fileIndex, chunkIndex)
                return true
            }
            val writer = writers[fileIndex]
            if (writer == null) {
                sendError(Protocol.ERR_STATE_INTEGRITY, "no writer for file $fileIndex", true, fileIndex)
                terminal = true
                return false
            }
            try {
                writer.writeChunk(chunkIndex.toLong() * chunkSize, data)
                // Durable ordering: flush -> hash -> bitmap -> ACK.
                writer.sync()
                db.saveChunkHash(transferId, fileIndex, chunkIndex, actualHash)
                bitmap.set(chunkIndex)
                db.updateChunkBitmap(transferId, fileIndex, bitmap.getBytes(), false)
            } catch (e: Exception) {
                sendError(Protocol.ERR_STATE_INTEGRITY, "write chunk failed: ${e.message}", true, fileIndex)
                failLocked("Failed", "write chunk failed: ${e.message}", fileIndex)
                return false
            }
            needAttempts.remove("$fileIndex:$chunkIndex")
            bytesReceived.addAndGet(expectedLen)
            sendAck(fileIndex, chunkIndex)
            completeNow = bitmap.isComplete()
            retryCompletion = donePending
            reportProgressLocked()
        }

        if (completeNow) {
            when (val outcome = finalizeOne(fileIndex)) {
                is FinalizeOutcome.Failed -> {
                    synchronized(lock) { failed[fileIndex] = outcome.reason }
                }
                else -> {}
            }
        }
        if (retryCompletion) {
            synchronized(lock) {
                if (donePending && allChunksPresentLocked()) {
                    // complete() takes the same lock, so run it outside.
                } else {
                    return true
                }
            }
            complete()
        }
        return true
    }

    private fun reportProgressLocked() {
        if (onProgress == null) return
        val elapsed = (System.currentTimeMillis() - startTime) / 1000L
        val totalFiles = entries.count { it.type == "file" }
        onProgress.invoke(
            TransferProgress(
                transferId = transferId,
                direction = "receive",
                bytesAcked = bytesReceived.get(),
                totalBytes = totalBytes,
                filesDone = filesCompleted.get(),
                totalFiles = totalFiles,
                speedBps = if (elapsed > 0) bytesReceived.get() / elapsed else 0L
            )
        )
    }

    private fun allChunksPresentLocked(): Boolean {
        for (entry in entries) {
            if (entry.type != "file") continue
            val bitmap = bitmaps[entry.index] ?: return false
            if (!bitmap.isComplete()) return false
        }
        return true
    }

    fun complete(): Boolean {
        synchronized(lock) {
            if (terminal) return true
            if (completing) return false
            completing = true
            doneReceived = true

            if (!allChunksPresentLocked()) {
                val msg = "DONE received before all chunks were received"
                sendError(Protocol.ERR_STATE_INTEGRITY, msg, true, null)
                failLocked("Failed", msg, null)
                return false
            }

            for (entry in entries) {
                if (entry.type != "file" || finalized.contains(entry.index)) continue
                if (failed.containsKey(entry.index)) continue
                when (val outcome = finalizeLocked(entry.index)) {
                    is FinalizeOutcome.Done -> {}
                    is FinalizeOutcome.Retry -> {
                        doneReceived = false
                        donePending = true
                        completing = false
                        return false
                    }
                    is FinalizeOutcome.Failed -> {
                        failed[entry.index] = outcome.reason
                        db.setFileError(transferId, entry.index, outcome.reason)
                        sendError(Protocol.ERR_PROTOCOL_ERROR, outcome.reason, false, entry.index)
                    }
                }
            }

            val unfinished = entries.filter { it.type == "file" && !finalized.contains(it.index) }
            if (failed.isNotEmpty() || unfinished.isNotEmpty()) {
                val reason = buildString {
                    append("not all files were finalized safely")
                    if (failed.isNotEmpty()) append(": " + failed.entries.joinToString("; ") { "file ${it.key}: ${it.value}" })
                    if (unfinished.isNotEmpty()) append("; unfinished: " + unfinished.joinToString(", ") { it.path })
                }
                sendError(Protocol.ERR_STATE_INTEGRITY, reason, true, null)
                failLocked("CompletedWithErrors", reason, null)
                return false
            }

            val doneOk = JSONObject().apply {
                put("transfer_id", transferId)
                put("files_completed", filesCompleted.get())
                put("bytes", bytesReceived.get())
            }.toString()
            try {
                Framing.writeJSONFrame(controlOutput, Protocol.FRAME_DONE_OK, doneOk)
            } catch (e: Exception) {
                failLocked("Failed", "write DONE_OK failed: ${e.message}", null)
                return false
            }
            terminal = true
            completing = false
            db.updateTransferProgress(transferId, filesCompleted.get(), bytesReceived.get(), "Completed")
            reportProgressLocked()
            onCompleted?.invoke(transferId)
            return true
        }
    }

    private fun finalizeOne(fileIndex: Int): FinalizeOutcome {
        synchronized(lock) {
            if (terminal || finalized.contains(fileIndex)) return FinalizeOutcome.Done
            return finalizeLocked(fileIndex)
        }
    }

    private fun finalizeLocked(fileIndex: Int): FinalizeOutcome {
        if (finalized.contains(fileIndex)) return FinalizeOutcome.Done
        val entry = entriesByIndex[fileIndex] ?: return FinalizeOutcome.Failed("unknown file index")
        val bitmap = bitmaps[fileIndex] ?: return FinalizeOutcome.Retry
        if (!bitmap.isComplete()) return FinalizeOutcome.Retry

        if (!verifyPriorLocked(entry, bitmap)) {
            return FinalizeOutcome.Retry
        }

        try {
            writers.remove(fileIndex)?.syncAndClose()
        } catch (e: Exception) {
            return FinalizeOutcome.Failed("flush ${entry.path} failed: ${e.message}")
        }

        if (mode == DestinationMode.SAF) {
            val parent = safParentDocs[fileIndex]
            val partDoc = safPartDocs[fileIndex]
            if (!alreadyFinal.contains(fileIndex)) {
                if (parent == null || partDoc == null) {
                    return FinalizeOutcome.Failed("missing SAF state for ${entry.path}")
                }
                val desired = safFinalNames[fileIndex] ?: partDoc.name?.removeSuffix(".lvpart") ?: entry.path.substringAfterLast('/')
                val finalName = getAvailableSafName(parent, desired)
                if (!partDoc.renameTo(finalName)) {
                    return FinalizeOutcome.Failed("rename SAF file ${entry.path} failed")
                }
                safFinalNames[fileIndex] = finalName
                safFinalDocs[fileIndex] = parent.findFile(finalName)
                    ?: return FinalizeOutcome.Failed("final SAF file missing for ${entry.path}")
            }
            val finalDoc = safFinalDocs[fileIndex]
                ?: return FinalizeOutcome.Failed("missing final SAF file for ${entry.path}")
            // SAF providers do not expose a portable mtime setter; skip it.
            db.updateChunkBitmap(transferId, fileIndex, bitmap.getBytes(), true)
            db.updateFileFinalPath(transferId, fileIndex, finalDoc.uri.toString())
        } else {
            var finalFile = targetFiles[fileIndex]
                ?: return FinalizeOutcome.Failed("missing target path for ${entry.path}")
            if (!alreadyFinal.contains(fileIndex)) {
                val part = partFiles[fileIndex]
                    ?: return FinalizeOutcome.Failed("missing partial file for ${entry.path}")
                finalFile = try {
                    PathPlanner.commitNoReplace(part, finalFile)
                } catch (e: Exception) {
                    return FinalizeOutcome.Failed("finalize ${entry.path} failed: ${e.message}")
                }
                targetFiles[fileIndex] = finalFile
            }
            if (entry.mtimeMs > 0) finalFile.setLastModified(entry.mtimeMs)
            try {
                MediaScannerConnection.scanFile(context, arrayOf(finalFile.absolutePath), null, null)
            } catch (_: Exception) {}
            db.updateChunkBitmap(transferId, fileIndex, bitmap.getBytes(), true)
            db.updateFileFinalPath(transferId, fileIndex, finalFile.absolutePath)
        }

        finalized.add(fileIndex)
        filesCompleted.incrementAndGet()
        try {
            val fileDone = JSONObject().apply { put("index", fileIndex) }.toString()
            Framing.writeJSONFrame(controlOutput, Protocol.FRAME_FILE_DONE, fileDone)
        } catch (_: Exception) {}
        reportProgressLocked()
        return FinalizeOutcome.Done
    }

    /**
     * Re-hashes chunks written by an earlier session and verifies a previously
     * finalized file. Any mismatch is cleared and re-requested with CHUNK_NEED.
     */
    private fun verifyPriorLocked(entry: ManifestEntry, bitmap: ChunkBitmap): Boolean {
        val prior = priorChunks[entry.index] ?: return true
        if (prior.isEmpty()) {
            priorChunks.remove(entry.index)
            return true
        }
        val isFinal = alreadyFinal.contains(entry.index)
        val bad = mutableListOf<Int>()
        for (c in prior.toList()) {
            if (!bitmap.isSet(c)) {
                prior.remove(c)
                continue
            }
            val hash = loadHashes(entry.index)[c]
            if (hash == null) {
                bad.add(c)
                continue
            }
            val data = try {
                readChunk(entry, c, isFinal)
            } catch (_: Exception) {
                bad.add(c)
                continue
            }
            if (!MessageDigest.getInstance("SHA-256").digest(data).contentEquals(hash)) {
                bad.add(c)
            }
        }

        if (bad.isEmpty()) {
            priorChunks.remove(entry.index)
            return true
        }

        if (isFinal) {
            // Never modify a finalized file in place. Re-receive the whole file
            // into a new partial; the final file is left untouched.
            val count = bitmap.chunkCount
            for (c in 0 until count) if (bitmap.isSet(c)) bitmap.clear(c)
            bytesReceived.addAndGet(-entry.size)
            if (bytesReceived.get() < 0) bytesReceived.set(0)
            alreadyFinal.remove(entry.index)
            priorChunks.remove(entry.index)
            try {
                writers[entry.index]?.syncAndClose()
            } catch (_: Exception) {}
            writers[entry.index] = openWriter(entry)
            db.updateChunkBitmap(transferId, entry.index, bitmap.getBytes(), false)
            sendNeed((0 until count).map { Pair(entry.index, it) })
            return false
        }

        for (c in bad) {
            if (bitmap.isSet(c)) bitmap.clear(c)
            bytesReceived.addAndGet(-chunkLength(entry, c))
        }
        if (bytesReceived.get() < 0) bytesReceived.set(0)
        prior.removeAll(bad.toSet())
        if (prior.isEmpty()) priorChunks.remove(entry.index)
        db.updateChunkBitmap(transferId, entry.index, bitmap.getBytes(), false)
        sendNeed(bad.map { Pair(entry.index, it) })
        return false
    }

    private fun readChunk(entry: ManifestEntry, chunkIndex: Int, fromFinal: Boolean): ByteArray {
        val length = chunkLength(entry, chunkIndex).toInt()
        val offset = chunkIndex.toLong() * chunkSize
        if (mode == DestinationMode.SAF) {
            val doc = if (fromFinal) {
                safFinalDocs[entry.index] ?: throw IOException("missing final SAF doc")
            } else {
                safPartDocs[entry.index] ?: throw IOException("missing SAF part")
            }
            val pfd = context.contentResolver.openFileDescriptor(doc.uri, "r")
                ?: throw IOException("cannot open SAF chunk source")
            pfd.use {
                val buf = ByteArray(length)
                var read = 0
                while (read < length) {
                    val n = android.system.Os.pread(it.fileDescriptor, buf, read, length - read, offset + read)
                    if (n <= 0) throw EOFException("short SAF read")
                    read += n
                }
                return buf
            }
        }
        val file = if (fromFinal) targetFiles[entry.index] else partFiles[entry.index]
        if (file == null || !file.exists()) throw IOException("missing chunk source")
        RandomAccessFile(file, "r").use {
            it.seek(offset)
            val buf = ByteArray(length)
            it.readFully(buf)
            return buf
        }
    }

    private fun openWriter(entry: ManifestEntry): ChunkWriter {
        if (mode == DestinationMode.SAF) {
            val parent = safParentDocs[entry.index] ?: throw IOException("missing SAF parent")
            val partName = "${safFinalNames[entry.index] ?: entry.path.substringAfterLast('/')}.${transferId.take(8)}.lvpart"
            val doc = parent.findFile(partName) ?: parent.createFile("application/octet-stream", partName)
                ?: throw IOException("cannot create SAF partial")
            safPartDocs[entry.index] = doc
            val pfd = context.contentResolver.openFileDescriptor(doc.uri, "rw")
                ?: throw IOException("cannot open SAF partial")
            return SafChunkWriter(pfd)
        }
        val part = partFiles[entry.index] ?: throw IOException("missing part path")
        return FileChunkWriter(part)
    }

    fun cancel() {
        synchronized(lock) {
            if (terminal) return
            cancelled = true
            terminal = true
            closeWritersLocked()
            db.updateTransferProgress(transferId, filesCompleted.get(), bytesReceived.get(), "Cancelled")
            onError?.invoke(transferId, "Transfer cancelled")
        }
    }

    fun peerError(message: String, fatal: Boolean, fileIndex: Int?) {
        if (fileIndex != null) {
            synchronized(lock) { failed[fileIndex] = message }
            db.setFileError(transferId, fileIndex, message)
        }
        if (fatal) {
            synchronized(lock) { failLocked("Failed", message, fileIndex) }
        }
    }

    fun cleanupInterrupted() {
        synchronized(lock) {
            closeWritersLocked()
            if (!terminal && !cancelled) {
                db.updateTransferProgress(transferId, filesCompleted.get(), bytesReceived.get(), "Interrupted")
            }
        }
    }

    private fun closeWritersLocked() {
        for ((idx, writer) in writers.toMap()) {
            try { writer.syncAndClose() } catch (_: Exception) {}
            writers.remove(idx)
        }
    }

    private fun failLocked(state: String, message: String, fileIndex: Int?) {
        if (terminal) return
        terminal = true
        completing = false
        closeWritersLocked()
        db.updateTransferProgress(transferId, filesCompleted.get(), bytesReceived.get(), state)
        db.setTransferError(transferId, state, message)
        if (fileIndex != null) db.setFileError(transferId, fileIndex, message)
        onError?.invoke(transferId, message)
    }

    private fun protocolFailure(message: String, code: Int = Protocol.ERR_PROTOCOL_ERROR): Boolean {
        synchronized(lock) {
            if (terminal) return false
            sendError(code, message, true, null)
            failLocked("Failed", message, null)
        }
        return false
    }

    private fun sendAck(fileIndex: Int, chunkIndex: Int) {
        try {
            Framing.writeRawFrame(
                controlOutput,
                Protocol.FRAME_CHUNK_ACK,
                Framing.encodeIndexPairs(listOf(Pair(fileIndex, chunkIndex)))
            )
        } catch (_: Exception) {}
    }

    private fun sendNeed(pairs: List<Pair<Int, Int>>) {
        var index = 0
        while (index < pairs.size) {
            val end = minOf(index + Constants.MAX_NEED_PAIRS_PER_FRAME.toInt(), pairs.size)
            try {
                Framing.writeRawFrame(
                    controlOutput,
                    Protocol.FRAME_CHUNK_NEED,
                    Framing.encodeIndexPairs(pairs.subList(index, end))
                )
            } catch (_: Exception) {}
            index = end
        }
    }

    fun sendError(code: Int, message: String, fatal: Boolean, fileIndex: Int?) {
        val json = JSONObject().apply {
            put("code", code)
            put("message", message)
            put("fatal", fatal)
            if (fileIndex != null) put("file_index", fileIndex)
        }.toString()
        try { Framing.writeJSONFrame(controlOutput, Protocol.FRAME_ERROR, json) } catch (_: Exception) {}
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

private fun getAvailableSafName(parentDoc: DocumentFile, desiredName: String): String {
    if (parentDoc.findFile(desiredName) == null) return desiredName
    val nameWithoutExt = desiredName.substringBeforeLast('.')
    val ext = if (desiredName.contains('.')) "." + desiredName.substringAfterLast('.') else ""
    for (i in 1..9999) {
        val candidate = "$nameWithoutExt ($i)$ext"
        if (parentDoc.findFile(candidate) == null) return candidate
    }
    throw IOException("no available SAF name for $desiredName")
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

    private val activeIncoming = ConcurrentHashMap<String, IncomingTransferSession>()

    var onIncomingOffer: ((transferId: String, peerName: String, peerFp: String, fileCount: Int, totalBytes: Long, acceptCallback: (Boolean) -> Unit) -> Unit)? = null
    var onProgress: ((TransferProgress) -> Unit)? = null
    var onCompleted: ((transferId: String) -> Unit)? = null
    var onError: ((transferId: String, error: String) -> Unit)? = null

    fun generatePairingQR(localIps: List<String>, deviceName: String): String {
        val tokenBytes = ByteArray(Constants.PAIR_TOKEN_BYTES.toInt())
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
            if (uri.scheme != "lanvault" || uri.host != "pair") {
                return@withContext Result.failure(IOException("Not a LANVault pairing URI"))
            }

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
                val helloJson = JSONObject().apply {
                    put("version", JSONObject().apply {
                        put("major", Protocol.MAJOR_VERSION)
                        put("minor", Protocol.MINOR_VERSION)
                    })
                    put("device_id", identity.deviceId)
                    put("name", VaultDatabase(context).getSetting("device_name", "Android Phone"))
                    put("platform", "android")
                    put("role", "control")
                    put("capabilities", JSONArray().put("resume").put("multi-stream"))
                }.toString()
                Framing.writeJSONFrame(output, Protocol.FRAME_HELLO, helloJson)

                val hOk = Framing.readFrame(input)
                if (hOk.type != Protocol.FRAME_HELLO_OK) {
                    throw IOException("Expected HELLO_OK, got ${hOk.type}")
                }
                val helloOk = JSONObject(hOk.payloadAsString())
                val peerDevId = helloOk.optString("device_id", "")
                if (peerDevId.uppercase() != fp.take(32).uppercase()) {
                    throw IOException("Peer device_id does not match the pairing fingerprint")
                }

                val pairReq = JSONObject().apply {
                    put("token", tok)
                    put("name", VaultDatabase(context).getSetting("device_name", "Android Phone"))
                    put("platform", "android")
                }.toString()
                Framing.writeJSONFrame(output, Protocol.FRAME_PAIR_REQUEST, pairReq)

                val resp = Framing.readFrame(input)
                if (resp.type == Protocol.FRAME_PAIR_OK) {
                    val respJson = JSONObject(resp.payloadAsString())
                    val peerDevName = respJson.optString("name", name)
                    VaultDatabase(context).addTrustedDevice(
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
                    val clientSocket = try { s.accept() as SSLSocket } catch (_: Exception) { break }
                    launch(Dispatchers.IO) { handleIncomingConnection(clientSocket) }
                }
            } catch (_: Exception) {}
        }
    }

    fun stopServer() {
        serverJob?.cancel()
        serverJob = null
        try { serverSocket?.close() } catch (_: Exception) {}
        for ((_, session) in activeIncoming) session.cleanupInterrupted()
        activeIncoming.clear()
    }

    private suspend fun handleIncomingConnection(socket: SSLSocket) = withContext(Dispatchers.IO) {
        var trackedTransferId = ""
        try {
            socket.soTimeout = Constants.HELLO_TIMEOUT_MS.toInt()
            socket.startHandshake()
            val certs = socket.session.peerCertificates
            val peerCert = certs.firstOrNull() as? java.security.cert.X509Certificate
                ?: throw IOException("No peer certificate provided")
            val peerFp = IdentityManager.computeFingerprint(peerCert)

            val input = socket.inputStream
            val output = socket.outputStream
            val helloFrame = Framing.readFrame(input)
            if (helloFrame.type != Protocol.FRAME_HELLO) {
                throw IOException("Expected HELLO frame")
            }
            val helloJson = JSONObject(helloFrame.payloadAsString())
            val version = helloJson.optJSONObject("version")
            if (version == null || version.optInt("major", -1) != Protocol.MAJOR_VERSION) {
                throw IOException("Unsupported wire protocol version")
            }
            val claimedDeviceId = helloJson.optString("device_id", "")
            if (claimedDeviceId.isEmpty() || !claimedDeviceId.equals(peerFp.take(32), ignoreCase = true)) {
                throw IOException("device_id does not match the presented certificate")
            }
            val peerName = helloJson.optString("name", "LANVault Peer").take(256)
            val role = helloJson.optString("role", "control")
            val dataTransferId = helloJson.optString("transfer_id", "")
            val streamToken = helloJson.optString("stream_token", "")

            val db = VaultDatabase(context)
            val paired = db.isTrusted(peerFp)
            val helloOkJson = JSONObject().apply {
                put("version", JSONObject().apply {
                    put("major", Protocol.MAJOR_VERSION)
                    put("minor", Protocol.MINOR_VERSION)
                })
                put("device_id", identity.deviceId)
                put("name", db.getSetting("device_name", "Android Phone"))
                put("platform", "android")
                put("paired", paired)
            }.toString()

            if (role == "data") {
                val session = activeIncoming[dataTransferId]
                if (session == null || session.streamToken != streamToken || session.peerFp != peerFp) {
                    Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, JSONObject().apply {
                        put("code", Protocol.ERR_PROTOCOL_ERROR)
                        put("message", "Invalid data connection credentials")
                        put("fatal", true)
                    }.toString())
                    socket.close()
                    return@withContext
                }
                if (session.isTerminal()) {
                    Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, JSONObject().apply {
                        put("code", Protocol.ERR_STATE_INTEGRITY)
                        put("message", "Transfer already finished")
                        put("fatal", true)
                    }.toString())
                    socket.close()
                    return@withContext
                }
                Framing.writeJSONFrame(output, Protocol.FRAME_HELLO_OK, helloOkJson)
                handleDataStream(session, input, output)
                return@withContext
            }
            if (role != "control") throw IOException("Invalid HELLO role")

            Framing.writeJSONFrame(output, Protocol.FRAME_HELLO_OK, helloOkJson)
            socket.soTimeout = Protocol.OFFER_RESPONSE_TIMEOUT_S.toInt() * 1000

            val nextFrame = Framing.readFrame(input)
            if (nextFrame.type == Protocol.FRAME_PAIR_REQUEST) {
                handlePairRequest(db, peerFp, sessionName = peerName, helloJson = helloJson, payload = nextFrame.payload, output = output)
                socket.close()
                return@withContext
            }
            if (nextFrame.type != Protocol.FRAME_OFFER) {
                throw IOException("Expected OFFER, got ${nextFrame.type}")
            }

            val offerJson = JSONObject(nextFrame.payloadAsString())
            val transferId = offerJson.optString("transfer_id", "")
            if (transferId.isEmpty() || transferId.length > 128) {
                throw IOException("Invalid transfer_id")
            }
            trackedTransferId = transferId
            val chunkSize = offerJson.optLong("chunk_size", Protocol.CHUNK_SIZE_DEFAULT.toLong())
            val totalBytes = offerJson.optLong("total_bytes", 0L)
            val fileCount = offerJson.optInt("file_count", 0)
            val dirCount = offerJson.optInt("dir_count", 0)
            val requestedStreams = offerJson.optInt("data_streams", Constants.DATA_STREAMS_DEFAULT.toInt())

            if (chunkSize < Protocol.CHUNK_SIZE_MIN || chunkSize > Protocol.CHUNK_SIZE_MAX) {
                throw IOException("chunk_size $chunkSize out of bounds")
            }
            if (fileCount < 0 || dirCount < 0 || fileCount.toLong() + dirCount > Protocol.MAX_MANIFEST_ENTRIES) {
                throw IOException("manifest entry count out of bounds")
            }
            if (totalBytes < 0) throw IOException("total_bytes cannot be negative")
            if (requestedStreams < 0 || requestedStreams > Constants.DATA_STREAMS_MAX.toInt()) {
                throw IOException("data_streams out of bounds")
            }

            val existing = db.getTransfer(transferId)
            var resumeCandidate = false
            if (existing != null) {
                if (existing.peerFp != peerFp) {
                    throw IOException("transfer_id is already in use by another device")
                }
                if (existing.state in setOf("Completed", "CompletedWithErrors", "Cancelled", "Failed")) {
                    throw IOException("transfer_id already refers to a finished transfer")
                }
                resumeCandidate = true
            }
            val allowUnpaired = db.getSetting("receiving.allow_unpaired", "false").equals("true", ignoreCase = true)
            if (!paired && !allowUnpaired && !resumeCandidate) {
                Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, JSONObject().apply {
                    put("code", Constants.ERR_NOT_PAIRED)
                    put("message", "Receiver does not allow unpaired senders; pair first")
                    put("fatal", true)
                }.toString())
                socket.close()
                return@withContext
            }

            val entries = readManifest(input, fileCount, dirCount, totalBytes)
            val manifestHash = Framing.computeManifestHash(entries)
            val planned = PathPlanner.plan(entries)

            val existingFiles = if (resumeCandidate) db.getTransferFiles(transferId).associateBy { it.fileIndex } else emptyMap()
            val savedChunkSize = if (resumeCandidate) db.getSetting("transfer.$transferId.chunk_size", "").toLongOrNull() else null
            val sameBinding = resumeCandidate &&
                existing != null &&
                existing.manifestHash.equals(manifestHash, ignoreCase = true) &&
                savedChunkSize == chunkSize
            if (existing != null && !sameBinding) {
                // Same certificate, changed manifest: discard only our own state.
                db.deleteTransferState(transferId)
            }
            val isResume = sameBinding

            if (!isResume) {
                val acceptCompletable = CompletableDeferred<Boolean>()
                val callback = onIncomingOffer
                if (callback == null) {
                    acceptCompletable.complete(false)
                } else {
                    callback.invoke(transferId, peerName, peerFp, fileCount, totalBytes) { accepted ->
                        acceptCompletable.complete(accepted)
                    }
                }
                val accepted = withTimeoutOrNull(Protocol.OFFER_RESPONSE_TIMEOUT_S * 1000L) { acceptCompletable.await() } ?: false
                if (!accepted) {
                    Framing.writeJSONFrame(output, Protocol.FRAME_REJECT, JSONObject().apply {
                        put("transfer_id", transferId)
                        put("code", Protocol.ERR_REJECTED)
                        put("message", "Declined by user")
                    }.toString())
                    socket.close()
                    return@withContext
                }
            }

            var destinationMode = DestinationMode.FILE
            var fileRoot: File? = null
            var safRoot: DocumentFile? = null
            if (isResume) {
                val storedSaf = db.getSetting("transfer.$transferId.saf_tree_uri", "")
                val storedDir = db.getSetting("transfer.$transferId.target_dir", "")
                if (storedSaf.isNotEmpty()) {
                    safRoot = try { DocumentFile.fromTreeUri(context, Uri.parse(storedSaf)) } catch (_: Exception) { null }
                    if (safRoot != null && safRoot.canWrite()) destinationMode = DestinationMode.SAF
                } else if (storedDir.isNotEmpty()) {
                    fileRoot = File(storedDir)
                }
            }
            if (safRoot == null && fileRoot == null) {
                val treeUriStr = db.getSetting("save_tree_uri", "")
                if (treeUriStr.isNotBlank()) {
                    try {
                        val doc = DocumentFile.fromTreeUri(context, Uri.parse(treeUriStr))
                        if (doc != null && doc.canWrite()) {
                            safRoot = doc
                            destinationMode = DestinationMode.SAF
                        }
                    } catch (_: Exception) {}
                }
            }

            if (safRoot == null && fileRoot == null) {
                var saveDir = File(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS), "LANVault")
                if (!saveDir.exists() && !saveDir.mkdirs()) {
                    saveDir = context.getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS) ?: File(context.filesDir, "LANVault")
                    saveDir.mkdirs()
                }
                fileRoot = saveDir
            }

            if (fileRoot != null) {
                if (!fileRoot.exists() && !fileRoot.mkdirs()) throw IOException("cannot create destination directory")
                db.setSetting("transfer.$transferId.target_dir", fileRoot.absolutePath)
                db.setSetting("transfer.$transferId.saf_tree_uri", "")
            } else if (safRoot != null) {
                db.setSetting("transfer.$transferId.saf_tree_uri", safRoot.uri.toString())
                db.setSetting("transfer.$transferId.target_dir", "")
            }
            db.setSetting("transfer.$transferId.chunk_size", chunkSize.toString())

            val tokenBytes = ByteArray(16)
            SecureRandom().nextBytes(tokenBytes)
            val generatedToken = Base64.getUrlEncoder().withoutPadding().encodeToString(tokenBytes)
            val streams = if (requestedStreams <= 0) 1 else minOf(requestedStreams, Constants.DATA_STREAMS_MAX.toInt(), 4)

            val session = IncomingTransferSession(
                context = context,
                db = db,
                transferId = transferId,
                peerName = peerName,
                peerFp = peerFp,
                streamToken = generatedToken,
                chunkSize = chunkSize,
                entries = entries,
                plannedPaths = planned,
                totalBytes = totalBytes,
                mode = destinationMode,
                fileRoot = fileRoot,
                safRoot = safRoot,
                controlOutput = output,
                createdAt = existing?.createdAt ?: System.currentTimeMillis(),
                onProgress = onProgress,
                onCompleted = onCompleted,
                onError = onError
            )

            val have = session.initialize(existingFiles, isResume)
            activeIncoming[transferId] = session

            db.recordTransfer(
                TransferRecord(
                    transferId = transferId,
                    direction = "receive",
                    peerFp = peerFp,
                    peerName = peerName,
                    state = "Transferring",
                    totalFiles = fileCount,
                    totalBytes = totalBytes,
                    completedFiles = session.filesCompleted.get(),
                    completedBytes = session.bytesReceived.get(),
                    manifestHash = manifestHash,
                    createdAt = existing?.createdAt ?: System.currentTimeMillis(),
                    errorMessage = null
                )
            )

            val acceptJson = JSONObject().apply {
                put("transfer_id", transferId)
                put("resume", isResume)
                put("stream_token", generatedToken)
                put("data_streams", streams)
                val haveJson = JSONObject()
                for ((idx, bitmap) in have) haveJson.put(idx.toString(), bitmap.toBase64())
                put("have", haveJson)
            }.toString()
            Framing.writeJSONFrame(output, Protocol.FRAME_ACCEPT, acceptJson)
            socket.soTimeout = Constants.DEAD_PEER_TIMEOUT_MS.toInt()

            while (!session.isTerminal()) {
                val ctrlFrame = try {
                    Framing.readFrame(input)
                } catch (_: Exception) {
                    break
                }
                when (ctrlFrame.type) {
                    Protocol.FRAME_DONE -> {
                        val ok = session.complete()
                        if (!ok && session.isTerminal()) break
                    }
                    Protocol.FRAME_CANCEL -> {
                        session.cancel()
                        break
                    }
                    Protocol.FRAME_PING -> {
                        try { Framing.writeRawFrame(output, Protocol.FRAME_PONG, ByteArray(0)) } catch (_: Exception) {}
                    }
                    Protocol.FRAME_PONG -> {}
                    Protocol.FRAME_PAUSE, Protocol.FRAME_RESUME -> {}
                    Protocol.FRAME_ERROR -> {
                        val err = JSONObject(ctrlFrame.payloadAsString())
                        val fileIdx = if (err.has("file_index")) err.optInt("file_index") else null
                        session.peerError(err.optString("message", "peer error"), err.optBoolean("fatal", false), fileIdx)
                    }
                    else -> {
                        session.sendError(Protocol.ERR_PROTOCOL_ERROR, "unexpected control frame", true, null)
                        break
                    }
                }
            }
            session.cleanupInterrupted()
        } catch (e: Exception) {
            onError?.invoke(trackedTransferId, e.message ?: "Incoming connection error")
        } finally {
            activeIncoming.remove(trackedTransferId)
            try { socket.close() } catch (_: Exception) {}
        }
    }

    private suspend fun handlePairRequest(
        db: VaultDatabase,
        peerFp: String,
        sessionName: String,
        helloJson: JSONObject,
        payload: ByteArray,
        output: OutputStream
    ) {
        val pairJson = JSONObject(String(payload, StandardCharsets.UTF_8))
        val receivedTok = pairJson.optString("token", "")
        val peerReqName = pairJson.optString("name", sessionName).take(256)
        val peerReqPlat = pairJson.optString("platform", "remote").take(64)
        val pairingSession = activePairing
        val now = System.currentTimeMillis()
        if (pairingSession == null ||
            (now - pairingSession.createdAt) > Protocol.PAIR_TOKEN_TTL_S * 1000L ||
            pairingSession.attempts >= Protocol.PAIR_MAX_FAILED_ATTEMPTS
        ) {
            activePairing = null
            Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, JSONObject().apply {
                put("code", Protocol.ERR_PAIR_TOKEN_INVALID)
                put("message", "Pairing token expired or invalid")
                put("fatal", true)
            }.toString())
            return
        }
        if (!MessageDigest.isEqual(
                receivedTok.toByteArray(StandardCharsets.UTF_8),
                pairingSession.tokenB64.toByteArray(StandardCharsets.UTF_8)
            )
        ) {
            pairingSession.attempts++
            Framing.writeJSONFrame(output, Protocol.FRAME_ERROR, JSONObject().apply {
                put("code", Protocol.ERR_PAIR_TOKEN_INVALID)
                put("message", "Pairing token mismatch")
                put("fatal", true)
            }.toString())
            return
        }
        activePairing = null
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
    }

    private fun readManifest(input: InputStream, fileCount: Int, dirCount: Int, totalBytes: Long): List<ManifestEntry> {
        val entries = mutableListOf<ManifestEntry>()
        var endJson: JSONObject? = null
        while (endJson == null) {
            val frame = Framing.readFrame(input)
            when (frame.type) {
                Protocol.FRAME_MANIFEST_PART -> {
                    val part = JSONObject(frame.payloadAsString())
                    val arr = part.optJSONArray("entries") ?: JSONArray()
                    if (arr.length() == 0) throw IOException("empty MANIFEST_PART")
                    if (arr.length() > Constants.MANIFEST_PART_MAX_ENTRIES) {
                        throw IOException("MANIFEST_PART exceeds entry limit")
                    }
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
                    if (entries.size.toLong() > Protocol.MAX_MANIFEST_ENTRIES) {
                        throw IOException("manifest exceeds maximum entry count")
                    }
                }
                Protocol.FRAME_MANIFEST_END -> {
                    endJson = JSONObject(frame.payloadAsString())
                }
                Protocol.FRAME_ERROR -> {
                    val err = JSONObject(frame.payloadAsString())
                    throw IOException("peer error during manifest: ${err.optString("message")}")
                }
                else -> throw IOException("unexpected frame during manifest: ${frame.type}")
            }
        }

        if (endJson.optInt("entry_count", -1) != entries.size) {
            throw IOException("MANIFEST_END entry_count does not match received entries")
        }
        for ((expectedIndex, entry) in entries.withIndex()) {
            if (entry.index != expectedIndex) throw IOException("manifest indices are not sequential")
        }
        val expectedHash = endJson.optString("manifest_hash", "")
        if (expectedHash.isEmpty()) throw IOException("manifest hash missing")
        val calcHash = Framing.computeManifestHash(entries)
        if (!expectedHash.equals(calcHash, ignoreCase = true)) throw IOException("manifest hash mismatch")

        var files = 0
        var dirs = 0
        var bytes = 0L
        for (entry in entries) {
            when (entry.type) {
                "file" -> {
                    if (entry.size < 0) throw IOException("negative file size")
                    files++
                    bytes += entry.size
                }
                "dir" -> {
                    if (entry.size != 0L) throw IOException("directory entry has non-zero size")
                    dirs++
                }
                else -> throw IOException("invalid entry type ${entry.type}")
            }
        }
        if (files != fileCount || dirs != dirCount || bytes != totalBytes) {
            throw IOException("manifest counts do not match OFFER")
        }
        return entries
    }

    private fun handleDataStream(session: IncomingTransferSession, input: InputStream, output: OutputStream) {
        try {
            while (true) {
                val frame = Framing.readFrame(input)
                when (frame.type) {
                    Protocol.FRAME_DATA -> {
                        if (!session.handleDataFrame(frame.payload)) break
                    }
                    Protocol.FRAME_DONE -> {
                        val ok = session.complete()
                        if (session.isTerminal()) break
                    }
                    Protocol.FRAME_PING -> {
                        try { Framing.writeRawFrame(output, Protocol.FRAME_PONG, ByteArray(0)) } catch (_: Exception) {}
                    }
                    Protocol.FRAME_PONG -> {}
                    Protocol.FRAME_CANCEL -> {
                        session.cancel()
                        break
                    }
                    Protocol.FRAME_ERROR -> {
                        val err = JSONObject(frame.payloadAsString())
                        val fileIdx = if (err.has("file_index")) err.optInt("file_index") else null
                        session.peerError(err.optString("message", "peer error"), err.optBoolean("fatal", false), fileIdx)
                        if (err.optBoolean("fatal", false)) break
                    }
                    else -> {
                        session.sendError(Protocol.ERR_PROTOCOL_ERROR, "unexpected frame on data stream", true, null)
                        break
                    }
                }
            }
        } catch (_: Exception) {
        }
    }

    private sealed class SendEvent {
        data class Ack(val pair: Pair<Int, Int>) : SendEvent()
        data class Need(val pair: Pair<Int, Int>) : SendEvent()
        data class FileDone(val index: Int) : SendEvent()
        data class DoneOk(val json: JSONObject) : SendEvent()
        data class Failure(val message: String) : SendEvent()
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

    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    suspend fun startSendSources(
        targetIp: String,
        targetPort: Int,
        sources: List<SendSource>,
        peerFingerprint: String? = null
    ): String = withContext(Dispatchers.IO) {
        val db = VaultDatabase(context)
        val entries = mutableListOf<ManifestEntry>()
        var totalBytes = 0L
        var fileCount = 0
        var dirCount = 0
        sources.forEachIndexed { index, src ->
            if (!src.isDirectory) {
                totalBytes += src.size
                fileCount++
            } else {
                dirCount++
            }
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
        val chunkSize = Protocol.CHUNK_SIZE_DEFAULT.toLong()

        val sslContext = IdentityManager.createSSLContext(identity, peerFingerprint)
        val sf = sslContext.socketFactory
        val socket = sf.createSocket(targetIp, targetPort) as SSLSocket
        socket.startHandshake()

        val certs = socket.session.peerCertificates
        val peerCert = certs.firstOrNull() as? java.security.cert.X509Certificate
            ?: throw IOException("No peer certificate provided by receiver")
        val actualPeerFp = IdentityManager.computeFingerprint(peerCert)
        if (!peerFingerprint.isNullOrBlank()) {
            if (!peerFingerprint.equals(actualPeerFp, ignoreCase = true)) {
                throw IOException("Peer fingerprint mismatch: expected $peerFingerprint, got $actualPeerFp")
            }
        } else {
            if (!db.isTrusted(actualPeerFp)) {
                throw IOException("Receiver ($actualPeerFp) is not a paired or trusted device. Pair devices first.")
            }
        }

        val resumable = db.findResumableSend(manifestHash, actualPeerFp)
        val transferId = resumable?.transferId ?: UUID.randomUUID().toString().replace("-", "").take(16)
        val createdAt = resumable?.createdAt ?: System.currentTimeMillis()
        val dataTls = IdentityManager.createSSLContext(identity, actualPeerFp).socketFactory

        val input = socket.inputStream
        val output = socket.outputStream
        var completed = false
        var dataSocket: SSLSocket? = null
        var readerJob: Job? = null
        val events = Channel<SendEvent>(Channel.UNLIMITED)

        try {
            var bytesAcked = 0L
            var filesDone = 0
            val sent = HashSet<String>()
            val acked = HashSet<String>()
            val inFlight = HashSet<String>()
            val haveBitmaps = HashMap<Int, ChunkBitmap>()
            val needOutstanding = HashMap<String, Int>()
            var doneOkJson: JSONObject? = null
            val maxInFlight = Protocol.WINDOW_CHUNKS_PER_STREAM

            fun keyOf(fileIndex: Int, chunkIndex: Int) = "$fileIndex:$chunkIndex"
            fun chunkCountOf(index: Int): Int {
                val entry = entries[index]
                return if (entry.size <= 0L) 0 else ((entry.size + chunkSize - 1) / chunkSize).toInt()
            }
            fun lengthOf(index: Int, chunkIndex: Int): Long {
                val entry = entries[index]
                val offset = chunkIndex.toLong() * chunkSize
                val remaining = entry.size - offset
                return if (remaining < chunkSize) remaining else chunkSize
            }

            fun writeControl(frameType: Byte, json: String) {
                Framing.writeJSONFrame(output, frameType, json)
            }

            suspend fun sendChunk(index: Int, chunkIndex: Int, data: ByteArray) {
                val payload = Framing.encodeDataFramePayload(index, chunkIndex, data)
                Framing.writeRawFrame(dataSocket!!.outputStream, Protocol.FRAME_DATA, payload)
                val key = keyOf(index, chunkIndex)
                sent.add(key)
                inFlight.add(key)
            }

            fun readChunkAt(src: SendSource, offset: Long, length: Int): ByteArray {
                if (src.file != null) {
                    RandomAccessFile(src.file, "r").use {
                        it.seek(offset)
                        val buf = ByteArray(length)
                        it.readFully(buf)
                        return buf
                    }
                }
                val uri = src.uri ?: throw IOException("missing send source")
                val stream = context.contentResolver.openInputStream(uri)
                    ?: throw IOException("cannot open source URI")
                stream.use {
                    var skipped = 0L
                    while (skipped < offset) {
                        val n = it.skip(offset - skipped)
                        if (n <= 0) throw EOFException("source ended while seeking")
                        skipped += n
                    }
                    val buf = ByteArray(length)
                    var read = 0
                    while (read < length) {
                        val n = it.read(buf, read, length - read)
                        if (n <= 0) throw EOFException("source ended during retransmit")
                        read += n
                    }
                    return buf
                }
            }

            fun processAck(pair: Pair<Int, Int>) {
                val (fIdx, cIdx) = pair
                if (fIdx !in entries.indices || entries[fIdx].type != "file") {
                    throw IOException("protocol violation: ACK names unknown file $fIdx")
                }
                if (cIdx < 0 || cIdx >= chunkCountOf(fIdx)) {
                    throw IOException("protocol violation: ACK chunk $cIdx out of range")
                }
                val key = keyOf(fIdx, cIdx)
                if (!sent.contains(key)) throw IOException("protocol violation: ACK for unsent chunk")
                if (acked.contains(key)) throw IOException("protocol violation: duplicate ACK")
                if (!inFlight.contains(key)) throw IOException("protocol violation: ACK for chunk not in flight")
                acked.add(key)
                inFlight.remove(key)
                bytesAcked += lengthOf(fIdx, cIdx)
                val elapsed = (System.currentTimeMillis() - createdAt) / 1000L
                onProgress?.invoke(
                    TransferProgress(
                        transferId, "send", bytesAcked, totalBytes, filesDone, fileCount,
                        if (elapsed > 0) bytesAcked / elapsed else 0L
                    )
                )
            }

            suspend fun processNeed(pair: Pair<Int, Int>, events: Channel<SendEvent>) {
                val (fIdx, cIdx) = pair
                if (fIdx !in entries.indices || entries[fIdx].type != "file") {
                    throw IOException("protocol violation: NEED names unknown file $fIdx")
                }
                if (cIdx < 0 || cIdx >= chunkCountOf(fIdx)) {
                    throw IOException("protocol violation: NEED chunk $cIdx out of range")
                }
                val key = keyOf(fIdx, cIdx)
                if (!sent.contains(key) && haveBitmaps[fIdx]?.isSet(cIdx) != true) {
                    throw IOException("protocol violation: NEED for a chunk that was never sent or advertised")
                }
                if (acked.contains(key)) {
                    // Receiver discovered that a resumed/finalized chunk is
                    // corrupt. Re-send it and require a fresh ACK.
                    acked.remove(key)
                }
                haveBitmaps[fIdx]?.clear(cIdx)
                val count = needOutstanding.merge(key, 1, Int::plus) ?: 1
                if (count > 512) throw IOException("too many re-requests for $key")
                while (!inFlight.contains(key) && inFlight.size >= maxInFlight) {
                    val ev = withTimeoutOrNull(30_000L) { events.receive() }
                        ?: throw IOException("timeout waiting for window while retransmitting")
                    when (ev) {
                        is SendEvent.Ack -> processAck(ev.pair)
                        is SendEvent.Need -> processNeed(ev.pair, events)
                        is SendEvent.FileDone -> { filesDone = maxOf(filesDone, ev.index + 1) }
                        is SendEvent.Failure -> throw IOException(ev.message)
                        is SendEvent.DoneOk -> doneOkJson = ev.json
                    }
                }
                val data = readChunkAt(sources[fIdx], cIdx.toLong() * chunkSize, lengthOf(fIdx, cIdx).toInt())
                sendChunk(fIdx, cIdx, data)
            }

            suspend fun handleEvent(ev: SendEvent, events: Channel<SendEvent>) {
                when (ev) {
                    is SendEvent.Ack -> processAck(ev.pair)
                    is SendEvent.Need -> processNeed(ev.pair, events)
                    is SendEvent.FileDone -> { filesDone = maxOf(filesDone, ev.index + 1) }
                    is SendEvent.DoneOk -> doneOkJson = ev.json
                    is SendEvent.Failure -> throw IOException(ev.message)
                }
            }

            suspend fun processAvailable(events: Channel<SendEvent>) {
                while (true) {
                    val ev = events.tryReceive().getOrNull() ?: break
                    handleEvent(ev, events)
                }
            }

            suspend fun awaitEvent(events: Channel<SendEvent>, timeoutMs: Long = 30_000L) {
                val ev = withTimeoutOrNull(timeoutMs) { events.receive() }
                    ?: throw IOException("timeout waiting for receiver control frame")
                handleEvent(ev, events)
            }

            db.recordTransfer(
                TransferRecord(
                    transferId, "send", actualPeerFp, targetIp, "Transferring", fileCount, totalBytes,
                    0, 0, manifestHash, createdAt, null
                )
            )

            writeControl(Protocol.FRAME_HELLO, JSONObject().apply {
                put("version", JSONObject().apply {
                    put("major", Protocol.MAJOR_VERSION)
                    put("minor", Protocol.MINOR_VERSION)
                })
                put("device_id", identity.deviceId)
                put("name", db.getSetting("device_name", "Android Phone"))
                put("platform", "android")
                put("role", "control")
                put("capabilities", JSONArray().put("resume").put("multi-stream"))
            }.toString())

            val hOkFrame = Framing.readFrame(input)
            if (hOkFrame.type != Protocol.FRAME_HELLO_OK) {
                throw IOException("Expected HELLO_OK, got ${hOkFrame.type}")
            }
            val helloOk = JSONObject(hOkFrame.payloadAsString())
            if (helloOk.optJSONObject("version")?.optInt("major", -1) != Protocol.MAJOR_VERSION) {
                throw IOException("Unsupported receiver protocol version")
            }
            if (!helloOk.optString("device_id", "").equals(actualPeerFp.take(32), ignoreCase = true)) {
                throw IOException("Receiver device_id does not match its certificate")
            }

            writeControl(Protocol.FRAME_OFFER, JSONObject().apply {
                put("transfer_id", transferId)
                put("chunk_size", chunkSize)
                put("file_count", fileCount)
                put("dir_count", dirCount)
                put("total_bytes", totalBytes)
                put("data_streams", 1)
            }.toString())

            val part = JSONObject().apply {
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
            writeControl(Protocol.FRAME_MANIFEST_PART, part)
            writeControl(Protocol.FRAME_MANIFEST_END, JSONObject().apply {
                put("entry_count", entries.size)
                put("manifest_hash", manifestHash)
            }.toString())

            val respFrame = Framing.readFrame(input)
            if (respFrame.type == Protocol.FRAME_REJECT) {
                val rej = JSONObject(respFrame.payloadAsString())
                throw IOException("Transfer declined: ${rej.optString("message", "user declined")}")
            }
            if (respFrame.type == Protocol.FRAME_ERROR) {
                val err = JSONObject(respFrame.payloadAsString())
                throw IOException("Receiver error: ${err.optString("message")}")
            }
            if (respFrame.type != Protocol.FRAME_ACCEPT) {
                throw IOException("Expected ACCEPT, got ${respFrame.type}")
            }
            val accept = JSONObject(respFrame.payloadAsString())
            if (accept.optString("transfer_id", "") != transferId) {
                throw IOException("ACCEPT transfer_id mismatch")
            }
            val streamToken = accept.optString("stream_token", "")
            if (streamToken.isEmpty()) throw IOException("ACCEPT missing stream_token")
            val streams = accept.optInt("data_streams", 0)
            if (streams != 1) throw IOException("ACCEPT negotiated unsupported data_streams=$streams")
            accept.optJSONObject("have")?.let { haveJson ->
                for (fileKey in haveJson.keys()) {
                    val fIdx = fileKey.toIntOrNull()
                        ?: throw IOException("invalid resume bitmap file index")
                    if (fIdx !in entries.indices || entries[fIdx].type != "file") {
                        throw IOException("resume bitmap names unknown file")
                    }
                    val bitmap = try {
                        ChunkBitmap.fromBase64(chunkCountOf(fIdx), haveJson.getString(fileKey))
                    } catch (e: Exception) {
                        throw IOException("invalid resume bitmap: ${e.message}")
                    }
                    haveBitmaps[fIdx] = bitmap
                    for (c in 0 until bitmap.chunkCount) {
                        if (bitmap.isSet(c)) bytesAcked += lengthOf(fIdx, c)
                    }
                }
            }

            val ds = dataTls.createSocket(targetIp, targetPort) as SSLSocket
            ds.startHandshake()
            dataSocket = ds
            val dataIn = ds.inputStream
            val dataOut = ds.outputStream
            Framing.writeJSONFrame(dataOut, Protocol.FRAME_HELLO, JSONObject().apply {
                put("version", JSONObject().apply {
                    put("major", Protocol.MAJOR_VERSION)
                    put("minor", Protocol.MINOR_VERSION)
                })
                put("device_id", identity.deviceId)
                put("name", db.getSetting("device_name", "Android Phone"))
                put("platform", "android")
                put("role", "data")
                put("transfer_id", transferId)
                put("stream_token", streamToken)
            }.toString())
            val dataHelloOk = Framing.readFrame(dataIn)
            if (dataHelloOk.type != Protocol.FRAME_HELLO_OK) {
                val msg = if (dataHelloOk.type == Protocol.FRAME_ERROR) {
                    JSONObject(dataHelloOk.payloadAsString()).optString("message")
                } else "expected HELLO_OK"
                throw IOException("data connection rejected: $msg")
            }

            readerJob = CoroutineScope(Dispatchers.IO).launch {
                try {
                    while (isActive) {
                        val frame = Framing.readFrame(input)
                        when (frame.type) {
                            Protocol.FRAME_CHUNK_ACK -> {
                                Framing.decodeIndexPairs(frame.payload).forEach { events.send(SendEvent.Ack(it)) }
                            }
                            Protocol.FRAME_CHUNK_NEED -> {
                                val pairs = Framing.decodeIndexPairs(frame.payload)
                                if (pairs.size > Constants.MAX_NEED_PAIRS_PER_FRAME) {
                                    events.send(SendEvent.Failure("CHUNK_NEED exceeds pair limit"))
                                    return@launch
                                }
                                pairs.forEach { events.send(SendEvent.Need(it)) }
                            }
                            Protocol.FRAME_FILE_DONE -> {
                                val idx = JSONObject(frame.payloadAsString()).optInt("index", -1)
                                events.send(SendEvent.FileDone(idx))
                            }
                            Protocol.FRAME_DONE_OK -> {
                                events.send(SendEvent.DoneOk(JSONObject(frame.payloadAsString())))
                                return@launch
                            }
                            Protocol.FRAME_ERROR -> {
                                val err = JSONObject(frame.payloadAsString())
                                events.send(SendEvent.Failure("receiver error: ${err.optString("message")}"))
                                return@launch
                            }
                            Protocol.FRAME_PING -> {
                                try { Framing.writeRawFrame(output, Protocol.FRAME_PONG, ByteArray(0)) } catch (_: Exception) {}
                            }
                            Protocol.FRAME_PONG -> {}
                            else -> {
                                events.send(SendEvent.Failure("unexpected control frame ${frame.type}"))
                                return@launch
                            }
                        }
                    }
                } catch (e: Exception) {
                    if (isActive) events.send(SendEvent.Failure(e.message ?: "control connection closed"))
                }
            }

            for (entry in entries) {
                if (entry.type != "file" || entry.size == 0L) continue
                val src = sources[entry.index]
                if (src.file != null) {
                    if (src.file.length() != entry.size || src.file.lastModified() != entry.mtimeMs) {
                        val err = "source file changed: ${entry.path}"
                        writeControl(Protocol.FRAME_ERROR, JSONObject().apply {
                            put("code", Constants.ERR_FILE_CHANGED)
                            put("message", err)
                            put("file_index", entry.index)
                            put("fatal", true)
                        }.toString())
                        throw IOException(err)
                    }
                }
                val count = chunkCountOf(entry.index)
                val have = haveBitmaps[entry.index]
                for (c in 0 until count) {
                    if (have?.isSet(c) == true) continue
                    val key = keyOf(entry.index, c)
                    if (acked.contains(key)) continue
                    while (inFlight.size >= maxInFlight) {
                        awaitEvent(events)
                    }
                    processAvailable(events)
                    val data = readChunkAt(src, c.toLong() * chunkSize, lengthOf(entry.index, c).toInt())
                    sendChunk(entry.index, c, data)
                    processAvailable(events)
                }
            }

            while (inFlight.isNotEmpty()) {
                awaitEvent(events)
            }
            processAvailable(events)

            // Verify every missing chunk has a real ACK before claiming success.
            for (entry in entries) {
                if (entry.type != "file" || entry.size == 0L) continue
                val have = haveBitmaps[entry.index]
                for (c in 0 until chunkCountOf(entry.index)) {
                    if (have?.isSet(c) == true) continue
                    if (!acked.contains(keyOf(entry.index, c))) {
                        throw IOException("file ${entry.index} chunk $c was never acknowledged")
                    }
                }
            }

            val doneJson = JSONObject().apply { put("transfer_id", transferId) }.toString()
            // DONE belongs on the control connection only.
            writeControl(Protocol.FRAME_DONE, doneJson)

            while (doneOkJson == null) {
                awaitEvent(events, 5 * 60_000L)
            }
            val doneOk = doneOkJson!!
            if (doneOk.optString("transfer_id", "") != transferId) {
                throw IOException("DONE_OK transfer_id mismatch")
            }
            val reportedFiles = doneOk.optInt("files_completed", -1)
            val reportedBytes = doneOk.optLong("bytes", -1L)
            if (reportedFiles < 0 || reportedFiles > fileCount) throw IOException("DONE_OK files_completed out of range")
            if (reportedBytes < 0 || reportedBytes > totalBytes) throw IOException("DONE_OK bytes out of range")

            completed = true
            db.updateTransferProgress(transferId, reportedFiles, reportedBytes, "Completed")
            onCompleted?.invoke(transferId)
            transferId
        } catch (e: Exception) {
            if (!completed) {
                db.setTransferError(transferId, "Interrupted", e.message ?: "send failed")
                onError?.invoke(transferId, e.message ?: "send failed")
            }
            throw e
        } finally {
            try { readerJob?.cancel() } catch (_: Exception) {}
            try { dataSocket?.close() } catch (_: Exception) {}
            try { socket.close() } catch (_: Exception) {}
        }
    }
}
