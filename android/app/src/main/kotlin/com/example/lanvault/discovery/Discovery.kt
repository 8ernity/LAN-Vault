package com.example.lanvault.discovery

import android.content.Context
import android.net.wifi.WifiManager
import com.example.lanvault.protocol.Protocol
import kotlinx.coroutines.*
import org.json.JSONObject
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.InetSocketAddress
import java.nio.charset.StandardCharsets
import java.util.concurrent.ConcurrentHashMap

data class DiscoveredPeer(
    val deviceId: String,
    val name: String,
    val platform: String,
    val ips: List<String>,
    val port: Int,
    val lastSeen: Long
)

class DiscoveryService(
    private val context: Context,
    private val deviceId: String,
    private val deviceNameProvider: () -> String,
    private val visibilityProvider: () -> String
) {
    private val peers = ConcurrentHashMap<String, DiscoveredPeer>()
    private var scope: CoroutineScope? = null
    private var socket: DatagramSocket? = null
    private var multicastLock: WifiManager.MulticastLock? = null

    var onPeersUpdated: ((List<DiscoveredPeer>) -> Unit)? = null

    fun start() {
        if (scope != null) return
        val newScope = CoroutineScope(Dispatchers.IO + SupervisorJob())
        scope = newScope

        try {
            val wifi = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
            multicastLock = wifi?.createMulticastLock("lanvault_discovery")?.apply {
                setReferenceCounted(true)
                acquire()
            }
        } catch (_: Exception) {}

        // Start listener
        newScope.launch {
            listenForBeacons()
        }

        // Start broadcaster
        newScope.launch {
            broadcastLoop()
        }

        // Expiry cleanup loop
        newScope.launch {
            while (isActive) {
                delay(3000)
                val now = System.currentTimeMillis()
                val removed = peers.entries.removeIf { now - it.value.lastSeen > 10_000 }
                if (removed) {
                    onPeersUpdated?.invoke(getPeers())
                }
            }
        }
    }

    fun stop() {
        scope?.cancel()
        scope = null
        try {
            socket?.close()
        } catch (_: Exception) {}
        try {
            multicastLock?.release()
        } catch (_: Exception) {}
        peers.clear()
    }

    fun getPeers(): List<DiscoveredPeer> = peers.values.toList().sortedByDescending { it.lastSeen }

    fun addManualPeer(peer: DiscoveredPeer) {
        peers[peer.deviceId] = peer
        onPeersUpdated?.invoke(getPeers())
    }

    private fun getBroadcastAddresses(): List<InetAddress> {
        val list = mutableListOf<InetAddress>()
        try {
            val interfaces = java.net.NetworkInterface.getNetworkInterfaces()
            while (interfaces.hasMoreElements()) {
                val iface = interfaces.nextElement()
                if (iface.isLoopback || !iface.isUp) continue
                for (addr in iface.interfaceAddresses) {
                    val bcast = addr.broadcast
                    if (bcast != null) {
                        list.add(bcast)
                    }
                }
            }
        } catch (_: Exception) {}
        try {
            list.add(InetAddress.getByName("255.255.255.255"))
        } catch (_: Exception) {}
        return list.distinct()
    }

    private suspend fun listenForBeacons() = withContext(Dispatchers.IO) {
        try {
            val s = DatagramSocket(null).apply {
                reuseAddress = true
                broadcast = true
                bind(InetSocketAddress(Protocol.BEACON_PORT))
            }
            socket = s
            val buf = ByteArray(2048)

            while (isActive) {
                val packet = DatagramPacket(buf, buf.size)
                s.receive(packet)
                val text = String(packet.data, 0, packet.length, StandardCharsets.UTF_8)
                val senderIp = packet.address.hostAddress ?: continue

                try {
                    val json = JSONObject(text)
                    val peerId = json.optString("id").ifEmpty { json.optString("device_id") }
                    if (peerId.isNotBlank() && peerId != deviceId) {
                        val name = json.optString("name", "LANVault Device")
                        val platform = json.optString("plat").ifEmpty { json.optString("platform", "unknown") }
                        val port = json.optInt("port", Protocol.DEFAULT_PORT)

                        val peer = DiscoveredPeer(
                            deviceId = peerId,
                            name = name,
                            platform = platform,
                            ips = listOf(senderIp),
                            port = port,
                            lastSeen = System.currentTimeMillis()
                        )
                        peers[peerId] = peer
                        onPeersUpdated?.invoke(getPeers())
                    }
                } catch (_: Exception) {}
            }
        } catch (_: Exception) {}
    }

    private suspend fun broadcastLoop() = withContext(Dispatchers.IO) {
        while (isActive) {
            val vis = visibilityProvider()
            if (vis != "hidden") {
                try {
                    val json = JSONObject().apply {
                        put("v", 1)
                        put("id", deviceId)
                        put("device_id", deviceId)
                        put("name", deviceNameProvider())
                        put("plat", "android")
                        put("platform", "android")
                        put("port", Protocol.DEFAULT_PORT)
                        put("type", "beacon")
                        put("visibility", vis)
                    }.toString()

                    val bytes = json.toByteArray(StandardCharsets.UTF_8)
                    val addrs = getBroadcastAddresses()
                    for (addr in addrs) {
                        try {
                            val packet = DatagramPacket(bytes, bytes.size, addr, Protocol.BEACON_PORT)
                            val s = DatagramSocket().apply { broadcast = true }
                            s.send(packet)
                            s.close()
                        } catch (_: Exception) {}
                    }
                } catch (_: Exception) {}
            }
            delay(Protocol.BEACON_INTERVAL_MS.toLong())
        }
    }
}
