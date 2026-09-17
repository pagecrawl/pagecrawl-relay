package io.pagecrawl.relay

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import io.pagecrawl.relay.NetworkPolicy.Decision
import io.pagecrawl.relay.go.mobile.Relay
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            RelayTheme {
                RelayApp()
            }
        }
    }
}

/**
 * PageCrawl indigo as the accent only, on the platform's own surfaces. The dark value is the
 * lighter indigo the PageCrawl app uses in dark mode, which keeps contrast on a dark surface.
 */
@Composable
private fun RelayTheme(content: @Composable () -> Unit) {
    // Neutral surfaces set explicitly: Material's defaults are tinted purple, which reads as
    // somebody else's app rather than PageCrawl's.
    val colors = if (isSystemInDarkTheme()) {
        darkColorScheme(
            primary = Color(0xFF6B8CFF),
            onPrimary = Color.Black,
            background = Color.Black,
            surface = Color.Black,
            onBackground = Color(0xFFF3F4F6),
            onSurface = Color(0xFFF3F4F6),
            onSurfaceVariant = Color(0xFF9CA3AF),
            surfaceVariant = Color(0xFF1C1C1E),
            surfaceContainerLowest = Color.Black,
            surfaceContainerLow = Color(0xFF111113),
            surfaceContainer = Color(0xFF1C1C1E),
            surfaceContainerHigh = Color(0xFF1C1C1E),
            surfaceContainerHighest = Color(0xFF1C1C1E),
            secondaryContainer = Color(0xFF1E2A4A),
            onSecondaryContainer = Color(0xFFE0E7FF),
            outline = Color(0xFF3A3A3C),
            outlineVariant = Color(0xFF2C2C2E),
        )
    } else {
        lightColorScheme(
            primary = Color(0xFF2955C3),
            onPrimary = Color.White,
            background = Color.White,
            surface = Color.White,
            onBackground = Color(0xFF111827),
            onSurface = Color(0xFF111827),
            onSurfaceVariant = Color(0xFF6B7280),
            surfaceVariant = Color(0xFFF3F4F6),
            surfaceContainerLowest = Color.White,
            surfaceContainerLow = Color(0xFFF9FAFB),
            surfaceContainer = Color(0xFFF3F4F6),
            surfaceContainerHigh = Color(0xFFF3F4F6),
            surfaceContainerHighest = Color(0xFFF3F4F6),
            secondaryContainer = Color(0xFFE8EDFA),
            onSecondaryContainer = Color(0xFF1B2A55),
            outline = Color(0xFFD1D5DB),
            outlineVariant = Color(0xFFE5E7EB),
        )
    }
    MaterialTheme(colorScheme = colors, content = content)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun RelayApp() {
    val context = LocalContext.current
    val vault = remember { KeyVault(context) }
    var key by remember { mutableStateOf(vault.read()) }

    Scaffold(topBar = { TopAppBar(title = { Text("PageCrawl Relay") }) }) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            val current = key
            if (current == null) {
                AddKey(onKey = { token ->
                    vault.write(token)
                    key = token
                })
            } else {
                Controls(
                    token = current,
                    onRemove = {
                        RelaySettings(context).enabled = false
                        RelayService.stop(context)
                        vault.clear()
                        key = null
                    },
                )
            }
        }
    }
}

@Composable
private fun AddKey(onKey: (String) -> Unit) {
    var pasted by remember { mutableStateOf("") }
    var problem by remember { mutableStateOf<String?>(null) }

    val scanner = rememberLauncherForActivityResult(ScanContract()) { result ->
        val contents = result.contents ?: return@rememberLauncherForActivityResult
        val token = RelayKey.parse(contents)
        if (token != null) {
            onKey(token)
        } else {
            problem = "That QR code is not a relay key. Scan the code shown under Settings, Relays, Add machine."
        }
    }

    Text(
        "Let this phone carry your PageCrawl checks",
        style = MaterialTheme.typography.headlineSmall,
    )
    Text(
        "Checks for pages that only your connection can reach, or that turn away datacenter " +
            "addresses, will leave from this phone's network instead of PageCrawl's.",
        style = MaterialTheme.typography.bodyMedium,
    )
    Text(
        "In PageCrawl on your computer, open Settings, Relays, Add machine. It shows a QR code " +
            "and a key.",
        style = MaterialTheme.typography.bodyMedium,
    )

    Button(
        onClick = {
            problem = null
            scanner.launch(
                ScanOptions()
                    .setDesiredBarcodeFormats(ScanOptions.QR_CODE)
                    .setPrompt("Point the camera at the QR code in PageCrawl")
                    .setBeepEnabled(false)
                    .setOrientationLocked(false),
            )
        },
        modifier = Modifier.fillMaxWidth(),
    ) {
        Text("Scan QR code")
    }

    OutlinedTextField(
        value = pasted,
        onValueChange = {
            pasted = it
            problem = null
        },
        label = { Text("Or paste the key") },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )

    OutlinedButton(
        onClick = {
            val token = RelayKey.parse(pasted)
            if (token != null) onKey(token) else problem = "That is not a relay key. It is 64 letters and digits."
        },
        enabled = pasted.isNotBlank(),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Text("Use this key")
    }

    problem?.let {
        Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
    }
}

