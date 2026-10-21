package nexus

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

// FormField 是一个 multipart 字段。File 非空表示文件部件。
//
// Nexus 的 components 接口要求字段名是字面量点号形式（maven2.asset1），
// Restish 的请求体 shorthand 会把点号当嵌套结构，所以这条链路自己拼 multipart。
type FormField struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
	File  string `json:"file,omitempty"`
}

// PostComponent 发送一次 components POST，返回状态码与响应体。
func (c *Client) PostComponent(ctx context.Context, repo string, fields []FormField) (int, []byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range fields {
		if f.File != "" {
			fw, err := mw.CreateFormFile(f.Name, filepath.Base(f.File))
			if err != nil {
				return 0, nil, err
			}
			src, err := os.Open(f.File)
			if err != nil {
				return 0, nil, err
			}
			_, copyErr := io.Copy(fw, src)
			src.Close()
			if copyErr != nil {
				return 0, nil, copyErr
			}
			continue
		}
		if err := mw.WriteField(f.Name, f.Value); err != nil {
			return 0, nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return 0, nil, err
	}

	path := "/v1/components?repository=" + url.QueryEscape(repo)
	resp, err := c.Do(ctx, http.MethodPost, path, &buf, mw.FormDataContentType())
	if err != nil {
		return 0, nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	return resp.StatusCode, body, nil
}
