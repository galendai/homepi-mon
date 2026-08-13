# Module Spec 005：本地 Web Admin 与配置编排

> 模块 ID：MOD-005
> 版本：0.7
> 所属阶段：Phase 2
> 状态：已实现并完成 macOS + DietPi 代码门禁与实机事务验收；等待用户检查

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
- Web Admin 构建与正式用户服务目标二进制的版本漂移检测、提示和 Apply 运行目标说明。

### 2.2 不包含

- LAN/公网 Web Admin、通用管理 API、多租户、RBAC 或 SaaS。
- Raspberry Pi 本地设置菜单、浏览器、键盘、触摸或入站管理端口。
- 任意 SSH 命令、远程 Shell、软件安装、主机重启或非 HomePi 文件管理。
- Provider 登录、登出、OAuth、设备授权、Token 刷新、账号切换或登录态写回。
- HomeLab 连接器和多页面轮播仍属于 Phase 3；Phase 4 的远程显示命令由本模块提供受限的本机 Web Admin 入口，
  但不改变命令协议或 Pi 连接边界。
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
| Runtime Version Probe | 读取服务目标、执行受限 `--version` 探测并只输出脱敏构建一致性状态 |
| Display Profile Store | 保存非秘密 SSH host、node URL、style、data dir 等期望状态 |
| Display Deployer | 通过固定 SSH 操作读取状态、传输候选环境、校验、替换、重启和回滚 |
| Kiosk Control Adapter | 从 loopback Web Admin 接收固定操作 DTO，复用独立控制凭据调用 Phase 4 命令 API，并返回脱敏 ACK/结果 |
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
- Web Admin 构建版本、正式服务目标版本、服务安装/运行状态和版本一致性。
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

Display 页面同时提供 Remote Kiosk control：目标只取已保存 profile 的 `device_id`，操作限定为切页、前后页、临时轮播、刷新、受限消息和亮度。每次操作展示 command ID、sequence、阶段状态和稳定错误码；浏览器不展示或保存控制凭据。

### 5.4 视觉与交互体验

- 界面采用清新的“薄荷海盐控制台”视觉语言：雾白与浅薄荷构成主要背景，柔和青蓝表示
  在线/可信，杏黄色表示 pending/需处理事务，珊瑚红仅用于错误或破坏性操作；导航使用低饱和
  浅色表面，不以大面积深色制造压迫感，视觉装饰不得掩盖状态含义。
- 正文字体使用本地系统 UI 字体栈，标题以中等字重和紧凑字距建立层级；仅 revision、ID、ASCII
  预览等技术字段使用等宽字体。不得依赖在线字体，正文不得使用过重字形造成阅读疲劳。
- 桌面端使用固定导航轨与宽内容区；窄屏收敛为顶部导航和单列布局。表格在窄屏允许横向滚动，
  不得压缩到字段不可辨认或导致页面整体横向溢出。
- Overview 首屏以摘要卡展示 Node、Provider、Display 和草稿状态；详细标识与 revision 使用等宽
  字体并允许安全换行，空状态必须给出下一步说明，不能只保留空表格或 `undefined`。
- Provider 编辑表单按 Identity、Connection、Schedule、Authentication 分组；类型相关字段动态显示，
  所有输入具有显式 label、辅助说明和可见 focus 状态。
- 已配置 Provider 每行必须同时提供 `Edit`、`Enable/Disable` 与 `Delete`；前两者为普通
  Draft 操作，删除仍是需确认的破坏性操作。表格动作在桌面和窄屏均必须可见、可键盘访问。
- `Edit` 将当前 Draft 的非秘密字段回填至同一表单，进入编辑模式后锁定稳定 ID 和 Type，
  显示当前编辑对象与 `Cancel edit`；保存或取消后回到新增模式。密钥永不回填，空值保留
  已有引用，只有新输入才创建候选轮换。
- `Enable/Disable` 直接更新内存 Draft 并立即显示 pending 差异，不自动 Test、Apply 或重启服务；
  重新启用使已有测试结果失效，用户必须重新只读 Test 后才能 Apply。
