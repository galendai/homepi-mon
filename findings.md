# 调研发现

## 本地项目
- 当前实机三屏由 `homepi-display run -pages HOME,CODING,API` 形成：HOME、TOKEN PLAN、API USAGE；部署 drop-in 不在仓库。
- 技术栈为 Go 1.26.5 + cgo + LVGL 9.6 + fbdev，480×320 RGB565；非 Linux/cgo 为 60×20 legacy。
- 数据链路是 `Connector → Scheduler → state.Current → WebSocket snapshot_full → syncclient → UI/LVGL`；服务端约 1 秒检测版本，Display 合并窗口 200 ms，无需新增 Display 轮询。
- `HomeLabService` 只有健康、版本和容器/告警等字段，无法表达代理节点、延迟、当前选择和活动连接，需新增 OpenClash 专有快照模型。
- Connector 单次任务超时 30 秒、Provider 最小采集间隔 5 秒；Connector 不自行重试，错误由 Scheduler 分类、退避并保留 last-known-good。
- 现有外部采集器都是只读 HTTP；`secret_ref` 使用 keyring。配置校验需将 `openclash` 加入私网 base_url 白名单，并专门处理当前“非回环 HTTP 一律拒绝”的规则。
- 原生第四屏需扩展 `internal/ui`、`pixel_screen.go`、`guirender` Go/C 桥接；只做 legacy 虽改动少，但不符合当前三屏原生卡片体验。
- 新页面应仿照 HOME 特判 `ParsePage`/dwell，不应直接扩充 `allPages`，以免既有五页持久轮播配置被判定无效；远程切页和 Web Admin 页面选项也需同步。

## side-router 与 OpenClash
- SSH 实测可访问 `side-router`，BatchMode 成功。
- 系统：OpenWrt 24.10.1，`bcm27xx/bcm2711`，`aarch64_cortex-a72`；内核 `6.6.90`。
- SSH 身份为 root，但本调研仅执行只读查询，不利用写权限。
- 已确认存在 `uci`、`curl`、`wget`、`ubus`、`jsonfilter`，可支持只读 UCI 与 HTTP API 调查。
- 实测安装包：`luci-app-openclash 0.48.002`；核心进程 `/etc/openclash/clash -d /etc/openclash -f /etc/openclash/sui.yaml`。
- UCI 白名单字段：`enable=1`、`core_version=linux-arm64`、`en_mode=fake-ip-tun`、`proxy_mode=rule`；LAN IP 为 `192.168.31.2`。
- Clash API 监听 `*:9090`，`/version` 返回 200：`meta=true`、核心版本 `alpha-gbc76c13`。
- API 已配置 Bearer secret；调研命令仅在远端 shell 内从 UCI 读取并用于请求，没有输出或复制 secret。
- 使用远端回环地址 `127.0.0.1:9090` 的 GET `/proxies` 返回 200，响应 18,292 字节，实测约 4 ms。
- jsonfilter 字段摘取和首版 Ruby 汇总命令出现转义/引号错误，未修改任何远端状态；后续改用无嵌套单引号的 Ruby 汇总。
- `/proxies` 实测共 36 个对象：6 个 Selector、5 个 URLTest、19 个订阅节点、6 个内置 Direct/Reject/Pass 等对象。
- 当前主选择组 `🚀 PROXY` 指向 `YINNET-HY2`；全局 `GLOBAL` 为 `DIRECT`，实际规则出口应以具体选择组而非 GLOBAL 判断。
- 19 个订阅节点均有历史测速记录，12 个 `alive=true`、7 个 `alive=false`；延迟值含 0（失败）到 1403 ms。
- 路由器 UTC 为 2026-11-25 12:07:59，而最新测速时间为 2026-11-24 19:00:21，已有约 17 小时，说明只轮询 `/proxies` 不能满足实时性，必须主动测速或明确展示陈旧状态。
- `/providers/proxies` 返回 11 个 Provider，但包含 default、选择组与自动测速组的重复视图，不宜直接当作订阅列表；`updatedAt` 均为零值，且无 `subscriptionInfo`。
- `/connections` 实测 62 条活动连接，按 `chains[0]` 聚合为 `YINNET-HY2=46`、`DIRECT=16`，可用于展示节点实时在用连接数，无需呈现源/目的 IP。
- 连接响应还提供每连接 upload/download 和总量计数，但第一版无需采集隐私敏感 metadata。
- 从开发机直连 `192.168.31.2:9090/proxies` 不带认证返回 401，约 9 ms；API 认证有效。
- 对当前出口 `YINNET-HY2` 执行一次 GET `/proxies/YINNET-HY2/delay?timeout=5000&url=...generate_204`，返回 `{"delay":202}`，SSH 往返约 723 ms。
- 随后 `/proxies` 中该节点 `alive=true`，最新测速时间更新为 `2026-11-25T12:11:12.712178064Z`；证明无需改配置即可主动刷新延迟，但该接口会产生一次真实代理网络探测并更新内存历史。

