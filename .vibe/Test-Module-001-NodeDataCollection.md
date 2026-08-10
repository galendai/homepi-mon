# 模块 001 测试文档：跨平台远端 daemon 与数据采集

> 对应规格：Module-Spec-001-NodeDataCollection.md
> 状态：P1-01 ~ P1-03 范围内的用例已执行；P1-04 ~ P1-06 范围内的用例仍未实现
> 最近执行：2026-08-10，macOS 25.5.0 arm64，Go 1.26.5，`make check`
> 结果说明：标记「未实现」的用例依赖尚未交付的功能（真实连接器、系统凭据库、跨平台服务），
> 不得视为通过。

## 1. Unit Test

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| U001 | MiniMax 返回 used=32、limit=100、滚动窗口信息 | 生成 remaining=68、percent=68%、window=rolling_5h、precision=exact | 未实现（需 P1-05 真实连接器）；标准化管线已由 Mock 覆盖：value=68/limit=100 产出 percent=68%、window=rolling_5h | 待执行 |
| U002 | Kimi 余额返回 available/voucher/cash 三值 | 生成三个独立 decimal 指标，币种和 observed_at 正确 | 未实现（需 P1-05） | 待执行 |
| U003 | DeepSeek 返回总余额和赠金/充值明细 | 保留各余额语义，不与 Coding Plan 合并 | 未实现（需 P1-05）；kind 隔离已验证：balance 卡片不渲染进度条，见 UI R007 | 待执行 |
| U004 | Codex `rate_limit.primary_window`、`secondary_window` 与 `used_percent/reset_at` | 生成 5 小时/每周窗口，precision=verified、source_kind=compatibility_api | 未实现（需 P1-06） | 待执行 |
| U005 | limit 缺失但 value 存在 | 不生成 percent，主值仍可展示 | 通过。`TestPercentUndefinedWithoutLimit`：Percent() 返回 ok=false，Severity 返回 unknown，UI 渲染 `-- LEFT` 与空进度条 | 通过 |
| U006 | Provider 时间为 UTC，展示时区 Asia/Shanghai | 存储 UTC，输出重置时间可被 UI 正确本地化 | 通过。`TestTimesRoundTripAsUTC`：08:32Z 在 Asia/Shanghai 渲染为 16:32 | 通过 |
| U007 | 远端本机 Codex `auth.json` 含 access_token、refresh_token、account_id | daemon 只读取必需字段，不输出/持久化完整 Token，不修改文件；传输模型无凭据字段 | 部分通过。传输模型侧已验证：协议类型不含任何凭据字段，`TestSerialisedSnapshotCarriesNoSecrets` 与 `AuditJSON` 对 18 个禁用键扫描无命中。`auth.json` 只读部分需 P1-06 | 部分通过 |
| U008 | 429 含 Retry-After=120 | 任意抖动值下次调度均不早于 120 秒 | 通过。`TestRetryAfterWins` 覆盖 5 个抖动位置，`NextDelay` 始终 ≥120 秒 | 通过 |
| U009 | 响应 schema 缺少必需字段 | 连接器 schema_changed，保留旧值，不输出错误新值 | 通过。`TestValidateRejectsBadPayloads` 拒绝 10 类非法负载；`TestApplyMetricsRejectsInvalid` 证明非法批次不改变已有状态；schema_changed 映射为 DisplayError 且保留旧值 | 通过 |
| U010 | 同一 source_epoch 的两个并发结果：version 10 后到，version 11 先到 | version 10 不覆盖 version 11 | 通过。`TestSupersedesRejectsVersionRegression` 与 `TestLateResultDoesNotOverwriteNewer`：低 seq 批次 applied=0，值保持 11，版本号不递增 | 通过 |
| U011 | Kimi Coding `/usages` 返回 5 小时和每周 limits | 正确排序两个窗口、计算使用率和 reset | 未实现（需 P1-06）；双窗口分组渲染已由 Mock 覆盖，见 E2E E-E001 | 待执行 |
| U012 | Kimi Coding `/usages` 返回 404、`/usage` 正常 | 只回退一次并生成 verified/compatibility 指标 | 未实现（需 P1-06） | 待执行 |
| U013 | 自定义 Base URL 重定向至元数据 IP | 请求被 SSRF 规则拒绝 | 未实现（需 P1-04） | 待执行 |
| U014 | 配置包含未知连接器和 0 秒周期 | ValidateConfig 返回可定位字段错误，服务不带坏配置启动 | 部分通过。`Scheduler.ValidateAll` 在监听前执行，`homepi-node serve` 对缺失 `-node-id`/`-device-id`/`-mock-fixture` 直接拒绝启动；完整配置 schema 需 P1-04 | 部分通过 |
| U015 | Codex 配置尝试 Cookie 或第三方导出认证 | Phase 1 配置校验拒绝，仅允许 daemon 在远端本机读取受控登录态 | 未实现（需 P1-04/P1-06） | 待执行 |
| U016 | MiniMax 主路径 schema 不匹配、兼容路径契约匹配 | 按明确兼容策略切换并记录 endpoint capability，不产生重复请求风暴 | 未实现（需 P1-05） | 待执行 |
| U017 | 配置 MiniMax/Kimi 的 region=global/cn/custom、Base URL 与秘密引用 | 解析到正确区域端点；配置与日志仅出现秘密引用/掩码 | 未实现（需 P1-04） | 待执行 |
| U018 | daemon 重启后 source_epoch 改变且 version 从 0 开始 | 客户端接受新 epoch 的完整当前快照，不误判为版本倒退 | 通过。`TestSupersedesAcceptsNewEpochFromZero`、`TestRestartProducesNewEpochFromZero`、`TestDaemonRestartIsAcceptedAsNewEpoch`：epoch 变化时 version 42 → 0 被接受 | 通过 |
| U019 | 现有 Codex Token 过期并返回 401 | 进入 blocked_auth，最近成功指标立即标记 auth，只生成运行官方 CLI 的脱敏提示；不调用 OAuth/refresh/login | 通过。`TestCollectErrorAnnotatesLastSuccessfulMetric` 先成功再返回真实 `connector.Error(auth)`，旧值保留、指标进入 auth、TUI 卡片显示 AUTH；原始 401 文案未发布 | 通过 |
| U020 | Phase 1 配置包含两个 active node | ValidateConfig 拒绝并指出只允许一台主力电脑 | 通过（来源侧）。`TestSourceBindingRejectsForeignNode`：非绑定 node 返回 ErrWrongSource；`TestForeignNodeIsRejected` 证明既不保存也不加载。配置文件层的双 node 校验需 P1-04 | 部分通过 |
| U021 | 同一连接器先成功，随后超时或网络失败 | 保留旧值并立即标记 timeout/network，TUI 显示 STALE；恢复成功后清除错误 | 通过。`TestCollectErrorAnnotatesLastSuccessfulMetric` 覆盖 auth → 成功恢复 → timeout，最终卡片为 STALE | 通过 |
| U022 | 连接器成功后连续失败 3 次再恢复 | Health 保留最后成功时间，失败次数为 1/2/3，next_attempt_at 准确，恢复后清零 | 通过。`TestHealthTracksConsecutiveFailuresAndNextAttempt` 验证第三次失败与后续恢复状态 | 通过 |

