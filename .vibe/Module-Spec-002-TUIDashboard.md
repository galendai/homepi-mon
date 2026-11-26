# Module Spec 002：TUI Dashboard

> 模块 ID：MOD-002  
> 版本：1.7
> 状态：待本轮验收

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
同一 Provider 提供多个 balance 指标时，卡片主余额必须稳定选择 `order` 最小的指标；选择不得
依赖快照输入顺序，名称、金额、状态必须来自同一指标。Kimi API 的主余额因此固定为
`available_balance`，现金和代金券分项继续保留在快照中。

rich 配色固定为亮青外框；进度括号与剩余块 `█` 为亮青，已消耗块 `░` 为亮黄。进度配色不从
Card Status 派生，避免正常卡片变绿、告警卡片整条变黄/红而混淆“用量”和“状态”两个维度；
状态仍由独立徽标色与文字表达。

### 4.1 Compact 布局（目标首屏）

完整 60×20 首屏以 `UI-Spec-001-ASCII-Design.md` 第 4 节为准，本模块不维护第二份界面副本。
先生成安全的 60×20 ASCII 语义帧，再由 rich 装饰层按固定单元替换字形、添加白名单 SGR；因此输入清洗与布局只有一份实现。
首版按 60×20、8×16 字体设计；运行时终端尺寸是最终依据。卡片内容优先级：名称、5 小时窗口、每周窗口、重置、余额、状态、新鲜度；次要信息不足空间时隐藏而非截断主值。

仅有 weekly 窗口的配额卡片也必须使用与其他 Coding Plan 一致的两行布局：第一行显示 Provider、进度条、剩余百分比和完整的 `RESET` 文案；第二行显示 `5H --` 占位符，并在共享状态列显示 `OK`、`STALE`、`AUTH` 或 `ERROR` 等状态徽标。`5H --` 只表示该 Provider 没有 5 小时窗口，不是 0% 或真实指标，不进入快照和状态计算。

### 4.3 主题选择与安全边界

FIX-007（2026-10-10）：Codex Review 按独立卡片分组，不能覆盖主 Codex 的周余量/重置时间。紧凑 Kimi MONTH 列读取 `.monthly-total` 的剩余百分比，明确代表月总额度；只存在 `.monthly-code` 时该列保持 `--`，CODE 已用量仍在原详情布局独立展示。月总/编程已用详情、最严重状态与 AUTH 隐藏规则不改。

- `HOMEPI_DISPLAY_STYLE=rich|ascii`，默认 `rich`；也可用 `-style` 覆盖环境值。
- 未知值启动失败，防止配置拼写错误后静默改变现场效果。
- rich 控制序列由渲染器常量生成，只允许 SGR；外部文本经过既有 7-bit ASCII 清洗后才进入语义帧。
- 每行结束前恢复默认样式；隐藏光标、alternate screen、清屏与光标归位仍由 Kiosk 生命周期管理。
- 相同 ViewModel + 相同主题必须逐字节相同；主题变化不改变 60×20 可见单元。

### 4.2 页面

- `overview`：兼容旧版非轮播的总览；Coding 卡片统一使用两行布局，空间不足时优先完整保留 Coding 区，不为 weekly-only 卡片恢复一行特例。
- `coding`：订阅窗口详情。
- `api`：余额详情。
- `openclash`：运行时可选的 Mihomo 当前出口、节点延迟和连接状态；不加入持久五页配置。
- `homelab`：节点资源。
- `services`：服务/容器/告警。
- `system`：Pi、连接器和同步诊断。

Phase 1 只启用 `overview` 单页且不接受本地输入；Phase 2 只通过远端 Web Admin 配置现有 Kiosk，不改变页面模型；Phase 3 启用完整页面集合并自动轮播；Phase 4 接受远程指定页命令。

Phase 3 每页仍使用同一 60×20 安全网格：`coding` 只显示 Coding Plan/订阅配额，`api` 只显示余额/成本，
`homelab` 最多显示 4 个节点，`services` 显示 Prometheus/Grafana/Portainer 及容器摘要，`system`
显示 Pi 和连接器诊断。超过页面容量的实体按稳定顺序分页/轮播，不缩小字体；首个实现以有界摘要
和固定最大卡片数为门禁，子页按当前页面 dwell 周期轮换，CRIT 实体固定首屏；不在 Pi 上保存
指标历史。

