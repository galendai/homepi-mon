# Module Spec 001：跨平台远端节点 daemon 与数据采集

> 模块 ID：MOD-001  
> 版本：0.10
> 状态：已认证

## 1. 模块目标

在受信任的 macOS、Windows 或 Linux 主力开发电脑上，以当前用户身份运行 `homepi-node` daemon，安全访问 Provider 官方 API、本机 CLI 登录态和系统凭据库，将结果转换为统一指标快照，并通过受保护的 LAN 接口提供给 Raspberry Pi。Pi 不得读取远端文件系统、`auth.json` 或任何 Provider 凭据。

Phase 1 只支持一个活动远端 node。daemon 对 CLI 登录态严格只读；登录、登出、账号切换、设备授权、Token 刷新和登录态写回全部由 Provider 官方 CLI 负责。

## 2. 职责边界

### 2.1 负责

- 连接器配置、调度、超时、限流和退避。
- Provider 认证、响应解析、单位与窗口标准化。
- 数据精度、新鲜度、错误状态和来源标注。
- 当前指标和连接器健康的内存状态维护。
- 完整快照、增量事件和诊断接口。
- 凭据保护与日志脱敏。
- 远端 Provider 区域、Base URL、账号和凭据引用配置。
- 三平台用户级 daemon 安装、自启动和诊断。

### 2.2 不负责

- TUI 布局和 Kiosk 渲染。
- 在 Provider 上发起产生费用的“测试请求”来估算余额。
- 抓取网页或保存登录 Cookie。
- 执行远程任意命令。
- 保存历史指标、趋势样本或 Provider 原始响应。
- 向 Pi 传输 Provider Token、Cookie、Authorization Header 或登录态文件内容。
- 调用或实现任何登录、登出、OAuth、设备授权、Token 刷新或登录态写回流程。
- 在 Phase 1 聚合、故障切换或展示多个远端 node。

## 3. 子组件

| 子组件 | 职责 |
|---|---|
| Connector Registry | 注册连接器类型、版本、能力和配置 schema |
| Scheduler | 触发采集，控制并发、抖动、超时与退避 |
| Provider Connector | 调用单一官方数据源并输出原始读数 |
| Normalizer | 映射指标类型、窗口、单位、精度和状态 |
| Current State | 在内存中保存当前快照、连接器状态和 daemon epoch |
| Provider Config Service | 为 CLI 与 Phase 2 Web Admin 统一配置账号、区域、Base URL、周期、系统凭据引用、测试和应用事务 |
| Device API | 提供设备作用域快照与事件流 |
| Diagnostics | 输出脱敏健康信息和自监控指标 |

## 4. 连接器接口语义

每个连接器实现以下逻辑能力：

- `ID`：稳定标识，例如 `openai-org-usage`。
- `Capabilities`：可输出的 metric kind、精度和所需权限。
- `ValidateConfig`：离线校验字段、URL、周期和阈值。
- `Collect`：在 context 超时范围内返回读数或分类错误。
- `Normalize`：将上游读数映射到 ProviderMetric。
- `Redact`：清除错误信息中的令牌、账号标识和敏感响应。
- `Health`：连接器可提供上游能力健康；Scheduler 统一维护最后成功、连续失败次数和下次允许请求时间。

连接器不得创建自己的无限重试循环；重试统一由 Scheduler 管理。

## 5. 首批连接器

| 连接器 | 默认周期 | 权限/凭据 | 输出 | 精度 |
|---|---:|---|---|---|
| MiniMax Coding Plan | 60 秒 | Token Plan/Coding Plan Key | 5 小时窗口余量、重置信息 | exact；双路径契约测试 |
| Codex Usage | 5 分钟 | daemon 在远端本机只读 Codex 登录态 | 5 小时、每周、可选代码审查窗口 | verified/compatibility |
| Kimi Coding Plan | 5 分钟 | Kimi Coding 专用 `sk-kimi-*` Key | 5 小时、每周窗口 | verified/compatibility |
| DeepSeek API Balance | 5 分钟 | DeepSeek API Key | 总/赠金/充值余额 | exact |
| Kimi API Balance | 5 分钟 | Moonshot API Key | 可用/代金券/现金余额 | exact |

