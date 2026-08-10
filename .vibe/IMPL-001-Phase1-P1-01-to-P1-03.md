# 实现说明 001：Phase 1 P1-01 ~ P1-03

> 文档 ID：IMPL-001
> 版本：0.2
> 日期：2026-08-10
> 对应任务：P1-01、P1-02、P1-03
> 状态：待用户验收

本文件记录 Phase 1 前三个任务的实际实现结构、与规格的偏差、以及尚未覆盖的部分。它是代码与
`.vibe` 规格之间的对照表，不重复规格内容。

## 1. 仓库结构

```text
cmd/homepi-node/       远端采集 daemon（serve / doctor / --version）
cmd/homepi-display/    Pi Kiosk 显示端（run / doctor / --version）
internal/buildinfo/    构建期注入的版本信息
internal/decimal/      定点十进制，金额与配额精确表示
internal/protocol/     P1-02 协议与领域模型、状态语义、脱敏审计
internal/state/        daemon 内存当前状态、epoch 与版本单调性
internal/connector/    连接器接口与错误分类
internal/connector/mock/  文件驱动的 Mock 连接器
internal/scheduler/    退避、抖动、并发上限、blocked_auth 策略
internal/nodeapi/      LAN 快照 API 与 WebSocket 事件流
internal/syncclient/   Pi 侧同步客户端与重连
internal/snapstore/    唯一一份最近成功快照，原子替换与隔离
internal/ui/           UI-001 的 60x20 ASCII 渲染器与 golden screen
internal/pihealth/     Pi 温度 / CPU / 内存采样
internal/e2e/          端到端垂直切片测试
examples/              可编辑的 Mock 数据示例
Makefile               构建、检查、交叉编译矩阵、golden 重生成
```

## 2. P1-01 工程与硬件基线

### 2.1 已完成

| 交付项 | 状态 | 证据 |
|---|---|---|
| Go module 与两个 cmd 入口 | 完成 | `go.mod`、`cmd/` |
| `--version` 可运行 | 完成 | 见第 6 节实际输出 |
| 六目标构建矩阵可重复生成 | 完成 | `make checksums`，六平台 × 两个二进制共 12 个产物 |
| 构建校验和 | 完成 | `dist/SHA256SUMS` |
| `doctor` 命令 | 完成 | 两个二进制均有；display 端输出终端尺寸与数据目录清单 |
| 实际 TTY 尺寸实测 | 完成 | Pi 上 `stty size < /dev/tty1` 返回 `20 60`，与 60×20 基线精确一致 |
| Pi 显示驱动与旋转 | 完成 | `dtoverlay=tft35a:rotate=90`，480×320 物理，fb0 单 framebuffer |

构建矩阵：`darwin/amd64`、`darwin/arm64`、`windows/amd64`、`linux/amd64`、`linux/arm64`、
`linux/arm/v7`。`CGO_ENABLED=0`，产物为静态二进制。Pi 3 B+ 实机为 aarch64，主推
`linux/arm64` 产物；`linux/arm/v7` 仍是 32 位 fallback。

本机工具链：Go 1.26.5 darwin/arm64（Homebrew 安装）。Pi 实机工具链待 P1-08 在 Pi 上
安装后确认。

### 2.2 实机基线（2026-08-10 用户回填）

用户在 Pi 上跑了诊断命令，以下为实测值。

| 项 | 实测值 | 与规格的关系 |
|---|---|---|
| OS | Debian 12 bookworm + DietPi 10.6.2 RC2 (`G_DIETPI_VERSION_CORE=10`) | 符合 HL-Spec §1 |
| 内核 | `6.12.96+rpt-rpi-v8 #1 SMP PREEMPT Debian 1:6.12.96-1+rpt1` | 较新，内核自带 KMS 与 Waveshare 驱动 |
| 架构 | aarch64 / 64-bit | Pi 3 B+ 在 64 位下运行 |
| 型号 | Raspberry Pi 3 Model B Plus Rev 1.3 | 完全匹配 |
| 实际 TTY 尺寸 | `stty size < /dev/tty1` → `20 60` | **精确命中 UI-001 60×20 推断基线**，不再是推断 |
| 物理显示 | 480×320，dtoverlay=tft35a:rotate=90，fb0 单 framebuffer | 与背板 `RoHS, 480x320 Pixel` 一致 |
| 内核字体 | `fbcon=font:ProFont6x11`（boot 期）→ console-setup `FONTFACE="Fixed" FONTSIZE="8x16"` | 当前 8×16，与 60×20 网格算术吻合（480/8=60 列，320/16=20 行）|
| cmdline | `console=tty1 ... fbcon=map:10` | tty1 是 Kiosk 的目标终端 |
| IP | 192.168.31.166 | LAN 内可用 |
| 内存 | 906 MB total / 832 MB available | 远超 Pi 3B+ 的 1 GB 总计 |

