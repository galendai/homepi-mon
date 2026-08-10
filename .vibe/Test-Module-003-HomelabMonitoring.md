# 模块 003 测试文档：HomeLab 监控

> 对应规格：Module-Spec-003-HomelabMonitoring.md  
> 状态：测试设计已认证，尚未实现/执行

## 1. Unit Test

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| U001 | node_exporter idle rate=0.72 | CPU 使用率标准化为 28% | 未执行 | 待执行 |
| U002 | MemTotal 与 MemAvailable | 正确计算内存使用率，避免使用 free 字段误判缓存 | 未执行 | 待执行 |
| U003 | 根分区使用率 86%/96% | 分别为 warning/critical | 未执行 | 待执行 |
| U004 | `up=0` 且存在旧资源指标 | 节点 critical，旧指标 stale | 未执行 | 待执行 |
| U005 | Prometheus 本身认证失败 | connector error，不把所有目标标为 down | 未执行 | 待执行 |
| U006 | 查询返回超过 series 上限 | 返回配置/高基数错误，不静默截断 | 未执行 | 待执行 |
| U007 | Grafana 新 API 可用 | 使用新 API 并记录版本/能力 | 未执行 | 待执行 |
| U008 | Grafana 仅支持旧 API | 按兼容矩阵降级，不抓取 HTML | 未执行 | 待执行 |
| U009 | Portainer 新版 `/system/status` | 正确解析健康和版本 | 未执行 | 待执行 |
| U010 | Portainer 旧版仅 `/status` | 经能力探测使用兼容路径并标明版本 | 未执行 | 待执行 |
| U011 | 自签证书未配置 CA | 连接失败并提示 CA 问题，不跳过 TLS | 未执行 | 待执行 |
| U012 | CPU 瞬时 100% 后恢复 | 短期聚合避免单点长期 critical | 未执行 | 待执行 |

## 2. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | Prometheus + node_exporter 正常 | 一轮内显示节点在线及 CPU/内存/磁盘 | 未执行 | 待执行 |
| E002 | 停止一个 node_exporter | 一个轮询周期内对应节点 critical，其他节点正常 | 未执行 | 待执行 |
| E003 | 停止 Prometheus | HomeLab 数据 stale/connector error，Coding 页面继续更新 | 未执行 | 待执行 |
| E004 | Grafana 只读 service account | 可读健康/告警摘要，任何写 API 未调用 | 未执行 | 待执行 |
| E005 | Portainer 只读 access token | 显示环境和容器摘要，无容器状态变化 | 未执行 | 待执行 |
| E006 | 100 个 HomeLab 节点模拟数据 | 远程端聚合/分页，单快照不超过目标体积 | 未执行 | 待执行 |
| E007 | 一项严重 Grafana/Prometheus 告警 | Services 页面置顶并显示原因 | 未执行 | 待执行 |

## 3. 安全与性能测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| S001 | HTTP 请求审计 | 只出现允许的 GET/查询请求，无 POST/PUT/DELETE 状态修改 | 未执行 | 待执行 |
| S002 | 日志扫描 Grafana/Portainer Token | 无完整 Token | 未执行 | 待执行 |
| P001 | 10 个固定 PromQL 并发轮询 | 查询数、超时和并发均在预算内，Pi 只收到聚合值 | 未执行 | 待执行 |
| P002 | 连续采集 HomeLab 当前指标并重启 daemon/Pi | 不生成 HomeLab 历史指标文件或数据库，Pi 快照仅包含最新聚合值 | 未执行 | 待执行 |

## 4. 执行记录

尚未执行。实现后记录 Prometheus、Grafana、Portainer 和 exporter 的精确版本。临时容器、Mock、测试脚本和测试数据在执行后清除。
