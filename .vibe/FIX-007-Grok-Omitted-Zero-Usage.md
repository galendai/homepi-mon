# FIX-007 Grok Unified Billing 省略零值兼容

- 日期：2026-09-06
- 状态：DEPLOYED AND VERIFIED
- 范围：Grok 解析与回归、规格和 README；不涉及认证生命周期、Display 布局或其他 Provider。

## 依据与契约

用户授权执行调研首选方案。官方源码证据及限定条件见 Module-Spec-001 的 Grok 省略值兼容依据。本修复替代 FIX-006 中“Unified 缺少百分比总是 N/A”的判断；FIX-006 的其他修复保持有效。

1. 仅明确 Unified Billing、百分比字段省略、weekly 周期有效且 start ≤ observed_at < end 时按 0% 已用，输出同 ID、value=100、limit=100、verified/compatibility_api、真实 reset。
2. Unified 显式 null 继续 N/A；非 Unified 缺失及非法值继续 schema_changed；省略值的过期/未来周期返回 schema_changed。
3. 显式合法百分比保持原有行为；按量付费和预付余额不得作为订阅百分比。
4. 只读 auth.json，仍不执行 CLI、不刷新认证、不传输凭据到 Pi。

## 验证

- 先运行新回归证明旧实现错误，再做最小修复。
- 聚焦 Collect→标准化指标→TUI；覆盖省略、0、非零、100、null、非法值与周期边界。
- make check、全仓 race、格式与 diff 检查。
- 新实现真实只读采集与本地 60×20 渲染，记录实际值和认证文件未变。
- 官方 /usage 人工对账：NOT RUN；部署后 Pi 快照及 TTY 验证已通过，见部署结果。

## 实际结果

- 旧实现失败基线：当前省略值和周期起点仍输出 N/A，未来周期/结束边界/过期周期未返回 schema_changed，合计 5 个分支失败。初次沙箱拒绝临时监听端口，允许本机测试网络后完成基线与后续验证。
- Grok/UI 聚焦测试、make check（gofmt/go vet/go test）、go test -race -count=1 ./... 均通过。新增表格覆盖 14 个子用例，既有认证及 schema 回归保留。
- 2026-09-06T12:27:59.430103Z 的候选实现只读 Collect 返回 grok-main.weekly：value=100、limit=100、precision=verified、source_kind=compatibility_api、status=ok；reset=2026-09-11T03:08:36.18093Z。本地 TUI 显示 100% LEFT / RESET 5D / 5H -- / OK。
- auth.json 内容哈希、mtime、mode 和 inode 对账未变；只打印标准化指标与渲染帧。
- 候选可执行文件：bin/homepi-node-grok-fix007，标识 0.1.0 / f4594cf-fix007；基于 f4594cf7aaa25a67ad8aa1840298d1f9ac5312df 加本轮未提交修改。
- 候选 SHA-256：`a1151473784fc320331231b11a50044f8ee80e30606a3ab40e4d44d051c801bf`。
- 本地验证阶段未部署；后续用户授权“立刻部署”，执行结果见下。部署验收时尚未提交或推送；官方 /usage 人工对账 NOT RUN。

## 授权部署与实机结果

- 用户于本轮明确授权“立刻部署”。2026-09-06 20:30（Asia/Shanghai）备份旧 binary，卸载现有 LaunchAgent 注册、原子替换已验证候选、使用原 plist 重新注册，服务恢复 installed=true/running=true。
- 安装路径：`/Users/galendai/.local/bin/homepi-node`；版本标识 `f4594cf-fix007`；安装 SHA-256 与候选一致：`a1151473784fc320331231b11a50044f8ee80e30606a3ab40e4d44d051c801bf`。
- 回滚文件：`/Users/galendai/.local/bin/homepi-node.pre-fix007-20260906-203036`。部署前后原 LaunchAgent plist、Grok auth.json 及节点 config.json 内容哈希未变。
- DietPi 快照 epoch=`8718c8bc-d45a-a528-2c29-e948ce9fb515`、version=11、generated_at=`2026-09-06T12:30:55.792729Z`；`grok-main.weekly` value=100/limit=100、status=ok、observed_at=`2026-09-06T12:30:55.167386Z`，reset=`2026-09-11T03:08:36.18093Z`。ConnectorHealth state=ok、consecutive_failures=0。
- 临时 CODING 页面命令执行成功（30 秒后自动恢复）；`/dev/vcsu1` 第 9–10 行确认 `Grok ... 100% LEFT RESET 5D` 与 `5H -- ... OK`。
- Pi Display 服务 active/running，MainPID=473 与部署前一致、NRestarts=0；无需更新或重启 Pi Display。
- 本轮完成安装文件、服务、真实上游采集、Pi 快照与字符屏缓冲回读验证。物理屏幕肉眼验收及官方 /usage 人工对账仍 NOT RUN。
