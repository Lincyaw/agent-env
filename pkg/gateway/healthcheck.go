package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CheckResult represents the outcome of a single health check.
type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "ok" or "warn"
	Message string `json:"message,omitempty"`
}

// HealthReport is the JSON response for /debug/health.
type HealthReport struct {
	Goroutines int                           `json:"goroutines"`
	Sessions   int                           `json:"sessions"`
	Allocator  map[string]AllocatorPoolStats `json:"allocator"`
	Checks     []CheckResult                 `json:"checks"`
}

// HealthChecker performs periodic health inspections of the gateway.
type HealthChecker struct {
	gw              *Gateway
	metrics         interfaces.MetricsCollector
	goroutineWindow []int
	windowSize      int
	interval        time.Duration
	feishuURL       string
	stopCh          chan struct{}
	wg              sync.WaitGroup
	imagePullStarts map[string]time.Time
	imagePullSeen   map[types.UID]struct{}
}

var imagePullMessageRE = regexp.MustCompile(`image "([^"]+)"`)

// NewHealthChecker creates a new HealthChecker.
func NewHealthChecker(gw *Gateway, metrics interfaces.MetricsCollector, feishuURL string) *HealthChecker {
	return &HealthChecker{
		gw:              gw,
		metrics:         metrics,
		windowSize:      5,
		interval:        60 * time.Second,
		feishuURL:       feishuURL,
		stopCh:          make(chan struct{}),
		imagePullStarts: make(map[string]time.Time),
		imagePullSeen:   make(map[types.UID]struct{}),
	}
}

// Start launches the background health check goroutine.
func (hc *HealthChecker) Start() {
	hc.wg.Add(1)
	go hc.loop()
}

// Stop signals the health check goroutine to exit and waits.
func (hc *HealthChecker) Stop() {
	close(hc.stopCh)
	hc.wg.Wait()
}

func (hc *HealthChecker) loop() {
	defer hc.wg.Done()
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	// Run once immediately
	hc.collect()

	for {
		select {
		case <-hc.stopCh:
			return
		case <-ticker.C:
			hc.collect()
		}
	}
}

func (hc *HealthChecker) collect() {
	// 1. Goroutines
	goroutines := runtime.NumGoroutine()
	if hc.metrics != nil {
		hc.metrics.SetGatewayGoroutines(goroutines)
	}
	hc.goroutineWindow = append(hc.goroutineWindow, goroutines)
	if len(hc.goroutineWindow) > hc.windowSize {
		hc.goroutineWindow = hc.goroutineWindow[len(hc.goroutineWindow)-hc.windowSize:]
	}

	// 2. Session count from store
	sessionCount := 0
	hc.gw.store.Range(func(_ string, _ *session) bool {
		sessionCount++
		return true
	})
	if hc.metrics != nil {
		hc.metrics.SetGatewaySessionsTotal(sessionCount)
	}

	// 3. Runtime allocator stats
	allocStats := hc.gw.allocatorDiagnosticStats()
	for pool, stats := range allocStats {
		if hc.metrics != nil {
			hc.metrics.SetIdleQueueDepth(pool, stats.IdleCount)
			hc.metrics.SetPendingWaiters(pool, stats.WaiterCount)
		}
	}
	if hc.metrics != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := hc.gw.publishCurrentPoolMetrics(ctx); err != nil {
			log.Printf("Warning: failed to publish pool metrics: %v", err)
		}
		if err := hc.collectImagePullMetrics(ctx); err != nil {
			log.Printf("Warning: failed to collect image pull metrics: %v", err)
		}
		cancel()
	}

	// 4. Cleanup stale gRPC connections (Shutdown/TransientFailure)
	if cleaned := hc.gw.CleanupStaleConnections(); cleaned > 0 {
		log.Printf("Cleaned up %d stale sidecar connections", cleaned)
	}
}

func (hc *HealthChecker) collectImagePullMetrics(ctx context.Context) error {
	if hc.gw == nil || hc.gw.k8sClient == nil || hc.metrics == nil {
		return nil
	}
	namespace := hc.gw.runtimeNamespace()
	var events corev1.EventList
	if err := hc.gw.k8sClient.List(ctx, &events, client.InNamespace(namespace)); err != nil {
		return err
	}
	sort.SliceStable(events.Items, func(i, j int) bool {
		return eventTimestamp(events.Items[i]).Before(eventTimestamp(events.Items[j]))
	})
	for i := range events.Items {
		event := &events.Items[i]
		if event.InvolvedObject.Kind != "Pod" || event.InvolvedObject.Name == "" {
			continue
		}
		image := imageFromPullEventMessage(event.Message)
		if image == "" {
			continue
		}
		eventTime := eventTimestamp(*event)
		if eventTime.IsZero() {
			continue
		}
		key := event.InvolvedObject.Namespace + "/" + event.InvolvedObject.Name + "/" + image
		switch event.Reason {
		case "Pulling":
			if _, ok := hc.imagePullStarts[key]; !ok {
				hc.imagePullStarts[key] = eventTime
			}
		case "Pulled":
			if _, seen := hc.imagePullSeen[event.UID]; seen {
				continue
			}
			start, ok := hc.imagePullStarts[key]
			if !ok || eventTime.Before(start) {
				continue
			}
			hc.metrics.RecordImagePullDuration(image, eventTime.Sub(start))
			hc.imagePullSeen[event.UID] = struct{}{}
			delete(hc.imagePullStarts, key)
		}
	}
	return nil
}

