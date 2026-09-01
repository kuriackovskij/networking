package eu.aleksk.ipbeamer.ui

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings as AndroidSettings
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import eu.aleksk.ipbeamer.data.AppState
import eu.aleksk.ipbeamer.data.LogStore
import eu.aleksk.ipbeamer.data.Settings
import eu.aleksk.ipbeamer.service.BeamService
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

class MainActivity : ComponentActivity() {

    private val requestNotif =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            requestNotif.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        setContent { IpBeamerTheme { MainScreen() } }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun MainScreen() {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val settings = remember { Settings(ctx) }

    var host by remember { mutableStateOf(settings.host) }
    var port by remember { mutableStateOf(settings.port.toString()) }
    var password by remember { mutableStateOf(settings.password) }
    var node by remember { mutableStateOf(settings.nodeName) }
    var interval by remember { mutableStateOf(settings.intervalMinutes.toString()) }
    var ackWait by remember { mutableStateOf(settings.ackWaitSeconds.toString()) }
    var autoStart by remember { mutableStateOf(settings.autoStart) }

    val state by AppState.state.collectAsStateWithLifecycle()
    val logs by LogStore.lines.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    val scroll = rememberScrollState()

    // Notification permission drives whether the status-bar icon can show.
    var notifGranted by remember { mutableStateOf(hasNotifPermission(ctx)) }
    val notifLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { notifGranted = it }
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val obs = LifecycleEventObserver { _, e ->
            if (e == Lifecycle.Event.ON_RESUME) notifGranted = hasNotifPermission(ctx)
        }
        lifecycleOwner.lifecycle.addObserver(obs)
        onDispose { lifecycleOwner.lifecycle.removeObserver(obs) }
    }

    fun persist() {
        settings.host = host
        settings.port = port.toIntOrNull() ?: 62201
        settings.password = password
        settings.nodeName = node
        settings.intervalMinutes = interval.toIntOrNull() ?: 45
        settings.ackWaitSeconds = ackWait.toIntOrNull() ?: 3
        settings.autoStart = autoStart
    }

    Scaffold(
        topBar = { TopAppBar(title = { Text("IP-Beamer") }) },
        snackbarHost = { SnackbarHost(snackbar) },
    ) { pad ->
        Column(
            Modifier.padding(pad).padding(16.dp).verticalScroll(scroll),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            if (!notifGranted) {
                NotificationCard(
                    onEnable = { notifLauncher.launch(Manifest.permission.POST_NOTIFICATIONS) },
                    onSettings = { openAppNotifSettings(ctx) },
                )
            }

            StatusCard(state)

            ElevatedCard {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text("Server", style = MaterialTheme.typography.titleMedium)
                    OutlinedTextField(host, { host = it }, label = { Text("Host") },
                        singleLine = true, modifier = Modifier.fillMaxWidth())
                    OutlinedTextField(port, { port = it.filter(Char::isDigit) },
                        label = { Text("Port") }, singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth())
                    OutlinedTextField(password, { password = it }, label = { Text("Password") },
                        singleLine = true, visualTransformation = PasswordVisualTransformation(),
                        modifier = Modifier.fillMaxWidth())
                    OutlinedTextField(node, { node = it }, label = { Text("Device name (label)") },
                        singleLine = true, modifier = Modifier.fillMaxWidth())
                }
            }

            ElevatedCard {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text("Timing", style = MaterialTheme.typography.titleMedium)
                    OutlinedTextField(interval, { interval = it.filter(Char::isDigit) },
                        label = { Text("Beam every (minutes, < 60)") }, singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth())
                    OutlinedTextField(ackWait, { ackWait = it.filter(Char::isDigit) },
                        label = { Text("Wait for ack (seconds)") }, singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth())
                    RowSwitch("Start automatically on boot", autoStart) { autoStart = it }
                }
            }

            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Button(
                    onClick = {
                        persist()
                        settings.enabled = true
                        BeamService.start(ctx)
                        if (!notifGranted) {
                            notifLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
                        }
                    },
                    modifier = Modifier.weight(1f),
                    enabled = host.isNotBlank() && password.isNotBlank(),
                ) { Text(if (state.running) "Beam now" else "Start") }

