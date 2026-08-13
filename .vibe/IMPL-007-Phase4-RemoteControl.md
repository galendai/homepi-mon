# Phase 4 远程显示控制实施记录

> 文档 ID：IMPL-007
> 范围：P4-01 至 P4-03
> 日期：2026-08-13
> 状态：代码与实机门禁完成，用户验收待确认

## 1. 目标与成功标准

本轮在不增加 Pi 入站监听、不开放任意 Shell/文件/主机管理能力、不改变单 node 绑定和 Provider
凭据边界的前提下，完成远端主机到 Display 的允许列表 UI 命令通道。

成功标准：

1. 管理员只能从远端主机本机使用 `homepi-node remote` 发布命令；控制面使用独立本机凭据，
   不复用下发到 Pi 的设备 Token。
2. daemon 为命令生成 UUID、目标、签发/过期时间和持久单调 sequence，经既有已认证 WebSocket
   发送；Pi 不新增 HTTP、WebSocket 或其他入站端口。
3. Display 按身份、目标、时效、sequence、允许 kind 和严格参数 schema 校验；拒绝未知字段、
   控制字符、越界值、错设备、过期命令和重放。
4. `show_page`、上一页、下一页、临时轮播开关/间隔、受限消息和刷新均有真实效果；亮度在未配置
   支持器时明确返回 `unsupported_capability`，不得假成功。
5. CRIT 告警优先于远程覆盖，远程覆盖优先于自动轮播；覆盖到期后恢复此前页面、轮播位置和
   剩余停留时间。
6. Pi 持久保存有界幂等结果和 sequence 高水位；相同 `command_id` 重放十次不重复执行，重启后
   仍返回原结果。
7. daemon 与 Pi 审计只记录命令元数据、结果、耗时以及消息长度/哈希；不记录完整消息、Provider
   Key、设备 Token 或控制凭据。
8. 聚焦测试、全仓/race、双主题 60×20、12 产物、Mac→DietPi 实机命令和端口/日志扫描完成；
   用户肉眼确认项单独保留为 `verification: pending`。

## 2. 实施前基线与授权

- 用户通过 `/goal` 明确要求完成 Phase 4，授权进入 P4-01..P4-03；这不等于授权 Git commit。
- 工作树开始时干净，HEAD 为 `68abb88 feat: add Phase 3 HomeLab monitoring`。
- `make check` 基线通过；Phase 3 代码门禁已完成，但物理 LCD 肉眼与真实
  Prometheus/Grafana/Portainer 故障矩阵仍等待用户验收，本轮不得把它们改写为通过。
- 正式链路仍为一台 macOS node 和一台 `dietpi` Display；Pi 当前只需主动连接 node 并监听既有
  SSH 维护端口 22。

## 3. 冻结的控制面与传输合同

### 3.1 管理员入口

公共入口为：

```text
homepi-node remote --device <id> show-page [--duration 30s] <page>
homepi-node remote --device <id> next-page|previous-page [--duration 30s]
homepi-node remote --device <id> set-rotation [--interval 15s] [--duration 5m] <on|off>
homepi-node remote --device <id> refresh [connector-id ...]
homepi-node remote --device <id> show-message --severity <info|warning|critical> --duration <5s..300s> <text>
homepi-node remote --device <id> set-brightness <0..100>
```

CLI 只连接当前配置的 node listener，并要求请求源为 node 本机。控制 API 使用 secret store 中独立的
`keyring:homepi-remote-control` 随机凭据；设备 Token 只用于 Display 的 snapshot/stream，不能发布
命令。控制 API 不支持 CORS、浏览器调用或非本机来源。

### 3.2 命令队列与恢复

- daemon 在本机数据目录原子保存一个 `0600` 有界命令状态文件，包含 sequence 高水位、未过期命令
  和脱敏结果；最多保留 64 个待处理命令和 256 个最近结果。
- 重连时只重发未过期且没有最终结果的命令；daemon 重启后从状态文件恢复。过期命令直接记录
  `expired`，不再下发。
- 队列满时优先淘汰最旧 `normal` 命令并记录 `overflow`；不得淘汰已经 accepted 的命令或让
  high/emergency 绕过 schema/身份校验。
- daemon 每次发布分配持久单调 sequence。Pi 保存最近 sequence 高水位；已知 command ID 先返回
  原结果，未知且 sequence 不大于高水位时返回 `rejected_out_of_order`。
- daemon 队列与 Pi 账本通过同目录临时文件和原子 rename 保证正常进程/service 重启可见；实机
  测得逐命令 file/directory fsync 在 Mac/Pi 命令链路上分别造成约 1.1 秒和 1.5–1.8 秒阻塞，
  因此为满足 UI 时延不强制这两类 fsync。突然断电可能丢失最后一条内核缓冲记录，Phase 4 不承诺
  断电级 exactly-once。

### 3.3 参数边界

