# FIX-003 Display 自动重连修复

## 状态

- 日期：2026-08-12
- 状态：DONE（等待用户检查）
- 范围：`homepi-display` WebSocket 断线后的自动恢复
- 提交：等待用户检查后执行

## 现场现象

Mac 上的 `homepi-node` 在 2026-08-12 21:59:04 短暂停止，并于 21:59:20
恢复。DietPi 上的 display 在 21:59:05 收到 WebSocket EOF，记录约 2 秒后的重试，
但此后未建立新的 TCP 连接，界面持续显示 `OFFLINE`。两端服务状态、LAN 端口、TLS
证书指纹、设备绑定和撤销状态均正常。

## 修复目标

1. 每次拨号、Hello 写入和读循环都必须有明确的超时或取消边界。
2. 节点在 display 运行期间短暂不可用后，display 必须继续退避重试，不得永久卡住。
3. 节点恢复后，display 必须在下一次重试中接收新 epoch 的快照并恢复 `LIVE`。
4. 保留最近一次有效快照；失败拨号不得覆盖或删除该快照。
5. 日志不得包含设备 Token、Authorization Header 或 Provider 秘密。

## 验证计划

- 单元测试：模拟健康会话断开、节点不可达、节点恢复，断言拨号持续进行并恢复连接。
- 回归测试：运行 `internal/syncclient`、display E2E 与全量 Go 测试。
- 静态门禁：`gofmt`、`go vet`、`git diff --check`、race test。
- 交叉构建：Linux ARMv7 `homepi-display`。
- 实机测试：部署至 `ssh dietpi`，停止 Mac 节点超过一次失败重试窗口，再启动节点；
  不重启 display，确认新快照写入且 Web Admin/display 恢复在线。

## 结果

- 失败基线已由 `TestRedactURLTerminatesAndRedactsEveryURL` 在 250 ms 内稳定复现。
- 根因是旧 `redactURL` 把 URL 写成 `://[redacted]` 后从字符串起点重新搜索，反复命中自己
  生成的占位符并进入无限忙循环。
- 实现改为单次前向扫描尚未处理的原始后缀；多个 URL 均被脱敏，authority/path 不泄漏。
- WebSocket 每次拨号增加独立 30 秒 context 截止时间。
- 新增 E016 自动化，真实连接失败后节点恢复可在同一进程中接受新 epoch 快照。
- `make check`、全仓 `go test -race -count=1 ./...`、格式/diff 检查和 Linux ARMv7
  交叉构建通过。
- ARM64 候选已部署到 DietPi。实机在 22:25:59 断流、22:26:01 发生一次失败拨号；节点
  恢复后 22:26:07 写入新 epoch。display PID 保持 6428，`NRestarts=0`，CPU 0.5%。
- 旧二进制保留在 `/usr/local/bin/homepi-display.pre-fix003`，候选传输文件已清理。
