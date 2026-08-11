# Module Spec 005：本地 Web Admin 与配置编排

> 模块 ID：MOD-005
> 版本：0.1
> 所属阶段：Phase 2
> 状态：已认证，尚未实现

## 1. 模块目标

在运行 `homepi-node` 的远端主机上提供仅限本机访问的 Web Admin，把 Provider 草稿、字段校验、
只读测试、系统凭据、配置保存、用户级服务应用，以及目标 Raspberry Pi Display 的持久参数部署
收敛为可预览、可验证、可回滚的事务。正常配置不要求用户手工编辑 JSON、Pi 环境文件或组合
`provider/config/stop/start/status/ssh/systemctl` 命令。

Phase 2 当前实机范围为 macOS 主机与 `ssh dietpi` 可达的目标 Raspberry Pi；Windows/Linux
保留配置事务和 Web Admin 的平台接口，但按产品所有者决定暂缓实机生命周期验收。

## 2. 范围

### 2.1 包含

- `homepi-node configure` 启动的 loopback-only Web Admin。
- Provider 列表、添加、编辑、启停、删除、只读测试、秘密轮换和批量 Apply。
- 磁盘配置、运行中任务和 Pi 当前快照三层状态的区别展示。
- Provider 草稿的类型化字段、默认值、SSRF/URL/周期/凭据契约校验。
- 版本化系统凭据引用、原子配置保存、daemon 重启、健康确认和补偿回滚。
- Display profile、脱敏状态读取、固定允许操作的 SSH 部署、候选环境校验、原子替换、重启和回滚。
- `homepi-display config validate` 非交互候选配置校验。
- 现有 CLI 与 Web Admin 复用同一配置事务、连接器构造、秘密存储和服务管理层。

### 2.2 不包含

- LAN/公网 Web Admin、通用管理 API、多租户、RBAC 或 SaaS。
- Raspberry Pi 本地设置菜单、浏览器、键盘、触摸或入站管理端口。
- 任意 SSH 命令、远程 Shell、软件安装、主机重启或非 HomePi 文件管理。
- Provider 登录、登出、OAuth、设备授权、Token 刷新、账号切换或登录态写回。
- HomeLab 连接器、多页面轮播和远程显示命令；分别属于 Phase 3 和 Phase 4。
- Provider 原始响应浏览器、长期配置历史或指标历史数据库。

## 3. 子组件

| 子组件 | 职责 |
|---|---|
| Configurator Server | loopback HTTP 生命周期、会话、路由、静态资源和安全响应头 |
| Redacted State API | 输出 node、Provider、服务和 Display 的脱敏配置/运行状态 |
| Draft Store | 以当前配置为基线保存单会话草稿和待应用差异；不持久化秘密 |
| Config Transaction Service | 统一校验、秘密引用提交、配置保存、服务重启、健康确认和回滚 |
| Provider Test Service | 使用持久配置或草稿 + 内存秘密 overlay 调用只读连接器 Collect |
| Service Manager | 复用 macOS LaunchAgent、Windows 用户服务和 Linux user systemd 适配层 |
| Display Profile Store | 保存非秘密 SSH host、node URL、style、data dir 等期望状态 |
| Display Deployer | 通过固定 SSH 操作读取状态、传输候选环境、校验、替换、重启和回滚 |
| Audit Summary | 记录操作类型、对象、阶段和脱敏结果；不记录请求体或秘密 |

## 4. 启动与访问模型

- 命令入口为 `homepi-node configure`；默认选择随机可用端口并只绑定 `127.0.0.1`。
- 可同时绑定 `::1`，但任何非 loopback 地址必须在监听前被拒绝。
- 默认自动打开本机浏览器；无法打开时只打印不含秘密的本地 URL。
- 管理服务与 LAN snapshot API 使用不同 Listener，不复用设备 Bearer Token，也不新增 Pi 入站端口。
- 前端 HTML/CSS/JavaScript 使用 Go `embed` 内置，不依赖 CDN、外部字体、统计脚本或在线构建产物。
- 配置器闲置超时或收到 SIGTERM 后停止；停止不影响正在运行的 `homepi-node serve`。

## 5. 页面与状态模型

### 5.1 Overview

至少显示：

