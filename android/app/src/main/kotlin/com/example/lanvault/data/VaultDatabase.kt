package com.example.lanvault.data

import android.content.ContentValues
import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper

data class TrustedDevice(
    val fingerprint: String,
    val deviceId: String,
    val name: String,
    val platform: String,
    val pairedAt: Long,
    val lastSeen: Long,
    val lastKnownIp: String = ""
)

data class TransferRecord(
    val transferId: String,
    val direction: String,
    val peerFp: String,
    val peerName: String,
    val state: String,
    val totalFiles: Int,
    val totalBytes: Long,
    val completedFiles: Int,
    val completedBytes: Long,
    val manifestHash: String,
    val createdAt: Long,
    val errorMessage: String? = null
)

/**
 * Durable per-file resume state. The bitmap is only advanced after the chunk
 * write has been flushed and its SHA-256 has been persisted in transfer_chunks.
 */
data class StoredFileState(
    val transferId: String,
    val fileIndex: Int,
    val type: String,
    val wirePath: String,
    val finalPath: String,
    val size: Long,
    val mtimeMs: Long,
    val chunkCount: Int,
    val chunkBitmap: ByteArray,
    val completed: Boolean,
    val error: String? = null
)

class VaultDatabase(context: Context) : SQLiteOpenHelper(context, "lanvault.db", null, 2) {

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """
            CREATE TABLE IF NOT EXISTS trusted_devices (
                fingerprint TEXT PRIMARY KEY,
                device_id TEXT NOT NULL,
                name TEXT NOT NULL,
                platform TEXT NOT NULL,
                paired_at INTEGER NOT NULL,
                last_seen INTEGER NOT NULL,
                last_known_ip TEXT DEFAULT ''
            )
            """.trimIndent()
        )

        db.execSQL(
            """
            CREATE TABLE IF NOT EXISTS transfers (
                transfer_id TEXT PRIMARY KEY,
                direction TEXT NOT NULL,
                peer_fp TEXT NOT NULL,
                peer_name TEXT NOT NULL,
                state TEXT NOT NULL,
                total_files INTEGER NOT NULL,
                total_bytes INTEGER NOT NULL,
                completed_files INTEGER NOT NULL,
                completed_bytes INTEGER NOT NULL,
                manifest_hash TEXT NOT NULL,
                created_at INTEGER NOT NULL,
                error_message TEXT
            )
            """.trimIndent()
        )

        db.execSQL(
            """
            CREATE TABLE IF NOT EXISTS settings (
                key TEXT PRIMARY KEY,
                value TEXT NOT NULL
            )
            """.trimIndent()
        )

        // Default settings
        db.execSQL("INSERT OR IGNORE INTO settings (key, value) VALUES ('device_name', 'Android Phone')")
        db.execSQL("INSERT OR IGNORE INTO settings (key, value) VALUES ('visibility', 'everyone')")

        createResumeTables(db)
    }

    private fun createResumeTables(db: SQLiteDatabase) {
        db.execSQL(
            """
            CREATE TABLE IF NOT EXISTS transfer_files (
                transfer_id TEXT NOT NULL,
                file_index INTEGER NOT NULL,
                type TEXT NOT NULL,
                wire_path TEXT NOT NULL,
                final_path TEXT NOT NULL,
                size INTEGER NOT NULL,
                mtime_ms INTEGER NOT NULL,
                chunk_count INTEGER NOT NULL,
                chunk_bitmap BLOB NOT NULL,
                completed INTEGER NOT NULL DEFAULT 0,
                error TEXT,
                PRIMARY KEY (transfer_id, file_index)
            )
            """.trimIndent()
        )
        db.execSQL(
            """
            CREATE TABLE IF NOT EXISTS transfer_chunks (
                transfer_id TEXT NOT NULL,
                file_index INTEGER NOT NULL,
                chunk_index INTEGER NOT NULL,
                sha256 BLOB NOT NULL,
                PRIMARY KEY (transfer_id, file_index, chunk_index)
            )
            """.trimIndent()
        )
        db.execSQL("CREATE INDEX IF NOT EXISTS idx_transfer_files_transfer ON transfer_files(transfer_id)")
        db.execSQL("CREATE INDEX IF NOT EXISTS idx_transfer_chunks_transfer ON transfer_chunks(transfer_id, file_index)")
    }

    override fun onOpen(db: SQLiteDatabase) {
        super.onOpen(db)
        try {
            db.execSQL("ALTER TABLE trusted_devices ADD COLUMN last_known_ip TEXT DEFAULT ''")
        } catch (_: Exception) {}
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        if (oldVersion < 2) {
            createResumeTables(db)
        }
    }

    fun listTrustedDevices(): List<TrustedDevice> {
        val list = mutableListOf<TrustedDevice>()
        val db = readableDatabase
        val cursor = db.rawQuery("SELECT fingerprint, device_id, name, platform, paired_at, last_seen, COALESCE(last_known_ip, '') FROM trusted_devices ORDER BY paired_at DESC", null)
        cursor.use {
            while (it.moveToNext()) {
                list.add(
                    TrustedDevice(
                        fingerprint = it.getString(0),
                        deviceId = it.getString(1),
                        name = it.getString(2),
                        platform = it.getString(3),
                        pairedAt = it.getLong(4),
                        lastSeen = it.getLong(5),
                        lastKnownIp = it.getString(6)
                    )
                )
            }
        }
        return list
    }

    fun addTrustedDevice(device: TrustedDevice) {
        val db = writableDatabase
        val values = ContentValues().apply {
            put("fingerprint", device.fingerprint)
            put("device_id", device.deviceId)
            put("name", device.name)
            put("platform", device.platform)
            put("paired_at", device.pairedAt)
            put("last_seen", device.lastSeen)
            put("last_known_ip", device.lastKnownIp)
        }
        db.insertWithOnConflict("trusted_devices", null, values, SQLiteDatabase.CONFLICT_REPLACE)
    }

    fun removeTrustedDevice(fingerprint: String) {
        val db = writableDatabase
        db.delete("trusted_devices", "fingerprint = ?", arrayOf(fingerprint))
    }

    fun listTransfers(limit: Int = 50): List<TransferRecord> {
        val list = mutableListOf<TransferRecord>()
        val db = readableDatabase
        val cursor = db.rawQuery(
            "SELECT transfer_id, direction, peer_fp, peer_name, state, total_files, total_bytes, completed_files, completed_bytes, manifest_hash, created_at, error_message FROM transfers ORDER BY created_at DESC LIMIT ?",
            arrayOf(limit.toString())
        )
        cursor.use {
            while (it.moveToNext()) {
                list.add(
                    TransferRecord(
                        transferId = it.getString(0),
                        direction = it.getString(1),
                        peerFp = it.getString(2),
                        peerName = it.getString(3),
                        state = it.getString(4),
                        totalFiles = it.getInt(5),
                        totalBytes = it.getLong(6),
                        completedFiles = it.getInt(7),
                        completedBytes = it.getLong(8),
                        manifestHash = it.getString(9),
                        createdAt = it.getLong(10),
                        errorMessage = if (it.isNull(11)) null else it.getString(11)
                    )
                )
            }
        }
        return list
    }

    fun recordTransfer(record: TransferRecord) {
        val db = writableDatabase
        val values = ContentValues().apply {
            put("transfer_id", record.transferId)
            put("direction", record.direction)
            put("peer_fp", record.peerFp)
            put("peer_name", record.peerName)
            put("state", record.state)
            put("total_files", record.totalFiles)
            put("total_bytes", record.totalBytes)
            put("completed_files", record.completedFiles)
            put("completed_bytes", record.completedBytes)
            put("manifest_hash", record.manifestHash)
            put("created_at", record.createdAt)
            put("error_message", record.errorMessage)
        }
        db.insertWithOnConflict("transfers", null, values, SQLiteDatabase.CONFLICT_REPLACE)
    }

    fun updateTransferProgress(transferId: String, completedFiles: Int, completedBytes: Long, state: String) {
        val db = writableDatabase
        val values = ContentValues().apply {
            put("completed_files", completedFiles)
            put("completed_bytes", completedBytes)
            put("state", state)
        }
        db.update("transfers", values, "transfer_id = ?", arrayOf(transferId))
    }

    fun setTransferError(transferId: String, state: String, message: String) {
        val db = writableDatabase
        val values = ContentValues().apply {
            put("state", state)
            put("error_message", message)
        }
        db.update("transfers", values, "transfer_id = ?", arrayOf(transferId))
    }

    fun isTrusted(fingerprint: String): Boolean {
        val db = readableDatabase
        db.rawQuery("SELECT 1 FROM trusted_devices WHERE fingerprint = ?", arrayOf(fingerprint)).use {
            return it.moveToFirst()
        }
    }

    fun getTrustedDevice(fingerprint: String): TrustedDevice? {
        val db = readableDatabase
        db.rawQuery(
            "SELECT fingerprint, device_id, name, platform, paired_at, last_seen, COALESCE(last_known_ip, '') FROM trusted_devices WHERE fingerprint = ?",
            arrayOf(fingerprint)
        ).use {
            if (!it.moveToFirst()) return null
            return TrustedDevice(
                fingerprint = it.getString(0),
                deviceId = it.getString(1),
                name = it.getString(2),
                platform = it.getString(3),
                pairedAt = it.getLong(4),
                lastSeen = it.getLong(5),
                lastKnownIp = it.getString(6)
            )
        }
    }

    fun getTransfer(transferId: String): TransferRecord? {
        val db = readableDatabase
        db.rawQuery(
            "SELECT transfer_id, direction, peer_fp, peer_name, state, total_files, total_bytes, completed_files, completed_bytes, manifest_hash, created_at, error_message FROM transfers WHERE transfer_id = ?",
            arrayOf(transferId)
        ).use {
            if (!it.moveToFirst()) return null
            return TransferRecord(
                transferId = it.getString(0),
                direction = it.getString(1),
                peerFp = it.getString(2),
                peerName = it.getString(3),
                state = it.getString(4),
                totalFiles = it.getInt(5),
                totalBytes = it.getLong(6),
                completedFiles = it.getInt(7),
                completedBytes = it.getLong(8),
                manifestHash = it.getString(9),
                createdAt = it.getLong(10),
                errorMessage = if (it.isNull(11)) null else it.getString(11)
            )
        }
    }

    /** Finds an unfinished send for the same peer and identical manifest bytes. */
    fun findResumableSend(manifestHash: String, peerFp: String): TransferRecord? {
        val db = readableDatabase
        db.rawQuery(
            "SELECT transfer_id, direction, peer_fp, peer_name, state, total_files, total_bytes, completed_files, completed_bytes, manifest_hash, created_at, error_message " +
                "FROM transfers WHERE direction = 'send' AND peer_fp = ? AND manifest_hash = ? AND state IN ('Transferring','Interrupted','Paused') ORDER BY created_at DESC LIMIT 1",
            arrayOf(peerFp, manifestHash)
        ).use {
            if (!it.moveToFirst()) return null
            return TransferRecord(
                transferId = it.getString(0),
                direction = it.getString(1),
                peerFp = it.getString(2),
                peerName = it.getString(3),
                state = it.getString(4),
                totalFiles = it.getInt(5),
                totalBytes = it.getLong(6),
                completedFiles = it.getInt(7),
                completedBytes = it.getLong(8),
                manifestHash = it.getString(9),
                createdAt = it.getLong(10),
                errorMessage = if (it.isNull(11)) null else it.getString(11)
            )
        }
    }

    fun deleteTransferState(transferId: String) {
        val db = writableDatabase
        db.beginTransaction()
        try {
            db.delete("transfer_chunks", "transfer_id = ?", arrayOf(transferId))
            db.delete("transfer_files", "transfer_id = ?", arrayOf(transferId))
            db.delete("transfers", "transfer_id = ?", arrayOf(transferId))
            db.setTransactionSuccessful()
        } finally {
            db.endTransaction()
        }
    }

    fun getTransferFiles(transferId: String): List<StoredFileState> {
        val list = mutableListOf<StoredFileState>()
        val db = readableDatabase
        db.rawQuery(
            "SELECT transfer_id, file_index, type, wire_path, final_path, size, mtime_ms, chunk_count, chunk_bitmap, completed, error FROM transfer_files WHERE transfer_id = ? ORDER BY file_index",
            arrayOf(transferId)
        ).use {
            while (it.moveToNext()) {
                list.add(
                    StoredFileState(
                        transferId = it.getString(0),
                        fileIndex = it.getInt(1),
                        type = it.getString(2),
                        wirePath = it.getString(3),
                        finalPath = it.getString(4),
                        size = it.getLong(5),
                        mtimeMs = it.getLong(6),
                        chunkCount = it.getInt(7),
                        chunkBitmap = it.getBlob(8) ?: ByteArray(0),
                        completed = it.getInt(9) != 0,
                        error = if (it.isNull(10)) null else it.getString(10)
                    )
                )
            }
        }
        return list
    }

    fun upsertTransferFile(state: StoredFileState) {
        val db = writableDatabase
        val values = ContentValues().apply {
            put("transfer_id", state.transferId)
            put("file_index", state.fileIndex)
            put("type", state.type)
            put("wire_path", state.wirePath)
            put("final_path", state.finalPath)
            put("size", state.size)
            put("mtime_ms", state.mtimeMs)
            put("chunk_count", state.chunkCount)
            put("chunk_bitmap", state.chunkBitmap)
            put("completed", if (state.completed) 1 else 0)
            put("error", state.error)
        }
        db.insertWithOnConflict("transfer_files", null, values, SQLiteDatabase.CONFLICT_REPLACE)
    }

    fun updateChunkBitmap(transferId: String, fileIndex: Int, bitmap: ByteArray, completed: Boolean) {
        val db = writableDatabase
        val values = ContentValues().apply {
            put("chunk_bitmap", bitmap)
            put("completed", if (completed) 1 else 0)
        }
        db.update("transfer_files", values, "transfer_id = ? AND file_index = ?", arrayOf(transferId, fileIndex.toString()))
    }

    fun updateFileFinalPath(transferId: String, fileIndex: Int, finalPath: String) {
        val db = writableDatabase
        val values = ContentValues().apply { put("final_path", finalPath) }
        db.update("transfer_files", values, "transfer_id = ? AND file_index = ?", arrayOf(transferId, fileIndex.toString()))
    }

    fun setFileError(transferId: String, fileIndex: Int, error: String) {
        val db = writableDatabase
        val values = ContentValues().apply { put("error", error) }
        db.update("transfer_files", values, "transfer_id = ? AND file_index = ?", arrayOf(transferId, fileIndex.toString()))
    }

    fun saveChunkHash(transferId: String, fileIndex: Int, chunkIndex: Int, hash: ByteArray) {
        val db = writableDatabase
        val values = ContentValues().apply {
            put("transfer_id", transferId)
            put("file_index", fileIndex)
            put("chunk_index", chunkIndex)
            put("sha256", hash)
        }
        db.insertWithOnConflict("transfer_chunks", null, values, SQLiteDatabase.CONFLICT_REPLACE)
    }

    fun getChunkHashes(transferId: String, fileIndex: Int): Map<Int, ByteArray> {
        val result = mutableMapOf<Int, ByteArray>()
        val db = readableDatabase
        db.rawQuery(
            "SELECT chunk_index, sha256 FROM transfer_chunks WHERE transfer_id = ? AND file_index = ?",
            arrayOf(transferId, fileIndex.toString())
        ).use {
            while (it.moveToNext()) {
                result[it.getInt(0)] = it.getBlob(1) ?: ByteArray(0)
            }
        }
        return result
    }

    fun getSetting(key: String, defaultValue: String): String {
        val db = readableDatabase
        val cursor = db.rawQuery("SELECT value FROM settings WHERE key = ?", arrayOf(key))
        cursor.use {
            if (it.moveToFirst()) return it.getString(0)
        }
        return defaultValue
    }

    fun setSetting(key: String, value: String) {
        val db = writableDatabase
        val values = ContentValues().apply {
            put("key", key)
            put("value", value)
        }
        db.insertWithOnConflict("settings", null, values, SQLiteDatabase.CONFLICT_REPLACE)
    }
}
