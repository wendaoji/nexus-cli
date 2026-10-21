// Package nexus 是与 Nexus REST API 通讯的最小客户端。
//
// 只覆盖 Restish 表达不了的那几件事（multipart 上传、凭据校验、任务轮询所需的
// 少量读取）。其余全部交给嵌入的 Restish，避免重复实现。
package nexus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	nexuscli "github.com/wendaoji/nexus-cli"
	"github.com/wendaoji/nexus-cli/internal/specs"
)

// DefaultOrigin 是编译期默认目标，来自 config/default-origin.txt（随二进制内嵌）。
// 运行时由 config.Resolve 按优先级覆盖。
var DefaultOrigin = strings.TrimSpace(nexuscli.DefaultOriginText)

// Client 是一个带 Basic 认证的 Nexus 客户端。
type Client struct {
	Origin  string // 例如 http://host:8081
	APIRoot string // 例如 /service/rest
	User    string
	Pass    string
	HTTP    *http.Client
}

// New 用解析好的目标地址与凭据构造客户端。
func New(origin, user, pass string) (*Client, error) {
	root, err := specs.APIRoot()
	if err != nil {
		return nil, err
	}
	return &Client{
		Origin:  strings.TrimRight(origin, "/"),
		APIRoot: strings.TrimRight(root, "/"),
		User:    user,
		Pass:    pass,
		HTTP:    &http.Client{Timeout: 300 * time.Second},
	}, nil
}

// URL 拼接完整请求地址。path 需以 "/" 开头，例如 "/v1/repositories"。
func (c *Client) URL(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.Origin + c.APIRoot + path
}

// Do 发起一次带认证的请求。调用方负责关闭 resp.Body。
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.URL(path), body)
	if err != nil {
		return nil, err
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.User != "" || c.Pass != "" {
		req.SetBasicAuth(c.User, c.Pass)
	}
	return c.HTTP.Do(req)
}

// CheckResult 描述一次凭据校验的结论。
type CheckResult struct {
	Status   int
	Verbatim string
}

// Valid 表示认证是否通过。403 说明已认证但无权限，凭据本身有效。
func (r CheckResult) Valid() bool { return r.Status == 200 || r.Status == 403 }

// Check 用 GET /v1/repositories 校验凭据：401=无效，403=有效但无权限。
func (c *Client) Check(ctx context.Context) (CheckResult, error) {
	resp, err := c.Do(ctx, http.MethodGet, "/v1/repositories", nil, "")
	if err != nil {
		return CheckResult{}, fmt.Errorf("校验凭据时网络错误: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return CheckResult{Status: resp.StatusCode, Verbatim: strings.TrimSpace(string(raw))}, nil
}

// ExplainCheck 把校验结果翻译成给人看的一句话。
func ExplainCheck(r CheckResult) string {
	switch {
	case r.Status == 200:
		return "凭据有效（HTTP 200）"
	case r.Status == 403:
		return "凭据有效，但该账号无权列出仓库（HTTP 403）—— 认证已通过"
	case r.Status == 401:
		return "凭据无效（HTTP 401）：请检查账号与密码/token passcode"
	case r.Status == 429:
		return "Nexus 认证限流（HTTP 429，短时间多次失败尝试所致）：等几分钟再试，不要连试"
	default:
		return fmt.Sprintf("校验失败（HTTP %d）", r.Status)
	}
}
