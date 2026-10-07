# IMPL-009 Kimi Coding Plan 中国大陆版余量显示

- 日期：2026-10-07
- 状态：DONE（本地实现与验证；后续真实账号与正式 Pi TTY 验收通过，详见 FIX-008/IMPL-010）
- 范围：现有 Kimi Coding 连接器的区域配置、客户端身份、weekly 语义、回归测试和使用说明。

## 可验证完成条件

1. `kimi_coding` 的 `cn` 查询中国大陆 Coding Plan 端点，Web Admin 默认 cn；历史 global 配置保留现有地址，国际端点可使用 custom 显式配置。
2. 官方契约的 weekly usage 与 5 小时 limits/detail 可转换为正常的 CODING 余量卡片；合法零值不会丢失，非法值不伪造余量。
3. 真实 HomePi User-Agent，401/403/429/5xx/schema 分类和脱敏，404 有界回退保持有效。
4. 聚焦契约与渲染检查、`make check`、本机与 Linux ARMv7 构建通过。真实账号对账、Pi 实机验收分别记录，不将合成样本视为上游实测。

## 验证记录

- 新回归先复现旧实现的三个问题：带名称的顶层 usage 被漏掉，元数据默认地址指向 Moonshot 开放平台，请求冒用 KimiCLI 身份。
- 修复后 `go test -count=1 ./internal/connector/kimicoding ./internal/ui ./internal/webadmin` 通过。合成响应的两个窗口保持独立指标并进入同一卡片，5h=68%、weekly=75%、RESET 4H，输出为 60×20；额度语义 11 个分支、错误 5 个分支、区域 2 个分支及既有 404 回退通过。
- `make check` 通过：gofmt、go vet、全仓 go test。`git diff --check` 通过。
- `make build` 的 darwin/arm64 node/display 通过；Linux ARMv7 Display 和 Windows amd64 node 交叉构建通过。候选位于 bin，基于 a0def2e 加本轮工作区变更，内嵌 commit 仅表示基线，不代表本轮变更已经提交。
- 首次沙箱测试无法监听 httptest 端口，本机构建无法写用户 Go 缓存；在允许这些测试/构建操作的主机上下文运行后通过。
- 只读检查当前用户配置确认没有 `kimi_coding` 账号；真实 Coding Plan 请求、官方控制台/CLI 数值对账和 Pi 实机验收为 NOT RUN。
- 中英文 README 已补充中国大陆账号的 Web Admin 与 secret-stdin 配置方式。未安装服务候选、未部署、未提交或推送。

## 用户配置后的 AUTH 调试补充

2026-10-07 用户增加 Key 后反馈 AUTH FAIL。后续 FIX-008 证实本地文件回退与 Keychain 后端不一致，恢复同一 Keychain 引用并定向刷新后，真实只读请求与正式 node→Pi 快照/控制台正常。本账号实际只返回可识别的 5 小时窗口（100%），未返回 weekly，并带有尚未接入的月额度字段。候选源码仍未部署；不能将本次旧服务恢复等同于新版服务部署验收。

后续月额度解析和进度条下方显示已由 [IMPL-010](IMPL-010-Kimi-Monthly-Usage.md) 完成，并通过真实账号只读预览。用户明确授权“部署更新”后，正式 node/display 更新、服务与 Pi TTY 验收通过；版本与回滚证据见 IMPL-010。
