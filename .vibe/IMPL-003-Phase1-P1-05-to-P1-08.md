# 实现说明 003：Phase 1 P1-05 至 P1-08

> 文档 ID：IMPL-003
> 版本：0.6
> 日期：2026-08-11
> 对应任务：P1-05、P1-06、P1-07、P1-08
> 状态：软件交付与 Mac/Pi 实机完成，待用户检查；真实账号与暂缓平台为补充验证

## 1. 范围与成功条件

- 交付 DeepSeek API、Kimi API、MiniMax Token/Coding Plan、Codex Usage、Kimi Coding 五个连接器。
- 交付 Codex 登录态只读解析、字段白名单、错误分类和脱敏契约测试。
- 交付 60×20 无输入 Kiosk 的 DietPi systemd/autostart 配置。
- 修复 P1-04 遗留的 E2E 同步 flaky，执行 `make check`、race、六目标构建与安全扫描。
- 可自动执行的门禁结果全部写回测试文档；当前验收剖面的实机安装与 Pi 实屏已经真实执行。
  真实账号对账不得由 Mock 代替，保留为补充验证；供电长时程失败事实已获产品所有者例外接受。

## 2. 实现顺序

1. 共享只读 HTTP 传输与上游错误分类。
2. 三个官方连接器及其契约测试。
3. 两个兼容连接器、Codex 只读登录态与降级测试。
4. DietPi Kiosk unit、环境模板与静态验收测试。
5. 全仓门禁、故障矩阵、构建产物与文档回填。

## 3. 安全边界

- 连接器不重试，调度器是唯一重试所有者。
- 认证只通过 Bearer Header；错误和测试输出不记录 Key、Header 或原始响应。
- Codex 不执行 CLI/OAuth/refresh，不写 `auth.json`，不接受 Cookie 或第三方导出凭据。
- 设备 Token 只放入 Pi 的 `0600` 环境文件，不写入 systemd unit 与命令行。

## 4. 交付结构

```text
internal/connector/httpjson.go          共享只读 HTTP、1 MiB 上限、分类与脱敏
internal/connector/providerutil/        decimal、reset、区域、秘密引用与 auth_file 工具
internal/connector/deepseek/            DeepSeek 官方余额
internal/connector/kimiapi/             Kimi 官方余额
internal/connector/minimax/             MiniMax Token Plan + Coding Plan 单次兼容回退
internal/connector/codexusage/          Codex auth.json 只读 + wham/usage
internal/connector/kimicoding/          Kimi Coding /usages + 404 单次回退
internal/kioskunit/                     DietPi systemd unit 与 0600 环境模板
```

`cmd/homepi-node` 已导入并注册五个真实连接器；`mock` 不再注册占位实现。
`provider add` 对四个 Key 型连接器写 keyring，对 `codex_usage` 仅保存 `auth_file`，对 `mock`
不创建无用秘密引用。

## 5. 连接器行为

| 类型 | 默认 Base URL | 路径与回退 | 输出 |
|---|---|---|---|
| `deepseek_api` | `https://api.deepseek.com` | `/user/balance`，不回退 | 每币种 total/granted/topped-up 三个 exact 余额 |
| `kimi_api` | global=`api.moonshot.ai`，cn=`api.moonshot.cn` | `/v1/users/me/balance`，不回退 | available/voucher/cash 三个 exact 余额 |
| `minimax_coding` | Token Plan=`www.minimax.io`/`www.minimaxi.com` | 主路径 schema_changed 或 404 时只调用一次 `api.*` Coding Plan 兼容路径 | 5h 与可选 weekly exact 配额 |
| `codex_usage` | `https://chatgpt.com` | `/backend-api/wham/usage`，固定 HTTP/1.1、不回退；可选附加额度兼容当前 `additional_rate_limits` 与旧 `code_review_rate_limit` | 5h、weekly、可识别的可选 code review verified 配额 |
| `kimi_coding` | `https://api.kimi.com` | `/coding/v1/usages` 仅 404 时回退 `/usage` 一次 | 5h 与 weekly verified 配额 |

共享 HTTP 层只接受 JSON 成功响应，最多读取 1 MiB，拒绝跨 scheme/authority 重定向。
错误中不保留 URL 查询、响应体、认证头或原始网络错误。401/403、429、5xx、timeout、network、
schema_changed 分别进入既有 Scheduler 策略。

## 6. Codex 只读证明

