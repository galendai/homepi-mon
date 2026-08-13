# 模块 005 测试文档：本地 Web Admin 与配置编排

> 对应规格：Module-Spec-005-WebAdmin.md
> 所属阶段：Phase 2
> 状态：P2-01..P2-04 自动化与 macOS + DietPi 事务验收通过；长期/物理屏补充观察见 6.8

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
| U013 | Provider Key/设备 Token/请求体进入 logger | 白名单日志过滤，输出无秘密和环境全文 | Keychain 实值对配置/profile/日志/仓库 0 命中；Pi 快照/日志 Token 0 命中 | 通过（实机扫描） |
| U014 | Display profile 自动推导 | source ID/证书指纹/token ref 来自 node/device 状态，响应不含 token/ref | Manager 从 config/cert/Keychain 推导；profile 仅保存 ref，API DTO 不含 ref/Token | 通过（displaydeploy_test/实机） |
| U015 | Display 环境含换行、NUL、Shell 展开、重复或未知键 | 候选环境生成/解析前拒绝 | 共享 parser 对上述输入全部拒绝，合法 7 键 round-trip | 通过（displayconfig_test） |
| U016 | SSH host 含 option/control/shell/path 字符，或请求修改 path/unit | 调用前拒绝；API 不接受远端 path/unit/command | option/shell/path host 被拒；HTTP DTO 无 path/unit/command 字段 | 通过（displaydeploy/webadmin_test） |
| U017 | Pi 候选环境合法 | 先只读 validate；Apply 固定临时文件 mode=0600/root:root，validate 后原子替换 | rich/ascii 两次候选 validate、原子替换和 restart 成功；文件元数据符合 | 通过（自动化/实机） |
| U018 | Display validate、SSH、重启或快照确认失败 | 正式旧环境保持不变或恢复 `.previous`；结果标记 rolled_back/manual recovery | 快照不前进触发 rollback；SSH 不可达在替换前失败；无候选残留 | 通过（displaydeploy_test/实机） |
| U019 | 状态 API 返回 Provider/Display | 只含脱敏字段、秘密存在性和掩码引用，不含明文/原始响应 | Provider 使用 maskRef；Display 返回 style/version/URL hash，不返回 Token/ref | 通过（configtx/displaydeploy/实机） |
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
| U038 | Display 固定操作映射为 Phase 4 命令 | 合法操作生成严格 `CommandRequest`，目标来自 profile，返回 command ID/sequence/状态 | 七类操作（show/next/previous/rotation/refresh/message/brightness）均生成对应 kind；fake controller 收到 `pi-kiosk`、sequence=7 和脱敏 published 状态 | 通过（webadmin_test） |
| U039 | Kiosk 操作未知字段/越界/控制字符/未配置 connector | 返回 400，daemon 不收到命令，消息全文不进入响应或日志 | strict JSON 拒绝 `shell` 字段；协议校验拒绝换行消息/越界参数；daemon 继续负责 connector 白名单 | 通过（webadmin_test/protocol） |
| U040 | Kiosk 命令状态轮询 | `GET /api/display/control/{id}` 返回脱敏中间/最终状态；非法 ID/query 被拒绝 | canonical UUID 返回 executed/code=ok；非法 ID 返回 400；query 由通用 API middleware 拒绝 | 通过（webadmin_test） |
| U041 | 无 Kiosk controller 或独立控制凭据不可用 | 页面显示服务不可用/脱敏错误，不暴露 secret ref/token | 缺省 controller 返回 503；适配器只读取独立 credential，错误为固定脱敏消息 | 通过（webadmin/api contract） |

