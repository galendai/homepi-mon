# Phase 3 多页面与 HomeLab 实施记录

> 文档 ID：IMPL-006
> 范围：P3-01 至 P3-04
> 日期：2026-08-12
> 状态：代码与可本机执行门禁完成，等待用户验收

## 1. 目标与成功标准

本轮在不改变单 node、凭据仅留在远端主机、Pi 只保存当前脱敏快照、无本地输入和无历史指标的
边界下，完成五页自动轮播以及 Prometheus、Grafana、Portainer 当前状态摘要。

成功标准：

1. `CODING / API / HOMELAB / SERVICES / SYSTEM` 五页按持久配置轮播；启动先显示 `CODING`。
2. 严重告警只临时抢占页面；解除后恢复原页面、原轮播位置和剩余停留时间。
3. HomeLab 连接器仅执行固定、有预算的 GET/instant query；不接受 Pi 或远程命令传入查询。
4. Prometheus 不可达时保留节点最后值并标记 stale/error，不把所有节点伪报为 down。
5. Grafana 与 Portainer 的认证失败、服务离线、API 不支持三类状态可区分。
6. Pi 快照只包含有界当前摘要，不包含 Token、原始响应、完整时间序列或历史记录。
7. 定向测试、`make check`、全仓 race、格式、diff、适用交叉构建与 60×20 双主题检查通过。
8. 形成可复现的 Mac→DietPi 五页轮播/降级验收步骤；未执行的真实 HomeLab 或物理屏步骤明确保留。

## 2. 实施前基线与假设

- 用户通过 `/goal` 明确授权进入并完成 Phase 3；这不等同于批准 Git commit，也不把 Phase 2
  `verification: pending` 改写为已验收。
- 2026-08-12 自动化基线 `make check` 全部通过，工作树开始时干净。
- 正式 node 为 macOS darwin/arm64 `homepi-node 0.1.0`，Keychain 后端、5 个 Provider、1 个 Display；
  Pi 为 Debian 12/ARM64，当前仅监听 SSH 22，未安装 Docker，也未发现本机 Prometheus、Grafana、
  Portainer 监听端口。
- 当前会话没有获得真实 Prometheus/Grafana/Portainer 地址、版本或认证方式。因此先完成官方 API
  契约和本地受控服务 E2E；真实服务版本/认证对账仍必须在 P3-04 实际结果中如实记录，不能用夹具冒充。
- Grafana 12+ 的新一代 API 使用 `/apis`，旧 `/api` 仍需兼容；Portainer 使用 `X-API-Key`；
  Prometheus 只使用 `/api/v1/query` instant query。

## 3. 冻结的数据与配置契约

### 3.1 快照

`MetricSnapshot` 增加可选 `homelab_nodes` 与 `homelab_services`，schema 升为向后兼容的 `1.1`。
旧客户端可忽略新增字段；ProviderMetric 既有枚举不扩张。每个实体只保存当前值、观察时间、状态、
错误分类和短脱敏说明。

- 节点：ID、名称、在线状态、CPU/内存/磁盘百分比、收发速率、观察时间、状态。
- 服务：ID、名称、类型、版本、健康、告警数、环境/容器/stack 计数、观察时间、状态。
- node 当前状态按实体 ID 替换；连接器失败时保留上一成功值并附加错误，不生成历史样本。

### 3.2 node Provider 配置

新增类型：`prometheus`、`grafana`、`portainer`。三者继续复用现有 `base_url`、`interval`、
`stale_after`、`secret_ref` 和配置事务；额外参数只允许进入 `options` 白名单。

- Prometheus：默认 7 条标准 node_exporter instant PromQL；允许管理员按固定键覆盖；查询预算 ≤10，
  单查询 series 上限默认 64、硬上限 1000，实体标签默认 `instance`。
- Grafana：健康、当前告警、规则能力探测；只读 service account bearer token；新 `/apis` 失败为
  404/unsupported 时才降级旧 `/api`。
- Portainer：`/api/system/status`，404 时降级 `/api/status`；读取 environments、stacks 和容器列表；
  使用 `X-API-Key`，请求和环境数量受固定预算限制。

所有 `base_url` 必须是 HTTPS；仅测试注入的本地 `httptest` 地址可使用 HTTP。默认验证证书，
不提供 `insecure_skip_verify`。

### 3.3 Display 配置

Display 环境增加：

- `HOMEPI_PAGE_ORDER=CODING,API,HOMELAB,SERVICES,SYSTEM`，五页必须恰好各出现一次。
- `HOMEPI_PAGE_DWELL_SECONDS=CODING:15,API:15,HOMELAB:15,SERVICES:15,SYSTEM:15`，每页 5–300 秒。

Web Admin Display 草稿、候选校验、SSH 原子应用和回滚共同使用该契约；值不进入命令行。

## 4. 里程碑

### M3.1：五页路由

产出：五页确定性 ASCII/rich 画面、配置解析、轮播状态机、严重告警抢占/恢复测试。
手动测试：使用 5 秒停留配置观察五页顺序；注入/解除 critical 后确认返回原页。

### M3.2：Prometheus

产出：固定 instant query、节点聚合、预算/高基数/超时/认证/不可达回归。
手动测试：停一个 exporter 只影响对应节点；停 Prometheus 时 Coding 继续、HomeLab 为 stale/error。

### M3.3：Grafana 与 Portainer

产出：版本能力探测、告警/环境/容器/stack 摘要、只读请求审计和错误分类。
手动测试：分别使用有效只读 Token、错误 Token 和离线服务，不调用任何状态修改 API。

### M3.4：Phase 3 门禁

