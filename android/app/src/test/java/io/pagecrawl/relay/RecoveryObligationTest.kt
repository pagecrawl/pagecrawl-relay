package io.pagecrawl.relay

import org.junit.Assert.*
import org.junit.Test

class RecoveryObligationTest {
    @Test fun failedDiskClearKeepsRecoveryAvailableDespiteCachedFalse() {
        val obligation = RecoveryObligation()
        obligation.beforeWrite(true)
        obligation.afterWrite(true, saved = true)
        obligation.beforeWrite(false)
        obligation.afterWrite(false, saved = false)
        assertTrue(obligation.needed(persistedValue = false))
        obligation.afterWrite(false, saved = true)
        assertFalse(obligation.needed(persistedValue = false))
    }

    @Test fun diskMarkerSurvivesProcessRestartAndFailedSaveStaysVisible() {
        assertTrue(RecoveryObligation().needed(persistedValue = true))
        val obligation = RecoveryObligation()
        obligation.beforeWrite(true)
        obligation.afterWrite(true, saved = false)
        assertTrue(obligation.needed(persistedValue = false))
    }
}
