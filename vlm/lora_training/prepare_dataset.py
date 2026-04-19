"""
prepare_dataset.py — 将 testdata/screenshots/labels.json 转换为 LLaMA-Factory ShareGPT 格式。

用法:
    python prepare_dataset.py
    python prepare_dataset.py --extra-data-dir D:/extra_screenshots

输出:
    vlm/lora_training/data/train.json
    vlm/lora_training/data/val.json
    vlm/lora_training/data/dataset_info.json
"""
import argparse
import json
import os
import random
import sys
from collections import Counter
from pathlib import Path

# 项目根目录（lora_training/ 的上上级）
PROJECT_ROOT = Path(__file__).parent.parent.parent.resolve()
SCREENSHOTS_DIR = PROJECT_ROOT / "testdata" / "screenshots"
LABELS_FILE = SCREENSHOTS_DIR / "labels.json"
DATA_DIR = Path(__file__).parent / "data"

VALID_STATES = frozenset([
    "lobby", "matching", "in_flight", "parachuting",
    "looting", "running_zone", "healing", "game_over", "anomaly",
])

PROMPT = (
    "You are analyzing a PUBG game screenshot for state classification.\n"
    "Classify the current game state into EXACTLY one of:\n"
    "lobby, matching, in_flight, parachuting, looting, running_zone, healing, game_over, anomaly\n\n"
    'Return JSON only: {"state": "<state>", "action": "observe", "confidence": <0.0-1.0>}'
)


def load_labels(labels_path: Path) -> dict:
    if not labels_path.exists():
        print(f"ERROR: labels.json not found: {labels_path}", file=sys.stderr)
        sys.exit(1)
    with open(labels_path, encoding="utf-8") as f:
        labels = json.load(f)
    if not labels:
        print("ERROR: labels.json is empty", file=sys.stderr)
        sys.exit(1)
    return labels


def validate_and_collect(labels: dict, screenshots_dir: Path) -> list:
    """验证图片文件存在，返回 [(abs_path, state), ...] 列表。"""
    entries = []
    missing = []
    invalid_states = []

    for fname, state in labels.items():
        if state not in VALID_STATES:
            invalid_states.append((fname, state))
            continue
        img_path = (screenshots_dir / fname).resolve()
        if not img_path.exists():
            missing.append(fname)
            continue
        entries.append((img_path, state))

    if invalid_states:
        for fname, state in invalid_states:
            print(f"ERROR: invalid state '{state}' for {fname}", file=sys.stderr)
        sys.exit(1)

    if missing:
        for fname in missing:
            print(f"ERROR: missing image: {fname}", file=sys.stderr)
        sys.exit(1)

    return entries


def to_sharegpt(img_abs_path: Path, state: str) -> dict:
    """将单条样本转换为 LLaMA-Factory ShareGPT 格式。"""
    # 使用正斜杠绝对路径，避免 LLaMA-Factory 路径解析歧义
    img_str = str(img_abs_path).replace("\\", "/")
    return {
        "conversations": [
            {
                "from": "human",
                "value": f"<image>\n{PROMPT}",
            },
            {
                "from": "gpt",
                "value": json.dumps(
                    {"state": state, "action": "observe", "confidence": 0.95},
                    ensure_ascii=False,
                ),
            },
        ],
        "images": [img_str],
    }


def split_dataset(entries: list, val_ratio: float = 0.2) -> tuple:
    """按 8:2 比例拆分训练集/验证集。样本数 ≤5 时全量作为训练集。"""
    if len(entries) <= 5:
        return entries, []
    random.shuffle(entries)
    val_size = max(1, int(len(entries) * val_ratio))
    return entries[val_size:], entries[:val_size]


def write_dataset_info(data_dir: Path, has_val: bool = True) -> None:
    """写入 LLaMA-Factory 所需的 dataset_info.json。

    Args:
        data_dir:  数据集目录（写入 dataset_info.json 的位置）
        has_val:   是否生成了 val.json（样本数 ≤5 时不生成验证集）
    """
    split_columns = {
        "messages": "conversations",
        "images": "images",
    }
    info = {
        "pubg_states_train": {
            "file_name": "train.json",
            "formatting": "sharegpt",
            "columns": split_columns,
        }
    }
    if has_val:
        info["pubg_states_val"] = {
            "file_name": "val.json",
            "formatting": "sharegpt",
            "columns": split_columns,
        }
    with open(data_dir / "dataset_info.json", "w", encoding="utf-8") as f:
        json.dump(info, f, ensure_ascii=False, indent=2)


def main():
    parser = argparse.ArgumentParser(description="准备 LoRA 训练数据集")
    parser.add_argument(
        "--extra-data-dir", default=None,
        help="额外标注截图目录（需含 labels.json），与主数据集合并"
    )
    args = parser.parse_args()

    # 加载主数据集
    labels = load_labels(LABELS_FILE)
    entries = validate_and_collect(labels, SCREENSHOTS_DIR)

    # 合并额外数据集
    if args.extra_data_dir:
        extra_dir = Path(args.extra_data_dir).resolve()
        extra_labels_path = extra_dir / "labels.json"
        extra_labels = load_labels(extra_labels_path)
        extra_entries = validate_and_collect(extra_labels, extra_dir)
        print(f"合并额外数据: {len(extra_entries)} 条（来自 {extra_dir}）")
        entries.extend(extra_entries)

    # 统计各状态样本数
    state_counts = Counter(state for _, state in entries)
    print(f"\n数据集统计（共 {len(entries)} 条）:")
    for state in sorted(VALID_STATES):
        count = state_counts.get(state, 0)
        warn = " ⚠ 样本不足（建议 ≥50）" if 0 < count < 50 else (" ⚠ 无样本" if count == 0 else "")
        print(f"  {state:15s}: {count:3d} 张{warn}")

    # 拆分数据集
    train_entries, val_entries = split_dataset(entries)
    print(f"\n训练集: {len(train_entries)} 条 | 验证集: {len(val_entries)} 条")

    # 转换为 ShareGPT 格式
    DATA_DIR.mkdir(parents=True, exist_ok=True)

    train_data = [to_sharegpt(p, s) for p, s in train_entries]
    val_data = [to_sharegpt(p, s) for p, s in val_entries]

    with open(DATA_DIR / "train.json", "w", encoding="utf-8") as f:
        json.dump(train_data, f, ensure_ascii=False, indent=2)

    if val_data:
        with open(DATA_DIR / "val.json", "w", encoding="utf-8") as f:
            json.dump(val_data, f, ensure_ascii=False, indent=2)
    else:
        print("注意: 样本数 ≤5，跳过验证集生成")

    write_dataset_info(DATA_DIR, has_val=bool(val_data))

    print(f"\n[OK] 数据集已写入 {DATA_DIR}/")
    print(f"  train.json : {len(train_data)} 条")
    print(f"  val.json   : {len(val_data)} 条")
    print(f"  dataset_info.json: 已生成（{'含 val 注册' if val_data else '仅 train，样本不足跳过 val'}）")


if __name__ == "__main__":
    main()
