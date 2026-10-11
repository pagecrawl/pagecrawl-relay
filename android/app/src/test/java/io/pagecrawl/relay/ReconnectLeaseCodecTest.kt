package io.pagecrawl.relay

import java.time.Instant
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class ReconnectLeaseCodecTest {
    private val now = 1_000_000L
    private val id = "fe598cca-bd87-4b21-8f0a-25feff53c428"

    private fun response(expiry: Long = now + 180_000): JSONObject = JSONObject()
        .put("lease_id", id)
        .put("expires_at", Instant.ofEpochMilli(expiry).toString())
        .put("max_hold_seconds", 300)

    @Test fun acceptsAcknowledgedBoundedLease() {
        val lease = ReconnectLeaseCodec.acknowledge(response().toString(), now)
        assertNotNull(lease)
        assertEquals(id, lease!!.id)
        assertEquals(now + 180_000, lease.expiresAt)
        assertTrue(lease.storedForRecovery)
    }

    @Test fun expiredOrOverlongLeasesFailClosed() {
        for (expiry in listOf(now - 1, now, now + 300_001)) {
            assertNull(ReconnectLeaseCodec.acknowledge(response(expiry).toString(), now))
        }
        assertNotNull(ReconnectLeaseCodec.acknowledge(response(now + 300_000).toString(), now))
    }

    @Test fun requiresUuidAndIsoDateAcknowledgment() {
        for (value in listOf("", "not-a-lease", "1234", "$id-extra")) {
            assertNull(ReconnectLeaseCodec.acknowledge(response().put("lease_id", value).toString(), now))
        }
        assertNull(ReconnectLeaseCodec.acknowledge(response().put("expires_at", "tomorrow").toString(), now))
    }

    @Test fun missingAndIncorrectProtocolFieldsFailClosed() {
        for (field in listOf("lease_id", "expires_at", "max_hold_seconds")) {
            val response = response().also { it.remove(field) }
            assertNull(ReconnectLeaseCodec.acknowledge(response.toString(), now))
        }
        for (value in listOf<Any>("300", 300.5, true, 0, 600, JSONObject.NULL)) {
            assertNull(ReconnectLeaseCodec.acknowledge(response().put("max_hold_seconds", value).toString(), now))
        }
        assertNull(ReconnectLeaseCodec.acknowledge("not json", now))
        assertNull(ReconnectLeaseCodec.acknowledge("[]", now))
    }

    @Test fun acceptsBackendTimestampWithUtcOffset() {
        val body = response().put("expires_at", "1970-01-01T00:19:40.000000+00:00")
        assertNotNull(ReconnectLeaseCodec.acknowledge(body.toString(), now))
    }
}