- 缺省路径为当前用户 `~/.codex/auth.json`，也可用绝对 `auth_file` 或 `~/...`。
- `Lstat` 拒绝符号链接和非普通文件；打开前后使用 `SameFile`、size、mode、mtime 防止换档。
- 只解析 `tokens.access_token` 与 `tokens.account_id`；`refresh_token` 是未知字段，不进入结构体。
- 不导入 `os/exec`，不访问 OAuth/refresh/login 路径，不写文件。
- 契约测试记录采集前后 SHA-256、mode、size、mtime 完全一致，HTTP 服务器只收到一次 wham 请求。
- 当前 macOS/Go 1.26.5 对真实 Codex 上游实测默认 HTTP/2 进入 network error，HTTP/1.1 成功；
  因此仅 Codex transport 固定 HTTP/1.1，不增加应用层重试，不影响其余 Provider。

## 7. DietPi Kiosk

`homepi-display` 新增：

- `service-unit`：输出经测试锁定的 tty1 systemd unit。
- `environment-example`：输出 `/etc/homepi-display/environment` 模板。
- `run` 的 node URL、device ID、source node ID、证书指纹、设备 Token、数据目录均支持环境变量。
- 运行时终端小于 60×20 时显示按实际列行裁切的纯 ASCII 诊断页，不再输出越界主画面。

unit 使用 `StandardInput=null`、`TTYPath=/dev/tty1`、`RestartSec=10s`、5 次/300 秒启动限制、
`WantedBy=multi-user.target`。`Conflicts=getty@tty1.service` 与 `After=getty@tty1.service` 同时存在，
确保 getty 完成退出清屏后才绘制首帧。设备 Token 只存在权限 `0600` 的环境文件，不进入 unit 或 argv。

实机缺陷记录（2026-08-11）：首次 `enable --now` 后进程 active、快照已落盘，但 `/dev/vcs1`
全为空格；直接写 tty1 正常，停止并重启 Kiosk 后完整首屏出现。根因是原 unit 只有 `Conflicts=`
而没有顺序约束，getty 与 display 并行切换时退出清屏覆盖首帧。修复以 unit 契约测试锁定。

同轮强制停止在 `snapstore.Save` 的原子 rename 前留下 `last-known-good.json.tmp-*`。进程内
`defer os.Remove` 只能覆盖正常返回，无法覆盖 SIGKILL/断电；`snapstore.New` 因此须在单实例写入
开始前清理同前缀普通文件。若候选是符号链接则拒绝启动，不跟随链接，也不删除链接外目标。

修复复验：focused test 与 race 均通过；目标 Pi 从 active getty 启动 Kiosk 后 getty inactive、
display active，`/dev/vcs1` 60×20 首屏包含 661 个非空格字符。升级前存在 1 个中断临时文件，
新版启动后为 0；主快照继续同步，原始二进制保留为 `.previous`。

## 8. 自动化结果（2026-08-10）

| 门禁 | 实际结果 |
|---|---|
| `make check` | gofmt、go vet、全仓测试通过 |
| 测试计数 | 当前 193 个测试/子测试通过（新增 getty 顺序与中断临时文件恢复回归） |
| `go test -race -count=1 ./...` | 全仓通过 |
| `go test -race -count=10 ./internal/e2e` | 连续 10 轮通过 |
| E2E 同步定向复验 | 修复原子写入期间枚举目录的竞态后，race 连续 20 轮通过 |
| `make checksums` | 6 目标 × 2 二进制，共 12 个产物 |
| 清单回归 | 首轮发现 `SHA256SUMS` 自包含；修复后严格 12 条，十二项自校验均 OK |
| `go mod verify` | `all modules verified` |
| `govulncheck ./...` | `No vulnerabilities found.`（golang.org/x/vuln v1.6.0） |
| LICENSE 检查 | 10 个依赖模块均存在 ISC/MIT/BSD 类宽松许可证文件 |

构建版本：0.1.0，Go 1.26.5；darwin/arm64 的 node/display 版本信息均可执行。当前二进制中的
commit `c07ada9` 是工作树基线提交，仅用于本轮构建验证；未提交改动不存在可代表最终发布源码的
commit，因此用户确认并提交后必须重新构建发布产物。

本机真实 Codex 正常态冒烟：固定 HTTP/1.1 后 `provider test` 退出码 0，并解析 1 个指标；
请求前后 `auth.json` 的 SHA-256、mode、size、mtime、inode 完全一致。该结果证明当前登录态、
路径和响应主 schema 可用，但没有证明控制台数值一致，也没有覆盖登录过期与官方 CLI 续期恢复。

