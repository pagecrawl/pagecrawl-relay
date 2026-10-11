package io.pagecrawl.relay

import android.content.Context
import android.content.SharedPreferences
import android.os.UserManager
import androidx.annotation.MainThread
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

data class ReconnectScheduleState(
    val enabled: Boolean = false,
    val intervalMinutes: Int = 30,
    val nextAttemptAt: Long? = null,
    val waitingReason: String? = null,
    val scheduledBusy: Boolean = false,
)

/** The already-running Relay service checks the optional schedule, without alarms or another service. */
object ReconnectScheduler {
    private var initialized = false
    private var schedule = FixedReconnectSchedule()
    private var storageProblem: String? = null
    private val mutable = MutableStateFlow(ReconnectScheduleState())
    val state: StateFlow<ReconnectScheduleState> = mutable.asStateFlow()

    @MainThread
    fun initialize(context: Context) {
        // Radio recovery can run before unlock, but the optional schedule uses normal app storage.
        if (initialized || !context.getSystemService(UserManager::class.java).isUserUnlocked) return
        val preferences = preferences(context)
        schedule = FixedReconnectSchedule.restore(
            runCatching { preferences.getBoolean(ENABLED, false) }.getOrDefault(false),
            runCatching { preferences.getInt(INTERVAL, 30) }.getOrDefault(30),
            runCatching { preferences.getLong(NEXT_ATTEMPT, 0L) }.getOrDefault(0L),
            System.currentTimeMillis(),
        )
        initialized = true
        // Persist normalization before any timer can dispatch a cycle.
        if (!persist(preferences, schedule)) storageProblem = STORAGE_PROBLEM
        publish()
    }

    @MainThread
    fun configure(context: Context, enabled: Boolean, intervalMinutes: Int): Boolean {
        initialize(context)
        if (!initialized) {
            mutable.value = mutable.value.copy(waitingReason = "Unlock the phone before saving a reconnect schedule.")
            return false
        }
        val next = schedule.configure(enabled, intervalMinutes, System.currentTimeMillis())
        if (!persist(preferences(context), next)) {
            storageProblem = STORAGE_PROBLEM
            publish()
            // A failed save must not keep a requested scheduled cycle running.
            if (!enabled) AssistantReconnect.cancelScheduled()
            return false
        }
        val cancelScheduled = schedule.enabled && !enabled
        schedule = next
        storageProblem = null
        publish()
        if (cancelScheduled) AssistantReconnect.cancelScheduled()
        return true
    }

    @MainThread
    internal fun tick(context: Context) {
        initialize(context)
        if (!initialized) return
        if (storageProblem != null) {
            publish()
            return
        }
        val settings = RelaySettings(context)
        val relay = RelayMonitor.state.value
        val reconnect = AssistantReconnect.state.value
        val eligibility = ScheduledReconnectEligibility(
            relayEnabled = settings.enabled,
            mobileDataAllowed = settings.allowMobileData,
            relayRunning = relay.running,
            networkAllowed = relay.decision == NetworkPolicy.Decision.RUN,
            connected = relay.status.connected,
            reconnectBusy = reconnect.busy,
            recoveryRequired = reconnect.recoveryRequired,
            readinessError = if (schedule.enabled && !reconnect.busy) AssistantReconnect.readinessError(context) else null,
        )
        val attempt = schedule.poll(System.currentTimeMillis(), eligibility.blockedReason())
        if (attempt.schedule != schedule && !persist(preferences(context), attempt.schedule)) {
            storageProblem = STORAGE_PROBLEM
            publish()
            return
        }
        schedule = attempt.schedule
        publish()
        if (!attempt.start) return
        val preferences = preferences(context)
        val pause = runCatching { preferences.getInt("pause_seconds", 10) }.getOrDefault(10).coerceIn(1, 120)
        val idle = runCatching { preferences.getInt("idle_seconds", 30) }.getOrDefault(30).coerceIn(5, 300)
        if (!AssistantReconnect.start(context, pause, idle, scheduled = true)) reconnectFinished(context)
    }

    @MainThread
    internal fun reconnectFinished(context: Context) {
        initialize(context)
        if (!initialized) return
        schedule = schedule.finished(System.currentTimeMillis())
        if (!persist(preferences(context), schedule)) storageProblem = STORAGE_PROBLEM
        publish()
    }

    @MainThread
    internal fun relayStopped(context: Context) {
        initialize(context)
        if (!initialized) {
            AssistantReconnect.cancelScheduled()
            return
        }
        if (schedule.enabled) schedule = schedule.blocked("Turn Relay checks on to run automatic reconnects.")
        publish()
        AssistantReconnect.cancelScheduled()
    }

    private fun publish() {
        mutable.value = ReconnectScheduleState(
            enabled = schedule.enabled,
            intervalMinutes = schedule.intervalMinutes,
            nextAttemptAt = schedule.nextAttemptAt,
            waitingReason = storageProblem ?: schedule.waitingReason,
            scheduledBusy = schedule.scheduledBusy,
        )
    }

    private fun preferences(context: Context) = context.applicationContext
        .getSharedPreferences("relay_assistant_ui", Context.MODE_PRIVATE)

    private fun persist(preferences: SharedPreferences, next: FixedReconnectSchedule): Boolean = runCatching {
        preferences.edit().putBoolean(ENABLED, next.enabled).putInt(INTERVAL, next.intervalMinutes)
            .putLong(NEXT_ATTEMPT, next.nextAttemptAt ?: 0L).commit()
    }.getOrDefault(false)

    private const val ENABLED = "schedule_enabled"
    private const val INTERVAL = "schedule_interval_minutes"
    private const val NEXT_ATTEMPT = "schedule_next_attempt"
    private const val STORAGE_PROBLEM = "Could not save the reconnect schedule. Automatic reconnects are paused. Save the schedule again to retry."
}
