package io.pagecrawl.relay

import android.Manifest
import android.app.KeyguardManager
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.role.RoleManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.os.SystemClock
import android.provider.Settings
import android.service.voice.VoiceInteractionService
import androidx.annotation.MainThread
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import java.net.URL
import java.util.UUID
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout

data class AssistantSupport(
    val supported: Boolean,
    val selected: Boolean,
    val ready: Boolean,
    val reason: String,
)

/** Only the local UI can start a cycle. Assistant invocations without its one-use token do nothing. */
object AssistantReconnect {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val mutable = MutableStateFlow(ReconnectState())
    val state: StateFlow<ReconnectState> = mutable.asStateFlow()
    private var app: Context? = null
    private var service: RelayVoiceInteractionService? = null
    private var session: RelayVoiceInteractionSession? = null
    private var job: Job? = null
    private var pending: Command? = null
    private val dispatched = mutableMapOf<String, Command>()
    private val ledger = RadioRestoreLedger()
    private val recoveryObligation = RecoveryObligation()
    private var ipProbe: (suspend (Context, Network) -> String?)? = null
    private data class Command(
        val id: String, val enabled: Boolean, val finished: CompletableDeferred<Unit>,
        val afterEnabledTasks: Boolean, var launched: Boolean = false,
        var owner: RelayVoiceInteractionSession? = null,
    )
    internal const val COMMAND_ID = "io.pagecrawl.relay.assistant.COMMAND_ID"
    private const val RECOVERY = "restore_airplane_mode"

    fun support(context: Context): AssistantSupport {
        val selected = VoiceInteractionService.isActiveService(
            context, ComponentName(context, RelayVoiceInteractionService::class.java),
        )
        val available = context.packageManager.resolveActivity(voiceIntent(false), PackageManager.MATCH_DEFAULT_ONLY) != null
        val roleAvailable = Build.VERSION.SDK_INT < 29 ||
            context.getSystemService(RoleManager::class.java).isRoleAvailable(RoleManager.ROLE_ASSISTANT)
        return AssistantSupport(
            supported = available && roleAvailable,
            selected = selected,
            ready = available && roleAvailable && selected && service != null,
            reason = when {
                !available -> "This phone does not provide Android's airplane mode voice action."
                !roleAvailable -> "This phone does not allow selecting a digital assistant."
                !selected -> "Select PageCrawl Relay as the phone's default digital assistant. This replaces your current assistant."
                service == null -> "Waiting for Android to initialize the Relay assistant."
                else -> "Relay is the selected digital assistant. Reconnect is available on mobile data."
            },
        )
    }

    fun setupIntent(context: Context): Intent {
        // ASSISTANT is not a requestable role on standard Android builds. Settings owns selection.
        return Intent(Settings.ACTION_VOICE_INPUT_SETTINGS)
    }

    @MainThread
    fun initialize(context: Context) {
        app = context.applicationContext
        if (job?.isCompleted == false || !restoreNeeded(context)) return
        mutable.value = ReconnectState(
            ReconnectPhase.RECOVERY_REQUIRED,
            "A reconnect was interrupted. Turn airplane mode off in Android settings, or retry recovery with Relay selected as your assistant.",
        )
        if (runCatching { !airplaneOn(context) }.getOrDefault(false) || support(context).ready) recover(context)
        else notifyRecovery(context)
    }

    @MainThread
    fun start(context: Context, pauseSeconds: Int = 10, idleSeconds: Int = 30): Boolean {
        if (job?.isCompleted == false) return false
        if (restoreNeeded(context)) {
            initialize(context)
            return false
        }
        app = context.applicationContext
        begin(context) { it.run(pauseSeconds, idleSeconds) }
        return true
    }

    @MainThread
    fun cancel() { job?.cancel() }

    @MainThread
    fun recover(context: Context): Boolean {
        if (job?.isCompleted == false || !restoreNeeded(context)) return false
        app = context.applicationContext
        begin(context) { it.recover() }
        return true
    }

    /** Optional integration hook. A probe must return only the address observed through [Network]. */
    @MainThread
    fun setIpProbe(probe: (suspend (Context, Network) -> String?)?) {
        check(job?.isCompleted != false) { "An IP probe cannot change during a reconnect." }
        ipProbe = probe
    }

    internal fun voiceReady(voice: RelayVoiceInteractionService) {
        service = voice
        initialize(voice)
    }

    internal fun voiceUnavailable(voice: RelayVoiceInteractionService) {
        if (service !== voice) return
        service = null
        job?.cancel()
        app?.let { if (restoreNeeded(it)) notifyRecovery(it) }
    }

