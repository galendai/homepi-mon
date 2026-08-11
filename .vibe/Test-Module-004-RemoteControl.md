# 模块 004 测试文档：远程控制

> 对应规格：Module-Spec-004-RemoteControl.md  
> 所属阶段：Phase 4
> 状态：测试设计已认证，尚未实现/执行

## 1. Unit Test

| ID | 输入/前置条件 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| U001 | 合法 show_page(page_id=api)，未过期 | Accepted 后 Executed，当前页为 api | 未执行 | 待执行 |
| U002 | 未知 kind=`exec_shell` | rejected_unknown_command，不执行任何动作 | 未执行 | 待执行 |
| U003 | device_id 与当前设备不一致 | rejected_wrong_target | 未执行 | 待执行 |
| U004 | expires_at 早于当前时间 | expired，不改变 UI | 未执行 | 待执行 |
| U005 | 同一 command_id 重放 10 次 | 只执行一次，其余返回原结果 | 未执行 | 待执行 |
| U006 | next_page 参数带多余 shell 字符串 | schema 校验拒绝，不解释为命令 | 未执行 | 待执行 |
| U007 | show_message 含 ESC、BEL、超长文本 | 控制字符清理、长度限制或拒绝 | 未执行 | 待执行 |
| U008 | set_brightness(level=150) | rejected_invalid_param | 未执行 | 待执行 |
| U009 | set_brightness 合法但硬件不支持 | failed_unsupported_capability | 未执行 | 待执行 |
| U010 | sequence 小于已执行 sequence | rejected_out_of_order | 未执行 | 待执行 |
| U011 | emergency 抢占 normal 页面 | 显示 emergency；结束后恢复抢占前页面 | 未执行 | 待执行 |
| U012 | 自动轮播即将切页时收到普通远程 show_page | 远程目标页按命令显示；命令展示期结束后恢复此前轮播位置 | 未执行 | 待执行 |
| U013 | 队列超过上限 | 丢弃最旧普通命令，保留最新状态，返回 overflow 诊断 | 未执行 | 待执行 |

## 2. E2E Test

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| E001 | 正常 WebSocket，发送 show_page | 1 秒内切页并收到最终 ACK | 未执行 | 待执行 |
| E002 | 断网时发送 30 秒有效命令，60 秒后重连 | 旧命令不执行并报告 expired/不再下发 | 未执行 | 待执行 |
| E003 | 断网期间 Phase 1 保持 overview，Phase 3 开启自动轮播 | 不等待远程通道；Phase 1 页面不变，Phase 3 按本地配置继续轮播 | 未执行 | 待执行 |
| E004 | 设备 A/B 在线，命令目标 A | 仅 A 执行，B 无状态变化 | 未执行 | 待执行 |
| E005 | 撤销设备 Token 后保持旧连接/重连 | 会话按策略终止或拒绝，不能继续接收命令 | 未执行 | 待执行 |
| E006 | 重启 Pi 后重放已执行 command_id | 从持久幂等记录识别，不再次执行 | 未执行 | 待执行 |
| E007 | 远程 show_message 到期 | 自动恢复原页面，消息不残留 | 未执行 | 待执行 |
| E008 | 对 Pi 进行端口扫描 | 无本模块新增的公网入站监听 | 未执行 | 待执行 |
| E009 | 同一 LAN 中未配对 node 尝试发送命令 | Pi 拒绝命令与连接，不改变页面；审计记录来源错误码 | 未执行 | 待执行 |
| E010 | 已绑定 node=A，另一个已知但未绑定 node=B 发送合法命令 | B 的命令被拒绝，只有 A 能控制 Phase 1 UI | 未执行 | 待执行 |

## 3. 安全测试

| ID | 输入/环境 | 预期输出 | 实际输出 | 结果 |
|---|---|---|---|---|
| S001 | 构造 JSON 类型混淆、超深嵌套和超大消息 | 在大小/深度/schema 边界拒绝，进程不崩溃 | 未执行 | 待执行 |
| S002 | 伪造设备身份、目标和时间戳 | 全部拒绝，审计记录稳定错误码 | 未执行 | 待执行 |
| S003 | 扫描审计日志 | 可关联 command_id 与结果，不含 Provider Key/完整敏感消息 | 未执行 | 待执行 |

## 4. 执行记录

尚未执行。实现后每次测试需记录输入消息、ACK、最终 UI 状态和时间测量；攻击样本、临时证书、Mock 服务与测试文件执行后清除。