**结论**：UI-001 60×20 不再是照片推断，而是 Pi 实机的运行时网格。代码已经
按运行时尺寸渲染（`TIOCGWINSZ` + `cmd/homepi-display/winsize_unix.go`），
该假设在实机上得到验证，golden screen 的列位置也是正确的。

### 2.3 风险（产品所有者已接受延期）：欠压告警仍然存在

`vcgencmd get_throttled` 返回 `throttled=0x50005`，含义：

- bit 0 = 1：当前正在发生欠压
- bit 2 = 1：当前 ARM 频率被节流
- bit 16 = 1：本次启动以来发生过欠压
- bit 18 = 1：本次启动以来发生过节流

`dmesg | grep -ci 'voltage'` 返回 28 次，其中最近一条
`hwmon hwmon1: Undervoltage detected!` 仍然出现。

PRD §M0 与 HL-Spec §11 都把「内核日志无欠压」列为稳定性测试的硬门槛：
欠压会导致 SD 卡损坏、随机重启与指标异常。

**产品所有者决定（2026-08-10）**：该问题延期至产品上线后处理，不阻塞开发进度。
本轮交付物按此决定继续推进；P1-08 跑 24 小时稳定性测试时必须在测试报告的「电源状态」
一栏注明 `vcgencmd get_throttled` 的当前值，否则不得宣称 P1-08 通过。

常见原因与建议排查顺序（上线前必须处理）：

1. **电源适配器**：5V 3A 是 Pi 3 B+ + 3.5" SPI 屏的最低要求。原装或品牌 5V 3A USB。
2. **线材**：低质量 USB 线（特别是只支持充电、不支持数据的）会有明显压降。建议使用
   截面积 22 AWG 或更粗的短线（< 1 米）。带电源指示灯的线材通常是陷阱。
3. **屏幕供电**：Waveshare 3.5" SPI 屏直接从 5V 取电，会拉低整机电压。如果使用 GPIO
   跳线给屏幕独立供电 5V，可以分担一部分负载。
4. **万用表实测**：在负载下（Pi + 屏幕都在工作）测 GPIO pin 2/4 的 5V 实际值，应 ≥ 4.85V。
5. **临时规避**：可在 `/boot/config.txt` 加 `usb_max_current_enable=1` 与
   `max_usb_current=1`（已内置 USB 电流限制的情况下），但这只是软缓解，不能根治。

消除欠压后再次运行 `vcgencmd get_throttled`，要求 `throttled=0x0`。

### 2.4 LAN TLS 与配对方案

P1-01 要求确认方案，本轮**确认采用**：TLS 证书指纹固定 + 单设备 Bearer Token。

当前实现程度：

- 设备 Token 已实现：256-bit，`crypto/rand` 生成，常数时间比较，设备作用域。
- Token 通过 `HOMEPI_DEVICE_TOKEN` 环境变量传给 display，避免出现在进程列表。
- TLS 终止与证书指纹固定**尚未实现**，属于 P1-04 交付范围。当前 `serve` 默认监听
  `127.0.0.1:8443` 明文 HTTP，仅用于 P1-03 的本机 Mock 验证。

**实机网络拓扑提示**：Pi 在 192.168.31.166，daemon 主力电脑只要在同一 192.168.31.0/24
网段并能接受 Pi 的出站连接即可。`make run-node -addr 0.0.0.0:8443` 可让 daemon 监听所有
网卡；Pi 用 `-node-url http://<daemon-ip>:8443` 连接。LAN 内 TLS 终止（自签证书 + 指纹
固定）属于 P1-04。

