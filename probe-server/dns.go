package main

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strings"
)

// Servidor DNS autoritativo mínimo para uma única zona: responde A para
// qualquer nome dentro dela (ex.: <nonce>.p.pinglume.test), o que permite
// medir resolução sem cache. Fora da zona responde REFUSED. Não é recursivo.

const (
	typeA     = 1
	classIN   = 1
	rcodeOK   = 0
	rcodeFail = 2
	rcodeNX   = 3
	rcodeRef  = 5
)

type dnsQuery struct {
	id     uint16
	rd     bool
	name   string // minúsculo, sem ponto final
	qtype  uint16
	qclass uint16
	raw    []byte // seção da pergunta, para ecoar na resposta
}

var errMalformed = errors.New("consulta DNS malformada")

func parseQuery(b []byte) (dnsQuery, error) {
	var q dnsQuery
	if len(b) < 12 {
		return q, errMalformed
	}
	q.id = binary.BigEndian.Uint16(b[0:2])
	flags := binary.BigEndian.Uint16(b[2:4])
	if flags&0x8000 != 0 || binary.BigEndian.Uint16(b[4:6]) != 1 {
		return q, errMalformed // é resposta ou não tem exatamente uma pergunta
	}
	q.rd = flags&0x0100 != 0
	i := 12
	var labels []string
	for {
		if i >= len(b) {
			return q, errMalformed
		}
		n := int(b[i])
		if n == 0 {
			i++
			break
		}
		if n > 63 || i+1+n > len(b) {
			return q, errMalformed // ponteiros de compressão não são válidos na pergunta
		}
		labels = append(labels, strings.ToLower(string(b[i+1:i+1+n])))
		i += 1 + n
	}
	if i+4 > len(b) {
		return q, errMalformed
	}
	q.name = strings.Join(labels, ".")
	q.qtype = binary.BigEndian.Uint16(b[i : i+2])
	q.qclass = binary.BigEndian.Uint16(b[i+2 : i+4])
	q.raw = b[12 : i+4]
	return q, nil
}

func inZone(name, zone string) bool {
	return name == zone || strings.HasSuffix(name, "."+zone)
}

// buildResponse monta a resposta. answer só é usado com rcode OK e pergunta A/IN.
func buildResponse(q dnsQuery, rcode int, answer netip.Addr) []byte {
	out := make([]byte, 12, 12+len(q.raw)+16)
	binary.BigEndian.PutUint16(out[0:2], q.id)
	flags := uint16(0x8000 | 0x0400) // QR + AA
	if q.rd {
		flags |= 0x0100
	}
	flags |= uint16(rcode)
	binary.BigEndian.PutUint16(out[2:4], flags)
	binary.BigEndian.PutUint16(out[4:6], 1)
	withAnswer := rcode == rcodeOK && q.qtype == typeA && q.qclass == classIN && answer.Is4()
	if withAnswer {
		binary.BigEndian.PutUint16(out[6:8], 1)
	}
	out = append(out, q.raw...)
	if withAnswer {
		a := answer.As4()
		out = append(out,
			0xC0, 0x0C, // ponteiro para o nome da pergunta
			0, typeA, 0, classIN,
			0, 0, 0, 0, // TTL 0: nada deve ser guardado em cache
			0, 4, a[0], a[1], a[2], a[3])
	}
	return out
}

// answerFor decide a resposta conforme a zona e a falha simulada; nil = não responder.
func answerFor(q dnsQuery, zone string, addr netip.Addr, fault string) []byte {
	if !inZone(q.name, zone) {
		return buildResponse(q, rcodeRef, addr)
	}
	switch fault {
	case "drop":
		return nil
	case "servfail":
		return buildResponse(q, rcodeFail, addr)
	case "nxdomain":
		return buildResponse(q, rcodeNX, addr)
	}
	// Pergunta de outro tipo (ex.: AAAA): NOERROR sem resposta.
	return buildResponse(q, rcodeOK, addr)
}

func serveDNS(pc net.PacketConn, zone string, addr netip.Addr, faults *Faults) error {
	buf := make([]byte, 512)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return err
		}
		q, err := parseQuery(buf[:n])
		if err != nil {
			continue // descarta lixo sem responder
		}
		if resp := answerFor(q, zone, addr, faults.Get().DNS); resp != nil {
			pc.WriteTo(resp, from)
		}
	}
}
