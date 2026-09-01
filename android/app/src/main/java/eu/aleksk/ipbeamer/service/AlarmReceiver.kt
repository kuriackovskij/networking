package eu.aleksk.ipbeamer.service

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import eu.aleksk.ipbeamer.data.LogStore

/** Fired by the scheduled alarm; nudges the service to send the next beam. */
class AlarmReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent?) {
        try {
            BeamService.beam(context)
        } catch (e: Exception) {
            LogStore.log("alarm could not start service: ${e.message}")
        }
    }
}
