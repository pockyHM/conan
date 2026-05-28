# 代码审查修复报告

日期：2026-05-23

## 审查范围

- 当前 Go 源码主树：`cmd/`、`internal/`、`pkg/`
- 配置加载、agent/web 工具、格式一致性、基础测试和竞态测试
- 未处理开始前已有的本地构建产物：根目录 `conan` 二进制、`tmp/`

## 修复项

1. 修复 `agent.web.allow_private_network` 无法显式关闭的问题。
   - 根因：配置合并只跟踪到 `agent.web` 一层，且布尔值只在 `true` 时覆盖。
   - 修复：扩展 YAML 字段存在性跟踪到嵌套路径，并在 `mergeWeb` 中按字段是否显式出现合并布尔值。
   - 覆盖测试：`TestLoadClusterAllowsWebPrivateNetworkFalseOverride`

2. 修复 `web/fetch` 的 `max_chars` 按字节截断多字节字符的问题。
   - 根因：实现使用 `len(text)` 和 `text[:maxChars]`，对中文等 UTF-8 文本会把字符数误当字节数。
   - 修复：新增 rune 边界截断逻辑，`max_chars` 现在按字符数生效。
   - 覆盖测试：`TestWebFetchMaxCharsTruncatesAtRuneBoundary`

3. 统一主源码树 gofmt 格式。
   - 范围：`cmd/`、`internal/` 中 gofmt 报出的文件。
   - 未格式化 `.claude/worktrees` 下的镜像工作区文件。

## 验证结果

- `gofmt -l cmd internal pkg`：无输出
- `go vet ./...`：通过
- `go test ./...`：通过
- `go test -race ./...`：通过

## 备注

- `git status` 中的根目录 `conan` 二进制改动和 `tmp/` 未跟踪目录在本次审查前已存在，本次未回滚、未纳入修复。

