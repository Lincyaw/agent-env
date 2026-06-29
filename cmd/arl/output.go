package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"
)

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
}

func colorEnabled() bool {
	return !flagNoColor && term.IsTerminal(int(os.Stdout.Fd()))
}

func age(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		return fmt.Sprintf("%dd%dh", days, h)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func conditionSummary(conditions []PoolCondition) string {
	for _, c := range conditions {
		if c.Type == "Ready" {
			if c.Status == "True" {
				return "Ready"
			}
			return c.Reason
		}
	}
	if len(conditions) == 0 {
		return "Unknown"
	}
	return conditions[0].Type + "=" + conditions[0].Status
}

func shortImage(image string) string {
	parts := strings.Split(image, "/")
	last := parts[len(parts)-1]
	return truncate(last, 40)
}

func printExecResults(results []StepResult) error {
	for _, r := range results {
		if r.Output.Stdout != "" {
			fmt.Print(r.Output.Stdout)
		}
		if r.Output.Stderr != "" {
			fmt.Fprint(os.Stderr, r.Output.Stderr)
		}
		if r.Output.ExitCode != 0 {
			return fmt.Errorf("exit code %d", r.Output.ExitCode)
		}
	}
	return nil
}
