package io.pagecrawl.relay

import android.content.Context
import android.net.Network
import android.os.UserManager
import java.io.ByteArrayOutputStream
import java.net.URL
import java.time.OffsetDateTime
import java.util.concurrent.Executors
import java.util.concurrent.SynchronousQueue
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import javax.net.ssl.HttpsURLConnection
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeout
import org.json.JSONObject
import kotlin.coroutines.resume

/** Public response validation is independent of the Android HTTP and storage adapters. */
internal object ReconnectLeaseCodec {
    private val leaseId = Regex("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")

    fun validId(value: String): Boolean = leaseId.matches(value)

    fun acknowledge(body: String, now: Long): BackendReconnectLease? = runCatching {
        val response = JSONObject(body)
        val id = response.getString("lease_id").takeIf(::validId) ?: return null
        val expiry = OffsetDateTime.parse(response.getString("expires_at")).toInstant().toEpochMilli()
        val maximum = response.opt("max_hold_seconds")
        if (maximum !is Int || maximum != 300 || expiry <= now || expiry - now > 300_000) return null
        BackendReconnectLease(id, expiry)
    }.getOrNull()
}

/** Only the phone's validated mobile network may carry a reconnect request or its credential. */
internal class AndroidReconnectCoordinator(
    private val context: Context,
    private val cellular: () -> Network?,
) {
    fun pendingLease(): BackendReconnectLease? {
        val preferences = preferences()
        val id = runCatching { preferences.getString(LEASE_ID, null) }.getOrNull() ?: return null
        val expiry = runCatching { preferences.getLong(LEASE_EXPIRY, 0L) }.getOrDefault(0L)
        val now = System.currentTimeMillis()
        if (!ReconnectLeaseCodec.validId(id) || expiry <= now || expiry - now > 300_000) {
            clearLease(id)
            return null
        }
        return BackendReconnectLease(id, expiry)
    }

    suspend fun begin(holdSeconds: Int): BackendReconnectLease? {
        if (!context.getSystemService(UserManager::class.java).isUserUnlocked) return null
        val token = runCatching { KeyVault(context).read() }.getOrNull()?.let(RelayKey::parse) ?: return null
        val network = cellular() ?: return null
        val response = post(network, token, BEGIN_URL, JSONObject().put("hold_seconds", holdSeconds)) ?: return null
        if (response.code == 404) {
            error("PageCrawl needs the reconnect update before this control can run. Mobile data has not been interrupted.")
        }
        if (response.code != 200) return null
        val lease = ReconnectLeaseCodec.acknowledge(response.body, System.currentTimeMillis()) ?: return null
        val saved = runCatching {
            preferences().edit().putString(LEASE_ID, lease.id).putLong(LEASE_EXPIRY, lease.expiresAt).commit()
        }.getOrDefault(false)
        // The cycle releases a failed save without turning radios off.
        return lease.copy(storedForRecovery = saved)
    }

    suspend fun complete(lease: BackendReconnectLease): Boolean {
        if (!context.getSystemService(UserManager::class.java).isUserUnlocked) return false
        val token = runCatching { KeyVault(context).read() }.getOrNull()?.let(RelayKey::parse) ?: return false
        val network = cellular() ?: return false
        val response = post(network, token, COMPLETE_URL, JSONObject().put("lease_id", lease.id)) ?: return false
        if (response.code != 200 || runCatching { JSONObject(response.body).opt("ok") == true }.getOrDefault(false).not()) return false
        return clearLease(lease.id)
    }

    private fun preferences() = context.createDeviceProtectedStorageContext()
        .getSharedPreferences("assistant_recovery", Context.MODE_PRIVATE)

    private fun clearLease(id: String): Boolean = runCatching {
        val preferences = preferences()
        // A late cleanup can never erase the marker for a newer acknowledged pause.
        if (preferences.getString(LEASE_ID, null) != id) true
        else preferences.edit().remove(LEASE_ID).remove(LEASE_EXPIRY).commit()
    }.getOrDefault(false)

    private data class Reply(val code: Int, val body: String)

    private suspend fun post(network: Network, token: String, endpoint: String, body: JSONObject): Reply? = try {
        withTimeout(10_000) {
            suspendCancellableCoroutine { continuation ->
                val connection = AtomicReference<HttpsURLConnection?>()
                val task = runCatching {
                    workers.submit {
                        val reply = try {
                            val http = network.openConnection(URL(endpoint)) as HttpsURLConnection
                            connection.set(http)
                            if (continuation.isActive) blockingPost(http, token, body) else null
                        } catch (_: Exception) { null }
                        finally { runCatching { connection.get()?.disconnect() } }
                        // A late response after timeout cannot start a cycle or write a lease marker.
                        continuation.resume(reply)
                    }
                }.getOrNull()
                if (task == null) continuation.resume(null)
                else continuation.invokeOnCancellation {
                    task.cancel(true)
                    // Closing a blocking adapter never delays the caller's ten-second deadline.
                    deadlines.execute { runCatching { connection.get()?.disconnect() } }
                }
            }
        }
    } catch (_: Exception) { null }

    private fun blockingPost(connection: HttpsURLConnection, token: String, body: JSONObject): Reply {
        // Redirects must never receive the bearer credential.
        connection.instanceFollowRedirects = false
        connection.requestMethod = "POST"
        connection.connectTimeout = 4_000
        connection.readTimeout = 4_000
        connection.useCaches = false
        connection.doOutput = true
        connection.setRequestProperty("Authorization", "Bearer $token")
        connection.setRequestProperty("Accept", "application/json")
        connection.setRequestProperty("Content-Type", "application/json")
        val bytes = body.toString().toByteArray(Charsets.UTF_8)
        connection.setFixedLengthStreamingMode(bytes.size)
        connection.outputStream.use { it.write(bytes) }
        val code = connection.responseCode
        if (code != 200) return Reply(code, "")
        val bytesOut = ByteArrayOutputStream()
        connection.inputStream.use { input ->
            val buffer = ByteArray(1_024)
            while (true) {
                val count = input.read(buffer)
                if (count < 0) break
                check(bytesOut.size() + count <= 4_096) { "Invalid reconnect response." }
                bytesOut.write(buffer, 0, count)
            }
        }
        return Reply(code, bytesOut.toString(Charsets.UTF_8.name()))
    }

    private companion object {
        const val BEGIN_URL = "https://pagecrawl.io/api/relay-client/reconnect"
        const val COMPLETE_URL = "$BEGIN_URL/complete"
        const val LEASE_ID = "backend_lease_id"
        const val LEASE_EXPIRY = "backend_lease_expires_at"
        val deadlines = Executors.newSingleThreadScheduledExecutor { task ->
            Thread(task, "relay-reconnect-http-deadline").apply { isDaemon = true }
        }
        // A broken DNS or HTTP adapter cannot accumulate an unbounded set of request threads.
        val workers = ThreadPoolExecutor(0, 2, 30, TimeUnit.SECONDS, SynchronousQueue()) { task ->
            Thread(task, "relay-reconnect-http").apply { isDaemon = true }
        }
    }
}