- page 只允许 `CODING/API/HOMELAB/SERVICES/SYSTEM`。
- 页面覆盖和消息 duration 为 5–300 秒；页面覆盖默认 30 秒。轮播配置覆盖默认 5 分钟。
- rotation interval 为 5–300 秒，只临时覆盖五页 dwell；到期恢复持久 Display 配置。
- message 最多 116 个 7-bit 可打印字符；拒绝 ESC、BEL、CR/LF、NUL、其他控制字符和未知字段；
  不解析 ANSI、Markdown、URL 或命令模板。
- connector ID 最多 16 个，每个只允许既有安全 ID 字符；空列表表示刷新全部启用连接器。
- brightness 为 0–100；未注入受支持的背光驱动时返回 `failed/unsupported_capability`。
- issued_at 最多允许比本机时间快 30 秒；expires_at 必须晚于 issued_at，且命令最大传输 TTL 为
  5 分钟。显示 duration 与传输过期是两个独立字段。

### 3.4 ACK 与刷新

- 校验通过后 Display 先发送 `accepted`，UI 状态更新后发送 `executed`；校验失败只返回一个
  `rejected` 或 `expired` 最终结果。
- `refresh_data` 收到 `accepted` 后由 daemon 对允许的 connector 触发一次受既有并发/超时限制的
  即时采集；Display 收到命令后首个更新快照再返回 `executed`。上游失败通过 connector health
  进入该快照，不伪装为刷新成功的指标值。
- 每个结果包含 command ID、status、稳定 error code、接收/完成时间和耗时；不包含内部堆栈。

## 4. UI 仲裁合同

优先级固定为：本地计算的 CRIT 页面 > 有效远程消息 > 有效远程页面/轮播覆盖 > 自动轮播。

- 页面覆盖冻结原轮播剩余时间；到期后从原页面和剩余 dwell 继续。
- CRIT 在远程页面覆盖期间出现时立即抢占；CRIT 解除后，如果远程覆盖仍有效则回到远程页，否则
  回到原轮播位置。
- 远程消息只替换数据区顶部三行，显示 `REMOTE NOTICE`、严重度、来源和倒计时；消息到期或被更高
  优先级覆盖后不残留。
- 远程命令不修改 `/etc/homepi-display/environment`；持久页面顺序/dwell 仍只由 Phase 2 固定 SSH
  配置事务管理。

## 5. 里程碑

### M4.1：命令协议与安全校验

产出：协议类型、严格解码、独立本机控制凭据、持久队列、WebSocket 投递、ACK/结果与攻击回归。
手动测试：错设备、过期、未知 kind、未知字段、ANSI/控制字符和重复 ID 均返回稳定错误码。

### M4.2：UI 仲裁与幂等

产出：Router 临时覆盖、消息三行渲染、刷新、可选亮度、Pi 持久幂等和重启恢复。
手动测试：切页/消息 1 秒内可见；CRIT 抢占；覆盖到期恢复原页；重放十次只执行一次。

### M4.3：发布门禁

产出：全回归、race、双主题/恶意文本、12 产物、Mac→DietPi、断网重连、重启重放、端口与秘密扫描，
并在 README 与测试文档提供操作/回退说明。

## 6. 变更纪律

- 先更新本文件、HL/PRD/Module/UI/Test/Development Plan，再实现代码和测试。
- 只触及 Phase 4 所需协议、node 控制面、同步客户端、Display 状态机、UI 和文档；不重构相邻模块。
- 测试临时控制凭据、状态目录、证书和夹具全部使用测试临时目录并由测试清理。
- Phase 4 达到代码层 `DONE/95%/verification pending` 后暂停；用户检查前不提交。

## 7. 实际结果

P4-01..P4-03 已完成至 `DONE/95%/verification pending`：协议、独立本机凭据、持久有界队列、
WebSocket ACK、Display 幂等/UI 仲裁、刷新闭环、双主题 60×20、README/回退说明均已交付。

最终门禁为 `make check`、全仓 race、关键包 race×10、`go mod verify`、`git diff --check` 和六目标×
两程序共 12 个发布产物全部通过。Mac node 与 DietPi Display 已部署当前工作树构建；show_page
sequence=9 约 0.16 秒 executed，show_message 签发到 Pi 执行约 0.38 秒且 Pi 内部 1 ms，指定
deepseek-main 刷新在新快照后 executed，brightness 明确 failed/unsupported_capability。Display
离线 6 秒时的 5 秒 TTL 命令为 expired；两端 service 重启后 sequence/结果继续恢复。

Pi 仅监听 SSH 22，Pi→node 控制路由为 403，两端状态文件均为 `0600`，通知全文/凭据扫描通过。
Pi 保留 `/usr/local/bin/homepi-display.pre-phase4-20260813` 等明确回退副本；Mac 安装器保留
`~/.local/bin/*.previous`。当前真实 Codex CRIT 按规格压过远程页/消息，所以解除 CRIT 后的物理屏
肉眼确认仍为用户手动项；未把它写成通过，也未执行 Git commit。
