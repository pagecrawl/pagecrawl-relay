package io.pagecrawl.relay

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import kotlin.coroutines.coroutineContext

enum class ReconnectPhase {
    IDLE, CHECKING_IP, WAITING_FOR_IDLE, PREPARING_RECONNECT, ENABLING, PAUSING, DISABLING, WAITING_FOR_NETWORK,
    RESUMING_CHECKS, VERIFYING_IP, COMPLETE, ERROR, RECOVERY_REQUIRED,
}

data class ReconnectState(
    val phase: ReconnectPhase = ReconnectPhase.IDLE,
    val message: String = "Reconnect manually or enable Scheduled reconnects. Each attempt waits for Relay to stay idle.",
    val secondsRemaining: Int = 0,
    val beforeIp: String? = null,
    val afterIp: String? = null,
    val ipChanged: Boolean? = null,
    val backendWaitPending: Boolean = false,
) {
    val busy: Boolean get() = phase in setOf(
        ReconnectPhase.CHECKING_IP, ReconnectPhase.WAITING_FOR_IDLE, ReconnectPhase.PREPARING_RECONNECT,
        ReconnectPhase.ENABLING, ReconnectPhase.PAUSING, ReconnectPhase.DISABLING,
        ReconnectPhase.WAITING_FOR_NETWORK, ReconnectPhase.RESUMING_CHECKS, ReconnectPhase.VERIFYING_IP,
    )
    val recoveryRequired: Boolean get() = phase == ReconnectPhase.RECOVERY_REQUIRED
}

internal data class RelayActivity(val activeConnections: Int, val revision: Long)
internal data class BackendReconnectLease(val id: String, val expiresAt: Long, val storedForRecovery: Boolean = true)

/** The radio operation is independent of Android's service lifecycle, so failure paths can be tested. */
internal interface ReconnectEnvironment {
    fun startError(): String?
    fun airplaneOn(): Boolean
    fun cellularAvailable(): Boolean
    fun markRestoreNeeded(needed: Boolean): Boolean
    fun recoveryReason(): String = "Retry recovery with Relay selected as your assistant."
    fun relayActivity(): RelayActivity?
    fun pendingBackendLease(): BackendReconnectLease? = null
    suspend fun beginBackendReconnect(holdSeconds: Int): BackendReconnectLease?
    suspend fun completeBackendReconnect(lease: BackendReconnectLease): Boolean
    suspend fun command(enabled: Boolean)
    suspend fun probeIp(): String?
    suspend fun pause(milliseconds: Long)
    fun now(): Long
    fun wallNow(): Long = now()
}

