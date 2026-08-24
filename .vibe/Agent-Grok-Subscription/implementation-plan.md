# Grok 订阅量显示功能工作记录

## 目标

为 HomePi 增加 Grok 订阅量的采集与显示能力，并保持现有 Provider、快照和 TUI 展示的契约边界。

## 当前状态

- 工作树状态：开始前检查为干净。
- 需求来源：用户请求“增加显示 Grok 订阅量的功能”。
- Spec 与现有实现：已读取并确认现有 `ProviderMetric`、Scheduler、快照和 TUI 卡片链路可以承载一个 weekly quota 指标。
- Grok 数据来源、认证方式、订阅量定义与失败降级：已完成本机官方 CLI 结构核对，改为按 Codex 模式读取认证态并主动请求 CLI billing endpoint；代码实现与自动化验证待本轮完成。

## 数据源核对

- xAI 官方 FAQ 说明消费版使用一个共享的 weekly usage pool，Usage 页面展示百分比、产品拆分和重置信息；xAI Management API 的 prepaid balance 仍属于 API team，不纳入本指标。
- 官方 Grok CLI 源码使用 `GET /billing?format=credits`，通过 Bearer Token、`x-userid`、`x-grok-client-version` 和 CLI token-auth 标识获取 credits billing 响应；HomePi 只复用这一只读请求，不执行 CLI 或刷新登录态。
- 本机 `grok 1.0.5` 的 `~/.grok/auth.json` 含有效登录项 `key/user_id/auth_mode/expires_at`；HomePi 只读取请求所需字段和过期时间，不读取 `refresh_token`、邮箱或 Cookie。
- 采用对象：消费版 SuperGrok 当前 weekly 共享用量池的剩余百分比。xAI API team 的 prepaid balance 不纳入本功能，避免把不同计费主体混为一个指标。

## 执行约束

- 先更新相关 Spec 和测试文档，再进行代码实现。
- 仅修改与本功能直接相关的文件，保留用户已有改动。
- 区分本地自动化测试、真实 Provider 验证和设备手动验收；未执行的部分标记为 `NOT RUN`。
- 本轮不执行 `git commit`，待用户检查变更后再决定。

## 实现契约

- 新 Provider 类型：`grok_usage`，Provider key：`grok`，来源：`compatibility_api`，精度：`verified`。
- 默认只读 `~/.grok/auth.json`；配置中的 `auth_file` 可指定替代认证文件路径。HomePi 不把认证文件或其中的 Token 发送给 Pi。
- 每次采集主动请求 `https://cli-chat-proxy.grok.com/v1/billing?format=credits`；`region=custom` 只允许配置系统已校验的兼容代理 Base URL。
- 只产生一个 `KindQuota`/`WindowWeekly` 指标：`value = 100 - config.creditUsagePercent`、`limit = 100`、`unit = percent`，`resets_at = config.currentPeriod.end`，`observed_at = daemon request time`。
- 认证选择只读取有效登录项的 `key/user_id/auth_mode/expires_at`；请求额外带 CLI token-auth、用户 ID 和客户端版本头；不读取或输出 `refresh_token`、Cookie、邮箱或原始响应，不执行 `grok login`、刷新或写回 CLI 状态。
- 认证文件缺失/符号链接/非普通文件/大小超限、无有效登录项、billing 请求失败或响应 schema/百分比无效均分类为安全错误，不生成虚假的 0%/100% 指标；旧值由 Scheduler 按既有错误策略保留并标记。
- 余额、Extra Usage Credits、API team prepaid balance 和产品拆分不在本次范围内。

## 交付状态

- 自动化：主动 HTTP 请求、auth.json 只读与认证选择、响应 schema、失败分类、全仓 `go test ./...`、`go vet ./...` 和 `git diff --check` 均通过。
- 本机真实验证：当前官方 Grok CLI 登录态执行 `homepi-node provider test -id grok-main` 成功，返回 1 metric；auth.json SHA-256、mode、size、mtime、inode 未改变。
- 运行时交付：新版 `homepi-node` 已安装并重启 LaunchAgent；DietPi 收到 snapshot version 17 的 `grok-main.weekly`，`observed_at=2026-08-24T09:16:51Z`、remaining=41、source_kind=`compatibility_api`，TTY 已显示 `Grok ... 41% LEFT RESET 4D OK`。
- 待用户验收：官方 CLI `/usage` 数值人工对账、抓包、用户对文档和差异的检查，以及目标屏幕的最终肉眼确认；不以自动化或 daemon 日志替代用户确认。

