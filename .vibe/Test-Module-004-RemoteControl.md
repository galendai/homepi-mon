# 模块 004 测试文档：远程控制

> 对应规格：Module-Spec-004-RemoteControl.md  
> 所属阶段：Phase 4
> 状态：自动化与 Mac→DietPi 实机门禁通过，用户肉眼验收待确认

## 1. Unit Test

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| U001 | 合法 show_page(page_id=api)，未过期 | Accepted 后 Executed，当前页为 api | `Controller` 与完整 stream E2E 返回 accepted/executed，Router=API | 通过 |
| U002 | 未知 kind=`exec_shell` | rejected_unknown_command，不执行任何动作 | 严格协议测试返回 `unknown_command` | 通过 |
| U003 | device_id 与当前设备不一致 | rejected_wrong_target | Controller 返回 `rejected/wrong_target` | 通过 |
| U004 | expires_at 早于当前时间 | expired，不改变 UI | 协议与队列均记录 `expired`，未进入 Display 动作 | 通过 |
| U005 | 同一 command_id 重放 10 次 | 只执行一次，其余返回原结果 | 进程内与重建 Controller 后均返回原 `executed` | 通过 |
| U006 | next_page 参数带多余 shell 字符串 | schema 校验拒绝，不解释为命令 | 未知参数和 `exec_shell` 均在 schema 层拒绝 | 通过 |
| U007 | show_message 含 ESC、BEL、超长文本 | 控制字符清理、长度限制或拒绝 | ESC/控制字符/超过 116 字节均拒绝 | 通过 |
| U008 | set_brightness(level=150) | rejected_invalid_param | 返回 `invalid_param` | 通过 |
| U009 | set_brightness 合法但硬件不支持 | failed_unsupported_capability | 自动化及实机 sequence=4 均返回该失败码 | 通过 |
| U010 | sequence 小于已执行 sequence | rejected_out_of_order | 持久高水位拒绝旧 sequence | 通过 |
| U011 | emergency 抢占 normal 页面 | 显示 emergency；结束后恢复抢占前页面 | Router 回归通过；实机真实 CRIT 压过远程覆盖 | 通过 |
| U012 | 自动轮播即将切页时收到普通远程 show_page | 远程目标页按命令显示；命令展示期结束后恢复此前轮播位置 | 冻结并恢复原页面与剩余 dwell 的确定性时钟测试通过 | 通过 |
| U013 | 队列超过上限 | 丢弃最旧普通命令，保留最新状态，返回 overflow 诊断 | 64 条满载后 high 命令保留，最旧 published normal=`failed/overflow` | 通过 |
| U014 | 控制 API 使用设备 Token、非本机来源或浏览器 Origin | 全部 403/401；只有独立本机控制凭据可发布 | 单测覆盖 401/403；Pi 实机访问控制路由为 403 | 通过 |
| U015 | 命令含未知字段/参数、future>30s、TTL>5m 或非规范 UUID | 严格拒绝，稳定错误码，不进入队列 | 含极大整数溢出样本在内均拒绝 | 通过 |
| U016 | daemon 重启后加载未过期命令和 sequence | 只恢复未完成命令；新 sequence 大于重启前高水位 | 单测恢复 pending；实机 node 重启后 sequence 6→7→9 | 通过 |
| U017 | Pi 幂等文件 mode/条数/损坏输入 | `0600`、最多 256 条；损坏文件隔离且 Kiosk 可启动 | 257 条后仅保留 256；损坏隔离；实机文件 `0600` | 通过 |
| U018 | show_page/next/previous 覆盖到期 | 恢复原页面、原轮播位置和剩余 dwell | 三种动作及到期恢复测试通过 | 通过 |
| U019 | set_rotation 临时开关/interval 到期 | 覆盖期按新值工作；到期恢复持久配置 | 开关、10 秒 interval 与到期恢复测试通过 | 通过 |
| U020 | refresh_data 指定/全部连接器 | 复用 scheduler 并发/timeout；新快照后 executed；未知 ID 拒绝 | scheduler/stream 闭环测试通过；实机 deepseek-main 刷新后 executed | 通过 |
| U021 | 审计 show_message 和秘密扫描 | 只记录长度/hash；无全文、Provider Key、设备/控制 Token | 单测及两端实机日志/状态文件扫描通过 | 通过 |
| U022 | remote notice normal/超长/恶意文本，ASCII/rich | 三行覆盖，双主题严格 60×20，只有允许 SGR/字符 | ASCII/rich 恶意文本网格回归均为 60×20 | 通过 |