func imageFromPullEventMessage(message string) string {
	match := imagePullMessageRE.FindStringSubmatch(message)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func eventTimestamp(event corev1.Event) time.Time {
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.FirstTimestamp.IsZero() {
		return event.FirstTimestamp.Time
	}
	return event.CreationTimestamp.Time
}

// BuildReport constructs a full health report.
func (hc *HealthChecker) BuildReport() HealthReport {
	goroutines := runtime.NumGoroutine()

	sessionCount := 0
	hc.gw.store.Range(func(_ string, _ *session) bool {
		sessionCount++
		return true
	})

	allocStats := hc.gw.allocatorDiagnosticStats()

	checks := hc.runChecks(sessionCount)

	return HealthReport{
		Goroutines: goroutines,
		Sessions:   sessionCount,
		Allocator:  allocStats,
		Checks:     checks,
	}
}

func (hc *HealthChecker) runChecks(sessionCount int) []CheckResult {
	var checks []CheckResult

	// Check: goroutine trend (all samples in sliding window monotonically increasing)
	check := CheckResult{Name: "goroutine_trend", Status: "ok"}
	if len(hc.goroutineWindow) >= hc.windowSize {
		allIncreasing := true
		for i := 1; i < len(hc.goroutineWindow); i++ {
			if hc.goroutineWindow[i] <= hc.goroutineWindow[i-1] {
				allIncreasing = false
				break
			}
		}
		if allIncreasing {
			check.Status = "warn"
			check.Message = fmt.Sprintf("goroutine count has been monotonically increasing over %d samples: %v", hc.windowSize, hc.goroutineWindow)
		}
	}
	checks = append(checks, check)

	// Check: session count consistency
	atomicCount := hc.gw.store.Count()
	check = CheckResult{Name: "session_count_consistency", Status: "ok"}
	if int64(sessionCount) != atomicCount {
		check.Status = "warn"
		check.Message = fmt.Sprintf("range count (%d) != store counter (%d)", sessionCount, atomicCount)
	}
	checks = append(checks, check)

	return checks
}

// HandleDebugHealth is the HTTP handler for GET /debug/health.
func (hc *HealthChecker) HandleDebugHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		report := hc.BuildReport()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(report)
	}
}

// --- Feishu Webhook Adapter ---

// alertManagerPayload represents the AlertManager webhook payload.
type alertManagerPayload struct {
	Status string              `json:"status"`
	Alerts []alertManagerAlert `json:"alerts"`
}

type alertManagerAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
}

// feishuCardMessage builds a Feishu interactive card message from alerts.
type feishuCardMessage struct {
	MsgType string     `json:"msg_type"`
	Card    feishuCard `json:"card"`
}

type feishuCard struct {
	Header   feishuCardHeader    `json:"header"`
	Elements []feishuCardElement `json:"elements"`
}

type feishuCardHeader struct {
	Title    feishuText `json:"title"`
	Template string     `json:"template"`
}

type feishuText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type feishuCardElement struct {
	Tag     string      `json:"tag"`
	Content *feishuText `json:"content,omitempty"`
	Text    *feishuText `json:"text,omitempty"`
}

// HandleAlertManagerWebhook receives AlertManager webhook and forwards to Feishu.
func (hc *HealthChecker) HandleAlertManagerWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hc.feishuURL == "" {
			writeError(w, http.StatusServiceUnavailable, "feishu webhook URL not configured")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read body")
			return
		}

		var payload alertManagerPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			writeError(w, http.StatusBadRequest, "invalid AlertManager payload")
			return
		}

		card := hc.buildFeishuCard(payload)
		cardJSON, err := json.Marshal(card)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to marshal feishu card")
			return
		}

		resp, err := http.Post(hc.feishuURL, "application/json", bytes.NewReader(cardJSON))
		if err != nil {
			log.Printf("Warning: failed to send feishu alert: %v", err)
			writeError(w, http.StatusBadGateway, "failed to send to feishu")
			return
		}
		defer resp.Body.Close()

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}
}

func (hc *HealthChecker) buildFeishuCard(payload alertManagerPayload) feishuCardMessage {
	template := "red"
	title := fmt.Sprintf("[FIRING:%d] ARL Alert", len(payload.Alerts))
	if payload.Status == "resolved" {
		template = "green"
		title = fmt.Sprintf("[RESOLVED:%d] ARL Alert", len(payload.Alerts))
	}

	var lines []string
	for _, alert := range payload.Alerts {
		name := alert.Labels["alertname"]
		severity := alert.Labels["severity"]
		summary := alert.Annotations["summary"]
		description := alert.Annotations["description"]
		status := alert.Status

		line := fmt.Sprintf("**%s** [%s] `%s`", name, severity, status)
		if summary != "" {
			line += "\n" + summary
		}
		if description != "" {
			line += "\n" + description
		}
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n---\n")

	return feishuCardMessage{
		MsgType: "interactive",
		Card: feishuCard{
			Header: feishuCardHeader{
				Title:    feishuText{Tag: "plain_text", Content: title},
				Template: template,
			},
			Elements: []feishuCardElement{
				{
					Tag:  "markdown",
					Text: &feishuText{Tag: "lark_md", Content: content},
				},
			},
		},
	}
}
