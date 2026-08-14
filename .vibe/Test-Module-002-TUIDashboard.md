# 模块 002 测试文档：TUI Dashboard

> 对应规格：Module-Spec-002-TUIDashboard.md
> 状态：Phase 3 五页、实体子页与自动轮播门禁完成；真实告警/服务手动对账待执行
> 最近执行：2026-08-14，FIX-004 主余额选择回归、完整门禁与 DietPi TTY 实机验证

Phase 2 Web Admin 对 Display 候选环境、SSH 原子部署和回滚的测试记录在
`Test-Module-005-WebAdmin.md`；本文件继续负责 TUI、Kiosk、快照和 systemd 运行契约。

## 1. Unit Test

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| U001 | 60×20 终端、3 个 Coding Plan 与 2 个 API 余额 | 选择 compact 布局，五类卡片的主值与状态均不截断 | 通过。`TestGoldenScreens/overview_live`：五类卡片主值、窗口、重置、状态全部完整可见，与 golden 逐字节一致 | 通过 |
| U002 | 终端小于最低尺寸 | 显示诊断页，不发生越界或 panic | 通过。运行时读取 `TIOCGWINSZ`；`TestSmallTerminalDiagnosticFitsAndUsesASCII` 验证 32×6 诊断页严格适配、只含 7-bit ASCII | 通过 |
| U003 | status=critical、ASCII 模式 | 使用 CRIT 文案表达，不只依赖颜色 | 通过。`TestStatusesAreTextOnly`：八种状态均以文本可辨，ASCII 渲染不输出颜色转义 | 通过 |
| U004 | estimated 指标低于 critical 阈值 | 最多显示 warning/estimate，不伪装精确 critical | 通过。`TestSoftPrecisionCapsAtWarning`：estimated/manual 在 3% 剩余时封顶 WARN，exact 同值为 CRIT；`TestEstimatedValueIsLabelled` 验证 EST 标签 | 通过 |
| U005 | observed_at 超过 stale_after | 保留数值并显示 STALE 与距今时间 | 通过。`TestDeriveDisplayStatusSemantics`（过期预算、逐指标 stale_after 覆盖两例）；`TestOfflineFooter` 验证 `DATA STALE 12M` | 通过 |
| U006 | 同一 source_epoch 接收 snapshot version 9，当前为 10 | 丢弃旧快照，页面不回退 | 通过。`TestSupersedesRejectsVersionRegression`；`syncclient.applySnapshot` 在 Supersedes 失败时直接丢弃且不通知重绘 | 通过 |
| U007 | 快照含未知可选字段 | 忽略字段并正常渲染 | 通过。`TestDecodeIgnoresUnknownFields`：顶层与 metric 层各注入一个未知字段，解码成功且指标不丢失 | 通过 |
| U008 | 快照主 schema 版本不兼容 | 显示升级提示，不加载错误数据 | 部分通过。`TestDecodeRejectsIncompatibleMajor` 与 `TestIncompatibleSchemaIsQuarantined`：拒绝加载并隔离文件；服务端在 hello 阶段回 `schema_unsupported` 并关闭连接。屏幕上的专用升级提示页属 P1-07 | 部分通过 |
| U009 | 最近成功 JSON 快照损坏 | 隔离损坏单文件，进入空状态，进程不退出且不创建 SQLite | 通过。`TestCorruptSnapshotIsQuarantined`：损坏文件移至 `last-known-good.corrupt`，返回 ErrNoSnapshot，后续正常写入可完全恢复；`syncclient.New` 对该错误只记日志不退出 | 通过 |
| U010 | 200 ms 内连续到达 8 个 delta | 合并更新，重绘次数受限且最终状态正确 | 部分通过。`Changed()` 为深度 1 缓冲通道，`loop` 收到信号后等待 200 ms 合并窗口并 `drainChanges` 再单次重绘；`screen.draw` 对相同帧不写终端。实测重绘计数属 P1-07 | 部分通过 |
| U011 | stdin 关闭，Phase 1 启动 | 固定显示 overview，不等待输入、不退出 | 通过（手动冒烟）。display 在 stdout 重定向到文件、无 TTY 的条件下正常渲染并持续运行；代码中无任何 stdin 读取路径 | 通过 |
| U012 | 随机按键/鼠标转义事件输入 TTY | 页面和配置不变化，进程继续运行 | 通过（结构性）。`cmd/homepi-display` 不读 stdin、不启用 mouse mode、不注册按键；rich 主题只追加渲染器生成的白名单 SGR | 通过 |
| U013 | show_message 含 ANSI escape | 文本被清理，终端状态不被注入改变 | 通过。`TestANSIEscapesAreStripped`：Provider 名、节点别名、金额三处注入 `\x1b[2J`、`\x07`、`\r\n`，渲染后无任何控制字节 | 通过 |
| U014 | source_epoch 改变且新 version=0 | 接受新 daemon 的完整快照并替换当前状态 | 通过。`TestSupersedesAcceptsNewEpochFromZero`、`TestDaemonRestartIsAcceptedAsNewEpoch` | 通过 |
| U015 | 终端实际尺寸不是 60×20 | 以运行时尺寸选择布局并记录诊断，不盲用照片推断值 | 部分通过。`ioctlWinsize` 使用 `TIOCGWINSZ` 取实际尺寸，`doctor` 输出实际值并与 60×20 基线对比告警。运行时降级布局属 P1-07 | 部分通过 |
| U016 | 当前绑定 node=A，收到 node=B 快照 | 拒绝 B，不合并或覆盖 A 的 UI 数据，并记录 wrong_source | 通过。`TestSourceBindingRejectsForeignNode`、`TestForeignNodeIsRejected`、`TestForeignSnapshotIsRejected`：既不保存也不加载，客户端记录 source_node 与 bound_to 的诊断 | 通过 |
| U017 | ASCII 模式渲染 Phase 1 正常、离线、认证失败状态 | 每行宽度不超过 60 单元，总高度不超过 20 行，仅使用 7-bit ASCII | 通过。`TestGridIsAlwaysExact`：正常/离线/AUTH/空/严重/恶意输入六种模型，每帧恰好 20 行 × 60 单元，全部字节位于 0x20–0x7e | 通过 |
| U018 | 终端无颜色且不支持 Unicode，配置 `style=ascii` | 框线、进度条和状态文本结构完整，输出与既有 golden 逐字节一致 | `TestASCIIStyleMatchesExistingGoldenRenderer` 对四种状态逐字节一致，且不含 escape；Pi 当前二进制 ASCII 冒烟同样无 rich SGR/字形 | 通过 |
| U019 | WebSocket 会话收到有效快照或心跳后断开 | 下一次重连从首次失败退避开始，不累积历史独立故障 | 通过。`session` 仅在有效快照/心跳后返回 healthy，`TestNextFailureCountResetsAfterHealthySession` 验证健康会话后计数重置为 1 | 通过 |
| U020 | 保存最近成功快照 | 临时文件和父目录均完成同步后返回成功 | 通过。`Store.Save` 在 rename 后调用平台 `syncDir`，`TestSyncDir` 在 Unix 测试目录执行真实目录 fsync | 通过 |
| U021 | DietPi systemd unit 与环境模板 | unit 绑定 tty1、等待 getty 完全停止、stdin=null、失败退避、自启动；Token 只引用环境变量且模板权限要求 0600 | 通过。测试同时锁定 `Conflicts=getty@tty1.service` 与 `After=getty@tty1.service`；Pi 上从 active getty 启动后 getty 变 inactive、display active、完整首屏保留 | 通过 |
| U022 | Kiosk stdin 为关闭的 pipe，连续重绘与退出 | 不读 stdin、不启用 mouse mode，隐藏光标，退出恢复光标 | 通过。`TestScreenIsOutputOnlyAndRestoresCursor` 验证隐藏/恢复光标、无 mouse mode、相同帧不重复写；运行参数可全由环境文件注入 | 通过 |
| U023 | 目标 Pi 首次 `enable --now`，getty@tty1 原先活动 | getty 完全停止后 Kiosk 才绘制；进程 active、快照落盘且 `/dev/vcs1` 同时出现 HomePi 首屏 | 通过。修复后真实执行 getty active → start display：getty inactive、display active，1,200 字节字符屏有 661 个非空格字符并呈现完整 60×20 首屏 | 通过 |
| U024 | 原子快照写入期间进程被强制停止，目录遗留 `last-known-good.json.tmp-*` | 下次启动清理同前缀普通临时文件；符号链接安全失败且不影响外部目标 | 通过。`TestNewRemovesInterruptedTempFile` 与 `TestNewRefusesInterruptedTempSymlink` 通过；Pi 启动前复现 1 个遗留文件，升级启动后为 0，主快照与首屏正常 | 通过 |
| U025 | rich 渲染正常/离线/AUTH/空/严重/恶意输入六种模型 | 剥离 ANSI 后每帧 20×60；只含指定单宽字符；无输入控制字符幸存 | `TestRichGridAndGlyphWhitelist`、`TestRichUsesOnlyApprovedSGRAndResetsEveryLine`：六种模型全部满足，恶意输入无 escape 逸出 | 通过 |
| U026 | rich 的 LIVE/OK/WARN/CRIT/AUTH/STALE/OFFLINE/WAIT | 状态文本保留，并使用规格映射的 SGR 颜色；reset 不跨行泄漏 | `TestRichStatusPaletteKeepsText` 覆盖 8 种 DisplayStatus，`TestRichLinkPalette` 覆盖 4 种 LinkStatus；20 行均以 reset 结束 | 通过 |
| U027 | `style=rich`、`style=ascii`、未知值 | 默认 rich；ASCII 与 golden 一致；未知值返回明确错误 | `ParseStyle` 接受大小写与首尾空白；默认 rich；`rainbow` 返回 `want rich or ascii`；环境/flag 路径通过 | 通过 |
| U028 | 相同 ViewModel 各用 rich/ASCII 渲染 20 次 | 每个主题内部逐字节确定，相同帧仍被去重 | 两主题各重复 20 次完全一致；`TestScreenIsOutputOnlyAndRestoresCursor` 继续证明相同帧不写，并新增样式恢复 reset | 通过 |
| U029 | rich 正常、WARN、CRIT 三种卡片 | 所有画面外框均为亮青；进度括号/`█` 均为亮青、`░` 均为亮黄；状态徽标仍按独立语义色变化 | `TestRichBrightBorderAndCyanYellowProgress` 三组均通过：外框/括号/`█`=`1;36`，`░`=`1;33`，徽标分别为绿/黄/红 | 通过 |
| U030 | balance 主值为 `1`、`1.2`、`1.235` | TUI 分别显示 `CNY 1.00`、`CNY 1.20`、`CNY 1.24`，ASCII/rich 语义一致 | `TestBuildFormatsBalancesWithExactlyTwoDecimals` 生成三张卡片，金额文本分别为 `CNY 1.00`、`CNY 1.20`、`CNY 1.24`；全部 UI golden 继续通过 | 通过 |
| U031 | 拨号错误文本包含 `https://host/path` | URL 只扫描一次并替换为 `https://[redacted]`；函数在 250 ms 内返回且原 host/path 不泄漏 | 通过。测试先稳定复现 250 ms 超时；修复为只扫描尚未处理的原始后缀后，`TestRedactURLTerminatesAndRedactsEveryURL` 同时验证两个 URL 均脱敏、原 authority 不泄漏并立即返回 | 通过 |
| U032 | 默认 Phase 3 页面配置 | 五页恰好各一次、启动页为 CODING、默认每页 15 秒 | `DefaultRotationConfig` 与 Pi 正式环境均为五页唯一、CODING 启动、15 秒 | 通过 |
| U033 | 页面顺序缺页/重复/未知页，停留时间 <5 或 >300 秒 | Display 候选和运行时使用同一校验并拒绝 | `ParseRotationConfig`、`displayconfig.Parse/Validate`、`displaydeploy.Manager.Edit` 全部拒绝非法值 | 通过 |
| U034 | 时钟跨过当前页 dwell | 只前进一页；长时间跳跃按配置确定性追赶 | 虚拟时钟 10 秒前进一页；121 秒跳跃确定落在 HOMELAB，不长循环 | 通过 |
| U035 | HOMELAB 出现 CRIT，原页为 API 且剩余 7 秒 | 立即显示 HOMELAB；解除后返回 API 并继续剩余 7 秒 | `TestRouterRotatesPreemptsAndRestoresRemainingDwell` 通过，长时间抢占不消耗 API 剩余时间 | 通过 |
| U036 | 多个页面同时 CRIT | 按配置顺序稳定选择；状态不变时不反复重置抢占 | 自定义顺序中 SERVICES 在 HOMELAB 前，同时 CRIT 稳定选 SERVICES | 通过 |
| U037 | 五页 normal/empty/stale/auth/critical/恶意文本，ASCII/rich | 剥离样式后均为 20×60；ASCII 仅 7-bit；控制字符不逸出 | 五页×全状态×两主题均为精确 60×20，恶意 ANSI/换行被清理 | 通过 |
| U038 | snapshot 含 homelab_nodes/services，旧字段仍存在 | Provider 页面与 HomeLab 页面各自读取同一 1.1 当前快照；无历史副本 | Phase 3 E2E 同时渲染 Provider/HomeLab/Services/System；Pi 落盘 schema 1.1 且仍仅一份 LKG | 通过 |
| U039 | Web Admin 修改 page order/dwell 后 Test/Apply 失败 | 候选验证失败不替换 Pi 环境；健康失败恢复上一轮轮播配置 | Display candidate 输入锁定新键；非法配置在 SSH 替换前拒绝，快照不前进时 rollback 回归通过 | 通过 |
| U040 | 同一 Provider 的 available/voucher/cash 三个 balance 以乱序进入 ViewModel 构建 | 选择 `order` 最小的 available 作为卡片名称、金额和状态；不受输入顺序影响 | `TestBuildUsesLowestOrderBalanceAsProviderPrimary` 以正常/乱序两组输入均得到 `Kimi Available / CNY 0.00` | 通过（ui） |

