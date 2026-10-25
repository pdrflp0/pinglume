package main

import (
	"flag"
	"fmt"
	"strings"
	"time"
)

var symptomLabel = map[string]string{
	"audio_cut": "áudio cortou", "video_freeze": "vídeo congelou", "dropped_call": "chamada caiu",
	"slow": "lentidão", "other": "outro",
}

var verdictLabel = map[string]string{
	"aguardando_dados":          "aguardando dados",
	"sem_cobertura":             "sem cobertura",
	"degradacao_observada":      "degradação observada",
	"sem_degradacao_detectavel": "sem degradação detectável",
}

type incidentDTO struct {
	ID         string    `json:"id"`
	ReportedAt time.Time `json:"reportedAt"`
	Symptom    string    `json:"symptom"`
	Note       string    `json:"note"`
	Verdict    string    `json:"verdict"`
}

type analysisDTO struct {
	Verdict     string    `json:"verdict"`
	RuleVersion string    `json:"ruleVersion"`
	WindowFrom  time.Time `json:"windowFrom"`
	WindowTo    time.Time `json:"windowTo"`
	Final       bool      `json:"final"`
	Findings    []struct {
		Text   string   `json:"text"`
		Inputs []string `json:"inputs"`
	} `json:"findings"`
	Alternatives []string `json:"alternatives"`
	NextTest     string   `json:"nextTest"`
	Caveats      []string `json:"caveats"`
}

func cmdMark(args []string) error {
	fs := flag.NewFlagSet("mark", flag.ExitOnError)
	var c common
	c.register(fs)
	symptom := fs.String("symptom", "audio_cut", "audio_cut | video_freeze | dropped_call | slow | other")
	note := fs.String("note", "", "observação livre (até 500 caracteres)")
	ago := fs.Duration("ago", 0, "há quanto tempo aconteceu, ex.: 2m (até 24h)")
	fs.Parse(args)

	body := map[string]any{"symptom": *symptom, "note": *note}
	if *ago > 0 {
		body["at"] = time.Now().Add(-*ago).UTC().Format(time.RFC3339Nano)
	}
	var resp struct {
		Incident incidentDTO `json:"incident"`
		Analysis analysisDTO `json:"analysis"`
	}
	if err := apiCallBody(c, "POST", "/api/v1/incidents", body, &resp); err != nil {
		return err
	}
	fmt.Printf("relato registrado (%s) às %s\n", symptomLabel[resp.Incident.Symptom],
		resp.Incident.ReportedAt.Local().Format("15:04:05"))
	fmt.Printf("a análise usa as medições de %s a %s; veja com `pinglume incidents --id %s`\n",
		resp.Analysis.WindowFrom.Local().Format("15:04:05"), resp.Analysis.WindowTo.Local().Format("15:04:05"), resp.Incident.ID)
	return nil
}

func cmdIncidents(args []string) error {
	fs := flag.NewFlagSet("incidents", flag.ExitOnError)
	var c common
	c.register(fs)
	window := fs.Duration("window", 24*time.Hour, "período listado")
	id := fs.String("id", "", "mostra a análise completa de um relato")
	fs.Parse(args)

	if *id != "" {
		var resp struct {
			Incident incidentDTO `json:"incident"`
			Analysis analysisDTO `json:"analysis"`
		}
		if err := apiCall(c, "GET", "/api/v1/incidents/"+*id, &resp); err != nil {
			return err
		}
		printAnalysis(resp.Incident, resp.Analysis)
		return nil
	}
	var list []incidentDTO
	if err := apiCall(c, "GET", fmt.Sprintf("/api/v1/incidents?window=%s", window), &list); err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Printf("nenhum relato nas últimas %s\n", window)
		return nil
	}
	for _, inc := range list {
		fmt.Printf("%s  %-16s  %-26s  %s\n", inc.ReportedAt.Local().Format("02/01 15:04:05"),
			symptomLabel[inc.Symptom], verdictLabel[inc.Verdict], inc.ID)
	}
	return nil
}

func printAnalysis(inc incidentDTO, a analysisDTO) {
	fmt.Printf("Relato: %s às %s", symptomLabel[inc.Symptom], inc.ReportedAt.Local().Format("02/01 15:04:05"))
	if inc.Note != "" {
		fmt.Printf(" — %q", inc.Note)
	}
	fmt.Printf("\nJanela analisada: %s–%s · regra %s\n", a.WindowFrom.Local().Format("15:04:05"),
		a.WindowTo.Local().Format("15:04:05"), a.RuleVersion)
	fmt.Printf("Resultado: %s\n\n", strings.ToUpper(verdictLabel[a.Verdict]))
	fmt.Println("Observações:")
	for _, f := range a.Findings {
		fmt.Printf("  • %s\n    [%s]\n", f.Text, strings.Join(f.Inputs, "; "))
	}
	if len(a.Alternatives) > 0 {
		fmt.Println("\nOutras explicações possíveis:")
		for _, x := range a.Alternatives {
			fmt.Printf("  • %s\n", x)
		}
	}
	if a.NextTest != "" {
		fmt.Printf("\nPróximo teste: %s\n", a.NextTest)
	}
	if len(a.Caveats) > 0 {
		fmt.Println("\nLimites:")
		for _, x := range a.Caveats {
			fmt.Printf("  • %s\n", x)
		}
	}
}
