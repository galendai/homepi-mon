# IMPL-011 OpenCode Go 订阅剩余额度

- 日期：2026-10-07
- 状态：本地实现、自动化检查与 Mac/Pi 程序部署验收通过；OpenCode Go 真实账号等待用户在 Web Admin 配置 Key。
- 范围：新增 `opencode_go` Provider、CLI/Web Admin 配置入口、三窗口 CODING 卡片与容量超限子页。

## 可验证完成条件

1. API Key 经现有系统凭据库引用读取；每次只读 GET 官方 `/zen/go/v1/usage`，不启动 CLI、不刷新登录、不产生模型调用、不泄露响应或凭据。
2. 当前及原始已合并响应契约都输出独立 5h/weekly/monthly 剩余百分比指标与 UTC 重置时间；缺失、非法或矛盾的窗口报 schema_changed；三窗口不互相覆盖。
3. 同一卡片主条显示 5h，第二行显示三窗口剩余值；monthly 参与 CRIT/STALE/AUTH 判断，AUTH 隐藏缓存值并提示更新 Go API Key。
4. 五订阅共存、含 Kimi 月已用行时，60×20 ASCII/rich 完整卡片分为子页，按 dwell 轮换；告警不阻止查看其他订阅。
5. 聚焦测试、仓库 `make check` 和差异检查通过；真实账号与生产部署证据单独记录。

## 实现与证据

- 官方来源：[当前接口](https://github.com/anomalyco/opencode/blob/dev/packages/console/app/src/routes/zen/go/v1/usage.ts)、[订阅计算](https://github.com/anomalyco/opencode/blob/dev/packages/console/core/src/subscription.ts)、[原始已合并 PR](https://github.com/anomalyco/opencode/pull/16513)，于本轮实时读取。当前 `usage.*.percent` 与旧版 `*Usage.usagePercent` 均为已用，使用 exact decimal 计算 100-used；不依据套餐价格硬编码美元额度。
- 不增加 wire schema；沿用 quota/percent、monthly、compatibility_api 和现有 Scheduler HTTP 错误分类。元数据声明 `.5h/.weekly/.monthly`，global/custom 区域，最小采集周期 30 秒；README 建议 5 分钟。
- 测试：`internal/connector/opencodego` 覆盖 GET 地址/鉴权/客户端身份、两种契约、0/100/小数、缺失/null/异常比例、reset、current 优先、HTTP 401/403/429/500、单次请求、错误脱敏及实际 UI 映射。
- `internal/ui/opencode_test.go` 覆盖月窗口状态、AUTH 隐藏数值、5h 主窗口保留、五张卡片的 ASCII/rich 完整子页及 CRIT 期间轮换；`internal/webadmin/opencode_test.go` 覆盖选项、草稿配置、Key 不回显与 cn 拒绝。
- `GOCACHE=/tmp/homepi-go-build go test ./internal/connector/opencodego ./internal/ui ./internal/webadmin ./internal/connector` 通过。
- `GOCACHE=/tmp/homepi-go-build make check` 通过（gofmt、go vet、全仓 go test）；`git diff --check` 通过。沙箱默认系统 Go 缓存和回环监听受限，最终检查使用临时缓存与主机权限，不修改测试约束。
- `node --check internal/webadmin/static/app.js` 通过；Mac node 与 Linux ARM64 display 候选 `go build -trimpath` 通过，输出分别为 `/tmp/homepi-node-opencode-go` 与 `/tmp/homepi-display-opencode-go-linux-arm64`，没有覆盖已有安装或候选目录。

## 显示示例（合成测试数据）

```text
OpenCode Go [#############---] 80% LEFT      RESET 4H
  5H 80% | WEEK 65% | MONTH 50% LEFT             OK
```

这里是订阅额度百分比，另行充值的 Zen 余额不在此次范围内。实现阶段没有安装或重启正式服务。后续提交 `42a7b5a` 已推送 main，正式程序部署见下；Go 真实账号只读采集、控制台人工对账和物理屏肉眼检查仍为 NOT RUN。

## 正式部署验收（2026-10-07 16:57–17:01 Asia/Shanghai）

用户明确授权“部署opencode go相关更新”，并选择自行在 Web Admin 填入 Go API Key。从干净的已推送 `42a7b5a` 构建正式候选，先更新 Display，再更新 node；未添加账号、修改配置或读取其他应用的 Key。

| 程序 | 平台 | 提交 | SHA-256 |
|---|---|---|---|
| Mac node | darwin/arm64 | 42a7b5a | 214a9dc36b63bc31aa1afa9b8351111ab66f744b3504d401aa09cb9012059997 |
| Mac display | darwin/arm64 | 42a7b5a | b3901e91571ea230adfcd8132c96cc9693bb25d71fe4e15e3bd087ffe30e371c |
| Pi display | linux/arm64 | 42a7b5a | 13013f3866960e3539e36a0bcd0686647b33d68544f1ae582bdd048bbfa42044 |

- Mac 通过项目 `scripts/install-local.sh --skip-build` 更新原安装并重新注册 LaunchAgent。正式 node PID=85404，state=running、runs=1、last exit=never exited；配置验证通过，仍为 7 providers / 1 device。配置 SHA-256 前后均为 `200fc11443fe43e1202ded402250e428b81719a789ef50ab44e874992d54f098`。
- Pi 重新只读确认 aarch64，经既有 SSH 标准输入上传，候选哈希核对后备份并原子替换；原 `homepi-display.service` active/running，PID=1223、NRestarts=0、ExecMainStatus=0。`/proc/1223/exe` 的哈希与候选相同；environment 哈希前后均为 `2509fc85651c8f8f3d37b6eb6911f1f5dd5324fcb349e9686477840501df4c62`。
- Pi 已收到 node 重启后的 schema=1.1 快照；epoch=`9257d696-9151-fcc4-7143-dff98f818180`，version=20，generated_at=`2026-10-07T08:59:53.031759Z`。现有启用 ConnectorHealth 全部 ok、consecutive_failures=0。
- 只读检查真实 `/dev/vcsa1`、`/dev/vcsu1`：20×60、1200 字符；MiniMax、Codex、Kimi 与原有月使用量正常，SYNCED 0S AGO；CRIT 来自原有 Kimi 月余量。Go 指标为 0 条，因为尚未配置账号；不将安装成功当成 Go 真实余额验收。
- 新版 Web Admin 已在 `http://127.0.0.1:52818/` 启动，静态选项包含 OpenCode Go。`/api/status` 的 runtime.state=synced，admin/service 的 version=0.1.0、commit=42a7b5a，service_installed/service_running=true。地址为本次进程临时地址，默认无请求一小时后退出。

回滚保留：Mac `/Users/galendai/.local/bin/homepi-pre-opencode-go-42a7b5a/` 含旧 node/display/plist，安装脚本另保留 `.previous`；Pi `/usr/local/bin/homepi-display.pre-opencode-go-42a7b5a`。此次无需回滚。
