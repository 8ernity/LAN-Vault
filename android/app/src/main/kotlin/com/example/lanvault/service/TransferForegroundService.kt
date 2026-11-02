package com.example.lanvault.service

import android.app.*
import android.content.Context
import android.content.Intent
import android.os.Binder
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import com.example.lanvault.crypto.IdentityManager
import com.example.lanvault.data.TransferRecord
import com.example.lanvault.data.VaultDatabase
import com.example.lanvault.discovery.DiscoveryService
import com.example.lanvault.transfer.TransferEngine
import com.example.lanvault.transfer.TransferProgress
import kotlinx.coroutines.*
import java.io.File

class TransferForegroundService : Service() {

    inner class LocalBinder : Binder() {
        val service: TransferForegroundService get() = this@TransferForegroundService
    }

    private val binder = LocalBinder()
    private val scope = CoroutineScope(Dispatchers.Main + SupervisorJob())

    lateinit var db: VaultDatabase
    lateinit var transferEngine: TransferEngine
    lateinit var discovery: DiscoveryService

    var activeProgress: TransferProgress? = null
        private set

    var pendingOffer: IncomingOfferState? = null
        private set

    data class IncomingOfferState(
        val transferId: String,
        val peerName: String,
        val peerFp: String,
        val fileCount: Int,
        val totalBytes: Long,
        val callback: (Boolean) -> Unit
    )

    override fun onCreate() {
        super.onCreate()
        db = VaultDatabase(this)
        val identity = IdentityManager.getOrCreateIdentity(this)

        createNotificationChannels()

        transferEngine = TransferEngine(this, identity).apply {
            onIncomingOffer = { id, name, fp, count, bytes, cb ->
                pendingOffer = IncomingOfferState(id, name, fp, count, bytes, cb)
                showIncomingOfferNotification(name, count, bytes)
            }
            onProgress = { p ->
                activeProgress = p
                updateNotificationProgress(p)
            }
            onCompleted = { id ->
                activeProgress = null
                db.updateTransferProgress(id, 0, 0, "completed")
                showCompletedNotification()
                stopForeground(STOP_FOREGROUND_REMOVE)
            }
            onError = { id, err ->
                activeProgress = null
                db.updateTransferProgress(id, 0, 0, "failed")
                stopForeground(STOP_FOREGROUND_REMOVE)
            }
        }

        discovery = DiscoveryService(
            context = this,
            deviceId = identity.deviceId,
            deviceNameProvider = { db.getSetting("device_name", "Android Phone") },
            visibilityProvider = { db.getSetting("visibility", "contacts_only") }
        )

        discovery.start()
        transferEngine.startServer(scope)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val notif = buildServiceNotification("LANVault service ready")
        startForeground(NOTIFICATION_ID_SERVICE, notif)
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onDestroy() {
        super.onDestroy()
        discovery.stop()
        transferEngine.stopServer()
        scope.cancel()
    }

    // Android 15 dataSync service timeout handling (PRD line 216 & line 575)
    override fun onTimeout(startId: Int, fgsType: Int) {
        activeProgress?.let {
            db.updateTransferProgress(it.transferId, it.filesDone, it.bytesAcked, "paused_timeout")
        }
        stopSelf()
    }

    fun respondToIncomingOffer(accept: Boolean) {
        pendingOffer?.let {
            it.callback(accept)
            if (accept) {
                db.recordTransfer(
                    TransferRecord(
                        transferId = it.transferId,
                        direction = "receive",
                        peerFp = it.peerFp,
                        peerName = it.peerName,
                        state = "transferring",
                        totalFiles = it.fileCount,
                        totalBytes = it.totalBytes,
                        completedFiles = 0,
                        completedBytes = 0L,
                        manifestHash = "",
                        createdAt = System.currentTimeMillis()
                    )
                )
            }
        }
        pendingOffer = null
    }

    fun sendFiles(targetIp: String, targetPort: Int, files: List<File>, onComplete: (String?) -> Unit) {
        scope.launch {
            try {
                val tid = transferEngine.startSend(targetIp, targetPort, files)
                db.recordTransfer(
                    TransferRecord(
                        transferId = tid,
                        direction = "send",
                        peerFp = "",
                        peerName = targetIp,
                        state = "completed",
                        totalFiles = files.size,
                        totalBytes = files.sumOf { it.length() },
                        completedFiles = files.size,
                        completedBytes = files.sumOf { it.length() },
                        manifestHash = "",
                        createdAt = System.currentTimeMillis()
                    )
                )
                onComplete(tid)
            } catch (e: Exception) {
                onComplete(null)
            }
        }
    }

    private fun createNotificationChannels() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val nm = getSystemService(NotificationManager::class.java)

            val serviceChan = NotificationChannel(
                CHANNEL_SERVICE,
                "Service Status",
                NotificationManager.IMPORTANCE_LOW
            )
            val progressChan = NotificationChannel(
                CHANNEL_PROGRESS,
                "Transfer Progress",
                NotificationManager.IMPORTANCE_LOW
            )
            val requestChan = NotificationChannel(
                CHANNEL_REQUEST,
                "Incoming Requests",
                NotificationManager.IMPORTANCE_HIGH
            )

            nm.createNotificationChannel(serviceChan)
            nm.createNotificationChannel(progressChan)
            nm.createNotificationChannel(requestChan)
        }
    }

    private fun buildServiceNotification(text: String): Notification {
        return NotificationCompat.Builder(this, CHANNEL_SERVICE)
            .setContentTitle("LANVault")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setOngoing(true)
            .build()
    }

    private fun updateNotificationProgress(p: TransferProgress) {
        val pct = if (p.totalBytes > 0) ((p.bytesAcked * 100) / p.totalBytes).toInt() else 0
        val mbPerSec = p.speedBps / 1_000_000.0
        val text = "${p.filesDone}/${p.totalFiles} files · ${String.format("%.1f", mbPerSec)} MB/s"

        val notif = NotificationCompat.Builder(this, CHANNEL_PROGRESS)
            .setContentTitle("Transferring ($pct%)")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setProgress(100, pct, false)
            .setOngoing(true)
            .build()

        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(NOTIFICATION_ID_PROGRESS, notif)
    }

    private fun showIncomingOfferNotification(senderName: String, fileCount: Int, totalBytes: Long) {
        val mb = totalBytes / 1_000_000.0
        val text = "$senderName wants to send $fileCount files (${String.format("%.1f", mb)} MB)"

        val notif = NotificationCompat.Builder(this, CHANNEL_REQUEST)
            .setContentTitle("Incoming LANVault Transfer")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_sys_download_done)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setAutoCancel(true)
            .build()

        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(NOTIFICATION_ID_REQUEST, notif)
    }

    private fun showCompletedNotification() {
        val notif = NotificationCompat.Builder(this, CHANNEL_SERVICE)
            .setContentTitle("Transfer Completed")
            .setContentText("Files have been saved to Downloads/LANVault")
            .setSmallIcon(android.R.drawable.stat_sys_download_done)
            .setAutoCancel(true)
            .build()

        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(NOTIFICATION_ID_COMPLETED, notif)
    }

    companion object {
        const val CHANNEL_SERVICE = "lanvault_service"
        const val CHANNEL_PROGRESS = "lanvault_progress"
        const val CHANNEL_REQUEST = "lanvault_requests"

        const val NOTIFICATION_ID_SERVICE = 1001
        const val NOTIFICATION_ID_PROGRESS = 1002
        const val NOTIFICATION_ID_REQUEST = 1003
        const val NOTIFICATION_ID_COMPLETED = 1004
    }
}
