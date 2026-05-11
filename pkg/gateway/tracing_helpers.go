package gateway

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// traceStartAttrs builds a SpanStartOption from alternating key/value strings.
// Empty values are skipped to keep the span tidy when fields are optional.
func traceStartAttrs(kv ...string) trace.SpanStartOption {
	attrs := make([]attribute.KeyValue, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == "" {
			continue
		}
		attrs = append(attrs, attribute.String(kv[i], kv[i+1]))
	}
	return trace.WithAttributes(attrs...)
}

func recordSpanErr(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
