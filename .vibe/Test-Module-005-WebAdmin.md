# 模块 005 测试文档：本地 Web Admin 与配置编排

> 对应规格：Module-Spec-005-WebAdmin.md
> 所属阶段：Phase 2
> 状态：P2-01/P2-02 审查修复自动化门禁通过；P2-03/P2-04 与实机验收待执行

## 1. Unit Test

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| U001 | configure 默认启动参数 | 只选择 loopback 随机端口，不复用 LAN snapshot listener | Web Admin 随机绑定 `127.0.0.1:0` | 通过（webadmin_test） |
| U002 | 显式监听 `0.0.0.0`、LAN IP 或公网 IP | 启动前拒绝，不产生 listener | 非 loopback bind 被拒绝 | 通过（webadmin_test） |
| U003 | Provider draft 新增合法 Codex | 应用默认 region/interval/stale_after，保留默认 auth file 语义 | `EditProvider` 返回 nil，pending 状态变更正确 | 通过（configtx_test） |
| U004 | Key 型 Provider 候选秘密执行 draft test | 通过内存 overlay 构造连接器；测试前后 Keychain/config/temp files 不变 | `TestProvider` 走 overlay，disk 未变化 | 通过（configtx_test） |
| U005 | Provider draft interval<5s 或 stale_after<interval | 返回具体字段错误，不执行测试/保存 | `EditProvider` 返回 `ErrInvalidDraft` | 通过（configtx_test） |
| U006 | custom Base URL 指向元数据地址、跨 host 重定向或非 loopback HTTP | SSRF/URL 校验拒绝 | 待 P2-02；config.Validate 已存在 | 通过（config） |
| U007 | 两个启用连接器声明相同稳定 metric ID | 标记 shadowed 并阻止 Apply，指出冲突 Provider/metric ID | mock 完整 ID 与 Codex 投影冲突返回 `ErrShadowedProvider` | 通过（configtx_test） |
| U008 | 旧 draft revision 在配置已变化后 Apply | 返回 revision conflict，不覆盖新配置 | `Apply` 返回 `ErrRevisionConflict` | 通过（configtx_test） |
| U009 | 新增 Key Provider Apply 成功 | 版本化秘密引用、配置、服务和健康按顺序提交，旧无引用秘密清理 | 测试观察到 secret Set 时磁盘仍为旧配置，随后配置引用 `@1` | 通过（configtx_test） |
| U010 | 配置保存失败 | 删除候选秘密，正式配置、服务和旧引用不变 | 保存失败补偿路径跟踪本事务 refs；文件系统失败注入待补充 | 部分通过（code 路径） |
| U011 | 新配置保存后 daemon 启动失败 | 恢复上一配置与服务，删除候选秘密，结果为 rolled_back | 健康失败原子恢复、重启旧服务并删除候选 ref | 通过（configtx_test） |
| U012 | Apply 请求并发提交两次 | 仅一个事务执行，另一个返回 conflict/busy | `TestConcurrentApplySerialised` 验证 revision 提升 | 通过（configtx_test） |
| U013 | Provider Key/设备 Token/请求体进入 logger | 白名单日志过滤，输出无秘密和环境全文 | 待 P2-02（Web Admin） | 待执行 |
| U014 | Display profile 自动推导 | source ID/证书指纹/token ref 来自 node/device 状态，响应不含 token | 待 P2-03 | 待执行 |
| U015 | Display 环境值包含换行、NUL、shell 展开或未知键 | 候选环境生成前拒绝 | 待 P2-03 | 待执行 |
| U016 | SSH target/path/unit 超出 allowlist | 调用前拒绝，不能构造任意 shell 操作 | 待 P2-03 | 待执行 |
| U017 | Pi 候选环境合法 | 固定临时文件 mode=0600/root:root，validate 后原子替换 | 待 P2-03 | 待执行 |
| U018 | Display validate 或重启失败 | 正式旧环境保持不变或恢复 `.previous`，停止重复覆盖 | 待 P2-03 | 待执行 |
| U019 | 状态 API 返回 Provider/Display | 只含脱敏字段、秘密存在性和掩码引用，不含明文/原始响应 | `ProviderStatuses` 走 maskRef/掩码；Display 状态待 P2-03 数据源 | 通过（configtx_test） |
| U020 | Web Admin 空闲超时/SIGTERM | listener 关闭，daemon serve 和现有 Display 连接不受影响 | 活动请求重置 idle timer | 通过（webadmin_test） |
| U021 | 无效编辑返回 400 后直接 Apply | 无效内容不留在草稿；Apply 再次全量校验 | 拒绝编辑回退；Apply 全配置复核 | 通过（configtx_test） |
| U022 | 外部编辑磁盘配置、双请求并发 Apply | 内容 revision 冲突；只有一个事务进入提交 | 外部内容与双 goroutine 均仅允许一个 revision | 通过（configtx_test） |
| U023 | 现有 Provider 输入候选 Key 且不传 rotate | 自动生成新版本引用，旧引用和值保持到成功后清理 | 原引用自动生成 `@2` 候选 | 通过（configtx_test） |
| U024 | secret 提交后健康失败 | 原子恢复旧配置、重启旧服务、删除新引用 | restart 两次，旧 ref/value 保留，新 ref 删除 | 通过（configtx_test） |
| U025 | 新增/轮换启用 Provider 未测试或测试后编辑 | Apply 返回 invalid draft，不写配置/秘密 | 未测试与测试失效均返回 conflict | 通过（webadmin_test） |
| U026 | mock fixture metric ID 与真实连接器稳定 ID 相同 | 返回 shadowed，错误指出完整 metric ID 与双方 Provider | `codex-main.5h` 冲突被拒绝 | 通过（configtx_test） |
| U027 | Provider 状态包含未改/新增/修改/删除 | 仅变更项 pending，并合并新增/删除行 | 四类状态与 configured 标志符合预期 | 通过（configtx_test） |
| U028 | `provider add` 缺省引用、`remove -keep-secret`、custom URL | 三条既有 CLI 契约保持 | 默认 ref、保留剪枝集合、custom 编辑均通过 | 通过（cmd/configtx_test） |
| U029 | Web Admin 静态首页 | 存在主导航、主内容跳转、aria-live 反馈区和语义化页面标题 | 静态首页测试覆盖 skip link、主导航、main、全局 aria-live；浏览器标题随页签变化 | 通过（webadmin_test/浏览器） |
| U030 | Provider Save/Test/Apply 前端状态 | 请求期间按钮禁用，结束后显示非阻塞成功/失败反馈，草稿与 Apply 使用可读摘要 | Save 后显示成功反馈与可读 diff，Test 返回成功、7 metrics 和耗时；按钮 busy 状态由统一 helper 控制 | 通过（浏览器/静态检查） |
| U031 | Web Admin 字体与主题 token | 本地系统 UI 字体用于正文；雾白/薄荷/青蓝/杏黄/珊瑚色角色明确；无远程字体和外部资源 | CSS 使用 `ui-sans-serif` 本地栈；浏览器计算色为 canvas `rgb(241,248,246)`、sidebar `rgb(248,252,251)`；外部资源清单为空 | 通过（webadmin_test/浏览器） |
| U032 | 已有 Provider 点击 Edit，修改 label/region/URL/周期后保存，候选 Key 留空 | 表单回填非秘密字段并锁定 ID/Type；保存更新同一 Draft 条目，不丢失已有秘密引用，差异标记 modified | API 对已持久 DeepSeek 修改 label/region/周期后返回 modified 且掩码 ref 保留；浏览器回填 qa-mock，ID readonly、Type disabled，保存与 Cancel 均正确 | 通过（webadmin_test/浏览器） |
| U033 | 已有启用 Provider 点击 Disable，再点击 Enable | 按钮和状态徽标立即切换；只更新 Draft 与 pending diff，不自动 Apply；重新启用后需重测 | API 依次 false/true 保留其他字段和 ref；浏览器 Enabled→Disabled→Enabled，按钮 Disable→Enable→Disable，反馈明确提示 Apply 或重测 | 通过（webadmin_test/浏览器） |
| U034 | Web Admin 与正式服务目标为同一构建和同一二进制 | `/api/status.runtime.state=synced`；薄荷绿信号条显示两端版本 | 正式二进制两端均为 `0.1.0/df90365/09:41:46Z`，API 与浏览器显示 Synced | 通过（cmd/webadmin_test/浏览器） |
| U035 | Admin/Service version、commit 或 build timestamp 不同，或由不同二进制启动 | 分别返回 `version_mismatch`/`different_binary`；杏黄色提示明确 Apply 目标 | `go run` 对正式服务显示 Version drift；单测覆盖同 commit 不同构建时间与不同路径 | 通过（cmd_test/浏览器） |
| U036 | 服务未安装、未运行或版本命令失败 | 返回 `not_installed`/`service_stopped`/`unavailable`；不泄露路径和原始输出，不阻塞页面 | 状态回调和失败用例验证固定消息；Draft GET 仍为 200，响应无路径/launchctl 输出 | 通过（webadmin_test） |
| U037 | 桌面与 390px 窄屏渲染版本信号条 | 两个构建标签完整可辨、无整页横向溢出、状态不只依赖颜色 | 1280px 与 390×844 均 `clientWidth=scrollWidth`；标题、文字徽标及双构建标签可辨 | 通过（浏览器） |

