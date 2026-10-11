package io.pagecrawl.relay

import org.junit.Assert.assertEquals
import org.junit.Test

class IdleWindowTest {
    @Test fun needsAnUninterruptedQuietWindow() {
        val window = IdleWindow(5_000)
        assertEquals(5_000L, window.remaining(0, RelayActivity(0, 0)))
        assertEquals(1_000L, window.remaining(4_000, RelayActivity(0, 0)))
        assertEquals(5_000L, window.remaining(4_500, RelayActivity(0, 2)))
        assertEquals(0L, window.remaining(9_500, RelayActivity(0, 2)))
    }

    @Test fun activeConnectionsRestartTheWindowEvenWithoutTraffic() {
        val window = IdleWindow(5_000)
        window.remaining(0, RelayActivity(0, 0))
        assertEquals(5_000L, window.remaining(4_000, RelayActivity(1, 1)))
        assertEquals(5_000L, window.remaining(10_000, RelayActivity(0, 2)))
        assertEquals(0L, window.remaining(15_000, RelayActivity(0, 2)))
    }

    @Test fun missingOrInvalidStatusNeverCountsAsIdle() {
        for (activity in listOf(null, RelayActivity(-1, 0), RelayActivity(0, -1))) {
            val window = IdleWindow(5_000)
            window.remaining(0, RelayActivity(0, 0))
            assertEquals(5_000L, window.remaining(4_000, activity))
            assertEquals(5_000L, window.remaining(8_000, RelayActivity(0, 0)))
        }
    }
}
