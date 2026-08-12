# IMPL-005：GitHub 双语 README

## 1. 任务目标

依据 `HL-Spec.md` 与 `Module-Spec-007-ProjectDocumentation.md`，创建英文默认 README 和简体中文
README，完整说明项目安装、运行与开发方式。

## 2. 实现顺序

1. 从 Makefile、binary usage、安装脚本和 kiosk 模板提取真实公共入口。
2. 编写结构一致的 `README.md` 与 `README.zh-CN.md`。
3. 校验相对链接、命令集合、敏感信息、Markdown 格式和全仓回归。
4. 将实际结果写回 `Test-Module-007-ProjectDocumentation.md`，等待用户检查后再提交。

## 3. 边界

- 本任务只修改规格和项目文档，不修改程序行为。
- 不把当前工作区中的 MiniMax、金额格式或 TUI 改动混入 README 实现。
- 不执行服务安装、Provider Apply、真实凭据测试、Pi 部署或 Git commit。

## 4. 成功标准

- GitHub 首次访问者可从英文或中文文档完成 mock 快速启动。
- 真实部署说明清楚区分 node 主机、Pi display、loopback Web Admin 和用户级服务。
- 开发者可找到格式化、测试、构建、交叉发布及规格驱动工作流。
- 所有自动化文档测试通过；手动 GitHub 预览与 Quick start 保留给用户验收。

## 5. 实际结果

2026-08-12 完成 `README.md` 与 `README.zh-CN.md`，两份文档均为 728 行，包含一致的 39 个 Bash
操作块。仓库内链接、Make target、脚本路径、binary usage、敏感信息扫描、Markdown 空白检查和
全仓 Go/Bash 回归均通过。未运行 GitHub 在线预览、真实 Provider、服务 Apply、Pi 部署或 Mock 双进程
手动验收；这些项目明确保留给用户检查。
