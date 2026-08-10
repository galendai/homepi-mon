# 模块 002 测试文档：TUI Dashboard

> 对应规格：Module-Spec-002-TUIDashboard.md
> 状态：P1-03 范围内的用例已执行；依赖 Pi 实机与 Bubble Tea 集成的用例仍未执行
> 最近执行：2026-08-10，macOS 25.5.0 arm64，Go 1.26.5，`make check`

## 1. Unit Test

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| U001 | 60×20 终端、3 个 Coding Plan 与 2 个 API 余额 | 选择 compact 布局，五类卡片的主值与状态均不截断 | 通过。`TestGoldenScreens/overview_live`：五类卡片主值、窗口、重置、状态全部完整可见，与 golden 逐字节一致 | 通过 |
| U002 | 终端小于最低尺寸 | 显示诊断页，不发生越界或 panic | 部分通过。`homepi-display doctor` 读取 `TIOCGWINSZ` 并在小于 60×20 时告警；运行时降级布局属 P1-07 | 部分通过 |
| U003 | status=critical、无颜色终端 | 使用符号和 CRIT 文案表达，不只依赖颜色 | 通过。`TestStatusesAreTextOnly`：八种状态均以文本可辨，渲染器不输出任何颜色转义序列 | 通过 |
| U004 | estimated 指标低于 critical 阈值 | 最多显示 warning/estimate，不伪装精确 critical | 通过。`TestSoftPrecisionCapsAtWarning`：estimated/manual 在 3% 剩余时封顶 WARN，exact 同值为 CRIT；`TestEstimatedValueIsLabelled` 验证 EST 标签 | 通过 |
| U005 | observed_at 超过 stale_after | 保留数值并显示 STALE 与距今时间 | 通过。`TestDeriveDisplayStatusSemantics`（过期预算、逐指标 stale_after 覆盖两例）；`TestOfflineFooter` 验证 `DATA STALE 12M` | 通过 |
| U006 | 同一 source_epoch 接收 snapshot version 9，当前为 10 | 丢弃旧快照，页面不回退 | 通过。`TestSupersedesRejectsVersionRegression`；`syncclient.applySnapshot` 在 Supersedes 失败时直接丢弃且不通知重绘 | 通过 |
| U007 | 快照含未知可选字段 | 忽略字段并正常渲染 | 通过。`TestDecodeIgnoresUnknownFields`：顶层与 metric 层各注入一个未知字段，解码成功且指标不丢失 | 通过 |
| U008 | 快照主 schema 版本不兼容 | 显示升级提示，不加载错误数据 | 部分通过。`TestDecodeRejectsIncompatibleMajor` 与 `TestIncompatibleSchemaIsQuarantined`：拒绝加载并隔离文件；服务端在 hello 阶段回 `schema_unsupported` 并关闭连接。屏幕上的专用升级提示页属 P1-07 | 部分通过 |
| U009 | 最近成功 JSON 快照损坏 | 隔离损坏单文件，进入空状态，进程不退出且不创建 SQLite | 通过。`TestCorruptSnapshotIsQuarantined`：损坏文件移至 `last-known-good.corrupt`，返回 ErrNoSnapshot，后续正常写入可完全恢复；`syncclient.New` 对该错误只记日志不退出 | 通过 |
| U010 | 200 ms 内连续到达 8 个 delta | 合并更新，重绘次数受限且最终状态正确 | 部分通过。`Changed()` 为深度 1 缓冲通道，`loop` 收到信号后等待 200 ms 合并窗口并 `drainChanges` 再单次重绘；`screen.draw` 对相同帧不写终端。实测重绘计数属 P1-07 | 部分通过 |
| U011 | stdin 关闭，Phase 1 启动 | 固定显示 overview，不等待输入、不退出 | 通过（手动冒烟）。display 在 stdout 重定向到文件、无 TTY 的条件下正常渲染并持续运行；代码中无任何 stdin 读取路径 | 通过 |
| U012 | 随机按键/鼠标转义事件输入 TTY | 页面和配置不变化，进程继续运行 | 通过（结构性）。`cmd/homepi-display` 不读 stdin、不启用 mouse mode、不注册按键；只输出 6 个固定 ANSI 序列 | 通过 |
| U013 | show_message 含 ANSI escape | 文本被清理，终端状态不被注入改变 | 通过。`TestANSIEscapesAreStripped`：Provider 名、节点别名、金额三处注入 `\x1b[2J`、`\x07`、`\r\n`，渲染后无任何控制字节 | 通过 |
| U014 | source_epoch 改变且新 version=0 | 接受新 daemon 的完整快照并替换当前状态 | 通过。`TestSupersedesAcceptsNewEpochFromZero`、`TestDaemonRestartIsAcceptedAsNewEpoch` | 通过 |
| U015 | 终端实际尺寸不是 60×20 | 以运行时尺寸选择布局并记录诊断，不盲用照片推断值 | 部分通过。`ioctlWinsize` 使用 `TIOCGWINSZ` 取实际尺寸，`doctor` 输出实际值并与 60×20 基线对比告警。运行时降级布局属 P1-07 | 部分通过 |
| U016 | 当前绑定 node=A，收到 node=B 快照 | 拒绝 B，不合并或覆盖 A 的 UI 数据，并记录 wrong_source | 通过。`TestSourceBindingRejectsForeignNode`、`TestForeignNodeIsRejected`、`TestForeignSnapshotIsRejected`：既不保存也不加载，客户端记录 source_node 与 bound_to 的诊断 | 通过 |
| U017 | 渲染 UI Spec 001 的 Phase 1 正常、离线、认证失败状态 | 每行宽度不超过 60 单元，总高度不超过 20 行，仅使用 7-bit ASCII | 通过。`TestGridIsAlwaysExact`：正常/离线/AUTH/空/严重/恶意输入六种模型，每帧恰好 20 行 × 60 单元，全部字节位于 0x20–0x7e | 通过 |
| U018 | 终端无颜色且不支持 Unicode | 框线、进度条和状态符号结构完整，所有状态仍可仅凭文本识别 | 通过。渲染器只产出 7-bit ASCII 且不含颜色转义，因此无色/无 Unicode 与全功能终端输出完全相同 | 通过 |
| U019 | WebSocket 会话收到有效快照或心跳后断开 | 下一次重连从首次失败退避开始，不累积历史独立故障 | 通过。`session` 仅在有效快照/心跳后返回 healthy，`TestNextFailureCountResetsAfterHealthySession` 验证健康会话后计数重置为 1 | 通过 |
| U020 | 保存最近成功快照 | 临时文件和父目录均完成同步后返回成功 | 通过。`Store.Save` 在 rename 后调用平台 `syncDir`，`TestSyncDir` 在 Unix 测试目录执行真实目录 fsync | 通过 |

