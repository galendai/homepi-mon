# Phase 2 完成执行记录

> 日期：2026-08-12
> 范围：P2-01 至 P2-04，macOS + `ssh dietpi` 实机
> 状态：代码与 macOS + DietPi 验收完成，等待用户检查

## 1. 成功标准

1. P2-01/P2-02：本机 Web Admin 真实启动，只监听 loopback；Provider 草稿测试与 Apply 走正式
   LaunchAgent；版本、Keychain、配置事务和浏览器安全边界通过。
2. P2-03：Display profile 不含明文 Token；`homepi-display config validate` 可独立校验候选环境；
   Web Admin 只能对固定文件和固定 unit 执行读取、候选写入、原子替换、重启、健康确认和回滚。
3. P2-04：自动化、故障注入、秘密扫描、端口检查和 Mac→Pi rich/ASCII 实机往返通过；失败路径
   保留或恢复上一份有效环境，Pi 无新增入站管理端口。
4. 全仓普通测试、race、vet、前端语法、格式、交叉构建和 diff 检查通过；Windows/Linux 原生服务
   生命周期继续明确标记为未执行。

## 2. 执行顺序

1. 记录 Mac/Pi 只读基线和版本差异。
2. 先更新高层规格、模块规格和测试矩阵，再实现 Display 配置事务。
3. 运行单元/集成故障注入后构建并部署候选二进制。
4. 通过 Web Admin 公共 HTTP 路径执行 rich→ascii→rich，并检查快照前进、unit 稳定和回滚证据。
5. 执行安全、秘密、端口和全仓门禁；仅在所有必需检查通过后更新开发计划。

## 3. 安全约束

- SSH host 仅允许安全别名/主机名；不得以 `-` 开头，不接受空白、控制字符、Shell 元字符或路径。
- 远端命令由程序内固定脚本定义；路径固定为 `/etc/homepi-display/environment`、
  `/etc/homepi-display/environment.previous` 和 `homepi-display.service`，用户值只经 stdin 传输。
- 候选环境只允许七个 `HOMEPI_*` 键，值拒绝换行、NUL 和 Shell 展开语法；正式文件必须是
  root:root、`0600` 的普通文件且不得为符号链接。
- Display profile 仅保存 SSH host、device ID、node URL、style、data dir 和设备 Token 引用；API
  不返回完整引用，更不返回 Token 明文。
- 实机检查只输出环境键名、文件元数据和快照元数据，不输出环境值、Provider Key 或原始响应。

## 4. 基线结果

- Mac：`homepi-node 0.1.0`，正式 LaunchAgent 正常运行，配置校验为 5 Providers / 1 Device，
  secret backend 为 Keychain。
- Pi：`homepi-display 0.1.0`（commit `c07ada9`），`homepi-display.service` active/enabled，
  `NRestarts=0`；环境文件为 root:root `0600`，最近成功快照存在且由 display 用户以 `0600` 保存。
- 差距：Pi 二进制尚无 `config validate`；Web Admin Display 页仍为只读占位，P2-03/P2-04 尚未实现。

## 5. 结果记录

### 5.1 P2-01/P2-02

- 正式 Web Admin 与 LaunchAgent 构建均为 `0.1.0/a90a4c7`，页面显示 Synced。
- 4 个启用的真实 Provider 只读测试全部成功：Codex 1、DeepSeek 3、Kimi 3、MiniMax 1 项指标。
- 浏览器停用 MiniMax 并 Apply，配置 revision 由 `a9e24091dacd` 更新为 `a30d0cad050d`；页面显示
  validating/persisting/restarting/verifying/completed。随后重新启用、只读测试并 Apply，revision
  恢复为等价配置 `a9e24091dacd`；正式 LaunchAgent 始终通过健康检查。
- 服务实例重启后，旧页面 CSRF token 的修改请求返回 403；刷新取得新 token 后才可继续。
- 临时停止 Pi unit 后再次执行 Provider Apply：node 返回 healthy=true、display_sync=pending；恢复
  Pi 后获取新 source epoch。重新启用并测试 MiniMax 后 Apply 仍成功，最终配置保持 4 个启用 Provider。

### 5.2 P2-03

