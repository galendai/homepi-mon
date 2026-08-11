# 模块 005 测试文档：本地 Web Admin 与配置编排

> 对应规格：Module-Spec-005-WebAdmin.md
> 所属阶段：Phase 2
> 状态：测试设计已认证，尚未实现/执行

## 1. Unit Test

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| U001 | configure 默认启动参数 | 只选择 loopback 随机端口，不复用 LAN snapshot listener | 未执行 | 待执行 |
| U002 | 显式监听 `0.0.0.0`、LAN IP 或公网 IP | 启动前拒绝，不产生 listener | 未执行 | 待执行 |
| U003 | Provider draft 新增合法 Codex | 应用默认 region/interval/stale_after，保留默认 auth file 语义 | 未执行 | 待执行 |
| U004 | Key 型 Provider 候选秘密执行 draft test | 通过内存 overlay 构造连接器；测试前后 Keychain/config/temp files 不变 | 未执行 | 待执行 |
| U005 | Provider draft interval<5s 或 stale_after<interval | 返回具体字段错误，不执行测试/保存 | 未执行 | 待执行 |
| U006 | custom Base URL 指向元数据地址、跨 host 重定向或非 loopback HTTP | SSRF/URL 校验拒绝 | 未执行 | 待执行 |
| U007 | 两个启用连接器声明相同稳定 metric ID | 标记 shadowed 并阻止 Apply，指出冲突 Provider/metric ID | 未执行 | 待执行 |
| U008 | 旧 draft revision 在配置已变化后 Apply | 返回 revision conflict，不覆盖新配置 | 未执行 | 待执行 |
| U009 | 新增 Key Provider Apply 成功 | 版本化秘密引用、配置、服务和健康按顺序提交，旧无引用秘密清理 | 未执行 | 待执行 |
| U010 | 配置保存失败 | 删除候选秘密，正式配置、服务和旧引用不变 | 未执行 | 待执行 |
| U011 | 新配置保存后 daemon 启动失败 | 恢复上一配置与服务，删除候选秘密，结果为 rolled_back | 未执行 | 待执行 |
| U012 | Apply 请求并发提交两次 | 仅一个事务执行，另一个返回 conflict/busy | 未执行 | 待执行 |
| U013 | Provider Key/设备 Token/请求体进入 logger | 白名单日志过滤，输出无秘密和环境全文 | 未执行 | 待执行 |
| U014 | Display profile 自动推导 | source ID/证书指纹/token ref 来自 node/device 状态，响应不含 token | 未执行 | 待执行 |
| U015 | Display 环境值包含换行、NUL、shell 展开或未知键 | 候选环境生成前拒绝 | 未执行 | 待执行 |
| U016 | SSH target/path/unit 超出 allowlist | 调用前拒绝，不能构造任意 shell 操作 | 未执行 | 待执行 |
| U017 | Pi 候选环境合法 | 固定临时文件 mode=0600/root:root，validate 后原子替换 | 未执行 | 待执行 |
| U018 | Display validate 或重启失败 | 正式旧环境保持不变或恢复 `.previous`，停止重复覆盖 | 未执行 | 待执行 |
| U019 | 状态 API 返回 Provider/Display | 只含脱敏字段、秘密存在性和掩码引用，不含明文/原始响应 | 未执行 | 待执行 |
| U020 | Web Admin 空闲超时/SIGTERM | listener 关闭，daemon serve 和现有 Display 连接不受影响 | 未执行 | 待执行 |

## 2. Web 安全测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| S001 | 非同源 Origin 发起 Provider 修改 | 403，不修改 draft/config/Keychain | 未执行 | 待执行 |
| S002 | 缺失或错误 CSRF token 的 POST/PUT/DELETE | 403；GET 不执行状态修改 | 未执行 | 待执行 |
| S003 | 伪造 Host、DNS rebinding 形式 Host | 请求拒绝，不返回状态 | 未执行 | 待执行 |
| S004 | 检查响应头 | CSP、frame-ancestors none、nosniff、no-referrer、敏感响应 no-store 均存在 | 未执行 | 待执行 |
| S005 | 前端静态资源与浏览器网络记录 | 无 CDN、外部字体、分析脚本和远端 fetch | 未执行 | 待执行 |
| S006 | API Key 放入 query、URL path 或错误字段 | schema 拒绝；访问日志、浏览器历史无 Key | 未执行 | 待执行 |
| S007 | 超大/超深 JSON、超长字段和请求洪泛 | 在大小、深度、长度、速率边界拒绝，进程保持可用 | 未执行 | 待执行 |
| S008 | 恶意 Provider label/SSH host 含 HTML、ANSI、换行 | 前端按文本渲染，后端字段校验拒绝控制字符 | 未执行 | 待执行 |
| S009 | 扫描 config、display profile、日志、HTTP 响应、Pi 环境以外文件和快照 | Provider Key/完整设备 Token 无命中；Pi 环境仅有设备 Token且0600 | 未执行 | 待执行 |
| S010 | Mac/Pi 端口扫描 | Web Admin 只在 Mac loopback；Pi 无新增入站管理端口 | 未执行 | 待执行 |

