package eu.aleksk.ipbeamer.service

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import eu.aleksk.ipbeamer.data.LogStore
import eu.aleksk.ipbeamer.data.Settings

/** Auto-starts beaming after boot, if enabled and auto-start is on. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent?) {
        if (intent?.action != Intent.ACTION_BOOT_COMPLETED) return
        val s = Settings(context)
        if (s.enabled && s.autoStart && s.isConfigured()) {
            LogStore.log("boot: auto-starting")
            BeamService.start(context)
        }
    }
}
