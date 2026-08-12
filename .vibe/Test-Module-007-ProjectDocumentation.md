# 模块 007 测试文档：项目文档与 GitHub 入口

> 对应规格：Module-Spec-007-ProjectDocumentation.md
> 状态：静态、一致性与回归测试通过；GitHub 预览和 Mock Quick start 待用户验收

## 1. 静态与一致性测试

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| DOC001 | 检查 `README.md` 与 `README.zh-CN.md` | 两份文件存在、非空、各含唯一 H1 | 两份文件各 728 行；代码围栏外 H1 均为 1，围栏均为 88 且闭合 | 通过 |
| DOC002 | 检查双语入口 | 英文链接中文，中文链接英文 | 英文含 `README.zh-CN.md`，中文含 `README.md` | 通过 |
| DOC003 | 提取两份 README 的仓库内相对链接 | 每个目标文件均存在 | 逐项解析相对链接并检查目标，全部存在 | 通过 |
| DOC004 | 对照 Makefile、`cmd/` 与 `scripts/` 审计公共命令 | 不包含不存在的 target、脚本或子命令 | 12 个引用 Make target、3 个引用脚本均存在；两个 binary 的 `--help` 实际运行通过 | 通过 |
| DOC005 | 比较 shell 命令块和关键命令集合 | 双语版本操作路径一致 | 两份文档各 39 个 Bash 块；忽略本地化注释和空行后内容逐行一致 | 通过 |
| DOC006 | 扫描凭据、绝对用户路径和危险示例 | 无真实 Key、Token、`/Users/<name>` 或明文凭据 | 指定用户路径、Key/Token 高风险模式无命中；示例仅使用 replace 占位符 | 通过 |
| DOC007 | `git diff --check` | 无空白错误 | 无输出，退出码 0 | 通过 |

## 2. 回归测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| DOC101 | `go test ./... -count=1 -timeout=180s` | 全仓测试通过 | 30 个有测试包通过；`internal/connector/mock` 无测试文件 | 通过 |
| DOC102 | `go vet ./...` | 全仓静态检查通过 | 无输出，退出码 0 | 通过 |
| DOC103 | `bash -n scripts/*.sh scripts/lib/*.sh` | 全部已提交脚本语法通过 | 7 个脚本全部通过 | 通过 |

## 3. 手动验收

| ID | 操作 | 预期结果 | 实际结果 | 结果 |
|---|---|---|---|---|
| DOC201 | 在 GitHub Markdown 预览中打开英文 README | 结构清晰，默认入口可完成安装、配置和开发 | 待用户检查 | 待验收 |
| DOC202 | 切换至简体中文 README 并返回英文 | 语言切换可见，章节与命令一致 | 待用户检查 | 待验收 |
| DOC203 | 按任一版本的 Quick start 使用 mock 数据启动 node/display | 本地 loopback 演示可运行，不需要真实凭据 | 待用户检查 | 待验收 |

## 4. 清理要求

验证只读取 README、源码和规格；若临时生成链接清单或测试目录，执行结束后必须删除，不得把测试
辅助文件留在仓库。

## 5. 执行记录

执行日期：2026-08-12；环境：macOS darwin/arm64、Go 1.26.5、基线 commit `d8fafc1`。

附加命令契约检查：`make -n run-node`、`make -n run-display`、`go run ./cmd/homepi-node --help`、
`go run ./cmd/homepi-display --help` 均通过；`go test -race ./... -count=1 -timeout=180s` 全仓通过。
验证使用内联只读命令和进程替换，没有创建临时测试脚本、链接清单或运行数据，因此无需额外清理。
DOC201-DOC203 保持待验收，不把 Markdown 静态检查写成 GitHub 实际渲染或 Mock 双进程运行已经通过。
