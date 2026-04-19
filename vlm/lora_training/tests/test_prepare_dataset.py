"""
单元测试：vlm/lora_training/prepare_dataset.py

覆盖范围：
- labels.json → ShareGPT 格式转换正确性
- 8:2 拆分逻辑（含边界：样本数 ≤5、样本数为 1）
- 缺失图片时报错退出（负面路径）
- --extra-data-dir 合并逻辑（happy path）
- 空状态（某 FSM 状态 0 样本）时警告但不崩溃
"""
import json
import os
import sys
from pathlib import Path

import pytest
from PIL import Image

# 将 lora_training/ 加入路径，以便直接 import prepare_dataset
LORA_DIR = Path(__file__).parent.parent.resolve()
sys.path.insert(0, str(LORA_DIR))

import prepare_dataset as pd_mod


# ──────────────────────────────────────────────────────────────
# Fixtures
# ──────────────────────────────────────────────────────────────

def make_png(path: Path) -> None:
    """在指定路径创建一张 1×1 的占位 PNG。"""
    path.parent.mkdir(parents=True, exist_ok=True)
    img = Image.new("RGB", (1, 1), color=(0, 0, 0))
    img.save(path, format="PNG")


@pytest.fixture()
def tmp_screenshots(tmp_path):
    """创建一个临时截图目录，含 labels.json 和对应图片文件。"""
    screenshots_dir = tmp_path / "screenshots"
    screenshots_dir.mkdir()

    states = [
        "lobby", "lobby",
        "matching",
        "in_flight",
        "parachuting",
        "looting",
        "running_zone",
        "healing",
        "game_over",
        "anomaly",
    ]
    labels = {}
    for i, state in enumerate(states):
        fname = f"{state}_{i:03d}.png"
        make_png(screenshots_dir / fname)
        labels[fname] = state

    with open(screenshots_dir / "labels.json", "w", encoding="utf-8") as f:
        json.dump(labels, f)

    return screenshots_dir, labels


# ──────────────────────────────────────────────────────────────
# 1. labels.json → ShareGPT 格式转换正确性
# ──────────────────────────────────────────────────────────────

class TestToSharegpt:
    def test_structure(self, tmp_screenshots):
        screenshots_dir, labels = tmp_screenshots
        fname = next(iter(labels))
        state = labels[fname]
        img_path = (screenshots_dir / fname).resolve()

        result = pd_mod.to_sharegpt(img_path, state)

        assert "conversations" in result
        assert "images" in result
        assert len(result["conversations"]) == 2
        assert result["conversations"][0]["from"] == "human"
        assert result["conversations"][1]["from"] == "gpt"

    def test_human_turn_contains_image_token(self, tmp_screenshots):
        screenshots_dir, labels = tmp_screenshots
        fname = next(iter(labels))
        img_path = (screenshots_dir / fname).resolve()

        result = pd_mod.to_sharegpt(img_path, "lobby")
        assert "<image>" in result["conversations"][0]["value"]

    def test_gpt_turn_is_valid_json(self, tmp_screenshots):
        screenshots_dir, labels = tmp_screenshots
        fname = next(iter(labels))
        img_path = (screenshots_dir / fname).resolve()

        result = pd_mod.to_sharegpt(img_path, "lobby")
        gpt_value = result["conversations"][1]["value"]
        parsed = json.loads(gpt_value)
        assert parsed["state"] == "lobby"
        assert parsed["action"] == "observe"
        assert isinstance(parsed["confidence"], float)
        assert 0.0 <= parsed["confidence"] <= 1.0

    def test_image_path_is_absolute(self, tmp_screenshots):
        screenshots_dir, labels = tmp_screenshots
        fname = next(iter(labels))
        img_path = (screenshots_dir / fname).resolve()

        result = pd_mod.to_sharegpt(img_path, "lobby")
        img_str = result["images"][0]
        assert os.path.isabs(img_str), f"Expected absolute path, got: {img_str!r}"

    def test_image_path_uses_forward_slashes(self, tmp_screenshots):
        screenshots_dir, labels = tmp_screenshots
        fname = next(iter(labels))
        img_path = (screenshots_dir / fname).resolve()

        result = pd_mod.to_sharegpt(img_path, "lobby")
        assert "\\" not in result["images"][0]

    def test_image_path_exists(self, tmp_screenshots):
        screenshots_dir, labels = tmp_screenshots
        fname = next(iter(labels))
        img_path = (screenshots_dir / fname).resolve()

        result = pd_mod.to_sharegpt(img_path, "lobby")
        # 还原为本地路径进行存在性验证
        local_path = Path(result["images"][0].replace("/", os.sep))
        assert local_path.exists(), f"Image path does not exist: {local_path}"


# ──────────────────────────────────────────────────────────────
# 2. 8:2 拆分逻辑
# ──────────────────────────────────────────────────────────────