本机真实 macOS 服务生命周期：在事前确认默认配置目录和 LaunchAgent 均不存在后执行完整流程；
修复已停服务卸载的 `No process to signal.` 幂等处理后全部通过。最终确认 launchd job/plist 不存在，
并删除本轮生成的配置、证书、日志和临时二进制。
测试结束后 `bin/`、`dist/` 和临时参考仓库会清理，不把构建产物或外部样本留在工作树。

## 9. 仍需用户执行的外部验收

以下条件无法由本机 Mock 或契约服务器替代，因此当前不得宣称 Phase 1 已最终验收：

1. 当前验收剖面以本机 macOS daemon 完成 install/start/status/doctor，并与目标 Pi 建立真实链路；
   Windows 与额外 Linux daemon 按产品所有者 2026-08-11 指令暂缓，保留为补测项。
2. 五个真实账号逐项与官方控制台/CLI 对账；日志、快照和 Pi 数据目录执行脱敏检查。
3. Pi 安装 Kiosk，验证冷启动、tty1、无键盘/触摸、断网、AUTH、错误 Key、CLI 续期恢复。
4. Pi 连续运行 24 小时并记录 RSS、CPU、崩溃、花屏、日志量和 `vcgencmd get_throttled`。

2026-08-11 只读基线为 `get_throttled=0x50000`：当前欠压/节流位为 0，但历史发生位仍存在。
24 小时报告必须记录开始和结束原值；可判断观察窗口内是否出现新的当前告警，但不得写成设备
从未发生过欠压或节流。

实际前置检查（2026-08-11）：Pi reboot 后先读到 `0x50005`；本次启动累计 7 条
Undervoltage detected，收尾码为 `0xD0000`。低位为 0 表示收尾时无当前告警，但历史欠压、
节流和 soft temperature limit 位均已置位。这证明供电/限制告警仍会复现；本轮 24 小时测试
按前置失败提前终止；若未来补测，应在电源整改后从新的开始值重新计时。

产品所有者随后明确指示“供电问题请忽略，直接继续开发”。因此上述稳定性用例仍记录为未通过，
不可改写成无欠压；但其状态作为 accepted-by-owner 例外，不再阻塞 P1-08 软件交付完成。

## 10. 真实 Provider 配置与对账

先按 IMPL-002 完成 `config init`、TLS 与 `device add`。每个 Key 通过受控环境变量进入系统凭据库，
命令中不要粘贴真实 Key：

```sh
HOMEPI_PROVIDER_SECRET='在当前终端安全注入' ./bin/homepi-node provider add \
  -id minimax-main -type minimax_coding -account-label main -region global \
  -interval 60s -stale-after 5m

HOMEPI_PROVIDER_SECRET='在当前终端安全注入' ./bin/homepi-node provider add \
  -id deepseek-main -type deepseek_api -account-label main -region global \
  -interval 5m -stale-after 15m

HOMEPI_PROVIDER_SECRET='在当前终端安全注入' ./bin/homepi-node provider add \
  -id kimi-api-main -type kimi_api -account-label main -region cn \
  -interval 5m -stale-after 15m

HOMEPI_PROVIDER_SECRET='在当前终端安全注入' ./bin/homepi-node provider add \
  -id kimi-coding-main -type kimi_coding -account-label main -region global \
  -interval 5m -stale-after 15m

./bin/homepi-node provider add \
  -id codex-main -type codex_usage -account-label main -region global \
  -interval 5m -stale-after 15m
```

若 Codex 官方 CLI 登录态不在默认位置，追加 `-auth-file /绝对路径/auth.json`。逐个执行：

```sh
./bin/homepi-node provider list
./bin/homepi-node provider test -id minimax-main
./bin/homepi-node provider test -id deepseek-main
./bin/homepi-node provider test -id kimi-api-main
./bin/homepi-node provider test -id kimi-coding-main
./bin/homepi-node provider test -id codex-main
```

预期 metric 数量：MiniMax 1~2、DeepSeek 每币种 3、Kimi API 3、Kimi Coding 1~2、Codex 1~3。
真实输出必须与控制台同一观察时点对账；窗口、重置时间或余额语义不一致即验收失败。

## 11. DietPi 安装、回退与 24 小时检查

在 Pi 上执行，先把 ARMv7 二进制放入当前目录：

