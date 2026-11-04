package com.example.lanvault.ui.pairing

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.example.lanvault.crypto.DeviceIdentity
import com.example.lanvault.protocol.Constants
import com.example.lanvault.transfer.TransferEngine
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import java.net.URLEncoder

/**
 * ViewModel for pairing QR code lifecycle management.
 * PC-02-B: Synchronizes QR token lifecycle with Windows desktop.
 * 
 * Manages single token session with countdown, preventing regeneration
 * while Windows is still displaying the same QR code.
 */
class PairingViewModel : ViewModel() {
    
    data class PairingState(
        val qrContent: String = "",
        val remainingSeconds: Int = Constants.PAIR_TOKEN_TTL_S.toInt(),
        val isRefreshing: Boolean = false
    )
    
    private val _state = MutableStateFlow(PairingState())
    val state: StateFlow<PairingState> = _state.asStateFlow()
    
    private var countdownJob: Job? = null
    private var currentTokenBytes: ByteArray? = null
    
    /**
     * Initialize or refresh the QR code.
     * Creates new token and starts countdown timer.
     * 
     * @param identity Device identity for fingerprint
     * @param deviceName Display name for this device
     * @param localIps List of local IP addresses
     * @param transferEngine Optional transfer engine for token generation
     */
    fun refreshQR(
        identity: DeviceIdentity,
        deviceName: String,
        localIps: List<String>,
        transferEngine: TransferEngine?
    ) {
        // Cancel existing countdown
        countdownJob?.cancel()
        
        viewModelScope.launch {
            _state.value = _state.value.copy(isRefreshing = true)
            
            // Generate new QR with fresh token
            val qrContent = transferEngine?.generatePairingQR(localIps, deviceName)
                ?: generateFallbackQR(identity, deviceName, localIps)
            
            // Store token for potential reuse (future enhancement)
            currentTokenBytes = transferEngine?.activePairing?.token
            
            _state.value = PairingState(
                qrContent = qrContent,
                remainingSeconds = Constants.PAIR_TOKEN_TTL_S.toInt(),
                isRefreshing = false
            )
            
            // Start countdown
            startCountdown()
        }
    }
    
    /**
     * Starts countdown timer that auto-refreshes at expiry.
     * Decrements every second until reaching 0, then regenerates QR.
     */
    private fun startCountdown() {
        countdownJob = viewModelScope.launch {
            while (_state.value.remainingSeconds > 0) {
                delay(1000L)
                val newRemaining = _state.value.remainingSeconds - 1
                _state.value = _state.value.copy(remainingSeconds = newRemaining)
                
                if (newRemaining <= 0) {
                    // Auto-refresh when expired (requires re-calling refreshQR from UI)
                    break
                }
            }
        }
    }
    
    /**
     * Manually refresh QR before expiry.
     * Useful when user clicks refresh button.
     */
    fun manualRefresh(
        identity: DeviceIdentity,
        deviceName: String,
        localIps: List<String>,
        transferEngine: TransferEngine?
    ) {
        refreshQR(identity, deviceName, localIps, transferEngine)
    }
    
    /**
     * Pause countdown (e.g., when screen is not visible).
     */
    fun pauseCountdown() {
        countdownJob?.cancel()
    }
    
    /**
     * Resume countdown (e.g., when screen becomes visible again).
     */
    fun resumeCountdown() {
        if (_state.value.remainingSeconds > 0) {
            startCountdown()
        }
    }
    
    /**
     * Fallback QR generation if TransferEngine is unavailable.
     * Does not create actual pairing token (for display only).
     */
    private fun generateFallbackQR(
        identity: DeviceIdentity,
        deviceName: String,
        localIps: List<String>
    ): String {
        val ipStr = localIps.joinToString(",")
        val encodedName = URLEncoder.encode(deviceName, "UTF-8")
        return "lanvault://pair?v=1&fp=${identity.fingerprint}&ip=$ipStr&port=${Constants.DEFAULT_PORT}&name=$encodedName&tok="
    }
    
    override fun onCleared() {
        super.onCleared()
        countdownJob?.cancel()
    }
}
