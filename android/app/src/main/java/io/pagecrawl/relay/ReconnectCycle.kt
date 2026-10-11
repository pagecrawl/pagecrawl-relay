package io.pagecrawl.relay

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.withContext

enum class ReconnectPhase {
    IDLE, CHECKING_IP, ENABLING, PAUSING, DISABLING, WAITING_FOR_NETWORK,
    VERIFYING_IP, COMPLETE, ERROR, RECOVERY_REQUIRED,
}

data class ReconnectState(
    val phase: ReconnectPhase = ReconnectPhase.IDLE,
    val message: String = "Reconnect mobile data through Android's selected assistant.",
    val secondsRemaining: Int = 0,
    val beforeIp: String? = null,
    val afterIp: String? = null,
    val ipChanged: Boolean? = null,
) {
    val busy: Boolean get() = phase in setOf(
        ReconnectPhase.CHECKING_IP, ReconnectPhase.ENABLING, ReconnectPhase.PAUSING,
        ReconnectPhase.DISABLING, ReconnectPhase.WAITING_FOR_NETWORK, ReconnectPhase.VERIFYING_IP,
    )
    val recoveryRequired: Boolean get() = phase == ReconnectPhase.RECOVERY_REQUIRED
}

/** The radio operation is independent of Android's service lifecycle, so failure paths can be tested. */
internal interface ReconnectEnvironment {
    fun startError(): String?
    fun airplaneOn(): Boolean
    fun cellularAvailable(): Boolean
    fun markRestoreNeeded(needed: Boolean): Boolean
    fun recoveryReason(): String = "Retry recovery with Relay selected as your assistant."
    suspend fun command(enabled: Boolean)
    suspend fun probeIp(): String?
    suspend fun pause(milliseconds: Long)
    fun now(): Long
}

internal class ReconnectCycle(
    private val environment: ReconnectEnvironment,
    private val publish: (ReconnectState) -> Unit,
) {
    private var state = ReconnectState()

    suspend fun run(pauseSeconds: Int) {
        var restoreNeeded = false
        var problem: String? = null
        try {
            require(pauseSeconds in 1..120) { "Choose a pause between 1 and 120 seconds." }
            check(!environment.airplaneOn()) { "Airplane mode is already on. Turn it off before reconnecting." }
            environment.startError()?.let { error(it) }
            update(ReconnectPhase.CHECKING_IP, "Checking the current public IPv4 address.")
            val before = environment.probeIp() ?: error(
                "Could not check the public IPv4 address. Mobile data has not been interrupted.",
            )
            state = state.copy(beforeIp = before)
            // A network or assistant selection can change while the probe is running.
            environment.startError()?.let { error(it) }
            check(!environment.airplaneOn()) { "Airplane mode was turned on before the reconnect started." }
            check(environment.markRestoreNeeded(true)) { "Could not save recovery information. Reconnect was not started." }
            restoreNeeded = true
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
                val restored = withContext(NonCancellable) { restore(sendEvenIfOff = true) }
                if (!restored) {
                    reportRecoveryRequired()
                    return
                }
            }
        }
        if (problem != null) {
            update(ReconnectPhase.ERROR, problem)
            return
        }
        try {
            update(ReconnectPhase.WAITING_FOR_NETWORK, "Airplane mode is off. Waiting for mobile internet.")
            val connected = await(60_000) { environment.airplaneOn() || environment.cellularAvailable() }
            requireStillOff()
            check(connected) {
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
                },
            )
        } catch (_: CancellationException) {
            val message = runCatching {
                if (environment.airplaneOn()) "Airplane mode was changed outside Relay after reconnecting. Check Android settings."
                else "Reconnect canceled. Airplane mode is off."
            }.getOrDefault("Reconnect canceled. Check airplane mode in Android settings.")
            update(ReconnectPhase.ERROR, message)
        } catch (failure: Exception) {
            update(ReconnectPhase.ERROR, failure.message ?: "Could not verify the mobile connection.")
        }
    }

    private fun requireStillOff() {
        check(!environment.airplaneOn()) {
            "Airplane mode was changed outside Relay after reconnecting. Check Android settings."
        }
    }

    suspend fun recover() {
        update(ReconnectPhase.DISABLING, "Recovering the interrupted reconnect. Turning airplane mode off.")
        if (withContext(NonCancellable) { restore(sendEvenIfOff = true) }) {
            update(ReconnectPhase.COMPLETE, "Airplane mode is off. The interrupted reconnect has been recovered.")
        } else {
            reportRecoveryRequired()
        }
    }

    private fun reportRecoveryRequired() {
        val message = when (runCatching { environment.airplaneOn() }.getOrNull()) {
            false -> "Airplane mode is off. ${environment.recoveryReason()}"
            true -> "Turn airplane mode off in Android settings, or select Relay as your assistant and retry recovery."
            null -> "Relay could not confirm airplane mode is off. Check Android settings, then retry recovery."
        }
        update(ReconnectPhase.RECOVERY_REQUIRED, message)
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
}