## 3. P1-02 协议与领域模型

### 3.1 实现要点

- `MetricSnapshot` / `ProviderMetric` / `ConnectorHealth` 按 HL-Spec 5 实现，**结构上不存在**
  token、cookie、authorization、auth.json、原始响应等字段，因此泄露只能来自实现错误而非
  协议设计，并由 `protocol.AuditJSON` 自动检查。
- `source_epoch` + `snapshot_version`：`Supersedes` 保证同 epoch 内版本只能前进，新 epoch
  无条件接受（daemon 重启后版本从 0 重新开始不被误判为回退）。
- `SourceBinding.Accept` 实现单 node 来源校验，拒绝而非合并。
- 状态语义分两层：
  - 数据面 `MetricStatus`（ok/warning/critical/unknown/error/stale）。
  - 展示面 `DisplayStatus`（OK/WARN/CRIT/DELAY/STALE/AUTH/N/A/ERROR），由
    `DeriveDisplayStatus` 派生，连接器无法直接指定，因此上游数据无法伪造徽标。
  - 顶栏 `LinkStatus`（LIVE/OFFLINE/WAIT/CRIT）。
- 求值顺序遵循 HL-Spec 7：认证 → 可用性 → 传输错误 → 新鲜度 → 数值阈值。
  `estimated` / `manual` 精度封顶在 WARN。
- 金额使用 `internal/decimal` 定点表示，JSON 以字符串编码，`0.1` 往返后仍是 `0.1`。
- 未知字段被忽略（HL-Spec 6.3），主版本不兼容被拒绝。

### 3.2 与规格的偏差

| 项 | 规格 | 实现 | 理由 |
|---|---|---|---|
| `snapshot_delta` | HL-Spec 6.2 定义了增量消息 | 消息类型已定义，服务端目前只发 `snapshot_full` | Phase 1 快照远小于 256 KB 预算，全量推送更简单且无版本补齐风险；delta 留给后续按需实现，客户端已能接收该类型 |
| `source_epoch` 类型 | HL-Spec 5.1 写 UUID | 实现为受限字符集字符串，daemon 生成 UUID 形态值 | 便于测试注入可读 epoch；校验仍拒绝控制字符与超长值 |

## 4. P1-03 Mock 端到端垂直切片

### 4.1 数据流

```text
examples/mock-fixture.json
  -> mock.Connector.Collect       每轮重新读文件，支持运行中编辑
  -> scheduler                    抖动、并发上限 4、退避、blocked_auth
  -> state.Current                内存唯一当前值，无历史
  -> nodeapi                      Bearer 认证、ETag、WebSocket
  -> syncclient                   出站连接、来源校验、版本校验、重连
  -> snapstore                    临时文件 + fsync + 原子替换，唯一一份
  -> ui.Build -> ui.Render        60x20 ASCII 帧
```

### 4.2 关键实现决策

- **Mock 数据文件每轮重读**：这是 P1-03 手动验收动作（改数据 → 屏幕变化）的前提。
- **渲染是纯函数**：`Render(ViewModel) []string`，同输入必同输出，这是 golden screen 可信的
  基础，也让「仅在可见状态变化时重绘」可以用帧比较实现。
- **越界防护集中在一处**：所有内容行都经过 `content()`，先剥离非可打印 ASCII 再强制补齐 /
  截断到 58 单元。任何调用方都无法绕过，因此上游名称里的 ANSI 转义不可能到达终端。
- **Kiosk 只写不读**：`cmd/homepi-display` 全程不读 stdin、不开启 mouse mode、不注册按键。
  ANSI 序列只用了 6 个：进入/离开 alt screen、显示/隐藏光标、home、clear。
- **ETag 基于 (epoch, version)** 而非响应体哈希。响应体含 `generated_at`，每次请求都变，
  用体哈希会导致 304 永不命中、Pi 每次轮询都全量下载。
- **掉线检测跟随心跳**：客户端读超时 = 3 × 服务端广播的心跳间隔，钳制在 15–90 秒，
  对齐 PRD 9.1 的 90 秒陈旧度目标。

### 4.3 本轮修复的三个实现缺陷