周期是默认建议，若上游 rate limit 更严格则以其为准。

Phase 1 不实现 OpenAI API Organization Usage、GLM、Gemini 或本地 Token 账本。

### 5.1 Phase 1 端点与兼容策略

参考实现基线为 `liubaicai/ai-usage-board` master commit `45a99784d19469d64abd3f49fc3303150413dd9f`。本规格只借鉴端点行为和适配器边界，不复制其 Web UI、Cookie 认证或单 JSON 密钥存储方案。

| 连接器 | 主路径 | 回退/备注 |
|---|---|---|
| MiniMax Coding Plan | 官方 `/v1/token_plan/remains` 或账号实际可用官方路径 | 兼容参考项目 `/v1/api/openplatform/coding_plan/remains`；`model_remains` 可能同时包含聊天、语音、视频和图片行，优先 `general`/`MiniMax-M*`，否则选择第一个具备有效有限额度证据的行；权威 `current_*_remaining_percent` 优先于旧 count 推导，status=3 表示 unlimited，不渲染为有限额度 |
| Codex Usage | `https://chatgpt.com/backend-api/wham/usage` | 参考项目明确标记为社区逆向接口；当前 macOS/Go 1.26.5 实测 HTTP/2 失败而 HTTP/1.1 成功，因此仅此连接器固定 HTTP/1.1，不做应用层重试；主窗口读取 `rate_limit`，可选代码审查兼容旧 `code_review_rate_limit` 与当前 `additional_rate_limits` 嵌套结构；daemon 只在本机读取登录态，Pi 不接触 `auth.json`；不用 Cookie；接口变化时显示 N/A/compatibility error |
| Kimi Coding Plan | `https://api.kimi.com/coding/v1/usages` | 404 回退 `/usage`；与开放平台 Key 隔离；接口变化时显示 N/A/compatibility error |
| DeepSeek API | 官方 `https://api.deepseek.com/user/balance` | 无网页回退 |
| Kimi API | 官方区域 `/v1/users/me/balance` | 国内/国际 base URL 按 Key 区域配置 |

兼容性连接器必须保存脱敏契约样本、识别必需字段，并把 schema 变化与认证失败区分开。

## 6. 调度与错误策略

### 6.1 调度

- 启动后添加 0–10% 随机抖动，避免同时请求所有 Provider。
- 全局默认最大并发 4，单 Provider 默认并发 1。
- 成功后按固定周期调度；短暂失败使用指数退避并加抖动。
- 401/403 认证错误进入 `blocked_auth`，至少 15 分钟后或配置变更时再试。
- `blocked_auth` 的诊断只提示用户在远端运行对应官方 CLI；daemon 不启动 CLI、不刷新 Token、不修改凭据文件。官方 CLI 完成续期后，下一次低频探测或 daemon 重启自动恢复。
- 429 将 `Retry-After` 作为不可突破的最小等待时间；允许不提前请求的正向抖动，无该字段时按指数退避。
- 5xx/网络超时最多快速重试 1 次，随后交给正常退避。
- 成功后连续失败次数清零；Health 保留最近一次成功时间，并记录应用抖动后的下次尝试时间。

### 6.2 错误分类

| 错误 | 状态 | 是否保留旧值 | 是否重试 |
|---|---|---:|---:|
| auth | error | 是 | 低频/配置变更 |
| rate_limited | delayed | 是 | 是，遵循窗口 |
| timeout/network | stale | 是 | 是 |
| schema_changed | error | 是 | 低频，需升级 |
| unsupported | unavailable | 否 | 否 |
| invalid_config | error | 否 | 配置变更后 |

连接器已有成功指标时，后续分类错误必须更新这些旧指标的 `error_class` 与脱敏提示；不得只更新
`connector_health`。下一次成功采集用新指标清除失败状态。

## 7. 标准化规则

- 金额使用 decimal 语义，禁止用二进制浮点直接累计账单。`balance`/`cost` 内部保留上游精度，在统一协议边界将 `value` 和金额类 `limit` 四舍五入为固定两位小数；非金额指标不受影响。
- 时间统一为 UTC RFC 3339，TUI 按本地时区显示。
- 若上游只给 `used` 和 `limit`，可计算 remaining 和 percent，并标记派生字段。
- 若 limit 未知，不计算 percent。
- rolling window 必须保留窗口名称和 resets_at；无法准确计算重置时间时显示“滚动恢复”。
- API 余额和订阅配额使用不同 metric ID 和卡片分组。