    internal fun commandFor(id: String?, voiceSession: RelayVoiceInteractionSession): Boolean? {
        val command = pending?.takeIf { it.id == id } ?: return null
        if (command.launched) return null
        command.launched = true
        command.owner = voiceSession
        dispatched[command.id] = command
        if (command.enabled) ledger.onDispatched(command.id)
        session = voiceSession
        return command.enabled
    }

    internal fun commandFinished(id: String?) {
        val command = dispatched.remove(id) ?: return
        if (command.enabled) ledger.onFinished(command.id)
        else ledger.offFinished(command.afterEnabledTasks)
        command.finished.complete(Unit)
        // A late ON completion must trigger another OFF after an interrupted cycle.
        app?.let { if (job?.isCompleted != false && restoreNeeded(it)) initialize(it) }
    }

    internal fun commandFailed(id: String?, failure: Exception) {
        // startVoiceActivity threw before dispatching the Settings task.
        val command = dispatched.remove(id) ?: pending?.takeIf { it.id == id } ?: return
        if (command.enabled) ledger.onRejected(command.id)
        command.finished.completeExceptionally(failure)
    }

    internal fun sessionDestroyed(voiceSession: RelayVoiceInteractionSession) {
        val abandoned = dispatched.values.filter { it.owner === voiceSession }
        for (command in abandoned) {
            dispatched.remove(command.id)
            if (command.enabled) ledger.onSessionDestroyed(command.id)
            command.finished.completeExceptionally(IllegalStateException("Android interrupted the assistant session."))
        }
        if (session === voiceSession) {
            session = null
            job?.cancel()
        }
        app?.let { if (job?.isCompleted != false && restoreNeeded(it)) initialize(it) }
    }

    internal fun voiceIntent(enabled: Boolean): Intent = Intent(Settings.ACTION_VOICE_CONTROL_AIRPLANE_MODE)
        .addCategory(Intent.CATEGORY_VOICE)
        .putExtra(Settings.EXTRA_AIRPLANE_MODE_ENABLED, enabled)

