# vulnfixtures — 漏洞测试代码(请勿合并)

本包包含 **刻意引入的明显漏洞**,仅用于验证漏洞扫描器 / PR 安全审查工具是否能发现它们。
此包不接入任何生产代码路径。

**这个 PR 是测试 PR,禁止合并到 main。** 漏洞清单:

| # | 漏洞 | 文件 | 常见扫描器规则 |
|---|------|------|----------------|
| VULN-01 | OS 命令注入 | cmd_injection.go | gosec G204, Semgrep shell-injection |
| VULN-02 | SQL 注入 | sql_injection.go | gosec G201, Semgrep sql-injection |
| VULN-03 | 路径穿越 / 任意文件读取 | path_traversal.go | gosec G304, Semgrep path-traversal |
| VULN-04 | 弱哈希 (MD5/SHA1) | weak_crypto.go | gosec G501/G505 |
| VULN-05 | 硬编码密钥 | hardcoded_secret.go | gitleaks, trufflehog, Semgrep |
| VULN-06 | SSRF / 无限制 URL 请求 | insecure_http.go | Semgrep go-ssrf |
| VULN-07 | 可预测随机数 | insecure_rand.go | gosec G404 |

验证方法(本地):

```bash
go build ./internal/vulnfixtures/...
gosec ./internal/vulnfixtures/...
```