## 8. 状态与持久化

- 当前指标、连接器退避状态和健康摘要只保存在 daemon 内存中。
- Current State 在内存中记录 connector 与其最近成功 metric ID 的归属，用于失败状态传播；该归属不进入 wire schema。
- daemon 每次启动生成新的 `source_epoch`，`snapshot_version` 在该 epoch 内从零单调递增。
- 非秘密 Provider 配置和配对设备配置可以持久化，但不得包含明文 Token。
- Provider 秘密只保存于操作系统凭据库；Pi 侧只持久化一份最近成功的脱敏快照。
- 不创建 SQLite 指标库、`metric_samples`、快照序列或趋势文件。

## 9. API

### 9.1 `GET /v1/devices/{id}/snapshot`

- 验证设备身份和路径中的设备 ID 一致。
- 返回该设备订阅的指标子集。
- 支持 `If-None-Match`。
- 响应不得包含 Provider 凭据或原始敏感错误。

### 9.2 WebSocket stream

- `hello` 必须包含客户端版本、schema 主版本和最后快照版本。
- daemon 第一次成功采集到至少一个指标前只发送心跳，不发送会覆盖 Pi 最近成功数据的空快照。
- 若 epoch 相同且版本差距可补齐，发送 delta；否则发送当前完整快照。
- 单设备慢消费者缓冲有上限，溢出时丢弃 delta 并要求重新拉全量。

## 10. 配置

配置至少包含：

- daemon 监听地址、TLS/配对模式和稳定的 `source_node_id`。
- Phase 1 恰好一个活动 node；检测到第二个活动 node 配置时校验失败。
- 设备与其订阅的指标/页面。
- 连接器类型、账号别名、区域（`global`/`cn`/`custom`）、Base URL、启用状态、刷新周期和 stale_after。
- 阈值、币种和时区。
- 秘密引用名称，不直接写明文 Key。

提供 `homepi-node provider add/edit/list/test/remove` 命令及等价 Windows PowerShell 调用。`test` 只使用现有凭据调用只读用量/余额端点，绝不触发登录或 Token 刷新；`list` 只显示秘密引用和掩码，不回显 Token。配置加载顺序和覆盖规则必须固定并在实现文档中说明。

Phase 2 的 Module 005 在本模块配置契约之上增加 loopback Web Admin。Web 与 CLI 必须复用同一
Provider Config Service、connector registry、secret store 和 platform service manager，不得复制
第二套 Provider schema 或降低本模块现有校验。Web Admin 的草稿、版本化秘密引用、Apply、
健康确认、回滚和浏览器安全要求以 `Module-Spec-005-WebAdmin.md` 为准。

- `config init` 只生成最小非秘密配置，不预置设备或 Provider；首次配置分别由
  `device add` 与 `provider add` 完成。
- `codex_usage` 可配置 `auth_file`，缺省为当前用户的 `~/.codex/auth.json`；该字段只允许
  绝对路径或 `~` 开头路径。daemon 只读取 `tokens.access_token` 与 `tokens.account_id`，
  不使用 `refresh_token`、不读取 Cookie、不修改文件。
- `serve -config` 与默认配置文件模式均合并显式 `-addr`、`-node-id`、`-node-label`、
  `-interval` 覆盖项；未显式传入的 flag 不改变文件配置。
- Provider 的 `stale_after` 是采集任务的统一新鲜度预算，采集结果进入 Current State 前必须
  覆盖到每条指标，不依赖连接器或测试夹具自行填写。
- TLS 的 `cert`/`key` 必须同为 `auto` 或同时为可读路径；显式路径必须被实际加载。

## 11. 安全要求