- node ID/label、安装状态、进程状态、当前进程启动时间和已加载 Provider 数。
- 磁盘配置版本/修改时间、是否存在未应用变更、最后一次 Apply 结果。
- Display systemd/连接状态、最新快照时间、source epoch/version 和当前主题。
- 不读取或展示 Provider Key、设备 Token、`auth.json` 内容或原始响应。

### 5.2 Provider 页面

每个 Provider 卡片必须区分：

- `configured`：存在于磁盘配置。
- `pending`：草稿与磁盘配置不同。
- `loaded`：当前 daemon 启动时已加载。
- `healthy`、`blocked_auth`、`rate_limited`、`network`、`schema_changed`、`disabled`。
- `shadowed`：已检测到其他启用连接器可能产生相同稳定 metric ID，例如 mock 与真实 Codex。

支持字段：`id`、`type`、`account_label`、`region`、`base_url`、`interval`、`stale_after`、
`enabled`、Codex `auth_file`、mock `mock_fixture` 以及 Key 型 Provider 的候选秘密。

普通界面隐藏 `secret_ref` 细节，只显示“未配置/已配置/待轮换”；高级诊断只显示掩码引用。

### 5.3 Display 页面

基础字段：

- SSH host，当前 Mac/DietPi 验收默认 `dietpi`。
- `device_id`、`node_url` 和 `rich|ascii` style。

自动推导或高级字段：

- `source_node_id` 来自 node config。
- `node_cert_pin` 来自当前 daemon 证书。
- `device_token_ref` 来自已配对设备，明文只在服务器端解析并经 SSH stdin 下发。
- `data_dir` 默认 `/var/lib/homepi-display`。
- 远端固定环境路径 `/etc/homepi-display/environment` 和 unit `homepi-display.service` 不允许用户改为任意路径/unit。

页面展示期望状态、Pi 当前脱敏状态和差异；应用后等待 Display 建立 WebSocket 并收到新快照，不能只以 `systemctl active` 判定成功。

## 6. Provider 草稿与只读测试

1. Draft 从最近一次磁盘配置生成；每次写操作带 revision，旧 revision 提交返回冲突。
2. 类型、ID、区域、URL、周期、`stale_after`、凭据类型和 Provider registry 在内存中完整校验。
3. Key 型 Provider 的候选秘密只进入密码字段和本次测试内存 overlay；不写 URL、浏览器存储、日志、临时文件或持久配置。
4. `codex_usage` 只读取选定 `auth_file`，继续拒绝 symlink/非普通文件且不修改登录态。
5. 测试调用与 CLI `provider test` 相同的只读 `Collect`，30 秒超时，输出指标数量、耗时和脱敏错误分类；不输出原始响应。
6. 测试成功不自动 Apply；用户必须检查差异并显式确认。
7. 删除、停用和可能导致指标消失的变更必须显示影响摘要；Provider 删除不默认保留孤立秘密。

## 7. 配置事务

### 7.1 Apply 前置条件

- 草稿 revision 仍基于当前磁盘配置；否则要求重新加载并解决冲突。
- 全配置通过 `ValidateWithRegistry`，所有新增/轮换凭据的启用 Provider 已通过草稿只读测试。
- 检测重复 Provider ID、无效秘密引用和可预判的稳定 metric ID 冲突；冲突默认阻止 Apply，用户明确解决后才能继续。
- 捕获上一份配置、服务状态和旧秘密引用；不把旧秘密返回给浏览器。

### 7.2 提交顺序

1. 为新增或轮换的秘密创建新版本化 Keychain 引用。
2. 原子保存指向新引用的非秘密配置。
3. 通过平台 Service Manager 重启 daemon。
4. 确认进程加载的配置 revision 与启用 Provider 数一致。
5. 等待变更 Provider 首次进入成功或明确失败状态。
6. 等待已连接 Display 收到新 source epoch 的快照；Display 离线时明确降级为“node 已应用、Display 待同步”，不伪写完整成功。
7. 完整成功后删除已无引用的旧秘密；保留 CLI 可复现的脱敏结果摘要。

### 7.3 补偿回滚

- 配置保存前失败：删除候选秘密，不修改正式配置或服务。
- 配置保存后、服务健康前失败：恢复上一份配置并重启上一版本配置；删除候选秘密。
- 旧配置恢复也失败：停止自动重试，保留两份非秘密配置证据和旧秘密引用，标记 `manual_recovery_required`；不使用更强删除或覆盖操作。
- Apply 互斥；同一时刻只允许一个事务，浏览器刷新不能重复提交。