## 2. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | DietPi 冷启动、存在本地快照 | 3 秒内出现首屏，网络就绪后自动更新 | 部分通过（开发机代替 Pi）。`TestRestartRendersFromDiskWithNoHistory`：新建客户端在构造阶段即加载磁盘快照，首帧不依赖网络。Pi 实机计时属 P1-07 | 部分通过 |
| E002 | DietPi 冷启动、无网络、存在快照 | 显示缓存和 OFFLINE/STALE，不停留黑屏 | 通过（开发机）。手动冒烟：daemon 关闭状态下冷启动 display，立即显示缓存数据、顶栏 OFFLINE、页脚 DATA STALE / RETRYING | 通过 |
| E003 | 运行中断开远程节点 15 分钟 | Phase 1 保持 overview，顶栏离线，卡片逐步 stale | 部分通过。`TestDaemonOfflineKeepsLastKnownGood`：断开后顶栏 OFFLINE、旧值保留、无伪造零值，时间推进后卡片转 STALE。15 分钟长时程属 P1-08 | 部分通过 |
| E004 | 恢复远程节点 | 自动重连并更新，无需重启 TUI | 部分通过。重连退避 2–60 秒带 ±20% 抖动已实现；`TestMockDataFlowsToScreen` 覆盖了持续连接下的自动更新。断开-恢复往返实测属 P1-08 | 部分通过 |
| E005 | 使用稳定电源、内核日志确认无欠压后，Waveshare 目标屏连续运行 24 小时 | 无欠压、花屏、崩溃、明显内存增长或日志暴涨 | 未执行（需 Pi 实机，P1-08）；欠压前置条件仍为 BLOCKED | 待执行 |
| E006 | 不连接键盘、禁用 stdin 后冷启动 100 次 | 每次进入 overview Kiosk，无交互提示、无阻塞 | 部分通过。无 TTY 冷启动已手动验证；`TestNoLocalInteractionHints` 确认四种画面均不出现按键/触摸提示。100 次循环属 P1-07 | 部分通过 |
| E007 | Phase 2 配置 5 页自动轮播 | 按 page_order/dwell_seconds 切换，告警抢占结束后恢复原位置 | 未执行（Phase 2 范围） | 待执行 |
| E008 | systemd 杀死进程一次 | 服务按退避重启并恢复快照；无每秒崩溃循环 | 未执行（需 P1-07 systemd unit） | 待执行 |
| E009 | 切换到 16 色终端 | 布局和数据不变，颜色与符号降级正确 | 通过（平凡满足）。渲染器不发出任何颜色转义，输出与终端色彩能力无关 | 通过 |
| E010 | 连续接收 100 次更新并重启 Pi | 数据目录始终最多一个有效最近成功快照，无历史版本、SQLite 或指标样本 | 通过。`TestOnlyOneSnapshotFileEverExists`（100 次写入）与 `TestRestartRendersFromDiskWithNoHistory`；手动冒烟经 175 个快照版本后目录仍只有一个文件 | 通过 |
| E011 | Phase 1 配置一台主力电脑并连接 | overview 仅显示该 node 别名和五类指标，不出现 node 选择或聚合 UI | 通过。手动冒烟画面顶栏仅 `DEV-MAC`，页脚 `SOURCE DEV-MAC`，无任何选择器 | 通过 |
| E012 | 在实际 480×320 屏按 UI Spec 001 渲染 overview | 60×20 内五类指标、同步状态和 Pi 健康均完整可读，无滚动或交互提示 | 未执行（需 Pi 实机，P1-07/P1-08）。60×20 网格已由 `TestGridIsAlwaysExact` 保证 | 待执行 |
| E013 | Pi 已有旧 epoch 最近成功快照，新 daemon 启动但首次采集尚未成功 | 保留内存和磁盘旧快照；连接维持 WAIT/OFFLINE 语义；新 daemon 成功采集后才替换 | 通过。`TestStreamWaitsForDataBeforeFirstSnapshot` 验证空 daemon 先发 heartbeat；`TestEmptyRestartDoesNotOverwriteLastKnownGood` 验证内存/磁盘旧快照保留并在首次真实指标后切换 epoch | 通过 |

