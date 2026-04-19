# testdata/screenshots

此目录存放 VLM 可行性 Spike 使用的标注截图数据集，纳入 git 版本管理。

## 命名规范

截图文件按以下规范命名：

```
{state}_{index:03d}.png
```

- `state`：FSM 状态名（见下方状态列表）
- `index`：从 `001` 开始的三位数序号

示例：`lobby_001.png`、`looting_003.png`

## FSM 状态列表

| 状态名 | 说明 |
|--------|------|
| `lobby` | 游戏大厅 |
| `matching` | 匹配中 |
| `in_flight` | 飞机飞行阶段 |
| `parachuting` | 跳伞阶段 |
| `looting` | 拾取物资阶段 |
| `running_zone` | 跑圈阶段 |
| `healing` | 使用治疗道具 |
| `game_over` | 游戏结束 |
| `anomaly` | 异常状态（弹窗/报错等） |

## 标注文件格式

`labels.json` 记录每张截图对应的 ground truth FSM 状态：

```json
{
  "lobby_001.png": "lobby",
  "matching_001.png": "matching"
}
```

## 数据集分布（共 20 张）

| 状态 | 数量 | 文件名 |
|------|------|--------|
| lobby | 3 | lobby_001.png ~ lobby_003.png |
| matching | 2 | matching_001.png ~ matching_002.png |
| in_flight | 2 | in_flight_001.png ~ in_flight_002.png |
| parachuting | 2 | parachuting_001.png ~ parachuting_002.png |
| looting | 3 | looting_001.png ~ looting_003.png |
| running_zone | 2 | running_zone_001.png ~ running_zone_002.png |
| healing | 2 | healing_001.png ~ healing_002.png |
| game_over | 2 | game_over_001.png ~ game_over_002.png |
| anomaly | 2 | anomaly_001.png ~ anomaly_002.png |

## 使用说明

运行 Spike 验证脚本：

```bash
just spike <qwen_model_dir> <cogagent_model_dir>
# 或
cd vlm && .venv/Scripts/python spike.py --qwen-path <p1> --cogagent-path <p2>
```

使用 `--dry-run` 跳过真实推理（用于测试流程）：

```bash
cd vlm && .venv/Scripts/python spike.py --dry-run --qwen-path dummy --cogagent-path dummy
```

> **注意**：`--qwen-path` 和 `--cogagent-path` 必须是 HuggingFace 模型目录（包含 `config.json`），
> **不是** `.gguf` 文件。`.gguf` 格式是给 llama.cpp/Ollama 使用的，与 spike.py 不兼容。

## 截图要求

- 格式：PNG
- 来源：真实游戏截图（非模拟或合成图像）
- 每张截图应清晰反映对应 FSM 状态的典型界面
- 当前目录中的 PNG 文件为占位图，需替换为真实游戏截图后方可进行有效的 Spike 评估