## 方案依据
### 总体决策
- 在 `homepi-node` 新增 `openclash` Connector，复用 Scheduler、统一快照、WebSocket、Display 去重和错误退避；Display 不直接访问路由器。
- 传输采用开发机已实测可达的 `http://192.168.31.2:9090`，Bearer secret 由 homepi-node 的 `secret_ref=keyring:...` 提供；不进入快照、日志或 Display。
- OpenClash 专用配置校验只允许字面私网/回环 IP 使用 HTTP，主机名和其他 Provider 仍按原规则要求 HTTPS；不在全局放宽 SSRF。
- 新快照 `openclash` 为 additive optional 字段，wire schema 从 1.2 升至 1.3；使用专有结构而非硬塞 HomeLabService/ProviderMetric。

### 采集与实时性
- 每 5 秒 GET `/proxies` 与 `/connections`，更新选择关系、节点集合、活动连接数；建议 `stale_after=20s`。
- 每次状态采集后轮换测 2 个真实节点，GET `/proxies/{name}/delay?timeout=5000&url=http%3A%2F%2Fwww.gstatic.com%2Fgenerate_204`；最多 2 并发，遵守 30 秒采集任务 context，不做 Connector 自有重试。当前 19 节点约 50 秒一轮，单轮集中测速的 25 秒阻塞被消除。
- 节点来源为 `/proxies` 中无 `all` 选项的对象，排除 DIRECT/REJECT/PASS 等内置对象；新协议类型可直接接受。`/providers/proxies` 不进入运行时链路，避免重复 Provider 视图。
- 主选择组由 `options.selector` 指定，当前配置为 `🚀 PROXY`；沿 Selector/URLTest 的 `now` 链解析到真实节点，带循环检测。活动连接仅使用 `chains[0]` 聚合，不保留源/目的 metadata。
- 按 checked_at 选择最久未测的 2 个节点，新节点（零值）优先；其余节点缓存最近一次主动测速的 alive/delay/checked_at。绝不把 Mihomo 旧 history 当作实时测速。
- 单节点超时/失败：该节点 `alive=false`、delay=null、checked_at=当前时间，不影响其他节点。401/403/429 向上抛出，让 Scheduler 走认证/限流退避。
- 19 节点、约 50 秒一轮约为 32,832 个 204 请求/天。`delay_stale_after` 按节点数和采集间隔动态计算为约 2 个完整测速周期，不能用 20 秒状态 fresh 掩盖延迟年龄。

### 快照模型
- `OpenClashReport`：observed_at、stale_after、delay_stale_after、selected_group、selected_node、total_connections、nodes、status/error_class/message。
- `OpenClashNode`：name、alive(*bool)、delay_ms(*int)、checked_at、active_connections。nil 区分未知/失败与真实零值；不保存 secret、订阅 URL、服务器地址或原始连接。
- 协议上限建议 64 节点，名称/错误文本有界、名称唯一、计数非负、Clone 深拷贝；state 按 connector ownership 替换并在采集失败时标注 last-known-good。

