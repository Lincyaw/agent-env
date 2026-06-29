package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type logEntry struct {
	PodName   string `json:"podName,omitempty"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Source    string `json:"source"`
}

func streamLogs(c *Client, path string, follow bool, tail int, showPod bool) error {
	params := url.Values{}
	if follow {
		params.Set("follow", "true")
	}
	if tail > 0 {
		params.Set("tail", fmt.Sprintf("%d", tail))
	}
	if flagNamespace != "default" {
		params.Set("namespace", flagNamespace)
	}
	fullPath := path
	if len(params) > 0 {
		fullPath += "?" + params.Encode()
	}

	req, err := http.NewRequest("GET", c.base+fullPath, nil)
	if err != nil {
		return err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	// Use a long-lived client for streaming
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return responseError(resp)
	}

	if flagOutput == "json" {
		// Pass through raw NDJSON
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
		return scanner.Err()
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var entry logEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			fmt.Fprintln(os.Stderr, scanner.Text())
			continue
		}

		ts := shortTimestamp(entry.Timestamp)
		level := formatLevel(entry.Level)

		if showPod && entry.PodName != "" {
			fmt.Printf("%s %s [%s] %s: %s\n", ts, level, entry.Source, entry.PodName, entry.Message)
		} else {
			fmt.Printf("%s %s [%s] %s\n", ts, level, entry.Source, entry.Message)
		}
	}
	return scanner.Err()
}

func shortTimestamp(ts string) string {
	if len(ts) >= 19 {
		return ts[11:19] // HH:MM:SS
	}
	return ts
}

func formatLevel(level string) string {
	label := "INF"
	switch strings.ToLower(level) {
	case "error":
		label = "ERR"
	case "warn":
		label = "WRN"
	}
	if !colorEnabled() {
		return label
	}
	switch label {
	case "ERR":
		return "\033[31mERR\033[0m"
	case "WRN":
		return "\033[33mWRN\033[0m"
	default:
		return "\033[36mINF\033[0m"
	}
}
