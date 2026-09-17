package io.pagecrawl.relay

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RelayStatusTest {
    // The shape relay.Snapshot marshals to (relay/state.go).
    private val snapshot = """
        {"connected":true,"paused":false,"uptime":"3m","connected_for":"2m","bytes":2048,
         "traffic":"2.0 KB","connections":3,"exit_ip":"","last_host":"example.com",
         "last_error":"","recent":[
           {"at":"10:01:02","host":"example.com","port":443,"allowed":true},
           {"at":"10:01:01","host":"192.168.1.1","port":80,"allowed":false,
            "reason":"destination refused by policy"}]}
    """.trimIndent()

    @Test
    fun readsTheSnapshotTheGoLibraryReturns() {
        val status = RelayStatus.fromJson(snapshot)

        assertTrue(status.connected)
        assertEquals("2.0 KB", status.traffic)
        assertEquals(3, status.connections)
        assertEquals("2m", status.connectedFor)
        assertEquals(2, status.recent.size)
        assertEquals("example.com", status.recent[0].host)
        assertFalse(status.recent[1].allowed)
        assertEquals("destination refused by policy", status.recent[1].reason)
    }

    @Test
    fun treatsUnreadableStatusAsNotConnected() {
        assertFalse(RelayStatus.fromJson("not json").connected)
        assertFalse(RelayStatus.fromJson(null).connected)
    }

    @Test
    fun readsSelfCheckResults() {
        val checks = SelfCheck.listFromJson(
            """[{"name":"DNS","ok":true,"detail":"resolves"},
                {"name":"Gateway accepted this machine","ok":false,"detail":"rejected",
                 "fix":"Enrol it again."}]""",
        )

        assertEquals(2, checks.size)
        assertTrue(checks[0].ok)
        assertEquals("Enrol it again.", checks[1].fix)
    }

    // A refused key never recovers by retrying, so it must not be worded as a retry.
    @Test
    fun wordsARejectedKeyAsSomethingToFixNotSomethingToWaitFor() {
        val status = RelayStatus.fromJson(
            """{"connected":false,"last_error":"websocket: close 4003: Unauthorized","rejected":true}""",
        )

        assertTrue(status.rejected)
        assertEquals("Key not accepted", RelayService.headline(NetworkPolicy.Decision.RUN, status))
        assertFalse(RelayService.detail(NetworkPolicy.Decision.RUN, status).startsWith("Retrying"))
    }

    @Test
    fun stillWordsAnOrdinaryDisconnectionAsARetry() {
        val status = RelayStatus.fromJson("""{"connected":false,"last_error":"connection reset"}""")

        assertFalse(status.rejected)
        assertEquals("Could not connect", RelayService.headline(NetworkPolicy.Decision.RUN, status))
        assertTrue(RelayService.detail(NetworkPolicy.Decision.RUN, status).startsWith("Retrying"))
    }
}
