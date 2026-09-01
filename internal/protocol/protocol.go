// Package protocol defines the IP-Beamer wire format and its authentication.
//
// A beam is a single UDP datagram. Its integrity and authenticity are proven
// by an HMAC-SHA256 over the whole message, keyed by a value derived from the
// shared password. A timestamp plus a random nonce make each beam unique so
// captured packets cannot be replayed. The datagram never carries the client's
// IP address: the server whitelists the source address it observes, which is by
// definition the address the client's traffic actually arrives from.
package protocol

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"time"
)

const (
	// Magic and Version identify a beam datagram.
	Magic   = "IPB1"
	Version = 1
	// AckMagic identifies the signed acknowledgement the server sends back.
	AckMagic = "IPBA"

	NonceLen    = 16
	HMACLen     = 32 // sha256
	MaxNodeName = 32

	// Fixed header: magic(4) + version(1) + timestamp(8) + nonce(16) + nameLen(1).
	headerLen = 4 + 1 + 8 + NonceLen + 1
	minLen    = headerLen + HMACLen
)

// saltConst stretches the password. A fixed salt is fine here because there is
// a single shared secret; the salt's job is to make the KDF domain-specific,
// while the iteration count is what makes offline brute force expensive.
var saltConst = []byte("ip-beamer/v1/pbkdf2-hmac-sha256")

// DeriveKey turns a human password into a 32-byte HMAC key. The stretching
// makes a captured beam expensive to brute-force offline, so even a modest
// passphrase stays safe.
func DeriveKey(password string) []byte {
	return pbkdf2([]byte(password), saltConst, 200000, 32)
}

// Packet is the decoded, authenticated content of a beam.
type Packet struct {
	Timestamp time.Time
	Nonce     [NonceLen]byte
	NodeName  string
}

// NewNonce returns a fresh cryptographically random nonce.
func NewNonce() ([NonceLen]byte, error) {
	var n [NonceLen]byte
	_, err := rand.Read(n[:])
	return n, err
}

// Encode serializes and signs a beam with the given key.
func Encode(p *Packet, key []byte) ([]byte, error) {
	name := []byte(p.NodeName)
	if len(name) > MaxNodeName {
		return nil, errors.New("node name too long")
	}
	buf := make([]byte, 0, minLen+len(name))
	buf = append(buf, Magic...)
	buf = append(buf, Version)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(p.Timestamp.Unix()))
	buf = append(buf, ts[:]...)
	buf = append(buf, p.Nonce[:]...)
	buf = append(buf, byte(len(name)))
	buf = append(buf, name...)
	return sign(buf, key), nil
}

// Decode verifies a beam's signature and parses it. Any error means the packet
// must be silently dropped.
func Decode(data, key []byte) (*Packet, error) {
	if len(data) < minLen {
		return nil, errors.New("short packet")
	}
	if string(data[:4]) != Magic {
		return nil, errors.New("bad magic")
	}
	if data[4] != Version {
		return nil, errors.New("bad version")
	}
	nameLen := int(data[headerLen-1])
	bodyLen := headerLen + nameLen
	if len(data) != bodyLen+HMACLen {
		return nil, errors.New("bad length")
	}
	if !verify(data[:bodyLen], data[bodyLen:], key) {
		return nil, errors.New("bad signature")
	}
	p := &Packet{
		Timestamp: time.Unix(int64(binary.BigEndian.Uint64(data[5:13])), 0),
		NodeName:  string(data[headerLen:bodyLen]),
	}
	copy(p.Nonce[:], data[13:13+NonceLen])
	return p, nil
}

// EncodeAck builds the signed acknowledgement carrying the granted IP so the
// client can display confirmation. Only an authenticated client can verify it,
// so replying does not reveal the port to scanners.
func EncodeAck(nonce [NonceLen]byte, ip string, key []byte) ([]byte, error) {
	ipb := []byte(ip)
	if len(ipb) > 255 {
		return nil, errors.New("ip too long")
	}
	buf := make([]byte, 0, len(AckMagic)+NonceLen+1+len(ipb)+HMACLen)
	buf = append(buf, AckMagic...)
	buf = append(buf, nonce[:]...)
	buf = append(buf, byte(len(ipb)))
	buf = append(buf, ipb...)
	return sign(buf, key), nil
}

// DecodeAck verifies an acknowledgement and checks it answers our beam.
func DecodeAck(data, key []byte, expectNonce [NonceLen]byte) (string, error) {
	const fixed = len(AckMagic) + NonceLen + 1
	if len(data) < fixed+HMACLen {
		return "", errors.New("short ack")
	}
	if string(data[:4]) != AckMagic {
		return "", errors.New("bad ack magic")
	}
	ipLen := int(data[4+NonceLen])
	bodyLen := fixed + ipLen
	if len(data) != bodyLen+HMACLen {
		return "", errors.New("bad ack length")
	}
	if !verify(data[:bodyLen], data[bodyLen:], key) {
		return "", errors.New("bad ack signature")
	}
	if subtle.ConstantTimeCompare(data[4:4+NonceLen], expectNonce[:]) != 1 {
		return "", errors.New("ack nonce mismatch")
	}
	return string(data[fixed:bodyLen]), nil
}

func sign(body, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return mac.Sum(body)
}

func verify(body, tag, key []byte) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return subtle.ConstantTimeCompare(tag, mac.Sum(nil)) == 1
}

// pbkdf2 implements PBKDF2-HMAC-SHA256 (RFC 8018) so the project has no
// external module dependencies and cross-compiles with a bare `go build`.
func pbkdf2(password, salt []byte, iter, keyLen int) []byte {
	prf := func(msg []byte) []byte {
		m := hmac.New(sha256.New, password)
		m.Write(msg)
		return m.Sum(nil)
	}
	hLen := sha256.Size
	numBlocks := (keyLen + hLen - 1) / hLen
	dk := make([]byte, 0, numBlocks*hLen)
	var idx [4]byte
	for block := 1; block <= numBlocks; block++ {
		binary.BigEndian.PutUint32(idx[:], uint32(block))
		u := prf(append(append([]byte{}, salt...), idx[:]...))
		t := make([]byte, hLen)
		copy(t, u)
		for i := 1; i < iter; i++ {
			u = prf(u)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}