internal class ReconnectCycle(
    private val environment: ReconnectEnvironment,
    private val publish: (ReconnectState) -> Unit,
) {
    private var state = ReconnectState()

    suspend fun run(pauseSeconds: Int, idleSeconds: Int = 30) {
        var restoreNeeded = false
        var restored = true
        var problem: String? = null
        var lease: BackendReconnectLease? = null
        var completion = LeaseCompletion(false, true)
        try {
            require(pauseSeconds in 1..120) { "Choose a pause between 1 and 120 seconds." }
            require(idleSeconds in 5..300) { "Choose an idle wait between 5 and 300 seconds." }
            check(!environment.airplaneOn()) { "Airplane mode is already on. Turn it off before reconnecting." }
            environment.startError()?.let { error(it) }
            update(ReconnectPhase.CHECKING_IP, "Checking the current public IPv4 address.")
            val before = environment.probeIp() ?: error(
                "Could not check the public IPv4 address. Mobile data has not been interrupted.",
            )
            state = state.copy(beforeIp = before)
            val idleActivity = waitForIdle(idleSeconds)
            // A network or assistant selection can change while the probe is running.
            environment.startError()?.let { error(it) }
            check(!environment.airplaneOn()) { "Airplane mode was turned on before the reconnect started." }
            update(ReconnectPhase.PREPARING_RECONNECT, "Preparing PageCrawl for the reconnect. Mobile data is still connected.")
            // Learn and save an acknowledged lease before observing cancellation. The HTTP
            // operation has its own finite deadline; canceling can never strand an unknown ON task.
            lease = withContext(NonCancellable) {
                environment.beginBackendReconnect((pauseSeconds + LEASE_OVERHEAD_SECONDS).coerceAtMost(300))
            } ?: error("PageCrawl did not confirm the reconnect. Mobile data has not been interrupted.")
            coroutineContext.ensureActive()
            check(lease.storedForRecovery) {
                "Could not save the PageCrawl reconnect pause for recovery. Mobile data has not been interrupted."
            }
            check(lease.expiresAt - environment.wallNow() >= (pauseSeconds + MINIMUM_LEASE_OVERHEAD_SECONDS) * 1_000L) {
                "The PageCrawl reconnect pause is too close to expiry. Mobile data has not been interrupted. Try again."
            }
            check(environment.relayActivity() == idleActivity) {
                "New Relay activity arrived while preparing the reconnect. Mobile data has not been interrupted. Try again when Relay is idle."
            }
            environment.startError()?.let { error(it) }
            check(!environment.airplaneOn()) { "Airplane mode was turned on before the reconnect started." }
            coroutineContext.ensureActive()
            check(environment.markRestoreNeeded(true)) { "Could not save recovery information. Reconnect was not started." }
            restoreNeeded = true
            coroutineContext.ensureActive()
            update(ReconnectPhase.ENABLING, "Turning airplane mode on.")
            environment.command(true)
            check(await(20_000) { environment.airplaneOn() }) { "Android did not confirm that airplane mode turned on." }
            for (remaining in pauseSeconds downTo 1) {
                check(environment.airplaneOn()) { "Airplane mode was changed outside Relay. Reconnect stopped." }
                update(ReconnectPhase.PAUSING, "Airplane mode is on. Mobile data will reconnect after the pause.", remaining)
                environment.pause(1_000)
            }
        } catch (_: CancellationException) {
            problem = "Reconnect canceled."
        } catch (failure: Exception) {
            problem = failure.message ?: "Reconnect could not be completed."
        } finally {
            if (restoreNeeded) {
                // Cancellation never skips the bounded restore attempt.
                restored = withContext(NonCancellable) { restore(sendEvenIfOff = true) }
            }
            lease?.let {
                // OFF recovery always precedes the best-effort backend request.
                completion = if (!restored) LeaseCompletion(false, false)
                else withContext(NonCancellable) { finishLease(it, waitForNetwork = restoreNeeded) }
                state = state.copy(backendWaitPending = !completion.released)
            }
        }
        if (!restored) {
            reportRecoveryRequired()
            return
        }
        if (problem != null) {
            update(ReconnectPhase.ERROR, problem + backendWaitMessage())
            return
        }
        try {
            requireStillOff()
            check(completion.networkReady) {
                "Airplane mode is off, but mobile internet did not return. Check mobile data and your carrier connection."
            }
            update(ReconnectPhase.VERIFYING_IP, "Mobile internet is back. Checking the public IPv4 address.")
            val after = environment.probeIp()
            requireStillOff()
            state = state.copy(afterIp = after, ipChanged = after?.let { it != state.beforeIp })
            update(
                ReconnectPhase.COMPLETE,
                when (state.ipChanged) {
                    true -> "Mobile internet reconnected with a different public IPv4 address."
                    false -> "Mobile internet reconnected with the same public IPv4 address. Your carrier controls the address."
                    null -> "Airplane mode is off. The public IPv4 address could not be verified after reconnecting."
                } + backendWaitMessage(),
            )
        } catch (_: CancellationException) {
            val message = runCatching {
                if (environment.airplaneOn()) "Airplane mode was changed outside Relay after reconnecting. Check Android settings."
                else "Reconnect canceled. Airplane mode is off."
            }.getOrDefault("Reconnect canceled. Check airplane mode in Android settings.")
            update(ReconnectPhase.ERROR, message + backendWaitMessage())
        } catch (failure: Exception) {
            update(ReconnectPhase.ERROR, (failure.message ?: "Could not verify the mobile connection.") + backendWaitMessage())
        }
    }

    private suspend fun waitForIdle(seconds: Int): RelayActivity {
        val deadline = environment.now() + 600_000
        val window = IdleWindow(seconds * 1_000L)
        while (true) {
            environment.startError()?.let { error(it) }
            check(!environment.airplaneOn()) { "Airplane mode was turned on before the reconnect started." }
            check(environment.now() < deadline) {
                "Relay did not stay idle within 10 minutes. Mobile data has not been interrupted. Try again later."
            }
            val activity = environment.relayActivity()
            val remaining = window.remaining(environment.now(), activity)
            if (remaining == 0L && activity != null) return activity
            update(
                ReconnectPhase.WAITING_FOR_IDLE,
                when {
                    activity == null -> "Waiting for Relay activity information. Mobile data has not been interrupted."
                    activity.activeConnections > 0 -> "Waiting for active Relay connections to finish. New activity restarts the idle wait."
                    else -> "Waiting for Relay to stay idle. New activity restarts the idle wait."
                },
                ((remaining + 999) / 1_000).toInt(),
            )
            environment.pause(1_000)
        }
    }

    private fun requireStillOff() {
        check(!environment.airplaneOn()) {
            "Airplane mode was changed outside Relay after reconnecting. Check Android settings."
        }
    }

    suspend fun recover() {
        val lease = environment.pendingBackendLease()
        state = state.copy(backendWaitPending = lease != null)
        update(ReconnectPhase.DISABLING, "Recovering the interrupted reconnect. Turning airplane mode off.")
        if (withContext(NonCancellable) { restore(sendEvenIfOff = true) }) {
            lease?.let {
                val completion = withContext(NonCancellable) { finishLease(it, waitForNetwork = true) }
                state = state.copy(backendWaitPending = !completion.released)
            }
            update(ReconnectPhase.COMPLETE, "Airplane mode is off. The interrupted reconnect has been recovered." + backendWaitMessage())
        } else {
            reportRecoveryRequired()
        }
    }

    suspend fun recoverBackendLease() {
        val lease = environment.pendingBackendLease()
        if (lease == null) {
            update(ReconnectPhase.COMPLETE, "PageCrawl's temporary reconnect pause has expired.")
            return
        }
        val completion = withContext(NonCancellable) { finishLease(lease, waitForNetwork = true) }
        state = state.copy(backendWaitPending = !completion.released)
        update(
            ReconnectPhase.COMPLETE,
            (if (completion.released) "PageCrawl's temporary reconnect pause has ended."
            else "PageCrawl's temporary reconnect pause is still pending.") + backendWaitMessage(),
        )
    }

    private data class LeaseCompletion(val networkReady: Boolean, val released: Boolean)

    private suspend fun finishLease(lease: BackendReconnectLease, waitForNetwork: Boolean): LeaseCompletion {
        return try {
            if (environment.airplaneOn()) return LeaseCompletion(false, false)
            if (waitForNetwork && !environment.cellularAvailable()) {
                update(ReconnectPhase.WAITING_FOR_NETWORK, "Airplane mode is off. Waiting for mobile internet before resuming PageCrawl checks.")
                await(60_000) { environment.airplaneOn() || environment.cellularAvailable() }
            }
            if (environment.airplaneOn() || !environment.cellularAvailable()) return LeaseCompletion(false, false)
            update(ReconnectPhase.RESUMING_CHECKS, "Mobile internet is back. Resuming PageCrawl checks.")
            LeaseCompletion(true, environment.completeBackendReconnect(lease))
        } catch (_: Exception) {
            LeaseCompletion(runCatching { !environment.airplaneOn() && environment.cellularAvailable() }.getOrDefault(false), false)
        }
    }

    private fun backendWaitMessage(): String = if (state.backendWaitPending) {
        " PageCrawl could not confirm resuming checks. Its temporary reconnect pause expires within five minutes."
    } else ""

    private fun reportRecoveryRequired() {
        val message = when (runCatching { environment.airplaneOn() }.getOrNull()) {
            false -> "Airplane mode is off. ${environment.recoveryReason()}"
            true -> "Turn airplane mode off in Android settings, or select Relay as your assistant and retry recovery."
            null -> "Relay could not confirm airplane mode is off. Check Android settings, then retry recovery."
        }
        update(ReconnectPhase.RECOVERY_REQUIRED, message + backendWaitMessage())
    }

    private suspend fun restore(sendEvenIfOff: Boolean): Boolean = try {
        update(ReconnectPhase.DISABLING, "Turning airplane mode off.")
        if (sendEvenIfOff || environment.airplaneOn()) environment.command(false)
        if (!await(20_000) { !environment.airplaneOn() }) false
        else environment.markRestoreNeeded(false)
    } catch (_: Exception) {
        // If the command failed after Android applied it, the observed radio state is authoritative.
        runCatching { !environment.airplaneOn() && environment.markRestoreNeeded(false) }.getOrDefault(false)
    }

    private suspend fun await(timeout: Long, condition: () -> Boolean): Boolean {
        val deadline = environment.now() + timeout
        while (!condition()) {
            if (environment.now() >= deadline) return false
            environment.pause(250)
        }
        return true
    }

    private fun update(phase: ReconnectPhase, message: String, remaining: Int = 0) {
        state = state.copy(phase = phase, message = message, secondsRemaining = remaining)
        publish(state)
    }

    private companion object {
        // Covers bounded assistant commands, radio confirmation, network return and HTTP cleanup.
        const val LEASE_OVERHEAD_SECONDS = 170
        const val MINIMUM_LEASE_OVERHEAD_SECONDS = 150
    }
}
