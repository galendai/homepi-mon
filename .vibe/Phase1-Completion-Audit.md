# Phase 1 完成性审计

> 日期：2026-08-11
> 范围：Development-Plan Phase 1（P1-01 至 P1-08）
> 结论：开发、完整自动化与 Mac/Pi 实机功能完成；供电风险已获产品所有者接受，等待用户最终检查与批准

## 1. 判定口径

- 「已证明」：当前工作树、测试输出、构建产物或已批准实机记录直接覆盖要求。
- 「部分证明」：实现和自动化存在，但要求中的真实账号、目标操作系统或 Pi 实机范围未覆盖。
- 「缺少证据」：必须在外部设备或账号执行，当前没有可核验输出。
- 契约服务器、Mock、交叉编译不得替代真实服务、真实账号或目标硬件验收。

## 2. 任务证据矩阵

| 任务 | 判定 | 已有直接证据 | 尚缺证据 |
|---|---|---|---|
| P1-01 工程与硬件基线 | 已证明并已批准 | 12 目标构建、版本输出、Pi 型号/TTY/屏幕/系统记录、TLS 配对方案 | 收尾 `0xD0000` 含历史欠压、节流与 soft temperature limit，稳定性被阻断 |
| P1-02 协议与领域模型 | 已证明并已批准 | 协议往返、版本/epoch、单 node、状态优先级、脱敏审计测试 | 无 |
| P1-03 Mock 垂直切片 | 已证明并已批准 | 60×20 golden、断网/重连、最近成功快照、无历史、Mock E2E | 无 |
| P1-04 跨平台 daemon | 当前验收剖面已证明 | 配置、安全、TLS、撤销、三平台实现与交叉构建；macOS 真实生命周期与 Mac→Pi 链路 | Windows/额外 Linux daemon 已由产品所有者明确暂缓，不记作实机通过 |
| P1-05 官方连接器 | 部分证明 | DeepSeek/Kimi API/MiniMax 契约、错误分类、decimal 与回退测试 | 三个真实账号同观察时点控制台对账与错误 Key 验证 |
| P1-06 兼容连接器 | 部分证明 | Codex/Kimi Coding 契约；Codex 真实正常态 HTTP/1.1 请求成功且 auth 文件不变 | Codex 控制台数值对账、过期/官方 CLI 续期恢复；Kimi Coding 真实账号 |
| P1-07 TUI/Kiosk | 当前验收剖面已证明 | 60×20 rich/ASCII 双主题、彩色 framebuffer、无输入 Kiosk、getty 顺序、systemd、自启、SIGKILL 退避、断线恢复、快照清理；真实截图 | 用户肉眼确认物理屏；供电长时程限制已获例外接受 |
| P1-08 发布门禁 | 软件交付完成，待批准 | 完整自动化/race、12 产物 SHA、安全扫描、Mac/Pi 故障矩阵、回退、重启自启；供电证据已披露并获例外接受 | 四个真实账号对账与暂缓平台实机仍未执行，不得写成已验证 |

## 3. 当前主机新增真实证据

环境：macOS 26.5.1（Darwin 25.5.0 arm64）、Go 1.26.5、Codex CLI 0.144.5。

Codex 正常态执行结果：

- 官方 CLI 登录态文件是 `0600` 普通文件，不是符号链接。
- 默认 Go HTTP/2 请求稳定归类为 `network`；相同登录态下 HTTP/1.1 请求成功。
- 实现仅对 Codex transport 固定 HTTP/1.1，没有增加连接器应用层重试。
- 修复后 `provider test` 退出码为 0，解析 1 个真实指标。
- 请求前后登录态文件 SHA-256、mode、size、mtime、inode 完全一致。
- 命令与记录没有输出 access token、refresh token 或 account ID。

证明边界：该结果证明当前登录态、请求路径、HTTP 协议和主响应 schema 可用，不证明指标与
Codex 控制台数值一致，也不覆盖过期登录态和官方 CLI 续期恢复。

