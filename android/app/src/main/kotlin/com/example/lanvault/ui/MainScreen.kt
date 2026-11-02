package com.example.lanvault.ui

import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.net.Uri
import android.provider.OpenableColumns
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.*
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.documentfile.provider.DocumentFile
import com.example.lanvault.crypto.IdentityManager
import com.example.lanvault.data.TransferRecord
import com.example.lanvault.data.TrustedDevice
import com.example.lanvault.data.VaultDatabase
import com.example.lanvault.discovery.DiscoveredPeer
import com.example.lanvault.protocol.Protocol
import com.example.lanvault.service.TransferForegroundService
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.io.FileOutputStream

enum class NavTab {
    HOME, SEND, RECEIVE, TRANSFERS, DEVICES
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MainScreen(
    service: TransferForegroundService?,
    initialSharedUris: List<Uri> = emptyList(),
    onClearSharedUris: (() -> Unit)? = null
) {
    val context = LocalContext.current
    val coroutineScope = rememberCoroutineScope()
    var currentTab by remember { mutableStateOf(NavTab.HOME) }

    val identity = remember { IdentityManager.getOrCreateIdentity(context) }
    val db = remember { VaultDatabase(context) }

    var peers by remember { mutableStateOf<List<DiscoveredPeer>>(emptyList()) }
    var transfers by remember { mutableStateOf<List<TransferRecord>>(emptyList()) }
    var trustedDevices by remember { mutableStateOf<List<TrustedDevice>>(emptyList()) }
    var deviceName by remember { mutableStateOf(db.getSetting("device_name", "Android Phone")) }
    var visibility by remember { mutableStateOf(db.getSetting("visibility", "contacts_only")) }
    var savePathDisplay by remember {
        mutableStateOf(db.getSetting("save_path_display", "Downloads/LANVault"))
    }
    var pendingSendSources by remember { mutableStateOf<List<com.example.lanvault.transfer.SendSource>>(emptyList()) }

    LaunchedEffect(initialSharedUris) {
        if (initialSharedUris.isNotEmpty()) {
            val sources = initialSharedUris.map { uri -> getSendSourceFromUri(context, uri) }
            pendingSendSources = pendingSendSources + sources
            currentTab = NavTab.SEND
            onClearSharedUris?.invoke()
        }
    }

    val folderPickerLauncher = rememberLauncherForActivityResult(
        contract = ActivityResultContracts.OpenDocumentTree()
    ) { uri: Uri? ->
        if (uri != null) {
            try {
                val takeFlags: Int = Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION
                context.contentResolver.takePersistableUriPermission(uri, takeFlags)

                val docFile = DocumentFile.fromTreeUri(context, uri)
                val displayName = getFolderDisplayName(context, uri, docFile)

                db.setSetting("save_tree_uri", uri.toString())
                db.setSetting("save_path_display", displayName)
                savePathDisplay = displayName

                Toast.makeText(context, "Save location changed to: $displayName", Toast.LENGTH_SHORT).show()
            } catch (e: Exception) {
                Toast.makeText(context, "Failed to set location: ${e.message}", Toast.LENGTH_SHORT).show()
            }
        }
    }

    var selectedPeerForSend by remember { mutableStateOf<DiscoveredPeer?>(null) }
    var showSettingsDialog by remember { mutableStateOf(false) }
    var showScanner by remember { mutableStateOf(false) }

    // Refresh state periodically
    LaunchedEffect(Unit) {
        service?.discovery?.onPeersUpdated = { list ->
            peers = list
        }
        while (true) {
            peers = service?.discovery?.getPeers() ?: emptyList()
            transfers = db.listTransfers()
            trustedDevices = db.listTrustedDevices()
            kotlinx.coroutines.delay(2000)
        }
    }

    if (showScanner) {
        QRScannerView(
            onQrScanned = { qrText ->
                showScanner = false
                coroutineScope.launch {
                    Toast.makeText(context, "Pairing with peer...", Toast.LENGTH_SHORT).show()
                    val engine = service?.transferEngine
                    if (engine != null) {
                        val result = engine.pairWithPeer(qrText)
                        if (result.isSuccess) {
                            val peerName = result.getOrNull() ?: "peer"
                            trustedDevices = db.listTrustedDevices()
                            Toast.makeText(context, "Successfully paired with $peerName!", Toast.LENGTH_SHORT).show()
                            currentTab = NavTab.SEND
                        } else {
                            val msg = result.exceptionOrNull()?.message ?: "Unknown error"
                            Toast.makeText(context, "Pairing failed: $msg", Toast.LENGTH_LONG).show()
                        }
                    } else {
                        Toast.makeText(context, "Transfer service not ready", Toast.LENGTH_SHORT).show()
                    }
                }
            },
            onClose = { showScanner = false }
        )
        return
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("LANVault", fontWeight = FontWeight.Bold, fontSize = 18.sp)
                        Text(
                            text = when (currentTab) {
                                NavTab.HOME -> "Network Vault"
                                NavTab.SEND -> "Send Files"
                                NavTab.RECEIVE -> "Receive Files"
                                NavTab.TRANSFERS -> "Transfer History"
                                NavTab.DEVICES -> "Trusted Devices"
                            },
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                    }
                },
                actions = {
                    // Quick Scan QR Button in Top Bar
                    IconButton(onClick = { showScanner = true }) {
                        Icon(
                            imageVector = Icons.Default.QrCodeScanner,
                            contentDescription = "Scan QR",
                            tint = MaterialTheme.colorScheme.primary
                        )
                    }

                    // Visibility Chip
                    AssistChip(
                        onClick = {
                            val next = if (visibility == "everyone") "contacts_only" else "everyone"
                            visibility = next
                            db.setSetting("visibility", next)
                        },
                        label = {
                            Text(if (visibility == "everyone") "Everyone" else "Contacts Only", fontSize = 11.sp)
                        },
                        leadingIcon = {
                            Icon(
                                imageVector = if (visibility == "everyone") Icons.Default.Public else Icons.Default.Lock,
                                contentDescription = null,
                                modifier = Modifier.size(14.dp)
                            )
                        }
                    )
                    IconButton(onClick = { showSettingsDialog = true }) {
                        Icon(Icons.Default.Settings, contentDescription = "Settings")
                    }
                }
            )
        },
        bottomBar = {
            Column {
                // Mini Transfer Progress Bar
                service?.activeProgress?.let { p ->
                    if (currentTab != NavTab.TRANSFERS && p.totalBytes > 0) {
                        val pct = ((p.bytesAcked.toFloat() / p.totalBytes.toFloat()) * 100f).coerceIn(0f, 100f)
                        Surface(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable { currentTab = NavTab.TRANSFERS },
                            color = MaterialTheme.colorScheme.primaryContainer,
                            tonalElevation = 6.dp
                        ) {
                            Column(modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    horizontalArrangement = Arrangement.SpaceBetween
                                ) {
                                    Text(
                                        text = "${if (p.direction == "send") "Sending" else "Receiving"} (${pct.toInt()}%)",
                                        style = MaterialTheme.typography.labelMedium,
                                        fontWeight = FontWeight.Bold
                                    )
                                    val mbSec = p.speedBps / 1_000_000.0
                                    Text(
                                        text = "${String.format("%.1f", mbSec)} MB/s",
                                        style = MaterialTheme.typography.labelSmall
                                    )
                                }
                                Spacer(modifier = Modifier.height(4.dp))
                                LinearProgressIndicator(
                                    progress = { pct / 100f },
                                    modifier = Modifier.fillMaxWidth().height(4.dp)
                                )
                            }
                        }
                    }
                }

                // Bottom Navigation Bar
                NavigationBar {
                    NavigationBarItem(
                        selected = currentTab == NavTab.HOME,
                        onClick = { currentTab = NavTab.HOME },
                        icon = { Icon(Icons.Default.Home, contentDescription = "Home") },
                        label = { Text("Home") }
                    )
                    NavigationBarItem(
                        selected = currentTab == NavTab.SEND,
                        onClick = { currentTab = NavTab.SEND },
                        icon = { Icon(Icons.AutoMirrored.Filled.Send, contentDescription = "Send") },
                        label = { Text("Send") }
                    )
                    NavigationBarItem(
                        selected = currentTab == NavTab.RECEIVE,
                        onClick = { currentTab = NavTab.RECEIVE },
                        icon = { Icon(Icons.Default.QrCode, contentDescription = "Receive") },
                        label = { Text("Receive") }
                    )
                    NavigationBarItem(
                        selected = currentTab == NavTab.TRANSFERS,
                        onClick = { currentTab = NavTab.TRANSFERS },
                        icon = { Icon(Icons.Default.SwapHoriz, contentDescription = "Transfers") },
                        label = { Text("Transfers") }
                    )
                    NavigationBarItem(
                        selected = currentTab == NavTab.DEVICES,
                        onClick = { currentTab = NavTab.DEVICES },
                        icon = { Icon(Icons.Default.Devices, contentDescription = "Devices") },
                        label = { Text("Devices") }
                    )
                }
            }
        }
    ) { paddingValues ->
        Box(modifier = Modifier.fillMaxSize().padding(paddingValues)) {
            when (currentTab) {
                NavTab.HOME -> HomeScreen(
                    identity = identity,
                    peers = peers,
                    recentTransfers = transfers.take(5),
                    onSendToPeer = { peer ->
                        selectedPeerForSend = peer
                        currentTab = NavTab.SEND
                    },
                    onGoToReceive = { currentTab = NavTab.RECEIVE },
                    onScanQR = { showScanner = true }
                )
                NavTab.SEND -> SendScreen(
                    peers = peers,
                    trustedDevices = trustedDevices,
                    initialPeer = selectedPeerForSend,
                    initialSources = pendingSendSources,
                    service = service,
                    onTransferStarted = {
                        pendingSendSources = emptyList()
                        currentTab = NavTab.TRANSFERS
                    }
                )
                NavTab.RECEIVE -> ReceiveScreen(
                    identity = identity,
                    deviceName = deviceName,
                    savePathDisplay = savePathDisplay,
                    service = service,
                    onChangeSaveLocation = { folderPickerLauncher.launch(null) },
                    onScanQR = { showScanner = true }
                )
                NavTab.TRANSFERS -> TransfersScreen(
                    transfers = transfers,
                    activeProgress = service?.activeProgress
                )
                NavTab.DEVICES -> DevicesScreen(
                    trustedDevices = trustedDevices,
                    onRemoveDevice = { fp ->
                        db.removeTrustedDevice(fp)
                        trustedDevices = db.listTrustedDevices()
                    },
                    onScanQR = { showScanner = true }
                )
            }

            // Incoming Transfer Dialog
            service?.pendingOffer?.let { offer ->
                AlertDialog(
                    onDismissRequest = { /* Require explicit action */ },
                    title = { Text("Incoming Transfer") },
                    text = {
                        val mb = offer.totalBytes / 1_000_000.0
                        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            Text("${offer.peerName} wants to send files to your device.")
                            Text("Files: ${offer.fileCount} (${String.format("%.1f", mb)} MB)", fontWeight = FontWeight.Bold)
                            Text("Sender Fingerprint: ${offer.peerFp.take(8)}", fontFamily = FontFamily.Monospace, fontSize = 12.sp)
                        }
                    },
                    confirmButton = {
                        Button(
                            onClick = {
                                service.respondToIncomingOffer(true)
                                currentTab = NavTab.TRANSFERS
                            }
                        ) {
                            Text("Accept")
                        }
                    },
                    dismissButton = {
                        OutlinedButton(
                            onClick = { service.respondToIncomingOffer(false) }
                        ) {
                            Text("Decline")
                        }
                    }
                )
            }

            // Settings Dialog
            if (showSettingsDialog) {
                var tempName by remember { mutableStateOf(deviceName) }
                AlertDialog(
                    onDismissRequest = { showSettingsDialog = false },
                    title = { Text("Preferences") },
                    text = {
                        Column(verticalArrangement = Arrangement.spacedBy(14.dp)) {
                            OutlinedTextField(
                                value = tempName,
                                onValueChange = { tempName = it },
                                label = { Text("Device Name") },
                                singleLine = true,
                                modifier = Modifier.fillMaxWidth()
                            )

                            // Save Location Setting
                            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                Text("Download Location", fontWeight = FontWeight.SemiBold, fontSize = 13.sp)
                                Surface(
                                    onClick = {
                                        showSettingsDialog = false
                                        folderPickerLauncher.launch(null)
                                    },
                                    shape = RoundedCornerShape(8.dp),
                                    color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.6f),
                                    modifier = Modifier.fillMaxWidth()
                                ) {
                                    Row(
                                        modifier = Modifier.padding(10.dp),
                                        verticalAlignment = Alignment.CenterVertically
                                    ) {
                                        Icon(
                                            Icons.Default.Folder,
                                            contentDescription = null,
                                            tint = MaterialTheme.colorScheme.primary,
                                            modifier = Modifier.size(20.dp)
                                        )
                                        Spacer(modifier = Modifier.width(8.dp))
                                        Text(
                                            text = savePathDisplay,
                                            modifier = Modifier.weight(1f),
                                            fontSize = 12.sp,
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis
                                        )
                                        Text(
                                            text = "Change",
                                            fontSize = 12.sp,
                                            fontWeight = FontWeight.Bold,
                                            color = MaterialTheme.colorScheme.primary
                                        )
                                    }
                                }
                            }

                            Text("Device ID: ${identity.deviceId}", fontSize = 11.sp, fontFamily = FontFamily.Monospace)
                            Text("Fingerprint: ${identity.shortFingerprint}", fontSize = 11.sp, fontFamily = FontFamily.Monospace)
                        }
                    },
                    confirmButton = {
                        Button(
                            onClick = {
                                deviceName = tempName
                                db.setSetting("device_name", tempName)
                                showSettingsDialog = false
                            }
                        ) {
                            Text("Save")
                        }
                    },
                    dismissButton = {
                        TextButton(onClick = { showSettingsDialog = false }) {
                            Text("Cancel")
                        }
                    }
                )
            }
        }
    }
}

