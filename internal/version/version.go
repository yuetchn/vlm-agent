package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Version 为当前程序版本，由构建时 ldflags 注入：
//
//	go build -ldflags "-X github.com/zerfx/new_jzd/internal/version.Version=v1.0.0"
//
// 本地开发默认值为 "dev"。
var Version = "dev"

// Check 请求 url 获取最新版本号（tag_name）。
// url 为空时立即返回 ("", nil)。
// 任何网络/解析错误直接返回，由调用方决定是否静默处理。
func Check(ctx context.Context, url string) (string, error) {
	if url == "" {
		return "", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("version: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "new_jzd/"+Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("version: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("version: read body: %w", err)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("version: parse json: %w", err)
	}
	return release.TagName, nil
}

// IsNewer 返回 latest 是否比 current 新（均为 vMAJOR.MINOR.PATCH 格式）。
// 任一版本解析失败时返回 false，不 panic。
func IsNewer(latest, current string) bool {
	lv := parseSemver(latest)
	cv := parseSemver(current)
	for i := range lv {
		if lv[i] != cv[i] {
			return lv[i] > cv[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{} // 解析失败归零，IsNewer 返回 false
		}
		result[i] = n
	}
	return result
}
