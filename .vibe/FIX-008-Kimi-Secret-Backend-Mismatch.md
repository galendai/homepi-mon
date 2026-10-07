# FIX-008 Kimi Coding AUTH FAIL：凭据后端不一致

- 日期：2026-10-07
- 状态：RESOLVED（运行时凭据恢复，node/Pi 快照与控制台回读通过）
- 范围：只读诊断、恢复既有 Kimi Key 的 Keychain 引用、定向刷新；不安装或部署新 binary。

## 证据与根因

1. 用户新增的 `kimi-coding` 已在磁盘配置中启用，`region=cn`，`secret_ref=keyring:provider-key:kimi-coding@1`，周期 60 秒。
2. 正式 LaunchAgent 使用已安装的 `f4594cf-fix007` binary，PID=22211。2026-10-07 15:14:08 日志确认凭据后端为 keychain；15:14:13 的 Kimi 采集失败类别为 auth。
3. 最新工作区 binary 通过正常主机上下文执行 `provider test -id kimi-coding`，返回 `auth: provider credential is not configured`。此错误来自本地秘密读取，没有发出上游请求。
4. 使用正确的 Keychain service `github.com/galendai/homepi-mon` 检查该引用，条目不存在（security exit 44）；同一引用的哈希文件存在于用户数据目录 secrets 中，权限 0600。
5. 使用该既有 fallback Key 和真实 HomePi 身份只读查询 `https://api.kimi.com/coding/v1/usages`，HTTP 200。Key 有效；故障是保存后的文件回退后端与 daemon 的 Keychain 读取后端不一致。保存时为何落入 fallback 的原始进程日志不足，不能把具体原因直接归为 Key 无效或 Kimi 拒绝认证。

## 恢复与验证

- 将原有 fallback Key 写入配置已引用但缺失的 Keychain 条目；写入前确认不存在，写入后在内存中比较读回值，完全一致。Key 经标准库的安全 stdin 路径传递，不写入 argv、仓库、日志或 Agent 输出；原 fallback 文件保留。
- 恢复后 `./bin/homepi-node provider test -id kimi-coding` 通过，1 metric / 1.282 秒。
- 通过既有控制接口仅执行 `refresh_data`，connector 选择 `kimi-coding`；sequence=20，结果 executed。未重启或替换 daemon。
- node snapshot version=57：`kimi-coding.5h` value=100/limit=100、status=ok、error_class 为空，observed_at=2026-10-07T07:21:00.248139Z；ConnectorHealth state=ok、consecutive_failures=0。
- Pi 缓存 snapshot version=63、generated_at=2026-10-07T07:22:22.505617Z，收到相同指标和正常健康状态。
- Pi `/dev/vcsa1` 的实际网格为 20×60；按 `/dev/vcsu1` UCS-4 字符缓冲回读到 `Kimi Coding ... 100% LEFT RESET 4H`，第二行为 `5H 100% LEFT ... OK`。物理屏肉眼检查未执行。

## AUTH 修复观察点的接口与限制

- 本次真实响应顶层为 `usages` 和 `limits`：5 小时额度在 legacy limits 中仍可被当前连接器读取；未返回 weekly。
- `usages` 包含 `limit_5h`、`limit_month_total`、`limit_month_code`。当前 HomePi 卡片只显示本次识别到的 5 小时窗口，100% 不代表月总额度或月编程额度也为 100%。新增月额度展示不属于本次 AUTH 修复。
- 新版官方契约见 [Kimi Code managed-usage 源码](https://github.com/MoonshotAI/kimi-code/blob/main/packages/oauth/src/managed-usage.ts)。前一次仅用合成样本验证两个窗口，不能视为此账号实际上有 weekly 权益。
- 工作区 IMPL-009 的源码修复仍未部署到正式服务；本次恢复解决的是本地凭据引用。后续通过系统 Keychain 可用的主机上下文操作配置，检查 secret backend 是否为 keychain，避免与 daemon 的后端不一致。
