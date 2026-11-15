package analyzer

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// FormatJSON serializa el Report en JSON con indentación legible.
func FormatJSON(w io.Writer, rep *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

// FormatText imprime un reporte de migrabilidad claro y accionable para terminal.
func FormatText(w io.Writer, rep *Report) {
	fmt.Fprintf(w, "gnofy analyze — Reporte de migrabilidad Solidity → Gno\n")
	fmt.Fprintf(w, "Compilador: %s | Archivos: %d | Contratos: %d\n\n",
		rep.Compiler, len(rep.Files), len(rep.Contracts))

	if len(rep.Contracts) > 0 {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "CONTRATO\tTIPO\tFUNCS (OK/TOTAL)\tWARNS\tHOLES\tDETERMINISTA\tMODO RECOMENDADO")
		fmt.Fprintln(tw, "--------\t----\t----------------\t-----\t-----\t------------\t----------------")
		for _, c := range rep.Contracts {
			fmt.Fprintf(tw, "%s\t%s\t%d/%d\t%d\t%d\t%.1f%%\t%s\n",
				c.Name, c.Kind, c.CleanFunctions, c.TotalFunctions,
				c.WarnCount, c.HoleCount, c.DeterministicPct, c.RecommendedMode)
		}
		_ = tw.Flush()
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Resumen global: %.1f%% determinista (%d/%d funciones limpias, %d warns, %d holes)\n",
		rep.DeterministicPct, rep.CleanFunctions, rep.TotalFunctions, rep.WarnCount, rep.HoleCount)

	if len(rep.DetectedStandards) > 0 {
		fmt.Fprintf(w, "Estándares / librerías detectadas: %s\n", strings.Join(rep.DetectedStandards, ", "))
	}

	if len(rep.Findings) == 0 {
		fmt.Fprintln(w, "\nSin advertencias ni bloqueos. Listo para transpilación determinista.")
		return
	}

	holes := filterFindings(rep.Findings, LevelHole)
	warns := filterFindings(rep.Findings, LevelWarn)

	if len(holes) > 0 {
		fmt.Fprintf(w, "\n[HOLES — %d] Constructos sin traducción determinista 1:1:\n", len(holes))
		for _, f := range holes {
			fmt.Fprintf(w, "  - %-26s %s\n    %s\n", f.Code, f.Location, f.Message)
			if f.Suggestion != "" {
				fmt.Fprintf(w, "    → %s\n", f.Suggestion)
			}
		}
	}

	if len(warns) > 0 {
		fmt.Fprintf(w, "\n[WARNS — %d] Matices semánticos a revisar:\n", len(warns))
		for _, f := range warns {
			fmt.Fprintf(w, "  - %-26s %s\n    %s\n", f.Code, f.Location, f.Message)
			if f.Suggestion != "" {
				fmt.Fprintf(w, "    → %s\n", f.Suggestion)
			}
		}
	}
}

func filterFindings(all []Finding, level Level) []Finding {
	var out []Finding
	for _, f := range all {
		if f.Level == level {
			out = append(out, f)
		}
	}
	return out
}
