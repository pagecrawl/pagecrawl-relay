package io.pagecrawl.relay

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/**
 * Brings the relay back after the phone restarts or the app updates, if the person had it
 * switched on. A relay that silently stays off after an overnight update is a relay nobody
 * notices has stopped until checks start failing.
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED && intent.action != Intent.ACTION_MY_PACKAGE_REPLACED) {
            return
        }

        if (RelaySettings(context).enabled && KeyVault(context).read() != null) {
            RelayService.start(context)
        }
    }
}
