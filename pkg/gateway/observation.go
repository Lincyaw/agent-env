package gateway

import "strings"

const defaultObservationPreviewBytes = 4096

func (g *Gateway) observationPreviewLimit() int {
	limit := g.gwConfig.ObservationPreviewBytes
	if limit < 0 {
		limit = 0
	}
	if limit == 0 {
		limit = defaultObservationPreviewBytes
	}
	return limit
}

func (g *Gateway) retainedStepOutput(output StepOutput) (StepOutput, int, bool) {
	total := len(output.Stdout) + len(output.Stderr)
	if g.gwConfig.FullObservationEnabled {
		return output, total, false
	}
	limit := g.observationPreviewLimit()
	if total <= limit {
		return output, total, false
	}
	return truncateStepOutput(output, limit), total, true
}

func truncateStepOutput(output StepOutput, limit int) StepOutput {
	if limit <= 0 {
		return StepOutput{ExitCode: output.ExitCode}
	}
	stdoutLimit := limit / 2
	stderrLimit := limit - stdoutLimit
	if len(output.Stdout) < stdoutLimit {
		stderrLimit += stdoutLimit - len(output.Stdout)
		stdoutLimit = len(output.Stdout)
	}
	if len(output.Stderr) < stderrLimit {
		stdoutLimit += stderrLimit - len(output.Stderr)
		stderrLimit = len(output.Stderr)
	}
	return StepOutput{
		Stdout:   truncateStringBytes(output.Stdout, stdoutLimit),
		Stderr:   truncateStringBytes(output.Stderr, stderrLimit),
		ExitCode: output.ExitCode,
	}
}

func truncateStringBytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	return strings.Clone(value[:limit])
}
