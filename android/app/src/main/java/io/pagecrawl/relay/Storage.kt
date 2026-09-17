package io.pagecrawl.relay

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Keeps the relay key encrypted at rest.
 *
 * The key is a bearer credential: whoever holds it can carry checks for this relay. It is
 * encrypted with an AES key that lives in the Android Keystore, which never hands the AES
 * key to the app, so a copy of the app's files (a backup, a rooted file browser) holds only
 * ciphertext. Backup is also switched off for the app (see the manifest), and a Keystore key
 * does not travel to another phone anyway, so a restored copy decrypts to nothing and the
 * app asks for the key again instead of failing strangely.
 */
class KeyVault(context: Context) {
    private val prefs = context.getSharedPreferences("relay_key", Context.MODE_PRIVATE)

    fun read(): String? {
        val data = prefs.getString(DATA, null) ?: return null
        val iv = prefs.getString(IV, null) ?: return null

        return runCatching {
            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(Cipher.DECRYPT_MODE, secretKey(), GCMParameterSpec(128, decode(iv)))
            String(cipher.doFinal(decode(data)), Charsets.UTF_8)
        }.getOrNull()
    }

    fun write(token: String) {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        val data = cipher.doFinal(token.toByteArray(Charsets.UTF_8))

        prefs.edit()
            .putString(DATA, encode(data))
            .putString(IV, encode(cipher.iv))
            .apply()
    }

    fun clear() {
        prefs.edit().clear().apply()
    }

    private fun secretKey(): SecretKey {
        val store = KeyStore.getInstance(KEYSTORE).apply { load(null) }
        (store.getEntry(ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }

        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )

        return generator.generateKey()
    }

    private fun encode(bytes: ByteArray) = Base64.encodeToString(bytes, Base64.NO_WRAP)

    private fun decode(text: String) = Base64.decode(text, Base64.NO_WRAP)

    private companion object {
        const val KEYSTORE = "AndroidKeyStore"
        const val ALIAS = "pagecrawl-relay-key"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val DATA = "data"
        const val IV = "iv"
    }
}

/** The two choices the person makes: whether the relay runs, and whether it may use mobile data. */
class RelaySettings(context: Context) {
    private val prefs = context.getSharedPreferences("relay_settings", Context.MODE_PRIVATE)

    var enabled: Boolean
        get() = prefs.getBoolean(ENABLED, false)
        set(value) = prefs.edit().putBoolean(ENABLED, value).apply()

    var allowMobileData: Boolean
        get() = prefs.getBoolean(MOBILE_DATA, false)
        set(value) = prefs.edit().putBoolean(MOBILE_DATA, value).apply()

    private companion object {
        const val ENABLED = "enabled"
        const val MOBILE_DATA = "allow_mobile_data"
    }
}
