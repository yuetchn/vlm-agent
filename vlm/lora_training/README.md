# VLM LoRA 微调训练管道

当 Spike 验证（Story 1.7）准确率低于 90% 时，使用本目录中的工具对 VLM 模型进行 QLoRA 监督微调，目标是将 PUBG 游戏状态识别准确率提升至 ≥90%。

---

## 前置条件检查

执行微调前，先确认 Spike 结果需要微调：

```python
import json
with open("testdata/spike_report.json") as f:
    report = json.load(f)
if report["overall_status"] == "PASS":
    print("Spike 已通过，无需微调")
    exit(0)
print(f"Spike 状态: {report['overall_status']}，启动 LoRA 微调流程")
```

仅当 `overall_status` 为 `REVIEW` 或 `FAIL` 时继续。

---

## 目录结构

```
vlm/lora_training/
├── README.md                 本文件
├── requirements.txt          独立依赖（LLaMA-Factory + bitsandbytes）
├── .gitignore                忽略 output/ logs/ data/ *.safetensors
├── qwen2vl_lora_config.yaml  LLaMA-Factory 训练配置模板
├── prepare_dataset.py        labels.json → ShareGPT 格式数据集
├── train.py                  调用 LLaMA-Factory CLI 执行训练
├── export_model.py           合并 LoRA 权重到 base model
├── data/                     转换后的数据集（.gitignore，本地生成）
├── output/                   训练输出（.gitignore，本地生成）
│   ├── lora_weights/         LoRA adapter 权重
│   └── merged/               export_model.py 生成的完整模型
├── logs/                     训练日志 JSONL（.gitignore，本地生成）
└── tests/
    └── test_prepare_dataset.py
```

---

## 数据扩充规范

### 文件命名

```
{state}_{index:03d}.png
```

示例：`lobby_021.png`、`in_flight_005.png`

`index` 从该状态当前最大编号 +1 开始，避免覆盖现有文件。

### 放置路径

所有截图放入 `testdata/screenshots/`（**入 git**，与 Spike 数据集共享）。

### 标注格式

将新截图追加到 `testdata/screenshots/labels.json`，**不修改现有条目**：

```json
{
  "lobby_001.png": "lobby",
  "lobby_002.png": "lobby",
  "in_flight_001.png": "in_flight",
  "...": "..."
}
```

### 有效状态枚举与标注指南（共 9 个，不得自创）

| 状态            | 视觉特征                                               | 常见混淆点                              |
|-----------------|--------------------------------------------------------|-----------------------------------------|
| `lobby`         | 游戏大厅主界面，可见角色/武器选择、设置按钮、出发按钮 | 与 `matching` 区分：`lobby` 可交互，无"匹配中"计时 |
| `matching`      | 出现"正在匹配"/"MATCHING"文字或进度条，无法操作角色   | 小地图可能已出现；若已跳出机舱则为 `in_flight` |
| `in_flight`     | 飞机舱内视角或俯视地图视角，飞机图标可见，尚未跳伞    | 与 `parachuting` 区分：`in_flight` 仍在飞机上，无降落伞动画 |
| `parachuting`   | 降落伞张开动画，角色从高空向地面下落，视角为第三人称下落视角 | 与 `in_flight` 区分：已离开飞机且降落伞可见 |
| `looting`       | 近地面/地面视角，拾取物资 UI（背包栏、装备格）可见    | 与 `running_zone` 区分：`looting` 有打开背包/拾取浮窗；仅奔跑无浮窗为 `running_zone` |
| `running_zone`  | 主游戏视角，角色在地面奔跑/开车移动，无特殊交互 UI   | 毒圈计时器可见；若同时有治疗动画则为 `healing` |
| `healing`       | 角色使用医疗包/绷带/能量饮料，屏幕下方有治疗进度条    | 治疗动画期间角色静止或缓慢移动；进度条消失后切换为 `running_zone` 或 `looting` |
| `game_over`     | 游戏结束结算界面，显示"YOU WERE ELIMINATED"/"WINNER WINNER"、本局统计数据 | 结算后自动返回 `lobby`；仍在战斗中不标此状态 |
| `anomaly`       | 以上 8 种状态均不适用的截图：黑屏、加载页、网络断线提示、游戏 Crash 界面、未识别 UI | 仅在确实无法归类时使用，不得将"不确定"截图归入 `anomaly` |

> **ambiguous 截图处理原则：** 若截图介于两个状态之间（如正在打开背包的瞬间帧），优先选择**动作更完整**的状态（已打开背包 → `looting`，而非仍在奔跑时的 `running_zone`）。

### 建议数量

每状态 ≥50 张（当前基线约 20 张）。  
优先扩充 Spike 报告中准确率低的状态（见 `testdata/spike_report.json` 的 `results` 字段）。

---

## 端到端操作流程

### Step 0: 安装独立依赖