## 2. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | DietPi 冷启动、存在本地快照 | 3 秒内出现首屏，网络就绪后自动更新 | 部分通过。真实 Pi 重启后 unit 自动启动，日志在服务启动同一秒加载 version=140 最近成功快照，屏幕随后 LIVE；SSH 约 10 秒恢复，但未用外部相机精确测量通电到首屏 ≤3 秒 | 部分通过 |
| E002 | DietPi 冷启动、无网络、存在快照 | 显示缓存和 OFFLINE/STALE，不停留黑屏 | 通过（开发机）。手动冒烟：daemon 关闭状态下冷启动 display，立即显示缓存数据、顶栏 OFFLINE、页脚 DATA STALE / RETRYING | 通过 |
| E003 | 运行中断开远程节点 15 分钟 | Phase 1 保持 overview，顶栏离线，卡片逐步 stale | 部分通过。真实 Mac daemon 停止后 Pi 1 秒显示 OFFLINE，旧快照哈希不变；自动化覆盖 stale 时间推进。15 分钟连续时程未执行 | 部分通过 |
| E004 | 恢复远程节点 | 自动重连并更新，无需重启 TUI | 通过。真实 Mac daemon 启动后 Pi 在 3 秒内从 OFFLINE 自动回到 LIVE，display 进程未重启 | 通过 |
| E005 | 使用稳定电源、内核日志确认无当前欠压后，Waveshare 目标屏连续运行 24 小时 | 观察窗口内无当前欠压、花屏、崩溃、明显内存增长或日志暴涨 | 未通过且获例外接受。重启后读到 `0x50005`；本次启动累计 7 条 Undervoltage detected，收尾码为 `0xD0000`。产品所有者于 2026-08-11 明确要求忽略供电问题并继续开发；保留失败事实，但不再阻塞本轮软件交付 | accepted-by-owner |
| E006 | 不连接键盘、禁用 stdin 后冷启动 100 次 | 每次进入 overview Kiosk，无交互提示、无阻塞 | 部分通过。无 TTY 冷启动已手动验证；`TestNoLocalInteractionHints` 确认四种画面均不出现按键/触摸提示。100 次循环属 P1-07 | 部分通过 |
| E007 | Phase 3 配置 5 页自动轮播 | 按 page_order/dwell_seconds 切换，告警抢占结束后恢复原位置 | DietPi 以 5 秒配置实测完整顺序与回环，最终恢复 15 秒；抢占/恢复由虚拟时钟通过，物理屏真实 CRIT 注入待用户复现 | 部分通过 |
| E008 | systemd 杀死进程一次 | 服务按退避重启并恢复快照；无每秒崩溃循环 | 通过。真实 SIGKILL 后第 11 秒重启，`NRestarts` 0→1、PID 改变，加载 version=28 最近快照，屏幕 LIVE，临时快照文件为 0 | 通过 |
| E009 | 当前 DietPi `TERM=linux` rich 与 ASCII 两种配置 | 布局和数据不变；rich 色彩/字符正确；ASCII 降级完整 | 当前 ARM64 二进制默认 rich 在 tty1 正常；同一二进制 `-style ascii` 隔离冒烟输出 1,262 字节、包含 HOMEPI、rich SGR/字形均为 0，临时文件已清理 | 通过 |
| E010 | 连续接收 100 次更新并重启 Pi | 数据目录始终最多一个有效最近成功快照，无历史版本、SQLite 或指标样本 | 通过。`TestOnlyOneSnapshotFileEverExists`（100 次写入）与 `TestRestartRendersFromDiskWithNoHistory`；手动冒烟经 175 个快照版本后目录仍只有一个文件 | 通过 |
| E011 | Phase 1 配置一台主力电脑并连接 | overview 仅显示该 node 别名和五类指标，不出现 node 选择或聚合 UI | 通过。手动冒烟画面顶栏仅 `DEV-MAC`，页脚 `SOURCE DEV-MAC`，无任何选择器 | 通过 |
| E012 | 在实际 480×320 屏按 UI Spec 001 渲染 overview | 60×20 内五类指标、同步状态和 Pi 健康均完整可读，无滚动或交互提示 | 通过。真实 fb_ili9486 480×320/16bpp framebuffer 截图显示完整五类指标、LIVE、Pi 健康与 KIOSK LOCKED；字符缓冲严格 60×20。证据：`.vibe/evidence/Phase1-Pi-Screen.png` | 通过 |
| E013 | Pi 已有旧 epoch 最近成功快照，新 daemon 启动但首次采集尚未成功 | 保留内存和磁盘旧快照；连接维持 WAIT/OFFLINE 语义；新 daemon 成功采集后才替换 | 通过。`TestStreamWaitsForDataBeforeFirstSnapshot` 验证空 daemon 先发 heartbeat；`TestEmptyRestartDoesNotOverwriteLastKnownGood` 验证内存/磁盘旧快照保留并在首次真实指标后切换 epoch | 通过 |
| E014 | 在实际 480×320 framebuffer 启用 rich | `┌─┐│├┤└┘█░` 无缺字、无错列；结构色与状态色可辨；首屏仍为 60×20 | 通过。蓝框、青标题/Provider、洋红节点/Kiosk、绿进度/状态与白主值均正常；`/dev/vcsu1` 含 290 横线、30 竖线、完整角/接点、各 24 个 `█`/`░`。证据：`.vibe/evidence/Phase1-Pi-Screen-Rich.png` | 通过 |
| E015 | 在实际 480×320 framebuffer 部署 bright-border 配色 | 边框比 E014 更亮；进度条呈亮青剩余 + 亮黄已消耗；无缺字、错列或服务重启 | 通过。亮青边框明显高于 E014 深蓝亮度；进度为亮青 `█` + 亮黄 `░`，OK 保持绿色；字形计数不变，service 0 重启/0 warning。证据：`.vibe/evidence/Phase1-Pi-Screen-Rich-Bright.png` | 通过 |
| E016 | display 已连接；停止节点直到一次重连拨号失败，再恢复节点 | display 进程/PID 不重启，持续退避；节点恢复后自动接收新 epoch 快照并恢复 `LIVE` | 通过。失败基线曾卡住约 20 分钟；修复后二进制在 22:25:59 收到 EOF、22:26:01 记录一次已脱敏 `connection refused`，节点恢复后于 22:26:07 写入新 epoch。display PID 始终为 6428、`NRestarts=0`、CPU 0.5%，无需手动重启 | 通过 |