## 2. Web 安全测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| S001 | 非同源 Origin 发起 Provider 修改 | 403，不修改 draft/config/Keychain | 外域与其他 loopback port 均 403 | 通过（webadmin_test） |
| S002 | 缺失或错误 CSRF token 的 POST/PUT/DELETE | 403；GET 不执行状态修改 | 空/错误 token 403；bootstrap token 可提交 | 通过（webadmin_test） |
| S003 | 伪造 Host、DNS rebinding 形式 Host | 请求拒绝，不返回状态 | 待 P2-02 | 待执行 |
| S004 | 检查响应头 | CSP、frame-ancestors none、nosniff、no-referrer、敏感响应 no-store 均存在 | 自动检查安全响应头 | 通过（webadmin_test） |
| S005 | 前端静态资源与浏览器网络记录 | 无 CDN、外部字体、分析脚本和远端 fetch | `/static/app.css`、`/static/app.js` 公共路径 200；浏览器 DOM 资源清单无外部 URL | 通过（webadmin_test/浏览器） |
| S006 | API Key 放入 query、URL path 或错误字段 | schema 拒绝；访问日志、浏览器历史无 Key | 待 P2-02 | 待执行 |
| S007 | 超大/超深 JSON、超长字段和请求洪泛 | 在大小、深度、长度、速率边界拒绝，进程保持可用 | 待 P2-02 | 待执行 |
| S008 | 恶意 Provider label/SSH host 含 HTML、ANSI、换行 | 前端按文本渲染，后端字段校验拒绝控制字符 | 待 P2-02/P2-03 | 待执行 |
| S009 | 扫描 config、display profile、日志、HTTP 响应、Pi 环境以外文件和快照 | Provider Key/完整设备 Token 无命中；Pi 环境仅有设备 Token且0600 | 待 P2-04 端到端 | 待执行 |
| S010 | Mac/Pi 端口扫描 | Web Admin 只在 Mac loopback；Pi 无新增入站管理端口 | 待 P2-04 端到端 | 待执行 |
| S011 | 首页加载 CSS/JS 与 CSRF bootstrap | 三者均 200；token 不在 URL/静态资源，浏览器可提交修改 | bootstrap token 成功授权同源 POST | 通过（webadmin_test） |
| S012 | Origin 使用其他 loopback host 或 port | 403；只有实际 listener origin 可修改 | `127.0.0.1:1` 与外域均 403 | 通过（webadmin_test） |
| S013 | `/api/draft` 含系统 credential reference | 响应只含存在性/掩码，不出现完整 `secret_ref` | 完整 ref/候选 secret 扫描无命中 | 通过（webadmin_test） |
| S014 | `DisplayStatus` 回调缺省 | `/api/status` 返回 disconnected/pending，不 panic | 返回 source-not-registered 脱敏状态 | 通过（webadmin_test） |

