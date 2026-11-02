---
inclusion: fileMatch
fileMatchPattern: "**/*.kt"
---

# Android Development Standards for LANVault

## Architecture Pattern

Follow **MVVM + Repository** pattern:

```
UI (Compose) → ViewModel → Repository → DataSource
```

- **UI**: Composable functions, zero business logic
- **ViewModel**: State management, UI events
- **Repository**: Single source of truth, abstracts data sources
- **DataSource**: Room database, network, preferences

## Kotlin Coroutines

Use coroutines for all async operations:

```kotlin
// ViewModel
viewModelScope.launch {
    repository.transferFile(file).collect { progress ->
        _state.value = state.value.copy(progress = progress)
    }
}

// Repository
fun transferFile(file: File): Flow<Progress> = flow {
    // Use flowOn(Dispatchers.IO) for IO operations
}.flowOn(Dispatchers.IO)
```

**Dispatchers**:
- `Dispatchers.Main`: UI updates
- `Dispatchers.IO`: Network, disk I/O
- `Dispatchers.Default`: CPU-intensive work

## Jetpack Compose Best Practices

### State Management

```kotlin
// ✅ Do: Hoist state to ViewModel
@Composable
fun TransferScreen(viewModel: TransferViewModel = viewModel()) {
    val state by viewModel.state.collectAsState()
    TransferContent(
        state = state,
        onPause = viewModel::pauseTransfer
    )
}

// ❌ Don't: Manage state in composable
@Composable
fun BadScreen() {
    var counter by remember { mutableStateOf(0) } // Lost on recomposition
}
```

### Side Effects

- `LaunchedEffect`: For suspending operations tied to composable lifecycle
- `DisposableEffect`: For cleanup (unregister listeners)
- `SideEffect`: For non-compose state synchronization

```kotlin
LaunchedEffect(transferId) {
    viewModel.observeTransfer(transferId).collect { update ->
        // Handle updates
    }
}

DisposableEffect(Unit) {
    val listener = networkCallback()
    connectivityManager.registerCallback(listener)
    onDispose {
        connectivityManager.unregisterCallback(listener)
    }
}
```

## Material 3 Guidelines

- Use `MaterialTheme.colorScheme` for colors
- Use `MaterialTheme.typography` for text styles
- Use `MaterialTheme.shapes` for component shapes
- Support dynamic color (Material You)

```kotlin
Surface(
    color = MaterialTheme.colorScheme.surface,
    tonalElevation = 2.dp
) {
    Text(
        text = title,
        style = MaterialTheme.typography.titleLarge,
        color = MaterialTheme.colorScheme.onSurface
    )
}
```

## Scoped Storage (API 29+)

### Reading Files

Use Storage Access Framework (SAF):

```kotlin
val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
    addCategory(Intent.CATEGORY_OPENABLE)
    type = "*/*"
    putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
}
startActivityForResult(intent, REQUEST_CODE)

// In onActivityResult
uri?.let { uri ->
    contentResolver.openInputStream(uri)?.use { stream ->
        // Read file
    }
}
```

### Writing Files

Use MediaStore or SAF:

```kotlin
val resolver = context.contentResolver
val contentValues = ContentValues().apply {
    put(MediaStore.Downloads.DISPLAY_NAME, fileName)
    put(MediaStore.Downloads.MIME_TYPE, mimeType)
    put(MediaStore.Downloads.IS_PENDING, 1)
}

val uri = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, contentValues)
uri?.let {
    resolver.openOutputStream(it)?.use { output ->
        // Write file
    }
    // Mark complete
    contentValues.clear()
    contentValues.put(MediaStore.Downloads.IS_PENDING, 0)
    resolver.update(it, contentValues, null, null)
}
```

## Foreground Services (Android 15+)

For data transfer services:

```kotlin
class TransferForegroundService : Service() {
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val notification = createNotification()
        
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            // Android 14+: Use dataSync type
            startForeground(
                NOTIFICATION_ID, 
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
            )
        } else {
            startForeground(NOTIFICATION_ID, notification)
        }
        
        return START_STICKY
    }
    
    // Handle Android 15 timeout
    override fun onTimeout(startId: Int) {
        // Save state, stop gracefully
        stopSelf(startId)
    }
}
```

**AndroidManifest.xml**:
```xml
<service
    android:name=".service.TransferForegroundService"
    android:foregroundServiceType="dataSync"
    android:enabled="true"
    android:exported="false" />
```

## Security Best Practices

### TLS Configuration

```kotlin
val sslContext = SSLContext.getInstance("TLSv1.3")
sslContext.init(keyManagers, trustManagers, SecureRandom())

val socket = sslContext.socketFactory.createSocket() as SSLSocket
socket.enabledProtocols = arrayOf("TLSv1.3")
socket.needClientAuth = true // Mutual TLS
```