## 3. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | Mac 浏览器打开 configure | Overview 显示 daemon/config/Provider/Display 脱敏状态，无需编辑文件 | 未执行 | 待执行 |
| E002 | 草稿新增 `codex-main` 并测试 | 30 秒内返回成功和指标数；config/Keychain 尚未改变 | 未执行 | 待执行 |
| E003 | 停用 `phase1-mock` 后 Apply | 自动保存、重启 daemon、确认 codex-main 健康；Pi 新快照不再含 mock-codex | 未执行 | 待执行 |
| E004 | DeepSeek/Kimi/MiniMax 候选 Key 错误 | 测试显示 blocked_auth；当前有效配置和运行数据不变 | 未执行 | 待执行 |
| E005 | 轮换一个真实 Provider Key | 新 Key 测试/应用成功，旧引用删除，期间不向 Pi 发送秘密 | 未执行 | 待执行 |
| E006 | Apply 后 daemon 新进程启动但首次采集失败 | UI 区分进程 running 与 Provider failure，不伪写完整成功；按策略回滚 | 未执行 | 待执行 |
| E007 | Display rich→ascii | SSH 候选校验、原子替换、systemd 重启、WebSocket/快照恢复；实屏为 ASCII | 未执行 | 待执行 |
| E008 | Display ascii→rich | 同一流程恢复 rich，60×20 布局与数据语义不变 | 未执行 | 待执行 |
| E009 | SSH 在候选写入后中断 | 正式环境未替换；Kiosk 继续使用旧配置，无未约束临时文件 | 未执行 | 待执行 |
| E010 | Display 环境替换后 systemd 启动失败 | 自动恢复 `.previous` 并重新启动；UI 显示 rolled_back | 未执行 | 待执行 |
| E011 | Pi 离线时 Provider Apply 成功 | node 标记 applied，Display 标记 pending sync；Pi 恢复后自动确认新快照 | 未执行 | 待执行 |
| E012 | Web Admin 不可用，使用现有 CLI | Provider/config/service 救援路径仍可用且语义一致 | 未执行 | 待执行 |

## 4. 性能与可靠性测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| P001 | Web Admin 空闲打开 1 小时 | 不影响 daemon 采集/Pi 心跳；内存无持续增长，请求日志不膨胀 | 未执行 | 待执行 |
| P002 | 20 个 Provider 草稿、连续 100 次编辑/放弃 | revision 和 diff 正确，无 Keychain/config 临时遗留 | 未执行 | 待执行 |
| P003 | 连续 20 次 rich/ascii 切换含 5 次故障注入 | 每次仅一份正式环境和一份 `.previous`；无崩溃循环或临时文件累积 | 未执行 | 待执行 |

## 5. 手动验收

1. 在当前 macOS 主机执行唯一入口打开 Web Admin，确认监听只在 loopback。
2. 在 Provider 页面测试 `codex-main`，停用 `phase1-mock`，一次 Apply 后在 Pi 实屏看到真实 Codex。
3. 输入一次错误 Key，确认只显示脱敏认证错误且当前配置、Keychain 引用和屏幕不变。
4. 将 Display 从 rich 切为 ASCII，再切回 rich；两次均不手工 SSH 编辑文件或执行 systemctl。
5. 在 Display Apply 中断 SSH，确认 Pi 保持上一配置；恢复 SSH 后可重新应用。
6. 扫描浏览器网络、Mac 配置/日志、Pi 快照和进程 argv，确认没有 Provider Key 或完整设备 Token 泄漏。

## 6. 执行记录

尚未执行。实现阶段必须记录浏览器版本、macOS 版本、`homepi-node`/`homepi-display` commit、
目标 DietPi/systemd 版本、每个故障注入的实际输出和回滚状态。测试生成的候选配置、临时环境、
Mock、浏览器 profile 和测试凭据在执行后清理；不得删除用户原有配置或凭据。
