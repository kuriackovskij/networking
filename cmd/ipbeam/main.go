// Command ipbeam is the reference IP-Beamer client. It sends one authenticated
// beam and, if the server has acknowledgements enabled, prints the IP that was
// granted access. It runs anywhere Go runs (Linux, macOS, Windows) and is the
// logic the mobile/desktop apps will wrap.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/kuriackovskij/networking/internal/protocol"
)

// version is set at build time via -ldflags "-X main.version=…".
var version = "dev"

func main() {
	server := flag.String("server", "", "server host:port, e.g. myhome.example.com:62201")
	password := flag.String("password", "", "shared password")
	node := flag.String("node", "", "node name (a label for this device)")
	wait := flag.Duration("wait", 3*time.Second, "how long to wait for the acknowledgement")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVer {
		fmt.Println("ipbeam", version)
		os.Exit(0)
	}

	if *server == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "usage: ipbeam -server host:port -password PASS [-node name]")
		os.Exit(2)
	}

	key := protocol.DeriveKey(*password)
	nonce, err := protocol.NewNonce()
	if err != nil {
		log.Fatalf("nonce: %v", err)
	}
	msg, err := protocol.Encode(&protocol.Packet{
		Timestamp: time.Now(),
		Nonce:     nonce,
		NodeName:  *node,
	}, key)
	if err != nil {
		log.Fatalf("encode: %v", err)
	}

	raddr, err := net.ResolveUDPAddr("udp", *server)
	if err != nil {
		log.Fatalf("resolve %s: %v", *server, err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write(msg); err != nil {
		log.Fatalf("send: %v", err)
	}
	fmt.Printf("beam sent to %s\n", *server)

	conn.SetReadDeadline(time.Now().Add(*wait))
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Println("no acknowledgement received (server may have ack disabled, or the beam was rejected)")
		return
	}
	ip, err := protocol.DecodeAck(buf[:n], key, nonce)
	if err != nil {
		fmt.Printf("received an invalid acknowledgement: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("access granted for %s\n", ip)
}
