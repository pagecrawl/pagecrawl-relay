package io.pagecrawl.relay

import android.content.Intent
import android.speech.RecognitionService
import android.speech.SpeechRecognizer

/** Android's assistant metadata requires this service. Relay never records or recognizes speech. */
class RelayRecognitionService : RecognitionService() {
    override fun onStartListening(recognizerIntent: Intent, listener: Callback) {
        listener.error(SpeechRecognizer.ERROR_CLIENT)
    }
    override fun onStopListening(listener: Callback) { listener.error(SpeechRecognizer.ERROR_CLIENT) }
    override fun onCancel(listener: Callback) = Unit
}
