package eu.aleksk.ipbeamer.beam

import eu.aleksk.ipbeamer.protocol.Protocol
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetSocketAddress

/** Outcome of a single beam attempt. */
sealed class BeamResult {
    data class Acked(val grantedIp: String) : BeamResult()
    data object SentNoAck : BeamResult()
    data class Error(val message: String) : BeamResult()
}

/**
 * Sends one authenticated beam and waits briefly for the signed acknowledgement.
 * Blocking — must be called off the main thread.
 */
object BeamClient {
    fun beam(host: String, port: Int, password: String, nodeName: String, waitMs: Int): BeamResult {
        return try {
            val key = Protocol.deriveKey(password)
            val nonce = Protocol.newNonce()
            val now = System.currentTimeMillis() / 1000
            val msg = Protocol.encodeBeam(key, nonce, nodeName, now)

            DatagramSocket().use { sock ->
                sock.soTimeout = waitMs
                sock.connect(InetSocketAddress(host, port))
                sock.send(DatagramPacket(msg, msg.size))

                val buf = ByteArray(1500)
                val resp = DatagramPacket(buf, buf.size)
                try {
                    sock.receive(resp)
                    val ip = Protocol.decodeAck(buf.copyOf(resp.length), key, nonce)
                    if (ip != null) BeamResult.Acked(ip) else BeamResult.SentNoAck
                } catch (_: java.net.SocketTimeoutException) {
                    BeamResult.SentNoAck
                }
            }
        } catch (_: java.net.PortUnreachableException) {
            BeamResult.Error("no server on $host:$port (is ipbeamd running?)")
        } catch (e: java.net.UnknownHostException) {
            BeamResult.Error("cannot resolve host \"$host\"")
        } catch (e: Exception) {
            BeamResult.Error(e.message ?: e.toString())
        }
    }
}