## 5. 状态呈现

OpenCode Go 卡片（`opencode_go`）：5 小时窗口作为主进度条与重置时间；第二行显示 `5H 80% | WEEK 65% | MONTH 50% LEFT`，均为剩余额度。独立 monthly 指标不能覆盖主窗口，三个窗口均参与最严重状态与 CriticalPages 计算；AUTH 隐藏所有数值并提示更新 Go API Key。不改变 Kimi 月已用行。

Coding 卡片总高度超过标题下的 10 行时按完整卡片划分子页，标题显示 `CODING PLANS 1/2` 等页码；按 CODING dwell（默认 15 秒）轮换，告警期间仍轮换，避免隐藏其他订阅。适用于轮播 CODING 页和旧版 overview，ASCII/rich 使用同一子页选择，不截断卡片。仍沿用固定 Provider 顺序。

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
- Phase 3 由 `HOMEPI_PAGE_ORDER` 与 `HOMEPI_PAGE_DWELL_SECONDS` 定义页面顺序和每页 5–300 秒停留时间；五页必须恰好各出现一次。
- 进程启动始终先显示 `CODING`；随后从 `CODING` 在配置顺序中的位置继续轮播。
- `CRIT` 只参与状态、颜色和告警计数，不抢占页面、不冻结停留时间；一个或多个页面持续严重告警时仍按配置完整轮播五页。
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
  Token 存在性、数据目录、主题、页面顺序和停留时间校验，但不得初始化终端、建立网络连接或回显 Token。

## 10. 性能约束

- 稳态 RSS ≤120 MB。
- 空闲 CPU 平均 ≤5%。
- 默认重绘 ≤2 FPS；无变化时不因数据轮询重绘。
- 首屏最近成功快照加载 ≤3 秒。
- 快照变化到可见更新目标 ≤1 秒；SPI 硬件限制单独记录。
- 网络接收与渲染解耦，慢屏不得阻塞心跳/ACK。

## 11. 验收标准

- Phase 3 五页在 rich/ASCII 下均严格为 60×20，且无本地按键、触摸、菜单或退出提示。
- 自动轮播按配置顺序和停留时间推进；严重告警出现、持续或解除均不改变轮播位置与计时。
- HomeLab/Services 无数据、stale、认证失败、服务离线和不支持状态都以文字区分。

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
- weekly-only 卡片处于 stale、auth 或 error 状态时，第一行完整保留主值和重置文案，第二行完整保留 `5H --` 占位符和状态徽标，且不得互相粘连或截断；占位符不得被解释为真实 5 小时数据。

## 12. 紧凑总览离线验证入口（2026-10-10）

- 实现 UI-001 6.2 的候选纯函数与 `homepi-display preview --snapshot PATH`。
- `--layout compact|current` 比较紧凑总览与现有轮播 CODING 页；`--style rich|ascii` 选择主题。
- `--at RFC3339` 固定重放时间；默认当前时间，默认不假定在线连接。
- 预览只读取指定的脱敏快照并输出一帧，不读取设备/Provider 凭据、不初始化终端、网络或快照存储。
- 第一轮仅做离线候选验证，未改变正式 `run`；后续正式接入见第 13 节。
- 目标 ARM64 交叉构建与相同 8×16 字库像素重放可作为本地证据，不能代替实屏刷新、资源或人眼验收。

## 13. 紧凑总览正式接入（2026-10-10）

