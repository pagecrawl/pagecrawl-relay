package io.pagecrawl.relay

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test

class ReconnectCycleTest {
    private class Environment : ReconnectEnvironment {
        var on = false
        var marker = false
        var error: String? = null
        var internet = true
        var clock = 0L
        var failOn = false
        var failOff = false
        var onObserved = true
        var cancelDuringOn = false
        var cancelDuringPause = false
        var saveFails = false
        var unknownRadio = false
        var unsettledOn = false
        var enableWhileWaiting = false
        var enableDuringFinalProbe = false
        var activity: (() -> RelayActivity?)? = null
        var duringPause: (() -> Unit)? = null
        var holdWhileOn: CompletableDeferred<Unit>? = null
        var holdRestore: CompletableDeferred<Unit>? = null
        val enabled = CompletableDeferred<Unit>()
        val restoring = CompletableDeferred<Unit>()
        var ip: String? = "192.0.2.1"
        val commands = mutableListOf<Boolean>()
        val timeline = mutableListOf<String>()
        override fun startError() = error
        override fun relayActivity(): RelayActivity? = if (activity == null) RelayActivity(0, 0) else activity!!.invoke()
        override fun airplaneOn(): Boolean {
            if (unknownRadio) error("Could not read airplane mode.")
            return on
        }
        override fun cellularAvailable(): Boolean {
            if (enableWhileWaiting && commands.contains(false)) on = true
            return internet
        }
        override fun markRestoreNeeded(needed: Boolean): Boolean {
            timeline += "marker:$needed"
            if (saveFails || (!needed && unsettledOn)) return false
            marker = needed
            return true
        }
        override suspend fun command(enabled: Boolean) {
            assertTrue("Recovery marker must be durable before any radio command", marker)
            commands += enabled
            timeline += "command:$enabled"
            if (enabled && cancelDuringOn) throw CancellationException()
            if (enabled && failOn || !enabled && failOff) error("Assistant action unavailable.")
            if (!enabled) { restoring.complete(Unit); holdRestore?.await() }
            if (!enabled || onObserved) on = enabled
            if (enabled) this.enabled.complete(Unit)
        }
        override suspend fun probeIp(): String? {
            if (commands.isEmpty()) return ip
            if (enableDuringFinalProbe) on = true
            return "192.0.2.2"
        }
        override suspend fun pause(milliseconds: Long) {
            clock += milliseconds
            duringPause?.invoke()
            if (on) holdWhileOn?.await()
            if (on && cancelDuringPause) { cancelDuringPause = false; throw CancellationException() }
        }
        override fun now() = clock
    }

    @Test fun waitsForActiveConnectionsThenTheFullIdleWindowBeforeChangingRadio() = runBlocking {
        val env = Environment()
        env.activity = { RelayActivity(if (env.clock < 5_000) 1 else 0, 2) }
        var enabledAt = 0L
        val states = mutableListOf<ReconnectState>()
        ReconnectCycle(env) {
            states += it
            if (it.phase == ReconnectPhase.ENABLING) enabledAt = env.clock
        }.run(1, 5)
        assertEquals(10_000L, enabledAt)
        assertTrue(states.any { it.phase == ReconnectPhase.WAITING_FOR_IDLE && it.busy })
        assertEquals(listOf(true, false), env.commands)
    }

    @Test fun shortActivityBetweenPollsRestartsTheEntireIdleWindow() = runBlocking {
        val env = Environment()
        env.activity = { RelayActivity(0, if (env.clock < 3_000) 0 else 2) }
        var enabledAt = 0L
        ReconnectCycle(env) { if (it.phase == ReconnectPhase.ENABLING) enabledAt = env.clock }.run(1, 5)
        assertEquals(8_000L, enabledAt)
    }

    @Test fun canCancelWhileWaitingWithoutChangingRadioOrSavingRecovery() = runBlocking {
        val env = Environment()
        env.duringPause = { if (env.commands.isEmpty()) throw CancellationException() }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.run(1, 30)
        assertEquals(ReconnectPhase.ERROR, state.phase)
        assertEquals("Reconnect canceled.", state.message)
        assertTrue(env.commands.isEmpty())
        assertFalse(env.marker)
    }