## 3. 性能测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| P001 | Pi 3 稳态运行 1 小时 | RSS ≤120 MB，空闲 CPU 平均 ≤5% | 部分通过且获例外接受。rich 上线短样本 RSS=10,828 KiB、CPU=0.3%，仍远低于预算；未形成 1 小时平均/趋势，产品所有者已明确忽略供电相关稳定性限制 | accepted-by-owner |
| P002 | 每秒 10 个模拟 delta，持续 5 分钟 | UI 正常更新，实际渲染 ≤2 FPS，心跳不超时 | 未执行（需 P1-07 计量）。已实现的抑制机制：200 ms 合并窗口 + 相同帧不写终端 | 待执行 |
| P003 | 远程快照改变可见主值 | 快照接收至可见更新 ≤1 秒，另记录 SPI 实际限制 | 部分通过（开发机）。手动冒烟中编辑 Mock 文件后数秒内屏幕更新；端到端计时与 SPI 限制需 Pi 实机 | 部分通过 |
| P004 | Phase 3 五页轮播 5 分钟，数据保持不变 | 只在切页/分钟/可见状态变化时写帧；平均 CPU ≤5%、RSS ≤120 MB | Pi 5 秒高频轮播 31 次取样：平均 CPU 0.52%、峰值 0.6%、最大 RSS 13,064 KiB；同步年龄按分钟变化，相同帧由 screen 去重 | 通过 |

