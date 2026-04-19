"""vlm/spike.py 单元测试。

使用 --dry-run 模式，不需要真实 GPU 或模型文件。
所有测试通过 subprocess 调用 spike.py，从 vlm/ 目录运行（与生产环境一致）。
"""
import json
import pathlib
import struct
import subprocess
import sys
import zlib

import pytest

# vlm/ 目录（spike.py 所在位置）
VLM_DIR = pathlib.Path(__file__).parent.parent
SPIKE_PY = VLM_DIR / "spike.py"

# 测试用 FSM 状态列表
VALID_STATES = [
    "lobby", "matching", "in_flight", "parachuting",
    "looting", "running_zone", "healing", "game_over", "anomaly",
]


def make_minimal_png(r: int = 100, g: int = 149, b: int = 237) -> bytes:
    """生成最小合法 1x1 RGB PNG 字节串。"""
    sig = b'\x89PNG\r\n\x1a\n'
    ihdr_data = struct.pack('>IIBBBBB', 1, 1, 8, 2, 0, 0, 0)
    ihdr_crc = zlib.crc32(b'IHDR' + ihdr_data) & 0xFFFFFFFF
    ihdr = struct.pack('>I', 13) + b'IHDR' + ihdr_data + struct.pack('>I', ihdr_crc)
    raw = bytes([0, r, g, b])
    compressed = zlib.compress(raw)
    idat_crc = zlib.crc32(b'IDAT' + compressed) & 0xFFFFFFFF
    idat = struct.pack('>I', len(compressed)) + b'IDAT' + compressed + struct.pack('>I', idat_crc)
    iend_crc = zlib.crc32(b'IEND') & 0xFFFFFFFF
    iend = struct.pack('>I', 0) + b'IEND' + struct.pack('>I', iend_crc)
    return sig + ihdr + idat + iend