- 用户已选择紧凑布局，正式 `run` 的 screen 输出入口默认接入 UI-001 6.2 的总览。
- 标题栏下方恢复整行亮青分割线，ASCII 使用对应横线；固定 20 行内的数据区从 14 行调整为 13 行，PLAN 表头位于第 4 行。分割线也适用于空状态和离线状态，底部设备区与页脚位置不变。
- 本轮显示微调沿用 UI-001 6.2：订阅区与 API 区之间固定一空行并计入容量；每个订阅统一占一条额度行，Kimi 不追加 MONTH USED。rich 按各窗口余量独立给百分比着色，状态徽标独立。使用既有白名单 SGR，保留 ASCII 信息等价与重复帧抑制。
- 沿用五个页面 ID 和现有环境配置：逻辑 `CODING` 页显示 `OVERVIEW`；API、HOMELAB、SERVICES、SYSTEM 保留原详情布局。Router 决定启动、自动轮播与远程 show_page；CRIT 不参与路由仲裁。
- 无远程通知且数据完整放得下时，同页显示所有订阅窗口、API 主余额、实际 Pi health 和快照告警/新鲜度。Kimi 双月已用明细保留在原详情布局，MONTH 列继续显示月总剩余，月告警规则不变。
- 紧凑页页脚追加当前位置和 AUTO/HOLD 状态；只将显示用快照年龄按分钟取整，状态新鲜度仍按原精确时间推导。相同内容的计时 tick 不得产生重复终端写入。
- 超出 13 行或主值过长时，正式入口回退到原 CODING 分页详情，并在标题明示 `COMPACT FULL`；API 主余额仍可从 API 详情页查看。禁止将不完整数据伪装成完整总览。
- 远程通知沿用原布局，保留来源、严重度和恢复倒计时；通知结束后下一次绘制恢复总览。CRIT 优先规则不改。
- `preview --layout compact` 保持严格验证：超容量/过长主值仍返回错误；`--layout current` 保留三行卡片对照。
- 首轮接入范围为本地实现与验证；随后用户明确要求“立即部署”，授权安装当前 Display 二进制、重启服务及部署验证。保留旧二进制以便失败回滚；Git commit/push、用户肉眼验收和长期稳定性单独记录。

## 14. LVGL 像素首页扩展（2026-11-24）

- Linux+cgo 的 ARM64 Display 使用 LVGL 9.6 官方 fbdev 后端，直接输出 480×320 RGB565；不引入 X11、Wayland、SDL、浏览器或外部字体运行时。纯 Go/非 Linux 构建继续保留原终端 renderer。
- GUI 启动通过 `NewHomeRouter` 在原五页配置前插入 `HOME`，默认停留 30 秒；持久化 `HOMEPI_PAGE_ORDER` 和 dwell 配置不被修改。远程 `show_page HOME`、前后切页、临时轮播和消息覆盖保持有效。
- HOME 左栏宽 288 px，右栏宽 192 px。左栏显示 64×64 场景，每格占 4×4 屏幕像素，整幅为 256×256 px。每格左上 3×3 使用主色，右边和底边各留 1 px 同色系网格，亮度为主色的 80%。图像与右栏间留 16 px，图下不显示文字，栏间和上下状态栏靠留白分隔。
- 右栏的 176 px 文字区域显示时钟、日期、当前温度、天气状态和高低温。图形版内嵌 Montserrat Bold（700）的十个数字和冒号，以 58 px 绘制时钟。温度 24 px，天气状态 14 px，日期和高低温 12 px。天气来自快照中的可选 `WeatherSummary`，缺值显示 `UNAVAILABLE/--`，超过有效期时显示 `STALE`。长描述换行后高低温随之下移。
- 时钟上方优先呈现当前告警，按 CRIT、认证或采集错误、WARN 排序。同级沿用卡片顺序。额度说明取触发徽标的实际窗口，估算值带 `~`。告警解除后显示所选代理最近一次有效探测的毫秒数。失效的探测显示状态，等待快照或连接离线时收起这一行。
- 高低温下方使用 `WeatherSummary` 的湿度与风速，零值照常显示。缺项省略，天气过期、采集异常或剩余高度容不下整行时收起。
- 原 CODING/API/HOMELAB/SERVICES/SYSTEM 页在 GUI 中使用 Unscii 8 垂直整数两倍输出 8×16 字形，完整保留 60×20 ASCII 帧，不因为首页接入而空白或丢弃数据。远程通知优先于像素首页。
- 控件预建且只更新变化文本；显示模型和 legacy 帧均有重复帧抑制。天气显示年龄按分钟取整，但陈旧判断使用精确时间。
- ARM64 GUI 由 `scripts/build-display-gui.sh` 在固定 Go Docker 镜像中交叉构建，LVGL 固定 commit 并静态链接。启动时依次探测 `/dev/fb0`、`/dev/fb1`，选择实际可用的 480×320 RGB565 节点并记录 `framebuffer selected`；既有安装必须更新 systemd unit，使服务用户通过 `video` 组访问 framebuffer。
- `HOMEPI_HOME_ONLY=true` 通过 `Router.SetHomeOnly` 持续停留 HOME，旧环境缺省为 false。远程页面命令仍可临时覆盖；显式 set_rotation 命令可退出 home-only 并恢复轮播。

