# FIX-002：Web Admin 与配置事务审查修复

> 日期：2026-08-11
> 范围：Phase 2 P2-01/P2-02 增量
> 状态：自动化门禁完成，待用户检查，未提交

## 1. 目标

修复审查确认的 22 项缺陷，使浏览器公共路径可操作，配置与秘密提交具备可补偿事务语义，
并保持既有 CLI/custom region 兼容性。所有结论以自动化回归和公开命令路径为准；macOS
LaunchAgent、Windows/Linux 原生服务生命周期与真实 Provider 网络测试保留为手动验收。

## 2. 修复矩阵

| ID | 优先级 | 修复目标 | 回归证据 |
|---|---|---|---|
| R01 | P1 | `/static/*` 正确映射嵌入资源 | CSS/JS 返回 200 与正确内容类型 |
| R02 | P1 | 浏览器同源安全取得 CSRF token | bootstrap API + 状态修改成功/错误 token 403 |
| R03 | P1 | Web Apply 强制真实 restart/health | 注入回调调用计数；生产接入 service manager |
| R04 | P1 | Apply 前重新校验完整草稿 | 无效编辑不污染草稿；持久化前校验 |
| R05 | P1 | 先提交版本化秘密，再写配置 | 可观测步骤顺序与失败补偿 |
| R06 | P1 | 健康失败恢复配置后重启旧服务 | restart 调用两次 |
| R07 | P1 | 现有 Provider 候选秘密自动版本化 | 不设置 rotate 仍生成新引用 |
| R08 | P1 | revision 绑定磁盘内容 | 外部编辑产生 conflict |
| R09 | P1 | 加锁后复核 revision | 并发 Apply 仅一个成功 |
| R10 | P1 | `remove -keep-secret` 不剪枝 | Apply 保留集合回归 |
| R11 | P1 | 新增/轮换启用 Provider 必须测试 | 未测试/测试后再编辑均拒绝 Apply |
| R12 | P1 | 保持 custom region | 真实连接器 custom URL 编辑通过 |
| R13 | P2 | 恢复 add 默认 secret ref | 无新秘密但预置引用路径通过 |
| R14 | P2 | 完整串行化同一草稿请求 | 并发 API/race 回归 |
| R15 | P2 | Origin 精确比较 host/port | 其他 loopback port/host 403 |
| R16 | P2 | 请求活动重置空闲超时 | 活动请求延长 listener 生命周期 |
| R17 | P2 | pending 只标实际差异并合并增删 | 未改、添加、修改、删除状态回归 |
| R18 | P2 | 检测真实 metric ID 冲突 | mock 完整 ID 与真实类型投影冲突 |
| R19 | P2 | DisplayStatus 可缺省 | `/api/status` 无 panic，返回 disconnected |
| R20 | P2 | 草稿 secret_ref 脱敏 | HTTP 响应秘密引用扫描无命中 |
| R21 | P2 | 回滚配置原子恢复 | 回滚复用原子 Save 路径 |
| R22 | P2 | 失败 Apply 清理已提交候选秘密 | 补偿 Delete 及错误合并回归 |

## 3. 完成门禁

- 定向：`go test -count=1 ./internal/configtx ./internal/webadmin ./cmd/homepi-node`
- 全量：`make check`、`go test -race -count=1 ./...`
- 静态：`go vet ./...`、`gofmt -l cmd internal`、`git diff --check`
- 构建：当前平台构建、Windows amd64 node、Linux amd64/armv7 node 交叉编译
- 手动剩余：真实 Keychain、真实 Provider、macOS LaunchAgent 端到端；Windows/Linux 原生服务生命周期

## 4. 实际结果

- 定向包测试：通过；覆盖事务顺序、外部 revision、并发 Apply、原子回滚、旧服务重启、
  候选 secret 补偿、保留 secret、metric ID 冲突、pending 合并、Web 静态资源/CSRF/Origin/
  脱敏/并发/空闲超时和服务回调。
- `make check`：通过（gofmt、go vet、全仓 go test）。
- `go test -race -count=1 -timeout=180s ./...`：全部包通过。
- `go mod verify`：`all modules verified`。
- 当前 macOS、Windows amd64、Linux amd64、Linux arm/v7 `homepi-node` 构建：通过，输出到
  `/dev/null`，无仓库构建遗留。
- `gofmt -l cmd internal`：无输出；`git diff --check`：通过。

未执行且不得标记通过：真实 Provider 网络/凭据测试、真实 macOS Keychain 轮换、当前主机
LaunchAgent restart/health 端到端、Windows/Linux 原生用户服务生命周期、真实浏览器人工操作和
Pi 实屏验收。HTTP 黑盒自动化已覆盖浏览器所依赖的公共静态资源与 API 路径。
