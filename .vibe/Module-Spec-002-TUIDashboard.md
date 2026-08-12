# Module Spec 002：TUI Dashboard

> 模块 ID：MOD-002  
> 版本：1.2
> 状态：已认证

## 1. 模块目标

在 Raspberry Pi 3 Model B+ + DietPi 的本地控制台上，以低资源、低重绘、离线可用、无交互 Kiosk 方式展示统一指标。

## 2. 运行环境

- Raspberry Pi 3 Model B+。
- 已验证可启动并显示 CLI 的 DietPi 环境；实现前记录版本、内核与显示配置作为基线。
- 3.5inch RPi Display，背板标注 `RoHS, 480x320 Pixel`。
- 固定横屏 480×320；附件照片推断基线为 60 列 × 20 行、约 8×16 点阵字体，启动后以终端实际报告尺寸为准。
- Go ARMv7 二进制，systemd 管理，DietPi 启动后前台显示。
- 当前照片出现反复 `Undervoltage detected!`；完成 24 小时稳定性测试前必须消除欠压。

## 3. 子组件

| 子组件 | 职责 |
|---|---|
| Sync Client | 拉取快照、订阅事件、重连和版本校验 |
| Last-known-good Snapshot | 原子保存唯一一份最后有效脱敏快照 |
| State Store | 合并快照、连接状态、页面和告警 |
| Layout Engine | 按终端行列选择 compact/standard 布局 |
| Renderer | 纯 Go 确定性 rich/ASCII 双主题输出，控制低频重绘 |
| Command Handler | 执行 Module 004 验证后的 UI 命令 |
| Kiosk Supervisor | 管理默认页、自动轮播、无 stdin 运行和故障恢复 |
| Environment Validator | 为 Phase 2 Module 005 校验候选 Display 环境，不占用 TTY、不启动 Kiosk |

## 4. 页面与布局

视觉实现以 `UI-Spec-001-ASCII-Design.md` 为唯一产品界面基线。目标 DietPi 默认使用 `rich`：
Linux console 基础色/亮色、单宽 Unicode 线框与块状进度条；`ascii` 保留逐字节 7-bit golden screen。
两种主题只能改变样式，不能改变信息、行列、状态文本或刷新语义。
金额主值必须包含大写币种并固定显示两位小数；整数和一位小数补零，超出两位时按 half away from zero 四舍五入。

rich 配色固定为亮青外框；进度括号与剩余块 `█` 为亮青，已消耗块 `░` 为亮黄。进度配色不从
Card Status 派生，避免正常卡片变绿、告警卡片整条变黄/红而混淆“用量”和“状态”两个维度；
状态仍由独立徽标色与文字表达。

### 4.1 Compact 布局（目标首屏）

完整 60×20 首屏以 `UI-Spec-001-ASCII-Design.md` 第 4 节为准，本模块不维护第二份界面副本。
先生成安全的 60×20 ASCII 语义帧，再由 rich 装饰层按固定单元替换字形、添加白名单 SGR；因此输入清洗与布局只有一份实现。
首版按 60×20、8×16 字体设计；运行时终端尺寸是最终依据。卡片内容优先级：名称、5 小时窗口、每周窗口、重置、余额、状态、新鲜度；次要信息不足空间时隐藏而非截断主值。

### 4.3 主题选择与安全边界

- `HOMEPI_DISPLAY_STYLE=rich|ascii`，默认 `rich`；也可用 `-style` 覆盖环境值。
- 未知值启动失败，防止配置拼写错误后静默改变现场效果。
- rich 控制序列由渲染器常量生成，只允许 SGR；外部文本经过既有 7-bit ASCII 清洗后才进入语义帧。
- 每行结束前恢复默认样式；隐藏光标、alternate screen、清屏与光标归位仍由 Kiosk 生命周期管理。
- 相同 ViewModel + 相同主题必须逐字节相同；主题变化不改变 60×20 可见单元。

