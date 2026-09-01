# IP-Beamer wire protocol (v1)

A "beam" is a single UDP datagram. It proves the sender knows the shared
password and cannot be usefully replayed. The server whitelists the **source IP
it observes**, so the datagram never carries an IP address of its own.

## Key derivation

The shared password is stretched into a 32-byte key:

```
key = PBKDF2-HMAC-SHA256(password, salt = "ip-beamer/v1/pbkdf2-hmac-sha256",
                         iterations = 200000, dkLen = 32)
```

Stretching means that even if an attacker captures a beam, brute-forcing the
password offline is expensive. Use a strong passphrase (4–6 random words).

## Beam datagram

All multi-byte integers are big-endian.

| Field      | Size (bytes) | Notes                                   |
|------------|--------------|-----------------------------------------|
| magic      | 4            | ASCII `IPB1`                            |
| version    | 1            | `0x01`                                  |
| timestamp  | 8            | Unix seconds (uint64)                   |
| nonce      | 16           | cryptographically random                |
| nameLen    | 1            | length of node name (0–32)              |
| nodeName   | nameLen      | UTF-8 label for the device (optional)   |
| hmac       | 32           | HMAC-SHA256(key, all bytes above)       |

Total: 62–94 bytes.

## Acknowledgement datagram

Sent back only when the beam authenticated, so it never reveals the port to an
attacker who cannot forge a valid HMAC.

| Field   | Size (bytes) | Notes                                    |
|---------|--------------|------------------------------------------|
| magic   | 4            | ASCII `IPBA`                             |
| nonce   | 16           | echoes the beam's nonce                  |
| ipLen   | 1            | length of the granted IP string          |
| ip      | ipLen        | the source IP that was whitelisted       |
| hmac    | 32           | HMAC-SHA256(key, all bytes above)        |

## Server acceptance rules

A beam is acted on only if **all** hold; otherwise it is dropped in silence:

1. `magic` and `version` match.
2. The HMAC verifies (constant-time compare).
3. `|now − timestamp|` is within the replay window (default 30 s). Clients and
   server should keep clocks roughly in sync (NTP).
4. The `nonce` has not been seen within twice the replay window.

On success the server adds the observed source IP to the allow-list with the
configured timeout (default 60 min). A repeat beam re-adds the element and
resets its timer — this is the keep-alive.

## Security properties and limits

- **Spoofing:** an attacker cannot open access for an arbitrary IP, because only
  the real source address of an authenticated packet is whitelisted.
- **Replay:** blocked by the timestamp window plus the nonce cache.
- **Scanner-invisibility:** unauthenticated packets get no reply and no rule.
- **CGNAT caveat:** on carrier-grade NAT (some mobile networks) the public IP is
  shared, so whitelisting it is coarser than a single household. This is
  inherent to any IP allow-list, not specific to this protocol.
- **Amplification-safe:** the ack is smaller than the beam, so the server cannot
  be used as a UDP amplifier.