## 2. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | Mock Provider 正常响应，Pi 请求完整快照 | 返回 200、ETag、完整统一指标，不含秘密 | 通过。`TestSnapshotRequiresTokenAndReturnsETag`：200 + ETag，解码为合法快照，AuditJSON 无命中 | 通过 |
| E002 | Pi 携带相同 ETag 再请求 | 返回 304 或等价未变化响应 | 通过。`TestIfNoneMatchReturns304`：相同 ETag → 304；数据变化后同一 ETag → 200。修复了 ETag 基于响应体哈希导致永不命中的缺陷 | 通过 |
| E003 | 一个 Provider 先成功后超时，三个正常 | 快照含三个新值和一个立即 stale 的旧值，TUI 可见 STALE，进程存活 | 通过。`TestCollectErrorAnnotatesLastSuccessfulMetric` 验证旧值到 STALE 卡片链路，`TestOneFailingConnectorDoesNotBlockOthers` 验证其他连接器继续更新 | 通过 |
| E004 | `homepi-node` 断网 10 分钟后恢复 | 退避期间无请求风暴；恢复后自动更新并清除 stale | 部分通过。退避策略已单测（`TestTransientBackoffGrowsAndCaps`：指数增长并封顶 10 分钟）；客户端重连退避 2–60 秒带 ±20% 抖动。10 分钟长时程实测需 P1-08 | 部分通过 |
| E005 | 设备 A 尝试读取设备 B 路径 | 返回 403/404，不泄露设备 B 指标 | 通过。`TestDeviceScopedAuthorization`：无 Token、错 Token、他设备路径、不存在设备均返回 401，且四种响应文案完全相同，无法用于枚举设备 | 通过 |
| E006 | WebSocket 客户端落后多个版本 | 服务端发送完整快照或可验证 delta，不产生版本倒退 | 通过。hello 携带 last_epoch/last_snapshot_version；epoch 相同则跳过已有版本，epoch 不同则强制全量。Phase 1 只发 snapshot_full，见 IMPL-001 3.2 | 通过 |
| E007 | daemon 重启且没有指标数据库 | 生成新 source_epoch，重新采集当前值；不恢复或创建历史指标 | 通过。`TestDaemonRestartIsAcceptedAsNewEpoch`；daemon 侧无任何持久化写入路径 | 通过 |
| E008 | 五个 Phase 1 连接器使用测试账号，与官方 UI/CLI 或锁定参考实现对账 | DeepSeek/Kimi 余额一致；MiniMax/Codex/Kimi Coding 窗口、使用率和重置时间一致 | 未实现（需 P1-05/P1-06 与真实账号） | 待执行 |
| E009 | Codex/Kimi Coding 兼容端点返回未知 schema | 对应卡片 unavailable/compatibility error，其他四类连接器继续更新 | 部分通过。`TestAuthStateRendersOfficialCLIAction` 证明单个 Provider 降级时其余继续更新；真实兼容端点需 P1-06 | 部分通过 |
| E010 | 分别在 macOS、Windows PowerShell、Linux 安装 daemon | 均以当前用户身份完成安装、自启动、status/doctor、停止和卸载；不使用 root/SYSTEM | 未实现（需 P1-04）。已具备：六平台交叉构建产物、`doctor` 对 root 运行发出告警 | 待执行 |
| E011 | 远端执行 provider add/edit/list/test/remove | 可配置国际站、国内站、自定义 URL 和 API Token 引用；list/doctor 不回显 Token | 未实现（需 P1-04） | 待执行 |
| E012 | Pi 仅获得 daemon LAN 地址和设备凭据 | Pi 无远端目录挂载且无法读取 `auth.json`；抓包只见认证后的标准化指标 | 部分通过。`TestNoSecretsCrossTheWireOrHitDisk`：发布的快照与落盘的快照均通过脱敏审计。Pi 侧代码不含任何远端文件访问路径。实机抓包需 P1-08 | 部分通过 |
| E013 | 连续采集并重启 daemon/Pi | daemon 无历史指标文件；Pi 数据目录最多一份最近成功快照，无 SQLite/样本序列 | 通过。`TestOnlyOneSnapshotFileEverExists`：100 次写入后目录恒为 1 个文件；`TestRestartRendersFromDiskWithNoHistory` 在真实数据流下复核；手动冒烟 175 个快照版本后目录仍只有 `last-known-good.json` | 通过 |
| E014 | 监控登录态文件哈希/mtime并拦截子进程与刷新网络请求，触发认证过期 | 文件不变，无 CLI/OAuth/刷新子进程或请求；用户用官方 CLI 续期后下一轮采集恢复 | 未实现（需 P1-06）。结构性证据：代码中不存在 `os/exec` 导入，daemon 无法创建任何子进程 | 待执行 |

