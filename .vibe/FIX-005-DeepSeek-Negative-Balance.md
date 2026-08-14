# FIX-005 DeepSeek 负余额兼容修复

## 状态

- 日期：2026-08-14
- 状态：DONE（用户已确认）
- 范围：DeepSeek API 余额解析与实机恢复
- 提交：已获用户确认，随本次变更提交

## 现场现象

DeepSeek API 连接器从 2026-08-14 09:21:20 起持续进入 `schema_changed`，当前错误为
`DeepSeek balance value is invalid`。Pi 最近成功快照中没有 DeepSeek 当前指标，Health 为
`error`。

使用正式连接器相同的 macOS Keychain 凭据和官方 `GET /user/balance` 端点执行只读诊断后，
上游返回 HTTP 200、`is_available=false`，当前 CNY 余额为：

- `total_balance=-1.80`
- `granted_balance=0.00`
- `topped_up_balance=-1.80`

DeepSeek 官方文档把三个字段定义为字符串金额，并说明总余额由赠金与充值余额组成，没有声明
金额非负。现有实现额外拒绝任意负值，因而把合法的欠费状态误判为 schema 变化。

## 修复契约

1. `total_balance` 与 `topped_up_balance` 必须是可解析的 signed decimal，允许负数并保留
   exact 精度。
2. `granted_balance` 必须是可解析的非负 decimal；负赠金仍视为上游语义异常。
3. 任一必需字段缺失或不可解析、币种不是 CNY/USD、同币种重复，仍返回
   `schema_changed`。
4. `is_available=false` 仍生成三条当前余额指标，但状态为 `error` 且消息为
   `account unavailable`；不得把账户可用性与金额符号混为一谈。
5. 当前真实响应应生成 Total、Granted、Topped 三条指标。TUI 沿用 FIX-004 已建立的
   `(order, id)` 主余额规则，API 卡主值显示 `DeepSeek Total / CNY -1.80`。
6. 不修改协议 schema、金额阈值、凭据、Provider 配置、Kimi 规则或其他连接器校验。

## 验证计划

- DeepSeek 单元回归：真实负 total/topped-up 样本成功；负 granted、不可解析 total、
  不可解析 topped-up 均失败。
- 先在旧实现运行真实样本回归，确认失败基线，再做最小连接器改动。
- 聚焦测试：`internal/connector/deepseek`、`internal/ui`、`internal/protocol`、E2E。
- 全量门禁：`make check`、`go test -race -count=1 ./...`、`gofmt -l`、
  `git diff --check`，并执行 Windows amd64 node 与 Linux ARMv7 display 交叉构建。
- 实机：源码真实 `provider test -id deepseek-main` 成功后，只替换 Mac node；确认 Pi 快照
  出现三条 DeepSeek 当前余额、Health 恢复为 `ok`、连续失败清零，并核对 TTY API 页。

## 实际结果

- 失败基线：新增真实样本回归在旧实现上稳定返回
  `schema_changed: DeepSeek balance value is invalid`；负赠金与非数字字段的拒绝行为保持正常。
- DeepSeek 连接器仅对 Total 与 Topped-up 开放 signed decimal；Granted 的非负门禁和所有字段的
  可解析门禁保持不变。
- 聚焦 connector/UI/protocol/E2E 测试通过；DeepSeek/UI 重复 10 轮通过；`make check`、
  全仓 race、Windows amd64 node、Linux ARMv7 display、格式和空白检查全部通过。
- 当前源码和候选 Mac node 均通过真实 `provider test -id deepseek-main`，返回 3 个指标。
- Mac node 候选 `68abb88-fix005` 已原子替换并重启成功，当前二进制 SHA-256 为
  `f180fb2015024a2ec74947d359da07887acb83b7523b628d935b30b7724010d2`；旧 FIX-004 node
  保留为 `~/.local/bin/homepi-node.pre-fix005`。
- Pi 快照确认 DeepSeek Health 为 `ok`、连续失败为 0；Total `-1.80`、Granted `0.00`、
  Topped-up `-1.80`。由于上游 `is_available=false`，三条指标按契约显示
  `error / account unavailable`，这不是连接器失败。
- Pi display 未替换，仍为 `68abb88-fix004`；service active、`NRestarts=0`。TTY 自动轮播到
  API 页后实际显示 `DeepSeek Total  CNY -1.80  ERROR`，页状态为 LIVE。
- 用于切页验收的远程命令因 Pi 时钟约慢 15 分钟而以 `issued_in_future` 拒绝；随后改用只读
  自动轮播完成验收。时钟同步问题与本修复无关，本次未修改 Pi 时间或 NTP 配置。
- 配置、DeepSeek API Key、协议 schema、Pi 二进制和其他连接器均未修改；已获用户确认随本次修复提交。