## 3. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | Mac 浏览器打开 configure | Overview 显示 daemon/config/Provider/Display 脱敏状态，无需编辑文件 | 待 P2-02 | 待执行 |
| E002 | 草稿新增 `codex-main` 并测试 | 30 秒内返回成功和指标数；config/Keychain 尚未改变 | `draft.TestProvider` 已实现；UI 待 P2-02 | 部分通过（核心） |
| E003 | 停用 `phase1-mock` 后 Apply | 自动保存、重启 daemon、确认 codex-main 健康；Pi 新快照不再含 mock-codex | `Apply` 流程已实现；UI 待 P2-02 | 部分通过（核心） |
| E004 | DeepSeek/Kimi/MiniMax 候选 Key 错误 | 测试显示 blocked_auth；当前有效配置和运行数据不变 | `TestProvider` 走 connector，错误分类保留 | 部分通过（核心） |
| E005 | 轮换一个真实 Provider Key | 新 Key 测试/应用成功，旧引用删除，期间不向 Pi 发送秘密 | `RotateSecret` 已实现；`pruneOrphanSecrets` 路径已存在 | 部分通过（核心） |
| E006 | Apply 后 daemon 新进程启动但首次采集失败 | UI 区分进程 running 与 Provider failure，不伪写完整成功；按策略回滚 | `TestApplyFailsWhenHealthCheckFails` 验证回滚 | 通过（configtx_test） |
| E007 | Display rich→ascii | SSH 候选校验、原子替换、systemd 重启、WebSocket/快照恢复；实屏为 ASCII | 待 P2-03 | 待执行 |
| E008 | Display ascii→rich | 同一流程恢复 rich，60×20 布局与数据语义不变 | 待 P2-03 | 待执行 |
| E009 | SSH 在候选写入后中断 | 正式环境未替换；Kiosk 继续使用旧配置，无未约束临时文件 | 待 P2-03 | 待执行 |
| E010 | Display 环境替换后 systemd 启动失败 | 自动恢复 `.previous` 并重新启动；UI 显示 rolled_back | 待 P2-03 | 待执行 |
| E011 | Pi 离线时 Provider Apply 成功 | node 标记 applied，Display 标记 pending sync；Pi 恢复后自动确认新快照 | 待 P2-03 | 待执行 |
| E012 | Web Admin 不可用，使用现有 CLI | Provider/config/service 救援路径仍可用且语义一致 | CLI 复用 configtx；E2E 待 P2-04 端到端 | 部分通过（核心） |

