package main

import (
	"net"
	"sync"
)

type connLimiter struct {
	net.Listener
	maxTotal, maxPerIP int
	mu                 sync.Mutex
	total              int
	perIP              map[string]int
}

func newConnLimiter(ln net.Listener, maxTotal, maxPerIP int) *connLimiter {
	return &connLimiter{Listener: ln, maxTotal: maxTotal, maxPerIP: maxPerIP, perIP: map[string]int{}}
}

func (l *connLimiter) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		if l.acquire(host) {
			return &limitedConn{Conn: c, release: func() { l.release(host) }}, nil
		}
		c.Close()
	}
}

func (l *connLimiter) acquire(host string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= l.maxTotal || l.perIP[host] >= l.maxPerIP {
		return false
	}
	l.total++
	l.perIP[host]++
	return true
}

func (l *connLimiter) release(host string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total--
	if l.perIP[host]--; l.perIP[host] == 0 {
		delete(l.perIP, host)
	}
}

type limitedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConn) Close() error {
	c.once.Do(c.release)
	return c.Conn.Close()
}