## 15. LVGL TOKEN PLAN / API BALANCE 卡片页（2026-11-25）

- `BuildTokenPlanModel` 仅处理 `ViewModel.Coding`，`BuildAPIUsageModel` 仅处理 `ViewModel.API`，共享页首/页脚但互不把另一组错误带入当前页。Token Plan 主窗口仍按 5H→WEEK→MONTH 选择非负可用值，不修改快照协议或 Provider 采集。
- `pixelScreen` 分别通过 `Renderer.DrawTokenPlan` 和 `DrawAPIUsage` 渲染 CODING/API。原生标题由页面类型固定为 `TOKEN PLAN` / `API BALANCE`（HOME 同理），不读取 NodeLabel，也不绘制第二行重复标题。Token Plan 的 1–4 个订阅使用 218×112 大卡、Montserrat 40 主值和进度条。5–6 个使用 218×72 紧凑卡、Montserrat 24 主值。两种卡片都把主窗口标签和另外两个窗口放在主值上方同一行，后两项靠右对齐。API Balance 最多 8 张 218×50 px 卡片，每卡单行显示服务商和主余额。币种与两位小数的金额间隔一个字符。正常数字为青绿色，低于 `¥ 20.00` 的人民币数字为报警红色。两页行、列间距均为 8 px，末卡底边分别为 280/272，无快照、ACTION、超容量或值过宽时仅当前页回退 `RenderKioskString`。
- API legacy 回退为每张余额保留可选 ACTION；PageAPI 无论是否暂停轮播都直接使用 ACTION-aware `apiBody`，需要时按 dwell 轮换 `API BALANCE n/m` 子页。每张余额与其 ACTION 不拆散，标题下最多 10 行，禁止因固定 60×20 帧丢弃尾部实体或处理提示。
- Go/C 桥接分别维护两组有界文本和 tone；C 预建两张 screen 及全部卡片，隐藏未使用卡片，每次只刷新当前 screen。其他原生/legacy 页面切换和远程通知优先级不变。
- `TokenPlanFrame` 与 `APIUsageFrame` 使用固定数组且可比较，独立执行重复帧抑制；快照显示年龄按分钟取整。
- ARM64 构建继续由 `scripts/build-display-gui.sh` 完成，启用 Montserrat 20。本地自动化和交叉构建不冒充物理 TFT 肉眼验收。

## 16. 固定单页运行（2026-11-25）

- `homepi-display run -page-only PAGE` 通过既有 `ParsePage` 接受 `HOME/CODING/API/OPENCLASH/HOMELAB/SERVICES/SYSTEM`，与 `-home-only` 互斥；空值保持原轮播行为。该运行参数不修改持久环境或页面顺序。
- `Router.SetPageOnly` 固定目标页并停止自动轮播；`SetHomeOnly` 保持为 HOME 的兼容包装。远程临时页面覆盖到期后返回固定页，显式 `set-rotation` 解除固定。OPENCLASH 锁页时基础集合只有一个条目，解锁后仍为 1/1。
- GUI 的固定 CODING 仍使用含 HOME 的六页路由，因此位置为 `2/6`；非 GUI 固定数据页不额外插入 HOME。远程命令和快照刷新保持有效。

## 17. 运行时页面子集（2026-11-25）