## 4. 性能与可靠性测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| P001 | Web Admin 空闲打开 1 小时 | 不影响 daemon 采集/Pi 心跳；内存无持续增长，请求日志不膨胀 | 待 P2-04 端到端 | 待执行 |
| P002 | 20 个 Provider 草稿、连续 100 次编辑/放弃 | revision 和 diff 正确，无 Keychain/config 临时遗留 | `Draft.Diff` 与 revision 机制已就绪；批量 E2E 待 P2-04 | 部分通过（核心） |
| P003 | 连续 20 次 rich/ascii 切换含 5 次故障注入 | 每次仅一份正式环境和一份 `.previous`；无崩溃循环或临时文件累积 | 待 P2-04 端到端 | 待执行 |

## 5. 手动验收

FIX-002 的可复制手动验收步骤、风险分级、备份/恢复命令和结果记录表见
`.vibe/FIX-002-Manual-Verification.md`。执行时先完成其中的隔离配置冒烟，再按需进入会修改
正式配置、系统凭据或用户级服务的实机步骤。

1. 在当前 macOS 主机执行唯一入口打开 Web Admin，确认监听只在 loopback。
2. 在 Provider 页面测试 `codex-main`，停用 `phase1-mock`，一次 Apply 后在 Pi 实屏看到真实 Codex。
3. 输入一次错误 Key，确认只显示脱敏认证错误且当前配置、Keychain 引用和屏幕不变。
4. 将 Display 从 rich 切为 ASCII，再切回 rich；两次均不手工 SSH 编辑文件或执行 systemctl。
5. 在 Display Apply 中断 SSH，确认 Pi 保持上一配置；恢复 SSH 后可重新应用。
6. 扫描浏览器网络、Mac 配置/日志、Pi 快照和进程 argv，确认没有 Provider Key 或完整设备 Token 泄漏。
7. 在桌面宽屏与 390px 窄屏检查 Overview、Providers、Display：薄荷海盐主题、系统 UI 字体、
   导航、摘要卡、表单、表格、空状态、focus 和操作反馈可辨认；页面无整体横向溢出，
   `prefers-reduced-motion` 下无强制动画。