@Composable
fun HomeScreen(
    identity: com.example.lanvault.crypto.DeviceIdentity,
    peers: List<DiscoveredPeer>,
    recentTransfers: List<TransferRecord>,
    onSendToPeer: (DiscoveredPeer) -> Unit,
    onGoToReceive: () -> Unit,
    onScanQR: () -> Unit
) {
    LazyColumn(
        modifier = Modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp)
    ) {
        // This Device Card
        item {
            Card(
                modifier = Modifier.fillMaxWidth(),
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant)
            ) {
                Column(modifier = Modifier.padding(16.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Box(
                            modifier = Modifier
                                .size(40.dp)
                                .clip(CircleShape)
                                .background(MaterialTheme.colorScheme.primary),
                            contentAlignment = Alignment.Center
                        ) {
                            Icon(Icons.Default.PhoneAndroid, contentDescription = null, tint = Color.White)
                        }
                        Spacer(modifier = Modifier.width(12.dp))
                        Column {
                            Text("This Android Device", fontWeight = FontWeight.Bold, fontSize = 16.sp)
                            Text("TLS 1.3 Fingerprint: ${identity.shortFingerprint}", fontFamily = FontFamily.Monospace, fontSize = 12.sp)
                        }
                    }
                }
            }
        }

        // Quick Actions Row (Scan QR & Show QR)
        item {
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Button(
                    onClick = onScanQR,
                    modifier = Modifier.weight(1f)
                ) {
                    Icon(Icons.Default.QrCodeScanner, contentDescription = null, modifier = Modifier.size(18.dp))
                    Spacer(modifier = Modifier.width(8.dp))
                    Text("Scan QR")
                }
                OutlinedButton(
                    onClick = onGoToReceive,
                    modifier = Modifier.weight(1f)
                ) {
                    Icon(Icons.Default.QrCode, contentDescription = null, modifier = Modifier.size(18.dp))
                    Spacer(modifier = Modifier.width(8.dp))
                    Text("My QR")
                }
            }
        }

        // Nearby Peers Section
        item {
            Text("Nearby Devices (${peers.size})", fontWeight = FontWeight.Bold, fontSize = 16.sp)
        }

        if (peers.isEmpty()) {
            item {
                Card(
                    modifier = Modifier.fillMaxWidth(),
                    colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface)
                ) {
                    Box(modifier = Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
                        Text("Searching for LANVault devices on local Wi-Fi...", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        } else {
            items(peers) { peer ->
                Card(
                    modifier = Modifier.fillMaxWidth(),
                    colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface)
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.SpaceBetween
                    ) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Icon(
                                imageVector = if (peer.platform == "windows") Icons.Default.Laptop else Icons.Default.PhoneAndroid,
                                contentDescription = null,
                                modifier = Modifier.size(32.dp),
                                tint = MaterialTheme.colorScheme.primary
                            )
                            Spacer(modifier = Modifier.width(12.dp))
                            Column {
                                Text(peer.name, fontWeight = FontWeight.SemiBold)
                                Text("${peer.platform} · ${peer.ips.firstOrNull() ?: ""}", fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                        Button(onClick = { onSendToPeer(peer) }) {
                            Text("Send")
                        }
                    }
                }
            }
        }

        // Recent Transfers Section
        if (recentTransfers.isNotEmpty()) {
            item {
                Text("Recent Transfers", fontWeight = FontWeight.Bold, fontSize = 16.sp)
            }
            items(recentTransfers) { t ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.SpaceBetween
                    ) {
                        Column {
                            Text(t.peerName, fontWeight = FontWeight.Medium)
                            val mb = t.totalBytes / 1_000_000.0
                            Text("${t.direction.uppercase()} · ${t.totalFiles} files (${String.format("%.1f", mb)} MB)", fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        Text(
                            text = t.state.uppercase(),
                            fontSize = 11.sp,
                            fontWeight = FontWeight.Bold,
                            color = if (t.state == "completed") Color(0xFF047857) else MaterialTheme.colorScheme.primary
                        )
                    }
                }
            }
        }
    }
}