### 第四屏 UI
- 页面 ID `OPENCLASH`、标题 `OPENCLASH`，运行时顺序 `HOME,CODING,API,OPENCLASH`，默认 dwell 15 秒。
- 480×320 原生屏：顶部显示 `PROXY → 当前节点`、在用连接和测速年龄；正文两列、每列 10 行，覆盖当前 19 个节点。
- 每行显示选中标记、节点名、延迟/失败和大于零的连接数。延迟 <300ms 为 OK，300–800ms 为 warning，>800ms 为 critical；FAIL 和 `--` 使用文字，不只依赖颜色。
- 节点按名称稳定排序；超过 20 个时保留当前出口并显示 `+N MORE`，不静默截断。无数据、鉴权失败、路由器离线沿用统一 chrome 和 STALE/错误状态。
- 同步提供 60×20 legacy 两列回退；原生 Frame 可比较并沿用重复帧抑制。

### 实施文件边界
1. 后端：`internal/connector/openclash/`、`cmd/homepi-node/main.go`、`internal/config/validate.go`、Provider metadata/Web Admin TYPE_INFO。
2. 协议与状态：`internal/protocol/openclash.go`、snapshot/validate、`internal/connector/connector.go`、`internal/state/current.go`、`internal/scheduler/scheduler.go`。
3. Scheduler 的 auth 提示需按 Provider 区分，OpenClash 401 应提示更新 keyring credential，不能套用“运行官方 CLI”。
4. 屏端：`internal/ui/pages.go`、新增 openclash model、build/render、`cmd/homepi-display/pixel_screen.go`、`internal/guirender/renderer.go`、`renderer_linux.go`、`bridge.h`、`bridge.c`。
5. `OPENCLASH` 与 HOME 一样作为运行时扩展页处理 dwell，不扩充持久化五页 `allPages`；同步远程 validPage、Web Admin 页面选项和 CLI help。
6. 文档：README/README.zh-CN 增加 JSON Provider 示例、刷新语义、只读边界与四屏部署参数。

### 建议 Provider 配置（实施时写入 homepi-node，非 side-router）
```json
{
  "id": "openclash-side-router",
  "type": "openclash",
  "region": "custom",
  "base_url": "http://192.168.31.2:9090",
  "secret_ref": "keyring:openclash-side-router",
  "interval": "5s",
  "stale_after": "20s",
  "options": {"selector": "🚀 PROXY"}
}
```

### 验证与验收
- Connector：真实响应 fixture、节点去重、选择链/环、Unicode 路径转义、最久未测轮换、节点增删、失败与 401/429、超时、无内部重试、secret 不出现在错误中。
- 协议/状态：边界、唯一性、Clone、ownership、last-known-good/stale；UI：19 节点全显、20+ overflow、长 Unicode、FAIL/未知/无数据、60×20。
- 轮播：旧 `HOME,CODING,API` 不变，新顺序为 1/4–4/4，远程前后翻页/临时切页一致。
- 运行 `make check`、GUI ARM64 构建和全链路测试，再在 Pi 实机确认 LVGL 布局与刷新。
- 正常网络下选择/连接更新预算约 6.2 秒加网络耗时；两个节点同时测速超时时最坏约 11.2 秒加状态请求耗时。当前 19 节点延迟约 50 秒一轮，实屏显示 checked_at 年龄。
- 实机集成时仅在 side-router 执行只读 checksum/GET；对 `/etc/config/openclash` 与 `/etc/openclash/sui.yaml` 前后 checksum 比对。禁止 UCI set、PUT/POST、节点选择、服务重启和配置写入。

### 部署与回滚
- 先部署支持 schema 1.3 的 Display/Node，再在 homepi-node 配置 secret/provider，最后把 DietPi drop-in 改为 `run -pages HOME,CODING,API,OPENCLASH` 并重启 homepi 服务。
- 回滚只移除第四页参数并禁用 OpenClash Provider；side-router 无需也无法回滚，因为始终不修改其配置。
- 已知风险：Bearer 在可信 LAN 的明文 HTTP 中传输；若不接受，可仅替换 Connector transport 为固定命令的 SSH 只读采集，快照/UI 不变，但第一版不引入这条较慢且更复杂的通道。

