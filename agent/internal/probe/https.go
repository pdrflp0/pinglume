package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
)

type HTTPSGet struct {
	URL        string
	ServerName string
	RootCAs    *x509.CertPool
	Timeout    time.Duration
}

func (HTTPSGet) Kind() string         { return "https" }
func (HTTPSGet) ProtocolVersion() int { return 1 }

const maxBody = 1024

func (p HTTPSGet) Probe(ctx context.Context) Result {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	start := time.Now()
	phases := map[string]int64{}
	mark := func(name string) { phases[name] = time.Since(start).Microseconds() }
	trace := &httptrace.ClientTrace{
		DNSDone:              func(httptrace.DNSDoneInfo) { mark("dns") },
		ConnectDone:          func(_, _ string, err error) { mark("connect") },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { mark("tls") },
		GotFirstResponseByte: func() { mark("ttfb") },
	}
	tr := &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		ForceAttemptHTTP2: false,
		TLSClientConfig:   &tls.Config{ServerName: p.ServerName, RootCAs: p.RootCAs, MinVersion: tls.VersionTLS12},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, p.URL, nil)
	if err != nil {
		return Result{Status: "error", ErrorDetail: err.Error()}
	}
	req.Header.Set("User-Agent", "pinglume-probe/1")
	resp, err := client.Do(req)
	if err != nil {
		return Result{Duration: time.Since(start), Status: classifyHTTPS(ctx, err), ErrorDetail: err.Error(), Phases: phases}
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	elapsed := time.Since(start)
	if err != nil {
		return Result{Duration: elapsed, Status: classifyHTTPS(ctx, err), ErrorDetail: err.Error(), Phases: phases}
	}
	if resp.StatusCode != http.StatusOK {
		return Result{Duration: elapsed, Status: "http_error", ErrorDetail: fmt.Sprintf("status %d", resp.StatusCode), Phases: phases}
	}
	return Result{Duration: elapsed, Status: "ok", Phases: phases}
}

func classifyHTTPS(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	var (
		unknownCA x509.UnknownAuthorityError
		hostErr   x509.HostnameError
		invalid   x509.CertificateInvalidError
		verifyErr *tls.CertificateVerificationError
		alert     tls.AlertError
		recordErr tls.RecordHeaderError
	)
	if errors.As(err, &unknownCA) || errors.As(err, &hostErr) || errors.As(err, &invalid) ||
		errors.As(err, &verifyErr) || errors.As(err, &alert) || errors.As(err, &recordErr) ||
		strings.Contains(err.Error(), "tls: ") {
		return "tls_error"
	}
	return Classify(ctx, err)
}
