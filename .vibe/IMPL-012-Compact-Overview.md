# 紧凑总览正式接入

```yaml
id: IMPL-012
status: DONE
progress: 95%
verification: pending
started_at: 2026-10-10
completed_at: 2026-10-10
authority: local-implementation-and-dietpi-display-deployment
deployment: deployed
deployment_verified_at: 2026-10-10T04:29:34Z
```

用户先要求验证紧凑布局，在离线快照和实机字库重放后要求“按紧凑视图实现”。

目标：将已选定的 60×20 总览接入 `homepi-display run`，在现有 TFT 字号下同页显示订阅额度和 API 主余额。

验收依据为 UI-001 6.2/6.3、MOD-002 13 和 Test-MOD-002 7。保留五页面 Router、API/HomeLab/Services/System 详情、远程通知、CRIT 仲裁及重复帧抑制。超容量明确回退分页详情，严格离线 preview 不改变。

第一轮先完成本地实现、聚焦测试、仓库 check、race 和 Linux ARM64 交叉构建；该轮未授权 DietPi 安装/重启或 Git commit/push，后续部署授权如下。

新增授权：2026-10-10 用户明确要求“立即部署”，授权将当前实现安装到现有 DietPi Display 并重启该服务。部署前保留旧二进制，候选传输后核对 SHA-256，使用同文件系统原子替换；启动或正式输出验证失败时恢复旧二进制。部署验收包含实际进程哈希、TTY 网格/关键值、framebuffer 画面和短时资源/画面变化观察。Git commit/push 仍未授权，用户肉眼验收及长期稳定性继续单独记录。

实际结果：2026-10-10 完成正式 screen 接入、轮播/HOLD 页脚、分钟级年龄显示、明示分页回退和通知恢复。5 订阅 + 4 API 总览、15 订阅的 8 子页回退、精确 freshness 边界、终端重复帧/余额变化均通过；全仓 make check、UI/display race、静态 ARM64 构建和 20×60 真实快照重放通过。测试输出及边界记录在 Test-MOD-002 第 7 节。

第一轮预览代码保留在工作区，本轮实现也未提交。临时测试目录/二进制清理，长期预览留在会话输出目录；其余 Phase 任务的历史状态不改写。

部署结果：2026-10-10 10:42:31 +08:00 确认已安装到现有 DietPi。候选版本 `0.1.0 (92dd76e-dirty+compact)`，当前进程 PID 702；安装文件及 `/proc/702/exe` SHA-256 同为 `2cce29f4a558f51404e4865f0ac6206aa846c72607949eaaee9494fe0914a8a1`。`homepi-display.service` active/running、NRestarts=0。

旧二进制保留在 `/usr/local/bin/homepi-display.bak-20261010-compact-jVaTsY`，SHA-256 为 `a13e0e664f3b8fab0bcb3d4b87c9b9fd8336f07ac91dec3b61a590ec76244e5b`。部署事务带失败自动回滚，实际未触发。Node、服务 unit、环境配置均未修改；现有 CODING dwell 实际为 30 秒并正确显示。

实机证据见 Test-MOD-002 第 8 节及 `.vibe/evidence/IMPL-012-Pi-Compact-Overview.png`：实际 TTY 20×60、framebuffer 480×320 RGB565，4 订阅 + 2 主余额 + Kimi 月已用完整，连接恢复后 LAN OK。20.34 秒只读观察期间 PID 和重启计数未变化，单核口径 Display 平均 CPU 0.246%，末尾 RSS 13184 KiB，40 次 framebuffer 采样发现 1 次画面变化。

部署与短时机器验证已完成；用户肉眼可读性和长期连续运行仍待验收，因此 verification=pending、progress=95%。Git commit/push 未执行。

后续验收反馈（2026-10-10）：用户报告 Kimi/Codex 周/月显示不正确。本任务回到 IN_PROGRESS、verification=rejected；修复与重新验收记录见 FIX-007 及 Test-MOD-002 第 9 节。布局部署证据保留，但不代表 Provider 窗口语义验收通过。

重新验收结果（2026-10-10 11:10）：FIX-007 已完成 Codex 按时长归列、Review 隔离和 Kimi 月总余量列；聚焦、全仓、race、两端构建与已授权部署链路全部通过，实际屏幕显示 Codex WEEK 53% / Kimi MONTH 8%。任务恢复 DONE、verification=pending、progress=95%，等待用户确认修复后实屏；Git 未提交/推送。证据见 Test-MOD-002 第 9 节与 FIX-007。

本轮显示微调（2026-10-10）：用户要求订阅/API 之间一空行和百分比配色。沿用当前已授权 Display 部署范围，实现 UI-001 6.2 新规则，并按 Test-MOD-002 第 10 节验证。状态暂回 IN_PROGRESS，既有窗口修复及两端部署证据保留。