### 已实施：OPENCLASH 每节点一卡设计 v3
- 用户审核后否定2×2组卡方案，明确要求“每个节点一个卡片”。
- 原生 480×320 采用纯卡片风格：2×5、每张 218×42 px，列 x=16/242，行 y=48/96/144/192/240，列间距8px、行间距6px；顶栏、Pi状态栏和页脚沿用其他数据页。
- 每子页最多10张节点卡；当前19节点分 `OPENCLASH 1/2` 与 `2/2`，按15秒 dwell 轮换。原生页顶栏使用独立 `OPENCLASH n/m` 标签，摘要行只显示当前出口、总连接和测速年龄；legacy 摘要额外附带子页号。当前出口始终在摘要可见，卡片子页不因 selected 冻结。
- 每卡显示 selected 标记、节点名、状态短文本、延迟、非零连接数；延迟色阶沿用 <300 OK、300–800 WARN、>800 CRIT。未测为 `--`，曾测但超出节点 delay 预算为 `STALE`，认证失败为 `AUTH`。
- legacy 60×20 同步为摘要行 + 5行×2列、每半行29 cells；每子页10卡并显示 `OPENCLASH n/m`，严格20行×60列。
- 该设计已实现并通过 make check、ARM64 LVGL build、Pi 原生 framebuffer OCR 和独立复核。

### 已实施：OPENCLASH 单行名称+ping卡片 v4
- 用户要求卡内只留一行“名称+ping”，字号下调且卡片宽高不变。
- 原生卡仍为218×42；名称使用 Source Han Sans SC 14 CJK，ping 使用 Montserrat 12，按 LVGL 实际字体行高计算 `(42-height)/2` 的 y 坐标，实现垂直居中。
- 卡内状态、连接数和 selected 标记已移除；当前出口仍在顶部摘要。ping 无数值时显示 `FAIL/STALE/AUTH/--`，不丢失异常语义。
- legacy 29-cell 半卡同步为23-cell名称 + 空格 + 5-cell右对齐ping。
- 最终版本通过 make check、ARM64 LVGL build、Pi 原生 framebuffer OCR/像素行带验证和独立复核。selector 使用20px实际行高；名称bearing校验与140px标签宽度一致。
- 未在本次范围修复既有分页机制问题：子页按wall-clock bucket可能在某些外层轮播相位重复同一子页；远程show-page继承底层页面dwell。当前page-only OPENCLASH调试不受影响。

### 已实施：OPENCLASH 副标题移除与active高亮 v5
- 副标题栏整行移除，不再显示 `PROXY > 当前出口`、总连接数或测速年龄。
- 原生当前 active 出口使用 `#315464` 亮蓝灰背景和 `#7FB3D5` 亮青边框，其他卡片保持原中性背景/边框。
- 卡片尺寸、单行名称+ping、字体、分页和顶底栏不变；legacy 无背景绘制能力，active 仅用 `*` 标记。
- 实机补充发现 `gui_renderer_draw_openclash` 未设置 `openclash_subpage`，已补齐Go→C→LVGL绑定。
- 最终Pi采样覆盖两个子页：`OPENCLASH 1/2`和`OPENCLASH 2/2`均显示；YINNET-HY2卡背景为RGB565解码 `(49,85,98)`，与#315464一致，其他卡为 `(24,32,32)`。
- 最终Pi二进制SHA256为 `7a69cfe8402b3ee3c05c6b7095c682580c8f70c92967773c6be6793c522aa820`，服务保持既有 `-page-only OPENCLASH` 调试参数。
- 独立复核发现的legacy子页标签缺失与无用C helper均已修复；复审无新回归。legacy现有第二页 `*` 与第一页无离页选中标记的直接回归断言。
