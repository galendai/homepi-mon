# FIX-006 Codex 遗留窗口与 Grok Unified Billing 降级修复

## 状态

- 日期：2026-09-06
- 状态：DEPLOYED AND VERIFIED
- 范围：连接器成功批次替换语义、Codex 遗留窗口清理、Grok Unified Billing 无额度字段降级、macOS 原子升级服务重注册
- 提交：未请求

## 现场现象

Pi 快照中 `codex-main` ConnectorHealth 已恢复为 `ok`，5 小时指标也已在
2026-09-06 18:21（Asia/Shanghai）更新，但上游本次未返回 weekly 窗口。Current State
仍保留 12:32 的旧 `codex-main.weekly`，超过 15 分钟新鲜度预算后把整张 Codex 卡片拖成
`STALE`。成功采集目前只覆盖返回的 metric ID，没有删除同一连接器本次不再返回的旧 ID。

`grok-main` 最近成功停在 2026-09-04，随后累计 334 次失败。只读实机诊断确认当前 Grok
CLI 1.0.13 的 billing 响应仍给出 weekly `currentPeriod`，但在
`isUnifiedBillingUser=true` 时不再给出 `creditUsagePercent`；旧解析器因此持续返回
`schema_changed: Grok credit usage is invalid`。响应同时没有可用于推导订阅剩余百分比的
`includedUsed` 或 `totalUsed`，不能伪造旧数值。

## 修复契约

1. 同一连接器的一次成功采集原子替换该连接器拥有的 Current State 指标集合；本次缺失的
   旧 metric ID 必须删除，其他连接器的指标不受影响。
2. 失败采集继续保留并标记该连接器最后成功值；不得把失败误当作空成功批次删除指标。
3. 较旧 sequence 的批次不得删除或覆盖较新指标；删除后保留 sequence 防护，避免旧批次复活。
4. Grok 响应包含有效 weekly 周期与 `isUnifiedBillingUser=true`、但缺少
   `creditUsagePercent` 时，生成同一稳定 ID 的 weekly unavailable 指标：无 value/limit、
   `precision=unavailable`、`status=unknown`，保留真实 reset 时间，TUI 显示 `N/A`。
5. Grok 非 Unified Billing 响应缺失百分比、百分比不可解析/越界、周期缺失或非 weekly 时，
   仍返回 `schema_changed`。
6. 旧响应仍包含合法 `creditUsagePercent` 时，继续生成 0–100 的剩余百分比指标。
7. Grok connector 本身不读取或写入 `refresh_token`，不启动 Grok/Codex CLI，不修改认证文件、
   Provider 配置、协议 schema、调度间隔或 `stale_after`。用户另行授权的官方 Grok CLI
   LaunchAgent 与 connector 保持独立。
8. macOS 升级已注册且运行中的 `homepi-node` 时，先卸载旧 LaunchAgent 注册，再替换
   executable 并重新注册；新服务启动失败时恢复旧 binary 和旧服务。

## 验证计划

- State 回归：同一连接器先发布 5h/weekly，后续成功只发布 5h，weekly 被删除；其他连接器
  保留；较旧批次不能删除较新指标。
- Scheduler 回归：成功恢复批次的指标集合收缩后，快照不再残留会老化为 `STALE` 的窗口。
- Grok 回归：旧百分比响应保持 42%；Unified Billing 缺失百分比时得到 N/A 指标；非 Unified
  缺失及非法百分比继续 `schema_changed`。
- 聚焦测试：`internal/state`、`internal/scheduler`、`internal/connector/grokusage`、`internal/ui`。
- 全量门禁：`make check`、`go test -race -count=1 ./...`、`gofmt -l`、`git diff --check`。
- 获得用户确认后部署本机 LaunchAgent，并从 DietPi 最新快照回读 Codex/Grok 指标与
  ConnectorHealth。

## 实际结果

- 失败基线：新增 State/Scheduler 回归在旧实现中保留 `codex.weekly`；Grok Unified Billing
  回归在旧实现中返回 `schema_changed: Grok credit usage is invalid`。
- Current State 现在只在成功批次上删除同一连接器本次缺失的旧指标，并为删除 ID 保留
  sequence tombstone；失败值保留、其他连接器隔离和乱序防护不变。
- Grok 旧百分比响应继续生成 42% 剩余；当前 Unified Billing 字段形状生成一个
  `precision=unavailable`、`status=unknown` 的 weekly 指标，由 TUI 显示 `N/A` 与真实 reset。
- 聚焦测试通过；`make check` 全仓通过；`go test -race -count=1 ./...` 全仓通过；
  `gofmt -l cmd internal` 与 `git diff --check` 无输出。
- 当前源码候选使用正式配置执行只读真实请求：`grok-main` 成功返回 1 metric（649ms），
  `codex-main` 成功返回 1 metric（760ms）。该只读候选验证未修改认证文件或 Provider 配置。
- 首次替换 binary 时，macOS 对仍已加载的 LaunchAgent 拒绝新 executable，系统日志记录
  `OS_REASON_CODESIGNING | Launch Constraint Violation`；原安装脚本成功恢复旧 binary 和运行服务。
  安装脚本现改为 macOS 先卸载注册、替换后重新注册，并在启动失败时恢复旧 binary 与服务；
  `bash -n scripts/install-local.sh`、`make check` 通过。
- 本机正式 LaunchAgent 已部署构建标识 `34c85bd-fix006`，安装 binary 与候选 SHA-256 均为
  `3f76b8ee313cd4a163d5be8774a370f95576a487749b43df17fb292017c1ad44`，服务状态为
  `installed=true running=true`。
- DietPi 快照版本 43（2026-09-06T10:46:59Z）只包含新鲜的 `codex-main.5h`，旧
  `codex-main.weekly` 已消失；`grok-main.weekly` 为无 value/limit、
  `precision=unavailable`、`status=unknown`，并保留 `resets_at=2026-09-11T03:08:36.18093Z`。
  两个 connector 的 `consecutive_failures` 均为 0，且 `last_error_at=null`。
- 独立用户级 `com.galendai.grok-auth-refresh` LaunchAgent 已注册：登录时及每 3600 秒调用一次
  官方 `grok models`，不发起推理。实测官方登录态和模型列表读取成功、退出码 0、执行后不常驻；
  stdout/stderr 分别记录在 `~/Library/Logs/grok-auth-refresh.out.log` 和
  `~/Library/Logs/grok-auth-refresh.err.log`。当前令牌仍有效，未通过篡改到期时间强制验证刷新。
- 未提交、未推送；DietPi display binary 未修改。
