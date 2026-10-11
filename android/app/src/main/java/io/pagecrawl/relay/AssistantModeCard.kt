package io.pagecrawl.relay

import android.content.Context
import android.content.Intent
import android.app.Activity
import android.provider.Settings
import android.view.WindowManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import java.text.DateFormat
import java.util.Date

/** Optional phone controls, kept separate from starting and stopping ordinary relaying. */
@Composable
internal fun AssistantModeCard() {
    val context = LocalContext.current
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val preferences = remember { context.getSharedPreferences("relay_assistant_ui", Context.MODE_PRIVATE) }
    val state by AssistantReconnect.state.collectAsStateWithLifecycle()
    val schedule by ReconnectScheduler.state.collectAsStateWithLifecycle()
    val relay by RelayMonitor.state.collectAsStateWithLifecycle()
    var support by remember { mutableStateOf(AssistantReconnect.support(context)) }
    var pauseText by remember {
        mutableStateOf(preferences.getInt("pause_seconds", 10).coerceIn(1, 120).toString())
    }
    var confirmReconnect by remember { mutableStateOf(false) }
    var confirmSchedule by remember { mutableStateOf(false) }
    var intervalText by remember {
        mutableStateOf(preferences.getInt("schedule_interval_minutes", 30).coerceIn(5, 1440).toString())
    }
    var idleText by remember {
        mutableStateOf(preferences.getInt("idle_seconds", 30).coerceIn(5, 300).toString())
    }
    var setupProblem by remember { mutableStateOf<String?>(null) }
    val pauseSeconds = pauseText.toIntOrNull()?.takeIf { it in 1..120 }
    val idleSeconds = idleText.toIntOrNull()?.takeIf { it in 5..300 }
    val intervalMinutes = intervalText.toIntOrNull()?.takeIf { it in 5..1440 }

    fun configureSchedule(enabled: Boolean, minutes: Int) {
        setupProblem = if (ReconnectScheduler.configure(context, enabled, minutes)) null
        else "The reconnect schedule could not be saved. Try again."
    }

    val keepScreenOn = state.busy || (schedule.enabled && relay.running && support.ready)
    DisposableEffect(context, keepScreenOn) {
        val window = (context as? Activity)?.window
        val alreadyKeptOn = window?.attributes?.flags?.and(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON) != 0
        if (keepScreenOn) window?.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        onDispose {
            if (keepScreenOn && !alreadyKeptOn) window?.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        }
    }

    val setup = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        AssistantReconnect.initialize(context)
        support = AssistantReconnect.support(context)
    }

    LaunchedEffect(lifecycle) {
        ReconnectScheduler.initialize(context)
        lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            while (isActive) {
                support = AssistantReconnect.support(context)
                delay(1_000)
            }
        }
    }

    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surfaceContainer,
            contentColor = MaterialTheme.colorScheme.onSurface,
        ),
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Assistant mode", style = MaterialTheme.typography.titleMedium)
            Text(
                "Experimental control for reconnecting mobile data without root. Choosing PageCrawl Relay " +
                    "as this phone's digital assistant replaces your current assistant.",
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(
                "Reconnect manually or enable a fixed interval. Each reconnect waits for Relay to stay idle first.",
                style = MaterialTheme.typography.bodyMedium,
            )
            if (schedule.enabled && (!support.selected || !support.supported)) {
                Text("Scheduled reconnects are waiting for Assistant mode to be available and selected.")
                OutlinedButton(onClick = { configureSchedule(false, schedule.intervalMinutes) }) {
                    Text("Turn schedule off")
                }
            }

            if (!support.supported) {
                Text(support.reason, style = MaterialTheme.typography.bodyMedium)
            } else {
                Text(
                    when {
                        support.ready -> "Assistant mode is ready."
                        support.selected -> "Assistant mode is selected. Waiting for Android to connect it."
                        else -> "Select PageCrawl Relay in Android's assistant settings to enable this control."
                    },
                    style = MaterialTheme.typography.bodyMedium,
                )
                OutlinedButton(
                    enabled = !state.busy,
                    onClick = {
                        setupProblem = null
                        runCatching { setup.launch(AssistantReconnect.setupIntent(context)) }
                            .onFailure { setupProblem = "Android could not open assistant settings on this phone." }
                    },
                ) {
                    Text(if (support.selected) "Assistant settings" else "Set up Assistant mode")
                }

                if (support.selected) {
                    OutlinedTextField(
                        value = idleText,
                        onValueChange = { value ->
                            idleText = value.filter(Char::isDigit).take(3)
                            idleText.toIntOrNull()?.takeIf { it in 5..300 }?.let {
                                preferences.edit().putInt("idle_seconds", it).apply()
                            }
                        },
                        label = { Text("Wait for idle (seconds)") },
                        supportingText = { Text("Choose 5 to 300 seconds with no active Relay connections or traffic. New activity restarts the wait.") },
                        isError = idleSeconds == null,
                        singleLine = true,
                        enabled = !state.busy && !state.recoveryRequired,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = pauseText,
                        onValueChange = { value ->
                            pauseText = value.filter(Char::isDigit).take(3)
                            pauseText.toIntOrNull()?.takeIf { it in 1..120 }?.let {
                                preferences.edit().putInt("pause_seconds", it).apply()
                            }
                        },
                        label = { Text("Airplane mode pause (seconds)") },
                        supportingText = { Text("Time disconnected, from 1 to 120 seconds. Your carrier may keep the same IP.") },
                        isError = pauseSeconds == null,
                        singleLine = true,
                        enabled = !state.busy && !state.recoveryRequired,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        value = intervalText,
                        onValueChange = { value ->
                            intervalText = value.filter(Char::isDigit).take(4)
                            intervalText.toIntOrNull()?.takeIf { it in 5..1440 }?.let {
                                configureSchedule(schedule.enabled, it)
                            }
                        },
                        label = { Text("Reconnect interval (minutes)") },
                        supportingText = { Text("Choose 5 to 1440 minutes between attempts. The next interval starts when a reconnect finishes.") },
                        isError = intervalMinutes == null,
                        singleLine = true,
                        enabled = !state.busy && !state.recoveryRequired,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        Text("Scheduled reconnects", modifier = Modifier.weight(1f))
                        Switch(
                            checked = schedule.enabled,
                            enabled = schedule.enabled || (support.ready && intervalMinutes != null && pauseSeconds != null && idleSeconds != null && !state.busy && !state.recoveryRequired),
                            onCheckedChange = { enabled ->
                                if (enabled) confirmSchedule = true
                                else configureSchedule(false, intervalMinutes ?: schedule.intervalMinutes)
                            },
                        )
                    }
                    if (schedule.enabled) {
                        schedule.nextAttemptAt?.let {
                            Text("Next reconnect due: ${DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(it))}", style = MaterialTheme.typography.bodySmall)
                        }
                        schedule.waitingReason?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
                    }
                    Text(
                        "The schedule runs while Relay checks is on and this phone is awake and unlocked. " +
                            "Leave Relay open on a charger to keep its screen on. Missed intervals do not cause extra reconnects.",
                        style = MaterialTheme.typography.bodySmall,
                    )
                    Button(
                        enabled = support.ready && relay.running && relay.status.connected && pauseSeconds != null && idleSeconds != null && !state.busy && !state.recoveryRequired,
                        onClick = { confirmReconnect = true },
                        modifier = Modifier.fillMaxWidth(),
                    ) { Text("Reconnect mobile data") }
                    if (!relay.running || !relay.status.connected) {
                        Text("Turn Relay checks on and wait for Relaying checks before reconnecting.", style = MaterialTheme.typography.bodySmall)
                    }
                    Text(
                        "Keep Relay open and the phone unlocked through the wait. Its screen stays on during the cycle. " +
                            "Keep Relay checks on, enable Also use mobile data, and use mobile data with Wi-Fi off. " +
                            "The IP comparison uses ipify to check this phone's public IPv4 before and after reconnecting.",
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
            }

            setupProblem?.let {
                Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
            }
            if (state.message.isNotBlank()) {
                Text(
                    state.message,
                    color = if (state.phase == ReconnectPhase.ERROR || state.recoveryRequired) {
                        MaterialTheme.colorScheme.error
                    } else {
                        MaterialTheme.colorScheme.onSurface
                    },
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            if (state.secondsRemaining > 0) {
                val unit = if (state.secondsRemaining == 1) "second" else "seconds"
                Text("${state.secondsRemaining} $unit remaining", style = MaterialTheme.typography.bodySmall)
            }
            if (state.busy) {
                CircularProgressIndicator()
                TextButton(onClick = { AssistantReconnect.cancel() }) { Text("Cancel reconnect") }
            }
            state.beforeIp?.let { Text("Public IPv4 before: $it", style = MaterialTheme.typography.bodySmall) }
            state.afterIp?.let { Text("Public IPv4 after: $it", style = MaterialTheme.typography.bodySmall) }

            if (state.recoveryRequired) {
                if (support.ready) {
                    Button(onClick = { AssistantReconnect.recover(context) }) { Text("Turn airplane mode off") }
                }
                OutlinedButton(onClick = {
                    runCatching { context.startActivity(Intent(Settings.ACTION_AIRPLANE_MODE_SETTINGS)) }
                        .onFailure { setupProblem = "Open Android Settings and turn airplane mode off." }
                }) { Text("Open airplane mode settings") }
            }
        }
    }

    if (confirmReconnect) {
        AlertDialog(
            onDismissRequest = { confirmReconnect = false },
            title = { Text("Reconnect mobile data?") },
            text = {
                Text(
                    "After ${idleSeconds ?: 30} idle seconds, Relay temporarily disconnects this phone. " +
                        "New Relay activity restarts the wait; other apps and calls are not checked. " +
                        "PageCrawl must confirm a brief pause for this phone's checks before the connection changes. " +
                        "The pause ends when mobile internet returns and expires within five minutes. " +
                        "Your carrier may keep the same IP. Your normal offline policy applies after the pause. " +
                        "If recovery is interrupted, turn airplane mode off in Settings.",
                    modifier = Modifier.verticalScroll(rememberScrollState()),
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    confirmReconnect = false
                    if (pauseSeconds != null && idleSeconds != null) AssistantReconnect.start(context, pauseSeconds, idleSeconds)
                }) { Text("Reconnect") }
            },
            dismissButton = {
                TextButton(onClick = { confirmReconnect = false }) { Text("Cancel") }
            },
        )
    }

    if (confirmSchedule) {
        AlertDialog(
            onDismissRequest = { confirmSchedule = false },
            title = { Text("Schedule mobile reconnects?") },
            text = {
                Text(
                    "Relay schedules the next reconnect ${intervalMinutes ?: 30} minutes after each attempt finishes. " +
                        "Each reconnect waits for ${idleSeconds ?: 30} idle seconds first. " +
                        "PageCrawl must confirm a brief pause for this phone's checks before the connection changes. " +
                        "The pause ends when mobile internet returns and expires within five minutes. " +
                        "This temporarily disconnects the whole phone and can interrupt calls or other apps. " +
                        "Keep Relay checks on, allow mobile data, and keep the phone awake and unlocked. " +
                        "Your carrier may keep the same public IP. Turn Scheduled reconnects off to stop the schedule.",
                    modifier = Modifier.verticalScroll(rememberScrollState()),
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    confirmSchedule = false
                    intervalMinutes?.let { configureSchedule(true, it) }
                }) { Text("Enable schedule") }
            },
            dismissButton = { TextButton(onClick = { confirmSchedule = false }) { Text("Cancel") } },
        )
    }
}
