# Module Spec 006：构建、发布与本地安装自动化

> 模块 ID：MOD-006
> 版本：0.1
> 状态：已实现并通过自动化验证；真实用户服务安装/回滚仍待维护窗口验收

## 1. 模块目标

为 macOS/Linux 开发环境提供稳定、可重复、可审计的构建入口，统一所有 binary 输出路径、版本元数据、
跨平台发布矩阵、校验和生成与本地安装流程。普通开发者只需要记忆 `make` 目标；复杂流程收敛到仓库
`scripts/`，不得再默认把临时 `homepi-node`/`homepi-display` binary 写入仓库根目录。

## 2. 范围

### 2.1 包含

- `scripts/build.sh`：构建当前平台的 `homepi-node` 与 `homepi-display` 到 `bin/`。
- `scripts/check.sh`：执行 gofmt 检查、`go vet ./...` 与 `go test ./...`。
- `scripts/dist.sh`：构建六个平台组合、两个命令共 12 个 `dist/` 产物。
- `scripts/verify-release.sh`：生成只包含当前版本 12 个产物的 `SHA256SUMS` 并自校验。
- `scripts/install-local.sh`：将已验证的本机 binary 原子安装到用户目录，保留单份 `.previous`，并仅在
  既有用户服务原本运行时重启；提供无副作用的 `--dry-run`。
- Makefile 继续提供 `build`、`check`、`dist`、`checksums`、`verify-release`、`install-local` 等稳定入口。

### 2.2 不包含

- Web Admin 本身、配置草稿或 Apply 事务。
- `scripts/smoke-webadmin.sh`；该脚本依赖尚未提交的 `homepi-node configure`，必须与 Web Admin 变更
  一起验收和提交。
- root/system 级安装、远端发布、自动 Git commit/push、真实 Provider 凭据或 Pi 部署。
- Windows 原生 PowerShell 安装脚本；本模块只保证 Windows binary 的交叉构建。

## 3. 目录与入口契约

```text
Makefile                    用户入口与兼容 target
scripts/lib/common.sh       仓库定位、版本元数据和共享目标矩阵
scripts/build.sh            当前平台构建
scripts/check.sh            标准质量门禁
scripts/dist.sh             六平台交叉构建
scripts/verify-release.sh   12 产物校验和与自校验
scripts/install-local.sh    用户目录安装、备份、重启与回滚
bin/                        当前平台临时产物；Git 忽略
dist/                       发布产物；Git 忽略
```

脚本必须从任意当前目录启动，使用自身位置解析仓库根目录；不得假设调用者已经 `cd` 到仓库。

## 4. 构建契约

- 默认版本为 `0.1.0`，允许通过 `VERSION` 覆盖。
- commit 默认来自 `git rev-parse --short HEAD`，获取失败时使用 `unknown`。
- build date 默认使用 UTC RFC3339 秒精度，允许通过 `BUILD_DATE` 覆盖以支持可重复验证。
- 所有交付 binary 使用 `-trimpath` 与 `-ldflags "-s -w ..."`，注入 version/commit/date。
- 当前平台构建必须先在 `bin/` 内的受限临时目录完成；两个 binary 全部成功后才替换正式 `bin/` 目标。
- 发布构建矩阵固定为：`darwin/amd64`、`darwin/arm64`、`windows/amd64`、`linux/amd64`、
  `linux/arm64`、`linux/arm/v7`。Windows 后缀为 `.exe`。
- 重复执行不得让旧 `SHA256SUMS` 或其他版本产物进入当前版本清单。

## 5. 本地安装与服务安全

- 默认安装目录为 `$HOME/.local/bin`，可用 `--install-dir` 覆盖；拒绝空路径、相对路径和 `/`。
- 默认先执行标准当前平台构建；`--skip-build` 只使用既有 `bin/`，并仍验证 binary 可执行与版本输出。
- `--dry-run` 只打印计划，不创建安装目录、不覆盖 binary、不停止或启动服务。
- 新 binary 必须先复制到安装目录内的临时文件、设置 `0755` 并通过 `--version`，再用同目录 rename 替换。
- 既有目标最多保留一份 `<binary>.previous`；安装中途失败时恢复本次替换前的目标。
- 只有检测到 `homepi-node` 用户服务在安装前为 `installed=true running=true` 时才执行 stop/start。
- 新服务启动或状态检查失败时恢复旧 binary，并尝试重新启动旧服务；错误必须返回非零。
- 安装脚本不得执行首次 `install`、`uninstall`、root 写入或修改正式配置/凭据。

## 6. Shell 约束

- 使用 `#!/usr/bin/env bash` 和 `set -euo pipefail`，兼容 macOS 自带 Bash 3.2。
- 代码注释和稳定日志使用英文；变量不得重用 `HOME`、`PATH`、`PWD` 等系统变量。
- 路径展开必须加双引号；临时路径必须位于明确的 `bin/`、`dist/` 或安装目录内。
- 清理只能作用于脚本本次创建且已解析的临时路径。
- 脚本不得读取、打印或持久化 Provider Key、设备 Token、CSRF token 或其他业务秘密。

## 7. 验收标准

- `bash -n` 对全部已提交 shell 脚本通过。
- 从仓库外目录执行 `scripts/build.sh`，只生成 `bin/homepi-node` 与 `bin/homepi-display`。
- 两个本机构建的 `--version` 包含指定 version、当前 commit、指定 UTC build date 和正确平台。
- `make check` 与直接执行 `scripts/check.sh` 语义一致并通过。
- `make checksums VERSION=<version>` 生成 12 个产物；`SHA256SUMS` 恰好 12 行且全部自校验通过。
- 重复执行 release verification 不把清单自身或旧版本文件纳入当前清单。
- `install-local.sh --dry-run` 不改变安装目标 checksum、服务 PID 或服务状态。
- staged diff 只包含本模块规格、测试记录、Makefile 和构建脚本；Web Admin/configtx/Provider 改动不进入提交。
