// Package apim provides clients for WSO2 APIM Publisher and Service Catalog APIs.
package apim

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wso2/adc/internal/config"
)

// AuthProvider provides authorization headers for APIM API calls.
type AuthProvider interface {
	// AuthHeader returns the Authorization header value (e.g., "Basic xxx" or "Bearer xxx").
	AuthHeader() (string, error)
}

// basicAuth implements AuthProvider for HTTP Basic authentication.
type basicAuth struct {
	header string
}

// NewBasicAuth creates a basic auth provider.
func NewBasicAuth(username, password string) AuthProvider {
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return &basicAuth{header: "Basic " + encoded}
}

// AuthHeader returns the Basic auth header.
func (b *basicAuth) AuthHeader() (string, error) {
	return b.header, nil
}

// oauth2Auth implements AuthProvider for OAuth2 client credentials.
type oauth2Auth struct {
	mu            sync.Mutex
	clientID      string
	clientSecret  string
	tokenEndpoint string
	scopes        []string
	httpClient    *http.Client

	accessToken string
	expiresAt   time.Time
}

// NewOAuth2Auth creates an OAuth2 auth provider.
func NewOAuth2Auth(cfg config.AuthConfig, verifySsl bool) AuthProvider {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifySsl},
	}
	return &oauth2Auth{
		clientID:      cfg.ClientID,
		clientSecret:  cfg.ClientSecret,
		tokenEndpoint: cfg.TokenEndpoint,
		scopes:        cfg.Scopes,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// AuthHeader returns a Bearer token, refreshing if needed.
func (o *oauth2Auth) AuthHeader() (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.accessToken != "" && time.Now().Before(o.expiresAt) {
		return "Bearer " + o.accessToken, nil
	}

	if err := o.refreshToken(); err != nil {
		return "", err
	}
	return "Bearer " + o.accessToken, nil
}

func (o *oauth2Auth) refreshToken() error {
	data := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {strings.Join(o.scopes, " ")},
	}

	req, err := http.NewRequest(http.MethodPost, o.tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(o.clientID, o.clientSecret)

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("parse token response: %w", err)
	}

	if tokenResp.ExpiresIn <= 0 {
		return fmt.Errorf("invalid token expiry: %d seconds", tokenResp.ExpiresIn)
	}
	buffer := 60
	if tokenResp.ExpiresIn <= buffer {
		buffer = tokenResp.ExpiresIn / 3
	}
	o.accessToken = tokenResp.AccessToken
	o.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn-buffer) * time.Second)
	return nil
}

// NewAuthProvider creates the appropriate auth provider based on config.
func NewAuthProvider(managedCfg config.ManagedConfig) AuthProvider {
	if managedCfg.Source.Auth.AuthType == "oauth2" {
		return NewOAuth2Auth(managedCfg.Source.Auth, managedCfg.Source.VerifySSL)
	}
	return NewBasicAuth(managedCfg.Source.Auth.Username, managedCfg.Source.Auth.Password)
}