### 4.2 页面

- `overview`：Phase 1 总览，包含三个 Coding Plan 和两个 API 余额。
- `coding`：订阅窗口详情。
- `api`：余额详情。
- `homelab`：节点资源。
- `services`：服务/容器/告警。
- `system`：Pi、连接器和同步诊断。

Phase 1 只启用 `overview` 单页且不接受本地输入；Phase 2 只通过远端 Web Admin 配置现有 Kiosk，不改变页面模型；Phase 3 启用完整页面集合并自动轮播；Phase 4 接受远程指定页命令。

## 5. 状态呈现

| 状态 | 色彩建议 | 符号 | 文案 |
|---|---|---|---|
| ok | 绿色 | `OK` | LIVE/OK |
| warning | 黄色 | `!` | WARN |
| critical | 红色 | `X` | CRIT |
| delayed | 蓝色 | `~` | DELAY |
| stale | 灰/黄 | `~` | STALE |
| unavailable | 灰色 | `-` | N/A |
| error | 红色 | `?` | ERROR |

目标实机 `TERM=linux` 报告 8 个基础色，亮色用粗体 SGR 表达；不得要求 256 色。状态不能只靠颜色或边框表达。

## 6. 事件循环与刷新

- 数据事件、窗口尺寸、定时钟和远程命令进入同一状态更新循环；不注册键盘、鼠标或触摸输入。
- 仅在可见状态变化时渲染；时钟默认每 30 秒更新。
- 不使用持续 spinner；连接中使用静态符号或低频闪烁。
- 数据更新可合并 100–250 ms，避免多个连接器同时更新造成连刷。
- 终端尺寸小于最低值时显示诊断页，提示调整字体/旋转。

## 7. 同步与最近成功快照

- 启动先加载唯一一份本地最近成功快照，再建立网络连接。
- 快照写入使用临时文件 + 文件 fsync + 原子替换 + 父目录 fsync；不使用 SQLite，不保留旧版本或指标历史。
- 同一 `source_epoch` 只接受更大的 snapshot version；epoch 变化时接受新 daemon 的完整当前快照。
- 新 daemon 尚无指标时维持 `WAIT`/心跳，不得用空快照覆盖内存或磁盘中的最近成功快照。
- 收到至少一条有效快照或心跳后将会话视为健康；该会话结束后的重连退避从首次失败重新开始。
- 连接失败日志必须先完成有界、单次扫描的 URL 脱敏，再进入下一次退避；脱敏函数不得重新扫描
  自己生成的占位文本，也不得因包含 `://` 的拨号错误形成忙循环。
- 节点短暂离线且至少一次重连拨号失败后，客户端仍须继续退避重试；节点恢复后无需重启
  `homepi-display` 即可接收新 epoch 快照并恢复 `LIVE`。失败期间只读既有最近成功快照。
- 本地时间明显偏差时显示时钟警告；新鲜度优先使用服务端时间与接收时间联合判断。
- 快照 schema 不兼容或损坏时隔离该单文件后进入空状态，不崩溃。

## 8. Kiosk 运行模型

- 应用启动不读取 stdin，不初始化 mouse mode，不访问 evdev/GPIO。
- Phase 1 固定显示 `overview`；数据变化驱动局部状态更新。
- Phase 1 只绑定一个 `source_node_id`，顶栏显示该主力电脑的用户配置别名；收到其他来源快照时拒绝并记录诊断。
- Phase 2 允许远端 Web Admin 修改持久 Kiosk 参数，但 Pi 仍不提供本地设置或输入。
- Phase 3 由配置定义 `page_order` 与每页 `dwell_seconds`，自动轮播。
- Phase 4 的远程显示命令可临时覆盖自动轮播；到期后恢复此前轮播位置。
- 本地维护通过 SSH/systemd 完成，不在 3.5 英寸屏幕上提供退出、设置或调试菜单。
- Phase 2 Web Admin 只能通过 Module 005 定义的固定 SSH 操作和候选配置校验修改持久参数；
  该部署通道不是 Kiosk 运行时远程控制，也不得接收任意 Shell 输入。