@Composable
private fun Controls(token: String, onRemove: () -> Unit) {
    val context = LocalContext.current
    val settings = remember { RelaySettings(context) }
    val service by RelayMonitor.state.collectAsStateWithLifecycle()
    var enabled by remember { mutableStateOf(settings.enabled) }
    var mobileData by remember { mutableStateOf(settings.allowMobileData) }
    var confirmRemove by remember { mutableStateOf(false) }

    val notifications = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {
        // The relay runs either way; without the permission Android only hides its notification.
        RelayService.start(context)
    }

    fun turnOn() {
        settings.enabled = true
        enabled = true
        val needsPermission = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        if (needsPermission) {
            notifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        } else {
            RelayService.start(context)
        }
    }

    // Switched on but not running (force-stopped, or killed and not restarted yet): opening
    // the app brings it back, rather than showing a switch that is on above a relay that is off.
    LaunchedEffect(Unit) {
        if (settings.enabled && !RelayMonitor.state.value.running) {
            RelayService.start(context)
        }
    }

    StatusCard(enabled = enabled, state = service)

    Card(modifier = Modifier.fillMaxWidth(), colors = panelColors()) {
        ListItem(
            headlineContent = { Text("Relay checks") },
            supportingContent = { Text("Carry PageCrawl checks through this phone.") },
            trailingContent = {
                Switch(checked = enabled, onCheckedChange = { on ->
                    if (on) {
                        turnOn()
                    } else {
                        settings.enabled = false
                        enabled = false
                        RelayService.stop(context)
                    }
                })
            },
        )
        HorizontalDivider()
        ListItem(
            headlineContent = { Text("Also use mobile data") },
            supportingContent = {
                Text(
                    "Off: the relay only runs on Wi-Fi. On: it also uses your data plan. You can set a " +
                        "monthly limit for this relay in PageCrawl under Settings, Relays.",
                )
            },
            trailingContent = {
                Switch(checked = mobileData, onCheckedChange = { on ->
                    settings.allowMobileData = on
                    mobileData = on
                    if (enabled) RelayService.start(context, RelayService.ACTION_SETTINGS_CHANGED)
                })
            },
        )
    }

    BatteryCard()

    SelfCheckCard(token = token)

    if (service.status.recent.isNotEmpty()) {
        RecentCard(service.status)
    }

    TextButton(onClick = { confirmRemove = true }, modifier = Modifier.fillMaxWidth()) {
        Text("Remove key from this phone", color = MaterialTheme.colorScheme.error)
    }

    if (confirmRemove) {
        AlertDialog(
            onDismissRequest = { confirmRemove = false },
            title = { Text("Remove the key?") },
            text = {
                Text(
                    "This phone stops relaying and forgets the key. The relay stays listed in " +
                        "PageCrawl until you remove it there.",
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    confirmRemove = false
                    onRemove()
                }) { Text("Remove", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = { TextButton(onClick = { confirmRemove = false }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun StatusCard(enabled: Boolean, state: ServiceState) {
    val headline: String
    val detail: String
    if (!enabled || !state.running) {
        headline = "Off"
        detail = "Checks use PageCrawl's own connection, or another relay you have set up."
    } else {
        headline = RelayService.headline(state.decision, state.status)
        detail = RelayService.detail(state.decision, state.status)
    }

    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.secondaryContainer),
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(headline, style = MaterialTheme.typography.titleLarge)
            Text(detail, style = MaterialTheme.typography.bodyMedium)
            if (enabled && state.status.connected && state.status.connectedFor.isNotEmpty()) {
                Text("Connected for ${state.status.connectedFor}", style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

/**
 * Android may pause a background app to save battery, which for a relay means checks that
 * fail while the phone looks fine. Offer the exemption, and say plainly what it costs.
 */
@Composable
private fun BatteryCard() {
    val context = LocalContext.current
    val power = remember { context.getSystemService(PowerManager::class.java) }
    var exempt by remember { mutableStateOf(power.isIgnoringBatteryOptimizations(context.packageName)) }

    val request = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        exempt = power.isIgnoringBatteryOptimizations(context.packageName)
    }

    Card(modifier = Modifier.fillMaxWidth(), colors = panelColors()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Battery", style = MaterialTheme.typography.titleMedium)
            Text(
                if (exempt) {
                    "Android will not pause the relay to save battery. Relaying keeps the phone " +
                        "awake, so it works best on a charger."
                } else {
                    "Android may pause the relay to save battery, and checks relayed through this " +
                        "phone would fail while it is paused. Relaying keeps the phone awake, so it " +
                        "works best on a charger."
                },
                style = MaterialTheme.typography.bodyMedium,
            )
            if (!exempt) {
                OutlinedButton(onClick = {
                    request.launch(
                        Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS)
                            .setData(Uri.parse("package:${context.packageName}")),
                    )
                }) { Text("Keep running in the background") }
            }
        }
    }
}

@Composable
private fun SelfCheckCard(token: String) {
    val scope = rememberCoroutineScope()
    var running by remember { mutableStateOf(false) }
    var results by remember { mutableStateOf<List<SelfCheck>>(emptyList()) }

    Card(modifier = Modifier.fillMaxWidth(), colors = panelColors()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Self-check", style = MaterialTheme.typography.titleMedium)
            Text(
                "Checks the key, the connection to PageCrawl and the local network protection, " +
                    "without interrupting a relay that is running.",
                style = MaterialTheme.typography.bodyMedium,
            )
            Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                OutlinedButton(
                    enabled = !running,
                    onClick = {
                        running = true
                        scope.launch {
                            // Makes network requests and can take a while, so never on the main thread.
                            results = withContext(Dispatchers.IO) {
                                SelfCheck.listFromJson(
                                    Relay(token, RelayService.platform(), BuildConfig.VERSION_NAME).checkJSON(),
                                )
                            }
                            running = false
                        }
                    },
                ) { Text(if (running) "Checking" else "Run self-check") }
                if (running) {
                    Spacer(Modifier.padding(start = 12.dp))
                    CircularProgressIndicator(modifier = Modifier.height(24.dp))
                }
            }
            results.forEach { check ->
                Column(Modifier.padding(top = 4.dp)) {
                    Text(
                        (if (check.ok) "OK  " else "Needs attention  ") + check.name,
                        style = MaterialTheme.typography.labelLarge,
                        color = if (check.ok) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.error,
                    )
                    Text(check.detail, style = MaterialTheme.typography.bodySmall)
                    if (!check.ok && check.fix.isNotEmpty()) {
                        Text(check.fix, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
    }
}

@Composable
private fun RecentCard(status: RelayStatus) {
    Card(modifier = Modifier.fillMaxWidth(), colors = panelColors()) {
        Column(Modifier.padding(vertical = 8.dp)) {
            Text(
                "Recent destinations",
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            status.recent.take(RECENT_SHOWN).forEach { destination ->
                ListItem(
                    headlineContent = { Text("${destination.host}:${destination.port}") },
                    supportingContent = {
                        Text(
                            if (destination.allowed) {
                                destination.at
                            } else {
                                "${destination.at}, refused: ${destination.reason}"
                            },
                        )
                    },
                )
            }
        }
    }
}

private const val RECENT_SHOWN = 10

/**
 * A grouped panel. The text colour is set outright: left to Material, a card on these neutral
 * surfaces takes the muted variant, which makes a heading look disabled.
 */
@Composable
private fun panelColors() = CardDefaults.cardColors(
    containerColor = MaterialTheme.colorScheme.surfaceContainer,
    contentColor = MaterialTheme.colorScheme.onSurface,
)
