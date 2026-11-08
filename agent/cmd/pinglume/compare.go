package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"pinglume/agent/internal/compare"
	"pinglume/agent/internal/store"
)

var compareVerdictLabel = map[string]string{
	compare.Better:        "melhora",
	compare.Worse:         "piora",
	compare.NoDifference:  "sem diferença detectável",
	compare.Inconclusive:  "inconclusivo",
	compare.NotComparable: "não comparável",
	compare.Insufficient:  "dados insuficientes",
}

var comparePhaseLabel = map[string]string{
	compare.PhaseBaseline:     "janela A em andamento",
	compare.PhaseAwaiting:     "janela A concluída; falta iniciar a janela B",
	compare.PhaseIntervention: "janela B em andamento",
	compare.PhaseDone:         "concluída",
	compare.PhaseCancelled:    "cancelada",
}

var compareKindLabel = map[string]string{"tcp": "TCP", "dns": "DNS", "https": "HTTPS", "udp_echo": "UDP"}

type experimentDTO struct {
	store.Experiment
	Phase  string          `json:"phase"`
	Result *compare.Result `json:"result"`
}

func cmdCompare(args []string) error {
	if len(args) == 0 {
		compareUsage()
		return errors.New("falta o subcomando")
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "new":
		return compareNew(rest)
	case "status":
		return compareStatus(rest)
	case "next":
		return compareNext(rest)
	case "cancel":
		return compareCancel(rest)
	}
	compareUsage()
	return fmt.Errorf("subcomando desconhecido: %s", verb)
}

func compareUsage() {
	fmt.Println(`uso: pinglume compare <subcomando> [opções]

subcomandos:
  new      começa uma comparação: a janela A (situação atual) abre agora
  status   mostra a etapa e, quando concluída, o resultado
  next     abre a janela B depois que a janela A terminou
  cancel   cancela a comparação em andamento`)
}

func compareNew(args []string) error {
	fs := flag.NewFlagSet("compare new", flag.ExitOnError)
	var c common
	c.register(fs)
	question := fs.String("question", "", "a pergunta, ex.: \"o cabo melhora as quedas?\"")
	change := fs.String("change", "", "a mudança que será testada, ex.: \"trocar o Wi-Fi pelo cabo\"")
	window := fs.Duration("window", 10*time.Minute, "duração de cada janela, de 10m a 60m, em minutos inteiros")
	condition := fs.String("condition", "", "situação da janela A, ex.: \"Wi-Fi, sala\"")
	fs.Parse(args)
	c.resolve()

	if *question == "" || *change == "" {
		return errors.New("--question e --change são obrigatórios")
	}
	if *window%time.Minute != 0 || *window < compare.MinWindow || *window > time.Hour {
		return errors.New("--window deve ter minutos inteiros entre 10m e 60m")
	}
	var e experimentDTO
	body := map[string]any{"question": *question, "intervention": *change,
		"windowMinutes": int(*window / time.Minute), "condition": *condition}
	if err := apiCallBody(c, "POST", "/api/v1/experiments", body, &e); err != nil {
		return err
	}
	fmt.Printf("comparação criada (%s)\n", e.ID)
	printExperiment(os.Stdout, e)
	fmt.Println("\nMantenha o computador ligado e acordado até o fim da janela A; depois rode `pinglume compare next`.")
	return nil
}

func compareStatus(args []string) error {
	fs := flag.NewFlagSet("compare status", flag.ExitOnError)
	var c common
	c.register(fs)
	id := fs.String("id", "", "identificador da comparação (padrão: a mais recente)")
	fs.Parse(args)
	c.resolve()

	e, err := pickExperiment(c, *id)
	if err != nil {
		return err
	}
	printExperiment(os.Stdout, e)
	return nil
}

