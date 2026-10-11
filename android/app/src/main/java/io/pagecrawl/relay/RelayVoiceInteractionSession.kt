package io.pagecrawl.relay

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import android.service.voice.VoiceInteractionSession

class RelayVoiceInteractionSession(private val relayContext: Context) : VoiceInteractionSession(relayContext) {
    private var settingsPackage: String? = null
    override fun onPrepareShow(args: Bundle?, flags: Int) {
        super.onPrepareShow(args, flags)
        // Relay has its own screen and never reads the foreground app or takes screenshots.
        setUiEnabled(false)
    }

    override fun onShow(args: Bundle?, flags: Int) {
        super.onShow(args, flags)
        val id = args?.getString(AssistantReconnect.COMMAND_ID)
        val enabled = AssistantReconnect.commandFor(id, this)
        if (enabled == null) {
            // Invoking the assistant by gesture or another app cannot toggle radios.
            if (!AssistantReconnect.state.value.busy) finish()
            return
        }
        try {
            val intent = AssistantReconnect.voiceIntent(enabled).putExtra(AssistantReconnect.COMMAND_ID, id)
            settingsPackage = relayContext.packageManager.resolveActivity(intent, PackageManager.MATCH_DEFAULT_ONLY)
                ?.activityInfo?.packageName
            startVoiceActivity(intent)
        } catch (failure: Exception) {
            AssistantReconnect.commandFailed(id, failure)
        }
    }

    override fun onTaskFinished(intent: Intent, taskId: Int) {
        AssistantReconnect.commandFinished(intent.getStringExtra(AssistantReconnect.COMMAND_ID))
        // The default implementation finishes the session here. Keep it for the OFF task.
    }

    override fun onHide() {
        // Android can hide the session while showing the voice Settings activity.
        // A hide is not cancellation of the person's reconnect request.
        super.onHide()
    }

    override fun onCancelRequest(request: Request) {
        if (request.isActive) request.cancel()
        if (trustedSettingsRequest(request)) AssistantReconnect.cancel()
    }

    override fun onRequestCompleteVoice(request: CompleteVoiceRequest) {
        if (trustedSettingsRequest(request)) request.sendCompleteResult(Bundle()) else request.cancel()
    }

    override fun onRequestAbortVoice(request: AbortVoiceRequest) {
        if (trustedSettingsRequest(request)) {
            request.sendAbortResult(Bundle())
            AssistantReconnect.cancel()
        } else request.cancel()
    }

    private fun trustedSettingsRequest(request: Request): Boolean =
        settingsPackage != null && request.callingPackage == settingsPackage && AssistantReconnect.state.value.busy

    override fun onDestroy() {
        AssistantReconnect.sessionDestroyed(this)
        super.onDestroy()
    }
}
