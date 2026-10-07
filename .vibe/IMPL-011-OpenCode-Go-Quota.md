# IMPL-011 OpenCode Go 订阅剩余额度

- 日期：2026-10-07
- 状态：本地实现与自动化检查通过；真实账号、正式部署验收未执行。
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

这里是订阅额度百分比，另行充值的 Zen 余额不在此次范围内。真实账号只读采集、控制台人工对账、Mac/Pi 安装和物理屏检查均为 NOT RUN。需更新 node 与 display，并配置 Go API Key 后启用。没有提交、推送或重启正式服务。