    @Test fun refusesReconnectIfActivityCannotBecomeIdleWithinTenMinutes() = runBlocking {
        val env = Environment().apply { activity = { RelayActivity(1, 1) } }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.run(1, 30)
        assertEquals(ReconnectPhase.ERROR, state.phase)
        assertTrue(state.message.contains("10 minutes"))
        assertTrue(env.commands.isEmpty())
        assertFalse(env.marker)
    }

    @Test fun missingActivityInformationNeverStartsRadioChanges() = runBlocking {
        val env = Environment().apply { activity = { null } }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.run(1, 30)
        assertEquals(ReconnectPhase.ERROR, state.phase)
        assertTrue(env.commands.isEmpty())
        assertFalse(env.marker)
    }

    @Test fun refusesIdleValuesOutsideTheSupportedRange() = runBlocking {
        for (seconds in listOf(4, 301)) {
            val env = Environment()
            var state = ReconnectState()
            ReconnectCycle(env) { state = it }.run(1, seconds)
            assertEquals(ReconnectPhase.ERROR, state.phase)
            assertTrue(env.commands.isEmpty())
            assertFalse(env.marker)
        }
    }

    @Test fun ongoingShortActivityNeverGetsMistakenForAnIdlePhone() = runBlocking {
        val env = Environment()
        env.activity = { RelayActivity(0, env.clock / 1_000) }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.run(1, 5)
        assertEquals(ReconnectPhase.ERROR, state.phase)
        assertTrue(state.message.contains("10 minutes"))
        assertTrue(env.commands.isEmpty())
        assertFalse(env.marker)
    }

    @Test fun abortsBeforeRadioChangesIfTheNetworkChangesWhileWaiting() = runBlocking {
        val env = Environment()
        env.duringPause = { env.error = "Wi-Fi is active." }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.run(1, 30)
        assertEquals(ReconnectPhase.ERROR, state.phase)
        assertEquals("Wi-Fi is active.", state.message)
        assertTrue(env.commands.isEmpty())
        assertFalse(env.marker)
    }

    @Test fun verifiesBothAddressesAndPersistsBeforeChangingRadio() = runBlocking {
        val env = Environment()
        val states = mutableListOf<ReconnectState>()
        ReconnectCycle(env, states::add).run(2)
        assertEquals(listOf(true, false), env.commands)
        assertTrue(env.timeline.indexOf("marker:true") < env.timeline.indexOf("command:true"))
        assertFalse(env.on)
        assertFalse(env.marker)
        assertEquals(listOf(2, 1), states.filter { it.phase == ReconnectPhase.PAUSING }.map { it.secondsRemaining })
        assertEquals(true, states.last().ipChanged)
        assertEquals("192.0.2.1", states.last().beforeIp)
        assertEquals("192.0.2.2", states.last().afterIp)
    }

    @Test fun refusesPreexistingAirplaneModeWithoutChangingIt() = runBlocking {
        val env = Environment().apply { on = true }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.run(1)
        assertEquals(ReconnectPhase.ERROR, state.phase)
        assertTrue(env.on)
        assertFalse(env.marker)
        assertTrue(env.commands.isEmpty())
    }

    @Test fun refusesDisallowedNetworkAndMissingProbeWithoutRadioChanges() = runBlocking {
        for (env in listOf(Environment().apply { error = "Wi-Fi is active." }, Environment().apply { ip = null })) {
            ReconnectCycle(env) {}.run(1)
            assertTrue(env.commands.isEmpty())
            assertFalse(env.marker)
        }
    }

    @Test fun refusesIfRecoveryCannotBeSaved() = runBlocking {
        val env = Environment().apply { saveFails = true }
        ReconnectCycle(env) {}.run(1)
        assertTrue(env.commands.isEmpty())
    }

    @Test fun restoresAfterCancellationWhileOnOrWhileEnableIsPending() = runBlocking {
        for (env in listOf(Environment().apply { cancelDuringPause = true }, Environment().apply { cancelDuringOn = true })) {
            ReconnectCycle(env) {}.run(10)
            assertEquals(listOf(true, false), env.commands)
            assertFalse(env.on)
            assertFalse(env.marker)
        }
    }

