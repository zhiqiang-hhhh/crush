// Package copilot provides GitHub Copilot integration.
package copilot

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/charmbracelet/crush/internal/log"
)

var assistantRolePattern = regexp.MustCompile(`"role"\s*:\s*"assistant"`)

// TokenFunc returns the current bearer token for Copilot API requests.
type TokenFunc func() string

// NewClient creates a new HTTP client with a custom transport that adds the
// X-Initiator header based on message history in the request body.
// If tokenFn is non-nil, it overrides the Authorization header on every
// request with the latest token, allowing transparent token refresh during
// long-running agent loops.
func NewClient(isSubAgent, debug bool, tokenFn TokenFunc) *http.Client {
	return &http.Client{
		Transport: &initiatorTransport{debug: debug, isSubAgent: isSubAgent, tokenFn: tokenFn},
	}
}

type initiatorTransport struct {
	debug      bool
	isSubAgent bool
	tokenFn    TokenFunc
}

func (t *initiatorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	const (
		xInitiatorHeader = "X-Initiator"
		userInitiator    = "user"
		agentInitiator   = "agent"
	)

	if req == nil {
		return nil, fmt.Errorf("HTTP request is nil")
	}

	// Override Authorization with latest token if available.
	if t.tokenFn != nil {
		if token := t.tokenFn(); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	if req.Body == http.NoBody {
		// No body to inspect; default to user.
		req.Header.Set(xInitiatorHeader, userInitiator)
		slog.Debug("Setting X-Initiator header to user (no request body)")
		return t.roundTrip(req)
	}

	// Clone request to avoid modifying the original.
	req = req.Clone(req.Context())

	// Read the original body into bytes so we can examine it.
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	defer req.Body.Close()

	// Restore the original body using the preserved bytes.
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Check for assistant messages using regex to handle whitespace
	// variations in the JSON while avoiding full unmarshalling overhead.
	initiator := userInitiator
	if assistantRolePattern.Match(bodyBytes) || t.isSubAgent {
		slog.Debug("Setting X-Initiator header to agent (found assistant messages in history)")
		initiator = agentInitiator
	} else {
		slog.Debug("Setting X-Initiator header to user (no assistant messages)")
	}
	req.Header.Set(xInitiatorHeader, initiator)

	return t.roundTrip(req)
}

func (t *initiatorTransport) roundTrip(req *http.Request) (*http.Response, error) {
	if t.debug {
		return log.NewHTTPClient().Transport.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}