## 2. Web 安全测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| S001 | 非同源 Origin 发起 Provider 修改 | 403，不修改 draft/config/Keychain | 外域与其他 loopback port 均 403 | 通过（webadmin_test） |
| S002 | 缺失或错误 CSRF token 的 POST/PUT/DELETE | 403；GET 不执行状态修改 | 空/错误 token 403；bootstrap token 可提交 | 通过（webadmin_test） |
| S003 | 伪造 Host、DNS rebinding 形式 Host | 请求拒绝，不返回状态 | 伪造 Host 返回 421 | 通过（webadmin_test） |
| S004 | 检查响应头 | CSP、frame-ancestors none、nosniff、no-referrer、敏感响应 no-store 均存在 | 自动检查安全响应头 | 通过（webadmin_test） |
| S005 | 前端静态资源与浏览器网络记录 | 无 CDN、外部字体、分析脚本和远端 fetch | `/static/app.css`、`/static/app.js` 公共路径 200；浏览器 DOM 资源清单无外部 URL | 通过（webadmin_test/浏览器） |
| S006 | API Key 放入 query、URL path 或错误字段 | schema 拒绝；访问日志、浏览器历史无 Key | API 任意 query 返回 400且不回显值；未知字段返回 400 | 通过（webadmin_test） |
| S007 | 超大/超深 JSON、超长字段和请求洪泛 | 在大小、深度、长度、速率边界拒绝，进程保持可用 | 64KiB cap、depth=16、strict DTO、120 mutation/min；边界测试通过 | 通过（webadmin_test） |
| S008 | 恶意 Provider label/SSH host 含 HTML、ANSI、换行 | 前端按文本渲染，后端字段校验拒绝控制字符 | 前端只用 textContent；SSH option/shell/control 输入被后端拒绝 | 通过（静态/displaydeploy_test） |
| S009 | 扫描 config、display profile、日志、HTTP 响应、Pi 环境以外文件和快照 | Provider Key/完整设备 Token 无命中；Pi 环境仅有设备 Token且0600 | 本机与 Pi 非预期位置命中均为 0；Pi environment root:root 0600 | 通过（实机扫描） |
| S010 | Mac/Pi 端口扫描 | Web Admin 只在 Mac loopback；Pi 无新增入站管理端口 | 验收后 8765 listener=0；Pi TCP listener 仅 22 | 通过（实机） |
| S011 | 首页加载 CSS/JS 与 CSRF bootstrap | 三者均 200；token 不在 URL/静态资源，浏览器可提交修改 | bootstrap token 成功授权同源 POST | 通过（webadmin_test） |
| S012 | Origin 使用其他 loopback host 或 port | 403；只有实际 listener origin 可修改 | `127.0.0.1:1` 与外域均 403 | 通过（webadmin_test） |
| S013 | `/api/draft` 含系统 credential reference | 响应只含存在性/掩码，不出现完整 `secret_ref` | 完整 ref/候选 secret 扫描无命中 | 通过（webadmin_test） |
| S014 | `DisplayStatus` 回调缺省 | `/api/status` 返回 disconnected/pending，不 panic | 返回 source-not-registered 脱敏状态 | 通过（webadmin_test） |
| S015 | 浏览器请求 Kiosk 控制接口缺少/伪造 Origin 或 CSRF | 403，不发布命令 | 缺失 CSRF 返回 403；通用 Origin/Host/Query 中间件继续在控制路由前拒绝 | 通过（webadmin_test） |
| S016 | 扫描 Kiosk API、静态资源、日志和浏览器存储 | 无独立控制凭据、设备 Token 或消息全文泄漏 | DTO/响应仅含 command 元数据；前端无 token/localStorage；静态资源无外部请求 | 通过（静态审计/浏览器） |

