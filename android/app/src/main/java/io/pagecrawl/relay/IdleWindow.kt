package io.pagecrawl.relay

/** Short connections between polls still reset the window through their activity revision. */
internal class IdleWindow(private val duration: Long) {
    private var quietSince: Long? = null
    private var revision: Long? = null

    fun remaining(now: Long, activity: RelayActivity?): Long {
        if (activity == null || activity.activeConnections < 0 || activity.revision < 0) {
            quietSince = null
            revision = null
            return duration
        }
        if (activity.activeConnections > 0) {
            quietSince = null
        } else if (quietSince == null || revision != activity.revision) {
            quietSince = now
        }
        revision = activity.revision
        return quietSince?.let { (duration - (now - it)).coerceIn(0, duration) } ?: duration
    }
}