## 验收记录

已将本轮指标定义为消费版 SuperGrok weekly 共享用量池剩余百分比，并已同步 HL-Spec、Module-Spec、PRD、UI-Spec 与测试文档。

连接器、配置校验、CLI/Web Admin 类型入口、快照到 TUI 的 weekly-only 展示和授权失败回归已实现；主动 billing 请求自动化、本机真实 `provider test` 与 Mac→Pi TTY 验证已完成。

尚未完成的仅是外部验收边界：官方 CLI `/usage` 数值人工对账、抓包、用户对文档和差异的检查，以及目标屏幕的最终肉眼确认；CLI `provider test`、Pi 快照、TTY 和 systemd 链路已完成实机核对，这些边界仍不伪装为用户确认。

## 2026-08-24 主动拉取执行记录

- `scripts/install-local.sh --no-restart` 已构建并安装新版 `homepi-node`；随后 LaunchAgent 重启短暂出现一次 macOS `OS_REASON_CODESIGNING` 约束失败，自动重试后恢复，当前 `homepi-node status` 为 `running=true`，新进程已监听。
- `homepi-node provider test -id grok-main` 使用默认 `~/.grok/auth.json` 主动请求 billing credits endpoint，返回 1 metric，未输出凭据。
- DietPi 的 `homepi-display.service` 仍为 `active`、`NRestarts=0`、`ExecMainStatus=0`；当前快照包含 `source_kind=compatibility_api` 的 `grok-main.weekly`，TTY 60×20 显示 `41% LEFT ... OK`。
- 本轮没有修改 Pi 的凭据、环境文件或 `homepi-display` 二进制；已有 Display 版本已能正确渲染新鲜 Grok 指标。

## 2026-08-24 weekly-only 卡片两行布局调整

- 用户反馈：Grok 当前显示为 `RESET 4D ... OK` 同行，视觉上与其他 Coding Plan 不一致；要求第一行显示 `RESET 4D`，第二行显示 `OK`。
- 设计决定：weekly-only Grok 继续不伪造 `5H` 数据，但改用标准两行卡片；第一行显示主值与 reset，第二行在共享状态列显示 `OK`/`STALE`/`AUTH`/`ERROR`。
- 容量边界：设备采用轮播，`CODING` 页显示完整 Coding 卡片，`API` 页显示余额；非轮播兼容总览在超出 60×20 时优先保留完整 Coding 区，不恢复 weekly-only 一行特例。
- 实现与验证状态：文档已先更新；渲染器、回归测试、ARM64 构建和 Pi TTY 验证待本轮完成。

## 2026-08-24 weekly-only 两行布局交付结果

- `internal/ui` 渲染器已移除 weekly-only 单行特例：Grok 第一行显示进度与 `RESET`，第二行在标准状态列显示 `OK`/`STALE`，不生成虚假的 `5H` 详情。
- 新增/调整 UI 回归覆盖 live `OK`、过期 `STALE`、四张 Coding 卡片的轮播 `CODING`/`API` 页面，以及非轮播总览的容量优先级；`go test ./...`、`go vet ./...`、UI/Display/E2E race 和 `git diff --check` 通过。
- 新版 `linux/arm64` Display 已部署到 Pi，SHA-256 为 `bcf64c47b09bfd68dc3aaf41a475b609d7dcf10aaef09b3a258d62398a81f0c5`；版本构建时间 `2026-08-24T09:44:32Z`，服务 `active`、`NRestarts=0`、`ExecMainStatus=0`。
- Pi 快照 version 205 仍包含 `grok-main.weekly`，`value=41`、`status=ok`；强制显示 `CODING` 页后读取 `/dev/vcsu1`，第 8 行含 `RESET 4D`，第 9 行单独显示 `OK`。
- 旧二进制已保留为 `/usr/local/bin/homepi-display.pre-grok-two-line-34c85bd`；未修改 Pi 凭据、环境文件或快照配置。物理屏最终肉眼确认与用户检查仍待执行。

## 2026-08-24 weekly-only 5H 缺失占位符调整

- 用户要求：Grok 没有 5 小时限额时，仍生成占位符以保持与其他 Coding Plan 的两行 UI 一致。
- 采用 `5H --` 作为纯展示占位符：不写入 `ProviderMetric`、不改变 weekly quota、百分比、阈值或状态计算；第二行右侧继续显示实际状态。
- 文档已先更新；渲染器、测试、ARM64 构建和 Pi TTY 回归已完成，暂不提交 Git。

