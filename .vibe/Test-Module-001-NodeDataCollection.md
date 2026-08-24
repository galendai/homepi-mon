# 模块 001 测试文档：跨平台远端 daemon 与数据采集

> 对应规格：Module-Spec-001-NodeDataCollection.md
> 状态：Grok Usage 主动拉取自动化与 Mac→Pi 实机链路通过；官方 CLI `/usage` 人工对账与用户验收待执行
> 最近执行：2026-08-24，Grok Usage 主动请求、真实 provider test、LaunchAgent 和 DietPi TTY
> 结果说明：标记「待执行」或「部分通过」的外部用例不得视为通过；Windows 与额外 Linux
> daemon 按产品所有者指令暂缓，不得记作已通过。

Phase 2 Web Admin 对本模块 Provider/配置/凭据契约的复用与事务测试记录在
`Test-Module-005-WebAdmin.md`；本文件保留 Phase 1 CLI、连接器和 daemon 基线，不将规划用例
提前记为已执行。

## 1. Unit Test

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| U001 | MiniMax 返回 used=32、limit=100、滚动窗口信息 | 生成 remaining=68、percent=68%、window=rolling_5h、precision=exact | 通过。`TestCollectFallsBackOnceAndNormalizesQuota` 得到 value=68、limit=100、rolling_5h、exact 和 UTC reset | 通过 |
| U002 | Kimi 余额返回 available/voucher/cash 三值 | 生成三个独立 decimal 指标，币种和 observed_at 正确 | 通过。`TestCollectNormalizesKimiBalancesExactly` 保留 49.59、46.59、3.00 三个 decimal 指标 | 通过 |
| U003 | DeepSeek 返回总余额和赠金/充值明细 | 保留各余额语义，不与 Coding Plan 合并 | 通过。`TestCollectNormalizesDeepSeekBalancesExactly` 保留 110.00、10.00、100.00 与 CNY 语义，三个指标均通过协议校验 | 通过 |
| U004 | Codex `rate_limit.primary_window`、`secondary_window` 与 `used_percent/reset_at` | 生成 5 小时/每周窗口，precision=verified、source_kind=compatibility_api | 通过。`TestCollectReadsAuthJSONWithoutMutationOrRefresh` 得到 68%/90% 剩余、两个 UTC reset、verified/compatibility_api | 通过 |
| U005 | limit 缺失但 value 存在 | 不生成 percent，主值仍可展示 | 通过。`TestPercentUndefinedWithoutLimit`：Percent() 返回 ok=false，Severity 返回 unknown，UI 渲染 `-- LEFT` 与空进度条 | 通过 |
| U006 | Provider 时间为 UTC，展示时区 Asia/Shanghai | 存储 UTC，输出重置时间可被 UI 正确本地化 | 通过。`TestTimesRoundTripAsUTC`：08:32Z 在 Asia/Shanghai 渲染为 16:32 | 通过 |
| U007 | 远端本机 Codex `auth.json` 含 access_token、refresh_token、account_id | daemon 只读取必需字段，不输出/持久化完整 Token，不修改文件；传输模型无凭据字段 | 通过。`TestCollectReadsAuthJSONWithoutMutationOrRefresh` 比较采集前后 SHA-256、mode、size、mtime 均不变且只发一次 wham 请求；协议脱敏测试继续通过 | 通过 |
| U008 | 429 含 Retry-After=120 | 任意抖动值下次调度均不早于 120 秒 | 通过。`TestRetryAfterWins` 覆盖 5 个抖动位置，`NextDelay` 始终 ≥120 秒 | 通过 |
| U009 | 响应 schema 缺少必需字段 | 连接器 schema_changed，保留旧值，不输出错误新值 | 通过。`TestValidateRejectsBadPayloads` 拒绝 10 类非法负载；`TestApplyMetricsRejectsInvalid` 证明非法批次不改变已有状态；schema_changed 映射为 DisplayError 且保留旧值 | 通过 |
| U010 | 同一 source_epoch 的两个并发结果：version 10 后到，version 11 先到 | version 10 不覆盖 version 11 | 通过。`TestSupersedesRejectsVersionRegression` 与 `TestLateResultDoesNotOverwriteNewer`：低 seq 批次 applied=0，值保持 11，版本号不递增 | 通过 |
| U011 | Kimi Coding `/usages` 返回 5 小时和每周 limits | 正确排序两个窗口、计算使用率和 reset | 通过。`TestCollectFallsBackOnlyOn404AndSortsWindows` 输出 5h=32/40、weekly=75/100，顺序固定且 reset_in 转 UTC | 通过 |
| U012 | Kimi Coding `/usages` 返回 404、`/usage` 正常 | 只回退一次并生成 verified/compatibility 指标 | 通过。同一测试锁定请求序列仅为 `/usages`、`/usage`；`TestCollectDoesNotFallbackOnUpstreamFailure` 证明 5xx 不回退 | 通过 |
| U013 | 自定义 Base URL 重定向至元数据 IP | 请求被 SSRF 规则拒绝 | 通过。`config.validateRegion` 拒绝 `169.254.169.254`（元数据）、RFC1918、私网 IP；`http://` 仅允许 loopback。详见 `TestValidateProviderRegionCustom` | 通过 |
| U014 | 配置包含未知连接器和 0 秒周期 | ValidateConfig 返回可定位字段错误，服务不带坏配置启动 | 通过。`internal/config`：`interval < 5s` 拒绝、未知 type 通过 `ValidateWithRegistry` 拒绝、`stale_after < interval` 拒绝；`internal/connector` registry 返回已知 type | 通过 |
| U015 | Codex 配置尝试 Cookie 或第三方导出认证 | Phase 1 配置校验拒绝，仅允许 daemon 在远端本机读取受控登录态 | 通过。`TestReadAuthFileRejectsLinksAndUnsupportedShapes` 拒绝顶层 access_token/符号链接；配置测试拒绝 secret_ref 与相对 auth_file | 通过 |
| U016 | MiniMax 主路径 schema 不匹配、兼容路径契约匹配 | 按明确兼容策略切换并记录 endpoint capability，不产生重复请求风暴 | 通过。`TestCollectFallsBackOnPrimarySchemaMismatch` 仅调用主路径与兼容路径各一次；认证失败不回退 | 通过 |
| U017 | 配置 MiniMax/Kimi 的 region=global/cn/custom、Base URL 与秘密引用 | 解析到正确区域端点；配置与日志仅出现秘密引用/掩码 | 通过。`config.validateRegion` 校验 region/base_url/scheme/host；`provider list` 对 `secret_ref` 显示 `keyring:...xxxx` 掩码（`provider.maskRef`） | 通过 |
| U018 | daemon 重启后 source_epoch 改变且 version 从 0 开始 | 客户端接受新 epoch 的完整当前快照，不误判为版本倒退 | 通过。`TestSupersedesAcceptsNewEpochFromZero`、`TestRestartProducesNewEpochFromZero`、`TestDaemonRestartIsAcceptedAsNewEpoch`：epoch 变化时 version 42 → 0 被接受 | 通过 |
| U019 | 现有 Codex Token 过期并返回 401 | 进入 blocked_auth，最近成功指标立即标记 auth，只生成运行官方 CLI 的脱敏提示；不调用 OAuth/refresh/login | 通过。`TestCollectErrorAnnotatesLastSuccessfulMetric` 先成功再返回真实 `connector.Error(auth)`，旧值保留、指标进入 auth、TUI 卡片显示 AUTH；原始 401 文案未发布 | 通过 |
| U020 | Phase 1 配置包含两个 active node | ValidateConfig 拒绝并指出只允许一台主力电脑 | 通过。`protocol.SourceBinding` 拒绝第二 node；配置文件层不支持多 node（schema 只允许一个 `source_node`）。`TestForeignNodeIsRejected` 验证既不保存也不加载 | 通过 |
| U021 | 同一连接器先成功，随后超时或网络失败 | 保留旧值并立即标记 timeout/network，TUI 显示 STALE；恢复成功后清除错误 | 通过。`TestCollectErrorAnnotatesLastSuccessfulMetric` 覆盖 auth → 成功恢复 → timeout，最终卡片为 STALE | 通过 |
| U022 | 连接器成功后连续失败 3 次再恢复 | Health 保留最后成功时间，失败次数为 1/2/3，next_attempt_at 准确，恢复后清零 | 通过。`TestHealthTracksConsecutiveFailuresAndNextAttempt` 验证第三次失败与后续恢复状态 | 通过 |
| U023 | 默认 TLS、自有 cert/key 与单边证书配置 | 默认监听完成 TLS 握手；显式路径加载指定证书；cert/key 缺一或混用 auto 时启动前拒绝 | 通过。`TestListenAndServeUsesTLSWhenConfigured` 完成真实 TLS 握手；`TestResolveTLSConfigLoadsExplicitPair` 锁定自有证书；`TestValidateTLSCertificatePair` 覆盖缺项与混用 | 通过 |
| U024 | daemon 运行时撤销唯一设备并重启 | 新请求立即 401、既有 WebSocket 关闭；配置移除设备且重启后零设备 daemon 健康启动 | 通过。`TestDynamicRevocationRejectsRequestsAndClosesExistingStream` 验证动态 401/关流；`TestRevokeDeviceRemovesConfigAndKeepsDurableRevocation` 验证配置、ACL、凭据顺序；`TestServerAllowsZeroPairedDevices` 验证零设备启动 | 通过 |
| U025 | 无配置旧版 serve flags；文件配置带显式覆盖参数 | 旧版 token flag/环境变量可启动；文件配置只合并显式 addr/node/label/interval 覆盖 | 通过。`TestLegacyServeConfigAcceptsFlagToken` 与 `TestFileServeConfigAppliesExplicitOverrides` 覆盖两种路径 | 通过 |
| U026 | `provider edit -id mock-demo -interval 10s -enabled false` | 同一 FlagSet 完成解析并保存两个字段 | 通过。`TestProviderEditParsesAllFieldsInOneFlagSet` 保存后重载得到 interval=10s、enabled=false | 通过 |
| U027 | 全新目录执行 `config init` 后添加 `pi-kiosk` 与 `mock-demo` | 初始 devices/providers 均为空，添加不因占位项冲突 | 通过。`TestExampleConfigStartsWithoutPlaceholderDevice` 验证空列表；`TestInitialConfigAcceptsFirstProviderAndDevice` 执行初始化与首次添加 | 通过 |
| U028 | macOS stop 对已加载和未加载 LaunchAgent 执行 `launchctl kill` | 不触发 `exec: Stdout already set`；未加载状态为幂等成功 | 通过（命令级）。`TestPlatformStopRunsLaunchctlWithoutCombinedOutputConflict` 用临时 fake launchctl 验证命令实际执行；真实 LaunchAgent 状态待用户手动验收 | 通过 |
| U029 | Windows SystemRoot 为有效绝对路径 | PowerShell 可执行路径在启动前展开，不包含字面 `%SystemRoot%` | 通过（构建级）。`TestPowerShellExecutableExpandsSystemRoot` 验证展开函数；Windows amd64 node 与 install 测试包交叉编译通过 | 通过 |
| U030 | display 使用 HTTPS 证书固定客户端 | HTTP client、拨号及 TLS 握手均保留有限超时，静默丢包可进入重连 | 通过。`TestResolveHTTPClientPreservesTimeouts` 验证 client=30s、dial/TLS/header timeout 均非零 | 通过 |
| U031 | 文件回退分别保存 `keyring:a:b` 与 `keyring:a_b` | 生成不同文件且两项秘密独立往返，不互相覆盖 | 通过。`TestFileBackendReferenceNamesDoNotCollide` 生成两个 SHA-256 文件并独立读回两值 | 通过 |
| U032 | fixture 自带 stale_after 与 Provider 配置不同 | 进入 Current State 的每条指标统一采用 Provider `stale_after` | 通过。`TestTaskStaleAfterOverridesConnectorMetric` 将 fixture 1s 覆盖为 Provider 45s | 通过 |
| U033 | `provider add` 的密钥输入接口 | 不注册 `-secret` argv 参数；环境变量或 `-secret-stdin` 可写入秘密库 | 通过。`TestProviderAddDoesNotAcceptSecretArgv` 拒绝 argv；`TestReadProviderSecretUsesEnvironmentOrStdin` 覆盖两个安全输入路径 | 通过 |
| U034 | Provider 返回跨主机 302、非 JSON、超过 1 MiB 响应或错误体内含假 Key | 请求拒绝并分类，错误与日志不包含 URL 查询、响应体、Authorization 或假 Key | 通过。`TestGetJSONRejectsUnsafeOrInvalidResponses` 与 `TestGetJSONClassifiesAndRedactsHTTPFailures` 覆盖全部输入；超时另由 `TestGetJSONClassifiesTimeoutWithoutURLDetails` 覆盖 | 通过 |
| U035 | Codex `auth.json` 含 access/refresh/account 字段并记录 hash、mode、mtime、inode | 只使用 access/account；采集前后文件证据不变；不发 refresh/OAuth/login 请求 | 通过。采集只发一次 `/backend-api/wham/usage`，无 OAuth/refresh/login 请求；文件证据完全不变 | 通过 |
| U036 | Codex `auth_file` 是符号链接、相对路径、Cookie/第三方导出或缺少 access_token | 启动前或采集前拒绝，且不输出原始登录态 | 通过。`TestReadAuthFileRejectsLinksAndUnsupportedShapes`、`TestValidateProviderCredentialShapes` 与缺字段契约测试全部通过 | 通过 |
| U037 | 已存在旧 `dist/SHA256SUMS` 时重复执行 `make checksums` | 清单只含当前版本 12 个二进制，不包含清单自身，且可在 `dist/` 内完整自校验 | 通过。首轮发现 13 条自包含缺陷；修复后重复执行生成 12 条，`shasum -a 256 -c SHA256SUMS` 十二项全为 OK | 通过 |
| U038 | Codex 当前 `additional_rate_limits` 或旧 `code_review_rate_limit` 返回代码审查窗口 | 只识别代码审查附加额度，按 secondary 优先、primary 兜底生成最多一个 verified 指标；未知附加额度忽略 | 通过。`TestCollectParsesCurrentAndLegacyCodeReviewLimits` 覆盖当前/旧结构、unknown 忽略及 secondary 优先 | 通过 |
| U039 | E2E 在快照原子写入期间检查“目录只有一个文件” | 先取消并等待采集器/客户端退出，再枚举稳定目录；不得把合法瞬时 `.tmp-*` 当作历史文件 | 通过。`TestRestartRendersFromDiskWithNoHistory` 调整同步点后，`go test -race -count=20 ./internal/e2e` 连续通过 | 通过 |
| U040 | Codex 上游在默认 Go HTTP/2 路径返回网络失败、HTTP/1.1 正常 | 仅 Codex transport 明确启用 HTTP/1.1 并禁用 HTTP/2；仍只发一次 GET，不增加连接器重试 | 通过。`TestCollectUsesHTTP1WithoutApplicationRetry` 在同时支持 HTTP/2 的 TLS server 上锁定 `HTTP/1.1` 且仅 1 次请求；真实 Codex 请求无需 `GODEBUG` 即成功 | 通过 |
| U041 | macOS 服务已停止时执行 `uninstall`，`launchctl kill` 返回 `No process to signal.` | stop 视为幂等成功并继续 unload/remove；真正的 launchctl 错误仍返回失败 | 通过。`TestPlatformStopTreatsAlreadyStoppedAgentAsNoop` 锁定 exit 3 文案；真实 LaunchAgent 修复后成功卸载 | 通过 |
| U042 | MiniMax `model_remains` 首行为零额度媒体模型，后续 `general`/`MiniMax-M*` 使用 remaining percent 或有效 count | 选择聊天配额行；百分比结构即使 count=0 仍输出 5h/weekly；旧 count 结构保持兼容；unlimited 不伪装成 100% 有限额度 | 三条回归分别得到 remaining 97%/weekly 77%、旧 count 68/100、unlimited unavailable；不再返回 interval quota invalid | 通过（minimax_test） |
| U043 | `balance`/`cost` 值分别为整数、一位、三位小数，同时带金额 `limit`；quota 使用三位小数 | 所有金额 JSON 字段补零/四舍五入为两位，quota 保留原精度，序列化不改写内存中的 exact decimal | `TestMonetaryMetricsMarshalWithExactlyTwoDecimals`：`12.345→12.35`、`7→7.00`、金额 limit `9.999→10.00`；quota `12.345` 不变，原内存值不变 | 通过（protocol） |
| U044 | Kimi 返回 `available=0`、`voucher=0`、`cash=-1.0373199`；另测负 available、负 voucher、不可解析 cash | 真实负现金响应生成三条 exact 指标；现金保留负数；其他三个非法输入分别 `schema_changed` | `TestCollectAllowsNegativeKimiCashBalance` 精确保留三值；`TestCollectRejectsInvalidKimiBalanceSemantics` 的三个负向分支均返回 `schema_changed` | 通过（kimiapi） |
| U045 | DeepSeek 返回 `is_available=false`、`total=-1.80`、`granted=0.00`、`topped_up=-1.80`；另测负 granted、不可解析 total/topped-up | 真实负总额与充值额生成三条 exact 指标并按账户可用性标记 error；其他非法输入分别 `schema_changed` | `TestCollectAllowsNegativeDeepSeekSettledBalances` 精确保留三值并标记 `account unavailable`；`TestCollectRejectsInvalidDeepSeekBalanceSemantics` 的三个负向分支均返回 `schema_changed` | 通过（deepseek） |
| U046 | Grok `auth.json` 包含有效 `key/user_id/auth_mode/expires_at`，billing 响应包含 `config.creditUsagePercent=58` 与 weekly `currentPeriod` | 生成一个 `grok.weekly` weekly quota，value=42、limit=100、verified/compatibility_api，`observed_at` 取本次请求时间、重置时间取响应周期结束 | 通过。`TestCollectActivelyFetchesCreditsWithoutMutatingAuth` 锁定 `/v1/billing?format=credits`、CLI headers 和 value=42；时间固定为注入的 request time | 通过 |
| U047 | Grok `auth.json` 有多个登录项，其中一个有效期更晚；文件包含 `refresh_token`、邮箱等无关字段 | 选择有效期最新的登录项，只读取 `key/user_id/auth_mode/expires_at`；不输出或发送 refresh token、邮箱和原始 JSON | 通过。同一测试服务端只收到选中项 Bearer/`x-userid`，auth.json 的 SHA-256、mode、size、mtime、inode 均未变化 | 通过 |
| U048 | Grok auth 文件缺失、符号链接、非普通文件、超过大小上限、无有效登录项；billing 响应缺字段/非 weekly/百分比越界 | 返回 auth/invalid_config/schema_changed 分类错误，不执行 CLI、不刷新文件、不生成伪造百分比 | 通过。`TestCollectRejectsInvalidAuthAndBillingResponses` 与 `TestCollectRejectsUnsafeOrInvalidGrokAuthFile` 覆盖 HTTP 401、schema、过期登录、相对路径、符号链接和 1 MiB+ 文件 | 通过 |
| U049 | `grok_usage` 配置无 `secret_ref`，默认 `~/.grok/auth.json`，或使用 `~/`/绝对 `auth_file`；可选 `region=custom` + HTTPS `base_url` | 配置校验通过；相对路径、secret_ref、cn 和未通过 URL 校验的 custom 配置被拒绝；CLI/Web Admin 不要求 Provider Key | 通过。`ExpandGrokAuthFile`、连接器 ValidateConfig、CLI/Web Admin 字段标签和 global/custom 元数据测试均通过 | 通过 |
| U050 | 源码已包含 `grok_usage`，但用户 PATH 下的 `homepi-node` 仍为旧构建 | 安装后的 CLI 注册 Grok，`provider add`、`config validate`、`provider test` 均可执行 | 通过。发现 `/Users/galendai/.local/bin/homepi-node` 为 2026-08-14 构建且不含 `grok_usage`；重建 2026-08-24 构建后，临时配置实际返回 `added provider`、`ok (1 providers)`、`1 metrics`；临时目录已清理 | 通过 |