func compareNext(args []string) error {
	fs := flag.NewFlagSet("compare next", flag.ExitOnError)
	var c common
	c.register(fs)
	condition := fs.String("condition", "", "situação da janela B, ex.: \"cabo, sala\"")
	fs.Parse(args)
	c.resolve()

	cur, err := pickExperiment(c, "")
	if err != nil {
		return err
	}
	var e experimentDTO
	if err := apiCallBody(c, "POST", "/api/v1/experiments/"+cur.ID+"/intervention",
		map[string]string{"condition": *condition}, &e); err != nil {
		return err
	}
	fmt.Println("janela B iniciada; faça a mudança agora e mantenha o computador acordado")
	printExperiment(os.Stdout, e)
	return nil
}

func compareCancel(args []string) error {
	fs := flag.NewFlagSet("compare cancel", flag.ExitOnError)
	var c common
	c.register(fs)
	fs.Parse(args)
	c.resolve()

	cur, err := pickExperiment(c, "")
	if err != nil {
		return err
	}
	var e experimentDTO
	if err := apiCall(c, "POST", "/api/v1/experiments/"+cur.ID+"/cancel", &e); err != nil {
		return err
	}
	fmt.Println("comparação cancelada")
	return nil
}

func pickExperiment(c common, id string) (experimentDTO, error) {
	var e experimentDTO
	if id != "" {
		return e, apiCall(c, "GET", "/api/v1/experiments/"+id, &e)
	}
	var list []experimentDTO
	if err := apiCall(c, "GET", "/api/v1/experiments", &list); err != nil {
		return e, err
	}
	if len(list) == 0 {
		return e, errors.New("nenhuma comparação ainda; comece com `pinglume compare new`")
	}
	return list[0], nil
}

func printExperiment(w io.Writer, e experimentDTO) {
	fmt.Fprintf(w, "Pergunta: %s\nMudança testada: %s\n", e.Question, e.Intervention)
	fmt.Fprintf(w, "Etapa: %s\n", comparePhaseLabel[e.Phase])
	now := time.Now()
	for _, win := range e.Windows {
		role := map[string]string{"baseline": "A", "intervention": "B"}[win.Role]
		end := win.PlannedEnd
		if win.End != nil {
			end = *win.End
		}
		line := fmt.Sprintf("Janela %s: %s a %s", role, win.Start.Local().Format("15:04:05"), end.Local().Format("15:04:05"))
		if left := end.Sub(now); left > 0 && e.Phase != compare.PhaseCancelled {
			line += fmt.Sprintf(" (faltam %s)", left.Round(time.Second))
		}
		fmt.Fprintln(w, line)
		for _, cd := range win.Conditions {
			origin := map[string]string{"declared": "declarada", "detected": "detectada"}[cd.Source]
			fmt.Fprintf(w, "  %s: %s (%s)\n", cd.Key, cd.Value, origin)
		}
	}
	if e.Result != nil {
		printCompareResult(w, *e.Result)
	}
}

func printCompareResult(w io.Writer, r compare.Result) {
	fmt.Fprintf(w, "\nResultado: %s · regra %s\n", strings.ToUpper(compareVerdictLabel[r.Verdict]), r.RuleVersion)
	for _, p := range r.Probes {
		fmt.Fprintf(w, "  %s (%s): %s\n", compareKindLabel[p.Kind], p.Endpoint, compareVerdictLabel[p.Verdict])
	}
	if len(r.Findings) > 0 {
		fmt.Fprintln(w, "\nObservações:")
		for _, f := range r.Findings {
			fmt.Fprintf(w, "  • %s\n    [%s]\n", f.Text, strings.Join(f.Inputs, "; "))
		}
	}
	if len(r.Alternatives) > 0 {
		fmt.Fprintln(w, "\nOutras explicações possíveis:")
		for _, x := range r.Alternatives {
			fmt.Fprintf(w, "  • %s\n", x)
		}
	}
	if r.NextTest != "" {
		fmt.Fprintf(w, "\nPróximo teste: %s\n", r.NextTest)
	}
	if len(r.Caveats) > 0 {
		fmt.Fprintln(w, "\nLimites:")
		for _, x := range r.Caveats {
			fmt.Fprintf(w, "  • %s\n", x)
		}
	}
}
