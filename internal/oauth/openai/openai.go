// Package openai provides functions to handle OpenAI OAuth device flow
// authentication, following the same flow as OpenAI's Codex CLI.
package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zhiqiang-hhhh/smith/internal/oauth"
)

const (
	clientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	defaultIssuer = "https://auth.openai.com"
	userAgent     = "Smith/1.0"
	// CodexBaseURL is the base URL for the ChatGPT Codex API endpoint.
	CodexBaseURL = "https://chatgpt.com/backend-api/codex"
	// MaxPollTimeout is the maximum time to wait for user authorization.
	MaxPollTimeout = 15 * time.Minute
)

// DeviceCode contains the response from requesting a device code.
type DeviceCode struct {
	DeviceAuthID    string      `json:"device_auth_id"`
	UserCode        string      `json:"user_code"`
	VerificationURL string      `json:"verification_url"`
	RawInterval     json.Number `json:"interval"`
	Interval        int         `json:"-"`
}

// RequestDeviceCode initiates the device code flow with OpenAI.
func RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	apiURL := defaultIssuer + "/api/accounts/deviceauth/usercode"

	body, err := json.Marshal(map[string]string{
		"client_id": clientID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("device code login is not enabled for this server")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed: status %d, body %q", resp.StatusCode, string(respBody))
	}

	var dc DeviceCode
	if err := json.Unmarshal(respBody, &dc); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if dc.RawInterval.String() != "" {
		n, err := strconv.Atoi(dc.RawInterval.String())
		if err != nil {
			return nil, fmt.Errorf("parse interval %q: %w", dc.RawInterval, err)
		}
		dc.Interval = n
	}

	dc.VerificationURL = defaultIssuer + "/codex/device"

	if dc.Interval < 5 {
		dc.Interval = 5
	}

	return &dc, nil
}

// codeSuccessResp is returned when polling succeeds.
type codeSuccessResp struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeChallenge     string `json:"code_challenge"`
	CodeVerifier      string `json:"code_verifier"`
}

// PollForToken polls the OpenAI device auth endpoint until the user
// completes authorization. It then exchanges the authorization code for
// tokens via PKCE and returns an OAuth token with an API key as the access
// token.
func PollForToken(ctx context.Context, dc *DeviceCode) (*oauth.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, MaxPollTimeout)
	defer cancel()

	interval := time.Duration(dc.Interval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	apiURL := defaultIssuer + "/api/accounts/deviceauth/token"

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("device auth timed out")
		case <-ticker.C:
		}

		body, err := json.Marshal(map[string]string{
			"device_auth_id": dc.DeviceAuthID,
			"user_code":      dc.UserCode,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", userAgent)

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("execute request: %w", err)
		}

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			var codeResp codeSuccessResp
			if err := json.Unmarshal(respBody, &codeResp); err != nil {
				return nil, fmt.Errorf("unmarshal token response: %w", err)
			}

			return exchangeCodeForTokens(ctx, &codeResp)
		}

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			continue
		}

		return nil, fmt.Errorf("device auth failed: status %d, body %q", resp.StatusCode, string(respBody))
	}
}

// exchangeCodeForTokens exchanges the authorization code for tokens
// using PKCE and returns an OAuth token with the access token for use
// with the ChatGPT Codex endpoint.
func exchangeCodeForTokens(ctx context.Context, codeResp *codeSuccessResp) (*oauth.Token, error) {
	redirectURI := defaultIssuer + "/deviceauth/callback"

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {codeResp.AuthorizationCode},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {codeResp.CodeVerifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, defaultIssuer+"/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: status %d, body %q", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("unmarshal token response: %w", err)
	}

	accountID := extractAccountID(tokenResp.IDToken)
	if accountID == "" {
		accountID = extractAccountID(tokenResp.AccessToken)
	}

	token := &oauth.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
		AccountID:    accountID,
	}
	token.SetExpiresAt()

	return token, nil
}

// extractAccountID parses the JWT payload and extracts the ChatGPT
// account ID used for the ChatGPT-Account-Id header.
func extractAccountID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}

	var claims struct {
		Auth *struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
		Organizations []struct {
			ID string `json:"id"`
		} `json:"organizations"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}

	if claims.Auth != nil && claims.Auth.AccountID != "" {
		return claims.Auth.AccountID
	}
	if len(claims.Organizations) > 0 {
		return claims.Organizations[0].ID
	}

	return ""
}

// RefreshToken refreshes an OpenAI OAuth token using the refresh token.
func RefreshToken(ctx context.Context, refreshToken string) (*oauth.Token, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, defaultIssuer+"/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed: status %d, body %q", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("unmarshal refresh response: %w", err)
	}

	newRefreshToken := tokenResp.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = refreshToken
	}

	accountID := extractAccountID(tokenResp.IDToken)
	if accountID == "" {
		accountID = extractAccountID(tokenResp.AccessToken)
	}

	token := &oauth.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: newRefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
		AccountID:    accountID,
	}
	token.SetExpiresAt()

	return token, nil
}
