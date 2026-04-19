# vlm-agent

> 基于 VLM + LLM 的视觉感知自动化框架 — Go 执行层 + Python 推理服务，gRPC 通信

将视觉语言模型（VLM）与大语言模型（LLM）结合，构建一套通用的屏幕视觉感知与自动化执行框架。Go 负责截图采集、输入模拟与状态调度；Python 负责本地 GPU 推理（Qwen2.5-VL）或调用远程 OpenAI-Vision 兼容接口；两端通过 gRPC 通信，实现低延迟的感知→决策→执行闭环。

---

## 架构概览

```
┌─────────────────────────────────┐        ┌──────────────────────────┐
│        Go Client (执行层)        │        │  Python VLM Server       │
│                                 │        │  (推理层)                 │
│  ┌──────────┐  ┌──────────────┐ │  gRPC  │                          │
│  │ 屏幕截图  │  │  输入控制器   │ │◄──────►│  Qwen2.5-VL / HTTP兼容   │
│  │ DXGI/GDI │  │ 键盘/鼠标    │ │        │  本地 GPU 或远程 API      │
│  └──────────┘  └──────────────┘ │        └──────────────────────────┘
│                                 │
│  ┌──────────┐  ┌──────────────┐ │        ┌──────────────────────────┐
│  │  状态机   │  │  LLM 决策层  │ │◄──────►│  OpenAI-Compatible LLM   │
│  │   FSM    │  │  策略调度    │ │  HTTP  │  GPT-4o / 本地模型        │
│  └──────────┘  └──────────────┘ │        └──────────────────────────┘
└─────────────────────────────────┘
```

---

## 功能特性

### 视觉感知与自动化
- **场景识别**：VLM 实时分析屏幕内容，理解当前应用状态
- **语义决策**：LLM 根据感知结果制定操作策略，支持复杂逻辑判断
- **自动执行**：根据决策结果自动模拟键鼠输入，完成界面交互
- **状态机调度**：FSM 管理多阶段任务流转，异常自动恢复
- **异常处理**：识别弹窗、超时、崩溃等边界情况并自动处理

### 通知推送（飞书）
- 任务执行结果推送（含截图）
- 累计统计数据汇报
- 异常告警：崩溃检测、连接超时等
- 免打扰时间段配置

### VLM 推理后端（双模式）
| 模式 | 说明 |
|------|------|
| `grpc` | 本地 GPU 推理，低延迟（~100ms），支持 GGUF 量化模型 |
| `http` | 调用 OpenAI Vision 兼容接口（Ollama、云端 API 等） |

---

## 技术栈

| 层级 | 技术 |
|------|------|
| 执行层 | Go 1.26+，DXGI/GDI 截图，Win32 输入模拟 |
| 推理层 | Python，Qwen2.5-VL，llama.cpp / Ollama |
| 通信 | gRPC + Protobuf |
| 策略 | OpenAI API 兼容 LLM（GPT-4o 或本地模型） |
| 通知 | 飞书 Bot（Webhook + App API） |
| 配置 | YAML + 加密 secrets（AES-256） |
| 日志 | zerolog，支持按大小/时间滚动 |

---

## 快速开始

### 前置要求

- Go 1.26+
- Python 3.10+（VLM Server 需要）
- NVIDIA GPU（本地推理模式，建议 ≥8GB VRAM）

### 安装依赖

```bash
# Go 依赖
go mod download

# Python VLM Server 依赖
cd vlm
pip install -r requirements.txt
```

### 配置

```bash
cp config.yaml.example config.yaml
# 编辑 config.yaml，填写 VLM/LLM 后端、飞书凭证等
```

首次运行时会交互式提示输入 API Key，加密保存至 `secrets.enc`。

### 启动

```bash
# 启动 Python VLM Server（本地 GPU 模式）
cd vlm && python server.py

# 启动 Go 客户端
go run ./cmd/new_jzd
```

---

## 配置说明

```yaml
vlm:
  backend: grpc           # grpc（本地 GPU）| http（远程 API）
  grpc_model_path: "models/Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf"
  http_endpoint: "http://localhost:11434/v1"
  http_model_name: "qwen2.5-vl:7b"

llm:
  endpoint: "https://api.openai.com/v1"
  model: "gpt-4o"

task:
  mode: "auto"            # 运行模式
  max_sessions: 0         # 0 = 无限制
```

完整配置项参见 [`config.yaml.example`](./config.yaml.example)。

---

## 项目结构

```
.
├── cmd/new_jzd/          # 程序入口
├── internal/
│   ├── adapters/
│   │   ├── capture/      # 屏幕截图（DXGI / GDI）
│   │   ├── input/        # 键鼠输入控制器
│   │   ├── vlm/          # VLM 客户端（gRPC + HTTP）
│   │   └── llm/          # LLM 客户端
│   ├── config/           # 配置加载、加密 secrets
│   ├── logger/           # 结构化日志 + 滚动
│   ├── ports/            # 接口定义（六边形架构）
│   └── vlmpb/            # Protobuf 生成代码
├── vlm/                  # Python VLM 推理服务
├── proto/                # Protobuf 定义文件
└── config.yaml.example
```

---

## License

MIT