- `homepi-display run -pages HOME,CODING,API,OPENCLASH` 通过 `SelectRotationPages` 按给定顺序选择本次进程页面；HOME 使用固定 30 秒 dwell，CODING/API 保留五页配置中的 dwell，OPENCLASH 使用 15 秒 dwell。不得重复，与 `-home-only`、`-page-only` 互斥。
- `NewSubsetRouter` 以子集第一页启动并轮播，不自动插入 HOME。可选四屏示例页码为 `1/4` HOME、`2/4` CODING、`3/4` API、`4/4` OPENCLASH。页面子集不修改持久 `HOMEPI_PAGE_ORDER` 或 dwell；未配置子集或锁页时 GUI 使用 HOME 加五页，OPENCLASH 不加入持久五页 `allPages`。
- Token Plan 与 API Balance 的原生卡片渲染、远程覆盖和 legacy 回退在子集模式下保持相同语义。

环境文件可通过 `HOMEPI_ROTATION_PAGES` 或 `HOMEPI_PAGE_LOCK` 持久指定启动模式。显式页面模式参数作为一组覆盖环境模式，五页顺序、dwell 和终端 style 独立覆盖。同一来源中的子集、单页和启用的 HOME_ONLY 互斥。`config capabilities` 报告 `rotation-env-v1 gui` 或 `rotation-env-v1 terminal`。默认 OPENCLASH dwell 为 15 秒，临时 show_page 时子页使用基础页的渲染 dwell，临时轮播间隔也可能覆盖默认值。

## 18. OPENCLASH 页面

- 使用 `-pages` 时的可选四屏顺序为 `HOME,CODING,API,OPENCLASH`，OPENCLASH 固定停留 15 秒；未配置子集或锁页时 GUI 使用 HOME 加五页自动轮播，OPENCLASH 不加入持久五页 `allPages`。远程 `show_page OPENCLASH`、前后切页、固定单页和 Web Admin 页面选项使用同一页面 ID，远程覆盖到期与消息优先级沿用既有规则。
- 480×320 LVGL 原生页标题固定为 `OPENCLASH`；顶栏在标题与连接状态之间使用独立 `OPENCLASH n/m` 子页标签（Montserrat 12，x=240，宽126px）。取消 `PROXY > 当前出口`、连接数与测速年龄副标题栏，不再显示摘要行。当前 active 出口仅用更亮的 `#315464` 卡片背景和 `#7FB3D5` 边框区分。节点区为纯卡片风格：2×5、每张 218×42 px，列 x=16/242，行 y=48/96/144/192/240；每子页最多10张节点卡，19个节点分2个子页按15秒轮换，不得因当前出口冻结子页。
- 每张节点卡只显示一行“名称 + ping”，不再显示卡内状态行、连接数或 selected 标记；卡片仍为218×42。名称使用 Source Han Sans SC 14 CJK，ping 使用 Montserrat 12，按 LVGL 字体实际行高在42px卡内垂直居中。延迟小于300ms使用OK色调，300–800ms使用warning，大于800ms使用critical；无数值延迟时 ping 位置显示 `FAIL`、`STALE`、`AUTH` 或 `--`。认证失败抑制旧延迟；rate-limit 页脚显示 `DELAY`，可用测速保留；上游错误显示 `ERROR`，网络/时间超限显示 `STALE`。
- 协议最多64节点；OPENCLASH 不使用 `+N MORE` 伪称全部可见，而是按稳定名称排序切分为多个10卡子页并显式显示 `OPENCLASH n/m`。当前出口始终以亮色卡片背景标识，不要求每个子页重复该节点卡。
- 无原生渲染条件、字体缺字或值超宽时，仅 OPENCLASH 回退到 60×20 legacy：顶部子页标签加5行×2列、每半行29 cells 的单行“名称+ping”卡片，同样每子页10卡并显示 `OPENCLASH n/m`。快照超过新鲜度预算时保留最后成功值并显式标记 `STALE`；测速年龄继续来自节点 `checked_at`，不得把旧 history 当作实时探测。原生卡内名称使用14 CJK；C 桥接按实际 glyph 与140px标签宽度验证，缺失 glyph、稀疏 Han 未覆盖或超宽时返回错误并仅让本页回退 legacy。
- 页面只消费 schema 1.3 的可选 `openclash` 脱敏快照，不读取 secret、订阅 URL 或连接 metadata，也不提供节点切换、UCI 写入或服务重启控件。