## 2. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | Mock Provider 正常响应，Pi 请求完整快照 | 返回 200、ETag、完整统一指标，不含秘密 | 通过。`TestSnapshotRequiresTokenAndReturnsETag`：200 + ETag，解码为合法快照，AuditJSON 无命中 | 通过 |
| E002 | Pi 携带相同 ETag 再请求 | 返回 304 或等价未变化响应 | 通过。`TestIfNoneMatchReturns304`：相同 ETag → 304；数据变化后同一 ETag → 200。修复了 ETag 基于响应体哈希导致永不命中的缺陷 | 通过 |
| E003 | 一个 Provider 先成功后超时，三个正常 | 快照含三个新值和一个立即 stale 的旧值，TUI 可见 STALE，进程存活 | 通过。`TestCollectErrorAnnotatesLastSuccessfulMetric` 验证旧值到 STALE 卡片链路，`TestOneFailingConnectorDoesNotBlockOthers` 验证其他连接器继续更新 | 通过 |
| E004 | `homepi-node` 断网 10 分钟后恢复 | 退避期间无请求风暴；恢复后自动更新并清除 stale | 部分通过。真实 Mac daemon 停止后 Pi 1 秒显示 OFFLINE 且快照哈希不变；Mac 启动后 3 秒自动回到 LIVE，无需重启 display。10 分钟长时程仍待执行 | 部分通过 |
| E005 | 设备 A 尝试读取设备 B 路径 | 返回同类拒绝，不泄露设备 B 指标 | 通过。自动化四种路径均返回相同 401；真实 Pi→Mac 请求再次确认无 Token、错误 Token、未知设备均为 401，无法区分或枚举设备 | 通过 |
| E006 | WebSocket 客户端落后多个版本 | 服务端发送完整快照或可验证 delta，不产生版本倒退 | 通过。hello 携带 last_epoch/last_snapshot_version；epoch 相同则跳过已有版本，epoch 不同则强制全量。Phase 1 只发 snapshot_full，见 IMPL-001 3.2 | 通过 |
| E007 | daemon 重启且没有指标数据库 | 生成新 source_epoch，重新采集当前值；不恢复或创建历史指标 | 通过。`TestDaemonRestartIsAcceptedAsNewEpoch`；daemon 侧无任何持久化写入路径 | 通过 |
| E008 | 五个 Phase 1 连接器使用测试账号，与官方 UI/CLI 或锁定参考实现对账 | DeepSeek/Kimi 余额一致；MiniMax/Codex/Kimi Coding 窗口、使用率和重置时间一致 | 部分通过。DeepSeek 官方端点当前 `-1.80/0.00/-1.80` 已经 Mac node→Pi 快照→TTY 逐层一致；Kimi 官方端点当前 `0.00/0.00/-1.04` 已通过相同链路。Codex 当前登录态真实请求成功；MiniMax 与各 Provider 控制台同观察点人工对账仍待执行 | 部分通过 |
| E017 | 当前用户官方 Grok CLI 已登录，读取本机 `~/.grok/auth.json`，同时观察 HomePi 进程、认证文件和网络行为 | `provider test` 主动返回 weekly 指标，与 CLI `/usage` 的百分比/周期/重置时间对账；HomePi 不启动 Grok CLI、不刷新或写回认证文件；Pi 只收到标准化指标 | 部分通过。真实 `homepi-node provider test -id grok-main` 返回 1 metric/515ms；LaunchAgent 重启后保持 `running=true`；Pi snapshot version 17 的 `grok-main.weekly` 为 value=41、observed_at=2026-08-24T09:16:51Z、source_kind=compatibility_api；TTY 60×20 显示 `Grok ... 41% LEFT RESET 4D OK`。官方 `/usage` 人工数值对账与抓包仍未执行 | 部分通过 |
| E009 | Codex/Kimi Coding 兼容端点返回未知 schema | 对应卡片 unavailable/compatibility error，其他四类连接器继续更新 | 部分通过。`TestAuthStateRendersOfficialCLIAction` 证明单个 Provider 降级时其余继续更新；真实兼容端点需 P1-06 | 部分通过 |
| E010 | 分别在 macOS、Windows PowerShell、Linux 安装 daemon | 均以当前用户身份完成安装、自启动、status/doctor、停止和卸载；不使用 root/SYSTEM | 部分通过。macOS 真实生命周期已通过；本轮将以 Mac 连接 Pi。Windows amd64 仅交叉构建、Linux 仅编译，二者按产品所有者 2026-08-11 指令暂缓，不记作已通过 | 部分通过 |
| E011 | 远端执行 provider add/edit/list/test/remove | 可配置国际站、国内站、自定义 URL 和 API Token 引用；list/doctor 不回显 Token | 通过。`cmd/homepi-node/provider.go` 子命令集；`list` 用 `maskRef` 仅暴露 `keyring:...xxxx`；`add` 将 secret 写入 zalando/go-keyring（或 0600 文件回退 + 告警）；`test` 触发 `connector.Build(...).Collect` | 通过 |
| E012 | Pi 仅获得 daemon LAN 地址和设备凭据 | Pi 无远端目录挂载且无法读取 `auth.json`；抓包只见认证后的标准化指标 | 部分通过。真实 TLS pin + 设备 Token 链路成功；用 Keychain 中真实 Token 对 Mac 配置/日志与 Pi 快照/journal 执行精确扫描均 clean。Pi 无远端文件访问路径；实机抓包仍未执行 | 部分通过 |
| E013 | 连续采集并重启 daemon/Pi | daemon 无历史指标文件；Pi 数据目录最多一份最近成功快照，无 SQLite/样本序列 | 通过。`TestOnlyOneSnapshotFileEverExists`：100 次写入后目录恒为 1 个文件；`TestRestartRendersFromDiskWithNoHistory` 在真实数据流下复核；手动冒烟 175 个快照版本后目录仍只有 `last-known-good.json` | 通过 |
| E014 | 监控登录态文件哈希/mtime并拦截子进程与刷新网络请求，触发认证过期 | 文件不变，无 CLI/OAuth/刷新子进程或请求；用户用官方 CLI 续期后下一轮采集恢复 | 未实现（需 P1-06）。结构性证据：代码中不存在 `os/exec` 导入，daemon 无法创建任何子进程 | 待执行 |
| E015 | `config init` → `device add` → TLS serve/display → `device revoke` → daemon 重启 | 首次配对可运行，HTTPS 握手成功，撤销即时生效且重启健康 | 部分通过。Mac Keychain 设备 Token、自动 TLS、Pi pinning 与真实双进程链路均已运行；Mac daemon 停止/启动后 3 秒恢复。真实 revoke 后再配对未执行，自动化撤销覆盖仍通过 | 部分通过 |