## 3. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | Mac 浏览器打开 configure | Overview 显示 daemon/config/Provider/Display 脱敏状态，无需编辑文件 | 正式构建 Synced，5 Provider、Pi style/epoch/version 正常展示 | 通过（浏览器/实机） |
| E002 | 草稿新增 `codex-main` 并测试 | 30 秒内返回成功和指标数；config/Keychain 尚未改变 | `draft.TestProvider` 已实现；UI 待 P2-02 | 部分通过（核心） |
| E003 | 停用 `phase1-mock` 后 Apply | 自动保存、重启 daemon、确认 codex-main 健康；Pi 新快照不再含 mock-codex | `Apply` 流程已实现；UI 待 P2-02 | 部分通过（核心） |
| E004 | DeepSeek/Kimi/MiniMax 候选 Key 错误 | 测试显示 blocked_auth；当前有效配置和运行数据不变 | `TestProvider` 走 connector，错误分类保留 | 部分通过（核心） |
| E005 | 轮换一个真实 Provider Key | 新 Key 测试/应用成功，旧引用删除，期间不向 Pi 发送秘密 | `RotateSecret` 已实现；`pruneOrphanSecrets` 路径已存在 | 部分通过（核心） |
| E006 | Apply 后 daemon 新进程启动但首次采集失败 | UI 区分进程 running 与 Provider failure，不伪写完整成功；按策略回滚 | `TestApplyFailsWhenHealthCheckFails` 验证回滚 | 通过（configtx_test） |
| E007 | Display rich→ascii | SSH 候选校验、原子替换、systemd 重启、WebSocket/快照恢复；实屏为 ASCII | Web UI Apply 成功，style=ascii，snapshot 7→9，unit active/0 restarts | 通过（实机；LCD观感待用户看屏） |
| E008 | Display ascii→rich | 同一流程恢复 rich，60×20 布局与数据语义不变 | Web UI Apply 成功，style=rich，snapshot 9→15，最终保持 rich | 通过（实机） |
| E009 | SSH 在候选写入后中断 | 正式环境未替换；Kiosk 继续使用旧配置，无未约束临时文件 | 不可解析 SSH host 在 Test 阶段失败，Apply 禁用，candidate=0 | 通过（实机前置失败） |
| E010 | Display 环境替换后 systemd 启动失败 | 自动恢复 `.previous` 并重新启动；UI 显示 rolled_back | 不可达 node URL 导致快照不前进，25秒后恢复 rich；active/0 restarts | 通过（健康失败回滚） |
| E011 | Pi 离线时 Provider Apply 成功 | node 标记 applied，Display 标记 pending sync；Pi 恢复后自动确认新快照 | 停止 Pi unit 后 Apply healthy=true/display_sync=pending；恢复后获得新 epoch，最终 active | 通过（实机） |
| E012 | Web Admin 不可用，使用现有 CLI | Provider/config/service 救援路径仍可用且语义一致 | config validate/status 与 4 个真实 provider test 成功；Pi validate/status 成功 | 通过（实机 CLI） |
| E013 | 浏览器在 Display 页面执行切页、消息、刷新和亮度 | 通过本机控制通道发布命令，页面轮询并展示最终状态；不新增 Pi 端口 | 自动化 UI/响应检查完成；真实 daemon + DietPi 命令执行待用户在现有实机上验收 | 待用户验收 |

## 4. 性能与可靠性测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| P001 | Web Admin 空闲打开 1 小时 | 不影响 daemon 采集/Pi 心跳；内存无持续增长，请求日志不膨胀 | 加速 idle reset/close 自动化通过；本轮实机会话约10分钟，1小时字面观察保留 | 部分通过（长期补充） |
| P002 | 20 个 Provider 草稿、连续 100 次编辑/放弃 | revision 和 diff 正确，无 Keychain/config 临时遗留 | `Draft.Diff` 与 revision 机制已就绪；批量 E2E 待 P2-04 | 部分通过（核心） |
| P003 | 连续 20 次 rich/ascii 切换含 5 次故障注入 | 每次仅一份正式环境和一份 `.previous`；无崩溃循环或临时文件累积 | 2 次实机切换+2类故障、自动化回滚压力通过；20次字面实机压力保留 | 部分通过（长期补充） |

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
10. 在 Display 页面用固定 profile 目标执行 Show page、Next、Refresh、Message；确认每次均出现
    command ID/最终状态，输入控制字符或未知字段被拒绝；暂不把亮度 unsupported 视为成功。

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
- CLI 保留显式保存语义；Web Admin 已完成正式 macOS LaunchAgent Restart/HealthCheck 往返；
  Windows/Linux 原生服务生命周期按产品所有者决定继续暂缓。