    @Test fun neverStartsCountdownUntilAndroidConfirmsOn() = runBlocking {
        val env = Environment().apply { onObserved = false }
        val states = mutableListOf<ReconnectState>()
        ReconnectCycle(env, states::add).run(2)
        assertFalse(states.any { it.phase == ReconnectPhase.PAUSING })
        assertEquals(listOf(true, false), env.commands)
        assertEquals(ReconnectPhase.ERROR, states.last().phase)
        assertTrue(env.clock >= 20_000)
    }

    @Test fun keepsRecoveryMarkerAfterRoleLossWhileAirplaneIsOn() = runBlocking {
        val env = Environment().apply { failOff = true }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.run(1)
        assertTrue(env.on)
        assertTrue(env.marker)
        assertEquals(ReconnectPhase.RECOVERY_REQUIRED, state.phase)
    }

    @Test fun cannotClearMarkerForUnsettledEnableEvenIfObservedOff() = runBlocking {
        val env = Environment().apply { cancelDuringOn = true; failOff = true; unsettledOn = true }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.run(1)
        assertFalse(env.on)
        assertTrue(env.marker)
        assertEquals(ReconnectPhase.RECOVERY_REQUIRED, state.phase)
        assertTrue(state.message.startsWith("Airplane mode is off."))
    }

    @Test fun unknownRadioStateDoesNotClearAnExistingMarker() = runBlocking {
        val env = Environment().apply { marker = true; unknownRadio = true; failOff = true }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.recover()
        assertTrue(env.marker)
        assertEquals(ReconnectPhase.RECOVERY_REQUIRED, state.phase)
    }

    @Test fun interruptedCycleCanBeRecoveredWithoutStartingANewCycle() = runBlocking {
        val env = Environment().apply { marker = true; on = true }
        ReconnectCycle(env) {}.recover()
        assertEquals(listOf(false), env.commands)
        assertFalse(env.on)
        assertFalse(env.marker)
    }

    @Test fun alreadyOffButUnclearedRecoveryReportsActualRadioState() = runBlocking {
        val env = Environment().apply { marker = true; on = false; unsettledOn = true; failOff = true }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.recover()
        assertTrue(env.marker)
        assertFalse(env.on)
        assertEquals(ReconnectPhase.RECOVERY_REQUIRED, state.phase)
        assertTrue(state.message.startsWith("Airplane mode is off."))
        assertTrue(state.message.contains("Retry recovery"))
    }

    @Test fun canceledJobRemainsIncompleteUntilRadioRestorationFinishes() = runBlocking {
        val env = Environment().apply {
            holdWhileOn = CompletableDeferred()
            holdRestore = CompletableDeferred()
        }
        val operation = launch { ReconnectCycle(env) {}.run(10) }
        env.enabled.await()
        operation.cancel()
        env.restoring.await()
        assertFalse(operation.isActive)
        assertFalse("A canceled restore must still block another cycle", operation.isCompleted)
        assertTrue(env.on)
        env.holdRestore!!.complete(Unit)
        operation.join()
        assertFalse(env.on)
        assertFalse(env.marker)
    }

    @Test fun boundsWaitingForCellularRecoveryAndDoesNotClaimRelayConnected() = runBlocking {
        val env = Environment().apply { internet = false }
        var state = ReconnectState()
        ReconnectCycle(env) { state = it }.run(1)
        assertEquals(ReconnectPhase.ERROR, state.phase)
        assertFalse(env.on)
        assertFalse(env.marker)
        assertTrue(env.clock >= 61_000)
    }

    @Test fun preservesUsersLaterAirplaneModeChangeAndDoesNotClaimItIsOff() = runBlocking {
        for (env in listOf(
            Environment().apply { enableWhileWaiting = true },
            Environment().apply { enableDuringFinalProbe = true },
        )) {
            var state = ReconnectState()
            ReconnectCycle(env) { state = it }.run(1)
            assertEquals(ReconnectPhase.ERROR, state.phase)
            assertTrue(state.message.contains("changed outside Relay"))
            assertTrue(env.on)
            assertFalse(env.marker)
            assertEquals("Do not undo a user's new independent action", listOf(true, false), env.commands)
        }
    }
}