## 8. Display 部署事务

### 8.1 SSH 边界

- 使用操作系统 SSH host key 验证和既有用户配置；不得自动接受未知 host key。
- 后端不提供 shell 字符串输入，只调用预定义操作与固定远端目标。
- Token 和候选环境内容只通过 SSH stdin 传输，不进入本地或远端进程 argv。
- 所有远端路径必须精确匹配 HomePi allowlist；拒绝符号链接、非普通文件、意外 owner/mode 和路径逃逸。

### 8.2 应用顺序

1. 读取并脱敏解析 Pi 当前环境、binary/version、unit 状态和最新快照元数据。
2. 在 Mac 内存构造候选环境；拒绝换行、NUL、shell 展开和未知键。
3. 经 stdin 写入固定目录内的新临时文件，设置 root:root、`0600`。
4. 在 Pi 上执行 `homepi-display config validate` 校验候选文件。
5. 将当前环境原子移动为单份 `.previous`，再原子替换正式环境。
6. 重启 `homepi-display.service`，确认 active、进程未崩溃循环、WebSocket 恢复和快照时间前进。
7. 任一步失败时保留正式旧配置或恢复 `.previous`；恢复后再次验证 active/连接，仍失败则停止并报告人工恢复步骤。

## 9. Web 安全

- Host 只接受实际 loopback listener；Origin 必须同源，禁用 CORS。
- 会话 Cookie 使用 HttpOnly、SameSite=Strict；所有状态修改需要 CSRF token 和非 GET 方法。
- CSP 至少禁止外部脚本、对象、frame 和任意远端连接；使用 `frame-ancestors 'none'`。
- 设置 `X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer` 和禁止缓存秘密表单响应。
- HTTP middleware 不记录请求体、Authorization、Cookie、查询参数中的敏感值；API 禁止通过 query 传秘密。
- 密钥输入不回填，状态 API 只返回布尔存在性和掩码引用；浏览器刷新后候选秘密必须重新输入。
- 对草稿大小、字段长度、JSON 深度、请求速率和并发测试数设置上限。

## 10. 配置兼容性

- `homepi-node` 现有 `config.json` 继续作为 daemon 非秘密配置来源，不为 Web UI 改用第二套 Provider schema。
- Display 部署期望状态存入独立的非秘密 `display-profiles.json`，避免旧 daemon 因未知字段拒绝 node 配置；文件权限 `0600`，只保存 token 引用，不保存 token。
- 新配置文件携带独立 schema version；未知主版本拒绝，新增可选字段按兼容规则处理。
- CLI Provider/Device/Service 命令改为复用 Config Transaction Service；现有脚本行为保持兼容，CLI 不要求浏览器。

## 11. 可观测性与恢复

- UI 展示脱敏 Apply 阶段：`validating`、`testing`、`saving`、`restarting`、`verifying`、`rolling_back`、`completed`、`failed`。
- 审计摘要至少含事务 ID、时间、对象 ID、字段名集合、结果和错误分类；不得含旧值、新值、密钥或环境全文。
- `doctor` 增加 Web Admin loopback 配置、最近 Apply 摘要和 Display profile 校验结果，不输出秘密。
- CLI 救援路径必须能够在 Web Admin 不可用时验证配置、恢复 `.previous` Display 环境并查看固定日志。

## 12. 验收标准

- 用户可在本机浏览器添加/测试 Codex、停用 mock 并一次 Apply；无需手工编辑配置或执行服务重启命令。
- Key 型 Provider 可测试、轮换和回滚；Keychain、配置、日志、HTTP 响应和 Pi 快照秘密扫描无命中。
- 无效 Provider 字段、错误 Key、配置并发修改、daemon 启动失败都不会破坏上一份有效配置。
- 用户可在 Web Admin 将目标 Pi 从 rich 切换到 ASCII 并切回；每次都自动验证、重启、恢复连接和确认快照。
- SSH 断开、候选环境错误、systemd 重启失败时 Pi 保留或恢复上一份环境；不留下历史配置集合或未约束临时文件。
- `lsof`/端口扫描证明 Web Admin 只监听 loopback，Pi 不新增入站端口。
- 当前 macOS + DietPi 实机流程通过；Windows/Linux 仅报告自动化/交叉构建，不伪写实机通过。
