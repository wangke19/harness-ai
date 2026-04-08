# PicoClaw Harness System - 架构文档

## 系统概述

本系统基于 OpenAI Harness Engineering 理念构建，使用 PicoClaw 作为核心 runtime agent，实现自主化的软件开发工作流。

**核心理念**: Human steer, Agents execute.（人类引导，代理执行）

## 架构分层

### Layer 1: Human Layer（人类层）
- **职责**: 需求定义、方向指导、最终决策
- **交互方式**: 自然语言、审查反馈、异常处理
- **输出**: 需求描述、审查意见、纠正指令

### Layer 2: Runtime Agent Layer（运行时代理层）
- **核心引擎**: PicoClaw (基于 Pi)
- **能力**: 事件驱动、状态管理、工具编排
- **代理类型**:
  - **Planner Agent**: 任务规划和分解
  - **Executor Agent**: 代码生成和执行
  - **Reviewer Agent**: 自动审查和验证
  - **Doc Gardener**: 知识库维护

### Layer 3: Tool Layer（工具层）
- **版本控制**: Git (worktree 管理)
- **代码托管**: GitHub (PR/Issue 自动化)
- **测试框架**: 自动化测试套件
- **文档系统**: Markdown + 结构化索引
- **容器化**: Docker (可选)
- **网络搜索**: Baidu/Tavily

## 核心设计原则

### 1. 知识驱动
- 所有决策基于结构化知识库
- 文档是系统记忆，不是装饰
- 渐进式披露：从概览到细节

### 2. 验证闭环
- 每个行动都经过验证
- 自动测试覆盖所有变更
- 审查反馈自动处理

### 3. 可追溯性
- 所有行动记录到日志
- 每个决策都有依据
- 学习持续积累

### 4. 渐进式自主性
- 从 Level 1（单次任务）开始
- 逐步提升到 Level 5（自主目标设定）
- 人类始终保持控制权

## 系统流程

```
需求输入
  ↓
Planner Agent: 需求分析 → 任务分解 → 生成执行计划
  ↓
人类确认
  ↓
Executor Agent: 创建 worktree → 执行开发 → 提交代码 → 创建 PR
  ↓
Reviewer Agent: 自动审查 → 运行测试 → 生成报告
  ↓
通过? → 修复问题
  ↓
人类审查并合并
  ↓
Doc Gardener: 更新知识库 → 记录学习
```

## 关键技术点

### 1. Git Worktree 管理
```bash
# 每个任务一个独立 worktree
git worktree add /tmp/task-123-feature origin/main
# 在 worktree 中工作，不影响主分支
# 完成后删除 worktree
git worktree remove /tmp/task-123-feature
```

### 2. 事件驱动架构
- 利用 Pi 的事件系统
- 实时流式输出进度
- 支持长时间运行的任务

### 3. 代理通信
- 代理之间通过文档通信
- Planner → Executor: exec-plans/*.md
- Executor → Reviewer: PR + 测试结果
- Reviewer → Executor: 审查报告

### 4. 知识库验证
- Linting 检查文档格式
- CI 验证文档正确性
- Doc Gardener 自动修复

## 可扩展性

### 添加新工具
1. 在 `tools/` 目录创建工具文档
2. 在 `AGENTS.md` 中注册工具
3. 为代理添加工具调用逻辑

### 添加新代理
1. 定义代理职责和接口
2. 创建对应的工具和能力
3. 在 `AGENTS.md` 中注册代理
4. 实现代理通信协议

### 提升自主性级别
- Level 1: 单次任务执行 ✅
- Level 2: 多步骤编排 ✅
- Level 3: 自主计划 (Planner Agent)
- Level 4: 自主执行 (Executor Agent)
- Level 5: 自主目标设定 (未来)

## 性能目标

- **响应时间**: 需求分析 < 30s
- **执行速度**: 单个任务 < 10min（简单）< 1h（复杂）
- **质量指标**: 测试通过率 > 95%
- **审查效率**: 自动审查覆盖率 > 80%

## 安全考虑

1. **权限控制**: 代理只能操作指定目录
2. **破坏性操作**: 需要人类确认
3. **敏感数据**: 不记录到知识库
4. **回滚机制**: Git 提供完整的版本历史

## 未来展望

- [ ] 支持多项目并行开发
- [ ] 集成更多自动化工具
- [ ] 支持跨语言项目
- [ ] 实现自主学习和优化
- [ ] 可视化进度追踪

## 参考资料

- [OpenAI Harness Engineering](https://openai.com/articles/harness-engineering)
- [Pi Engine Documentation](https://github.com/badlogic/pi-mono)
- [PicoClaw Documentation](https://github.com/openclaw/picoclaw)
