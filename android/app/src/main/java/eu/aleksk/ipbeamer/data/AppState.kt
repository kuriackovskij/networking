package eu.aleksk.ipbeamer.data

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/** Live status shared between the service and the UI. */
object AppState {

    enum class Status { STOPPED, PENDING, ACKED, SENT_NO_ACK, ERROR }

    data class State(
        val running: Boolean = false,
        val status: Status = Status.STOPPED,
        val grantedIp: String? = null,
        val lastBeamMs: Long = 0L,
        val nextBeamMs: Long = 0L,
        val detail: String? = null,
    )

    private val _state = MutableStateFlow(State())
    val state: StateFlow<State> = _state

    fun update(block: (State) -> State) {
        _state.value = block(_state.value)
    }
}