本轮结果（2026-10-10 11:36 +08:00）：空行、独立窗口配色和 Kimi 月已用反向配色已实现并部署，任务恢复 DONE / progress=95% / verification=pending。聚焦边界、全仓 make check、严格 20×60 实际快照预览和 ARM64 构建通过；旧 Codex 跨颜色文字断言已改为剥离 SGR 后验证。实际进程 PID 892，安装与进程 SHA-256 均为 `39d5c68865eebedb55d4683b927d443a795b9b27190e1e08cfa31bc849ec53c1`，服务 active/running、NRestarts=0。TTY 第 9 行为空、第 10 行为 API 标题，百分比绿/红/灰与余量一致；月已用 TOTAL 92.3% / CODE 92.1% 为红。实机 480×320 framebuffer 证据见 Test-MOD-002 第 10 节及 `.vibe/evidence/IMPL-012-Pi-Compact-Colors.png`。

本轮旧 Display 保留在 `/usr/local/bin/homepi-display.bak-20261010-compact-colors-ec6698a3`，哈希为此前 quota-columns 版本 `5941b13df2543c52d85dc8864eb19922cb922471cb7fd1173705d8ff0a13ec1e`；部署事务未触发回滚。环境配置哈希不变，本轮仅安装 Display。临时构建和辅助脚本清理，可复核的截图、TTY、脱敏颜色证据及 manifest 留在会话输出目录；Git 未提交/推送，物理屏幕肉眼验收仍待用户确认。

本轮反馈（2026-10-10）：用户认为 Kimi Coding 单独两行不协调，要求统一。任务回到 IN_PROGRESS / verification=rejected；按 UI-001 6.2 将总览统一为每个订阅一条额度行，移除总览 MONTH USED，保留 MONTH 月总余量、空行、颜色、原详情明细及月额度告警。验证与已授权 Display 部署记录见 Test-MOD-002 第 11 节。

单行统一结果（2026-10-10 12:12 +08:00）：聚焦、make check、两平台 Display 构建与真实快照预览通过，已部署到 DietPi。当前版本 `92dd76e-dirty+single-row`，PID 971，安装/进程 SHA-256 `450d975c0f819d88bd3e86b564439a01b1e64f3463369d8af4eb6762689819b0`；服务 active/running、NRestarts=0。实际 TTY 第 4～7 行为连续四个订阅，第 8 行空、第 9 行 API；Kimi 55% / -- / 8%，每窗口颜色和当前快照匹配，总览无 MONTH USED。原详情双月明细、月状态和 AUTH 隐藏回归通过。截图为 `.vibe/evidence/IMPL-012-Pi-Single-Row.png`。

备份 `/usr/local/bin/homepi-display.bak-20261010-single-row-a4d4ebd8` 保留前一轮 compact-colors 二进制，SHA-256 `39d5c68865eebedb55d4683b927d443a795b9b27190e1e08cfa31bc849ec53c1`；环境哈希不变，实际未回滚。状态恢复 DONE / verification=pending / progress=95%。临时脚本、构建产物和中间快照清理，截图/TTY/脱敏证据与 manifest 保留。Git 未提交/推送，用户肉眼验收仍待确认。

标题栏分割线调整（2026-10-10）：用户要求恢复顶部标题栏下方的分割线，任务暂回 IN_PROGRESS。按 UI-001 6.2 在第 3 行绘制整行分割线，数据区为 13 行；单行订阅、窗口颜色、API 前空行和底部健康/页脚保留。验证和沿用已授权范围的 Display 部署结果见 Test-MOD-002 第 12 节。

标题分割线结果（2026-10-10 12:29 +08:00）：聚焦、make check、两平台 Display 构建及真实快照严格预览通过，已部署到 DietPi。版本 `92dd76e-dirty+title-divider`，PID 1058，正式文件与 `/proc/1058/exe` SHA-256 均为 `ca92aaadefc3739a089cd6d7053f8dcc7c60526a02cb8491adeaf9231c55bf31`。服务 active/running、NRestarts=0；实际 TTY 第 3 行为亮青分割线，第 4 行 PLAN 表头，第 5～8 行四个订阅，第 9 行空、第 10 行 API。全部 12 个窗口与快照的文字/颜色匹配，底部 LAN OK。480×320 framebuffer 证据 `.vibe/evidence/IMPL-012-Pi-Title-Divider.png`。

备份 `/usr/local/bin/homepi-display.bak-20261010-title-divider-313b940c` 保留前一轮 single-row 二进制，SHA-256 `450d975c0f819d88bd3e86b564439a01b1e64f3463369d8af4eb6762689819b0`；环境哈希不变，未触发回滚。临时构建、脚本与中间快照清理，截图/TTY/脱敏验证证据与 manifest 保留。恢复 DONE / progress=95% / verification=pending；Git 未提交/推送，用户肉眼验收待确认。

轮播规则更新（2026-10-10 16:57 +08:00）：用户明确要求 CRIT 不阻止轮播，当前路由契约替代本记录中此前的 CRIT 页面抢占行为。FIX-009 已移除告警对五页路由的暂停，保留严重状态和计数；全仓/race/构建及已授权 Display 部署通过。版本 `92dd76e-dirty+crit-rotation`，PID 1423；100.55 秒、199 次真实 TTY 样本验证持续月 CRIT 下五页切换与回环，原停留参数与环境哈希不变。部署备份、五页画面与验收边界见 FIX-009 和 Test-MOD-002 第 13 节；Git 未提交/推送，用户肉眼验收仍 pending。