三个缺陷都是被测试发现的，不是事后审查发现的：

1. **ETag 永不命中**（`nodeapi`）：原实现哈希整个响应体，`generated_at` 每次不同，
   `If-None-Match` 永远返回 200。改为基于 `(source_epoch, snapshot_version)`。
2. **reset 倒计时向下取整**（`ui`）：`resets_in=2h` 显示成 `RESET 1H`，因为距今
   1h59m59s 被截断。为倒计时引入 `countdown()` 四舍五入到最近单位，与 UI-001 4 的
   `RESET 2H` 样例一致；「距今多久」仍然截断（`38S AGO`）。
3. **断网时 OFFLINE 被 CRIT 掩盖**（`protocol`）：UI-001 5.1 与 5.4 在顶栏优先级上冲突。
   原实现让 CRIT 优先，导致 daemon 挂掉且缓存数据为 critical 时，顶栏显示 `CRIT`、
   页脚显示告警数而非 `DATA STALE`/`RETRYING`，用户看不出链路已断。
   按 HL-Spec 7「先检查新鲜度再检查数值」改为 `WAIT > OFFLINE > CRIT > LIVE`：
   断线时屏幕上全是最近成功值，顶栏不应断言一个无法确认的实时严重状态。
   卡片级 CRIT 徽标与页脚告警计数仍然可见，信息没有丢失。
   已由 `TestOfflineOutranksCriticalInHeader` 锁定。

### 4.4 UI-001 列位置标准化

UI-001 4 与 5 的样图是手绘的，正常页脚右列在第 24 格、空状态页脚右列在第 25 格，不一致。
实现统一为 24，并在 `internal/ui/render.go` 的列常量处注明。
`internal/ui/testdata/*.txt` 是最终的视觉契约。

已更新 `UI-Spec-001-ASCII-Design.md` 第 4、5 节的样图与实现输出一致。

### 4.5 FIX-001 分支审查整改

- Scheduler 的真实分类错误会传播到该 connector 最近成功的指标，TUI 可立即显示 AUTH/STALE/ERROR；成功采集清除错误。
- `Retry-After` 是抖动后的硬下限；连接器 Health 保留最后成功、连续失败和实际下次尝试时间。
- 新 epoch 在首次指标成功前只发送 heartbeat，不再用空快照覆盖 Pi 最近成功快照。
- Sync Client 在有效会话后重置历史退避，且只有有效快照到达后才将缓存数据标记为 LIVE。
- Snapshot Store 在 rename 后执行父目录 fsync，补齐 Linux/Pi 断电持久性。
- `.vibe` 规格、测试与实施文档不再被 Git 忽略。

## 5. 未覆盖范围

以下属于 P1-04 ~ P1-08，本轮**未实现**，不得视为已完成：

- TLS、证书指纹固定、Token 撤销、LAN 访问控制（P1-04）。
- `provider add/edit/list/test/remove` 与配置文件（P1-04）。
- macOS Keychain / Windows Credential Manager / Linux Secret Service（P1-04）。
- LaunchAgent / PowerShell 后台任务 / `systemd --user`（P1-04）。
- 五个真实连接器（P1-05、P1-06）。
- Bubble Tea 集成、DietPi systemd + autostart、崩溃退避（P1-07）。

  说明：ADR-003 指定 TUI 使用 Bubble Tea/Lip Gloss。本轮实现的是纯渲染层与一个最小帧写
  出器，未引入 Bubble Tea。原因是 P1-03 只要求 golden screen 与 Mock 闭环，而 Bubble Tea
  的事件循环属于 P1-07 的交付内容。渲染层与写出器已分离，P1-07 接入 Bubble Tea 时
  `ui.Render` 可直接作为 `View()` 的返回值，无需改动。

- 六目标产物的实机安装、24 小时稳定性、Provider 对账（P1-08）。

## 6. 验证命令与实际输出

执行日期：2026-08-10，环境：macOS 25.5.0 arm64，Go 1.26.5。