## 2. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | 正常 WebSocket，发送 show_page | 1 秒内切页并收到最终 ACK | 自动化完整链路通过；实机最终签发→执行约 0.16 秒 | 通过 |
| E002 | 断网时发送 30 秒有效命令，60 秒后重连 | 旧命令不执行并报告 expired/不再下发 | 实机用 5 秒 TTL/6 秒离线等价验证，结果 `expired` | 通过 |
| E003 | 断网期间 Phase 1 保持 overview，Phase 3 开启自动轮播 | 不等待远程通道；Phase 1 页面不变，Phase 3 按本地配置继续轮播 | 既有 Phase 3 本地轮播回归通过；Phase 4 离线不阻塞 Kiosk | 通过 |
| E004 | 设备 A/B 在线，命令目标 A | 仅 A 执行，B 无状态变化 | 自动化验证目标过滤和错设备结果拒绝；未配置第二台实机 Pi | 通过（自动化） |
| E005 | 撤销设备 Token 后保持旧连接/重连 | 会话按策略终止或拒绝，不能继续接收命令 | 既有 Node API revocation E2E 随全仓门禁通过 | 通过（自动化） |
| E006 | 重启 Pi 后重放已执行 command_id | 从持久幂等记录识别，不再次执行 | Controller 重建后返回原结果；实机重启后高水位 7→9 | 通过 |
| E007 | 远程 show_message 到期 | 自动恢复原页面，消息不残留 | 确定性时钟到期测试通过 | 通过 |
| E008 | 对 Pi 进行端口扫描 | 无本模块新增的公网入站监听 | 实机只监听 Dropbear TCP 22 | 通过 |
| E009 | 同一 LAN 中未配对 node 尝试发送命令 | Pi 拒绝命令与连接，不改变页面；审计记录来源错误码 | source binding/认证回归通过；Pi→node 控制接口实测 403 | 通过 |
| E010 | 已绑定 node=A，另一个已知但未绑定 node=B 发送合法命令 | B 的命令被拒绝，只有 A 能控制 Phase 1 UI | source binding、设备 Token 和目标三层自动化回归通过 | 通过（自动化） |
| E011 | `homepi-node remote show-page API` 到 Pi 实屏 | 1 秒内可见 API，CLI 收到 accepted/executed | CLI 0.17 秒 executed；实屏真实 CODING CRIT 按规格压过 API，解除 CRIT 后肉眼项待用户确认 | 部分通过 |
| E012 | 消息覆盖期间注入/解除 CRIT | CRIT 立即抢占；解除后按消息剩余时间或原轮播位置恢复 | 自动化恢复通过；实机真实 CRIT 持续压过已 executed 消息 | 通过 |
| E013 | Display 离线发布命令后在 TTL 内/外重连 | TTL 内执行；TTL 外 expired 且不下发 | 实机 TTL 外为 `expired`；TTL 内由自动化 stream E2E 覆盖 | 通过 |
| E014 | 执行命令后重启 Display 并重放同 ID 十次 | 原结果返回十次，UI 不重复变化 | 自动化 exact-ID 重放十次和重启重放通过；实机账本重启恢复通过 | 通过 |
| E015 | daemon accepted refresh 后指定 connector 采集 | 受限即时采集，Pi 收到新快照并最终 executed | 自动化新快照闭环及实机 `deepseek-main` 刷新均 executed | 通过 |

## 3. 安全测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| S001 | 构造 JSON 类型混淆、超深嵌套和超大消息 | 在大小/深度/schema 边界拒绝，进程不崩溃 | 8 KiB 上限、strict JSON、未知字段与极大整数样本均拒绝 | 通过 |
| S002 | 伪造设备身份、目标和时间戳 | 全部拒绝，审计记录稳定错误码 | 认证、wrong_target、future、expired 回归通过 | 通过 |
| S003 | 扫描审计日志 | 可关联 command_id 与结果，不含 Provider Key/完整敏感消息 | Mac/Pi 日志和两端状态文件实机扫描通过 | 通过 |
| S004 | 使用 Pi 设备 Token 调用控制路由 | 拒绝；设备 Token 不能发布命令 | 设备 Token 返回 401；独立控制凭据成功 | 通过 |
| S005 | 检查 daemon/Pi 监听端口 | Phase 4 未增加 Pi 入站端口；node 复用既有 listener | Pi 仅 TCP 22；node 复用原 8443 listener | 通过 |

## 4. 执行记录

2026-08-13 实施前基线：工作树干净，HEAD `68abb88`；`make check` 全部包通过。Phase 3 代码门禁
已完成，但真实 HomeLab 三服务与物理 LCD 用户肉眼仍为 pending，不作为 Phase 4 已通过证据。

2026-08-13 最终自动化：`make check`、`go test -race ./...`、Phase 4 关键包 race×10、
`go mod verify`、`git diff --check` 均通过；六目标×两个程序的 12 个发布产物通过 checksum 与数量门禁。

Mac→DietPi 实机：两端升级到 HEAD `68abb88` 工作树构建并保留旧二进制。show_page 最终 sequence=9
在约 0.16 秒内 `executed`；show_message 从签发到 Pi 执行约 0.38 秒，Pi 内部执行 1 ms；指定
`deepseek-main` 刷新收到新快照后 `executed`；brightness 返回 `failed/unsupported_capability`。
Display 停止期间发布的 sequence=8 在 5 秒 TTL 外重连后为 `expired`，未下发。node/Display 重启后
sequence 与结果继续恢复；node `commands.json` 为 `0600`、9 条结果、0 pending，Pi
`command-results.json` 为 `0600`、8 条结果、高水位 9，服务 active 且 `NRestarts=0`。

安全实机：Pi→node 控制路由返回 403；Pi 只监听 SSH 22；两端日志/状态文件未发现控制/设备凭据、
Authorization/Bearer 或四条测试通知全文。当前真实 Codex 指标为 CRIT，物理屏按规格保持 CODING
CRIT 并压过远程页/消息，因此“解除 CRIT 后肉眼确认 API/通知”仍为用户手动验收项，不写为通过。

时延取舍：实测逐命令 file/directory fsync 在 Mac/Pi 分别造成约 1.1 秒与 1.5–1.8 秒阻塞；两端
改为同目录临时文件+原子 rename，保证正常进程/service 重启恢复并达到亚秒显示，但不承诺突然断电
时最后一条命令的 exactly-once。测试临时文件均已清理；Pi 上保留明确命名的升级前二进制用于回退。
