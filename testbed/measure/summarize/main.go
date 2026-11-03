package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type sample struct {
	ts          time.Time
	rssKB, cpuS float64
	in, out, db float64
}

var headerModel = map[string]struct {
	packets int
	bytes   int
	desc    string
}{
	"tcp":   {7, 52, "SYN, SYN-ACK, ACK, FIN, ACK, FIN, ACK"},
	"https": {14, 52, "7 de controle TCP + ~7 segmentos de TLS e HTTP"},
	"dns":   {2, 28, "consulta e resposta UDP"},
}

func readSamples(path string) ([]sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	var out []sample
	for _, r := range rows[1:] {
		if len(r) != 6 {
			continue
		}
		v := make([]float64, 6)
		for i, x := range r {
			v[i], _ = strconv.ParseFloat(strings.TrimSpace(x), 64)
		}
		out = append(out, sample{ts: time.Unix(int64(v[0]), 0), rssKB: v[1], cpuS: v[2], in: v[3], out: v[4], db: v[5]})
	}
	return out, nil
}

func readEnv(path string) map[string]string {
	env := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return env
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			env[k] = v
		}
	}
	return env
}

func readCounts(path string) map[string]int {
	counts := map[string]int{}
	f, err := os.Open(path)
	if err != nil {
		return counts
	}
	defer f.Close()
	rows, _ := csv.NewReader(f).ReadAll()
	for _, r := range rows {
		if len(r) == 2 {
			counts[r[0]], _ = strconv.Atoi(r[1])
		}
	}
	return counts
}

type coverage struct {
	count int
	ms    float64
}

func readCoverage(path string) map[string]coverage {
	cov := map[string]coverage{}
	f, err := os.Open(path)
	if err != nil {
		return cov
	}
	defer f.Close()
	rows, _ := csv.NewReader(f).ReadAll()
	for _, r := range rows {
		if len(r) == 3 {
			n, _ := strconv.Atoi(r[1])
			ms, _ := strconv.ParseFloat(r[2], 64)
			cov[r[0]] = coverage{n, ms}
		}
	}
	return cov
}

type span struct{ start, end time.Time }

func readSuspended(path string) []span {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	rows, _ := csv.NewReader(f).ReadAll()
	var out []span
	for _, r := range rows {
		if len(r) != 2 {
			continue
		}
		a, _ := strconv.ParseInt(r[0], 10, 64)
		b, _ := strconv.ParseInt(r[1], 10, 64)
		out = append(out, span{time.UnixMilli(a), time.UnixMilli(b)})
	}
	return out
}

func readSleeps(path string, from, to time.Time) []span {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	rows, _ := csv.NewReader(f).ReadAll()
	var out []span
	var asleep *time.Time
	for _, r := range rows {
		if len(r) != 2 {
			continue
		}
		t, err := time.Parse("2006-01-02 15:04:05 -0700", r[0])
		if err != nil || t.Before(from) || t.After(to) {
			continue
		}
		switch r[1] {
		case "Sleep":
			if asleep == nil {
				asleep = &t
			}
		case "Wake", "DarkWake":
			if asleep != nil {
				out = append(out, span{*asleep, t})
				asleep = nil
			}
		}
	}
	return out
}

func absSec(d time.Duration) float64 { return math.Abs(d.Seconds()) }

