# 模块 006 测试文档：构建、发布与本地安装自动化

> 对应规格：Module-Spec-006-BuildAutomation.md
> 状态：构建自动化与隔离安装验证通过；真实用户服务安装/回滚待验收

## 1. Unit / 静态测试

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| B001 | 对 `scripts/*.sh`、`scripts/lib/*.sh` 执行 `bash -n` | 全部语法通过 | 7 个脚本全部通过 | 通过 |
| B002 | 从 `/tmp` 执行 `scripts/build.sh` | 正确定位仓库，只写 `bin/` | 生成 `bin/homepi-node`、`bin/homepi-display`，无根目录 binary | 通过 |
| B003 | `VERSION=9.9.9 BUILD_DATE=2026-08-12T00:00:00Z scripts/build.sh` | 两个 binary 显示指定版本/日期与当前 commit | 两者均显示 `9.9.9`、`f990007`、指定 UTC 时间和 `darwin/arm64` | 通过 |
| B004 | 以无效 `GOFLAGS` 制造构建失败 | 不用半套新产物替换 `bin/` 正式目标 | 两个原产物 SHA 均未变化，`.build.*` 已清理 | 通过 |
| B005 | `scripts/install-local.sh --dry-run` | 输出安装/服务计划，无文件或服务状态变化 | `~/.local/bin/homepi-node` SHA 与 LaunchAgent PID 前后相同 | 通过 |
| B006 | 相对路径或 `/` 安装目录 | 安装开始前拒绝 | 两类路径均非零退出并给出 unsafe/absolute 错误 | 通过 |
| B007 | Shell 脚本扫描敏感参数和输出 | 无真实秘密、无 `-secret`、无 token 回显 | `rg` 对敏感参数模式无命中 | 通过 |

## 2. 集成测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| B101 | `scripts/check.sh`（经 `make check`） | gofmt、vet、全仓 test 通过 | 三项全部通过 | 通过 |
| B102 | `make build VERSION=0.1.0` | 调用脚本并生成两个本机 binary | 两个 Mach-O arm64 产物生成并通过 `--version` | 通过 |
| B103 | `make dist VERSION=0.1.0` | 六平台 × 两个 binary 共 12 个产物 | 12 项生成；ARMv7 保持既有 `linux-armv7` 命名 | 通过 |
| B104 | `make checksums VERSION=0.1.0` | 清单 12 行，逐项 `OK` | 12 行且十二项全部 `OK` | 通过 |
| B105 | 加入旧版本产物后重复 checksums；加入额外同版本产物 | 旧版本不入清单；额外同版本产物拒绝 | 重复验证仍为 12 行；第 13 个同版本产物返回明确错误 | 通过 |
| B106 | `go mod verify`、`git diff --check` | 依赖完整、无 whitespace error | `all modules verified`；diff check 通过 | 通过 |
| B107 | 查看 staged diff/status | Web Admin、configtx、Provider 修复均未暂存 | staged 仅含 MOD-006 两份文档、Makefile 和 6 个构建脚本；smoke/Web Admin 均未暂存 | 通过 |

## 3. 实机安装测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| B201 | macOS 现有服务运行，执行正式 install-local | 原子替换、单份备份、服务重启、状态 running | 本次不执行，仅验证 dry-run | 暂缓 |
| B202 | 新 binary 启动失败 | 恢复旧 binary 并重启旧服务 | 需要隔离用户/故障注入 | 暂缓 |
| B203 | Windows/Linux 原生安装 | 使用对应用户服务完成生命周期 | 当前仅交叉构建，不伪写实机通过 | 暂缓 |

补充隔离验证：在 `/tmp/homepi-install-test.*` 执行首次安装和二次替换（`--skip-build --no-restart`），
两个 binary 均可执行，第二次安装保留单份 `.previous`，其 SHA 与第一次安装的 node 完全一致；测试目录
随后已清理。该结果不替代 B201/B202 的真实 LaunchAgent 重启和失败回滚验收。

## 4. 执行记录

执行日期：2026-08-12；环境：macOS darwin/arm64、Go 1.26.5、commit `f990007`。

最终清理执行 `make clean`，`bin/`、`dist/` 和隔离安装测试目录均不存在。现有用户安装目录只执行
checksum/status 读取与 dry-run；已安装 binary SHA 和 LaunchAgent PID 均未变化。ShellCheck 当前环境未安装，
因此只记录 `bash -n`、实际执行和敏感模式扫描，不宣称 ShellCheck 通过。
