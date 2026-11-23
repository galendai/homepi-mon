# FIX-007 Codex 周窗口与 Kimi 月额度列修复

- 日期：2026-10-10
- 状态：DONE（实现、自动化、真实采集及已授权两端部署通过）
- 验收：pending（旧版本反馈 rejected；修复后机器验证通过，等待用户确认）
- 范围：Codex 主订阅窗口归类、代码审查窗口隔离、Kimi MONTH 列；保留既有紧凑布局及凭据边界。
- 授权：此前“立即部署”明确覆盖 DietPi Display。本轮完成修复、测试和候选构建后，用户明确选择“授权更新 Mac Node，并完成两端实屏验证”，补充授权安装/重启 Mac Node 及两端验收；Git 未授权。

## 现场证据

2026-10-10T02:58:32Z，使用正式 Provider 配置和既有本机凭据执行只读采集，诊断仅输出白名单用量字段，不打印或写入凭据：

- Codex `rate_limit.primary_window.limit_window_seconds=604800`、`used_percent=46`、`reset_at=1791948584`，secondary=null。当前代码错误生成 `.5h / rolling_5h / 54%`；实际为周余量 54%，2026-10-14T03:29:44Z 重置。本次接口未提供 5h 或 monthly。
- Kimi 返回 `limit_5h.used_ratio=0.451006`、`limit_month_total=0.9227`、`limit_month_code=0.921`；未返回 `limit_7d`。快照月剩余为 0.0773/0.079，但紧凑 MONTH 列为空。
- Pi snapshot 22996 / 2026-10-10T02:56:33Z、TTY 20×60 复现上述错列。Display PID 702，旧紧凑版本 `92dd76e-dirty+compact`。
- [OpenAI 官方 App Server 文档](https://learn.chatgpt.com/docs/app-server) 明确窗口有独立时长；primary/secondary 本身不表示 5h/weekly。[Kimi 官方解析](https://github.com/MoonshotAI/kimi-code/blob/main/packages/oauth/src/managed-usage.ts) 独立读取四种可选 quota 字段。

## 可验证修复条件

1. Codex 主订阅的每个非空窗口按 `limit_window_seconds` 归类：18000 为 5h，604800 为 weekly。只生成上游提供的窗口；不根据 primary/secondary 位置或 reset 距离猜测。
2. 缺失、非法、未知时长或重复同类窗口安全返回 `schema_changed`。保留既有 HTTP/1.1、单次 GET、只读 auth.json 和 exact decimal 语义。
3. Codex Review 使用独立卡片，不能覆盖 Codex 主订阅 WEEK；已知 review 时长也必须正确归类，旧 review 缺失时长的明确 weekly 兼容语义保留。
4. Kimi MONTH 列明确代表月总额度剩余，按既有整数百分比规则；仅 CODE 存在时不冒充总额度。下一行继续显示 `MONTH USED: TOTAL ... | CODE ...` 一位小数。AUTH 隐藏所有值，月告警保持。
5. 实际账户当前显示 Codex `5H -- / WEEK 54% / MONTH --`，Kimi `5H 55% / WEEK -- / MONTH 8%`，月已用 TOTAL 92.3% / CODE 92.1%。缺失 Kimi week 和 Codex month 保持 `--`。
6. 新增能在旧实现失败的定向回归，经 Collect→Build→Kiosk 验证列、reset、审查隔离、缺失窗口、AUTH/STALE、rich/ascii 20×60；聚焦、全仓和交叉构建通过。

## 实际结果

- 定向回归先在旧实现失败，修复后聚焦、全仓 make check、Codex/UI/Display race 全部通过；macOS node/display 和 Linux arm64 Display 构建成功。
- 修复后的只读真实请求于 2026-10-10T03:03:41Z 返回 Codex `.weekly` / 53% / 604800 秒，无 `.5h`；Kimi 总月剩余 7.73% / CODE 7.9%，本地严格 20×60 重放显示 MONTH 8%。
- DietPi Display 已更新，版本 `92dd76e-dirty+quota-columns`，PID 788、active/running、NRestarts=0；进程/安装哈希 `5941b13df2543c52d85dc8864eb19922cb922471cb7fd1173705d8ff0a13ec1e`。备份 `/usr/local/bin/homepi-display.bak-20261010-quota-columns-8f52c1`。Mac Node 更新成功，最终机器验收如下。
- 官方控制台同观察点人工对账和物理 TFT 肉眼验收仍为 NOT RUN。


## 两端部署后验收

- Mac Node：版本 `92dd76e-dirty+quota-columns`、built=2026-10-10T03:04:25Z；PID 1420→29356、服务 running=true。已安装 node SHA-256 为 `671d4c0346c54467f22674ead9ef4b1fd46faa83d89d22d931bf3d833ab41e4b`，匹配候选；进程命令为正式安装路径。配置和 LaunchAgent plist 前后哈希一致。
- 使用项目现有安装脚本更新 Mac 二进制包并重新注册 Node；备份为 `/Users/galendai/.local/bin/homepi-pre-quota-columns-20261010T0304-8f52c1/`，包含旧 node/display、LaunchAgent 与非秘密 manifest。Pi 备份见上。实际未回滚。
- 2026-10-10T03:10:27Z 的 Pi snapshot 24 / 新 epoch 已收到 `.weekly`=53%，无旧 `.5h`；Kimi 5h/双月数据均 status=ok。两连接器 health=ok、连续失败=0。
- 实际 TTY 20×60 与 480×320 framebuffer 对应正确列：Codex `-- / 53% / --`，Kimi `55% / -- / 8%`，TOTAL 92.3% / CODE 92.1% USED；未知 Kimi 周/Codex 月继续 --。
- 截图 `.vibe/evidence/FIX-007-Pi-Quota-Columns.png`，SHA-256 `b4eb46dca46defaa3944976e57168cf271c11b90c92ede54fb8a22db52ae132a`；详细命令、快照与部署证据见 Test-MOD-002 第 9 节。
- 本轮源码指纹在构建和部署后保持一致。临时诊断/脚本/测试目录清理，回滚备份与可审阅证据保留。Git 未提交/推送；用户肉眼、官方控制台和长期验收仍为 NOT RUN。
