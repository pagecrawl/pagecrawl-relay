package io.pagecrawl.relay

import java.net.URI
import java.net.URLDecoder

/**
 * Reads a relay key out of what the person scanned or pasted.
 *
 * A key is the 64-character token PageCrawl shows once under Settings, Relays, Add machine.
 * The QR code there carries it as `pagecrawl-relay://enrol?token=...`, so a scanned code is
 * recognisable as a relay key rather than any QR code that happened to be in front of the
 * camera. A pasted key is usually the bare token, often with whitespace from wherever it was
 * copied, and is accepted as that too.
 */
object RelayKey {
    private const val SCHEME = "pagecrawl-relay"
    private val TOKEN = Regex("^[A-Za-z0-9]{64}$")

    /** The key, or null when [input] is not one. */
    fun parse(input: String?): String? {
        val text = input?.trim().orEmpty()
        if (text.isEmpty()) {
            return null
        }

        if (text.startsWith("$SCHEME:", ignoreCase = true)) {
            return fromUri(text)
        }

        return text.takeIf { TOKEN.matches(it) }
    }

    /** The QR payload for [token]: what the web shows and what [parse] reads back. */
    fun uriFor(token: String): String = "$SCHEME://enrol?token=$token"

    private fun fromUri(text: String): String? {
        val uri = runCatching { URI(text) }.getOrNull() ?: return null
        if (!uri.scheme.equals(SCHEME, ignoreCase = true) || uri.host != "enrol") {
            return null
        }

        val token = uri.rawQuery
            ?.split("&")
            ?.map { it.split("=", limit = 2) }
            ?.firstOrNull { it.size == 2 && it[0] == "token" }
            ?.let { URLDecoder.decode(it[1], Charsets.UTF_8.name()) }
            ?.trim()

        return token?.takeIf { TOKEN.matches(it) }
    }
}
