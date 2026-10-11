package io.pagecrawl.relay

/** SharedPreferences can change its cached value despite a failed disk commit. */
internal class RecoveryObligation {
    private var outstanding = false
    fun beforeWrite(needed: Boolean) { if (needed) outstanding = true }
    fun afterWrite(needed: Boolean, saved: Boolean) { outstanding = needed || !saved }
    fun needed(persistedValue: Boolean): Boolean = outstanding || persistedValue
}