## 3. 性能测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| P001 | Pi 3 稳态运行 1 小时 | RSS ≤120 MB，空闲 CPU 平均 ≤5% | 未执行（需 Pi 实机，P1-07/P1-08） | 待执行 |
| P002 | 每秒 10 个模拟 delta，持续 5 分钟 | UI 正常更新，实际渲染 ≤2 FPS，心跳不超时 | 未执行（需 P1-07 计量）。已实现的抑制机制：200 ms 合并窗口 + 相同帧不写终端 | 待执行 |
| P003 | 远程快照改变可见主值 | 快照接收至可见更新 ≤1 秒，另记录 SPI 实际限制 | 部分通过（开发机）。手动冒烟中编辑 Mock 文件后数秒内屏幕更新；端到端计时与 SPI 限制需 Pi 实机 | 部分通过 |

## 4. 执行记录

| 日期 | 范围 | 命令 | 结果 |
|---|---|---|---|
| 2026-08-10 | P1-03 | `make check` | 全部通过 |
| 2026-08-10 | golden screen | `make golden` + `go test ./internal/ui/` | 四个画面固化于 `internal/ui/testdata/`，与 UI-Spec-001 第 4、5 节一致 |
| 2026-08-10 | 手动冒烟 | 见 IMPL-001 第 7 节 | 六项全部符合预期 |
| 2026-08-10 | FIX-001 审查整改 | `make check`、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`make checksums` | 84 个测试/子测试通过；race 通过；12 个跨平台产物构建并生成校验和 |

用户已确认 Pi 3 B+ 与 480×320 屏可显示 DietPi CLI；照片推断为横屏 60×20、8×16 字体，
实机运行时尺寸为最终依据（代码已使用 `TIOCGWINSZ`，未硬编码）。照片中的欠压告警必须先消除，
E005 才可开始。测试临时脚本与文件已在执行后清除。

## 5. 本轮由测试发现并修复的缺陷

| 缺陷 | 影响 | 修复 | 回归用例 |
|---|---|---|---|
| ETag 基于响应体哈希，`generated_at` 每次变化 | `If-None-Match` 永不命中，Pi 每次轮询都全量下载 | 改为基于 `(source_epoch, snapshot_version)` | `TestIfNoneMatchReturns304` |
| reset 倒计时向下取整 | `resets_in=2h` 显示为 `RESET 1H`，持续低估等待时间 | 倒计时改为四舍五入（`countdown`），「距今」仍截断 | golden `overview_live` |
| 断网时 `CRIT` 掩盖 `OFFLINE` | daemon 挂掉且缓存数据为 critical 时，顶栏与页脚都不提示链路已断 | 顶栏优先级改为 `WAIT > OFFLINE > CRIT > LIVE` | `TestOfflineOutranksCriticalInHeader`、`TestDeriveLinkStatus` |
