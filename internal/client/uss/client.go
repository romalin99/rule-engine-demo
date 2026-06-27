package uss

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// NewClient initializes an HTTP client with timeout and retry configuration.
// Guaranteed to initialize only once via sync.Once.
func NewClient(host, basePath string) *Client {
	globalOnce.Do(func() {
		globalClient = &Client{
			baseURL:  host,
			basePath: basePath,
			httpClient: &http.Client{
				Transport: &http.Transport{
					MaxIdleConns:        50,
					MaxConnsPerHost:     100,
					MaxIdleConnsPerHost: 30,
					IdleConnTimeout:     30 * time.Second,
					TLSHandshakeTimeout: clienthttp.DefaultTLSHandshakeTimeout,
					// DisableKeepAlives=false (default): keep-alive reduces connection
					// setup overhead and is critical for a high-throughput service.
					DisableKeepAlives:     false,
					DisableCompression:    false,
					ExpectContinueTimeout: 1 * time.Second,
					// Explicit buffer sizes avoid the default 4 KB read/write buffers
					// that are resized on the heap on every connection. 16 KB covers
					// most API responses in a single read, reducing syscall count.
					ReadBufferSize:  16 * 1024,
					WriteBufferSize: 16 * 1024,
					// ForceAttemptHTTP2 negotiates HTTP/2 when the server supports it,
					// enabling multiplexing and header compression over a single TCP
					// connection — reducing latency under concurrent requests.
					ForceAttemptHTTP2: true,
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

func (c *Client) doGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return readBody(resp)
}

func (c *Client) doGetWithRetry(ctx context.Context, rawURL string) ([]byte, error) {
	return c.doWithRetry(ctx, rawURL, func(ctx context.Context) ([]byte, error) {
		return c.doGet(ctx, rawURL)
	})
}

//nolint:unused
func (c *Client) doPut(ctx context.Context, rawURL string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	return c.doPutBytes(ctx, rawURL, b)
}

// doPutBytes sends a PUT request with a pre-serialised body.
// Using pre-serialised bytes allows doPutWithRetry to marshal once and reuse
// the same []byte across all retry attempts instead of re-marshaling on every
// attempt (which was the original behaviour before this refactor).
func (c *Client) doPutBytes(ctx context.Context, rawURL string, body []byte) ([]byte, error) {
	// bytes.NewReader is cheap: it does not copy the slice.
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return readBody(resp)
}

// doPutWithRetry marshals the payload exactly once before entering the retry
// loop, then reuses the resulting []byte for every attempt.  The previous
// implementation called doPut (which calls json.Marshal) on each retry,
// producing redundant allocations when the first attempt failed.
func (c *Client) doPutWithRetry(ctx context.Context, rawURL string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	return c.doWithRetry(ctx, rawURL, func(ctx context.Context) ([]byte, error) {
		return c.doPutBytes(ctx, rawURL, b)
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
		logs.Warn(ctx, "[USSClient] attempt %d/%d retryDelay=%dms url=%s err=%v", attempt, c.maxRetries, c.retryDelay.Milliseconds(), rawURL, err)

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
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return data, nil
}

// GetCustomer retrieves customer information by customer name.
func (c *Client) GetCustomer(ctx context.Context, customerName string, force bool) (*CustomerInfo, error) {
	start := time.Now()
	url := fmt.Sprintf("%s/%scustomer?customerName=%s&force=%t", c.baseURL, c.basePath, customerName, force)
	logs.Info(ctx, "[USSClient] GetCustomer request: %s", url)

	body, err := c.doGetWithRetry(ctx, url)
	if err != nil {
		logs.Warn(ctx, "[USSClient] GetCustomer failed, customerName=%s, elapsed=%dms, err=%v",
			customerName, time.Since(start).Milliseconds(), err)
		return nil, fmt.Errorf("USSClient request failed: %w", err)
	}

	var result CustomerInfo
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("deserialization failed: %w, raw response: %s", err, string(body))
	}

	logs.Info(ctx, "[USSClient] GetCustomer success, customerName=%s, elapsed=%dms",
		customerName, time.Since(start).Milliseconds())
	return &result, nil
}

// GeneratePasswordResetToken requests a one-time password reset token for the given customer.
// Corresponds to: PUT /tcg-uss-ae/password/reset-generate
func (c *Client) GeneratePasswordResetToken(ctx context.Context, customerName, merchantCode string) (*PasswordResetTokenResponse, error) {
	start := time.Now()
	url := fmt.Sprintf("%s/%spassword/reset-generate", c.baseURL, c.basePath)

	payload := PasswordResetTokenRequest{
		CustomerName: customerName,
		MerchantCode: merchantCode,
	}

	logs.Info(ctx, "[USSClient] GeneratePasswordResetToken request url=%s customerName=%s merchantCode=%s",
		url, customerName, merchantCode)

	body, err := c.doPutWithRetry(ctx, url, payload)
	if err != nil {
		logs.Warn(ctx, "[USSClient] GeneratePasswordResetToken failed, customerName=%s, merchantCode=%s, elapsed=%dms, err=%v",
			customerName, merchantCode, time.Since(start).Milliseconds(), err)
		return nil, fmt.Errorf("GeneratePasswordResetToken request failed: %w", err)
	}

	var result PasswordResetTokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("deserialization failed: %w, raw response: %s", err, string(body))
	}

	logs.Info(ctx, "[USSClient] GeneratePasswordResetToken success, customerName=%s, merchantCode=%s, elapsed=%dms",
		customerName, merchantCode, time.Since(start).Milliseconds())
	return &result, nil
}

// GetRegisteredPlayerCount returns the total number of registered players for the given merchant.
// Corresponds to: GET /tcg-uss-ae/customer/profile/condition/plain?merchantCode=xxx&page=1&size=1&pageable=true
//
// page=1&size=1 minimises response payload — only the pagination Total field is needed.
func (c *Client) GetRegisteredPlayerCount(ctx context.Context, merchantCode string) (int64, error) {
	start := time.Now()
	rawURL := fmt.Sprintf("%s/%scustomer/profile/condition/plain?merchantCode=%s&page=1&size=1&pageable=true",
		c.baseURL, c.basePath, url.QueryEscape(merchantCode))
	logs.Info(ctx, "[USSClient] GetRegisteredPlayerCount request: %s", rawURL)

	body, err := c.doGetWithRetry(ctx, rawURL)
	if err != nil {
		logs.Warn(ctx, "[USSClient] GetRegisteredPlayerCount failed, merchantCode=%s, elapsed=%dms, err=%v",
			merchantCode, time.Since(start).Milliseconds(), err)
		return 0, fmt.Errorf("USSClient GetRegisteredPlayerCount request failed: %w", err)
	}

	var result CustomerProfileConditionResp
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("deserialization failed: %w, raw response: %s", err, string(body))
	}
	if !result.Success {
		logs.Warn(ctx, "[USSClient] GetRegisteredPlayerCount business failed, merchantCode=%s, elapsed=%dms, raw=%s",
			merchantCode, time.Since(start).Milliseconds(), string(body))
		return 0, fmt.Errorf("USSClient GetRegisteredPlayerCount business failed for merchant %s", merchantCode)
	}

	logs.Info(ctx, "[USSClient] GetRegisteredPlayerCount success, merchantCode=%s, total=%d, elapsed=%dms",
		merchantCode, result.Value.Total, time.Since(start).Milliseconds())
	return result.Value.Total, nil
}

// GetCustomerByID fetches a customer's registration date
// (profile.regDate) and merchant code (value.merchantCode) by customer ID.
// regDate 用作 service-terms auto-init 写入时的 AGREE_TIME；
// merchantCode 供调用方做商户上下文校验或日志/审计标注。
//
// Corresponds to: GET /tcg-uss-ae/customer?customerId=xxx&force=false
//
// Return semantics:
//   - 上游 success=false 或 HTTP/解析失败 → 返回 (time.Time{}, "", error)
//   - 上游 success=true 但 regDate 为 null/空 → agreeTime 是 time.Time{}
//     (IsZero()==true)，error 为 nil；调用方据此决定是回退到 SYSTIMESTAMP
//     (repo NVL) 还是上抛错误。
//   - 上游 success=true 但 merchantCode 为 null/空 → 返回空串 ""，调用方自行判断。
func (c *Client) GetCustomerByID(ctx context.Context, customerID int64) (time.Time, string, error) {
	start := time.Now()
	rawURL := fmt.Sprintf("%s/%scustomer?customerId=%d&force=false", c.baseURL, c.basePath, customerID)
	logs.Info(ctx, "[USSClient] GetCustomerByID request: %s", rawURL)

	body, err := c.doGetWithRetry(ctx, rawURL)
	if err != nil {
		logs.Warn(ctx, "[USSClient] GetCustomerByID failed, customerID=%d, elapsed=%dms, err=%v",
			customerID, time.Since(start).Milliseconds(), err)
		return time.Time{}, "", fmt.Errorf("USSClient GetCustomerByID request failed: %w", err)
	}

	var result CustomerInfo
	if err := json.Unmarshal(body, &result); err != nil {
		return time.Time{}, "", fmt.Errorf("deserialization failed: %w, raw response: %s", err, string(body))
	}
	if !result.Success {
		logs.Warn(ctx, "[USSClient] GetCustomerByID business failed, customerID=%d, elapsed=%dms, raw=%s",
			customerID, time.Since(start).Milliseconds(), string(body))
		return time.Time{}, "", fmt.Errorf("USSClient GetCustomerByID business failed for customerID=%d", customerID)
	}

	// FlexTime embeds time.Time;regDate 为 null 时 .Time 仍是零值,IsZero()==true,
	// 由调用方决定后续处理(回退默认或当作错误)。
	agreeTime := result.Value.Profile.RegDate.Time
	// NullString.Val 为底层字符串；JSON null 会被解码为 ""，与"未设置"语义一致。
	merchantCode := result.Value.MerchantCode.Val

	logs.Info(ctx, "[USSClient] GetCustomerByID success, customerID=%d, merchantCode=%s, regDate=%s, elapsed=%dms",
		customerID, merchantCode, agreeTime.Format(time.DateTime), time.Since(start).Milliseconds())
	return agreeTime, merchantCode, nil
}

// GetCustomerPersonalInfo retrieves customer personal information by customer ID.
// Corresponds to: GET /tcg-uss-ae/customer/personal-info?customerId=xxx
func (c *Client) GetCustomerPersonalInfo(ctx context.Context, customerID int64) (*CustomerPersonalInfoValue, error) {
	start := time.Now()
	url := fmt.Sprintf("%s/%scustomer/personal-info?customerId=%d", c.baseURL, c.basePath, customerID)
	logs.Info(ctx, "[USSClient] GetCustomerPersonalInfo request: %s", url)

	body, err := c.doGetWithRetry(ctx, url)
	if err != nil {
		logs.Warn(ctx, "[USSClient] GetCustomerPersonalInfo failed, customerID=%d, elapsed=%dms, err=%v",
			customerID, time.Since(start).Milliseconds(), err)
		return nil, fmt.Errorf("USSClient request failed: %w", err)
	}

	var result CustomerPersonalInfo
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("deserialization failed: %w, raw response: %s", err, string(body))
	}

	logs.Info(ctx, "[USSClient] GetCustomerPersonalInfo success, customerID=%d, elapsed=%dms",
		customerID, time.Since(start).Milliseconds())
	return &result.Value, nil
}