@Composable
fun SendScreen(
    peers: List<DiscoveredPeer>,
    trustedDevices: List<TrustedDevice>,
    initialPeer: DiscoveredPeer?,
    initialSources: List<com.example.lanvault.transfer.SendSource> = emptyList(),
    service: TransferForegroundService?,
    onTransferStarted: () -> Unit
) {
    val context = LocalContext.current
    var selectedPeer by remember(initialPeer, peers) {
        mutableStateOf(initialPeer ?: peers.firstOrNull())
    }
    var selectedSources by remember(initialSources) { mutableStateOf(initialSources) }
    var manualIp by remember { mutableStateOf("") }
    var isSending by remember { mutableStateOf(false) }
    var showManualInput by remember { mutableStateOf(false) }

    val filePickerLauncher = rememberLauncherForActivityResult(
        contract = ActivityResultContracts.OpenMultipleDocuments()
    ) { uris: List<Uri> ->
        val newSources = uris.map { uri -> getSendSourceFromUri(context, uri) }
        selectedSources = selectedSources + newSources
    }

    LazyColumn(
        modifier = Modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp)
    ) {
        item {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text("1. Select Recipient", fontWeight = FontWeight.Bold, fontSize = 16.sp)
                TextButton(onClick = { showManualInput = !showManualInput }) {
                    Text(if (showManualInput) "Choose from List" else "Enter IP Manually", fontSize = 12.sp)
                }
            }
        }

        if (showManualInput) {
            item {
                OutlinedTextField(
                    value = manualIp,
                    onValueChange = {
                        manualIp = it
                        selectedPeer = null
                    },
                    label = { Text("Recipient IP (e.g. 10.207.172.77)") },
                    placeholder = { Text("10.207.172.77") },
                    modifier = Modifier.fillMaxWidth(),
                    singleLine = true
                )
            }
        } else {
            if (peers.isNotEmpty()) {
                items(peers) { peer ->
                    val isSelected = selectedPeer?.deviceId == peer.deviceId
                    Card(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable {
                                selectedPeer = peer
                                manualIp = ""
                            },
                        colors = CardDefaults.cardColors(
                            containerColor = if (isSelected)
                                MaterialTheme.colorScheme.primaryContainer
                            else
                                MaterialTheme.colorScheme.surface
                        )
                    ) {
                        Row(modifier = Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically) {
                            RadioButton(
                                selected = isSelected,
                                onClick = {
                                    selectedPeer = peer
                                    manualIp = ""
                                }
                            )
                            Spacer(modifier = Modifier.width(8.dp))
                            Column {
                                Text(peer.name, fontWeight = FontWeight.SemiBold)
                                Text("${peer.platform.replaceFirstChar { it.uppercase() }} · ${peer.ips.firstOrNull() ?: ""}", fontSize = 12.sp)
                            }
                        }
                    }
                }
            } else if (trustedDevices.isNotEmpty()) {
                item {
                    Text("Paired Devices:", fontSize = 13.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                items(trustedDevices) { dev ->
                    val isSelected = selectedPeer?.deviceId == dev.deviceId || (selectedPeer == null && manualIp == dev.lastKnownIp && dev.lastKnownIp.isNotBlank())
                    Card(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable {
                                val ips = if (dev.lastKnownIp.isNotBlank()) listOf(dev.lastKnownIp) else emptyList()
                                selectedPeer = DiscoveredPeer(
                                    deviceId = dev.deviceId,
                                    name = dev.name,
                                    platform = dev.platform,
                                    ips = ips,
                                    port = Protocol.DEFAULT_PORT,
                                    lastSeen = System.currentTimeMillis()
                                )
                                if (dev.lastKnownIp.isNotBlank()) {
                                    manualIp = dev.lastKnownIp
                                }
                            },
                        colors = CardDefaults.cardColors(
                            containerColor = if (isSelected)
                                MaterialTheme.colorScheme.primaryContainer
                            else
                                MaterialTheme.colorScheme.surface
                        )
                    ) {
                        Row(modifier = Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically) {
                            RadioButton(
                                selected = isSelected,
                                onClick = {
                                    val ips = if (dev.lastKnownIp.isNotBlank()) listOf(dev.lastKnownIp) else emptyList()
                                    selectedPeer = DiscoveredPeer(
                                        deviceId = dev.deviceId,
                                        name = dev.name,
                                        platform = dev.platform,
                                        ips = ips,
                                        port = Protocol.DEFAULT_PORT,
                                        lastSeen = System.currentTimeMillis()
                                    )
                                    if (dev.lastKnownIp.isNotBlank()) {
                                        manualIp = dev.lastKnownIp
                                    }
                                }
                            )
                            Spacer(modifier = Modifier.width(8.dp))
                            Column {
                                Text(dev.name, fontWeight = FontWeight.SemiBold)
                                val ipDisplay = if (dev.lastKnownIp.isNotBlank()) "IP: ${dev.lastKnownIp}" else "Paired device"
                                Text("${dev.platform.replaceFirstChar { it.uppercase() }} · $ipDisplay", fontSize = 12.sp)
                            }
                        }
                    }
                }
            } else {
                item {
                    Card(modifier = Modifier.fillMaxWidth()) {
                        Column(modifier = Modifier.padding(16.dp)) {
                            Text("No devices discovered on local network yet.", fontSize = 13.sp)
                            Spacer(modifier = Modifier.height(4.dp))
                            Text("Scan the Windows QR code to pair, or tap 'Enter IP Manually' above.", fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
            }
        }

        item {
            Text("2. Select Files", fontWeight = FontWeight.Bold, fontSize = 16.sp)
            Spacer(modifier = Modifier.height(8.dp))
            Button(
                onClick = { filePickerLauncher.launch(arrayOf("*/*")) },
                modifier = Modifier.fillMaxWidth()
            ) {
                Icon(Icons.Default.Add, contentDescription = null)
                Spacer(modifier = Modifier.width(8.dp))
                Text("Browse Files")
            }
        }

        if (selectedSources.isNotEmpty()) {
            item {
                val totalBytes = selectedSources.sumOf { it.size }
                val mb = totalBytes / 1_000_000.0
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text("${selectedSources.size} files selected (${String.format("%.1f", mb)} MB)", fontWeight = FontWeight.Medium)
                    TextButton(onClick = { selectedSources = emptyList() }) {
                        Text("Clear All", fontSize = 12.sp, color = MaterialTheme.colorScheme.error)
                    }
                }
            }
            items(selectedSources) { source ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Row(
                        modifier = Modifier.padding(10.dp),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Text(source.name, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                        val kb = source.size / 1000.0
                        Text("${String.format("%.1f", kb)} KB", fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        IconButton(onClick = { selectedSources = selectedSources.filter { it != source } }) {
                            Icon(Icons.Default.Close, contentDescription = "Remove", modifier = Modifier.size(16.dp))
                        }
                    }
                }
            }
        }

        item {
            val targetIp = selectedPeer?.ips?.firstOrNull { it.isNotBlank() } ?: manualIp.trim()
            val canSend = targetIp.isNotBlank() && selectedSources.isNotEmpty() && !isSending

            Button(
                onClick = {
                    val targetFp = selectedPeer?.let { p ->
                        trustedDevices.find { it.deviceId == p.deviceId }?.fingerprint
                    }
                    isSending = true
                    service?.sendSources(targetIp, Protocol.DEFAULT_PORT, selectedSources, targetFp) {
                        isSending = false
                        onTransferStarted()
                    }
                },
                enabled = canSend,
                modifier = Modifier.fillMaxWidth().height(50.dp)
            ) {
                Text(
                    when {
                        isSending -> "Sending..."
                        selectedSources.isEmpty() -> "Select Files to Send"
                        targetIp.isBlank() -> "Select Recipient or Enter IP Above"
                        else -> "Send Files to ${selectedPeer?.name ?: targetIp}"
                    }
                )
            }
        }
    }
}

@Composable
fun ReceiveScreen(
    identity: com.example.lanvault.crypto.DeviceIdentity,
    deviceName: String,
    savePathDisplay: String,
    service: TransferForegroundService?,
    onChangeSaveLocation: () -> Unit,
    onScanQR: () -> Unit
) {
    val localIps = remember { getLocalIpAddresses() }
    val qrContent = remember(identity, deviceName, localIps) {
        service?.transferEngine?.generatePairingQR(localIps, deviceName)
            ?: run {
                val ipStr = localIps.joinToString(",")
                val encodedName = java.net.URLEncoder.encode(deviceName, "UTF-8")
                "lanvault://pair?v=1&fp=${identity.fingerprint}&ip=$ipStr&port=${Protocol.DEFAULT_PORT}&name=$encodedName&tok="
            }
    }
    val qrBitmap = remember(qrContent) { QRHelper.generateQRCode(qrContent, 512) }

    Column(
        modifier = Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center
    ) {
        Card(
            modifier = Modifier.padding(16.dp),
            colors = CardDefaults.cardColors(containerColor = Color.White)
        ) {
            Image(
                bitmap = qrBitmap.asImageBitmap(),
                contentDescription = "Pairing QR Code",
                modifier = Modifier.size(220.dp).padding(12.dp)
            )
        }
        Spacer(modifier = Modifier.height(12.dp))
        Text(deviceName, fontWeight = FontWeight.Bold, fontSize = 18.sp)
        Text("Scan with Windows LANVault to pair instantly", color = MaterialTheme.colorScheme.onSurfaceVariant, fontSize = 13.sp)
        Spacer(modifier = Modifier.height(8.dp))
        Text("Fingerprint: ${identity.shortFingerprint}", fontFamily = FontFamily.Monospace, fontSize = 12.sp)

        Spacer(modifier = Modifier.height(20.dp))
        Button(
            onClick = onScanQR,
            modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp)
        ) {
            Icon(Icons.Default.QrCodeScanner, contentDescription = null)
            Spacer(modifier = Modifier.width(8.dp))
            Text("Or Scan PC's QR Code")
        }

        Spacer(modifier = Modifier.height(16.dp))
        Surface(
            onClick = onChangeSaveLocation,
            shape = RoundedCornerShape(12.dp),
            color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.7f),
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp)
        ) {
            Row(
                modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                Icon(
                    imageVector = Icons.Default.Folder,
                    contentDescription = "Save Folder",
                    tint = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.size(22.dp)
                )
                Spacer(modifier = Modifier.width(10.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "Files saved to:",
                        fontSize = 11.sp,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                    Text(
                        text = savePathDisplay,
                        fontSize = 13.sp,
                        fontWeight = FontWeight.SemiBold,
                        color = MaterialTheme.colorScheme.primary,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis
                    )
                }
                Spacer(modifier = Modifier.width(8.dp))
                FilledTonalButton(
                    onClick = onChangeSaveLocation,
                    contentPadding = PaddingValues(horizontal = 12.dp, vertical = 2.dp),
                    modifier = Modifier.height(32.dp)
                ) {
                    Text("Change", fontSize = 12.sp)
                }
            }
        }
    }
}

@Composable
fun TransfersScreen(
    transfers: List<TransferRecord>,
    activeProgress: com.example.lanvault.transfer.TransferProgress?
) {
    LazyColumn(
        modifier = Modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)
    ) {
        activeProgress?.let { p ->
            item {
                Card(
                    modifier = Modifier.fillMaxWidth(),
                    colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer)
                ) {
                    Column(modifier = Modifier.padding(16.dp)) {
                        val pct = if (p.totalBytes > 0) ((p.bytesAcked.toFloat() / p.totalBytes.toFloat()) * 100f).coerceIn(0f, 100f) else 0f
                        Text("Active Transfer (${p.direction.uppercase()})", fontWeight = FontWeight.Bold)
                        Spacer(modifier = Modifier.height(8.dp))
                        LinearProgressIndicator(
                            progress = { pct / 100f },
                            modifier = Modifier.fillMaxWidth().height(8.dp)
                        )
                        Spacer(modifier = Modifier.height(8.dp))
                        val mbDone = p.bytesAcked / 1_000_000.0
                        val mbTotal = p.totalBytes / 1_000_000.0
                        val mbSec = p.speedBps / 1_000_000.0
                        Text("${p.filesDone}/${p.totalFiles} files · ${String.format("%.1f", mbDone)}/${String.format("%.1f", mbTotal)} MB · ${String.format("%.1f", mbSec)} MB/s", fontSize = 12.sp)
                    }
                }
            }
        }

        item {
            Text("Transfer History (${transfers.size})", fontWeight = FontWeight.Bold, fontSize = 16.sp)
        }

        if (transfers.isEmpty()) {
            item {
                Box(modifier = Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) {
                    Text("No transfers recorded yet.", color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        } else {
            items(transfers) { t ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(14.dp),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Column {
                            Text(t.peerName, fontWeight = FontWeight.SemiBold)
                            val mb = t.totalBytes / 1_000_000.0
                            Text("${t.direction.uppercase()} · ${t.totalFiles} files (${String.format("%.1f", mb)} MB)", fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        Text(
                            text = t.state.uppercase(),
                            fontWeight = FontWeight.Bold,
                            fontSize = 11.sp,
                            color = if (t.state == "completed") Color(0xFF047857) else MaterialTheme.colorScheme.primary
                        )
                    }
                }
            }
        }
    }
}

@Composable
fun DevicesScreen(
    trustedDevices: List<TrustedDevice>,
    onRemoveDevice: (String) -> Unit,
    onScanQR: () -> Unit
) {
    LazyColumn(
        modifier = Modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)
    ) {
        item {
            Button(
                onClick = onScanQR,
                modifier = Modifier.fillMaxWidth()
            ) {
                Icon(Icons.Default.QrCodeScanner, contentDescription = null)
                Spacer(modifier = Modifier.width(8.dp))
                Text("Scan Windows QR to Pair")
            }
        }

        item {
            Text("Paired Devices (${trustedDevices.size})", fontWeight = FontWeight.Bold, fontSize = 16.sp)
        }

        if (trustedDevices.isEmpty()) {
            item {
                Card(modifier = Modifier.fillMaxWidth()) {
                    Box(modifier = Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) {
                        Text("No trusted devices paired yet.\nTap 'Scan Windows QR to Pair' above.", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        } else {
            items(trustedDevices) { dev ->
                Card(modifier = Modifier.fillMaxWidth()) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(14.dp),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Icon(Icons.Default.Laptop, contentDescription = null, modifier = Modifier.size(28.dp))
                            Spacer(modifier = Modifier.width(12.dp))
                            Column {
                                Text(dev.name, fontWeight = FontWeight.SemiBold)
                                Text("Fingerprint: ${dev.fingerprint.take(8)}", fontFamily = FontFamily.Monospace, fontSize = 12.sp)
                            }
                        }
                        IconButton(onClick = { onRemoveDevice(dev.fingerprint) }) {
                            Icon(Icons.Default.Delete, contentDescription = "Revoke", tint = MaterialTheme.colorScheme.error)
                        }
                    }
                }
            }
        }
    }
}

private fun getFileName(context: Context, uri: Uri): String? {
    var name: String? = null
    if (uri.scheme == "content") {
        val cursor = context.contentResolver.query(uri, null, null, null, null)
        cursor?.use {
            if (it.moveToFirst()) {
                val index = it.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                if (index != -1) name = it.getString(index)
            }
        }
    }
    return name ?: uri.path?.substringAfterLast('/')
}

private fun getFolderDisplayName(context: Context, uri: Uri, docFile: DocumentFile?): String {
    val docName = docFile?.name
    if (!docName.isNullOrBlank()) return docName

    val path = uri.path ?: ""
    if (path.contains(":")) {
        val segment = path.substringAfterLast(":")
        if (segment.isNotBlank()) {
            return segment.replace("/", " > ")
        }
    }
    return uri.lastPathSegment ?: "Custom Folder"
}

private fun getSendSourceFromUri(context: Context, uri: Uri): com.example.lanvault.transfer.SendSource {
    var name = "file_${System.currentTimeMillis()}"
    var size = 0L
    try {
        context.contentResolver.query(uri, null, null, null, null)?.use { cursor ->
            val nameIdx = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
            val sizeIdx = cursor.getColumnIndex(OpenableColumns.SIZE)
            if (cursor.moveToFirst()) {
                if (nameIdx != -1) cursor.getString(nameIdx)?.let { if (it.isNotBlank()) name = it }
                if (sizeIdx != -1) size = cursor.getLong(sizeIdx)
            }
        }
    } catch (_: Exception) {}
    if (size <= 0L) {
        try {
            context.contentResolver.openFileDescriptor(uri, "r")?.use { pfd ->
                size = pfd.statSize
            }
        } catch (_: Exception) {}
    }
    return com.example.lanvault.transfer.SendSource(uri = uri, file = null, name = name, size = size)
}

private fun getLocalIpAddresses(): List<String> {
    val ips = mutableListOf<String>()
    try {
        val interfaces = java.net.NetworkInterface.getNetworkInterfaces()
        while (interfaces.hasMoreElements()) {
            val iface = interfaces.nextElement()
            if (iface.isLoopback || !iface.isUp) continue
            for (addr in iface.inetAddresses) {
                if (addr is java.net.Inet4Address && !addr.isLoopbackAddress) {
                    val host = addr.hostAddress
                    if (host != null && host.isNotEmpty()) {
                        ips.add(host)
                    }
                }
            }
        }
    } catch (_: Exception) {}
    return ips
}
