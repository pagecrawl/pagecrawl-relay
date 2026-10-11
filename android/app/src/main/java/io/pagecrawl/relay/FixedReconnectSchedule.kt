package io.pagecrawl.relay

/** A due time is permission to try once, rather than a backlog of missed reconnects. */
internal data class FixedReconnectSchedule(
    val enabled: Boolean = false,
    val intervalMinutes: Int = DEFAULT_INTERVAL_MINUTES,
    val nextAttemptAt: Long? = null,
    val waitingReason: String? = null,
    val scheduledBusy: Boolean = false,
) {
    init { require(intervalMinutes in MIN_INTERVAL_MINUTES..MAX_INTERVAL_MINUTES) }

    fun configure(enabled: Boolean, intervalMinutes: Int, now: Long): FixedReconnectSchedule {
        require(intervalMinutes in MIN_INTERVAL_MINUTES..MAX_INTERVAL_MINUTES)
        if (enabled == this.enabled && intervalMinutes == this.intervalMinutes) return this
        val configured = copy(enabled = enabled, intervalMinutes = intervalMinutes, waitingReason = null)
        return configured.copy(nextAttemptAt = if (enabled) configured.deadline(now) else null)
    }

    fun poll(now: Long, blockedReason: String?): Attempt {
        if (!enabled || scheduledBusy) return Attempt(this, false)
        // A clock moved backwards must not postpone an enabled schedule indefinitely.
        val maximum = deadline(now)
        val due = nextAttemptAt?.takeIf { it > 0 && it <= maximum } ?: maximum
        if (now < due) return Attempt(copy(nextAttemptAt = due, waitingReason = null), false)
        if (blockedReason != null) return Attempt(copy(nextAttemptAt = due, waitingReason = blockedReason), false)
        // Consume the occurrence before dispatch. A restart cannot replay missed intervals.
        return Attempt(copy(nextAttemptAt = maximum, waitingReason = null, scheduledBusy = true), true)
    }

    fun finished(now: Long): FixedReconnectSchedule = copy(
        nextAttemptAt = if (enabled) deadline(now) else null,
        waitingReason = null,
        scheduledBusy = false,
    )

    fun blocked(reason: String): FixedReconnectSchedule = copy(waitingReason = reason)

    private fun deadline(now: Long): Long {
        val duration = intervalMinutes * 60_000L
        return now.coerceAtMost(Long.MAX_VALUE - duration) + duration
    }

    data class Attempt(val schedule: FixedReconnectSchedule, val start: Boolean)

    companion object {
        const val DEFAULT_INTERVAL_MINUTES = 30
        const val MIN_INTERVAL_MINUTES = 5
        const val MAX_INTERVAL_MINUTES = 1_440

        fun restore(enabled: Boolean, intervalMinutes: Int, nextAttemptAt: Long?, now: Long): FixedReconnectSchedule {
            val interval = intervalMinutes.takeIf { it in MIN_INTERVAL_MINUTES..MAX_INTERVAL_MINUTES }
                ?: DEFAULT_INTERVAL_MINUTES
            val schedule = FixedReconnectSchedule(enabled, interval)
            if (!enabled) return schedule
            val maximum = schedule.deadline(now)
            return schedule.copy(nextAttemptAt = nextAttemptAt?.takeIf { it > 0 && it <= maximum } ?: maximum)
        }
    }
}

/** Eligibility is separate from elapsed time and is checked again by the reconnect cycle. */
internal data class ScheduledReconnectEligibility(
    val relayEnabled: Boolean,
    val mobileDataAllowed: Boolean,
    val relayRunning: Boolean,
    val networkAllowed: Boolean,
    val connected: Boolean,
    val reconnectBusy: Boolean,
    val recoveryRequired: Boolean,
    val readinessError: String?,
) {
    fun blockedReason(): String? = when {
        recoveryRequired -> "Complete reconnect recovery before automatic reconnects can continue."
        reconnectBusy -> "Waiting for the current reconnect to finish."
        !relayEnabled || !relayRunning -> "Turn Relay checks on to run automatic reconnects."
        !mobileDataAllowed -> "Enable Also use mobile data to run automatic reconnects."
        readinessError != null -> readinessError
        !networkAllowed -> "Waiting for a mobile network Relay may use."
        !connected -> "Waiting for Relay to connect before automatically reconnecting mobile data."
        else -> null
    }
}
