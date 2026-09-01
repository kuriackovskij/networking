package eu.aleksk.ipbeamer.data

import android.content.Context
import android.content.SharedPreferences
import android.os.Build
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/**
 * App configuration, stored in EncryptedSharedPreferences so the shared secret
 * is encrypted at rest.
 */
class Settings(context: Context) {

    private val prefs: SharedPreferences = run {
        val master = MasterKey.Builder(context)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        EncryptedSharedPreferences.create(
            context,
            "ipbeamer_secure",
            master,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    }

    var host: String
        get() = prefs.getString("host", "") ?: ""
        set(v) = prefs.edit().putString("host", v.trim()).apply()

    var port: Int
        get() = prefs.getInt("port", 62201)
        set(v) = prefs.edit().putInt("port", v).apply()

    var password: String
        get() = prefs.getString("password", "") ?: ""
        set(v) = prefs.edit().putString("password", v).apply()

    /** Label sent with each beam; shown in the server logs. Defaults to the device model. */
    var nodeName: String
        get() = prefs.getString("node", "") ?: Build.MODEL
        set(v) = prefs.edit().putString("node", v).apply()

    /** How often to re-beam, in minutes (default 45, safely under the 60m server timeout). */
    var intervalMinutes: Int
        get() = prefs.getInt("interval_min", 45)
        set(v) = prefs.edit().putInt("interval_min", v.coerceIn(1, 59)).apply()

    /** Seconds to wait for the acknowledgement. */
    var ackWaitSeconds: Int
        get() = prefs.getInt("ack_wait_s", 3)
        set(v) = prefs.edit().putInt("ack_wait_s", v.coerceIn(1, 15)).apply()

    /** Whether beaming is currently switched on. */
    var enabled: Boolean
        get() = prefs.getBoolean("enabled", false)
        set(v) = prefs.edit().putBoolean("enabled", v).apply()

    /** Start automatically after the phone boots. */
    var autoStart: Boolean
        get() = prefs.getBoolean("autostart", true)
        set(v) = prefs.edit().putBoolean("autostart", v).apply()

    fun isConfigured(): Boolean = host.isNotEmpty() && password.isNotEmpty()
}
