# 模块 003 测试文档：HomeLab 监控

> 对应规格：Module-Spec-003-HomelabMonitoring.md  
> 所属阶段：Phase 3
> 状态：受控契约/端到端与实机轮播完成，真实 HomeLab 服务对账待执行

## 1. Unit Test

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| U001 | node_exporter idle rate=0.72 | CPU 使用率标准化为 28% | 受控 instant vector 28.4 四舍五入为 28% | 通过 |
| U002 | MemTotal 与 MemAvailable | 正确计算内存使用率，避免使用 free 字段误判缓存 | 默认 PromQL 固定使用 MemAvailable/MemTotal，42.2 输出 42% | 通过 |
| U003 | 根分区使用率 86%/96% | 分别为 warning/critical | 86.1 为 warning，96 为 critical | 通过 |
| U004 | `up=0` 且存在旧资源指标 | 节点 critical，旧指标 stale | state 将 offline 标为 critical，保留前一 CPU=28，失败可附加 stale/network | 通过 |
| U005 | Prometheus 本身认证失败 | connector error，不把所有目标标为 down | 401 分类 auth，不产生 `up=0`，短错误不含 Token | 通过 |
| U006 | 查询返回超过 series 上限 | 返回配置/高基数错误，不静默截断 | 请求 `limit=max+1`，客户端检出第 max+1 条并拒绝 | 通过 |
| U007 | Grafana 新 API 可用 | 使用新 API 并记录版本/能力 | 记录 12.1.0 / `apis-v0alpha1`，聚合 2 告警 | 通过 |
| U008 | Grafana 仅支持旧 API | 按兼容矩阵降级，不抓取 HTML | 新 API 404 后记录 `api-legacy`；非 404 不降级 | 通过 |
| U009 | Portainer 新版 `/system/status` | 正确解析健康和版本 | 解析 2.38.0 / `system-status` | 通过 |
| U010 | Portainer 旧版仅 `/status` | 经能力探测使用兼容路径并标明版本 | modern 404 后解析 2.17.1 / `legacy-status` | 通过 |
| U011 | 自签证书未安装到远端系统信任库 | TLS 连接失败并归类为 network，不跳过校验、不泄露底层请求细节 | 默认 client 拒绝未信任 `httptest` 证书，分类 network；无 insecure 选项 | 通过 |
| U012 | CPU 瞬时 100% 后恢复 | 短期聚合避免单点长期 critical | 默认 CPU 查询锁定 `[5m]` rate 平滑；真实 exporter 尖峰未执行 | 部分通过 |
| U013 | Prometheus 默认配置 | 只生成 7 条固定 instant query，均含 timeout/limit，预算不超过 10 | 发出 7 查询+1 buildinfo，每条 query 含 10s timeout/65 limit | 通过 |
| U014 | Prometheus option 含未知键、空/超长查询或 max_series>1000 | 启动前返回 invalid_config，不发网络请求 | 未知键、换行查询、max_series=1001 均拒绝 | 通过 |
| U015 | instant vector 缺实体标签、值非有限数、同查询实体重复 | 返回 schema_changed，不发布部分或误导值 | 三种恶意 vector 均 schema_changed，`HomeLab()` 仍为空 | 通过 |
| U016 | Prometheus 网络失败且已有节点摘要 | 保留最后节点数值，附加 network/stale；online 不改为 false | state 保留上一节点值并附加 network，online 不被失败路径改写 | 通过 |
| U017 | Grafana `/api/health` 正常，新 `/apis` 规则 API 可用 | 记录版本、新能力代际和当前告警摘要；所有请求为 GET | 3 个路径均 GET，bearer 正确，摘要为 critical/2 alerts | 通过 |
| U018 | Grafana 新规则 API 404、旧规则 API 正常 | 只在明确 404 后降级旧 API；不解析 HTML | 404-only fallback 通过，403 不调用 legacy | 通过 |
| U019 | Grafana 401/403 与连接拒绝 | 分别归类 auth 与 network，短说明不含 URL、Token、响应体 | 403 定向为 auth 且不 fallback；network 分类使用共享 HTTP 层覆盖，真实服务连接拒绝未执行 | 部分通过 |
| U020 | Portainer `/api/system/status` 404、`/api/status` 正常 | 记录旧能力代际与版本，继续只读摘要 | 记录 2.17.1 / legacy-status，所有请求 GET | 通过 |
| U021 | Portainer environments/stacks/container GET 摘要 | 正确聚合在线环境、运行/停止/失败容器和 stack 数 | 聚合 2 环境/1 在线/2 running/1 stopped/0 failed/2 stacks | 通过 |
| U022 | Portainer 环境数超过上限或请求预算 | 返回高基数/invalid_config，不静默截断 | 超限在 gateway 请求前拒绝；硬上限 7 保证每轮最多 10 GET | 通过 |
| U023 | HomeLab 摘要含控制字符、越界百分比/负计数/重复 ID | snapshot validation 拒绝，最近成功快照不被覆盖 | protocol/state 均拒绝，受拒轮次不提升 version | 通过 |
| U024 | 多个 HomeLab 连接器合并后超过 100 节点/32 服务，或不同连接器产生相同 ID | 整轮拒绝，已发布的其他连接器实体不被覆盖 | 60+50 节点和跨 owner ID 冲突均拒绝，原 60 节点/version 不变 | 通过 |
| U025 | Portainer environment 状态为离线 | 计入总数并标记 warning；不请求该 environment 的 Docker gateway | 离线 environment 计入 2 中的 1 个离线，其 gateway 未调用，服务 warning | 通过 |
| U026 | HomeLab `custom` base_url 使用 HTTPS RFC1918 IP；对照公网 Provider 使用同一 IP | HomeLab 三类通过；公网 Provider、链路本地和 metadata 地址仍拒绝 | 三类 HomeLab 通过 192.168.31.20 HTTPS；mock 对照仍拒绝 RFC1918，metadata 三类均拒绝 | 通过 |
| U027 | HomeLab 超过 4 节点或 Services 超过 5 项，且后部存在 critical 项 | 按 dwell 周期轮换有界窗口；critical 项稳定置顶并始终出现在首屏 | 6 节点在相邻 15 秒窗口显示 1/2、2/2；第 6 节点或服务变 CRIT 后回到 1/2 首屏并置顶 | 通过 |

