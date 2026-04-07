package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
)

const modelsURL = "https://api.githubcopilot.com/models"

type apiModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Vendor       string `json:"vendor"`
	Capabilities struct {
		Type   string `json:"type"`
		Limits struct {
			MaxContextWindowTokens int `json:"max_context_window_tokens"`
			MaxOutputTokens        int `json:"max_output_tokens"`
		} `json:"limits"`
		Supports struct {
			Vision bool `json:"vision"`
		} `json:"supports"`
	} `json:"capabilities"`
}

type modelsResponse struct {
	Data []apiModel `json:"data"`
}

var allowedVendors = map[string]bool{
	"Anthropic": true,
	"OpenAI":    true,
	"Google":    true,
}

var reasoningModels = map[string]bool{
	"o1": true, "o1-mini": true, "o1-preview": true,
	"o3": true, "o3-mini": true, "o4-mini": true,
}

func isReasoningModel(id string) bool {
	if reasoningModels[id] {
		return true
	}
	for prefix := range reasoningModels {
		if strings.HasPrefix(id, prefix+"-") {
			return true
		}
	}
	return strings.Contains(id, "thinking")
}

// FetchModels fetches the available model list from the Copilot API
// using the given Copilot bearer token, and returns only chat models
// from allowed vendors (Anthropic, OpenAI, Google).
func FetchModels(ctx context.Context, copilotToken string) ([]catwalk.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+copilotToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Openai-Intent", "conversation-panel")
	for k, v := range Headers() {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot models request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("copilot /models returned %d: %s", resp.StatusCode, body)
	}

	var mr modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	var models []catwalk.Model
	for _, m := range mr.Data {
		if !allowedVendors[m.Vendor] || m.Capabilities.Type != "chat" {
			continue
		}
		cm := catwalk.Model{
			ID:               m.ID,
			Name:             m.Name,
			ContextWindow:    int64(m.Capabilities.Limits.MaxContextWindowTokens),
			DefaultMaxTokens: int64(m.Capabilities.Limits.MaxOutputTokens),
			SupportsImages:   m.Capabilities.Supports.Vision,
		}
		if isReasoningModel(m.ID) {
			cm.CanReason = true
		}
		models = append(models, cm)
	}
	return models, nil
}
