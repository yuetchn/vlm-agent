//go:build !windows

package checker

// checkVLMGRPC 在非 Windows 平台跳过 VLM gRPC 服务检查
func checkVLMGRPC() CheckResult {
	return CheckResult{Name: "VLM 服务", OK: true, Message: "非 Windows 平台，跳过 VLM 服务检查"}
}