    private fun begin(context: Context, operation: suspend (ReconnectCycle) -> Unit) {
        val application = context.applicationContext
        val cycle = ReconnectCycle(AndroidEnvironment(application)) { mutable.value = it }
        mutable.value = ReconnectState(ReconnectPhase.CHECKING_IP, "Preparing reconnect.")
        val next = scope.launch(start = CoroutineStart.LAZY) {
            val wake = application.getSystemService(PowerManager::class.java)
                .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "PageCrawlRelay:assistant-reconnect")
                .apply { setReferenceCounted(false) }
            try {
                wake.acquire(900_000)
                operation(cycle)
            } finally {
                if (wake.isHeld) wake.release()
                pending = null
                val closing = session
                session = null
                runCatching { closing?.finish() }
                if (restoreNeeded(application)) {
                    if (!mutable.value.recoveryRequired) mutable.value = mutable.value.copy(
                        phase = ReconnectPhase.RECOVERY_REQUIRED,
                        message = "Recovery information could not be cleared. Verify airplane mode is off in Android settings, then retry recovery.",
                    )
                    notifyRecovery(application)
                }
                else application.getSystemService(NotificationManager::class.java).cancel(RECOVERY_NOTIFICATION)
            }
        }
        job = next
        next.start()
    }

    private class AndroidEnvironment(private val context: Context) : ReconnectEnvironment {
        override fun startError(): String? {
            val availability = support(context)
            if (!availability.ready) return availability.reason
            if (context.getSystemService(KeyguardManager::class.java).isKeyguardLocked ||
                !context.getSystemService(PowerManager::class.java).isInteractive) {
                return "Unlock the phone and keep Relay open to start reconnecting."
            }
            if (cellular(context) == null) return "Use mobile data with Wi-Fi and VPN turned off before reconnecting."
            val settings = RelaySettings(context)
            if ((settings.enabled || RelayMonitor.state.value.running) && !settings.allowMobileData) {
                return "Enable Also use mobile data before reconnecting while Relay checks is on."
            }
            return null
        }

        override fun airplaneOn() = airplaneOn(context)
        override fun cellularAvailable() = cellular(context) != null
        override fun relayActivity(): RelayActivity? {
            val status = RelayMonitor.currentStatus()
            if (status == null) {
                return if (!RelaySettings(context).enabled && !RelayMonitor.state.value.running && RelayMonitor.statusReader == null) {
                    RelayActivity(0, 0)
                } else null
            }
            val active = status.activeConnections ?: return null
            val revision = status.activityRevision ?: return null
            return RelayActivity(active, revision)
        }
        override fun recoveryReason(): String {
            val availability = support(context)
            return when {
                !availability.selected -> "Select Relay as your assistant to complete recovery."
                !availability.ready -> "Wait for the Relay assistant to initialize, then retry recovery."
                !ledger.canClear() -> "Retry recovery with Relay selected as your assistant."
                else -> "Recovery information could not be saved. Open Relay again to retry."
            }
        }
        override fun markRestoreNeeded(needed: Boolean): Boolean {
            if (!needed && !ledger.canClear()) return false
            recoveryObligation.beforeWrite(needed)
            val saved = recoveryPreferences(context).edit().putBoolean(RECOVERY, needed).commit()
            recoveryObligation.afterWrite(needed, saved)
            if (!needed && saved) {
                ledger.reset()
            }
            return saved
        }
        override suspend fun command(enabled: Boolean) {
            val voice = service ?: error("The Relay assistant is not ready. Open Android settings to turn airplane mode off.")
            check(support(context).selected) { "Relay is no longer the selected assistant." }
            if (!enabled) {
                // Wait for any already-launched ON task before issuing OFF. If it does not
                // settle, still attempt OFF but retain the marker for a subsequent recovery.
                runCatching { withTimeout(5_000) { dispatched.values.filter { it.enabled }.map { it.finished }.forEach { it.await() } } }
            }
            val command = Command(
                UUID.randomUUID().toString(), enabled, CompletableDeferred(),
                afterEnabledTasks = ledger.allOnSettled(),
            )
            pending = command
            try {
                voice.showSession(Bundle().apply { putString(COMMAND_ID, command.id) }, 0)
                withTimeout(10_000) { command.finished.await() }
            } finally {
                if (pending === command) pending = null
            }
        }

        override suspend fun probeIp(): String? {
            val network = cellular(context) ?: return null
            val address = try {
                withTimeout(10_000) {
                    val probe = ipProbe
                    if (probe != null) probe(context, network) else withContext(Dispatchers.IO) {
                        val connection = network.openConnection(URL("https://api.ipify.org")) as javax.net.ssl.HttpsURLConnection
                        try {
                            connection.connectTimeout = 4_000
                            connection.readTimeout = 4_000
                            connection.useCaches = false
                            connection.inputStream.bufferedReader().use { it.readText().trim() }
                                .takeIf(::validIpv4)
                        } finally { connection.disconnect() }
                    }
                }
            } catch (cancel: kotlinx.coroutines.CancellationException) {
                if (cancel !is kotlinx.coroutines.TimeoutCancellationException) throw cancel
                null
            } catch (_: Exception) { null }
            return address?.takeIf { cellular(context) == network && !airplaneOn(context) && validIpv4(it) }
        }

        override suspend fun pause(milliseconds: Long) = delay(milliseconds)
        override fun now(): Long = SystemClock.elapsedRealtime()
    }

    private fun cellular(context: Context): Network? {
        val manager = context.getSystemService(ConnectivityManager::class.java)
        val network = manager.activeNetwork ?: return null
        val caps = manager.getNetworkCapabilities(network) ?: return null
        return network.takeIf {
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) &&
                !caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) &&
                !caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) &&
                !caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN) &&
                caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN) &&
                caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
                caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
        }
    }

    private fun airplaneOn(context: Context): Boolean =
        Settings.Global.getInt(context.contentResolver, Settings.Global.AIRPLANE_MODE_ON) != 0

    private fun recoveryPreferences(context: Context) = context.createDeviceProtectedStorageContext()
        .getSharedPreferences("assistant_recovery", Context.MODE_PRIVATE)
    private fun restoreNeeded(context: Context) = recoveryObligation.needed(recoveryPreferences(context).getBoolean(RECOVERY, false))

    internal fun validIpv4(address: String): Boolean {
        val parts = address.split('.')
        return parts.size == 4 && parts.all { part ->
            part.isNotEmpty() && part.length <= 3 && part.all(Char::isDigit) &&
                part.toIntOrNull()?.let { it in 0..255 } == true
        }
    }

    private const val RECOVERY_NOTIFICATION = 2
    private fun notifyRecovery(context: Context) {
        val notifications = context.getSystemService(NotificationManager::class.java)
        notifications.createNotificationChannel(NotificationChannel(
            "assistant_recovery", "Reconnect recovery", NotificationManager.IMPORTANCE_HIGH,
        ))
        if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) return
        val open = PendingIntent.getActivity(
            context, RECOVERY_NOTIFICATION, Intent(Settings.ACTION_AIRPLANE_MODE_SETTINGS), PendingIntent.FLAG_IMMUTABLE,
        )
        notifications.notify(RECOVERY_NOTIFICATION, NotificationCompat.Builder(context, "assistant_recovery")
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle("Check airplane mode")
            .setContentText("A Relay reconnect was interrupted. Tap to turn airplane mode off in Android settings.")
            .setContentIntent(open).setAutoCancel(true).build())
    }
}
