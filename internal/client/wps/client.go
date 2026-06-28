package wps

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	json "github.com/bytedance/sonic"

	"tcg-rulex-engine/internal/client/clienthttp"
	"tcg-rulex-engine/pkg/logs"
)

const maxResponseSize = 1024 * 100 // 100 KB

var (
	globalClient *Client
	globalOnce   sync.Once
)

type Config struct {
	Host    string `mapstructure:"host"`
	BasPath string `mapstructure:"basePath"`
}

func (c *Config) Init() *Client    { return NewClient(c.Host, c.BasPath) }
func (c *Config) Close(cl *Client) { cl.Close() }

type Client struct {
	httpClient       *http.Client
	baseURL          string
	basePath         string
	maxRetries       int
	retryDelay       time.Duration
	singleReqTimeout time.Duration
	once             sync.Once
}

// NewClient initializes a global singleton WPS HTTP client.
func NewClient(host, basePath string) *Client {
	globalOnce.Do(func() {
		globalClient = &Client{
			baseURL:  host,
			basePath: basePath,
			httpClient: &http.Client{
				Transport: &http.Transport{
					MaxIdleConns:          50,
					MaxConnsPerHost:       100,
					MaxIdleConnsPerHost:   30,
					IdleConnTimeout:       30 * time.Second,
					TLSHandshakeTimeout:   clienthttp.DefaultTLSHandshakeTimeout,
					DisableKeepAlives:     true,
					DisableCompression:    true,
					ExpectContinueTimeout: 1 * time.Second,
					ReadBufferSize:        32 * 1024,
					WriteBufferSize:       16 * 1024,
					ForceAttemptHTTP2:     true,
				},
			},
			maxRetries:       3,
			retryDelay:       clienthttp.DefaultRetryDelay,
			singleReqTimeout: clienthttp.DefaultSingleReqTimeout,
		}
	})
	return globalClient
}

func (c *Client) Close() {
	c.once.Do(func() {
		if c.httpClient != nil {
			c.httpClient.CloseIdleConnections()
		}
	})
}

func (c *Client) doGet(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return readBody(resp)
}

func (c *Client) doGetWithRetry(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	return c.doWithRetry(ctx, rawURL, func(ctx context.Context) ([]byte, error) {
		return c.doGet(ctx, rawURL, headers)
	})
}

func (c *Client) doWithRetry(ctx context.Context, rawURL string, fn func(context.Context) ([]byte, error)) ([]byte, error) {
	var lastErr error

	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, c.singleReqTimeout)
		body, err := fn(reqCtx)
		cancel()

		if err == nil {
			return body, nil
		}

		lastErr = err
		logs.Warn(ctx, "[WPSClient] attempt %d/%d retryDelay=%dms url=%s err=%v",
			attempt, c.maxRetries, c.retryDelay.Milliseconds(), rawURL, err)

		if attempt < c.maxRetries {
			retryTimer := time.NewTimer(c.retryDelay)
			select {
			case <-retryTimer.C:
			case <-ctx.Done():
				retryTimer.Stop()
				return nil, fmt.Errorf("context cancelled, aborting retries: %w", ctx.Err())
			}
		}
	}

	return nil, fmt.Errorf("all %d attempts failed: %w", c.maxRetries, lastErr)
}

func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, &HTTPError{body: fmt.Sprintf("unexpected status code: %d", resp.StatusCode), code: resp.StatusCode}
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return data, nil
}

// GetResetPasswordStatus retrieves the password-reset channel configuration for a merchant.
// Corresponds to: GET /members/reset-password-status
//
// Usage:
//
//	status, err := wpsClient.GetResetPasswordStatus(ctx, "dfstar")
//	if err != nil {
//	    // handle error
//	}
//	fmt.Println(status.Value.IsEmailResetEnabled) // true
//	fmt.Println(status.Value.IsSmsResetEnabled)   // false
func (c *Client) GetResetPasswordStatus(ctx context.Context, merchantCode string) (*ResetPasswordStatusResponse, error) {
	start := time.Now()
	url := fmt.Sprintf("%s/%smembers/reset-password-status", c.baseURL, c.basePath)
	logs.Info(ctx, "[WPSClient] GetResetPasswordStatus url=%s merchant=%s", url, merchantCode)

	body, err := c.doGetWithRetry(ctx, url, map[string]string{
		"Merchant": merchantCode,
	})
	if err != nil {
		logs.Warn(ctx, "[WPSClient] GetResetPasswordStatus failed, merchant=%s, elapsed=%dms, err=%v",
			merchantCode, time.Since(start).Milliseconds(), err)
		return nil, fmt.Errorf("GetResetPasswordStatus request failed: %w", err)
	}

	var result ResetPasswordStatusResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("deserialization failed: %w, raw response: %s", err, string(body))
	}

	logs.Info(ctx, "[WPSClient] GetResetPasswordStatus success, merchant=%s, IsEmailResetEnabled=%v, IsSmsResetEnabled=%v, elapsed=%dms",
		merchantCode, result.Value.IsEmailResetEnabled, result.Value.IsSmsResetEnabled, time.Since(start).Milliseconds())

	return &result, nil
}