## 4. 执行记录

| 日期 | 范围 | 命令 | 结果 |
|---|---|---|---|
| 2026-08-10 | P1-03 | `make check` | 全部通过 |
| 2026-08-10 | golden screen | `make golden` + `go test ./internal/ui/` | 四个画面固化于 `internal/ui/testdata/`，与 UI-Spec-001 第 4、5 节一致 |
| 2026-08-10 | 手动冒烟 | 见 IMPL-001 第 7 节 | 六项全部符合预期 |
| 2026-08-10 | FIX-001 审查整改 | `make check`、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`make checksums` | 84 个测试/子测试通过；race 通过；12 个跨平台产物构建并生成校验和 |
| 2026-08-10 | P1-07 Kiosk 自动化 | `go test -count=1 ./cmd/homepi-display ./internal/kioskunit ./internal/ui ./internal/pihealth` | 环境注入、小终端诊断、输出生命周期、systemd 契约、60×20 golden 与 Pi health 全部通过 |
| 2026-08-10 | P1-08 并发稳定性 | `go test -race -count=10 ./internal/e2e` | 连续 10 轮通过；测试夹具已按 snapshot_version 同步并等待后台 goroutine 退出，旧 TempDir 竞态未复现 |
| 2026-08-11 | 目标 Pi 只读基线 | `ssh dietpi` 检查系统、TTY、节流与部署状态 | Debian 12/ARM64；tty1 精确 60×20；`get_throttled=0x50000`；HomePi binary/config/unit/data 均不存在 |
| 2026-08-11 | Mac→Pi 首次部署 | macOS LaunchAgent + Pi `systemctl enable --now` + `/dev/vcs1` | TLS/设备鉴权成功，Pi 保存 3,755 字节快照，RSS 7,920 KiB；首次屏幕全空白，定位为 getty 停止清屏竞态，U021/U023 转失败待修复 |
| 2026-08-11 | Pi 强制启停恢复 | getty active → 启动 Kiosk → 检查 `/dev/vcs1` 与数据目录 | unit 顺序修复后完整 60×20 首屏出现；同时发现 `last-known-good.json.tmp-*` 跨重启遗留，U024 转失败待修复 |
| 2026-08-11 | 两项实机缺陷回归 | focused test/race + Pi getty 冲突启动 + 遗留临时文件恢复 | unit/snapstore focused 与 race 全通过；Pi 首屏 661 个非空格字符、临时文件 0，U021/U023/U024 转通过 |
| 2026-08-11 | Pi 故障矩阵与回退 | Mac stop/start、display SIGKILL、`.previous` 回退/恢复 | OFFLINE 1 秒、LIVE 恢复 3 秒；SIGKILL 后 11 秒重启；旧版回退与当前版恢复均 active/LIVE，`.previous` 保留 |
| 2026-08-11 | Pi 重启自启与实屏 | reboot、systemd、`/dev/vcs1`、fb0 RGB565 截图 | boot ID 改变；SSH 约 10 秒恢复；display enabled/active、getty inactive、屏幕 LIVE、临时文件 0；截图写入 `.vibe/evidence/Phase1-Pi-Screen.png` |
| 2026-08-11 | 供电稳定性前置检查 | reboot 后 `get_throttled` 连续取样 + kernel journal | 首次为 `0x50005`；本次启动累计 7 次欠压检测，收尾为 `0xD0000`，新增历史 soft temperature limit 位。24 小时门禁按失败提前终止 |
| 2026-08-11 | 产品决策 | 用户指令“供电问题请忽略，直接继续开发” | E005/P001 原始失败与限制保留，状态记为 accepted-by-owner，不再阻塞 Phase 1 软件交付 |
| 2026-08-11 | 修复后完整门禁 | `make check`、全仓 race、E2E race×10、snapstore/kioskunit race×20、12 产物 SHA、安全检查 | 全部通过；无构建目录或负向测试临时目录遗留 |
| 2026-08-11 | rich 自动化 | focused test、`make check`、全仓 race、UI/display/kioskunit race×10、`go mod verify` | rich/ASCII、白名单、色彩、网格、确定性与生命周期全部通过；全仓标准/race 通过；依赖校验通过 |
| 2026-08-11 | rich 交叉构建 | `make checksums VERSION=0.1.0` + `shasum -a 256 -c` | 12 个产物全部构建并通过 SHA 自校验；目标 ARM64 SHA-256 为 `e7485563...ab63ce` |
| 2026-08-11 | rich DietPi 实机 | SHA 校验安装、systemd、`/dev/vcsu1`、ASCII 隔离冒烟、fb0 RGB565 截图 | service active/running、0 重启、无 warning；rich 字形/色彩通过，ASCII 无 rich 输出；截图写入 `.vibe/evidence/Phase1-Pi-Screen-Rich.png` |
| 2026-08-11 | bright-border 微调 | focused、`make check`、全仓 race、UI/display/kioskunit race×10、12 目标 SHA、Pi framebuffer | 全部门禁通过；ARM64 SHA `7310dc3f...5039b`；Pi active、0 重启/0 warning，亮青边框与青黄进度截图写入 `.vibe/evidence/Phase1-Pi-Screen-Rich-Bright.png` |
| 2026-08-12 | 金额文本固定两位 | `go test ./internal/protocol ./internal/ui`、focused API/快照/E2E、全仓 test/race/vet、`git diff --check` | 整数、一位和三位金额均统一为两位；ASCII/rich 共用 ViewModel 语义，全部通过 |
| 2026-08-12 | FIX-003 失败基线 | Mac node 短暂停止/恢复；Pi systemd、journal、TCP、快照时间和进程 CPU 核查 | 网络、TLS、Token、绑定与服务状态正常；含 URL 的失败日志触发 `redactURL` 无限循环，display 无后续 TCP 重连；U031/E016 转失败待修复 |
| 2026-08-12 | FIX-003 自动化 | U031 先失败、focused test、E016、`make check`、全仓 race、`gofmt -l`、`git diff --check`、Linux ARMv7 交叉构建 | U031 修复后通过；E016 自动化在一次真实失败拨号后约 2 秒退避恢复；全仓标准/race、vet、格式与 ARMv7 ELF 构建全部通过 |
| 2026-08-12 | FIX-003 DietPi 实机 | 部署 ARM64 候选，Mac node stop，等待 Pi 出现一次失败拨号，再 start node | 日志 URL 为 `https://[redacted]`；同一 PID 6428、0 service restart 接收新 epoch，快照于 22:26:07 前进，CPU 0.5%；旧二进制保留为 `/usr/local/bin/homepi-display.pre-fix003` |
| 2026-08-13 | Phase 3 自动化 | 五页/轮播/抢占、双主题 60×20、Display 环境事务、E2E、全仓 test/race、关键包 race×10 | 全部通过；旧 overview golden 不变，1 秒 tick 不会逐秒重写同步年龄 |
| 2026-08-13 | Phase 3 DietPi | SHA/候选校验、5 秒完整轮播、5 分钟资源取样、恢复 15 秒、字符缓冲与 RGB565 framebuffer | 顺序完整；平均 CPU 0.52%、最大 RSS 13,064 KiB；service active/0 restart/仅 SSH 22；schema 1.1 LIVE；证据 `.vibe/evidence/Phase3-Pi-Homelab.png` |
| 2026-08-13 | 最终规格对账 | 超容量实体 dwell 子页、CRIT 首屏；重跑全仓/race/race×10/12 产物并重新部署 Pi | 全部通过；最终 Display SHA `e1163879…0cb795`，service active、`NRestarts=0` |
| 2026-08-14 | FIX-004 主余额选择 | 失败基线、UI×10、全仓/race、Linux ARMv7 交叉构建、ARM64 DietPi 候选与 `/dev/vcs1` | 自动化全部通过；TTY API 页显示 `Kimi Available  CNY 0.00  OK`，service active、PID 1766、`NRestarts=0` |