- Save/Test/Apply 操作必须有运行中、成功和失败反馈；运行时禁用相关按钮以避免重复提交。
  成功/失败消息使用 `aria-live` 区域展示，不以阻塞式 alert 作为常规反馈。
- Overview 上方常驻运行版本信号条：同一构建且来自同一服务目标时使用低干扰薄荷绿；
  version/commit/build timestamp 不同或 Web Admin 由其他二进制启动时使用高对比杏黄色，
  并同时显示 Admin 与 Service 的脱敏版本。
- Provider Apply 操作区必须说明 Apply 将重启的正式 Service 构建；版本检测不可用时显示中性提示，
  不得猜测成功或阻塞草稿编辑、只读测试。
- Apply 结果以摘要和事务步骤展示，草稿差异以可读变更列表展示；原始 JSON 仅可作为次级诊断信息，
  不作为普通用户的主要界面。
- 破坏性删除继续要求明确确认；按钮、链接和表单控件可由键盘访问，focus 对比清晰。
- 动效仅用于页面切换和状态反馈，遵循 `prefers-reduced-motion`；页面继续保持零 CDN、零外部字体、
  零分析脚本，并与现有 CSP 一致。

## 6. Provider 草稿与只读测试

1. Draft 从最近一次磁盘配置生成；每次写操作带 revision，旧 revision 提交返回冲突。
   revision 由磁盘文件内容摘要生成，Apply 在取得事务互斥锁后重新读取并比较，外部编辑和并发
   Apply 均不得静默覆盖。Web Admin 对同一草稿的读、写、测试和 Apply 请求必须完整串行化。
2. 类型、ID、区域、URL、周期、`stale_after`、凭据类型和 Provider registry 在内存中完整校验。
3. Key 型 Provider 的候选秘密只进入密码字段和本次测试内存 overlay；不写 URL、浏览器存储、日志、临时文件或持久配置。
4. `codex_usage` 只读取选定 `auth_file`，继续拒绝 symlink/非普通文件且不修改登录态。
5. 测试调用与 CLI `provider test` 相同的只读 `Collect`，30 秒超时，输出指标数量、耗时和脱敏错误分类；不输出原始响应。
6. 测试成功不自动 Apply；用户必须检查差异并显式确认。
   测试结果绑定 Provider 的安全配置指纹；任何影响连接、凭据、启用状态或输出的后续编辑都会
   使结果失效。新增或候选秘密变化的启用 Provider 未通过测试时，服务端拒绝 Apply。
7. 删除、停用和可能导致指标消失的变更必须显示影响摘要；Provider 删除不默认保留孤立秘密。

## 7. 配置事务

### 7.1 Apply 前置条件

- 草稿 revision 仍基于当前磁盘配置；否则要求重新加载并解决冲突。
- 全配置通过 `ValidateWithRegistry`，所有新增/轮换凭据的启用 Provider 已通过草稿只读测试。
- 检测重复 Provider ID、无效秘密引用和可预判的稳定 metric ID 冲突；冲突默认阻止 Apply，用户明确解决后才能继续。
- 稳定 metric ID 冲突按连接器实际输出契约（含 mock fixture 声明的完整 ID）比较，不得以
  Provider 配置 ID 代替 metric ID。
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
- 候选秘密无论前端是否显式标记 rotate，只要对应已有 Provider，就必须写入新版本引用，绝不
  覆盖旧配置仍引用的凭据。失败补偿删除本事务已创建的引用；恢复配置使用与正常保存相同的
  临时文件、fsync、rename 和目录 fsync，并在新服务曾启动时再次重启旧配置。

## 8. Display 部署事务

### 8.1 SSH 边界

