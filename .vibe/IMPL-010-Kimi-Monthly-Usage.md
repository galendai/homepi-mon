# IMPL-010 Kimi 进度条下显示月使用量

- 日期：2026-10-07
- 状态：DONE（实现、自动化、真实账号及正式 node→Pi TTY 部署验收通过）
- 范围：Kimi 当前 ratio 契约、月总额度/月编程额度指标、同一卡片显示及状态判断。

## 可验证完成条件

1. 当前 `usages` 支持 5h、7d、monthly total/code，已用比例 0–1 转为独立剩余额度指标；当前窗口优先，旧 count 契约仍兼容，缺失窗口不伪造。
2. Kimi 进度条下一行显示 `MONTH USED: TOTAL 90.5% | CODE 90.3%`，一位小数来自 exact decimal；5H/WEEK 行保留原语义。月数据不能覆盖主进度条。
3. 月额度参与现有阈值、过期和告警；AUTH 优先并隐藏缓存数值。四张 Coding 卡片、后续卡片与 API 样式在 ASCII/Rich 中保持 60×20。
4. 聚焦测试、`make check`、Mac node/display 和实际 DietPi ARM64 候选构建通过。真实采集与正式服务部署分开记录。

## 实现与验证

- 新指标 ID 为 `<provider-id>.monthly-total/monthly-code`；协议已有 monthly 窗口，使用 value=remaining ratio、limit=1，未增加 wire schema。
- 新增契约测试覆盖当前字段优先、旧字段补缺、0/1、数字字符串、缺失/非法比例、双月额度的独立指标与 reset。元数据注册新增 ID，供订阅和错误状态使用。
- U045 覆盖单个月字段、一位小数准确取整、月数据不覆盖 5h、月 CRIT/STALE、AUTH 隐藏数据、四卡片 ASCII/Rich 布局、后续进度条和 API 样式。STALE 沿用既有非告警计数语义。
- `go test -count=1 ./internal/connector/kimicoding ./internal/ui ./internal/webadmin` 通过；`make check`（gofmt、go vet、全仓 go test）通过。`git diff --check` 通过。
- SSH 只读确认 DietPi 为 aarch64。构建 Mac node/display 与 Linux ARM64 Display，版本标签 `a0def2e-kimi-monthly` 表示基线加未提交工作区变更。
- 临时只读诊断程序直接通过 Keychain Get 读取既有引用并调用正常连接器，未 Set/Delete 凭据；Key 不进入 argv、文件、仓库或输出。临时源代码和诊断 binary 已清理，保留可审阅显示预览。

## 真实账号只读预览

观察时刻：2026-10-07 15:43:13 Asia/Shanghai。

| 指标 | 剩余比例 | 显示 | 上游重置时间 UTC |
|---|---|---|---|
| 5h | 1/1 | 100% LEFT | 2026-10-07T11:25:28Z |
| monthly-total | 0.0953/1 | TOTAL 90.5% USED | 2026-10-18T02:25:29Z |
| monthly-code | 0.0971/1 | CODE 90.3% USED | 2026-10-18T02:25:29Z |

```text
Kimi Coding [################] 100% LEFT     RESET 4H
  MONTH USED: TOTAL 90.5% | CODE 90.3%
  5H 100% LEFT                                 CRIT
```

CRIT 来自月剩余低于默认 10% 阈值，不代表 5h 已耗尽；本账号没有返回 weekly。原始 ratio 与 [官方 Kimi Code 解析契约](https://github.com/MoonshotAI/kimi-code/blob/main/packages/oauth/src/managed-usage.ts) 一致。官方控制台人工对账和物理屏肉眼检查为 NOT RUN；后续正式 Pi TTY 验收见下。

## 部署边界

最终候选（上述全仓检查之后重新构建）：

| 产物 | SHA-256 |
|---|---|
| `bin/homepi-node`（darwin/arm64） | `2e90e05cbd7f6eb504ca7f59198c0cd3f7894d1fdb5947231c72ad983d8d1248` |
| `bin/homepi-display-linux-arm64-kimi-monthly` | `1b469a8eb06355bcd09ad3d163a86095b2d77a9d4ec6af3147149b412eeb470d` |

Mac node/display 的 build time=2026-10-07T07:45:23Z；完整本地预览保存于 `tmp/kimi-monthly-preview.txt`。

用户于本轮明确要求“部署更新”，授权安装两个候选并重启服务。先更新 Display，再更新 node，避免旧 Display 把 monthly 当作主窗口。部署前后未修改代码，未提交或推送。

## 正式部署验收（2026-10-07 16:20–16:24 Asia/Shanghai）

- DietPi 缺少 SFTP 和 SCP 程序，直接通过既有 SSH 标准输入上传候选；不安装传输工具、不修改 SSH 配置。校验 SHA-256 后保留旧 binary、原子替换并重启原 unit，环境配置与令牌不变。
- Mac 使用 `scripts/install-local.sh --skip-build` 安装已验证的 node/display，重新注册原 LaunchAgent。额外保留旧 node/display 和 plist；配置文件前后 SHA-256 都为 `200fc11443fe43e1202ded402250e428b81719a789ef50ab44e874992d54f098`。
- 正式 Mac node SHA-256 与候选完全相同；LaunchAgent PID=25250、state=running、runs=1、last exit=never exited。已安装 binary 的 `provider test -id kimi-coding` 通过，3 metrics / 1.145 秒。
- DietPi `homepi-display.service` active/running、PID=1011、NRestarts=0、ExecMainStatus=0。`/proc/1011/exe` SHA-256 与 ARM64 候选完全相同；版本 `a0def2e-kimi-monthly`。ARM64 候选的 built 字段未注入，显示 unknown，版本和哈希用于追溯。
- Pi 快照 schema=1.1、version=12、generated_at=2026-10-07T08:22:18.683339Z、新 epoch=`39c5b924-a08b-c4ee-88a9-3251877e21c5`。三个 Kimi 指标均 status=ok、无 error_class、observed_at=2026-10-07T08:22:09.82504Z；ConnectorHealth state=ok、consecutive_failures=0。
- 真实 `/dev/vcsa1` 网格为 20×60；`/dev/vcsu1` 为 1200 个字符。第 9 行 Kimi 5h=100% LEFT/RESET 3H，第 10 行 `MONTH USED: TOTAL 90.5% | CODE 90.3%`，第 11 行 `5H 100% LEFT` 与 CRIT。完整画面保存于 `tmp/kimi-monthly-deployed.txt`；物理屏肉眼检查未执行。

回滚文件：

- Mac：`/Users/galendai/.local/bin/homepi-pre-kimi-monthly-20261007T0820/`，含旧 node/display 和 LaunchAgent plist；旧 node SHA-256=`a1151473784fc320331231b11a50044f8ee80e30606a3ab40e4d44d051c801bf`。安装脚本同时保留各 binary 的 `.previous`。
- DietPi：`/usr/local/bin/homepi-display.pre-kimi-monthly-20261007T0820`，旧版 `262fcd2`，SHA-256=`c91b10e6275fd298d941d4d93fde6b4f6fd969bd5a0373aa190f9b79e5da5f43`。
- 若需回滚两端，先恢复旧 node，再恢复旧 Display，避免旧 Display 读取新增月指标。此次无需回滚。
