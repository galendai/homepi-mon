# Module Spec 003：HomeLab 监控

> 模块 ID：MOD-003  
> 版本：0.2  
> 状态：已认证

## 1. 模块目标

复用 Module 001 的连接器框架，从 Prometheus、Grafana、Portainer 获取适合小屏摘要的 HomeLab 状态，并向 Module 002 提供稳定、低基数的页面模型。

## 2. 职责边界

- 负责只读查询、版本/能力探测、指标聚合、服务健康和告警摘要。
- 不负责部署或配置 Prometheus、Grafana、Portainer。
- 不负责修改容器、重启服务、确认 Grafana 告警或执行运维动作。
- 不保存或渲染完整时间序列；只提供当前小屏数值。后续如需短趋势，只能按需查询 Prometheus 固定窗口，结果不落盘。

## 3. Prometheus 连接器

### 3.1 默认即时查询

| 指标 | 语义 | 查询策略 |
|---|---|---|
| target_up | 目标在线 | 对配置 job/instance 聚合 `up` |
| cpu_percent | CPU 使用率 | 对 node_exporter idle rate 取反 |
| memory_percent | 内存使用率 | available/total 计算 |
| disk_percent | 指定挂载点使用率 | avail/size 计算，排除虚拟文件系统 |
| network_rate | 收发速率 | rate 后按节点求和 |
| firing_alerts | 活跃告警数 | 查询 `/api/v1/alerts` 或规则指标 |

精确 PromQL 在实现阶段按用户标签结构配置，不在代码中假定 job 名。

### 3.2 约束

- 仅允许管理员配置的查询模板，不接受来自 Pi/远程命令的任意 PromQL。
- 每个查询设置超时和结果数量上限。
- 首版只使用 instant query；若后续增加短趋势，只请求固定窗口、固定 step 且不持久化结果。
- 返回 series 过多时连接器报配置错误，不静默截断成误导值。

## 4. Grafana 连接器

- 启动时探测版本和可用 API 代际。
- 获取实例健康、版本、告警规则状态摘要和可选文件夹/Dashboard 数量。
- 使用 service account token；权限仅限读取所需资源。
- 针对旧 `/api` 与新 `/apis` 使用明确兼容矩阵。
- API 不支持的指标显示 unavailable，不从 HTML 页面解析。

## 5. Portainer 连接器

- 使用 access token 调用官方 API。
- 首先探测版本；新版本优先 `/system/status`，旧版本按兼容表使用旧路径。
- 输出：实例健康、环境总数/在线数、运行容器数、停止/失败数、stack 摘要。
- 不通过 Portainer 网关执行会改变 Docker/Kubernetes 状态的请求。
- 目标若启用自签证书，需安装受信 CA；默认禁止跳过 TLS 校验。

## 6. 页面模型

### 6.1 HomeLab 页面

每个节点卡片包含：节点名、在线状态、CPU、内存、磁盘、最严重状态。默认最多显示 4 个节点；更多节点用分页/轮播，而不是缩小字号。

### 6.2 Services 页面

服务卡片包含：名称、健康、版本、关键数量和最后检查时间。严重告警置顶，正常服务按配置顺序显示。

## 7. 聚合与状态规则

- 节点 `up=0` 时整体 critical，其资源指标保留最后值并标为 stale。
- 磁盘 ≥85% warning，≥95% critical；阈值可配置。
- CPU/内存使用移动窗口或短期平均，避免单点尖峰持续变红。
- 服务认证失败属于 connector error，不等同于服务 down。
- Prometheus 无法访问时，不能推断所有目标 down。
- 最严重状态按 critical > warning > stale > ok 聚合，但页面显示原因。

## 8. 认证与网络

- 所有 Token 保存在远端 `homepi-node` 所在主机的系统凭据库，不传给 Pi。
- Prometheus 若无原生认证，应通过反向代理/VPN 限制访问。
- Grafana/Portainer 使用只读最小权限 Token。
- 每个目标可配置 CA bundle、server name 和连接超时。
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