class TestSplitDataset:
    def _make_entries(self, n: int):
        return [(Path(f"/fake/img_{i}.png"), "lobby") for i in range(n)]

    def test_split_ratio_large(self):
        entries = self._make_entries(10)
        train, val = pd_mod.split_dataset(entries, val_ratio=0.2)
        assert len(train) + len(val) == 10
        assert len(val) == 2
        assert len(train) == 8

    def test_split_ratio_20_samples(self):
        entries = self._make_entries(20)
        train, val = pd_mod.split_dataset(entries, val_ratio=0.2)
        assert len(train) + len(val) == 20
        assert len(val) == 4

    def test_at_most_5_samples_all_train(self):
        for n in range(1, 6):
            entries = self._make_entries(n)
            train, val = pd_mod.split_dataset(entries, val_ratio=0.2)
            assert val == [], f"Expected no val set for n={n}, got {len(val)}"
            assert len(train) == n

    def test_single_sample(self):
        entries = self._make_entries(1)
        train, val = pd_mod.split_dataset(entries)
        assert len(train) == 1
        assert val == []

    def test_no_data_loss(self):
        entries = self._make_entries(100)
        train, val = pd_mod.split_dataset(entries, val_ratio=0.2)
        assert len(train) + len(val) == 100

    def test_val_at_least_1_when_large(self):
        entries = self._make_entries(6)
        _, val = pd_mod.split_dataset(entries, val_ratio=0.01)
        assert len(val) >= 1


# ──────────────────────────────────────────────────────────────
# 3. 缺失图片时报错退出（负面路径）
# ──────────────────────────────────────────────────────────────

class TestValidateAndCollect:
    def test_missing_image_exits(self, tmp_screenshots, capsys):
        screenshots_dir, labels = tmp_screenshots
        # 注入一个不存在的文件名
        bad_labels = dict(labels)
        bad_labels["nonexistent_999.png"] = "lobby"

        with pytest.raises(SystemExit) as exc_info:
            pd_mod.validate_and_collect(bad_labels, screenshots_dir)
        assert exc_info.value.code == 1

    def test_invalid_state_exits(self, tmp_screenshots):
        screenshots_dir, labels = tmp_screenshots
        bad_labels = dict(labels)
        first_key = next(iter(bad_labels))
        bad_labels[first_key] = "invalid_state_xyz"

        with pytest.raises(SystemExit) as exc_info:
            pd_mod.validate_and_collect(bad_labels, screenshots_dir)
        assert exc_info.value.code == 1

    def test_valid_entries_returned(self, tmp_screenshots):
        screenshots_dir, labels = tmp_screenshots
        entries = pd_mod.validate_and_collect(labels, screenshots_dir)
        assert len(entries) == len(labels)
        for path, state in entries:
            assert isinstance(path, Path)
            assert state in pd_mod.VALID_STATES
            assert path.exists()


# ──────────────────────────────────────────────────────────────
# 4. --extra-data-dir 合并逻辑（happy path）
# ──────────────────────────────────────────────────────────────

class TestExtraDataDir:
    def test_extra_data_merged(self, tmp_screenshots, tmp_path):
        screenshots_dir, labels = tmp_screenshots

        # 创建额外数据目录
        extra_dir = tmp_path / "extra"
        extra_dir.mkdir()
        extra_labels = {}
        for i in range(3):
            fname = f"lobby_extra_{i:03d}.png"
            make_png(extra_dir / fname)
            extra_labels[fname] = "lobby"

        with open(extra_dir / "labels.json", "w", encoding="utf-8") as f:
            json.dump(extra_labels, f)

        # 主数据集
        main_entries = pd_mod.validate_and_collect(labels, screenshots_dir)
        extra_entries = pd_mod.validate_and_collect(extra_labels, extra_dir)

        combined = main_entries + extra_entries
        assert len(combined) == len(labels) + len(extra_labels)

    def test_extra_data_missing_labels_json_exits(self, tmp_path):
        """额外目录不存在 labels.json 时应报错退出。"""
        empty_dir = tmp_path / "empty"
        empty_dir.mkdir()
        fake_labels_path = empty_dir / "labels.json"

        with pytest.raises(SystemExit):
            pd_mod.load_labels(fake_labels_path)


# ──────────────────────────────────────────────────────────────
# 5. 空状态（某 FSM 状态 0 样本）时输出警告但不崩溃
# ──────────────────────────────────────────────────────────────

class TestEmptyStates:
    def test_missing_state_no_crash(self, tmp_screenshots, capsys):
        """仅覆盖部分 FSM 状态时，程序不崩溃（警告由 main() 打印，此处测试底层函数）。"""
        screenshots_dir, labels = tmp_screenshots
        # 只包含 lobby 的标注
        single_state_labels = {k: v for k, v in labels.items() if v == "lobby"}

        # validate_and_collect 不应因为其他状态缺失而崩溃
        entries = pd_mod.validate_and_collect(single_state_labels, screenshots_dir)
        assert all(state == "lobby" for _, state in entries)

    def test_all_states_present_in_valid_states(self):
        """VALID_STATES 包含全部 9 个 FSM 状态。"""
        expected = {
            "lobby", "matching", "in_flight", "parachuting",
            "looting", "running_zone", "healing", "game_over", "anomaly",
        }
        assert pd_mod.VALID_STATES == frozenset(expected)