## 2. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | Prometheus + node_exporter 正常 | 一轮内显示节点在线及 CPU/内存/磁盘 | TLS 受控 Prometheus 生成 nas-01 在线及 28/42/86；真实 node_exporter 未提供 | 部分通过 |
| E002 | 停止一个 node_exporter | 一个轮询周期内对应节点 critical，其他节点正常 | `up=0` 状态/最近值契约自动化通过；真实 exporter stop/start 未执行 | 部分通过 |
| E003 | 停止 Prometheus | HomeLab 数据 stale/connector error，Coding 页面继续更新 | 失败仅标注 owner HomeLab，Phase 3 E2E 中 Coding 独立；真实停服未执行 | 部分通过 |
| E004 | Grafana 只读 service account | 可读健康/告警摘要，任何写 API 未调用 | 受控 TLS 完成 bearer + 3 个 GET 审计；真实 service account 未提供 | 部分通过 |
| E005 | Portainer 只读 access token | 显示环境和容器摘要，无容器状态变化 | 受控 TLS 完成 `X-API-Key` + GET-only 摘要；Pi 无 Docker，真实 token 未提供 | 部分通过 |
| E006 | 100 个 HomeLab 节点模拟数据 | 远程端聚合/分页，单快照不超过目标体积 | 100 节点+32 服务且最长文本为 64,072 字节；UI 分别按 4/5 项有界子页轮换，critical 固定首屏 | 通过 |
| E007 | 一项严重 Grafana/Prometheus 告警 | Services 页面置顶并显示原因 | critical 告警聚合为 CRIT 并触发稳定页抢占；真实告警未注入 | 部分通过 |
| E008 | 本地受控 Prometheus/Grafana/Portainer HTTP 服务 | node 通过真实 HTTP 路径采集，Pi 只收到有界 1.1 当前摘要 | 三连接器走 TLS `httptest`；Mock HomeLab 经 scheduler→node API→syncclient→LKG→五页，Pi 实收 schema 1.1 | 通过 |
| E009 | 依次让三项受控服务返回 401、连接失败、404 兼容路径 | 对应服务卡片状态准确，Coding/API 仍继续更新 | 401/403、404-only fallback、network 保留上值和初次 AUTH 卡片自动化通过；真实三服务未执行 | 部分通过 |

