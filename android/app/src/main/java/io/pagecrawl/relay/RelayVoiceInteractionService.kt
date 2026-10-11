package io.pagecrawl.relay

import android.service.voice.VoiceInteractionService
import android.service.voice.VoiceInteractionSession
import android.content.ComponentName

/** Selected by the person in Android settings, never started as a normal app service. */
class RelayVoiceInteractionService : VoiceInteractionService() {
    private var destroyed = false
    override fun onReady() {
        super.onReady()
        if (destroyed || !isActiveService(this, ComponentName(this, RelayVoiceInteractionService::class.java))) return
        setDisabledShowContext(VoiceInteractionSession.SHOW_WITH_ASSIST or VoiceInteractionSession.SHOW_WITH_SCREENSHOT)
        AssistantReconnect.voiceReady(this)
    }

    override fun onShutdown() {
        destroyed = true
        AssistantReconnect.voiceUnavailable(this)
        super.onShutdown()
    }

    override fun onDestroy() {
        destroyed = true
        AssistantReconnect.voiceUnavailable(this)
        super.onDestroy()
    }
}
