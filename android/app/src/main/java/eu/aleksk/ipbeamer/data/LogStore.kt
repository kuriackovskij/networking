package eu.aleksk.ipbeamer.data

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/** A small in-memory ring buffer of log lines, surfaced to the UI. */
object LogStore {
    private const val MAX = 300
    private val fmt = SimpleDateFormat("MM-dd HH:mm:ss", Locale.US)

    private val _lines = MutableStateFlow<List<String>>(emptyList())
    val lines: StateFlow<List<String>> = _lines

    @Synchronized
    fun log(message: String) {
        val entry = "${fmt.format(Date())}  $message"
        _lines.value = (_lines.value + entry).takeLast(MAX)
    }

    fun clear() {
        _lines.value = emptyList()
    }
}
