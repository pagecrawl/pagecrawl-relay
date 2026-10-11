package io.pagecrawl.relay

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** Recovers only a persisted, previously requested reconnect. It never starts a new cycle. */
class AssistantRecoveryReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action in setOf(Intent.ACTION_LOCKED_BOOT_COMPLETED, Intent.ACTION_BOOT_COMPLETED, Intent.ACTION_MY_PACKAGE_REPLACED)) {
            AssistantReconnect.initialize(context)
        }
    }
}