## 2026-08-24 weekly-only 5H 占位符验证结果

- `go test ./...`、`go vet ./...`、UI/Display/E2E race 和 `git diff --check` 全部通过。
- 新版 ARM64 Display 构建标识为 `262fcd2`，SHA-256 为 `c91b10e6275fd298d941d4d93fde6b4f6fd969bd5a0373aa190f9b79e5da5f43`；Pi 服务 `active`、`NRestarts=0`、`ExecMainStatus=0`。
- Pi snapshot version 401 仍只有 `grok-main.weekly`，`value=41`、`status=ok`；`/dev/vcsu1` 的 `CODING` 页第 8 行显示 `RESET 4D`，第 9 行显示 `5H --` 与 `OK`。
- 旧版已保留为 `/usr/local/bin/homepi-display.pre-grok-placeholder-262fcd2`；未修改 Pi 凭据、环境文件或快照配置。物理屏最终肉眼确认仍待执行。

## DietPi Display 部署计划

- 目标：将当前工作树构建的 `homepi-display` `linux/arm64` 部署到 `ssh dietpi:/usr/local/bin/homepi-display`。
- 部署前基线：Pi 当前运行 `68abb88-fix004`（2026-08-14），服务为 active；不修改 `/etc/homepi-display/environment`、设备 Token 或现有快照。
- 部署方式：本地构建并校验 ARM64 二进制，经临时远端文件传输后以权限保持的原子替换方式更新，保留旧文件为 `.previous`，再重启 `homepi-display.service`。
- 部署后验收：版本/平台、systemd active 状态、最近日志、last-known-good 快照中的 `grok-main.weekly` 与 60×20 显示链路均需核对；失败时恢复旧二进制。

## DietPi Display 部署结果

- 本地 `dist/homepi-display-0.1.0-linux-arm64` 为 ELF `linux/aarch64`，SHA-256 为 `dec154922810f9d0b677596a697fc2b6f7e1bd57069d29f0a66b8a696fded7b4`，包含 `WeeklyOnly` 与构建版本 `34c85bd`，构建时间为 `2026-08-24T08:18:07Z`。
- Pi 端已完成 SHA 一致性校验、原子替换和服务重启；当前 `/usr/local/bin/homepi-display` 为 `34c85bd`，`linux/arm64`，权限 `0755`，`homepi-display.service` 为 `active`。
- 当前 `NRestarts=0`、`ExecMainStatus=0`；旧版本与首次部署版本分别保留为 `/usr/local/bin/homepi-display.pre-grok-34c85bd` 和 `/usr/local/bin/homepi-display.pre-stale-badge-34c85bd`，均可用于回滚。
- Pi 的 `last-known-good.json` 已推进到 `snapshot_version=125`、schema `1.1`，其中包含 `grok-main.weekly`；TTY 第 9 行已完整显示 `Grok ... 42% LEFT    RESET 4D STALE`。

## 部署后画面问题修复

- 首次部署后读取 Pi `/dev/vcsu1` 已确认 Grok 卡片出现并显示 42% weekly 余量；由于本地日志观察时间已过期，状态正确变为 `STALE`，但 60 列单行布局将其渲染为 `RESET 4DSTAL`。
- 该问题属于 weekly-only 卡片的状态徽标与重置文案排布缺陷，不改变数据来源、快照字段或 stale 语义；已先同步 UI/Module/Test 文档，新增回归测试并通过全仓测试与 `go vet`。
- 修复版已重新构建并部署；第二次 TTY 读取显示 `RESET 4D STALE`，状态与重置文案均完整，旧二进制和修复前版本均保留。

## 2026-08-24 部署执行记录

- 首次部署：当前工作树的 ARM64 Display 原子替换成功，快照链路包含 `grok-main.weekly`；TTY 读取发现 stale weekly-only 卡片的 `RESET 4DSTAL` 截断。
- 修复验证：新增 `TestBuildRendersWeeklyOnlyGrokStaleBadgeWithFiveHourPlaceholder`；`go test ./...` 与 `go vet ./...` 均通过，`git diff --check` 通过。
- 二次部署：修复版 SHA 与 Pi 临时文件一致后原子替换；Pi 当前 version `34c85bd`、`active`、`NRestarts=0`、`ExecMainStatus=0`，snapshot version `125` 含 Grok，TTY 第 9 行完整显示 `RESET 4D STALE`。
