package mcs

import (
	"bytes"
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
	closeOnce        sync.Once
}

// NewClient initializes a global singleton client.
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
	c.closeOnce.Do(func() {
		if c.httpClient != nil {
			c.httpClient.CloseIdleConnections()
		}
	})
}

// readBody reads the response body, returning an error (with a partial snippet) for non-200 responses.
func readBody(resp *http.Response) ([]byte, error) {
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}
	return data, nil
}

// execute dispatches a pre-built request; ctx already carries a timeout.
func (c *Client) execute(ctx context.Context, req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req.WithContext(ctx)) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("http do failed: %w", err)
	}
	return readBody(resp)
}

// reqBuilder reconstructs an *http.Request for each retry attempt.
// Each attempt receives its own reqCtx (with an independent timeout).
type reqBuilder func(reqCtx context.Context) (*http.Request, error)

// doWithRetry executes any request with per-attempt timeouts and automatic retries.
func (c *Client) doWithRetry(ctx context.Context, build reqBuilder) ([]byte, error) {
	var lastErr error

	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, c.singleReqTimeout)

		req, err := build(reqCtx)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("build request failed: %w", err)
		}

		body, err := c.execute(reqCtx, req)
		cancel()

		if err == nil {
			return body, nil
		}

		lastErr = err
		logs.Warn(ctx, "[MCSClient] attempt %d/%d retryDelay=%dms url=%s err=%v", attempt, c.maxRetries, c.retryDelay.Milliseconds(), req.URL, err)

		if attempt < c.maxRetries {
			retryTimer := time.NewTimer(c.retryDelay)
			select {
			case <-retryTimer.C:
			case <-ctx.Done():
				retryTimer.Stop()
				return nil, fmt.Errorf("context cancelled during retry: %w", ctx.Err())
			}
		}
	}

	return nil, fmt.Errorf("all %d attempts failed: %w", c.maxRetries, lastErr)
}

// playerPost sets player identity headers and executes a POST with retry.
// bodyBytes is re-wrapped as bytes.NewReader on each attempt so the body can be re-read.
func (c *Client) playerPost(ctx context.Context, headers PlayerHeaders, rawURL string, bodyBytes []byte) ([]byte, error) {
	return c.doWithRetry(ctx, func(reqCtx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, rawURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		headers.apply(req)
		return req, nil
	})
}

// GetRegisterIP 查询玩家注册时的 IP。
// 任意错误（HTTP/解析/业务 success=false）均返回空字符串，调用方按"取不到"语义兜底，不再向上抛错。
func (c *Client) GetRegisterIP(ctx context.Context, customerID int64) string {
	start := time.Now()
	const op = "getRegisterIP"

	rawURL := fmt.Sprintf("%s/%sregister/getRegisterIp?customerId=%d", c.baseURL, c.basePath, customerID)
	logs.Info(ctx, "[MCSClient] GetRegisterIP url=%s", rawURL)

	respBody, err := c.doWithRetry(ctx, func(reqCtx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		return req, nil
	})
	if err != nil {
		logs.Warn(ctx, "[MCSClient] %s failed, customerID=%d, elapsed=%dms, err=%v",
			op, customerID, time.Since(start).Milliseconds(), err)
		return ""
	}

	logs.Info(ctx, "[MCSClient] %s response, customerID=%d, elapsed=%dms, raw respBody=%s",
		op, customerID, time.Since(start).Milliseconds(), string(respBody))

	var result GetRegisterIPResp
	if err := json.Unmarshal(respBody, &result); err != nil {
		logs.Warn(ctx, "[MCSClient] %s unmarshal failed: customerID=%d err=%v raw=%s",
			op, customerID, err, string(respBody))
		return ""
	}
	if !result.Success {
		logs.Warn(ctx, "[MCSClient] %s not success: customerID=%d message=%s errorCode=%s",
			op, customerID, result.Message, result.ErrorCode)
		return ""
	}
	return result.Value.RegisterIP
}

func (c *Client) VerifyPlayerInfo(
	ctx context.Context,
	headers PlayerHeaders,
	request VerifyFinanceHistoryReq,
) (*VerifyFinanceHistoryResp, error) {
	start := time.Now()
	const op = "verifyPlayerInfo"

	rawURL := fmt.Sprintf("%s/%splayer/verifyPlayerInfo", c.baseURL, c.basePath)

	reqBodyBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal failed: %w", op, err)
	}
	logs.Info(ctx, "[MCSClient] VerifyPlayerInfo url=%s \nheaders=%s \nreqBody=%s", rawURL, headers.String(), string(reqBodyBytes))

	respBody, err := c.playerPost(ctx, headers, rawURL, reqBodyBytes)
	if err != nil {
		logs.Warn(ctx, "[MCSClient] VerifyPlayerInfo failed, merchant=%s, elapsed=%dms, err=%v",
			headers.Merchant, time.Since(start).Milliseconds(), err)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	logs.Info(ctx, "[MCSClient] VerifyPlayerInfo success, merchant=%s, elapsed=%dms, raw respBody=%s",
		headers.Merchant, time.Since(start).Milliseconds(), string(respBody))

	var result VerifyFinanceHistoryResp
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("%s: unmarshal failed: %w, raw=%s", op, err, respBody)
	}
	return &result, nil
}
