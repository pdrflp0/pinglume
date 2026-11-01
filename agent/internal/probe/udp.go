package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"net"
	"sort"
	"sync"
	"time"
)

var udpMagic = []byte("PGLU")

const udpPacketSize = 64

type UDPEcho struct {
	Addr    string
	Count   int
	Spacing time.Duration
	Wait    time.Duration
}

func (UDPEcho) Kind() string         { return "udp_echo" }
func (UDPEcho) ProtocolVersion() int { return 1 }

type BurstStats struct {
	Sent, Received, Reordered int
	RTTs                      []time.Duration
	JitterUS                  int64
}

func (p UDPEcho) Probe(ctx context.Context) Result {
	start := time.Now()
	st, err := p.burst(ctx)
	elapsed := time.Since(start)
	if err != nil {
		return Result{Duration: elapsed, Status: Classify(ctx, err), ErrorDetail: err.Error()}
	}
	phases := map[string]int64{"sent": int64(st.Sent), "received": int64(st.Received),
		"lost": int64(st.Sent - st.Received), "reordered": int64(st.Reordered), "jitter_us": st.JitterUS}
	if st.Received == 0 {
		return Result{Duration: elapsed, Status: "timeout", ErrorDetail: "nenhum datagrama ecoado", Phases: phases}
	}
	sorted := append([]time.Duration(nil), st.RTTs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p50 := sorted[int(float64(len(sorted)-1)*0.5+0.5)]
	phases["rtt_p50_us"] = p50.Microseconds()
	phases["rtt_p95_us"] = sorted[int(float64(len(sorted)-1)*0.95+0.5)].Microseconds()
	return Result{Duration: p50, Status: "ok", Phases: phases}
}

func (p UDPEcho) burst(ctx context.Context) (BurstStats, error) {
	var st BurstStats
	if p.Count <= 0 || p.Count > 100 {
		return st, errors.New("rajada deve ter de 1 a 100 datagramas")
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", p.Addr)
	if err != nil {
		return st, err
	}
	defer conn.Close()

	var nonce [8]byte
	rand.Read(nonce[:])
	sentAt := make([]time.Time, p.Count)
	rtts := make([]time.Duration, p.Count)
	got := make([]bool, p.Count)
	order := make([]int, 0, p.Count)
	var mu sync.Mutex

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 256)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			now := time.Now()
			if n != udpPacketSize || string(buf[:4]) != string(udpMagic) || string(buf[8:16]) != string(nonce[:]) {
				continue
			}
			seq := int(binary.BigEndian.Uint32(buf[4:8]))
			mu.Lock()
			if seq < p.Count && !got[seq] && !sentAt[seq].IsZero() {
				got[seq] = true
				rtts[seq] = now.Sub(sentAt[seq])
				order = append(order, seq)
			}
			mu.Unlock()
		}
	}()

	pkt := make([]byte, udpPacketSize)
	copy(pkt, udpMagic)
	copy(pkt[8:16], nonce[:])
	tick := time.NewTicker(p.Spacing)
	defer tick.Stop()
	for i := 0; i < p.Count; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				conn.Close()
				<-done
				return st, ctx.Err()
			case <-tick.C:
			}
		}
		binary.BigEndian.PutUint32(pkt[4:8], uint32(i))
		mu.Lock()
		sentAt[i] = time.Now()
		mu.Unlock()
		if _, err := conn.Write(pkt); err == nil {
			st.Sent++
		}
	}
	select {
	case <-ctx.Done():
		conn.Close()
		<-done
		return st, ctx.Err()
	case <-time.After(p.Wait):
	}
	conn.Close()
	<-done

	var prev time.Duration
	var jitterSum float64
	for i, seq := range order {
		st.RTTs = append(st.RTTs, rtts[seq])
		if i > 0 {
			if seq < order[i-1] {
				st.Reordered++
			}
			jitterSum += math.Abs(float64(rtts[seq] - prev))
		}
		prev = rtts[seq]
	}
	st.Received = len(order)
	if len(order) > 1 {
		st.JitterUS = int64(jitterSum / float64(len(order)-1) / 1000)
	}
	return st, nil
}
