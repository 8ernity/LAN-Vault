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

class VaultDatabase(context: Context) : SQLiteOpenHelper(context, "lanvault.db", null, 1) {

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
    }

    override fun onOpen(db: SQLiteDatabase) {
        super.onOpen(db)
        try {
            db.execSQL("ALTER TABLE trusted_devices ADD COLUMN last_known_ip TEXT DEFAULT ''")
        } catch (_: Exception) {}
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        // Migration logic for future versions
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