func mb(b float64) string { return strings.Replace(fmt.Sprintf("%.2f MB", b/1048576), ".", ",", 1) }
func kb(b float64) string { return strings.Replace(fmt.Sprintf("%.1f KB", b/1024), ".", ",", 1) }
func num(f float64, d int) string {
	return strings.Replace(strconv.FormatFloat(f, 'f', d, 64), ".", ",", 1)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "uso: summarize <diretório da medição>")
		os.Exit(2)
	}
	dir := os.Args[1]
	s, err := readSamples(filepath.Join(dir, "samples.csv"))
	if err != nil || len(s) < 2 {
		fmt.Fprintln(os.Stderr, "amostras insuficientes:", err)
		os.Exit(1)
	}
	env := readEnv(filepath.Join(dir, "env.txt"))
	counts := readCounts(filepath.Join(dir, "counts.csv"))
	cov := readCoverage(filepath.Join(dir, "coverage.csv"))

	first, last := s[0], s[len(s)-1]
	var awakeS, asleepS, cpuPeak, rssSum, rssMax, in, out float64
	var perMin []float64
	for i, x := range s {
		rssSum += x.rssKB
		rssMax = math.Max(rssMax, x.rssKB)
		in += x.in
		out += x.out
		if i == 0 {
			continue
		}
		dt := x.ts.Sub(s[i-1].ts).Seconds()
		if dt <= 0 {
			continue
		}
		if dt > 120 {
			asleepS += dt
			continue
		}
		awakeS += dt
		d := x.cpuS - s[i-1].cpuS
		p := d / dt * 100
		perMin = append(perMin, p)
		cpuPeak = math.Max(cpuPeak, p)
	}
	measuredS := cov["measuring"].ms / 1000
	if measuredS <= 0 {
		measuredS = awakeS
	}
	measuredH := measuredS / 3600
	cpuAvg := last.cpuS / measuredS * 100
	sort.Float64s(perMin)
	cpuP95 := perMin[int(float64(len(perMin)-1)*0.95)]
	rssAvg := rssSum / float64(len(s))
	windowH := float64(len(s)) / 60
	inH, outH := in/windowH, out/windowH
	payloadH := inH + outH

	var overhead float64
	var execs int
	kinds := []string{"tcp", "dns", "https"}
	for _, k := range kinds {
		m := headerModel[k]
		overhead += float64(counts[k] * m.packets * m.bytes)
		execs += counts[k]
	}
	execsH := float64(execs) / measuredH
	overheadH := overhead / measuredH
	totalH := payloadH + overheadH
	dbPerExec := (last.db - first.db) / float64(execs)
	dbGrowthH := dbPerExec * execsH

	fmt.Printf("# Medição de recursos do agente\n\n")
	fmt.Printf("Medição real no testbed local, gerada por `testbed/measure/measure.sh` e `testbed/measure/summarize` a partir dos arquivos brutos da pasta `%s` (`env.txt`, `samples.csv`, `counts.csv`, `coverage.csv`, `suspended.csv`, `pmset.csv`). Nada aqui é estimativa de desenvolvimento, exceto o que está marcado como modelo. Para regenerar: `go -C testbed/measure run ./summarize <pasta>`.\n\n", filepath.Base(dir))
	fmt.Printf("## Ambiente\n\n| Item | Valor |\n| --- | --- |\n")
	for _, k := range []string{"inicio", "fim", "macos", "cpu", "memoria_gb", "go", "agente", "commit", "sondas"} {
		fmt.Printf("| %s | %s |\n", k, env[k])
	}
	fmt.Printf("| tempo medindo, segundo o próprio agente | %s h |\n", num(measuredH, 2))
	fmt.Printf("| computador suspenso, segundo o próprio agente | %s h em %d lacunas |\n", num(cov["suspended"].ms/3600000, 2), cov["suspended"].count)
	fmt.Printf("| amostras de 1 min do roteiro | %d (%s h entre amostras seguidas; %s h em intervalos > 2 min excluídos) |\n\n", len(s), num(awakeS/3600, 2), num(asleepS/3600, 2))
	fmt.Printf("As taxas por hora valem para o computador acordado e o agente medindo. Tempo suspenso não entra no denominador; a projeção de disco supõe operação contínua.\n\n")

	fmt.Printf("## Resultados\n\n| Recurso | Medido |\n| --- | --- |\n")
	fmt.Printf("| CPU, média (tempo de CPU total ÷ tempo medindo) | %s%% de um núcleo |\n", num(cpuAvg, 3))
	fmt.Printf("| CPU, p95 dos minutos | %s%% de um núcleo |\n", num(cpuP95, 3))
	fmt.Printf("| CPU, pior minuto | %s%% de um núcleo |\n", num(cpuPeak, 3))
	fmt.Printf("| Memória residente, média | %s |\n", mb(rssAvg*1024))
	fmt.Printf("| Memória residente, máxima | %s |\n", mb(rssMax*1024))
	fmt.Printf("| Tráfego de dados no socket (nettop), entrada | %s/h |\n", kb(inH))
	fmt.Printf("| Tráfego de dados no socket (nettop), saída | %s/h |\n", kb(outH))
	fmt.Printf("| Execuções de sonda | %d em %s h medindo (%s/h; esperado com os intervalos padrão: 1620/h) |\n", execs, num(measuredH, 2), num(execsH, 0))
	fmt.Printf("| Banco de dados, crescimento por execução | %s B |\n", num(dbPerExec, 0))
	fmt.Printf("| Banco de dados, crescimento por hora medindo | %s |\n", kb(dbGrowthH))
	fmt.Printf("| Banco de dados, tamanho final | %s |\n\n", mb(last.db))

	fmt.Printf("## Tráfego total com cabeçalhos\n\n")
	fmt.Printf("O `nettop` conta só os dados no socket. Os cabeçalhos IPv4 e TCP/UDP vêm de um modelo: pacotes por execução × tamanho do cabeçalho × execuções gravadas no banco.\n\n")
	fmt.Printf("| Sonda | Execuções | Pacotes por execução (modelo) | Cabeçalho por pacote | Cabeçalhos estimados |\n| --- | --- | --- | --- | --- |\n")
	for _, k := range kinds {
		m := headerModel[k]
		fmt.Printf("| %s | %d | %d (%s) | %d B | %s |\n", strings.ToUpper(k), counts[k], m.packets, m.desc, m.bytes, kb(float64(counts[k]*m.packets*m.bytes)))
	}
	fmt.Printf("\n| | Por hora | Por dia |\n| --- | --- | --- |\n")
	fmt.Printf("| Dados no socket (medido) | %s | %s |\n", kb(payloadH), mb(payloadH*24))
	fmt.Printf("| Cabeçalhos (modelo) | %s | %s |\n", kb(overheadH), mb(overheadH*24))
	fmt.Printf("| **Total estimado** | **%s** | **%s** |\n\n", kb(totalH), mb(totalH*24))

	fmt.Printf("## Comparação com as metas do desenho\n\n| Meta (docs/00-desenho-inicial.md) | Resultado |\n| --- | --- |\n")
	fmt.Printf("| Tráfego ≤ 5 MB/h para o conjunto de sondas | %s/h → %s |\n", kb(totalH), verdict(totalH <= 5*1048576))
	fmt.Printf("| Disco ~65 MB em 7 dias de dados brutos | %s projetados em 7 dias medindo sem parar → %s |\n\n", mb(dbGrowthH*24*7), verdict(dbGrowthH*24*7 <= 65*1048576))

	sleeps := readSleeps(filepath.Join(dir, "pmset.csv"), first.ts.Add(-2*time.Minute), last.ts)
	susp := readSuspended(filepath.Join(dir, "suspended.csv"))
	if len(sleeps) > 0 {
		const tol = 15 * time.Second
		used := make([]bool, len(susp))
		var hit int
		var maxStart, maxEnd float64
		var shortest time.Duration = -1
		for _, sl := range sleeps {
			if d := sl.end.Sub(sl.start); shortest < 0 || d < shortest {
				shortest = d
			}
			for j, g := range susp {
				if used[j] || absSec(g.start.Sub(sl.start)) > tol.Seconds() || absSec(g.end.Sub(sl.end)) > tol.Seconds() {
					continue
				}
				used[j] = true
				hit++
				maxStart = math.Max(maxStart, absSec(g.start.Sub(sl.start)))
				maxEnd = math.Max(maxEnd, absSec(g.end.Sub(sl.end)))
				break
			}
		}
		extra := 0
		for _, u := range used {
			if !u {
				extra++
			}
		}
		fmt.Printf("## Detecção de suspensão comparada ao log de energia do macOS\n\n")
		fmt.Printf("O agente detecta suspensão pela diferença entre relógio civil e monotônico (limiar: um heartbeat, 5 s). Aqui cada lacuna `suspended` gravada no banco é comparada aos eventos `Sleep` → `Wake`/`DarkWake` de `pmset -g log` no mesmo período, com tolerância de %d s nas duas pontas.\n\n", int(tol.Seconds()))
		fmt.Printf("| Item | Resultado |\n| --- | --- |\n")
		fmt.Printf("| Períodos de sono no log do macOS | %d (o mais curto: %s) |\n", len(sleeps), shortest.Round(time.Second))
		fmt.Printf("| Detectados pelo agente | %d |\n", hit)
		fmt.Printf("| Não detectados | %d |\n", len(sleeps)-hit)
		fmt.Printf("| Lacunas do agente sem sono correspondente | %d |\n", extra)
		fmt.Printf("| Maior diferença no início / no fim | %s s / %s s |\n\n", num(maxStart, 0), num(maxEnd, 0))
	}

	fmt.Printf("## Limites desta medição\n\n")
	fmt.Printf("- Endpoints no próprio computador (loopback): mede o custo do agente, não a latência da internet. Os tamanhos de pacote TCP e TLS são os mesmos de uma rede real; o MTU de loopback é maior, mas as mensagens das sondas cabem num pacote.\n")
	fmt.Printf("- Cabeçalhos por modelo, não por captura de pacotes (que exigiria privilégio de administrador).\n")
	fmt.Printf("- CPU e memória vêm do `ps` uma vez por minuto; picos mais curtos que isso podem não aparecer.\n")
	fmt.Printf("- O computador ficou suspenso a maior parte do período. CPU e memória vêm só dos minutos acordados, e a maior parte dos despertares foi curta (manutenção do macOS), quando o agente recupera as sondas atrasadas.\n")
	fmt.Printf("- A comparação com o `pmset` valida a heurística só para o sono deste computador neste período. Sono mais curto que o limiar não é detectado, e relógio adiantado continua indistinguível de suspensão.\n")
	fmt.Printf("- A sonda UDP (F12) ainda não existe e não está incluída.\n")
}

func verdict(ok bool) string {
	if ok {
		return "dentro da meta"
	}
	return "acima da meta"
}