- 使用操作系统 SSH host key 验证和既有用户配置；不得自动接受未知 host key。
- 后端不提供 shell 字符串输入，只调用预定义操作与固定远端目标。
- Token 和候选环境内容只通过 SSH stdin 传输，不进入本地或远端进程 argv。
- 所有远端路径必须精确匹配 HomePi allowlist；拒绝符号链接、非普通文件、意外 owner/mode 和路径逃逸。
- SSH host 只接受不以 `-` 开头的 DNS/SSH alias 安全字符；远端脚本为编译期固定字符串，浏览器
  不能传入命令、参数、路径或 unit。OpenSSH 继续使用本机既有 `known_hosts` 严格校验，不使用
  `StrictHostKeyChecking=no`、`accept-new` 或密码提示。
- 候选环境只允许 `HOMEPI_NODE_URL`、`HOMEPI_DEVICE_ID`、`HOMEPI_SOURCE_NODE_ID`、
  `HOMEPI_NODE_CERT_PIN`、`HOMEPI_DEVICE_TOKEN`、`HOMEPI_DISPLAY_DATA_DIR` 和
  `HOMEPI_DISPLAY_STYLE`；每个键恰好一次，未知键、重复键、空必填值、换行、NUL、反引号和
  `$(` 均拒绝。

### 8.2 应用顺序

1. 读取并脱敏解析 Pi 当前环境、binary/version、unit 状态和最新快照元数据。
2. 在 Mac 内存构造候选环境；拒绝换行、NUL、shell 展开和未知键。
3. 经 stdin 写入固定目录内的新临时文件，设置 root:root、`0600`。
4. 在 Pi 上执行 `homepi-display config validate` 校验候选文件。
5. 将当前环境原子移动为单份 `.previous`，再原子替换正式环境。
6. 重启 `homepi-display.service`，确认 active、进程未崩溃循环、WebSocket 恢复和快照时间前进。
7. 任一步失败时保留正式旧配置或恢复 `.previous`；恢复后再次验证 active/连接，仍失败则停止并报告人工恢复步骤。

### 8.3 HTTP 与持久状态契约

- `GET /api/display/profile`：返回非秘密期望 profile、Pi 当前脱敏状态、字段差异和
  `token_present`；不返回设备 Token 或完整 `device_token_ref`。
- `PUT /api/display/profile`：只更新服务器内存候选 profile；字段为 `ssh_host`、`device_id`、
  `node_url`、`style` 和 `data_dir`，服务器自动推导 source node、证书指纹与 Token 引用。
- `POST /api/display/test`：只执行字段/凭据解析、SSH 连通性、远端 binary/unit/environment 元数据
  探测和候选 `config validate`；不得替换正式环境或重启 unit。
- `POST /api/display/apply`：要求同一候选已成功 Test，随后执行 8.2 的完整事务。响应只返回
  `status`、脱敏步骤、当前 style、快照 epoch/version/time 和是否发生回滚。
- `POST /api/display/control`：接受固定 `action` DTO，服务端以 profile 的 `device_id` 构造并校验
  Phase 4 `CommandRequest`，成功返回 `202` 与脱敏 command 状态；未知字段、任意命令参数、非法范围、
  未配置连接器和缺少 profile 目标均拒绝。
- `GET /api/display/control/{command_id}`：只返回本机 Web Admin 已发布命令的脱敏状态，用于前端轮询；
  command ID 必须为规范 UUID，不接受 query 参数。
- 成功 Apply 后以原子写入保存 `display-profiles.json`，schema major 为 `1`、mode 为 `0600`；
  Apply 失败不推进 profile。磁盘仅保留单份正式 profile，不保存 Token、环境全文或历史集合。
- `homepi-display config validate --file <固定候选路径>` 解析 systemd EnvironmentFile 格式并复用
  `run` 的 URL、证书指纹、style、ID 与 data dir 校验；命令只输出 `ok` 或脱敏字段错误。

## 9. Web 安全

- Host 只接受实际 loopback listener；Origin 必须同源，禁用 CORS。
- Origin 必须与实际 listener 的 `http://host:port` 精确匹配，不接受其他 loopback host 或端口。
- 会话 Cookie 使用 HttpOnly、SameSite=Strict；所有状态修改需要 CSRF token 和非 GET 方法。
- CSRF token 由同源、`no-store` 的启动响应提供给内存中的前端客户端；静态 HTML/JS、URL、
  localStorage 和日志中不得包含固定 token。