## 3. 性能与安全测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| P001 | 20 个连接器、并发上限 4、运行 1 小时 | 实际并发不超过 4，无 goroutine/内存持续增长 | 部分通过。`TestConcurrencyIsBounded`：20 个连接器、上限 4，实测峰值并发未超过 4。1 小时长时程与内存趋势需 P1-08 | 部分通过 |
| S001 | 扫描日志、配置文件、快照和 API 响应中的测试 Key | 不存在完整 Key、Cookie、Authorization Header 或 `auth.json` 内容 | 通过。`protocol.AuditJSON` 对 18 个禁用键 + 植入的假 Key 扫描：快照序列化、HTTP 响应、健康探针、落盘文件均无命中。`TestAuditJSONDetectsLeaks` 反向验证审计器本身有效 | 通过 |
| S002 | TLS 错误证书和未知 CA | 默认拒绝连接，不自动跳过校验 | 通过。`internal/tlsconfig/PinningTransport.VerifyConnection` 比对 leaf cert SHA-256 与 pin；不匹配返回 `ErrPinMismatch`，测试覆盖 `TestPinningTransportRejectsMismatchedPin` 与 `TestPinningTransportAcceptsMatchingPin` | 通过 |
| S003 | 检查 macOS Keychain、Windows Credential Manager/DPAPI、Linux Secret Service 适配 | Token 仅进入对应用户凭据库；受限文件回退会给出明确安全告警 | 通过。`internal/secretstore` 通过 zalando/go-keyring 走 macOS Keychain / Windows CredMgr / Linux Secret Service；探测失败回退到 `FileBackend`，每次 Set/Delete 写 stderr `WARNING: secretstore ... is using the 0600-file fallback`。`TestFileBackendWarnsOnSetDelete` 锁定告警 | 通过 |
| S004 | 注入伪造 refresh_token 并使 access token 过期 | daemon 不读取或使用 refresh_token 执行刷新，不写回任何登录态 | 部分通过。结构性证据：代码中无 `os/exec`，无 OAuth/refresh 代码路径；auth 错误只产出固定的官方 CLI 提示（`TestAuthFailurePublishesBlockedAuthOnly`）。真实登录态注入需 P1-06 | 部分通过 |

