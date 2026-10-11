package io.pagecrawl.relay

import android.content.Context
import android.content.Intent
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
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

/** Optional phone controls, kept separate from starting and stopping ordinary relaying. */
@Composable
internal fun AssistantModeCard() {
    val context = LocalContext.current
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val preferences = remember { context.getSharedPreferences("relay_assistant_ui", Context.MODE_PRIVATE) }
    val state by AssistantReconnect.state.collectAsStateWithLifecycle()
    var support by remember { mutableStateOf(AssistantReconnect.support(context)) }
    var pauseText by remember {
        mutableStateOf(preferences.getInt("pause_seconds", 10).coerceIn(1, 120).toString())
    }
    var confirmReconnect by remember { mutableStateOf(false) }
    var setupProblem by remember { mutableStateOf<String?>(null) }
    val pauseSeconds = pauseText.toIntOrNull()?.takeIf { it in 1..120 }

    val setup = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        AssistantReconnect.initialize(context)
        support = AssistantReconnect.support(context)
    }

    LaunchedEffect(lifecycle) {
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
                        value = pauseText,
                        onValueChange = { value ->
                            pauseText = value.filter(Char::isDigit).take(3)
                            pauseText.toIntOrNull()?.takeIf { it in 1..120 }?.let {
                                preferences.edit().putInt("pause_seconds", it).apply()
                            }
                        },
                        label = { Text("Airplane mode pause (seconds)") },
                        supportingText = { Text("Choose 1 to 120 seconds. Your carrier may keep the same IP.") },
                        isError = pauseSeconds == null,
                        singleLine = true,
                        enabled = !state.busy && !state.recoveryRequired,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Button(
                        enabled = support.ready && pauseSeconds != null && !state.busy && !state.recoveryRequired,
                        onClick = { confirmReconnect = true },
                        modifier = Modifier.fillMaxWidth(),
                    ) { Text("Reconnect mobile data") }
                    Text(
                        "Use mobile data with Wi-Fi off. If relaying is enabled, also turn on Also use mobile data. " +
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
                    "This temporarily disconnects this phone and can interrupt checks and calls. " +
                        "Reconnect does not wait for active checks to finish. Your carrier may keep the same IP. " +
                        "Checks follow your PageCrawl offline policy. An interrupted check using a waiting policy " +
                        "can remain postponed for up to 24 hours after reconnecting. Reconnect between checks. " +
                        "If Android interrupts recovery, turn airplane mode off in Settings.",
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    confirmReconnect = false
                    pauseSeconds?.let { AssistantReconnect.start(context, it) }
                }) { Text("Reconnect") }
            },
            dismissButton = {
                TextButton(onClick = { confirmReconnect = false }) { Text("Cancel") }
            },
        )
    }
}