## 3. 安全与性能测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| S001 | HTTP 请求审计 | 只出现允许的 GET/查询请求，无 POST/PUT/DELETE 状态修改 | 三连接器 TLS handler 逐请求断言 GET，无写接口 | 通过 |
| S002 | 日志扫描 Grafana/Portainer Token | 无完整 Token | 错误仅保留状态分类/短说明，HTTPError 不保留 URL/body/header；植入 Token 不出现于 error/snapshot | 通过 |
| P001 | 10 个固定 PromQL 并发轮询 | 查询数、超时和并发均在预算内，Pi 只收到聚合值 | 实现默认 7 条串行 instant query+1 buildinfo，每条服务端 timeout ≤30s，Pi 只收节点/服务摘要 | 通过 |
| P002 | 连续采集 HomeLab 当前指标并重启 daemon/Pi | 不生成 HomeLab 历史指标文件或数据库，Pi 快照仅包含最新聚合值 | E2E 多轮替换与 Mac/Pi 重启后仍只有 `last-known-good.json`，无 SQLite/历史文件 | 通过 |
| P003 | 100 节点/1000 series 恶意或错误查询响应 | 在远端上限处拒绝；序列化快照仍 ≤256 KiB，Pi 不接收大响应 | max+1 检测不静默截断；100+32 最大快照 64,072 字节；跨连接器超限整轮拒绝 | 通过 |
| P004 | 7 条 PromQL + Grafana + Portainer 一轮采集 | 全局连接器并发 ≤4；单连接器请求预算和 30 秒 deadline 生效 | scheduler 全局并发 ≤4；Prometheus 8、Grafana 3/4、Portainer 最多 10 GET；共享 task deadline ≤30s | 通过 |

## 4. 执行记录

2026-08-12 实施前基线：目标 Pi 只监听 SSH 22 且未安装 Docker；当前会话未获得真实
Prometheus、Grafana、Portainer 地址、版本或认证方式。实现先使用 `httptest` 受控服务覆盖正式
HTTP 路径；真实服务精确版本、认证方式与停服验收不得用夹具结果代替。临时服务、Mock、测试脚本
和测试数据在执行后清除。

2026-08-13 代码门禁：`make check`、全仓 race、Phase 3 关键包 race×10、前端语法、
`git diff --check`、`gofmt -l`、`go mod verify` 全部通过；6 平台×2 二进制共 12 产物
通过 checksum。真实 HomeLab 服务地址、CA、版本与只读 Token 仍未提供，E001–E005/E007/E009
的真实服务部分保持“部分通过”，不用受控服务伪写为通过。

2026-08-13 最终规格对账：补充超过可见容量时的 dwell 子页轮换以及 CRIT 稳定首屏回归；随后重新执行
`make check`、全仓 race、关键包 race×10 和 12 产物校验，全部通过。最终 Linux/ARM64 Display
候选 SHA-256 为 `e1163879a9be4b39b7cdfd850792d4155a195992e65d03b569e689a82a0cb795`，
在 DietPi 完成配置校验、可回滚替换和重启；service active、`NRestarts=0`，旧二进制保留为
`/usr/local/bin/homepi-display.pre-phase3-pagination`。