## 4. 执行记录

| 日期 | 范围 | 命令 | 结果 |
|---|---|---|---|
| 2026-08-10 | P1-01 ~ P1-03 | `make check`（gofmt + go vet + go test ./...） | 全部通过，78 个用例 |
| 2026-08-10 | P1-01 构建矩阵 | `make checksums` | 六平台 × 两个二进制 = 12 个产物，校验和已生成 |
| 2026-08-10 | P1-03 手动冒烟 | 见 IMPL-001 第 7 节 | 正常/数据变化/AUTH/断网/离线冷启动/无历史 六项均符合预期 |
| 2026-08-10 | FIX-001 审查整改 | `make check`、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`make checksums` | 84 个测试/子测试通过；race 通过；12 个跨平台产物构建并生成校验和 |
| 2026-08-10 | P1-04 新增包单测 | `go test -count=1 ./internal/config/... ./internal/secretstore/... ./internal/tlsconfig/... ./internal/deviceacl/... ./internal/connector/...` | config 12 例 + secretstore 7 例 + tlsconfig 8 例 + deviceacl 9 例 + connector 5 例，全部通过 |
| 2026-08-10 | P1-04 脱敏审计 | `grep -rn '"os/exec"' cmd/ internal/` | 仅 `internal/install/install_{darwin,linux,windows}.go` 三处；其他代码路径无子进程调用 |
| 2026-08-10 | P1-04 评审前手动冒烟记录 | 原记录声称完整执行 macOS 10 步 | 评审证明 TLS、stop、revoke 路径与该结论矛盾；原“通过”结论作废，不作为证据 |
| 2026-08-10 | P1-04 评审整改门禁 | `make check` | gofmt、go vet、全仓 go test 全部通过 |
| 2026-08-10 | P1-04 race | `go test -race -count=1 ./...` | 最终全仓通过。此前两次重跑分别触发 `TestOfflineOutranksCriticalInHeader` 同步 flaky 与 `TestForeignSnapshotIsRejected` TempDir 清理竞态；两项已在 P1-08 前置清理中修复，详见 IMPL-002 §9 |
| 2026-08-10 | P1-04 平台构建 | Windows amd64 node build + install test compile；Linux ARMv7 display build | 三项通过；Windows/Linux 实机服务管理未执行 |
| 2026-08-10 | P1-05/P1-06 连接器契约 | `go test -count=1 ./internal/connector/...` | DeepSeek、Kimi API、MiniMax、Codex、Kimi Coding 及共享 HTTP 门禁全部通过；真实账号请求未执行 |
| 2026-08-10 | P1-08 全仓门禁 | `make check`、`go test -race -count=1 ./...`、`go test -race -count=10 ./internal/e2e` | 191 个测试/子测试通过；全仓 race 通过；E2E race 连续 10 轮通过 |
| 2026-08-10 | Codex 真实正常态 | 当前用户官方 CLI `auth.json` + `provider test` | 默认 HTTP/2 复现 network；实现固定 HTTP/1.1 后成功解析 1 个指标，退出码 0；采集前后 SHA-256、mode、size、mtime、inode 完全不变；未输出 Token，未做控制台数值对账 |
| 2026-08-10 | macOS 真实服务生命周期 | 全新默认目录执行 config init/validate、install/start/status/stop/uninstall | 首次发现已停服务卸载返回 `No process to signal.`；修复后 running=true、连续三次 running=false、最终 installed=false，launchd job/plist 与测试目录全部清除 |
| 2026-08-10 | P1-08 构建/安全 | `make checksums`、清单自校验、`go mod verify`、`govulncheck ./...`、依赖 LICENSE 检查 | 12 个产物自校验通过；模块完整；未发现已知漏洞；10 个依赖模块均有宽松许可证文件 |
| 2026-08-11 | 目标 Pi 只读基线 | `ssh dietpi` 执行系统、TTY、节流与 HomePi 部署状态检查 | 实际用户 root；Debian 12/ARM64；tty1=60×20；`get_throttled=0x50000`；binary/config/unit/data 均不存在 |
| 2026-08-11 | Mac→Pi 真实链路 | 用户级 LaunchAgent、Keychain、自动 TLS、Pi systemd 与设备 Token | 两端 active；无/错 Token 与未知设备均 401；真实 Token 未进入配置、日志或 Pi 快照；daemon 停止后 1 秒 OFFLINE，重启后 3 秒 LIVE |
| 2026-08-11 | 修复后完整门禁 | `make check`、全仓 race、E2E race×10、snapstore/kioskunit race×20、`make checksums`、`go mod verify`、`govulncheck@v1.6.0`、LICENSE | 全部通过；12 个产物 SHA 自校验均 OK，未发现漏洞，依赖许可证齐全，`bin/`/`dist/` 已清理 |
| 2026-08-12 | MiniMax 真实响应兼容修复 | Web Admin 只读 Test、MiniMax 三条回归、`go test ./...`、`go test -race ./...`、`go vet ./...`、`git diff --check` | 修改前真实请求稳定复现 `schema_changed: MiniMax interval quota is invalid`；多模型行、remaining percent、旧 count、unlimited 自动化均通过；修改后真实 Key 复测需重启当前旧版 configure 进程后执行 |
| 2026-08-12 | 金额固定两位小数 | focused 协议/UI/API/快照/E2E、`go test ./...`、`go test -race ./...`、`go vet ./...`、`git diff --check` | `balance`/`cost` 的 value/limit 对外补零或 half away from zero 四舍五入为两位；内部 exact decimal 与非金额精度不变；全部通过 |
| 2026-08-14 | FIX-004 Kimi 负现金余额 | 失败基线、focused×10、`make check`、全仓 race、Windows amd64 node、Linux ARMv7 display、真实 `provider test`、Mac→Pi 快照 | 自动化全部通过；真实响应生成三条余额，Kimi Health `ok`、连续失败 0；配置和凭据未修改 |
| 2026-08-14 | FIX-005 DeepSeek 负余额 | 失败基线、focused×10、`make check`、全仓 race、Windows amd64 node、Linux ARMv7 display、真实 `provider test`、Mac→Pi 快照与 TTY | 自动化全部通过；真实响应生成三条余额，DeepSeek Health `ok`、连续失败 0；上游 `is_available=false` 正确显示 `ERROR`，配置和凭据未修改 |
| 2026-08-24 | Grok 主动拉取实现 | `go test ./internal/connector/grokusage ./internal/connector/providerutil ./internal/config ./cmd/homepi-node ./internal/providermeta`、`go test ./...`、`go vet ./...`、`git diff --check` | 全部通过；本机 `provider test -id grok-main` 返回 1 metric；LaunchAgent 新版 daemon 恢复 `running=true`；DietPi `homepi-display.service` active，snapshot version 17 收到 `grok-main.weekly` value=41、source_kind=`compatibility_api`；TTY 显示 `Grok ... 41% LEFT RESET 4D OK`；未执行官方 `/usage` 人工数值对账和抓包 |

测试临时文件（`tmp/`、`bin/`、`dist/`）已在执行后清除。Mock 夹具 `examples/mock-fixture.json`
是长期交付物，不含任何真实凭据。