- CSP 至少禁止外部脚本、对象、frame 和任意远端连接；使用 `frame-ancestors 'none'`。
- 设置 `X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer` 和禁止缓存秘密表单响应。
- HTTP middleware 不记录请求体、Authorization、Cookie、查询参数中的敏感值；API 禁止通过 query 传秘密。
- 密钥输入不回填，状态 API 只返回布尔存在性和掩码引用；浏览器刷新后候选秘密必须重新输入。
- 草稿 API 使用专用脱敏 DTO；不得返回完整 `secret_ref` 或候选秘密。
- 对草稿大小、字段长度、JSON 深度、请求速率和并发测试数设置上限。

## 10. 配置兼容性

- `homepi-node` 现有 `config.json` 继续作为 daemon 非秘密配置来源，不为 Web UI 改用第二套 Provider schema。
- Display 部署期望状态存入独立的非秘密 `display-profiles.json`，避免旧 daemon 因未知字段拒绝 node 配置；文件权限 `0600`，只保存 token 引用，不保存 token。
- 新配置文件携带独立 schema version；未知主版本拒绝，新增可选字段按兼容规则处理。
- CLI Provider/Device/Service 命令改为复用 Config Transaction Service；现有脚本行为保持兼容，CLI 不要求浏览器。
- `provider add` 未显式指定引用时继续使用 `keyring:provider-key:<id>`；`provider remove
  -keep-secret` 必须在整个事务中保留被删除 Provider 的引用。`region=custom` + 合法 `base_url`
  对所有现有真实连接器继续兼容。

## 11. 可观测性与恢复

- UI 展示脱敏 Apply 阶段：`validating`、`testing`、`saving`、`restarting`、`verifying`、`rolling_back`、`completed`、`failed`。
- 每次 Runtime Version Probe 最多执行一次受超时约束的直接 `--version` 子进程，不经过 shell；API 仅返回
  `version`、`commit`、`built`、`platform`、安装/运行布尔值、状态码和固定提示，不返回目标路径或原始错误。
- 审计摘要至少含事务 ID、时间、对象 ID、字段名集合、结果和错误分类；不得含旧值、新值、密钥或环境全文。
- `doctor` 增加 Web Admin loopback 配置、最近 Apply 摘要和 Display profile 校验结果，不输出秘密。
- CLI 救援路径必须能够在 Web Admin 不可用时验证配置、恢复 `.previous` Display 环境并查看固定日志。

## 12. 验收标准

- 用户可在本机浏览器添加/测试 Codex、停用 mock 并一次 Apply；无需手工编辑配置或执行服务重启命令。
- 用户可在 Apply 前识别 Web Admin 与正式服务的版本漂移；更新正式二进制并由同一入口重开页面后提示恢复为已同步。
- Key 型 Provider 可测试、轮换和回滚；Keychain、配置、日志、HTTP 响应和 Pi 快照秘密扫描无命中。
- 无效 Provider 字段、错误 Key、配置并发修改、daemon 启动失败都不会破坏上一份有效配置。
- 用户可在 Web Admin 将目标 Pi 从 rich 切换到 ASCII 并切回；每次都自动验证、重启、恢复连接和确认快照。
- 用户可在 Web Admin Display 页面执行固定的切页、前后页、轮播、刷新、消息和亮度操作，并看到
  `published/accepted/executed/rejected/expired/failed` 状态；浏览器网络、静态资源和响应中无控制凭据。
- SSH 断开、候选环境错误、systemd 重启失败时 Pi 保留或恢复上一份环境；不留下历史配置集合或未约束临时文件。
- `lsof`/端口扫描证明 Web Admin 只监听 loopback，Pi 不新增入站端口。
- 当前 macOS + DietPi 实机流程通过；Windows/Linux 仅报告自动化/交叉构建，不伪写实机通过。
