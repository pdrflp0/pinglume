package export

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

// Redactor remove ou substitui identificadores antes de qualquer texto entrar
// na exportação, e conta o que fez para o relatório declarar a redação.
type Redactor struct {
	hostname string
	owner    []string // nome do dono extraído do nome do computador
	ifaces   map[string]string
	counts   map[string]int
}

func NewRedactor(hostname string) *Redactor {
	h := strings.TrimSpace(hostname)
	return &Redactor{hostname: h, owner: ownerNames(h), ifaces: map[string]string{}, counts: map[string]int{}}
}

var (
	// "MacBook-Pro-de-Fulano-Silva", "iMac-do-Fulano", "Mac-mini-da-Fulana"
	reOwnerPT = regexp.MustCompile(`(?i)^(?:mac(?:book)?(?:-?(?:pro|air))?|imac|mac-?mini|mac-?studio)-(?:de|do|da)-(.+)$`)
	// "Fulanos-MacBook-Pro", "Fulano-Silvas-iMac"
	reOwnerEN = regexp.MustCompile(`(?i)^(.+?)s?-(?:mac(?:book)?(?:-?(?:pro|air))?|imac|mac-?mini|mac-?studio)(?:-\d+)?$`)
)

// ownerNames extrai o nome do dono do nome padrão do computador no macOS:
// o nome inteiro e cada parte com 3+ letras.
func ownerNames(hostname string) []string {
	h := strings.TrimSuffix(hostname, ".local")
	var raw string
	if m := reOwnerPT.FindStringSubmatch(h); m != nil {
		raw = m[1]
	} else if m := reOwnerEN.FindStringSubmatch(h); m != nil {
		raw = strings.TrimSuffix(m[1], "s")
	}
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	out := []string{strings.Join(parts, " ")}
	for _, p := range parts {
		if len([]rune(p)) >= 3 {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

var (
	reEmail = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	reMAC   = regexp.MustCompile(`\b[0-9A-Fa-f]{2}(?:[:\-][0-9A-Fa-f]{2}){5}\b`)
	reIPv4  = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// IPv6: 5+ grupos ou forma abreviada com "::". Horários (21:40:43) têm só
	// 3 grupos e não podem ser confundidos com endereços.
	reIPv6 = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{1,4}:){4,7}[0-9A-Fa-f]{1,4}\b|\b(?:[0-9A-Fa-f]{1,4}:)+:(?:[0-9A-Fa-f]{1,4}:?)*\b`)
	// Sequências longas de dígitos (telefone, documento), com separadores comuns.
	reNumber = regexp.MustCompile(`\+?\(?\d[\d\s().\-]{7,}\d`)
	reIface  = regexp.MustCompile(`\b(?:en|utun|ipsec|ppp|bridge|wg|tun|wlan|eth|awdl|llw)\d+\b`)
)

// Iface troca o nome da interface por um rótulo estável dentro da exportação.
func (r *Redactor) Iface(name string) string {
	if name == "" {
		return ""
	}
	if l, ok := r.ifaces[name]; ok {
		return l
	}
	l := fmt.Sprintf("interface-%d", len(r.ifaces)+1)
	r.ifaces[name] = l
	r.counts["interface"]++
	return l
}

// Text limpa texto livre ou gerado: nome do dispositivo, e-mails, endereços
// MAC e IP, números longos e nomes de interface.
func (r *Redactor) Text(s string) string {
	if s == "" {
		return s
	}
	if r.hostname != "" {
		for _, h := range hostVariants(r.hostname) {
			if strings.Contains(strings.ToLower(s), strings.ToLower(h)) {
				s = replaceFold(s, h, "[dispositivo]")
				r.counts["dispositivo"]++
			}
		}
	}
	s = reEmail.ReplaceAllStringFunc(s, func(string) string { r.counts["email"]++; return "[e-mail]" })
	s = reMAC.ReplaceAllStringFunc(s, func(string) string { r.counts["mac"]++; return "[endereço MAC]" })
	s = reIPv6.ReplaceAllStringFunc(s, func(string) string { r.counts["ip"]++; return "[endereço IP]" })
	s = reIPv4.ReplaceAllStringFunc(s, func(m string) string {
		if a, err := netip.ParseAddr(m); err == nil && a.IsLoopback() {
			return m // loopback não identifica ninguém (é o testbed local)
		}
		r.counts["ip"]++
		return "[endereço IP]"
	})
	s = reNumber.ReplaceAllStringFunc(s, func(m string) string {
		digits := 0
		for _, c := range m {
			if c >= '0' && c <= '9' {
				digits++
			}
		}
		if digits < 8 {
			return m
		}
		r.counts["numero"]++
		return "[número]"
	})
	// Depois de e-mails e endereços, para não quebrar esses padrões.
	for _, name := range r.owner {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
		s = re.ReplaceAllStringFunc(s, func(string) string { r.counts["nome"]++; return "[nome]" })
	}
	s = reIface.ReplaceAllStringFunc(s, r.Iface)
	return s
}

// Target mantém alvos de loopback (testbed) e endpoints Pinglume, e oculta
// endereços de rede privada, que revelam a rede local.
func (r *Redactor) Target(t string) string {
	host := t
	if i := strings.Index(t, "://"); i >= 0 {
		host = t[i+3:]
	}
	host = strings.Split(host, "/")[0]
	if ap, err := netip.ParseAddrPort(host); err == nil && ap.Addr().IsPrivate() {
		r.counts["ip"]++
		return strings.Replace(t, ap.Addr().String(), "[endereço privado]", 1)
	}
	return t
}

// hostVariants cobre "MacBook-de-Fulano", "MacBook-de-Fulano.local" e o
// nome com espaços que o macOS mostra na interface.
func hostVariants(h string) []string {
	base := strings.TrimSuffix(h, ".local")
	vs := []string{h, base, strings.ReplaceAll(base, "-", " ")}
	sort.Slice(vs, func(i, j int) bool { return len(vs[i]) > len(vs[j]) })
	return vs
}

func replaceFold(s, old, repl string) string {
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(old))
	return re.ReplaceAllString(s, repl)
}

type Redaction struct {
	What  string `json:"what"`
	How   string `json:"how"`
	Count int    `json:"count"`
}

// Report descreve as redações aplicadas, inclusive as que não acharam nada.
func (r *Redactor) Report(notesIncluded bool, notesOmitted int) []Redaction {
	out := []Redaction{
		{What: "Nome do dispositivo", How: "não exportado; ocorrências em textos trocadas por [dispositivo]", Count: r.counts["dispositivo"]},
		{What: "Nome do dono contido no nome do computador (padrão do macOS)", How: "trocado por [nome] em todos os textos", Count: r.counts["nome"]},
		{What: "Nomes de interface de rede", How: "trocados por interface-1, interface-2…", Count: r.counts["interface"]},
		{What: "Endereços IP (exceto loopback do testbed)", How: "trocados por [endereço IP] ou [endereço privado]", Count: r.counts["ip"]},
		{What: "Endereços MAC", How: "trocados por [endereço MAC]", Count: r.counts["mac"]},
		{What: "E-mails", How: "trocados por [e-mail]", Count: r.counts["email"]},
		{What: "Números longos (telefone, documento)", How: "trocados por [número]", Count: r.counts["numero"]},
		{What: "Detalhes técnicos de erro e caminhos de arquivo", How: "não exportados; só contagens por status", Count: 0},
	}
	if notesIncluded {
		out = append(out, Redaction{What: "Observações dos relatos", How: "incluídas a pedido, com as substituições acima", Count: 0})
	} else {
		out = append(out, Redaction{What: "Observações dos relatos", How: "omitidas (padrão)", Count: notesOmitted})
	}
	return out
}
