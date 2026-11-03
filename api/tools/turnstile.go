package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileSiteVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type turnstileSiteVerifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// VerifyTurnstile validates a Turnstile response token with Cloudflare siteverify.
func VerifyTurnstile(ctx context.Context, secretKey, token, remoteIP string) error {
	token = strings.TrimSpace(token)
	if secretKey == "" {
		return fmt.Errorf("turnstile secret key is empty")
	}
	if token == "" {
		return fmt.Errorf("turnstile token is empty")
	}

	form := url.Values{}
	form.Set("secret", secretKey)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileSiteVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create turnstile siteverify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("turnstile siteverify request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read turnstile siteverify response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("turnstile siteverify returned status %d", resp.StatusCode)
	}

	var parsed turnstileSiteVerifyResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("decode turnstile siteverify response: %w", err)
	}
	if !parsed.Success {
		if len(parsed.ErrorCodes) > 0 {
			return fmt.Errorf("turnstile verification failed: %s", strings.Join(parsed.ErrorCodes, ", "))
		}
		return fmt.Errorf("turnstile verification failed")
	}
	return nil
}
