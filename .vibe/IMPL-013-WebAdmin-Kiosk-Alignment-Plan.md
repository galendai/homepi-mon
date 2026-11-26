# IMPL-013 Web Admin 与 Kiosk 页面配置对齐

日期 2026-11-26。代码基线 `52b3532`。本地实现与隔离验收已完成，DietPi 升级、屏幕观察和现场回滚待单独授权。

## 1. 目标与实机基线

操作者可以在 Display 表单保存、测试和应用轮播或单页启动配置，区分磁盘配置、启动时实际读取的设置、临时远程命令。保留五个持久数据页及其停留时间，支持 GUI、终端和既有配置文件。

2026-11-26 只读核实 DietPi。服务 active/running，NRestarts=0，systemd 和进程均为 `-pages HOME,CODING,API,OPENCLASH`。加载了 `home-only.conf` 和 `split-card-pages.conf`。环境文件中没有 HOME_ONLY，前一份 drop-in 设 true，后一份设 false，进程实际为 false。按启动设置推算的停留时间依次为 30、30、30、15 秒。`progress.md` 的 `zz-openclash-debug.conf` 是此前调试记录。旧 Display 的校验器接受七键配置，拒绝新增键，包括空值。

## 2. 配置契约

- 在原十键 EnvironmentFile 上增加可选的 `HOMEPI_ROTATION_PAGES`、`HOMEPI_PAGE_LOCK`。未使用的新键在写出时省略，继续兼容旧校验器。HOME 锁定仍可用旧键 `HOMEPI_HOME_ONLY=true` 表达。新模式在 Test 时检查已安装 Display 的能力，旧版本给出升级提示。
- 显式的 `-pages`、`-page-only`、`-home-only` 一起决定启动模式，覆盖环境中的整组模式设置。同一来源的子集、单页和启用的 home-only 互斥；重复页面、空成员、未知页面均报错。单独的 `-home-only=false` 可显式恢复默认模式。页序、五页 dwell 和 style 各自沿用 CLI 优先级。
- 默认 GUI 在五个数据页前插入 HOME，默认终端使用五个数据页。自定义子集按给定顺序启动，普通数据页沿用各自 dwell。HOME 和 OPENCLASH 默认分别 30 秒和 15 秒。单页锁定后，临时 show_page 到期返回锁定页。沿用既有行为，OPENCLASH 单页解锁后的基础序列只有一页。临时命令可能改变停留时间或渲染所用 dwell。
- 启动预览说明配置来源、顺序和常规 dwell。当前屏幕、临时覆盖剩余时间和在线状态需要独立运行时证据。
- Test 绑定草稿版本、候选环境内容和远端原文件。另一标签页保存草稿、凭据变更或 Pi 文件被改写之后需要重新核对并测试。回滚时校验本次候选和原备份的完整内容。

## 3. 实施与验收

| 工作 | 改动与验收 |
|---|---|
| 观测 | SSH 失败显示无法核实并停止比较。环境缺省值按 Display 契约比较。读取运行进程允许公开的页面参数与环境字段，识别现有 unit 配置的启动覆盖。分别显示磁盘值、进程启动设置、服务状态和快照时间。进程不可读时保留未知状态。 |
| 配置 | 环境解析与生成、CLI、profile 加载与保存、候选比较以及 SSH Test/Apply 覆盖默认、子集、锁页和旧 HOME_ONLY。保留原 schema 版本和已保存 profile。 |
| 操作界面 | 默认、子集、锁页三种模式，共享可选页面目录。表单改动立即使已测试的候选失效。验证 Save draft → Test SSH → Apply，以及测试后再次编辑、失败重试和已有启动参数覆盖的提示。 |
| 事务结果 | 核对环境、服务状态、新快照和启动设置。现有命令行覆盖时明确指出已保存的环境及覆盖来源。回滚操作和恢复检查成功后才能返回 rolled_back，无法确认恢复时标明需人工核实。 |
| 自动化与文档 | 针对缺省、冲突、不同版本、SSH 失败、恢复失败和草稿交互补回归。同步 Display 及 Web Admin 模块规格、示例与双语使用说明。运行聚焦测试、race、make check 和隔离浏览器验证。 |

