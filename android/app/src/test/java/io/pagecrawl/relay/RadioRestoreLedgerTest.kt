package io.pagecrawl.relay

import org.junit.Assert.*
import org.junit.Test

class RadioRestoreLedgerTest {
    @Test fun pendingOnCannotBeClearedByOffObservedBeforeItCompletes() {
        val ledger = RadioRestoreLedger()
        ledger.onDispatched("enable")
        val offStartedAfterOn = ledger.allOnSettled()
        ledger.offFinished(offStartedAfterOn)
        assertFalse(ledger.canClear())
        ledger.onFinished("enable")
        assertFalse(ledger.canClear())
        ledger.offFinished(ledger.allOnSettled())
        assertTrue(ledger.canClear())
    }

    @Test fun taskRejectedBeforeLaunchDoesNotRequireRadioRestoration() {
        val ledger = RadioRestoreLedger()
        ledger.onDispatched("enable")
        ledger.onRejected("enable")
        assertTrue(ledger.canClear())
    }

    @Test fun normalCompletionNeedsSubsequentOffAndResetAllowsAnotherCycle() {
        val ledger = RadioRestoreLedger()
        ledger.onDispatched("first")
        ledger.onFinished("first")
        assertFalse(ledger.canClear())
        ledger.offFinished(ledger.allOnSettled())
        assertTrue(ledger.canClear())
        ledger.reset()
        ledger.onDispatched("second")
        assertFalse(ledger.canClear())
    }

    @Test fun destroyedSessionCanRecoverWithFreshOffWithoutWaitingForever() {
        val ledger = RadioRestoreLedger()
        ledger.onDispatched("abandoned-on")
        ledger.onSessionDestroyed("abandoned-on")
        assertTrue(ledger.allOnSettled())
        assertFalse(ledger.canClear())
        ledger.offFinished(ledger.allOnSettled())
        assertTrue(ledger.canClear())
    }
}
