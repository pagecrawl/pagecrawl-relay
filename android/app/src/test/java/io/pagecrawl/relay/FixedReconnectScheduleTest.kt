package io.pagecrawl.relay

import org.junit.Assert.*
import org.junit.Test

class FixedReconnectScheduleTest {
    private val now = 1_000_000L
    private val interval = 30 * 60_000L

    @Test fun automaticReconnectsAreOffUntilExplicitlyEnabled() {
        val schedule = FixedReconnectSchedule()
        assertFalse(schedule.enabled)
        assertEquals(30, schedule.intervalMinutes)
        assertNull(schedule.nextAttemptAt)
        assertFalse(schedule.poll(Long.MAX_VALUE, null).start)
    }

    @Test fun enablingWaitsOneFullIntervalRatherThanReconnectImmediately() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        assertEquals(now + interval, enabled.nextAttemptAt)
        assertFalse(enabled.poll(now + interval - 1, null).start)
        assertTrue(enabled.poll(now + interval, null).start)
    }

    @Test fun changingIntervalStartsTheNewIntervalFromTheChange() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        val changed = enabled.configure(true, 5, now + 60_000)
        assertEquals(now + 360_000, changed.nextAttemptAt)
        assertFalse(changed.poll(now + 359_999, null).start)
        assertTrue(changed.poll(now + 360_000, null).start)
    }

    @Test fun savingUnchangedChoicesDoesNotPostponeTheSchedule() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        assertEquals(enabled, enabled.configure(true, 30, now + 60_000))
    }

    @Test fun disablingRemovesTheDueTimeAndCannotStartAnotherAttempt() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        val disabled = enabled.configure(false, 30, now + interval)
        assertNull(disabled.nextAttemptAt)
        assertFalse(disabled.poll(now + interval * 20, null).start)
    }

    @Test fun blockedOccurrenceKeepsItsDueTimeUntilItCanRun() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        val first = enabled.poll(now + interval, "Unlock the phone.")
        val later = first.schedule.poll(now + interval * 4, "Unlock the phone.")
        assertFalse(first.start)
        assertFalse(later.start)
        assertEquals(enabled.nextAttemptAt, later.schedule.nextAttemptAt)
        assertEquals("Unlock the phone.", later.schedule.waitingReason)
        assertTrue(later.schedule.poll(now + interval * 4, null).start)
    }

    @Test fun noWarningIsDisplayedForPrerequisitesBeforeTheDueTime() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        val polled = enabled.poll(now + 1, "Unlock the phone.")
        assertFalse(polled.start)
        assertNull(polled.schedule.waitingReason)
    }

    @Test fun overdueOccurrenceRunsOnceWithoutCatchingUpMissedIntervals() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        val due = now + interval * 20
        val claimed = enabled.poll(due, null)
        assertTrue(claimed.start)
        assertTrue(claimed.schedule.scheduledBusy)
        assertEquals(due + interval, claimed.schedule.nextAttemptAt)
        assertFalse(claimed.schedule.poll(due + interval * 2, null).start)
        val complete = claimed.schedule.finished(due + 60_000)
        assertFalse(complete.scheduledBusy)
        assertEquals(due + 60_000 + interval, complete.nextAttemptAt)
        assertFalse(complete.poll(due + 60_000, null).start)
    }

    @Test fun failedOrCanceledAttemptStillConsumesTheOccurrence() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        val claimed = enabled.poll(now + interval, null).schedule
        val finished = claimed.finished(now + interval + 10_000)
        assertEquals(now + interval * 2 + 10_000, finished.nextAttemptAt)
        assertFalse(finished.poll(now + interval + 15_000, null).start)
    }

    @Test fun manualReconnectFinishingBeforeDueTimeRestartsTheFullInterval() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        val finished = enabled.finished(now + 5 * 60_000)
        assertEquals(now + 35 * 60_000, finished.nextAttemptAt)
        assertFalse(finished.poll(now + interval, null).start)
    }

    @Test fun disablingDuringOwnedAttemptRetainsOwnershipUntilRestorationFinishes() {
        val claimed = FixedReconnectSchedule().configure(true, 30, now)
            .poll(now + interval, null).schedule
        val disabled = claimed.configure(false, 30, now + interval + 1)
        assertTrue(disabled.scheduledBusy)
        assertNull(disabled.nextAttemptAt)
        val restored = disabled.finished(now + interval + 2)
        assertFalse(restored.enabled)
        assertFalse(restored.scheduledBusy)
        assertNull(restored.nextAttemptAt)
    }

    @Test fun intervalChangeDuringAttemptUsesNewIntervalAfterCompletion() {
        val claimed = FixedReconnectSchedule().configure(true, 30, now)
            .poll(now + interval, null).schedule
        val changed = claimed.configure(true, 60, now + interval + 1)
        assertTrue(changed.scheduledBusy)
        val finished = changed.finished(now + interval + 10_000)
        assertEquals(now + interval + 10_000 + 60 * 60_000, finished.nextAttemptAt)
    }

    @Test fun restartPreservesAnOverdueOccurrenceButDoesNotCreateABacklog() {
        val restored = FixedReconnectSchedule.restore(true, 30, now, now + interval * 20)
        val claimed = restored.poll(now + interval * 20, null)
        assertTrue(claimed.start)
        assertFalse(claimed.schedule.poll(now + interval * 30, null).start)
    }

    @Test fun persistedClaimPreventsAQuickReplayAfterProcessDeath() {
        val claimed = FixedReconnectSchedule().configure(true, 30, now)
            .poll(now + interval, null).schedule
        val restored = FixedReconnectSchedule.restore(true, 30, claimed.nextAttemptAt, now + interval + 5_000)
        assertFalse(restored.scheduledBusy)
        assertFalse(restored.poll(now + interval + 5_000, null).start)
    }

    @Test fun backwardClockChangeCannotDelayAnAttemptBeyondOneFullInterval() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        val rewound = enabled.poll(now - 600_000, null)
        assertFalse(rewound.start)
        assertEquals(now - 600_000 + interval, rewound.schedule.nextAttemptAt)
        assertTrue(rewound.schedule.poll(now - 600_000 + interval, null).start)
    }

    @Test fun forwardClockChangeYieldsOnlyOneDueAttempt() {
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        val later = enabled.poll(now + 86_400_000, null)
        assertTrue(later.start)
        assertEquals(now + 86_400_000 + interval, later.schedule.nextAttemptAt)
    }

    @Test fun malformedStoredChoicesUseDefaultIntervalAndFreshDeadline() {
        val restored = FixedReconnectSchedule.restore(true, -1, -100, now)
        assertEquals(30, restored.intervalMinutes)
        assertEquals(now + interval, restored.nextAttemptAt)
        assertFalse(restored.poll(now, null).start)
        assertNull(FixedReconnectSchedule.restore(false, 30, now, now).nextAttemptAt)
    }

    @Test fun staleFutureStoredDeadlineIsNormalizedAfterAClockChange() {
        val restored = FixedReconnectSchedule.restore(true, 30, now + interval * 100, now)
        assertEquals(now + interval, restored.nextAttemptAt)
    }

    @Test fun intervalBoundsPermitFiveMinutesThroughTwentyFourHours() {
        assertEquals(5, FixedReconnectSchedule().configure(true, 5, now).intervalMinutes)
        assertEquals(1_440, FixedReconnectSchedule().configure(true, 1_440, now).intervalMinutes)
        for (interval in listOf(0, 4, 1_441, Int.MAX_VALUE)) {
            assertThrows(IllegalArgumentException::class.java) { FixedReconnectSchedule().configure(true, interval, now) }
        }
    }

    @Test fun deadlineCannotOverflowToAPastTime() {
        val enabled = FixedReconnectSchedule().configure(true, 1_440, Long.MAX_VALUE - 1)
        assertEquals(Long.MAX_VALUE, enabled.nextAttemptAt)
        assertFalse(enabled.poll(Long.MAX_VALUE - 1, null).start)
    }

    private val ready = ScheduledReconnectEligibility(true, true, true, true, true, false, false, null)

    @Test fun runningRelayWithAllRequiredPermissionsMayAttempt() {
        assertNull(ready.blockedReason())
    }

    @Test fun relayingAndMobileDataMustBothBeExplicitlyEnabled() {
        assertNotNull(ready.copy(relayEnabled = false).blockedReason())
        assertNotNull(ready.copy(relayRunning = false).blockedReason())
        assertNotNull(ready.copy(mobileDataAllowed = false).blockedReason())
    }

    @Test fun disconnectedOrDisallowedNetworksDoNotTriggerReconnects() {
        assertNotNull(ready.copy(networkAllowed = false).blockedReason())
        assertNotNull(ready.copy(connected = false).blockedReason())
    }

    @Test fun currentCycleAndRecoveryTakePriorityOverAnotherScheduledAttempt() {
        assertNotNull(ready.copy(reconnectBusy = true).blockedReason())
        assertNotNull(ready.copy(recoveryRequired = true).blockedReason())
        val enabled = FixedReconnectSchedule().configure(true, 30, now)
        assertFalse(enabled.poll(now + interval, ready.copy(reconnectBusy = true).blockedReason()).start)
        assertFalse(enabled.poll(now + interval, ready.copy(recoveryRequired = true).blockedReason()).start)
    }

    @Test fun assistantUnlockAndNetworkReadinessFailuresAreShownUnchanged() {
        for (reason in listOf("Unlock the phone.", "Select Relay as the assistant.", "Use mobile data without Wi-Fi or VPN.")) {
            assertEquals(reason, ready.copy(readinessError = reason).blockedReason())
            assertFalse(FixedReconnectSchedule().configure(true, 30, now)
                .poll(now + interval, ready.copy(readinessError = reason).blockedReason()).start)
        }
    }
}