## 3. 性能与安全测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| P001 | 20 个连接器、并发上限 4、运行 1 小时 | 实际并发不超过 4，无 goroutine/内存持续增长 | 部分通过。`TestConcurrencyIsBounded`：20 个连接器、上限 4，实测峰值并发未超过 4。1 小时长时程与内存趋势需 P1-08 | 部分通过 |
| S001 | 扫描日志、配置文件、快照和 API 响应中的测试 Key | 不存在完整 Key、Cookie、Authorization Header 或 `auth.json` 内容 | 通过。`protocol.AuditJSON` 对 18 个禁用键 + 植入的假 Key 扫描：快照序列化、HTTP 响应、健康探针、落盘文件均无命中。`TestAuditJSONDetectsLeaks` 反向验证审计器本身有效 | 通过 |
| S002 | TLS 错误证书和未知 CA | 默认拒绝连接，不自动跳过校验 | 未实现（需 P1-04 TLS） | 待执行 |
| S003 | 检查 macOS Keychain、Windows Credential Manager/DPAPI、Linux Secret Service 适配 | Token 仅进入对应用户凭据库；受限文件回退会给出明确安全告警 | 未实现（需 P1-04） | 待执行 |
| S004 | 注入伪造 refresh_token 并使 access token 过期 | daemon 不读取或使用 refresh_token 执行刷新，不写回任何登录态 | 部分通过。结构性证据：代码中无 `os/exec`，无 OAuth/refresh 代码路径；auth 错误只产出固定的官方 CLI 提示（`TestAuthFailurePublishesBlockedAuthOnly`）。真实登录态注入需 P1-06 | 部分通过 |

## 4. 执行记录

| 日期 | 范围 | 命令 | 结果 |
|---|---|---|---|
| 2026-08-10 | P1-01 ~ P1-03 | `make check`（gofmt + go vet + go test ./...） | 全部通过，78 个用例 |
| 2026-08-10 | P1-01 构建矩阵 | `make checksums` | 六平台 × 两个二进制 = 12 个产物，校验和已生成 |
| 2026-08-10 | P1-03 手动冒烟 | 见 IMPL-001 第 7 节 | 正常/数据变化/AUTH/断网/离线冷启动/无历史 六项均符合预期 |
| 2026-08-10 | FIX-001 审查整改 | `make check`、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`make checksums` | 84 个测试/子测试通过；race 通过；12 个跨平台产物构建并生成校验和 |

测试临时文件（`tmp/`、`bin/`、`dist/`）已在执行后清除。Mock 夹具 `examples/mock-fixture.json`
是长期交付物，不含任何真实凭据。
