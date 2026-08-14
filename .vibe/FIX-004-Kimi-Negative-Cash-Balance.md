# FIX-004 Kimi 负现金余额兼容修复

## 状态

- 日期：2026-08-14
- 状态：DONE（用户已确认）
- 范围：Kimi API 余额解析与 API 主余额选择
- 提交：已获用户确认，随本次变更提交

## 现场现象

Kimi API 卡片从 2026-08-13 23:21:35 起连续进入 `schema_changed`，截至
2026-08-14 08:09:25 累积 54 次失败。最后成功值为 `available=0.44`、
`voucher=0.00`、`cash=0.44` CNY。使用与正式连接器相同的 Go Keychain 和 HTTP
客户端执行只读请求后，官方 `/v1/users/me/balance` 返回 HTTP 200、`code=0`、
`status=true`，当前值为 `available=0`、`voucher=0`、`cash=-1.0373199`。

现有连接器把任意负余额判为 schema 变化，因此拒绝整个成功响应并持续展示过期值。
同时，TUI 对同一 Provider 的多个 balance 指标使用最后遍历到的指标作为主值，可能形成
“名称来自 Available、金额来自 Cash”的错配。

## 修复契约

1. `available_balance` 与 `voucher_balance` 必须是可解析的非负 decimal。
2. `cash_balance` 必须是可解析的 signed decimal，允许负数并保留 exact 精度。
3. 任一字段缺失或不可解析仍返回 `schema_changed`；负 available/voucher 仍拒绝。
4. 同一 Provider 有多个 balance 指标时，TUI 稳定选择 `order` 最小的指标作为主余额，
   不依赖输入顺序；其他余额仍保留在快照中。
5. Kimi 当前真实响应应成功生成三条指标，API 主卡显示 `Kimi Available / CNY 0.00`，
   ConnectorHealth 在下一次正常调度成功后清零。
6. 不修改协议 schema、金额阈值、凭据、Provider 配置或其他连接器的字段校验。

## 验证计划

- Kimi 单元回归：真实负现金样本成功；负 available、负 voucher、不可解析 cash 均失败。
- UI 单元回归：同一 Provider 多个余额乱序输入时仍选择最小 `order` 的名称和值。
- 聚焦测试：`internal/connector/kimiapi`、`internal/ui`、`internal/protocol`、E2E。
- 全量门禁：`make check`、`go test -race -count=1 ./...`、`gofmt -l`、
  `git diff --check`。
- 实机：构建并安装 Mac node 候选，验证 `provider test -id Kimi` 成功；确认 Pi 快照
  显示当前可用余额且 FAIL 清零，display PID/重启次数保持健康。

## 实际结果

- 失败基线：新增测试在旧实现上分别复现负现金 `schema_changed` 与
  `Kimi Available / CNY -1.04` 名称金额错配。
- Kimi 连接器仅对 Cash 开放 signed decimal；Available/Voucher 的非负门禁和所有字段的
  可解析门禁保持不变。
- TUI 按 `(order, id)` 稳定选择主余额，名称、金额和状态来自同一指标；乱序输入回归通过。
- 聚焦 connector/UI/protocol/E2E 测试通过；Kimi/UI 重复 10 轮通过；`make check`、
  全仓 race、Windows amd64 node、Linux ARMv7 display、格式和空白检查全部通过。
- 当前源码和安装后的 Mac node 均通过真实 `provider test -id Kimi`，返回 3 个指标。
- Mac node 候选 `68abb88-fix004` 已通过现有本地安装脚本替换并自动重启，旧二进制保留为
  `~/.local/bin/homepi-node.previous`。
- Pi 快照确认 Kimi Health 为 `ok`、连续失败为 0，余额为 Available `0.00`、
  Voucher `0.00`、Cash `-1.04`。
- ARM64 display 候选 SHA-256 为
  `2430f5f47db64fee6b0b1f5a7f69d6e2fa9a180507ce4fa0ecb1d63b8a242349`；部署后
  service active、PID 1766、`NRestarts=0`。TTY API 页实际显示
  `Kimi Available  CNY 0.00  OK`，页状态为 LIVE。
- 旧 Pi 二进制保留为 `/usr/local/bin/homepi-display.pre-fix004`；已获用户确认随本次修复提交。