- Pi 升级为含 `config validate` 的 linux/arm64 构建；旧正式环境未因二进制升级改变。
- Web Admin 公共页面完成 `rich → ascii → rich`：每次先 Save draft、Test SSH，再 Apply；ASCII
  应用后页面报告 style=ascii、snapshot version 7→9，恢复 rich 后 snapshot version 9→15。
- 两次正式环境及单份 `.previous` 均为 root:root `0600` 普通文件，unit active、`NRestarts=0`。
- 将候选 node URL 指向 Pi 不可达端口后，静态 Test 通过、Apply 重启后快照不前进，25 秒健康
  窗口到期并自动恢复上一份 rich 环境；恢复后 unit active、快照继续前进、无候选残留。
- SSH host 改为不可解析别名时 Test 安全失败，Apply 保持禁用，Pi 文件和服务未改变。

### 5.3 P2-04

- Host spoof、Origin、CSRF、API query、未知 JSON 字段、17 层 JSON、64 KiB body 和认证后请求
  洪泛均有拒绝门禁；所有响应带 CSP/nosniff/no-referrer/no-store。
- 本机 Keychain 实值对仓库、配置、profile 和日志扫描为 0 命中；profile 不含 `device_token`；
  Pi 快照/日志设备 Token 命中为 0，敏感 JSON key 命中为 0。
- Web Admin 验收结束后 8765 listener 数为 0；Pi TCP listener 仅为 22，未新增管理端口。
- `make check`、定向 race、前端语法、diff check、六平台 12 个发布产物及 checksum 验证全部通过。

### 5.4 CLI 救援路径

Web Admin 不可用时可使用以下既有入口；命令均不打印秘密：

```sh
homepi-node config validate
homepi-node status
homepi-node doctor
homepi-node provider list
homepi-node provider test -id PROVIDER_ID
ssh dietpi 'homepi-display config validate --file /etc/homepi-display/environment'
ssh dietpi 'systemctl status homepi-display.service --no-pager'
```

如 Display Apply 报告 `manual_recovery_required`，先检查正式环境与 `.previous` 都是 root:root
`0600` 普通文件，再复制 `.previous` 到固定候选路径、执行 `config validate`、原子替换并重启固定
unit；不得降低 host key 校验、改用任意远端路径或把 Token 放入 argv。

### 5.5 明确保留的验收边界

- Windows/Linux daemon 原生服务生命周期按产品所有者决定暂缓；仅交叉构建通过。
- 浏览器和远端状态证明 ASCII/rich 配置、进程及快照真实生效，但没有摄像头证据，物理 LCD 的
  最终视觉观感仍由用户检查。
- 1 小时空闲与 20 次实机主题压力未逐字面长时间执行；本轮覆盖加速 idle 生命周期、两次主题
  往返、一次重启后健康回滚及一次 SSH 前置失败，长期压力保留为补充可靠性观察，不阻塞代码门禁。

### 5.6 FIX-003 Display 自动重连追加门禁

- 现场失败基线：Mac node 短暂停止时，Pi display 收到 EOF；下一次拨号失败的错误文本包含 URL，
  旧脱敏器反复扫描自己生成的 `://[redacted]` 并进入无限忙循环。旧进程约 20 分钟无新 TCP
  连接，累计约 22 分 48 秒 CPU，必须手动重启才恢复。
- 自动化：新增 U031 验证多个 URL 在 250 ms 内完成脱敏且 authority 不泄漏；新增 E016 启动
  display 时先制造真实连接失败，再启动 daemon，确认同一进程经退避获得新 epoch 快照。
- 门禁：`make check`、`go test -race -count=1 ./...`、`gofmt -l cmd internal`、
  `git diff --check` 和 Linux ARMv7 交叉构建全部通过。
- 实机：部署 `a90a4c7-fix003` ARM64 候选；22:25:59 停止 Mac node 后 Pi 收到 EOF，22:26:01
  记录已脱敏的 `connection refused`，节点恢复后于 22:26:07 写入新 epoch。display PID 始终为
  6428、`NRestarts=0`、CPU 0.5%，无需重启 Kiosk。
- 回退：旧 Pi 二进制保留为 `/usr/local/bin/homepi-display.pre-fix003`；临时候选文件已清理。
