package com.example.lanvault.transfer

import com.example.lanvault.data.VaultDatabase
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext

/**
 * Thread-safe manager for transfer state transitions.
 * Prevents race conditions when multiple callbacks update the same transfer.
 * 
 * Addresses PC-01: Android transfer history persistence race condition
 * 
 * State machine enforces valid transitions:
 * - Transferring → Completed (success)
 * - Transferring → Failed (error)
 * - Transferring → Paused (user action or timeout)
 * 
 * Terminal states (Completed, Failed) cannot be overwritten.
 */
class TransferStateManager(private val db: VaultDatabase) {
    
    // Per-transfer mutexes to allow concurrent updates to different transfers
    private val transferLocks = mutableMapOf<String, Mutex>()
    private val locksMapMutex = Mutex()
    
    /**
     * Get or create a mutex for a specific transfer ID.
     */
    private suspend fun getLockForTransfer(transferId: String): Mutex {
        return locksMapMutex.withLock {
            transferLocks.getOrPut(transferId) { Mutex() }
        }
    }
    
    /**
     * Update transfer progress atomically.
     * Only updates if transfer is still in active/transferring state.
     * 
     * @param transferId Transfer identifier
     * @param completedFiles Number of files completed
     * @param completedBytes Number of bytes transferred
     * @return true if update succeeded, false if transfer is in terminal state
     */
    suspend fun updateProgress(
        transferId: String,
        completedFiles: Int,
        completedBytes: Long
    ): Boolean = withContext(Dispatchers.IO) {
        val lock = getLockForTransfer(transferId)
        lock.withLock {
            val current = db.getTransfer(transferId) ?: return@withContext false
            
            // Only update if still in active state (prevent overwriting terminal states)
            if (current.state.lowercase() !in listOf("transferring", "paused", "interrupted")) {
                return@withContext false
            }
            
            db.updateTransferProgress(
                transferId = transferId,
                completedFiles = completedFiles,
                completedBytes = completedBytes,
                state = "transferring"
            )
            
            true
        }
    }
    
    /**
     * Mark transfer as completed atomically.
     * Ensures exactly-once completion semantics.
     * 
     * @param transferId Transfer identifier
     * @param completedFiles Total files transferred
     * @param completedBytes Total bytes transferred
     * @return true if state transition succeeded, false if already terminal
     */
    suspend fun markCompleted(
        transferId: String,
        completedFiles: Int,
        completedBytes: Long
    ): Boolean = withContext(Dispatchers.IO) {
        val lock = getLockForTransfer(transferId)
        lock.withLock {
            val current = db.getTransfer(transferId) ?: return@withContext false
            
            // Only transition from active states to completed
            if (current.state.lowercase() !in listOf("transferring", "paused", "interrupted")) {
                return@withContext false
            }
            
            db.updateTransferProgress(
                transferId = transferId,
                completedFiles = completedFiles,
                completedBytes = completedBytes,
                state = "completed"
            )
            
            // Cleanup lock after completion
            cleanupLock(transferId)
            
            true
        }
    }
    
    /**
     * Mark transfer as failed atomically.
     * Records error message and prevents further progress updates.
     * 
     * @param transferId Transfer identifier
     * @param errorMessage Human-readable error description
     * @return true if state transition succeeded, false if already terminal
     */
    suspend fun markFailed(
        transferId: String,
        errorMessage: String
    ): Boolean = withContext(Dispatchers.IO) {
        val lock = getLockForTransfer(transferId)
        lock.withLock {
            val current = db.getTransfer(transferId) ?: return@withContext false
            
            // Only transition from active states to failed
            if (current.state.lowercase() !in listOf("transferring", "paused", "interrupted")) {
                return@withContext false
            }
            
            db.setTransferError(
                transferId = transferId,
                state = "failed",
                message = errorMessage
            )
            
            // Cleanup lock after failure
            cleanupLock(transferId)
            
            true
        }
    }
    
    /**
     * Mark transfer as paused atomically.
     * Used for user-initiated pause or Android 15 foreground service timeout.
     * 
     * @param transferId Transfer identifier
     * @param reason Human-readable pause reason (e.g., "User paused", "Service timeout")
     * @return true if state transition succeeded, false if already terminal
     */
    suspend fun markPaused(
        transferId: String,
        reason: String
    ): Boolean = withContext(Dispatchers.IO) {
        val lock = getLockForTransfer(transferId)
        lock.withLock {
            val current = db.getTransfer(transferId) ?: return@withContext false
            
            // Only transition from transferring to paused
            if (current.state.lowercase() != "transferring") {
                return@withContext false
            }
            
            db.setTransferError(
                transferId = transferId,
                state = "paused",
                message = reason
            )
            
            true
        }
    }
    
    /**
     * Cancel an active transfer atomically.
     * Transitions to failed state with cancellation message.
     * 
     * @param transferId Transfer identifier
     * @return true if cancellation succeeded, false if already terminal
     */
    suspend fun cancelTransfer(transferId: String): Boolean {
        return markFailed(transferId, "Transfer cancelled by user")
    }
    
    /**
     * Remove lock for completed/failed transfer to prevent memory leak.
     * Must be called within the transfer's lock.
     */
    private suspend fun cleanupLock(transferId: String) {
        locksMapMutex.withLock {
            transferLocks.remove(transferId)
        }
    }
}