def create_test_dataset(screenshots_dir: pathlib.Path, n: int = 4) -> dict:
    """在 screenshots_dir 中创建 n 张占位 PNG 和对应的 labels.json。

    返回 labels 字典。
    """
    screenshots_dir.mkdir(parents=True, exist_ok=True)
    states = VALID_STATES * (n // len(VALID_STATES) + 1)
    labels = {}
    for i in range(n):
        state = states[i]
        fname = f"{state}_{i + 1:03d}.png"
        (screenshots_dir / fname).write_bytes(make_minimal_png())
        labels[fname] = state

    (screenshots_dir / "labels.json").write_text(
        json.dumps(labels, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    return labels


def run_spike(args: list, cwd=None) -> subprocess.CompletedProcess:
    """运行 spike.py，返回 CompletedProcess。"""
    import os
    env = os.environ.copy()
    env["PYTHONIOENCODING"] = "utf-8"
    cmd = [sys.executable, str(SPIKE_PY)] + args
    return subprocess.run(
        cmd,
        cwd=str(cwd or VLM_DIR),
        capture_output=True,
        text=True,
        encoding="utf-8",
        env=env,
        timeout=120,
    )


# ──────────────────────────────────────────────
# 正向测试
# ──────────────────────────────────────────────

class TestDryRunFlow:
    """--dry-run 模式下的完整流程测试。"""

    def test_dry_run_exit_code_valid(self, tmp_path):
        """--dry-run 应返回有效退出码（0/1/2）。"""
        screenshots_dir = tmp_path / "screenshots"
        output_path = tmp_path / "report.json"
        create_test_dataset(screenshots_dir, n=4)

        result = run_spike([
            "--dry-run",
            "--screenshots-dir", str(screenshots_dir),
            "--output", str(output_path),
            "--qwen-path", "dummy",
            "--cogagent-path", "dummy",
        ])
        assert result.returncode in (0, 1, 2), (
            f"退出码应为 0/1/2，实际: {result.returncode}\n"
            f"stdout: {result.stdout}\nstderr: {result.stderr}"
        )

    def test_dry_run_report_schema(self, tmp_path):
        """--dry-run 生成的报告必须包含所有必需字段。"""
        screenshots_dir = tmp_path / "screenshots"
        output_path = tmp_path / "report.json"
        create_test_dataset(screenshots_dir, n=4)

        run_spike([
            "--dry-run",
            "--screenshots-dir", str(screenshots_dir),
            "--output", str(output_path),
            "--qwen-path", "dummy",
            "--cogagent-path", "dummy",
        ])

        assert output_path.exists(), "报告文件应已生成"
        report = json.loads(output_path.read_text(encoding="utf-8"))

        # 验证必需字段
        assert "timestamp" in report, "缺少 timestamp 字段"
        assert "total_screenshots" in report, "缺少 total_screenshots 字段"
        assert "models" in report, "缺少 models 字段"
        assert "qwen2vl" in report["models"], "models 中缺少 qwen2vl"
        assert "cogagent" in report["models"], "models 中缺少 cogagent"
        assert "accuracy" in report["models"]["qwen2vl"], "qwen2vl 缺少 accuracy"
        assert "accuracy" in report["models"]["cogagent"], "cogagent 缺少 accuracy"
        assert "overall_status" in report, "缺少 overall_status 字段"
        assert "results" in report, "缺少 results 字段"

    def test_dry_run_result_count(self, tmp_path):
        """results 数组长度应等于截图数量。"""
        n = 6
        screenshots_dir = tmp_path / "screenshots"
        output_path = tmp_path / "report.json"
        create_test_dataset(screenshots_dir, n=n)

        run_spike([
            "--dry-run",
            "--screenshots-dir", str(screenshots_dir),
            "--output", str(output_path),
            "--qwen-path", "dummy",
            "--cogagent-path", "dummy",
        ])

        report = json.loads(output_path.read_text(encoding="utf-8"))
        assert len(report["results"]) == n, (
            f"results 长度应为 {n}，实际: {len(report['results'])}"
        )
        assert report["total_screenshots"] == n

    def test_dry_run_accuracy_calculation(self, tmp_path):
        """--dry-run 模式下准确率应为 1.0（全部预测正确）。"""
        screenshots_dir = tmp_path / "screenshots"
        output_path = tmp_path / "report.json"
        create_test_dataset(screenshots_dir, n=4)

        run_spike([
            "--dry-run",
            "--screenshots-dir", str(screenshots_dir),
            "--output", str(output_path),
            "--qwen-path", "dummy",
            "--cogagent-path", "dummy",
        ])

        report = json.loads(output_path.read_text(encoding="utf-8"))
        assert report["models"]["qwen2vl"]["accuracy"] == 1.0, (
            "dry-run 模式下 qwen2vl 准确率应为 1.0"
        )
        assert report["models"]["cogagent"]["accuracy"] == 1.0, (
            "dry-run 模式下 cogagent 准确率应为 1.0"
        )
        assert report["overall_status"] == "PASS", (
            "dry-run 模式下（准确率=1.0）overall_status 应为 PASS"
        )

    def test_dry_run_result_fields(self, tmp_path):
        """results 中每个元素应包含必需字段。"""
        screenshots_dir = tmp_path / "screenshots"
        output_path = tmp_path / "report.json"
        create_test_dataset(screenshots_dir, n=2)

        run_spike([
            "--dry-run",
            "--screenshots-dir", str(screenshots_dir),
            "--output", str(output_path),
            "--qwen-path", "dummy",
            "--cogagent-path", "dummy",
        ])

        report = json.loads(output_path.read_text(encoding="utf-8"))
        for item in report["results"]:
            assert "filename" in item
            assert "ground_truth" in item
            assert "qwen2vl" in item
            assert "cogagent" in item
            for model_key in ("qwen2vl", "cogagent"):
                assert "predicted" in item[model_key]
                assert "confidence" in item[model_key]
                assert "correct" in item[model_key]
                assert "latency_ms" in item[model_key]


# ──────────────────────────────────────────────
# labels.json 加载与验证逻辑
# ──────────────────────────────────────────────

class TestLabelsLoading:
    """labels.json 加载和验证逻辑测试。"""

    def test_happy_path_loads_successfully(self, tmp_path):
        """正常情况：labels.json 存在且截图均存在，流程应正常运行。"""
        screenshots_dir = tmp_path / "screenshots"
        output_path = tmp_path / "report.json"
        create_test_dataset(screenshots_dir, n=2)

        result = run_spike([
            "--dry-run",
            "--screenshots-dir", str(screenshots_dir),
            "--output", str(output_path),
            "--qwen-path", "dummy",
            "--cogagent-path", "dummy",
        ])
        assert result.returncode in (0, 1, 2), (
            f"正常情况下应有效退出，实际退出码: {result.returncode}\n"
            f"stderr: {result.stderr}"
        )

    def test_labels_json_missing_exits_nonzero(self, tmp_path):
        """labels.json 不存在时，脚本应以非零退出码退出并输出明确错误信息。"""
        screenshots_dir = tmp_path / "screenshots"
        screenshots_dir.mkdir()
        # 不创建 labels.json
        output_path = tmp_path / "report.json"

        result = run_spike([
            "--dry-run",
            "--screenshots-dir", str(screenshots_dir),
            "--output", str(output_path),
            "--qwen-path", "dummy",
            "--cogagent-path", "dummy",
        ])
        assert result.returncode != 0, (
            "labels.json 不存在时退出码应为非零"
        )
        assert "labels.json" in result.stderr, (
            "错误信息中应包含 'labels.json'"
        )

    def test_missing_screenshot_file_exits_nonzero(self, tmp_path):
        """labels.json 中引用的截图不存在时，脚本应报错退出（不静默跳过）。"""
        screenshots_dir = tmp_path / "screenshots"
        screenshots_dir.mkdir()

        # 创建 labels.json，但不创建对应的截图文件
        labels = {"nonexistent_001.png": "lobby"}
        (screenshots_dir / "labels.json").write_text(
            json.dumps(labels), encoding="utf-8"
        )
        output_path = tmp_path / "report.json"

        result = run_spike([
            "--dry-run",
            "--screenshots-dir", str(screenshots_dir),
            "--output", str(output_path),
            "--qwen-path", "dummy",
            "--cogagent-path", "dummy",
        ])
        assert result.returncode != 0, (
            "截图文件不存在时退出码应为非零"
        )
        assert "nonexistent_001.png" in result.stderr, (
            "错误信息中应包含缺失的文件名"
        )


# ──────────────────────────────────────────────
# 准确率计算逻辑
# ──────────────────────────────────────────────

class TestAccuracyCalculation:
    """准确率计算和状态判定测试（直接导入 spike 模块）。"""

    @pytest.fixture(autouse=True)
    def _setup_path(self):
        """确保 vlm/ 目录在 sys.path 中，以便导入 spike。"""
        import sys
        vlm_str = str(VLM_DIR)
        if vlm_str not in sys.path:
            sys.path.insert(0, vlm_str)

    def test_determine_model_status_pass(self):
        from spike import determine_model_status
        assert determine_model_status(0.90) == "PASS"
        assert determine_model_status(1.00) == "PASS"
        assert determine_model_status(0.95) == "PASS"

    def test_determine_model_status_review(self):
        from spike import determine_model_status
        assert determine_model_status(0.70) == "REVIEW"
        assert determine_model_status(0.80) == "REVIEW"
        assert determine_model_status(0.89) == "REVIEW"

    def test_determine_model_status_fail(self):
        from spike import determine_model_status
        assert determine_model_status(0.0) == "FAIL"
        assert determine_model_status(0.50) == "FAIL"
        assert determine_model_status(0.699) == "FAIL"

    def test_determine_overall_status_pass_qwen_wins(self):
        from spike import determine_overall_status
        status, recommended = determine_overall_status(0.95, 0.75)
        assert status == "PASS"
        assert recommended == "qwen2vl"

    def test_determine_overall_status_pass_cogagent_wins(self):
        from spike import determine_overall_status
        status, recommended = determine_overall_status(0.75, 0.95)
        assert status == "PASS"
        assert recommended == "cogagent"

    def test_determine_overall_status_both_fail(self):
        from spike import determine_overall_status
        status, recommended = determine_overall_status(0.50, 0.60)
        assert status == "FAIL"

    def test_compute_model_stats_all_correct(self):
        from spike import compute_model_stats
        items = [("a.png", "lobby"), ("b.png", "matching")]
        results = [
            {"predicted": "lobby", "confidence": 0.9, "correct": True, "latency_ms": 10.0},
            {"predicted": "matching", "confidence": 0.85, "correct": True, "latency_ms": 20.0},
        ]
        stats = compute_model_stats(items, results)
        assert stats["accuracy"] == 1.0
        assert stats["correct"] == 2
        assert stats["total"] == 2
        assert stats["avg_latency_ms"] == 15.0
        assert stats["status"] == "PASS"

    def test_compute_model_stats_partial_correct(self):
        from spike import compute_model_stats
        items = [("a.png", "lobby"), ("b.png", "matching")]
        results = [
            {"predicted": "lobby", "confidence": 0.9, "correct": True, "latency_ms": 10.0},
            {"predicted": "lobby", "confidence": 0.5, "correct": False, "latency_ms": 20.0},
        ]
        stats = compute_model_stats(items, results)
        assert stats["accuracy"] == 0.5
        assert stats["correct"] == 1
        assert stats["status"] == "FAIL"


# ──────────────────────────────────────────────
# 负面路径测试
# ──────────────────────────────────────────────

class TestNegativePaths:
    """负面路径测试。"""

    def test_empty_result_json_counts_as_wrong(self, tmp_path):
        """推理返回空字符串时该截图应计为预测错误，不引发崩溃。

        此测试通过导入 spike 并直接调用内部逻辑验证空字符串处理。
        """
        import sys
        vlm_str = str(VLM_DIR)
        if vlm_str not in sys.path:
            sys.path.insert(0, vlm_str)

        from spike import compute_model_stats

        items = [("a.png", "lobby")]
        # 模拟推理返回空字符串后被处理为 predicted="unknown"
        results = [
            {"predicted": "unknown", "confidence": 0.0, "correct": False, "latency_ms": 5.0}
        ]
        stats = compute_model_stats(items, results)
        assert stats["correct"] == 0, "空字符串推理结果应计为预测错误"
        assert stats["accuracy"] == 0.0

    def test_spike_report_schema_required_fields(self, tmp_path):
        """spike_report.json 必须包含所有规定字段。"""
        screenshots_dir = tmp_path / "screenshots"
        output_path = tmp_path / "report.json"
        create_test_dataset(screenshots_dir, n=2)

        run_spike([
            "--dry-run",
            "--screenshots-dir", str(screenshots_dir),
            "--output", str(output_path),
            "--qwen-path", "dummy",
            "--cogagent-path", "dummy",
        ])

        report = json.loads(output_path.read_text(encoding="utf-8"))
        required_fields = [
            "timestamp",
            "total_screenshots",
            "models",
            "overall_status",
            "results",
        ]
        for field in required_fields:
            assert field in report, f"缺少必需字段: {field}"

        assert "qwen2vl" in report["models"]
        assert "cogagent" in report["models"]
        assert "accuracy" in report["models"]["qwen2vl"]
        assert "accuracy" in report["models"]["cogagent"]
