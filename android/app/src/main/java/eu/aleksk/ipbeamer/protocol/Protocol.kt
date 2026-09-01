package eu.aleksk.ipbeamer.protocol

import java.nio.ByteBuffer
import java.security.SecureRandom
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/**
 * IP-Beamer wire protocol — must stay byte-compatible with the Go server
 * (internal/protocol) and PROTOCOL.md. All integers are big-endian, which is
 * what java.nio.ByteBuffer uses by default.
 */
object Protocol {
    private const val MAGIC = "IPB1"
    private const val ACK_MAGIC = "IPBA"
    private const val VERSION = 1

    const val NONCE_LEN = 16
    private const val HMAC_LEN = 32
    private const val MAX_NAME = 32

    // Same salt, iterations and length as the Go DeriveKey, so an identical
    // password produces an identical key.
    private val SALT = "ip-beamer/v1/pbkdf2-hmac-sha256".toByteArray(Charsets.US_ASCII)
    private const val ITERATIONS = 200_000
    private const val KEY_LEN = 32

    private val rng = SecureRandom()

    /**
     * PBKDF2-HMAC-SHA256, implemented with Mac (available on every API level) so
     * it works on Android 7+ — SecretKeyFactory("PBKDF2WithHmacSHA256") only
     * exists on API 26+. Byte-identical to the Go server's DeriveKey.
     */
    fun deriveKey(password: String): ByteArray =
        pbkdf2(password.toByteArray(Charsets.UTF_8), SALT, ITERATIONS, KEY_LEN)

    private fun pbkdf2(password: ByteArray, salt: ByteArray, iterations: Int, keyLen: Int): ByteArray {
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(password, "HmacSHA256")) // key stays across doFinal() calls
        val hLen = mac.macLength
        val numBlocks = (keyLen + hLen - 1) / hLen
        val out = ByteArray(numBlocks * hLen)
        val block = ByteArray(salt.size + 4)
        System.arraycopy(salt, 0, block, 0, salt.size)
        var offset = 0
        for (i in 1..numBlocks) {
            block[salt.size] = (i ushr 24).toByte()
            block[salt.size + 1] = (i ushr 16).toByte()
            block[salt.size + 2] = (i ushr 8).toByte()
            block[salt.size + 3] = i.toByte()
            var u = mac.doFinal(block)
            val t = u.copyOf()
            for (j in 1 until iterations) {
                u = mac.doFinal(u)
                for (k in t.indices) t[k] = (t[k].toInt() xor u[k].toInt()).toByte()
            }
            System.arraycopy(t, 0, out, offset, hLen)
            offset += hLen
        }
        return out.copyOf(keyLen)
    }

    fun newNonce(): ByteArray = ByteArray(NONCE_LEN).also { rng.nextBytes(it) }

    /** Builds and signs a beam datagram. */
    fun encodeBeam(key: ByteArray, nonce: ByteArray, nodeName: String, unixSeconds: Long): ByteArray {
        val name = nodeName.toByteArray(Charsets.UTF_8)
        require(name.size <= MAX_NAME) { "node name too long" }
        val body = ByteBuffer.allocate(4 + 1 + 8 + NONCE_LEN + 1 + name.size)
        body.put(MAGIC.toByteArray(Charsets.US_ASCII))
        body.put(VERSION.toByte())
        body.putLong(unixSeconds)
        body.put(nonce)
        body.put(name.size.toByte())
        body.put(name)
        val b = body.array()
        return b + hmac(key, b)
    }

    /**
     * Verifies an acknowledgement and returns the granted IP, or null if the
     * datagram is not a valid ack for [expectNonce].
     */
    fun decodeAck(data: ByteArray, key: ByteArray, expectNonce: ByteArray): String? {
        val fixed = 4 + NONCE_LEN + 1
        if (data.size < fixed + HMAC_LEN) return null
        if (String(data, 0, 4, Charsets.US_ASCII) != ACK_MAGIC) return null
        val ipLen = data[4 + NONCE_LEN].toInt() and 0xff
        val bodyLen = fixed + ipLen
        if (data.size != bodyLen + HMAC_LEN) return null
        if (!constEq(hmac(key, data.copyOfRange(0, bodyLen)), data.copyOfRange(bodyLen, data.size))) return null
        if (!constEq(data.copyOfRange(4, 4 + NONCE_LEN), expectNonce)) return null
        return String(data, fixed, ipLen, Charsets.UTF_8)
    }

    private fun hmac(key: ByteArray, msg: ByteArray): ByteArray {
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(key, "HmacSHA256"))
        return mac.doFinal(msg)
    }

    private fun constEq(a: ByteArray, b: ByteArray): Boolean {
        if (a.size != b.size) return false
        var r = 0
        for (i in a.indices) r = r or (a[i].toInt() xor b[i].toInt())
        return r == 0
    }
}