产出：全回归、race、性能/快照体积、五页 60×20/ASCII/rich、交叉构建和 DietPi 记录。
手动测试：按 `.vibe/Test-Module-002-TUIDashboard.md` 与
`.vibe/Test-Module-003-HomelabMonitoring.md` 的 Phase 3 用例复现。

## 5. 变更纪律

- 先更新本文件、HL/Module/UI/Test 规格，再实现代码和测试。
- 不修改 Phase 4 命令协议，不引入本地按键/触摸，不创建历史数据库。
- 不把测试 Token 写入仓库、argv、日志、响应或证据文件；测试服务与临时文件执行后清理。
- Phase 3 达到代码层 `DONE/95%/verification pending` 后暂停；用户检查前不提交，也不进入 Phase 4。

## 6. 实际结果

### 6.1 交付内容

- 快照 schema 升为 1.1，增加有界 `homelab_nodes` / `homelab_services`，支持逐连接器
  原子替换、失败标注和离线节点最近成功资源值保留。合并后上限为 100 节点/
  32 服务，跨连接器 ID 冲突与越界整轮拒绝。
- Display 交付五页 ASCII/rich 渲染、顺序/停留配置、CRIT-only 抢占和解除后恢复
  原页/原位置/剩余 dwell。Web Admin、Display profile、SSH candidate/test/apply/rollback 共用
  `HOMEPI_PAGE_ORDER` 和 `HOMEPI_PAGE_DWELL_SECONDS` 契约。HomeLab/Services 超出 4/5 项时按
  dwell 轮换有界子页，CRIT 实体稳定置顶并固定首屏。
- Prometheus 交付 7 条固定 instant query、服务端 timeout 和不可静默截断的 series 上限；
  Grafana 交付 health/alert/modern `/apis` + 404-only legacy 探测；Portainer 交付
  `X-API-Key`、modern/legacy status、environment/stack/在线 environment 容器摘要。三者均只发 GET。
- Mock 夹具扩展 HomeLab 当前摘要，用于 scheduler→node API→sync client→单快照落盘→
  五页渲染的受控端到端回归。

### 6.2 自动化与发布门禁

| 输入/范围 | 预期 | 实际输出 | 结果 |
|---|---|---|---|
| `make check` | gofmt、vet、全部 unit/contract/E2E 通过 | 全部 Go 包通过 | 通过 |
| `go test -race -count=1 ./...` | 无数据竞争 | 全仓通过 | 通过 |
| Phase 3 关键包 `-race -count=10` | 连续 10 轮无竞争/间歇失败 | E2E/UI/三连接器/state/displaydeploy 通过 | 通过 |
| 前端语法、`git diff --check`、`gofmt -l`、`go mod verify` | 无语法/格式/依赖问题 | 全部通过 | 通过 |
| `make checksums VERSION=0.1.0` | 6 平台×2 二进制、SHA 自校验 | 精确 12 产物通过 | 通过 |
| 100 节点+32 服务，字段使用最大允许文本 | 快照 ≤256 KiB | 64,072 字节 | 通过 |
| 6 节点/6 服务子页轮换，末项为 CRIT | 超限实体可轮换；CRIT 始终出现在首屏 | 15 秒相邻窗口 1/2→2/2；CRIT 后固定 1/2 并置顶 | 通过 |
| 五页 normal/empty/stale/auth/critical/恶意文本、ASCII/rich | 每帧 60×20，ASCII 7-bit，rich 只用允许 SGR/字形 | 双主题全部通过，旧 overview golden 无回归 | 通过 |

### 6.3 Mac→DietPi 实机

- Mac `homepi-node` 已使用可回滚本地安装流程升级并重启 LaunchAgent；健康接口返回
  `status=ok`，正式配置继续使用 Keychain、5 个既有 Provider 和 1 个 Display。
- Pi ARM64 候选先完成 SHA 和 `config validate`，再保留
  `/usr/local/bin/homepi-display.pre-phase3` 与 `/etc/homepi-display/environment.pre-phase3`后替换。
  最终 service active、`NRestarts=0`，仅 TCP 22 监听，候选临时文件已清理。
- 最终规格对账后的 Display 候选 SHA-256 为
  `e1163879a9be4b39b7cdfd850792d4155a195992e65d03b569e689a82a0cb795`；DietPi 再次完成
  配置校验、可回滚替换和重启，`NRestarts=0`，替换前二进制保留为
  `/usr/local/bin/homepi-display.pre-phase3-pagination`。
- 5 秒压缩配置实测顺序为 `CODING→API→HOMELAB→SERVICES→SYSTEM→CODING`；
  结束后正式配置恢复为每页 15 秒。Pi 已从新 Mac epoch 收到 schema 1.1 快照。
- 5 分钟高频轮播取样 31 次：最大 RSS 13,064 KiB，平均 CPU 0.52%，峰值 0.6%；
  低于 120 MiB / 5% 门槛。同步年龄在 Phase 3 按分钟变化，1 秒轮播 tick 不会每秒重写相同帧。
- 480×320 RGB565 framebuffer 及 60×20 字符缓冲结构、色彩和页脚正常，证据：
  `.vibe/evidence/Phase3-Pi-Homelab.png`。

### 6.4 未执行的外部/手动验收

本轮未获得真实 Prometheus、Grafana、Portainer 地址、精确版本、受信 CA 和只读凭据；
目标 Pi 也未安装 Docker。因此 exporter 停止、三服务停服/错误 Token 和物理 LCD 用户肉眼验收
仍为 pending。受控 TLS HTTP 服务覆盖真实路径、方法、header、schema、404 兼容和 401/403 分类，
但不替代上述真实服务对账。未将这些项伪写为通过。
