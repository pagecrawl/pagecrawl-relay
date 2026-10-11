package io.pagecrawl.relay

/** An observed OFF cannot settle an ON voice task which Android has not finished yet. */
internal class RadioRestoreLedger {
    private val pendingOn = mutableSetOf<String>()
    private val completedOn = mutableSetOf<String>()
    private var offAfterOn = false

    fun onDispatched(id: String) { pendingOn += id; offAfterOn = false }
    fun onFinished(id: String) { pendingOn -= id; completedOn += id }
    // Android destroys the session after removing its voice tasks. Still require a new OFF.
    fun onSessionDestroyed(id: String) { pendingOn -= id; completedOn += id; offAfterOn = false }
    fun onRejected(id: String) { pendingOn -= id }
    fun allOnSettled(): Boolean = pendingOn.isEmpty()
    fun offFinished(startedAfterAllOn: Boolean) {
        if (startedAfterAllOn && pendingOn.isEmpty()) offAfterOn = true
    }
    fun canClear(): Boolean = pendingOn.isEmpty() && (completedOn.isEmpty() || offAfterOn)
    fun reset() { pendingOn.clear(); completedOn.clear(); offAfterOn = false }
}