- Display 脱敏状态已由真实固定 SSH probe 注入，并完成 rich/ascii、离线 pending sync 与回滚验收。
- 物理 LCD 摄像头观感和长时间压力作为补充观察，见 6.8，不伪写为已执行。

### 6.2 P2-02..P2-04 历史待执行记录（已由 6.4..6.8 覆盖）

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

### 6.8 Phase 2 完成验收（2026-08-12）

环境：macOS 正式 LaunchAgent、Keychain、`127.0.0.1:8765` 临时 Web Admin、`ssh dietpi`
root 管理通道、DietPi linux/arm64 `homepi-display.service`。

- Provider：4 个启用真实 Provider 的 CLI 只读测试均成功；浏览器完成 MiniMax
  Enabled→Disabled→Apply→Enabled→Test→Apply，最终 5 个配置/4 个启用，revision 恢复为
  `a9e24091dacd`，两次正式服务健康检查通过。
- Display：浏览器完成 rich→ascii→rich，快照分别从 7→9 和 9→15；最终环境 rich、unit active、
  `NRestarts=0`。不可达 node URL 在替换后因快照不前进触发回滚；不可解析 SSH host 在替换前失败。
- 安全：旧服务实例 CSRF token 在服务重启后返回 403；Host spoof/query/strict JSON/depth/body/rate
  自动化通过；Keychain 实值、profile、日志、HTTP DTO、Pi 快照的非预期秘密扫描均为 0。
- 端口：Web Admin 结束后 8765 无 listener；Pi 仅监听 TCP 22，无新增入站管理端口。
- 门禁：`make check`、configtx/displayconfig/displaydeploy/webadmin race、前端语法、diff check、
  darwin amd64/arm64、windows amd64、linux amd64/arm64/armv7 共 12 个产物与 checksum 全通过。
- 未伪写：Windows/Linux 原生服务生命周期未执行；无摄像头确认物理 LCD 观感；P001 的 1 小时与
  P003 的 20 次字面压力作为补充长期观察，不影响本次 Phase 2 代码/事务门禁。

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

### 6.9 P4-04 Web Admin Kiosk 控制入口（2026-08-13）

自动化环境：临时配置目录、loopback Web Admin `127.0.0.1:18767`、无设备 profile；未触碰正式配置、Keychain、daemon 或 Pi。

- 后端增加 `POST /api/display/control` 与 `GET /api/display/control/{command_id}`；七类允许操作均映射到 Phase 4 协议并通过严格校验。
- `webadmin_test` 覆盖目标设备来自 profile、command ID/sequence/脱敏状态、状态轮询、未知字段、控制字符、缺失 CSRF 和非法 UUID；全部通过。
- 控制适配器复用 CLI 已验证的独立 secret、TLS fingerprint pinning 和本机控制 HTTP 路由；浏览器 DTO/响应不包含 token、secret ref、params 或消息全文。
- Display 页面新增 Operate Kiosk 面板，沿用薄荷海盐 token、本地系统字体、自包含静态资源和非阻塞 aria-live 反馈；桌面截图检查通过，390px 下 `clientWidth=scrollWidth=390`、Kiosk panel width=358px，未出现整页横向溢出。
- `go test ./... -count=1 -timeout=180s`、Web Admin/displaydeploy/cmd race、`go vet ./...`、`node --check internal/webadmin/static/app.js` 和 `git diff --check`：通过。
- 使用独立 `HOMEPI_NODE_DATA_DIR`、`HOMEPI_DATA_DIR`、secret 目录启动临时 TLS daemon 与 Web Admin，真实链路返回 `POST /api/display/control=202`、`sequence=1`、`status=published`，随后 `GET` 返回 `200/published`；隔离 daemon 没有 Kiosk WebSocket，因此未伪写 `executed`。

尚未执行：真实 daemon + DietPi 的浏览器点击/命令生命周期、亮度硬件能力和用户肉眼确认；保留为 E013 用户验收项，不伪写为通过。
