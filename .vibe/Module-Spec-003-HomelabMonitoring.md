# Module Spec 003：HomeLab 监控

> 模块 ID：MOD-003  
> 所属阶段：Phase 3
> 版本：0.5
> 日期：2026-08-13
> 状态：已认证

## 1. 模块目标

复用 Module 001 的连接器框架，从 Prometheus、Grafana、Portainer 获取适合小屏摘要的 HomeLab 状态，并向 Module 002 提供稳定、低基数的页面模型。

## 2. 职责边界

- 负责只读查询、版本/能力探测、指标聚合、服务健康和告警摘要。
- 不负责部署或配置 Prometheus、Grafana、Portainer。
- 不负责修改容器、重启服务、确认 Grafana 告警或执行运维动作。
- 不保存或渲染完整时间序列；只提供当前小屏数值。后续如需短趋势，只能按需查询 Prometheus 固定窗口，结果不落盘。

## 3. Prometheus 连接器

Provider 类型 ID 为 `prometheus`。`base_url` 必须为 HTTPS origin；测试只允许通过注入的本地
HTTP client/server 使用 HTTP。可选 bearer token 由 `secret_ref` 延迟读取。连接器只调用
`GET /api/v1/query`，设置服务端 `timeout` 和 `limit` 参数，不使用 range query。

### 3.1 默认即时查询

| 指标 | 语义 | 查询策略 |
|---|---|---|
| target_up | 目标在线 | 仅按 `instance` 聚合 `up`，不硬编码 job |
| cpu_percent | CPU 使用率 | 对 node_exporter idle rate 取反 |
| memory_percent | 内存使用率 | available/total 计算 |
| disk_percent | 指定挂载点使用率 | avail/size 计算，排除虚拟文件系统 |
| network_rate | 收发速率 | rate 后按节点求和 |
| firing_alerts | 活跃告警数 | 固定 instant query 聚合 `ALERTS{alertstate="firing"}` |

默认查询只依赖 node_exporter 标准指标，不硬编码 job 名；管理员可通过 Provider `options` 的
固定键覆盖 `up/cpu/memory/disk/network_rx/network_tx/alerts` 查询。查询结果的实体标签默认
`instance`，可用 `entity_label` 修改。未知 option、空查询、控制字符或超长查询在启动前拒绝。

### 3.2 约束

- 仅允许管理员配置的查询模板，不接受来自 Pi/远程命令的任意 PromQL。
- 每个查询设置超时和结果数量上限。
- 首版只使用 instant query；若后续增加短趋势，只请求固定窗口、固定 step 且不持久化结果。
- 返回 series 过多时连接器报配置错误，不静默截断成误导值。
- 每轮最多 10 条查询；默认 7 条。单查询返回上限默认 64，配置最大值不得超过 1000。
- 所有必需查询构成一次原子采集；任一查询失败时保留上一轮 HomeLab 节点并附加分类错误。

## 4. Grafana 连接器

Provider 类型 ID 为 `grafana`，使用 bearer service account token。先读 `GET /api/health` 获取
数据库健康和版本，再读当前 Alertmanager 告警摘要；能力探测优先 Grafana 12+ `/apis` 规则资源，
仅在明确 404/unsupported 时降级旧 `/api/v1/provisioning/alert-rules`。所有请求均为 GET。

- 启动时探测版本和可用 API 代际。
- 获取实例健康、版本、告警规则状态摘要和可选文件夹/Dashboard 数量。
- 使用 service account token；权限仅限读取所需资源。
- 针对旧 `/api` 与新 `/apis` 使用明确兼容矩阵。
- API 不支持的指标显示 unavailable，不从 HTML 页面解析。

## 5. Portainer 连接器

Provider 类型 ID 为 `portainer`，access token 仅通过 `X-API-Key` 发送。每轮请求预算默认 10；
环境默认上限 4、硬上限 7，使 status + environments + stacks + 最多 7 个在线 gateway
始终不超过 10 个请求。超限时返回高基数/配置错误，不静默漏报。
容器统计只使用 GET Docker gateway。

- 使用 access token 调用官方 API。
- 首先探测版本；新版本优先 `/system/status`，旧版本按兼容表使用旧路径。
- 输出：实例健康、环境总数/在线数、运行容器数、停止/失败数、stack 摘要。
- 不通过 Portainer 网关执行会改变 Docker/Kubernetes 状态的请求。
- 只对 `Status=1` 的在线 environment 请求 Docker container gateway；离线 environment
  仍计入总数并使服务至少 warning，但不用必然失败的 gateway 请求扩大故障。
- 目标若启用自签证书，需安装受信 CA；默认禁止跳过 TLS 校验。

## 6. 页面模型

### 6.1 HomeLab 页面

每个节点卡片包含：节点名、在线状态、CPU、内存、磁盘、最严重状态。默认最多显示 4 个节点；更多节点用分页/轮播，而不是缩小字号。

### 6.2 Services 页面

服务卡片包含：名称、健康、版本和关键数量；观察新鲜度由统一同步页脚表达。严重告警置顶，正常服务按配置顺序显示。

## 7. 聚合与状态规则

- 节点 `up=0` 时整体 critical，其资源指标保留最后值并标为 stale。
- 磁盘 ≥85% warning，≥95% critical；阈值可配置。
- CPU/内存使用移动窗口或短期平均，避免单点尖峰持续变红。
- 服务认证失败属于 connector error，不等同于服务 down。
- Prometheus 无法访问时，不能推断所有目标 down。
- 每个连接器只替换自己所有的实体。全局合并后仍必须满足 100 节点 / 32 服务上限；
  跨连接器重复 ID 或总量越界时整轮拒绝，不覆盖其他连接器状态。
- 最严重状态按 critical > warning > stale > ok 聚合，但页面显示原因。

## 8. 认证与网络

- 所有 Token 保存在远端 `homepi-node` 所在主机的系统凭据库，不传给 Pi。
- Prometheus 若无原生认证，应通过反向代理/VPN 限制访问。
- Grafana/Portainer 使用只读最小权限 Token。
- `prometheus` / `grafana` / `portainer` 的 `custom` HTTPS 地址允许 RFC1918 私网 IP，
  因为 HomeLab 就是本地网服务；链路本地、云 metadata 和非 HTTPS 非 loopback 地址仍拒绝。
  该例外不适用于 Phase 1 公网 Provider 类型。
- 每个目标使用 URL 中的 hostname 做 TLS 名称校验，并使用 `homepi-node` 主机的系统信任库；
  自签证书必须先把 CA 安装到该主机的信任库。首版不开放连接器级 CA bundle、server name
  覆盖或跳过校验选项；连接超时可配置。
- 禁止 `insecure_skip_verify` 作为默认值。

## 9. 性能

- 默认轮询：Prometheus 15–30 秒；Grafana/Portainer 60 秒。
- 每轮 Prometheus 查询数应受固定预算限制，首版目标 ≤10。
- 查询在远程节点完成，Pi 不直接连接各 HomeLab 服务。
- 页面快照只传聚合结果，避免传递大时间序列。
- daemon 与 Pi 均不保存 HomeLab 历史指标。

## 10. 验收标准

- Prometheus 目标离线后一个轮询周期内状态变化，恢复后自动转绿。
- Grafana/Portainer 认证错误与服务离线在 UI 上可区分。
- 1000+ series 的错误查询被保护机制拒绝或在服务端聚合，不传到 Pi。
- 单个 HomeLab 服务不可达不影响 Coding 页面。
- 所有连接器只进行 GET/只读查询，测试可证明没有状态修改调用。
