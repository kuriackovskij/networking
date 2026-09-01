package protocol

import (
	"testing"
	"time"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	key := DeriveKey("correct horse battery staple")
	nonce, err := NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	in := &Packet{Timestamp: time.Unix(1700000000, 0), Nonce: nonce, NodeName: "phone"}
	msg, err := Encode(in, key)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(msg, key)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.NodeName != in.NodeName || !out.Timestamp.Equal(in.Timestamp) || out.Nonce != in.Nonce {
		t.Fatalf("round trip mismatch: %+v vs %+v", out, in)
	}
}

func TestDecodeRejectsWrongKey(t *testing.T) {
	nonce, _ := NewNonce()
	msg, _ := Encode(&Packet{Timestamp: time.Now(), Nonce: nonce, NodeName: "x"}, DeriveKey("right"))
	if _, err := Decode(msg, DeriveKey("wrong")); err == nil {
		t.Fatal("expected signature failure with wrong key")
	}
}

func TestDecodeRejectsTamper(t *testing.T) {
	key := DeriveKey("pw")
	nonce, _ := NewNonce()
	msg, _ := Encode(&Packet{Timestamp: time.Now(), Nonce: nonce, NodeName: "x"}, key)
	msg[10] ^= 0xff // flip a byte in the timestamp region
	if _, err := Decode(msg, key); err == nil {
		t.Fatal("expected signature failure after tamper")
	}
}

func TestAckRoundTrip(t *testing.T) {
	key := DeriveKey("pw")
	nonce, _ := NewNonce()
	ack, err := EncodeAck(nonce, "203.0.113.7", key)
	if err != nil {
		t.Fatal(err)
	}
	ip, err := DecodeAck(ack, key, nonce)
	if err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ip != "203.0.113.7" {
		t.Fatalf("got %q", ip)
	}
	var other [NonceLen]byte
	if _, err := DecodeAck(ack, key, other); err == nil {
		t.Fatal("expected nonce mismatch to fail")
	}
}
