package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/jicheng1014/okuptime-agent/internal/config"
)

const defaultBaseURL = "https://www.okuptime.com"

type Error struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
	Status  int             `json:"-"`
}

func (e *Error) Error() string { return e.Message }

type Client struct {
	base  *url.URL
	token string
	http  *http.Client
}

func BaseURL() (*url.URL, error) {
	base := os.Getenv("OKUPTIME_BASE_URL")
	if base == "" {
		base = defaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
		return nil, &Error{Code: "config_error", Message: "OKUPTIME_BASE_URL 必须是 HTTPS 地址；仅本机开发允许 HTTP"}
	}
	return u, nil
}

func NewPublic(timeout time.Duration) (*Client, error) {
	base, err := BaseURL()
	if err != nil {
		return nil, err
	}
	return &Client{base: base, http: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func New() (*Client, error) {
	client, err := NewPublic(20 * time.Second)
	if err != nil {
		return nil, err
	}
	token, err := config.Token()
	if err != nil {
		return nil, &Error{Code: "config_error", Message: err.Error()}
	}
	if !config.ValidToken(token) {
		return nil, client.tokenError(&Error{Code: "config_error", Message: "缺少有效访问令牌"})
	}
	client.token = token
	return client, nil
}

func (c *Client) tokenError(err *Error) *Error {
	tokenURL := c.base.ResolveReference(&url.URL{Path: "/api_tokens"}).String()
	err.Message += "；请打开 " + tokenURL + " 登录并创建访问令牌，再运行 okuptime config set-token"
	details := map[string]json.RawMessage{}
	if json.Unmarshal(err.Details, &details) != nil || details == nil {
		details = map[string]json.RawMessage{}
	}
	details["token_url"], _ = json.Marshal(tokenURL)
	err.Details, _ = json.Marshal(details)
	return err
}

func (c *Client) Do(method, path string, query url.Values, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	endpoint := (&url.URL{Scheme: c.base.Scheme, Host: c.base.Host, Path: "/api/v1" + path, RawQuery: query.Encode()}).String()
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &Error{Code: "network_error", Message: err.Error()}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, &Error{Code: "response_error", Message: "服务器响应过大"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var response struct {
			Error Error `json:"error"`
		}
		if json.Unmarshal(data, &response) != nil || response.Error.Code == "" {
			response.Error = Error{Code: "http_error", Message: "服务器返回 " + resp.Status}
		}
		response.Error.Status = resp.StatusCode
		if resp.StatusCode == http.StatusUnauthorized && c.token != "" {
			return nil, c.tokenError(&response.Error)
		}
		return nil, &response.Error
	}
	if !json.Valid(data) {
		return nil, &Error{Code: "response_error", Message: "服务器未返回有效 JSON"}
	}
	return data, nil
}