### Android Keystore

```kotlin
val keyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

val keyPairGenerator = KeyPairGenerator.getInstance(
    KeyProperties.KEY_ALGORITHM_EC,
    "AndroidKeyStore"
)

val parameterSpec = KeyGenParameterSpec.Builder(
    alias,
    KeyProperties.PURPOSE_SIGN or KeyProperties.PURPOSE_VERIFY
).apply {
    setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
    setDigests(KeyProperties.DIGEST_SHA256)
    setUserAuthenticationRequired(false)
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
        setIsStrongBoxBacked(true) // Hardware-backed if available
    }
}.build()

keyPairGenerator.initialize(parameterSpec)
val keyPair = keyPairGenerator.generateKeyPair()
```

## Network Operations

### Multicast Lock

Required for mDNS:

```kotlin
val wifiManager = context.getSystemService(Context.WIFI_SERVICE) as WifiManager
val multicastLock = wifiManager.createMulticastLock("lanvault_discovery")
multicastLock.setReferenceCounted(true)
multicastLock.acquire()

// Later
multicastLock.release()
```

### Network Callbacks

```kotlin
val connectivityManager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager

val networkCallback = object : ConnectivityManager.NetworkCallback() {
    override fun onAvailable(network: Network) {
        // Network connected
    }
    
    override fun onLost(network: Network) {
        // Network disconnected
    }
    
    override fun onCapabilitiesChanged(
        network: Network,
        networkCapabilities: NetworkCapabilities
    ) {
        val isWifi = networkCapabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)
        // Update UI
    }
}

val request = NetworkRequest.Builder()
    .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
    .build()

connectivityManager.registerNetworkCallback(request, networkCallback)
```

## Testing

### Unit Tests

```kotlin
@Test
fun `fingerprint calculation matches test vector`() = runTest {
    val cert = generateTestCertificate()
    val fingerprint = Identity.calculateFingerprint(cert)
    
    assertEquals(
        "expected_64_hex_chars_from_test_vector",
        fingerprint
    )
}
```

### Instrumented Tests

```kotlin
@RunWith(AndroidJUnit4::class)
class MainScreenTest {
    @get:Rule
    val composeTestRule = createAndroidComposeRule<MainActivity>()
    
    @Test
    fun navigationBar_displaysAllTabs() {
        composeTestRule.onNodeWithText("Home").assertIsDisplayed()
        composeTestRule.onNodeWithText("Send").assertIsDisplayed()
        composeTestRule.onNodeWithText("Receive").assertIsDisplayed()
        composeTestRule.onNodeWithText("Transfers").assertIsDisplayed()
        composeTestRule.onNodeWithText("Devices").assertIsDisplayed()
    }
}
```

## Performance

### LazyColumn Optimization

```kotlin
LazyColumn {
    items(
        items = transfers,
        key = { it.id } // Stable key for better performance
    ) { transfer ->
        TransferItem(transfer)
    }
}
```

### Remember Heavy Computations

```kotlin
val expensiveValue = remember(dependency) {
    computeExpensiveValue(dependency)
}
```

## Error Handling

```kotlin
sealed class Result<out T> {
    data class Success<T>(val data: T) : Result<T>()
    data class Error(val exception: Exception) : Result<Nothing>()
    object Loading : Result<Nothing>()
}

// In ViewModel
private val _state = MutableStateFlow<Result<Transfer>>(Result.Loading)
val state: StateFlow<Result<Transfer>> = _state.asStateFlow()

// In UI
when (val result = state) {
    is Result.Loading -> CircularProgressIndicator()
    is Result.Success -> TransferContent(result.data)
    is Result.Error -> ErrorMessage(result.exception.message)
}
```

## Resource Naming

Follow Android conventions:

- `activity_main.xml` (layouts, but we use Compose)
- `ic_launcher.xml` (drawables)
- `strings.xml` → `app_name`, `error_network_unavailable`
- `colors.xml` → `md_theme_light_primary`
- `dimens.xml` → `padding_small`, `text_size_large`

## Logging

Use Timber or similar:

```kotlin
// Debug builds only
if (BuildConfig.DEBUG) {
    Timber.d("Transfer progress: $progress%")
}

// Errors always
Timber.e(exception, "Failed to parse frame")
```

## Dependencies

Keep dependencies up to date:

- Compose BOM for version alignment
- Lifecycle 2.8+
- Navigation 2.8+
- Room 2.6+
- Coroutines 1.8+

#[[file:../../docs/PRD.md]]
