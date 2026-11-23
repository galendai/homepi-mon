# FIX-009 CRIT 不阻止五页轮播

```yaml
id: FIX-009
status: DONE
progress: 95%
verification: pending
started_at: 2026-10-10
completed_at: 2026-10-10
authority: local-fix-and-existing-dietpi-display-deployment
deployment: deployed
deployment_verified_at: 2026-10-10T08:57:34Z
```

用户明确要求“CRIT 警告不应该阻止轮播”。本修复替代 P3/P4 历史记录中的 CRIT 页面抢占契约；不把历史验收改写为本轮证明。沿用本轮“立即部署”对 DietPi Display 的授权；Git 提交/推送未授权。

## 现场基线

2026-10-10T08:42:51Z：Pi Display PID 1058，active/running、NRestarts=0；正式文件和实际进程 SHA-256 均为 `ca92aaadefc3739a089cd6d7053f8dcc7c60526a02cb8491adeaf9231c55bf31`。环境 SHA-256 为 `2509fc85651c8f8f3d37b6eb6911f1f5dd5324fcb349e9686477840501df4c62`。

实际 TTY 为 20×60；Kimi Coding MONTH 8% 持续 CRIT，画面固定 OVERVIEW / `1/5 AUTO 30S`。配置顺序为 CODING、API、HOMELAB、SERVICES、SYSTEM，停留 30/30/10/10/5 秒。

## 可验证成功条件

1. 持续单页或多页 CRIT 不改变路由位置与计时；按配置切完五页并回环。
2. 额度阈值、严重状态文字/颜色及全局告警计数保持有效，ASCII/rich 画面仍为 60×20。
3. 远程切页与显式暂停/恢复、临时间隔、覆盖到期后的剩余 dwell 恢复按现有规则执行；CRIT 不屏蔽远程消息。
4. 聚焦回归、全仓 make check、相关 race 和 Linux ARM64 构建通过。
5. 部署后进程哈希匹配候选、环境哈希不变、服务健康；真实 CRIT 存续期间在原 85 秒周期内观察五页与回环，保留脱敏 TTY 证据。

## 实际结果

规格与测试文档先更新。新增定向测试先在旧实现复现：持续 Kimi 月 CRIT 时第 30 秒仍停在 CODING，多页 CRIT 时启动即错误显示 SERVICES；修复后通过。

Router 不再接收 CRIT 集合，移除告警抢占与活动标志；Display 保持 `ui.Build` 的全局严重状态与告警计数，不再用告警屏蔽远程 notice。阈值、卡片排序与渲染没有因本修复改变。

UI/Controller/Display 聚焦、全仓 make check、相关三包 race、git diff --check 通过；Linux ARM64 静态候选和 Darwin ARM64 Display 构建成功。候选版本 `92dd76e-dirty+crit-rotation`，built=2026-10-10T08:50:51Z；源码指纹 `2543e0e0d3e2a68bede361b6743c5174763c04d817d25eb6c20abfca4b48ffc2`，156 文件。

2026-10-10T08:54:17Z 已安装至 DietPi，PID 1423、active/running、NRestarts=0；安装/实际进程 SHA-256 均为 `8c92bd18bc94714fae93807b5db1f9660bcffb62bf5e0628a7514b4f2fd6a381`，环境哈希不变。旧版本备份为 `/usr/local/bin/homepi-display.bak-20261010-crit-rotation-8c92bd18`，SHA-256 为 `ca92aaadefc3739a089cd6d7053f8dcc7c60526a02cb8491adeaf9231c55bf31`；实际未触发回滚。

2026-10-10T08:55:53Z～08:57:34Z 完成 100.55 秒只读观察：199 个 TTY 样本均有真实 CRIT，Kimi 总月剩余 7.73%、CODE 月剩余 7.9%；五页依序出现并回到 OVERVIEW。原停留配置仍为 30/30/10/10/5 秒，运行 PID 1423、NRestarts=0，进程与环境哈希稳定；五页 480×320 framebuffer 与 20×60 TTY 已检查，布局和红色告警保留。详见 Test-MOD-002 第 13 节。

实现、自动化、部署及机器观察完成；用户肉眼验收、实机远程命令重测及长期稳定性仍为 NOT RUN。临时构建、脚本和测试数据已清理，备份与脱敏证据保留。Git 未提交/推送；verification=pending、progress=95%。