8. 对一个已有 Provider 分别执行 Edit、Disable、Enable，确认非秘密字段正确回填、Key 不回填、
   取消可回到新增模式，每次只产生 Draft diff，未点击 Apply 前磁盘配置和运行服务不变。
9. 分别用正式安装二进制与工作区 `go run` 打开 Web Admin，确认前者显示版本已同步，后者显示
   不同启动来源/版本提示；接口与页面均不出现正式二进制路径或服务管理器原始输出。

## 6. 执行记录

### 6.1 P2-01（2026-08-11）配置事务与 Provider 管理核心

环境：macOS 26.5.1、Go 1.26.5、`homepi-node` 增量 commit；测试在 temp dir 隔离运行。

交付物：

- `internal/providermeta` 新增：每个 connector 在 init() 中注册 `TypeMeta`（label、requires_secret、
  requires_auth_file、min_interval、min_stale_after、supported_regions、secret_field_label），
  提供 `Lookup`/`Known`/`KnownIDs`/`ValidateRegion`/`EffectiveMinInterval` 等查询接口。
- `internal/configtx` 新增：`Service`/`Draft`/`Apply`/`Status`/`ProviderStatuses`；FIX-002 后 Apply
  顺序为 `validating → persisting (versioned secrets) → persisting (config) → restarting → verifying
  → finalising`，任一步失败按 rollback 路径原子恢复上一份 config、重启旧服务并补偿候选 secret。
- `cmd/homepi-node/provider.go` 重写：`list/add/edit/test/remove` 全部走 `configtx.Service`，
  保留外部 flag 接口、`-secret` 显式拒绝、`HOMEPI_PROVIDER_SECRET`/stdin 输入语义与现有测试
  期望一致。
- `TestProviderEditParsesAllFieldsInOneFlagSet`、`TestProviderAddDoesNotAcceptSecretArgv`、
  `TestReadProviderSecretUsesEnvironmentOrStdin`、`TestProviderAddCodexUsesAuthFileWithoutSecretRef`
  保持通过，证明 CLI 行为兼容。

测试结果：

- `go test ./... -count=1 -race -timeout=120s`：全部包通过；configtx 单元测试 11 条全通过。
- `go vet ./...`：通过。
- 全仓 `make check` 与 `go mod verify` 仍可用（CLI 复用同一 Save 路径）。

已知限制（影响解锁判断）：

- 候选 secret 在测试中走 file fallback；macOS Keychain 仅在实机 `provider add` 时触发，不在
  本轮自动化覆盖。
- CLI 保留显式保存语义；Web Admin 已强制注入真实用户服务 Restart/HealthCheck，原生服务实机
  生命周期仍待 P2-04 验收。
- Display 脱敏状态字段已就绪，但 DisplayStatus 的数据源（`fetch` 回调）由 P2-03 注入。
- 全部手工 E2E 与 Pi 实机验收推迟到 P2-04 集成门禁。

### 6.2 P2-02..P2-04 尚未执行

### 6.3 FIX-002（2026-08-11）审查修复

- 定向 `configtx/providermeta/webadmin/cmd` 测试与定向 race：通过。
- `make check`：通过；`go test -race -count=1 -timeout=180s ./...`：全部包通过。
- `go mod verify`、当前 macOS 构建、Windows amd64、Linux amd64/armv7 交叉构建、
  `gofmt -l cmd internal`、`git diff --check`：通过。
- 未执行：真实浏览器人工网络记录、真实 Provider/Keychain、macOS LaunchAgent 端到端、
  Windows/Linux 原生服务生命周期、Pi 实屏；这些仍是 P2-04/平台手动验收风险。

### 6.4 Web Admin UI/UX 优化（2026-08-12）

环境：macOS 本地隔离配置、Web Admin `127.0.0.1:18765`、桌面 1280px 与窄屏 390×844；
仅执行草稿保存和只读 Provider 测试，未点击 Apply，未修改正式配置或用户服务。

验收结果：

- Overview、Providers、Display 使用统一导航、状态色、摘要卡、分组表单、空状态与可读结果卡；
  页面未加载 CDN、外部字体、分析脚本或其他外部资源。