当前目标基线（2026-08-11）：`ssh dietpi` 实际以 `root` 登录 Debian 12/ARM64，`tty1` 为
60×20；HomePi 二进制、配置、unit 和数据目录均不存在。命令中的 `sudo` 对该 SSH 会话可省略；
仍须创建非特权 `homepi-display` 服务用户，设备 Token 只能经 stdin/0600 环境文件传递，不能
出现在 Agent 输出或远端进程参数中。

```sh
sudo useradd --system --home /var/lib/homepi-display \
  --create-home --shell /usr/sbin/nologin homepi-display
sudo install -m 0755 ./homepi-display /usr/local/bin/homepi-display
sudo mkdir -p /etc/homepi-display
/usr/local/bin/homepi-display environment-example | \
  sudo tee /etc/homepi-display/environment >/dev/null
/usr/local/bin/homepi-display service-unit | \
  sudo tee /etc/systemd/system/homepi-display.service >/dev/null
sudo chmod 0600 /etc/homepi-display/environment
sudoedit /etc/homepi-display/environment
sudo systemctl daemon-reload
sudo systemctl enable --now homepi-display.service
```

验收与诊断：

```sh
systemctl status homepi-display.service --no-pager
journalctl -u homepi-display.service --since '10 minutes ago' --no-pager
/usr/local/bin/homepi-display doctor
ps -eo user,pid,args | grep '[h]omepi-display run'
vcgencmd get_throttled
```

升级前把旧二进制复制为 `/usr/local/bin/homepi-display.previous`。回退：

```sh
sudo systemctl stop homepi-display.service
sudo install -m 0755 /usr/local/bin/homepi-display.previous \
  /usr/local/bin/homepi-display
sudo systemctl start homepi-display.service
```

24 小时后记录：`systemctl show` 的重启计数、`journalctl --since '24 hours ago'` 的错误/日志量、
RSS/CPU 样本、屏幕照片、`vcgencmd get_throttled` 开始/结束值。不要创建指标历史数据库；验收记录
只写测试结论与边界。

## 12. rich Linux console 视觉升级

当前 DietPi 实机探测为 `TERM=linux`、8 个基础色、UTF-8 mode、`C.UTF-8`、Fixed 8×16、
`tty1=60×20`。`homepi-display` 默认使用 `HOMEPI_DISPLAY_STYLE=rich`：蓝色线框、青色区块和
Provider、绿色正常、黄色告警/陈旧、红色严重、洋红认证/Kiosk，并用 `┌─┐│├┤└┘█░` 增强结构。
若目标字库或终端不支持，环境文件设置 `HOMEPI_DISPLAY_STYLE=ascii` 即恢复原 7-bit ASCII golden。
未知样式会拒绝启动，不静默降级。

2026-08-11 部署目标是 Debian 12/ARM64；目标产物 SHA-256 为
`e7485563937d542a04b4d35aeee21bf8525490a1de10133388b4b41e10ab63ce`。安装前旧版保存在
`/usr/local/bin/homepi-display.pre-rich`，原有 `.previous` 不覆盖。部署后 service active/running、
`NRestarts=0`、warning=0、RSS 10,828 KiB、CPU 短样本 0.3%。`/dev/vcsu1` 证明全部目标字形进入
Unicode 字符缓冲，RGB565 framebuffer 证据为 `.vibe/evidence/Phase1-Pi-Screen-Rich.png`。

同日第二轮配色把外框从深蓝调整为亮青，进度从状态派生色改为固定的亮青括号/剩余 `█` 与
亮黄已消耗 `░`；状态徽标仍独立使用绿/黄/红/洋红。目标 ARM64 SHA-256 为
`7310dc3f333c58219428faa7d7c84874ab0bfd4cdce47be474349e3b67b5039b`，升级前版本保存在
`/usr/local/bin/homepi-display.pre-palette`。部署后 service active、`NRestarts=0`、warning=0；
新 framebuffer 证据为 `.vibe/evidence/Phase1-Pi-Screen-Rich-Bright.png`。

## 13. 金额输出精度统一

2026-08-12 将 `balance` 与 `cost` 的对外格式收敛到 `ProviderMetric` JSON 边界：
`value` 和金额 `limit` 固定输出两位小数，不足补零，超出使用 decimal 的
half away from zero 四舍五入。TUI 金额文本在 ViewModel 边界使用同一规则。Connector
采集值、状态阈值比较与非金额指标仍保留原始 exact decimal 精度。

自动化覆盖整数补零、三位小数四舍五入、金额 limit、quota 精度隔离、内存值不变与
TUI 文本；全仓普通测试、race、vet 和 diff check 通过。