```bash
cd vlm/lora_training

# 创建独立虚拟环境（避免与 vlm/.venv 冲突）
python -m venv lora_venv
lora_venv/Scripts/activate       # Windows
# source lora_venv/bin/activate  # Linux/macOS

# 安装 CUDA 版 PyTorch（RTX 5070 Ti / CUDA 12.8+）
pip install torch torchvision --index-url https://download.pytorch.org/whl/cu128

# 安装 LLaMA-Factory 及其他依赖
pip install -r requirements.txt
```

> **注意**：若使用 RTX 4070 Ti（12GB VRAM），`qwen2vl_lora_config.yaml` 中已配置 4-bit 量化，无需修改。  
> 若使用 RTX 5070 Ti（16GB VRAM），可将 `fp16: true` 改为 `bf16: true` 以获得更好精度。

### Step 1: 扩充并标注数据集

按照"数据扩充规范"添加截图，更新 `testdata/screenshots/labels.json`。

### Step 2: 准备训练数据

```bash
# 在项目根目录执行（或通过 justfile）
just lora-prepare

# 等价命令
cd vlm/lora_training
lora_venv/Scripts/python prepare_dataset.py

# 若有额外标注目录
lora_venv/Scripts/python prepare_dataset.py --extra-data-dir D:/extra_screenshots
```

输出：`data/train.json`、`data/val.json`、`data/dataset_info.json`

### Step 3: 启动训练

```bash
# 通过 justfile（推荐）
just lora-train "D:/models/Qwen2.5-VL-7B-Instruct"

# 等价命令
cd vlm/lora_training
lora_venv/Scripts/python train.py --model-path "D:/models/Qwen2.5-VL-7B-Instruct"

# 覆盖训练轮数（快速验证用）
lora_venv/Scripts/python train.py --model-path "D:/models/Qwen2.5-VL-7B-Instruct" --epochs 1
```

训练完成后检查 `logs/train_*.jsonl` 中的 loss 收敛情况。

### Step 4: 导出合并模型

```bash
just lora-export "D:/models/Qwen2.5-VL-7B-Instruct"

# 等价命令
cd vlm/lora_training
lora_venv/Scripts/python export_model.py --model-path "D:/models/Qwen2.5-VL-7B-Instruct"
```

输出：`output/merged/`（完整模型，可直接用 Ollama 加载）

### Step 5: Ollama 部署

```bash
# 创建 Modelfile（在项目根目录执行）
cat > Modelfile <<'EOF'
FROM vlm/lora_training/output/merged
PARAMETER stop "<|im_end|>"
SYSTEM "You are a PUBG game state classifier."
EOF

# 注册模型
ollama create qwen2.5-vl-finetuned -f Modelfile

# 启动 Ollama 服务（若未运行）
ollama serve
```

### Step 6: 切换主项目配置（零代码改动）

编辑 `config.yaml`（无需修改任何代码）：

```yaml
vlm:
  backend: http
  http_endpoint: "http://localhost:11434/v1"
  http_model_name: "qwen2.5-vl-finetuned:latest"
```

### Step 7: 验证微调后准确率

```bash
# HTTP 模式 Spike（验证微调效果）
just spike-http

# 自定义端点和模型名
just spike-http "http://localhost:11434/v1" "qwen2.5-vl-finetuned:latest"
```

目标：`testdata/spike_report.json` 的 `overall_status` 更新为 `PASS`（准确率 ≥90%）。

---

## 常见问题

### OOM（显存不足）

1. 将 `qwen2vl_lora_config.yaml` 中的 `gradient_accumulation_steps` 从 `8` 改为 `16`
2. 将 `lora_rank` 从 `16` 降至 `8`（同步将 `lora_alpha` 改为 `16`）
3. 确认使用了 4-bit 量化（`quantization_bit: 4`，`quantization_method: bitsandbytes`）

### fp16 vs bf16 选择

| 显卡 | 推荐精度 | 配置 |
|------|----------|------|
| RTX 4070 Ti（12GB） | fp16 | `fp16: true`（已是默认） |
| RTX 5070 Ti（16GB） | bf16 | 将 `fp16: true` 改为 `bf16: true` |
| RTX 3090/4090 | bf16 | 同上 |

### LLaMA-Factory 找不到数据集

确认 `data/dataset_info.json` 存在（由 `prepare_dataset.py` 生成），且 `dataset` 字段值为 `pubg_states_train`。

### 训练脚本找不到 llamafactory-cli

确认在 `lora_venv` 中安装了 `llamafactory`，或者 `lora_venv/Scripts/llamafactory-cli.exe` 存在。  
若系统 PATH 中有 `llamafactory-cli`，也可直接使用（train.py 自动检测）。

---

## 参考文件

- Spike 脚本：`vlm/spike.py`
- Spike 报告：`testdata/spike_report.json`
- 截图数据集：`testdata/screenshots/`
- VLM 推理服务：`vlm/inference/model.py`
- 训练配置模板：`vlm/lora_training/qwen2vl_lora_config.yaml`
- 构建任务：`justfile`（`lora-prepare`、`lora-train`、`lora-export`、`spike-http`）
