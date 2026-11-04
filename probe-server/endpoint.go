package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
)

type endpointJSON struct {
	Label      string `json:"label"`
	Region     string `json:"region"`
	IP         string `json:"ip"`
	TCPPort    uint16 `json:"tcp_port"`
	UDPPort    uint16 `json:"udp_port,omitempty"`
	ServerName string `json:"server_name"`
	CertPEM    string `json:"cert_pem"`
}

func listenPort(addr string) (uint16, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(port, 10, 16)
	return uint16(n), err
}

func endpointEntry(tlsDir, label, region, publicIP, serverName, tcpAddr, udpAddr string) (string, error) {
	if _, err := netip.ParseAddr(publicIP); err != nil {
		return "", fmt.Errorf("-public-ip: %w", err)
	}
	if region == "" {
		return "", errors.New("-region é obrigatório")
	}
	cert, err := os.ReadFile(filepath.Join(tlsDir, "probe-server.pem"))
	if err != nil {
		return "", err
	}
	e := endpointJSON{Label: label, Region: region, IP: publicIP, ServerName: serverName, CertPEM: string(cert)}
	if e.TCPPort, err = listenPort(tcpAddr); err != nil {
		return "", fmt.Errorf("-tcp: %w", err)
	}
	if udpAddr != "" {
		if e.UDPPort, err = listenPort(udpAddr); err != nil {
			return "", fmt.Errorf("-udp: %w", err)
		}
	}
	b, err := json.Marshal(e)
	return string(b), err
}