用户已确认 Pi 3 B+ 与 480×320 屏可显示 DietPi CLI；2026-08-11 实机读取 `tty1=60×20`，
已直接确认 UI-001 网格基线（代码仍使用 `TIOCGWINSZ`，未硬编码）。节流码当前位已清零但
历史位保留；重启后已出现新的当前欠压与内核事件，E005 不能写成技术通过。产品所有者已明确
接受该限制并要求继续开发。测试临时脚本与文件已清除。

## 5. 本轮由测试发现并修复的缺陷

| 缺陷 | 影响 | 修复 | 回归用例 |
|---|---|---|---|
| ETag 基于响应体哈希，`generated_at` 每次变化 | `If-None-Match` 永不命中，Pi 每次轮询都全量下载 | 改为基于 `(source_epoch, snapshot_version)` | `TestIfNoneMatchReturns304` |
| reset 倒计时向下取整 | `resets_in=2h` 显示为 `RESET 1H`，持续低估等待时间 | 倒计时改为四舍五入（`countdown`），「距今」仍截断 | golden `overview_live` |
| 断网时 `CRIT` 掩盖 `OFFLINE` | daemon 挂掉且缓存数据为 critical 时，顶栏与页脚都不提示链路已断 | 顶栏优先级改为 `WAIT > OFFLINE > CRIT > LIVE` | `TestOfflineOutranksCriticalInHeader`、`TestDeriveLinkStatus` |
| URL 脱敏反复扫描自身占位文本 | 一次失败拨号即可让重连 goroutine 无限忙循环、CPU 升高并永久 OFFLINE | 改为单次前向扫描未处理后缀，并为 WebSocket 拨号增加独立 30 秒截止时间 | `TestRedactURLTerminatesAndRedactsEveryURL`、`TestDisplayReconnectsAfterDialFailureAndDaemonRecovery`、E016 实机 |
| 多余额 Provider 使用最后遍历值 | Kimi 卡片可能出现 Available 名称配 Cash 金额，负现金修复后会显示 `CNY -1.04` | 按 `(order, id)` 选择主余额，并从同一指标取得名称、金额和状态 | `TestBuildUsesLowestOrderBalanceAsProviderPrimary`、FIX-004 DietPi TTY 实机 |