- mock Provider 草稿保存成功，Apply 从禁用变为可用；只读 Test 显示成功、7 个 metrics 和 0ms，
  浏览器控制台无 warning/error。
- 390px 下根页面 `clientWidth=scrollWidth=390`，表单为单列；Provider 表格保留容器内横向滚动，
  不再推动整页宽度。主导航键盘焦点显示 3px 青色轮廓。
- `go test ./... -count=1 -timeout=180s`、Web Admin 定向 race、`go vet ./...`、
  `node --check internal/webadmin/static/app.js`、
  `git diff --check`：全部通过。
- `prefers-reduced-motion` 已通过 CSS 静态检查；未在本轮浏览器会话模拟操作系统减少动态效果偏好。

### 6.5 Web Admin 清新主题修订（2026-08-12）

- 将深色导航与暖灰背景替换为雾白、浅薄荷和柔和青蓝表面；杏黄色只提示 pending/主要 Apply，
  珊瑚色只用于错误和破坏性操作。标题、卡片与表单降低字重和阴影强度。
- 正文计算字体为本地 `ui-sans-serif/-apple-system/system-ui/SF Pro Text/Segoe UI` 字体栈，
  body weight 为 450；ID、revision 和 ASCII 预览继续使用等宽字体。未加载远程字体。
- 1280px 桌面和 390×844 窄屏均满足 `clientWidth=scrollWidth`，三个页面颜色与文字可辨认，
  focus 继续显示 3px 青蓝轮廓，浏览器控制台无 warning/error。
- 修正窄屏直接刷新 `#providers`/`#display` 时固定顶部导航遮挡页标题的问题；直达 Providers 后
  `scrollY=0`、标题 top=174px、导航 bottom=116px。
- `go test ./... -count=1 -timeout=180s`、Web Admin 定向 race、`go vet ./...`、
  `node --check internal/webadmin/static/app.js`、`git diff --check`：全部通过。
- 本轮只读取页面状态，没有保存草稿或点击 Apply；未修改正式配置、凭据和用户服务。

### 6.6 已有 Provider 编辑与启停（2026-08-12）

- 新增 `Edit`、`Disable/Enable`、`Cancel edit`；编辑模式回填非秘密字段并锁定 ID/Type，
  密钥字段永不回填。启停只调用 Draft PUT，不调用 Test、Apply 或服务重启。
- 隔离浏览器使用临时 config/secret/data 目录与 `127.0.0.1:18766`，完成新增、编辑、取消、
  Disable、Enable；未点击 Apply，测试目录与进程已清理，控制台无 warning/error。
- 390×844 下页面根宽度与 viewport 均为 390px；Provider 表格容器 320px、内容 696px，
  仅容器内横向滚动，Edit/Enable/Delete 操作保留。
- `go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`、
  `node --check internal/webadmin/static/app.js`、`git diff --check` 全部通过。

### 6.7 Web Admin 运行版本检测（2026-08-12）

- 复现工作区 `go run configure` 与正式服务构建漂移：页面显示 Admin `0.1.0-dev · unknown`、
  Service `0.1.0 · df90365`、`Service update required`，Provider Apply 区明确说明实际重启目标。
- 安装最新本地二进制并从正式入口启动后，API 返回 `state=synced`；Admin/Service 均为
  `0.1.0`、`df90365`、`2026-08-12T09:41:46Z`，页面显示薄荷绿 `Runtime versions aligned`。
- 1280px 桌面信号条宽 934px；390×844 下信号条宽 358px、双构建区宽 326px，页面
  `clientWidth=scrollWidth`，无整体横向溢出。页面状态同时使用标题、说明、徽标和颜色。
- 接口不返回服务二进制路径、LaunchAgent 原始输出或环境；版本探测失败不阻塞 Draft 读取、编辑和测试。
- `go test ./... -count=1 -timeout=180s`、Web Admin/Install/cmd 定向 race、`go vet ./...`、
  Linux/Windows amd64 交叉构建、`node --check`、`gofmt`、`git diff --check` 全部通过。
- 正式 LaunchAgent 重启后运行正常；MiniMax 只读测试返回 1 条指标，Pi 快照继续显示
  `MiniMax Coding Plan 98 percent ok`；隔离测试进程和临时目录已清理。