- 任何意外按键或触摸事件均不得改变页面或终止进程。

## 9. 启动与恢复

- systemd 服务负责进程存活和日志。
- DietPi autostart 将服务绑定到目标本地 TTY 或启动包装器。
- 启动失败时通过 SSH/journald 诊断，不依赖本地键盘进入维护界面。
- 连续崩溃采用 systemd 退避；不得每秒写错误日志。
- 断线重试不得形成忙循环；离线稳态 CPU 仍须满足空闲预算，且每次失败最多记录一条脱敏日志。
- 提供健康检查：进程、最后渲染、最后同步、终端尺寸和最近成功快照状态。
- Phase 1 提供可安装的 `homepi-display.service`：绑定 `/dev/tty1`、`StandardInput=null`、
  `Restart=on-failure`、`RestartSec=10s`、启动频率限制和 `multi-user.target` 自启动。
- node URL、device/node ID、证书指纹和设备 Token 通过权限 `0600` 的
  `/etc/homepi-display/environment` 注入；设备 Token 不得出现在 unit 或进程 argv。
- `homepi-display config validate` 读取指定候选环境并执行与 `run` 相同的 URL、ID、证书指纹、
  Token 存在性、数据目录和主题校验，但不得初始化终端、建立网络连接或回显 Token。

## 10. 性能约束

- 稳态 RSS ≤120 MB。
- 空闲 CPU 平均 ≤5%。
- 默认重绘 ≤2 FPS；无变化时不因数据轮询重绘。
- 首屏最近成功快照加载 ≤3 秒。
- 快照变化到可见更新目标 ≤1 秒；SPI 硬件限制单独记录。
- 网络接收与渲染解耦，慢屏不得阻塞心跳/ACK。

## 11. 验收标准

当前 Phase 1 实机拓扑（2026-08-11）为本机 macOS `homepi-node` → 目标 Raspberry Pi
`homepi-display`；Pi 通过 `ssh dietpi` 管理，实际 SSH 用户为 `root`。部署前只读基线确认
DietPi/Debian 12、ARM64、`tty1=60×20`、HomePi unit/二进制/配置均不存在。

Kiosk unit 必须在 `getty@tty1.service` 完全停止后才启动 display。`Conflicts=` 负责互斥，显式
`After=getty@tty1.service` 负责顺序；否则首次 `enable --now` 或冷启动时 getty 的退出清屏可能
覆盖已经绘制的 HomePi 首帧，形成进程在线、快照已同步但物理屏全空白的竞态。

- 在目标屏幕上主值无截断、状态可辨、关键卡片 5 秒内可读。
- 无网络冷启动能显示最近成功快照和明确离线状态。
- 在内核日志无欠压告警的前提下，连续 24 小时运行无花屏、内存持续增长或崩溃。
- 在 stdin 关闭、无键盘和无触摸设备条件下可冷启动并连续运行。
- 随机键盘/触摸事件不会改变 Phase 1 页面、退出应用或写入配置。
- 终端从 16 色到 256 色时布局不变化，只改变样式。
- 损坏快照、未知字段和旧版本快照均不会使 TUI 退出。
- 数据目录中最多存在一个有效最近成功快照文件，不出现指标历史、SQLite 数据库或滚动样本。
- 原子写入若被强制停止打断，下一次启动必须清理同目录下 `last-known-good.json.tmp-*` 普通文件；
  不跟随或删除该前缀的符号链接，遇到符号链接时安全失败并保留外部目标。
- 配置包含多个活动远端 node 或收到非绑定来源快照时，不合并数据，并给出可诊断错误。
- 60×20 基线下任何渲染行不得超过 60 个终端单元；英文 Provider 名和数字禁止被边框截断。
