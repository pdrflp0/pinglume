package main

import (
	"bytes"
	"math/rand/v2"
	"net"
	"time"
)

var udpMagic = []byte("PGLU")

const maxUDPPayload = 128

func serveUDPEcho(pc net.PacketConn, faults *Faults, maxPerSecond int) error {
	limiter := newIPLimiter(400, time.Second)
	global := newIPLimiter(maxPerSecond, time.Second)
	buf := make([]byte, 2048)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return err
		}
		if n < len(udpMagic) || n > maxUDPPayload || !bytes.HasPrefix(buf[:n], udpMagic) {
			continue
		}
		host, _, _ := net.SplitHostPort(from.String())
		if !limiter.allow(host) || !global.allow("") {
			continue
		}
		state := faults.Get()
		if state.UDPLoss > 0 && rand.IntN(100) < state.UDPLoss {
			continue
		}
		msg := append([]byte(nil), buf[:n]...)
		if j := faults.udpJitter(); j > 0 {
			time.AfterFunc(time.Duration(rand.Int64N(int64(j))), func() { pc.WriteTo(msg, from) })
			continue
		}
		pc.WriteTo(msg, from)
	}
}
