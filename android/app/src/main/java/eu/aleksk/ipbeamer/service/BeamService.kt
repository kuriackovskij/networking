package eu.aleksk.ipbeamer.service

import android.app.AlarmManager
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import eu.aleksk.ipbeamer.R
import eu.aleksk.ipbeamer.beam.BeamClient
import eu.aleksk.ipbeamer.beam.BeamResult
import eu.aleksk.ipbeamer.data.AppState
import eu.aleksk.ipbeamer.data.LogStore
import eu.aleksk.ipbeamer.data.Settings
import eu.aleksk.ipbeamer.ui.MainActivity
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * Foreground service that sends a beam, shows an ongoing status-bar notification,
 * and schedules the next beam with an (idle-friendly) alarm. It does not spin: it
 * beams, updates the notification, sets one alarm, and idles — so it is very
 * battery-light while still keeping the status-bar icon visible.
 */
class BeamService : Service() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val timeFmt = SimpleDateFormat("HH:mm", Locale.US)

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        ensureChannel()
        // START_STICKY re-delivers a null intent after a kill — treat that as START.
        when (intent?.action ?: ACTION_START) {
            ACTION_STOP -> {
                stop()
                return START_NOT_STICKY
            }
            else -> {
                startForegroundNow()
                beamAndReschedule()
            }
        }
        return START_STICKY
    }

    private fun startForegroundNow() {
        val notif = buildNotification(AppState.state.value)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            ServiceCompat.startForeground(
                this, NOTIF_ID, notif,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE,
            )
        } else {
            startForeground(NOTIF_ID, notif)
        }
        AppState.update { it.copy(running = true) }
    }

    private fun beamAndReschedule() {
        val s = Settings(this)
        if (!s.isConfigured()) {
            LogStore.log("not configured — set server and password")
            AppState.update { it.copy(status = AppState.Status.ERROR, detail = "not configured") }
            updateNotification()
            return
        }
        AppState.update { it.copy(status = AppState.Status.PENDING) }
        updateNotification()

        scope.launch {
            val result = BeamClient.beam(s.host, s.port, s.password, s.nodeName, s.ackWaitSeconds * 1000)
            val now = System.currentTimeMillis()
            val next = now + s.intervalMinutes * 60_000L
            when (result) {
                is BeamResult.Acked -> {
                    LogStore.log("acked — allowed ${result.grantedIp}")
                    AppState.update {
                        it.copy(status = AppState.Status.ACKED, grantedIp = result.grantedIp,
                            lastBeamMs = now, nextBeamMs = next, detail = null)
                    }
                }
                is BeamResult.SentNoAck -> {
                    LogStore.log("beam sent, no acknowledgement")
                    AppState.update {
                        it.copy(status = AppState.Status.SENT_NO_ACK, lastBeamMs = now,
                            nextBeamMs = next, detail = null)
                    }
                }
                is BeamResult.Error -> {
                    LogStore.log("error: ${result.message}")
                    AppState.update {
                        it.copy(status = AppState.Status.ERROR, lastBeamMs = now,
                            nextBeamMs = next, detail = result.message)
                    }
                }
            }
            scheduleNext(next)
            updateNotification()
        }
    }

    private fun scheduleNext(atMs: Long) {
        val am = getSystemService(Context.ALARM_SERVICE) as AlarmManager
        val pi = alarmIntent()
        am.cancel(pi)
        // Prefer exact + allow-while-idle so beams fire on time even in Doze, and
        // so the alarm counts as an allowed reason to (re)start the service. Fall
        // back to inexact if the OS won't grant exact alarms.
        val canExact = Build.VERSION.SDK_INT < Build.VERSION_CODES.S || am.canScheduleExactAlarms()
        when {
            Build.VERSION.SDK_INT >= Build.VERSION_CODES.M && canExact ->
                am.setExactAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, atMs, pi)
            Build.VERSION.SDK_INT >= Build.VERSION_CODES.M ->
                am.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, atMs, pi)
            else ->
                am.setExact(AlarmManager.RTC_WAKEUP, atMs, pi)
        }
    }

    private fun stop() {
        (getSystemService(Context.ALARM_SERVICE) as AlarmManager).cancel(alarmIntent())
        AppState.update { it.copy(running = false, status = AppState.Status.STOPPED, nextBeamMs = 0) }
        LogStore.log("stopped")
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private fun alarmIntent(): PendingIntent {
        val i = Intent(this, AlarmReceiver::class.java)
        val flags = PendingIntent.FLAG_UPDATE_CURRENT or
            (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) PendingIntent.FLAG_IMMUTABLE else 0)
        return PendingIntent.getBroadcast(this, 0, i, flags)
    }

    private fun updateNotification() {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(NOTIF_ID, buildNotification(AppState.state.value))
    }

    private fun buildNotification(state: AppState.State): android.app.Notification {
        val (title, text) = when (state.status) {
            AppState.Status.ACKED -> "Access active" to
                "Allowed ${state.grantedIp ?: "?"} · next ${timeFmt.format(Date(state.nextBeamMs))}"
            AppState.Status.SENT_NO_ACK -> "Beam sent (no ack)" to
                "next ${timeFmt.format(Date(state.nextBeamMs))}"
            AppState.Status.PENDING -> "Beaming…" to "contacting server"
            AppState.Status.ERROR -> "Problem" to (state.detail ?: "see logs")
            AppState.Status.STOPPED -> "Stopped" to ""
        }
        val tap = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or
                (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) PendingIntent.FLAG_IMMUTABLE else 0),
        )
        return NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_stat_beam)
            .setContentTitle(title)
            .setContentText(text)
            .setOngoing(true)
            .setShowWhen(false)
            .setContentIntent(tap)
            .setOnlyAlertOnce(true) // alert once on start; silent on the periodic updates
            .setPriority(NotificationCompat.PRIORITY_DEFAULT)
            .build()
    }

    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            // Remove the old low-importance channel so the notification no longer
            // lands under "Silent". A channel's importance can't be raised after
            // creation, so we recreate under a new id at DEFAULT importance.
            nm.deleteNotificationChannel(OLD_CHANNEL)
            if (nm.getNotificationChannel(CHANNEL) == null) {
                nm.createNotificationChannel(
                    NotificationChannel(CHANNEL, "IP-Beamer status", NotificationManager.IMPORTANCE_DEFAULT)
                        .apply { description = "Ongoing beam status" },
                )
            }
        }
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    // ACTION_BEAM is handled the same as START (foreground + beam + reschedule).

    companion object {
        const val ACTION_START = "eu.aleksk.ipbeamer.START"
        const val ACTION_STOP = "eu.aleksk.ipbeamer.STOP"
        const val ACTION_BEAM = "eu.aleksk.ipbeamer.BEAM"
        private const val CHANNEL = "ipbeamer_status_v2"
        private const val OLD_CHANNEL = "ipbeamer_status"
        private const val NOTIF_ID = 1

        fun start(ctx: Context) =
            androidx.core.content.ContextCompat.startForegroundService(
                ctx, Intent(ctx, BeamService::class.java).setAction(ACTION_START))

        fun beam(ctx: Context) =
            androidx.core.content.ContextCompat.startForegroundService(
                ctx, Intent(ctx, BeamService::class.java).setAction(ACTION_BEAM))

        fun stop(ctx: Context) =
            ctx.startService(Intent(ctx, BeamService::class.java).setAction(ACTION_STOP))
    }
}