```text
$ make check
== gofmt ==
== go vet ==
== go test ==
ok  github.com/galendai/homepi-mon/internal/buildinfo   0.919s
ok  github.com/galendai/homepi-mon/internal/decimal     0.457s
ok  github.com/galendai/homepi-mon/internal/e2e         2.389s
ok  github.com/galendai/homepi-mon/internal/nodeapi     2.562s
ok  github.com/galendai/homepi-mon/internal/pihealth    1.221s
ok  github.com/galendai/homepi-mon/internal/protocol    2.956s
ok  github.com/galendai/homepi-mon/internal/scheduler   2.341s
ok  github.com/galendai/homepi-mon/internal/snapstore   3.992s
ok  github.com/galendai/homepi-mon/internal/state       3.933s
ok  github.com/galendai/homepi-mon/internal/ui          4.298s
```

共 78 个测试用例，全部通过。

```text
$ ./bin/homepi-node --version
homepi-node 0.1.0
commit:   6b305a7
built:    2026-08-10T10:05:30Z
platform: darwin/arm64
go:       go1.26.5

$ ./bin/homepi-display --version
homepi-display 0.1.0
commit:   6b305a7
built:    2026-08-10T10:05:30Z
platform: darwin/arm64
go:       go1.26.5
```

```text
$ make checksums
14d33b73...  homepi-display-0.1.0-darwin-amd64
181dd084...  homepi-display-0.1.0-darwin-arm64
0e5aa0d4...  homepi-display-0.1.0-linux-amd64
50fd5672...  homepi-display-0.1.0-linux-arm64
ea6078e0...  homepi-display-0.1.0-linux-armv7
641e897d...  homepi-display-0.1.0-windows-amd64.exe
8b711a8f...  homepi-node-0.1.0-darwin-amd64
a5fdcb34...  homepi-node-0.1.0-darwin-arm64
e97ed90b...  homepi-node-0.1.0-linux-amd64
945105c0...  homepi-node-0.1.0-linux-arm64
20a72378...  homepi-node-0.1.0-linux-armv7
393e67a5...  homepi-node-0.1.0-windows-amd64.exe
```

## 7. 手动验收步骤

两个终端，无需任何真实 Provider Key。

```sh
# 终端 1：启动 daemon
make run-node

# 终端 2：启动本机 Kiosk
make run-display
```

然后依次验证：

1. **正常显示**：屏幕出现 60×20 画面，三个 Coding Plan 与两个 API 余额，顶栏 `LIVE`。
2. **数据变化**：编辑 `examples/mock-fixture.json`，把 `codex.coding.5h` 的 `value` 改成
   `"7"`，屏幕在数秒内变为 `7% LEFT`、卡片 `CRIT`、顶栏 `CRIT`、页脚 `2 WARNINGS`。
3. **认证过期**：给 `kimi-coding` 的两条 metric 加 `"error_class": "auth"`，
   对应卡片变为 `AUTH` + `ACTION: RE-AUTH WITH OFFICIAL CLI ON DEV-MAC`，
   值变为 `-- LEFT`，其余连接器继续更新。
4. **断网降级**：Ctrl-C 停掉终端 1 的 daemon，屏幕保留最后数值，顶栏变
   `OFFLINE`，页脚变 `DATA STALE nM` / `LAST SYNC hh:mm` / `RETRYING`。
5. **离线冷启动**：Ctrl-C 停掉终端 2，在 daemon 仍关闭的情况下重新 `make run-display`，
   屏幕立即显示缓存数据与 `OFFLINE`，不出现黑屏或空状态。
6. **无历史**：`ls ./tmp/display-data/` 只有 `last-known-good.json` 一个文件。
7. **无本地交互**：随意敲键盘，画面不变、进程不退出。

清理：`rm -rf tmp bin dist`。

## 8. 已知限制

- daemon 与 display 之间目前是明文 HTTP，只能用于本机/可信 LAN 的 Mock 验证。TLS 属于
  P1-04。
- `homepi-node serve` 的全部配置为命令行标志，尚无配置文件。
- Pi 健康信息只在 Linux 上可读；其他平台显示 `--`，不显示误导性的 0。
- `homepi-display doctor` 在非 TTY（管道）下报告终端尺寸不可用，属预期。
- 未做 Pi 实机性能测量（RSS ≤120 MB、CPU ≤5%、≤2 FPS），属 P1-07 / P1-08。