真实 Pi 的验收分两次。保留四屏启动参数时只核对覆盖提示。经操作者单独安排迁移后，在无页面启动参数的目标上应用明显不同的 `API,HOME,OPENCLASH`，记录重启后的三页顺序、页码及恢复旧环境的结果。当前 Pi 可用于只读探测。ARM64 GUI 构建与硬件部署分别记录。

## 4. 执行记录

| 核实对象 | 2026-11-26 结果 |
|---|---|
| 现有 DietPi，只读 | 最终版探针读到服务 active、重启计数 0、快照存在、两个已加载的 drop-in 和四页 `-pages`。进程 HOME_ONLY 为 false，已安装程序尚未声明 `rotation-env-v1`。 |
| Web Admin 操作 | 在 1280×900 和 390×844 的 Chrome 中，用真实 Web Admin、部署管理器和隔离 SSH 替身验证保存、测试、应用、刷新回读。覆盖测试后编辑、启动参数覆盖、三页轮播、OPENCLASH 锁页、旧版 HOME 锁页、旧版拒绝新模式、SSH 中断和恢复失败。 |
| 实际轮播 | 在断网 ARM64 Linux 容器用仓库 mock 数据启动 Node 与终端 Display。仅设置 `HOMEPI_ROTATION_PAGES=API,HOME,OPENCLASH` 选择页面。记录到 API 1/3（15 秒）、HOME 2/3（30 秒）、OPENCLASH 3/3（15 秒），第 60 秒回到 API 1/3，Display 保存了新快照。 |
| ARM64 GUI 和文件事务 | GUI 构建通过，运行 `config capabilities` 得到 `rotation-env-v1 gui`。容器内执行实际部署脚本，使用 systemctl 替身验证七键旧配置、候选校验、拒绝过期写入、原子替换、重启失败和逐字节恢复。GUI 构建标记 `52b3532-impl013-local`，SHA-256 为 `dac488599a7c4a336bcf7581c7aa1e5fd0f9f716eaba4c2c65bcd0faff5dc567`。 |
| 仓库检查 | 六个相关 Go 包使用 `-count=1` 重跑通过。`go test -race -count=1 ./internal/displaydeploy ./internal/webadmin`、`make check`、JavaScript 语法检查和 `git diff --check` 均通过。 |

使用反馈后的界面复验覆盖桌面 1440×1000、手机 390×844 和键盘操作。打开 Display 即可查看 Pi 启动时的屏幕顺序。用户可以把现场四屏复制到草稿，直接编辑 Home、Token Plan、API Usage、OpenClash 的选择顺序。隔离目标上核实了增删排序、可调与固定时长、拒绝空轮播、旧版锁定 Home、保存后刷新回读和 SSH 离线状态。SSH、目录和终端样式已收进连接设置。

版本提示条已增加 **Update service**。在隔离 Linux 服务上通过浏览器点击更新，模拟启动失败后确认旧构建 `262fcd2` 恢复健康，再次点击后确认新进程运行页面所示 Admin 构建。两次操作均保留了未保存的草稿。macOS 现场只读核实了按钮可用、已登记服务的目标目录可写，以及原进程通过本机 HTTPS 健康检查。

随后操作者点击更新。macOS 本机重新核验时，Admin 和健康的 Node 进程均报告 `52b3532-runtime-update-local`，之前的服务构建为 `fcea749`。

针对同步反馈，隔离浏览器验收覆盖桌面和 390px 手机宽度。点击后立即显示 **Syncing…**，重启中刷新第二个标签页也显示相同状态。模拟丢失更新响应及一次状态查询后，页面通过只读查询恢复并原位显示 **✓ Synced**。恢复旧版后可重试，持续无法确认时 **Check status** 只查询结果。

Pi 现场验收仍需升级 GUI Display，迁移现有四页启动参数，在物理屏幕上核对三页顺序与页码，再演练现场恢复。

## 5. 后续独立工作

Provider 类型目录、OpenClash selector 表单与单实例提示可独立交付。实时 Now showing 涉及 Display 上报、Node 按设备保存与过期、Admin 读取和离线呈现，单独排期。本轮用启动配置预览满足轮播配置验收。
