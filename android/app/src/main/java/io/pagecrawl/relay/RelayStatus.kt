package io.pagecrawl.relay

import org.json.JSONArray
import org.json.JSONObject

/** The relay's live status, read from the JSON the Go library returns (relay.Snapshot). */
data class RelayStatus(
    val connected: Boolean = false,
    val traffic: String = "0 B",
    val connections: Int = 0,
    val connectedFor: String = "",
    val lastError: String = "",
    /** PageCrawl refused the key. Retrying cannot fix that; a new key can. */
    val rejected: Boolean = false,
    val recent: List<Destination> = emptyList(),
) {
    data class Destination(
        val at: String,
        val host: String,
        val port: Int,
        val allowed: Boolean,
        val reason: String,
    )

    companion object {
        /** Anything unreadable is treated as a relay that has not connected yet. */
        fun fromJson(json: String?): RelayStatus {
            val obj = runCatching { JSONObject(json.orEmpty()) }.getOrNull() ?: return RelayStatus()

            return RelayStatus(
                connected = obj.optBoolean("connected"),
                traffic = obj.optString("traffic", "0 B"),
                connections = obj.optInt("connections"),
                connectedFor = obj.optString("connected_for"),
                lastError = obj.optString("last_error"),
                rejected = obj.optBoolean("rejected"),
                recent = destinations(obj.optJSONArray("recent")),
            )
        }

        private fun destinations(array: JSONArray?): List<Destination> {
            if (array == null) {
                return emptyList()
            }

            return (0 until array.length()).mapNotNull { index ->
                array.optJSONObject(index)?.let {
                    Destination(
                        at = it.optString("at"),
                        host = it.optString("host"),
                        port = it.optInt("port"),
                        allowed = it.optBoolean("allowed"),
                        reason = it.optString("reason"),
                    )
                }
            }
        }
    }
}

/** One self-check result, read from the JSON the Go library returns (relay.CheckResult). */
data class SelfCheck(val name: String, val ok: Boolean, val detail: String, val fix: String) {
    companion object {
        fun listFromJson(json: String?): List<SelfCheck> {
            val array = runCatching { JSONArray(json.orEmpty()) }.getOrNull() ?: return emptyList()

            return (0 until array.length()).mapNotNull { index ->
                array.optJSONObject(index)?.let {
                    SelfCheck(
                        name = it.optString("name"),
                        ok = it.optBoolean("ok"),
                        detail = it.optString("detail"),
                        fix = it.optString("fix"),
                    )
                }
            }
        }
    }
}