- 进程不得使用 root/Administrator；必须运行在拥有目标 CLI 登录态的当前用户上下文。
- macOS 使用 Keychain，Windows 使用 Credential Manager/DPAPI，Linux 使用 Secret Service；权限 `0600` 文件仅作为带安全告警的回退。
- Provider 密钥不得出现在 argv；CLI 仅接受 `HOMEPI_PROVIDER_SECRET` 或显式标准输入。
- 文件凭据回退的文件名必须由完整引用进行无碰撞编码或密码学哈希派生。
- URL 仅允许 HTTPS，局域网显式配置例外需给出警告。
- 自定义 Base URL 防止 SSRF：默认只允许配置的固定 host，禁止跟随到内网元数据地址。
- Phase 1 不启动任何 Coding CLI 子进程；官方 CLI 是登录与 Token 续期的唯一责任方，daemon 对本机登录态严格只读且禁止写回。
- Codex 登录态禁止跟随符号链接；采集前后文件内容、权限、mtime 与 inode 不得因 daemon 改变。
- 禁止 Codex 网页 Cookie、浏览器会话和第三方聚合导出作为 Phase 1 凭据来源。
- 原始 CLI/API 输出不持久化；诊断信息只保留字段白名单和脱敏错误分类。
- Provider HTTP 响应最多读取 1 MiB；不跟随跨主机重定向，拒绝非 JSON 成功响应，错误中
  只保留 HTTP 状态与分类，不保留请求查询、响应体或认证头。
- LAN 响应必须经过认证，且任何响应 schema 均不得包含凭据字段或原始认证头。
- `device revoke` 必须原子持久化撤销哈希、从配置移除设备并删除凭据；运行中的 daemon
  必须在后续鉴权和既有事件流中重新读取撤销状态。零设备配置允许 daemon 启动健康探针，
  但所有设备接口均返回统一未授权响应。

## 12. 跨平台运行规格

| 平台 | 安装与自启动 | 用户上下文要求 |
|---|---|---|
| macOS | LaunchAgent plist + CLI 安装/卸载命令 | 登录用户会话，允许读取该用户 Keychain 与 CLI 登录态 |
| Windows | PowerShell 安装脚本创建当前用户后台任务或等价用户服务 | 不使用 SYSTEM；读取当前用户 Credential Manager/DPAPI 与 CLI 登录态 |
| Linux | `systemd --user` unit，可启用 linger | 不使用 root system service；读取该用户 Secret Service/CLI 登录态 |

三平台必须提供相同的 `start`、`stop`、`status`、`doctor` 和 Provider 配置语义；`stop` 与
`uninstall` 对已停止服务必须幂等成功。平台差异只存在于安装与秘密存储适配层。

当前 Phase 1 实机验收剖面（2026-08-11）只使用本机 macOS daemon 与目标 Pi；Windows 与额外
Linux daemon 的实机生命周期按产品所有者指令暂缓。该剖面只缩小本轮外部证据范围，不改变
三平台产品契约；Windows/Linux 仍须保持构建、单测和后续补测能力。

## 13. 验收标准

- 任一连接器超时不影响其他连接器生成快照。
- 同一 metric ID 的版本单调递增，旧结果不能覆盖新结果。
- 对官方 API 的抽样数据与控制台一致，允许的上游延迟有明确标签。
- Codex/Kimi Coding Plan 与同账号官方 UI/CLI 或锁定版本参考实现一致；兼容端点变化时安全降级为 unavailable。
- 任何日志和设备响应不包含完整 Provider Key。
- 断网后旧数据继续可取并进入 stale。
- 配置校验能在启动前指出未知连接器、非法周期和缺失秘密引用。
- macOS、Windows PowerShell 和 Linux 均能以当前用户身份安装、自启动、停止和诊断 daemon。
- 在网络和文件权限层面验证 Pi 无法读取远端 `auth.json`，抓包确认只传输标准化脱敏指标。
- daemon 运行与重启后均不生成历史指标记录；Pi 上最多存在一份最近成功快照。
- 用户可在远端分别配置国际站、国内站或自定义 Base URL，并用秘密引用完成连接器只读测试。
- 认证过期测试能证明 daemon 未创建 CLI/OAuth 子进程、未发起刷新请求、未修改登录态文件，仅返回 `blocked_auth` 与官方 CLI 操作提示。
- Phase 1 只接受一个活动远端 node，第二个 node 配置在启动前被明确拒绝。