Mac/Pi 真实链路结果：

- macOS LaunchAgent、Keychain、自动 TLS、设备 Token 与 Pi systemd 链路均 active。
- 无 Token、错误 Token、未知设备均返回 401；两端配置、日志和 Pi 快照的真实 Token 扫描 clean。
- Mac 停止后 Pi 1 秒 OFFLINE 且快照哈希不变；Mac 启动后 3 秒 LIVE。
- display SIGKILL 后 11 秒重启，回退到 `.previous` 与恢复当前版均保持 LIVE。
- Pi reboot 后 boot ID 改变，display 自动启动、getty inactive、最近快照加载、screen LIVE。
- 修复了 getty 退出清屏覆盖首帧和 SIGKILL 遗留原子临时快照两个实机缺陷。
- ASCII framebuffer 证据：`.vibe/evidence/Phase1-Pi-Screen.png`。
- rich framebuffer 证据：`.vibe/evidence/Phase1-Pi-Screen-Rich.png`；蓝/青/洋红/绿/白色彩与
  `┌─┐│├┤└┘█░` 均在当前 DietPi Fixed 8×16 console 正常显示。
- bright-border framebuffer 证据：`.vibe/evidence/Phase1-Pi-Screen-Rich-Bright.png`；外框升级为
  亮青，剩余 `█` 为亮青、已消耗 `░` 为亮黄，状态徽标继续使用独立语义色。

硬件证据：重启后曾读到 `get_throttled=0x50005`，本次启动累计 7 条欠压检测；收尾为
`0xD0000`，当前位虽为 0，但历史欠压、节流和 soft temperature limit 位均已置位。稳定电源
前置条件已经失败，24 小时门禁不得开始或写成通过。

产品决策：产品所有者于 2026-08-11 明确要求忽略供电问题并继续开发。审计继续保留全部失败
证据，不把稳定性写成技术通过；该项改记 accepted-by-owner，不再阻塞 Phase 1 软件交付状态。

## 4. 最小剩余验收集

1. 按 IMPL-003 §10 完成 MiniMax、DeepSeek、Kimi API、Kimi Coding、Codex 控制台对账；
   Codex 正常请求无需重复，但必须补数值对账、过期和官方 CLI 续期恢复。
2. 用户肉眼核对真实屏与保存的 framebuffer。
3. 真实账号对账、Windows/额外 Linux 实机和稳定供电长时程均作为补充验证保留；未执行项不得
   写成通过，其中供电长时程已由产品所有者明确接受例外。
4. 用户检查证据并明确批准后，才可将 P1-04 至 P1-08 的 `verification` 改为 `approved`，
   并在用户明确要求后执行 Git commit。

## 5. 当前外部条件

- 当前主机没有 PowerShell/Windows 运行环境，也不是 Linux；产品所有者已明确要求当前暂缓
  Windows，并使用本机 Mac 作为唯一 daemon 测试主机。
- HomePi 当前已部署：Mac LaunchAgent 与 Pi systemd 均 active，Mac 配置含一个无秘密 Mock
  Provider 和一个设备；环境中未设置 MiniMax、DeepSeek、Kimi API/Kimi Coding 测试密钥，
  未读取或枚举用户 Keychain，因此四个账号不能在本轮安全代填。
- 目标 Pi 可通过 `ssh dietpi` 访问，SSH 配置实际以 `root` 登录；当前 Debian 12/ARM64、
  `tty1=60×20`、display enabled/active、getty inactive、screen LIVE。
- 部署后重启复现间歇性当前欠压/节流；产品所有者已明确接受该供电例外。
- Mac 用户级二进制、配置、证书、日志与 LaunchAgent 是当前有效部署，不作为测试遗留清除；
  仓库 `bin/`、`dist/` 和两端临时测试目录均已清理。