                OutlinedButton(
                    onClick = {
                        settings.enabled = false
                        BeamService.stop(ctx)
                    },
                    modifier = Modifier.weight(1f),
                    enabled = state.running,
                ) { Text("Stop") }
            }

            // Settings are saved on Start; this saves without (re)starting.
            TextButton(onClick = {
                persist()
                scope.launch { snackbar.showSnackbar("Settings saved") }
            }) { Text("Save settings") }

            BatteryCard(ctx)

            LogsCard(logs)
        }
    }
}

@Composable
private fun StatusCard(state: AppState.State) {
    val fmt = remember { SimpleDateFormat("HH:mm:ss", Locale.US) }
    val (label, tone) = when (state.status) {
        AppState.Status.ACKED -> "Access active (acknowledged)" to MaterialTheme.colorScheme.primary
        AppState.Status.SENT_NO_ACK -> "Beam sent — no acknowledgement" to MaterialTheme.colorScheme.tertiary
        AppState.Status.PENDING -> "Beaming…" to MaterialTheme.colorScheme.tertiary
        AppState.Status.ERROR -> "Problem — see logs" to MaterialTheme.colorScheme.error
        AppState.Status.STOPPED -> "Stopped" to MaterialTheme.colorScheme.onSurfaceVariant
    }
    ElevatedCard {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(label, style = MaterialTheme.typography.titleMedium, color = tone)
            state.grantedIp?.let { Text("Allowed IP: $it") }
            if (state.lastBeamMs > 0) Text("Last beam: ${fmt.format(Date(state.lastBeamMs))}")
            if (state.nextBeamMs > 0 && state.running) Text("Next beam: ${fmt.format(Date(state.nextBeamMs))}")
            state.detail?.let { Text(it, style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error) }
        }
    }
}

@Composable
private fun NotificationCard(onEnable: () -> Unit, onSettings: () -> Unit) {
    ElevatedCard {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Notifications are off", style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.error)
            Text("Android needs notification permission to show the IP-Beamer icon in the " +
                "status bar while it's running.", style = MaterialTheme.typography.bodySmall)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = onEnable) { Text("Enable") }
                TextButton(onClick = onSettings) { Text("Open settings") }
            }
        }
    }
}

@Composable
private fun BatteryCard(ctx: Context) {
    ElevatedCard {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Reliability", style = MaterialTheme.typography.titleMedium)
            Text("For beams to fire on time, exempt IP-Beamer from battery optimisation.",
                style = MaterialTheme.typography.bodySmall)
            OutlinedButton(onClick = { requestBatteryExemption(ctx) }) {
                Text("Ignore battery optimisation")
            }
        }
    }
}

@Composable
private fun LogsCard(logs: List<String>) {
    ElevatedCard {
        Column(Modifier.padding(16.dp)) {
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("Logs", style = MaterialTheme.typography.titleMedium)
                TextButton(onClick = { LogStore.clear() }) { Text("Clear") }
            }
            if (logs.isEmpty()) {
                Text("No activity yet.", style = MaterialTheme.typography.bodySmall)
            } else {
                logs.asReversed().take(60).forEach {
                    Text(it, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
    }
}

@Composable
private fun RowSwitch(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label)
        Switch(checked = checked, onCheckedChange = onChange)
    }
}

private fun hasNotifPermission(ctx: Context): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
        ContextCompat.checkSelfPermission(ctx, Manifest.permission.POST_NOTIFICATIONS) ==
        PackageManager.PERMISSION_GRANTED

private fun openAppNotifSettings(ctx: Context) {
    val i = Intent(AndroidSettings.ACTION_APP_NOTIFICATION_SETTINGS)
        .putExtra(AndroidSettings.EXTRA_APP_PACKAGE, ctx.packageName)
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    ctx.startActivity(i)
}

private fun requestBatteryExemption(ctx: Context) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.M) return
    val pm = ctx.getSystemService(Context.POWER_SERVICE) as PowerManager
    if (pm.isIgnoringBatteryOptimizations(ctx.packageName)) return
    val i = Intent(AndroidSettings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
        Uri.parse("package:${ctx.packageName}"))
    i.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    ctx.startActivity(i)
}
