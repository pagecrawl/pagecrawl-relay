package io.pagecrawl.relay

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class RelayKeyTest {
    private val key = "Ab3" + "x".repeat(60) + "9"

    @Test
    fun readsTheKeyFromTheQrCodeTheWebShows() {
        assertEquals(key, RelayKey.parse(RelayKey.uriFor(key)))
    }

    // A key copied from an email or a message nearly always carries a newline or spaces.
    @Test
    fun acceptsAPastedKeyWithWhitespaceAroundIt() {
        assertEquals(key, RelayKey.parse("  $key\n"))
    }

    // Any QR code can end up in front of the camera; only a relay key may be taken as one.
    @Test
    fun refusesAQrCodeThatIsNotARelayKey() {
        assertNull(RelayKey.parse("https://pagecrawl.io/app"))
        assertNull(RelayKey.parse("WIFI:S:home;T:WPA;P:secret;;"))
        assertNull(RelayKey.parse("pagecrawl-relay://somewhere-else?token=$key"))
    }

    @Test
    fun refusesSomethingShapedWrong() {
        assertNull(RelayKey.parse(""))
        assertNull(RelayKey.parse(null))
        assertNull(RelayKey.parse(key.dropLast(1)))
        assertNull(RelayKey.parse(key + "0"))
        assertNull(RelayKey.parse(key.dropLast(1) + "-"))
        assertNull(RelayKey.parse(RelayKey.uriFor("short")))
    }

    @Test
    fun readsTheTokenWhateverOrderTheQueryIsIn() {
        assertEquals(key, RelayKey.parse("pagecrawl-relay://enrol?source=web&token=$key"))
    }
}
