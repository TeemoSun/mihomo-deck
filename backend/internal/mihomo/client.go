package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	baseURL, secret string
	httpClient      *http.Client
	wsDialer        *websocket.Dialer
}

func NewClient(baseURL, secret string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), secret: secret, httpClient: &http.Client{Timeout: 30 * time.Second}, wsDialer: websocket.DefaultDialer}
}
func (c *Client) request(ctx context.Context, method, path string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, &UpstreamError{Err: err}
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &UpstreamError{Err: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: data}
	}
	return json.RawMessage(data), nil
}
func (c *Client) Version(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/version", nil)
}
func (c *Client) Configs(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/configs", nil)
}
func (c *Client) PatchConfigs(ctx context.Context, b json.RawMessage) (json.RawMessage, error) {
	return c.request(ctx, http.MethodPatch, "/configs", b)
}
func (c *Client) ReloadConfigs(ctx context.Context, force bool) (json.RawMessage, error) {
	return c.request(ctx, http.MethodPut, "/configs?force="+strconv.FormatBool(force), []byte(`{}`))
}
func (c *Client) UpdateGeo(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodPost, "/configs/geo", nil)
}
func (c *Client) Restart(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodPost, "/restart", nil)
}
func (c *Client) Proxies(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/proxies", nil)
}
func (c *Client) Proxy(ctx context.Context, name string) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/proxies/"+url.PathEscape(name), nil)
}
func (c *Client) SelectProxy(ctx context.Context, group, node string) (json.RawMessage, error) {
	b, _ := json.Marshal(map[string]string{"name": node})
	return c.request(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), b)
}
func (c *Client) UnfixProxy(ctx context.Context, group string) (json.RawMessage, error) {
	return c.request(ctx, http.MethodDelete, "/proxies/"+url.PathEscape(group), nil)
}
func delayQuery(testURL string, timeout int) string {
	v := url.Values{}
	v.Set("url", testURL)
	v.Set("timeout", strconv.Itoa(timeout))
	return v.Encode()
}
func (c *Client) TestProxyDelay(ctx context.Context, name, testURL string, timeoutMS int) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/proxies/"+url.PathEscape(name)+"/delay?"+delayQuery(testURL, timeoutMS), nil)
}
func (c *Client) TestProviderProxyDelay(ctx context.Context, provider, name, testURL string, timeoutMS int) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/providers/proxies/"+url.PathEscape(provider)+"/"+url.PathEscape(name)+"/healthcheck?"+delayQuery(testURL, timeoutMS), nil)
}
func (c *Client) TestGroupDelay(ctx context.Context, group, testURL string, timeoutMS int) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/group/"+url.PathEscape(group)+"/delay?"+delayQuery(testURL, timeoutMS), nil)
}
func (c *Client) ProxyProviders(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/providers/proxies", nil)
}
func (c *Client) ProxyProvider(ctx context.Context, name string) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/providers/proxies/"+url.PathEscape(name), nil)
}
func (c *Client) UpdateProxyProvider(ctx context.Context, name string) (json.RawMessage, error) {
	return c.request(ctx, http.MethodPut, "/providers/proxies/"+url.PathEscape(name), nil)
}
func (c *Client) HealthCheckProxyProvider(ctx context.Context, name string) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/providers/proxies/"+url.PathEscape(name)+"/healthcheck", nil)
}
func (c *Client) RuleProviders(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/providers/rules", nil)
}
func (c *Client) UpdateRuleProvider(ctx context.Context, name string) (json.RawMessage, error) {
	return c.request(ctx, http.MethodPut, "/providers/rules/"+url.PathEscape(name), nil)
}
func (c *Client) Rules(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/rules", nil)
}
func (c *Client) Connections(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodGet, "/connections", nil)
}
func (c *Client) CloseAllConnections(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodDelete, "/connections", nil)
}
func (c *Client) CloseConnection(ctx context.Context, id string) (json.RawMessage, error) {
	return c.request(ctx, http.MethodDelete, "/connections/"+url.PathEscape(id), nil)
}
func (c *Client) DNSQuery(ctx context.Context, name, qtype string) (json.RawMessage, error) {
	v := url.Values{"name": {name}, "type": {qtype}}
	return c.request(ctx, http.MethodGet, "/dns/query?"+v.Encode(), nil)
}
func (c *Client) FlushFakeIPCache(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodPost, "/cache/fakeip/flush", nil)
}
func (c *Client) FlushDNSCache(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, http.MethodPost, "/cache/dns/flush", nil)
}
func (c *Client) DialWS(ctx context.Context, pathWithQuery string) (*websocket.Conn, *http.Response, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, nil, err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return nil, nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	h := http.Header{}
	if c.secret != "" {
		h.Set("Authorization", "Bearer "+c.secret)
	}
	return c.wsDialer.DialContext(ctx, strings.TrimRight(u.String(), "/")+pathWithQuery, h)
}
