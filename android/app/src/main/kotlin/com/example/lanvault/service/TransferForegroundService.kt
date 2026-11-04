package com.example.lanvault.service

import android.app.*
import android.content.Context
import android.content.Intent
import android.graphics.BitmapFactory
import android.os.Binder
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import com.example.lanvault.R
import com.example.lanvault.crypto.IdentityManager
import com.example.lanvault.data.TransferRecord
import com.example.lanvault.data.VaultDatabase
import com.example.lanvault.discovery.DiscoveryService
import com.example.lanvault.transfer.TransferEngine
import com.example.lanvault.transfer.TransferProgress
import com.example.lanvault.transfer.TransferStateManager
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
    private lateinit var stateManager: TransferStateManager

    var activeProgress: TransferProgress? = null
        private set

    var pendingOffer: IncomingOfferState? = null
        private set

    private var offerTimeoutJob: Job? = null

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
        stateManager = TransferStateManager(db)
        val identity = IdentityManager.getOrCreateIdentity(this)

        createNotificationChannels()

        transferEngine = TransferEngine(this, identity).apply {
            onIncomingOffer = { id, name, fp, count, bytes, cb ->
                pendingOffer = IncomingOfferState(id, name, fp, count, bytes, cb)
                showIncomingOfferNotification(name, count, bytes)
                // Schedule 300s timeout per protocol specification
                offerTimeoutJob?.cancel()
                offerTimeoutJob = scope.launch {
                    delay(com.example.lanvault.protocol.Constants.OFFER_RESPONSE_TIMEOUT_S * 1000L)
                    if (pendingOffer != null) {
                        respondToIncomingOffer(false)
                        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
                        nm.cancel(NOTIFICATION_ID_REQUEST)
                    }
                }
            }
            onProgress = { p ->
                activeProgress = p
                updateNotificationProgress(p)
                // Thread-safe progress update using TransferStateManager
                scope.launch {
                    stateManager.updateProgress(
                        transferId = p.transferId,
                        completedFiles = p.filesDone,
                        completedBytes = p.bytesAcked
                    )
                }
            }
            onCompleted = { id ->
                activeProgress = null
                // Thread-safe completion using TransferStateManager
                scope.launch {
                    val transfer = db.getTransfer(id)
                    if (transfer != null) {
                        stateManager.markCompleted(
                            transferId = id,
                            completedFiles = transfer.totalFiles,
                            completedBytes = transfer.totalBytes
                        )
                    }
                }
                showCompletedNotification()
                stopForeground(STOP_FOREGROUND_REMOVE)
            }
            onError = { id, err ->
                activeProgress = null
                // Thread-safe error handling using TransferStateManager
                scope.launch {
                    stateManager.markFailed(id, err)
                }
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
        when (intent?.action) {
            ACTION_ACCEPT_OFFER -> {
                respondToIncomingOffer(true)
                val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
                nm.cancel(NOTIFICATION_ID_REQUEST)
            }
            ACTION_DECLINE_OFFER -> {
                respondToIncomingOffer(false)
                val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
                nm.cancel(NOTIFICATION_ID_REQUEST)
            }
            else -> {
                val notif = buildServiceNotification("LANVault service ready")
                // PC-04-A: Android API 29+ foreground service type specification
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                    startForeground(
                        NOTIFICATION_ID_SERVICE,
                        notif,
                        android.content.pm.ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
                    )
                } else {
                    startForeground(NOTIFICATION_ID_SERVICE, notif)
                }
            }
        }
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onDestroy() {
        super.onDestroy()
        offerTimeoutJob?.cancel()
        discovery.stop()
        transferEngine.stopServer()
        scope.cancel()
    }

    // Android 15 dataSync service timeout handling (PRD line 216 & line 575)
    override fun onTimeout(startId: Int, fgsType: Int) {
        activeProgress?.let { p ->
            // Thread-safe pause using TransferStateManager
            scope.launch {
                stateManager.markPaused(
                    transferId = p.transferId,
                    reason = "Android 15 foreground service timeout (PC-04)"
                )
            }
        }
        stopSelf()
    }

    fun respondToIncomingOffer(accept: Boolean) {
        offerTimeoutJob?.cancel()
        offerTimeoutJob = null
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

    fun sendSources(
        targetIp: String,
        targetPort: Int,
        sources: List<com.example.lanvault.transfer.SendSource>,
        peerFingerprint: String? = null,
        onComplete: (String?) -> Unit
    ) {
        scope.launch {
            try {
                val tid = transferEngine.startSendSources(targetIp, targetPort, sources, peerFingerprint)
                db.recordTransfer(
                    TransferRecord(
                        transferId = tid,
                        direction = "send",
                        peerFp = peerFingerprint ?: "",
                        peerName = targetIp,
                        state = "completed",
                        totalFiles = sources.size,
                        totalBytes = sources.sumOf { it.size },
                        completedFiles = sources.size,
                        completedBytes = sources.sumOf { it.size },
                        manifestHash = "",
                        createdAt = System.currentTimeMillis()
                    )
                )
                onComplete(tid)
            } catch (_: Exception) {
                onComplete(null)
            }
        }
    }

    fun sendFiles(targetIp: String, targetPort: Int, files: List<File>, onComplete: (String?) -> Unit) {
        val sources = files.map { file ->
            com.example.lanvault.transfer.SendSource(
                name = file.name,
                size = file.length(),
                mtime = file.lastModified(),
                isDirectory = file.isDirectory,
                file = file
            )
        }
        sendSources(targetIp, targetPort, sources, onComplete = onComplete)
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
        val largeIcon = try { BitmapFactory.decodeResource(resources, R.drawable.ic_logo) } catch (_: Exception) { null }
        return NotificationCompat.Builder(this, CHANNEL_SERVICE)
            .setContentTitle("LANVault")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .apply { if (largeIcon != null) setLargeIcon(largeIcon) }
            .setOngoing(true)
            .build()
    }

    private fun updateNotificationProgress(p: TransferProgress) {
        val pct = if (p.totalBytes > 0) ((p.bytesAcked * 100) / p.totalBytes).toInt() else 0
        val mbPerSec = p.speedBps / 1_000_000.0
        val text = "${p.filesDone}/${p.totalFiles} files · ${String.format("%.1f", mbPerSec)} MB/s"
        val largeIcon = try { BitmapFactory.decodeResource(resources, R.drawable.ic_logo) } catch (_: Exception) { null }

        val notif = NotificationCompat.Builder(this, CHANNEL_PROGRESS)
            .setContentTitle("Transferring ($pct%)")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .apply { if (largeIcon != null) setLargeIcon(largeIcon) }
            .setProgress(100, pct, false)
            .setOngoing(true)
            .build()

        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(NOTIFICATION_ID_PROGRESS, notif)
    }

    private fun showIncomingOfferNotification(senderName: String, fileCount: Int, totalBytes: Long) {
        val mb = totalBytes / 1_000_000.0
        val text = "$senderName wants to send $fileCount files (${String.format("%.1f", mb)} MB)"
        val largeIcon = try { BitmapFactory.decodeResource(resources, R.drawable.ic_logo) } catch (_: Exception) { null }

        val acceptIntent = Intent(this, TransferForegroundService::class.java).apply {
            action = ACTION_ACCEPT_OFFER
        }
        val acceptPending = PendingIntent.getService(this, 1, acceptIntent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)

        val declineIntent = Intent(this, TransferForegroundService::class.java).apply {
            action = ACTION_DECLINE_OFFER
        }
        val declinePending = PendingIntent.getService(this, 2, declineIntent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)

        val notif = NotificationCompat.Builder(this, CHANNEL_REQUEST)
            .setContentTitle("Incoming LANVault Transfer")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_sys_download_done)
            .apply { if (largeIcon != null) setLargeIcon(largeIcon) }
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setAutoCancel(true)
            .addAction(android.R.drawable.ic_input_add, "Accept", acceptPending)
            .addAction(android.R.drawable.ic_delete, "Decline", declinePending)
            .build()

        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(NOTIFICATION_ID_REQUEST, notif)
    }

    private fun showCompletedNotification() {
        val largeIcon = try { BitmapFactory.decodeResource(resources, R.drawable.ic_logo) } catch (_: Exception) { null }
        val notif = NotificationCompat.Builder(this, CHANNEL_SERVICE)
            .setContentTitle("Transfer Completed")
            .setContentText("Files have been saved to Downloads/LANVault")
            .setSmallIcon(android.R.drawable.stat_sys_download_done)
            .apply { if (largeIcon != null) setLargeIcon(largeIcon) }
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

        const val ACTION_ACCEPT_OFFER = "com.example.lanvault.ACTION_ACCEPT_OFFER"
        const val ACTION_DECLINE_OFFER = "com.example.lanvault.ACTION_DECLINE_OFFER"
    }
}
