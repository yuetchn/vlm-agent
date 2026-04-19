"""VLM 可行性 Spike 脚本 — 双模型对比识别验证。

用法:
    python spike.py --qwen-path <hf_model_dir> --cogagent-path <hf_model_dir>
    python spike.py --dry-run --qwen-path dummy --cogagent-path dummy

参数:
    --qwen-path:       Qwen2.5-VL HuggingFace 模型目录（必填）
    --cogagent-path:   CogAgent HuggingFace 模型目录（必填）
    --screenshots-dir: 截图目录（默认 ../testdata/screenshots）
    --output:          报告输出路径（默认 ../testdata/spike_report.json）
    --dry-run:         跳过真实推理，固定返回 ground truth（验证流程用）

VRAM 约束（4070Ti 12GB）：
    两个模型顺序执行。先完整跑 Qwen2.5-VL 全部截图，释放 VRAM 后再跑 CogAgent。
"""
import argparse
import gc
import json
import os
import sys
import time
from datetime import datetime, timezone, timedelta

# 将 vlm/ 目录加入 Python 路径，以便复用 inference/ 模块
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

SPIKE_SKILL_CONTEXT = """
You are analyzing a PUBG game screenshot for state classification.
Classify the current game state into EXACTLY one of:
lobby, matching, in_flight, parachuting, looting, running_zone, healing, game_over, anomaly

Return JSON only: {"state": "<state>", "action": "observe", "confidence": <0.0-1.0>}
"""

VALID_STATES = frozenset([
    "lobby", "matching", "in_flight", "parachuting",
    "looting", "running_zone", "healing", "game_over", "anomaly",
])

# 推荐模型名映射（写入 config.yaml.example 的 http_model_name）
MODEL_NAME_MAP = {
    "qwen2vl": "qwen2.5-vl:7b",
    "cogagent": "cogagent:9b",
}


def parse_args():
    parser = argparse.ArgumentParser(
        description="VLM 可行性 Spike — 双模型对比识别验证"
    )
    parser.add_argument(
        "--qwen-path", required=True,
        help="Qwen2.5-VL HuggingFace 模型目录路径（含 config.json 的目录，非 .gguf 文件）"
    )
    parser.add_argument(
        "--cogagent-path", required=True,
        help="CogAgent HuggingFace 模型目录路径（含 config.json 的目录，非 .gguf 文件）"
    )
    parser.add_argument(
        "--screenshots-dir",
        default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "testdata", "screenshots"),
        help="截图目录（默认 ../testdata/screenshots）"
    )
    parser.add_argument(
        "--output",
        default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "testdata", "spike_report.json"),
        help="报告输出路径（默认 ../testdata/spike_report.json）"
    )
    parser.add_argument(
        "--dry-run", action="store_true",
        help="跳过真实推理，固定返回 ground truth（confidence=0.9），用于验证整体流程"
    )
    return parser.parse_args()


def load_labels(screenshots_dir: str) -> dict:
    """加载 labels.json，验证截图文件存在，返回 {filename: ground_truth} 字典。

    Raises:
        SystemExit: labels.json 不存在或截图文件缺失时退出。
    """
    labels_path = os.path.join(screenshots_dir, "labels.json")
    if not os.path.exists(labels_path):
        print(
            f"❌ 错误：labels.json 不存在（{labels_path}）\n"
            "请先创建标注文件，格式见 testdata/screenshots/README.md",
            file=sys.stderr
        )
        sys.exit(1)

    with open(labels_path, "r", encoding="utf-8") as f:
        labels = json.load(f)

    if not labels:
        print("❌ 错误：labels.json 为空，没有截图可供测试", file=sys.stderr)
        sys.exit(1)

    invalid_states = [gt for gt in labels.values() if gt not in VALID_STATES]
    if invalid_states:
        print(
            "❌ 错误：labels.json 中含无效状态值（非 VALID_STATES 成员）：\n" +
            "\n".join(f"  - {s!r}" for s in sorted(set(invalid_states))),
            file=sys.stderr
        )
        sys.exit(1)

    screenshots_real = os.path.realpath(screenshots_dir)
    missing = []
    for fname in labels:
        fpath = os.path.realpath(os.path.join(screenshots_dir, fname))
        try:
            fpath_rel = os.path.relpath(fpath, screenshots_real)
        except ValueError:
            fpath_rel = ".."  # Windows 跨盘符时视为路径遍历
        if fpath_rel.startswith(".."):
            print(
                f"❌ 错误：labels.json 中含非法文件名（路径遍历）：{fname}",
                file=sys.stderr
            )
            sys.exit(1)
        if not os.path.exists(fpath):
            missing.append(fname)

    if missing:
        print(
            "❌ 错误：labels.json 中引用的截图文件不存在：\n" +
            "\n".join(f"  - {f}" for f in missing),
            file=sys.stderr
        )
        sys.exit(1)

    return labels


def determine_model_status(accuracy: float) -> str:
    """根据准确率返回模型判定状态。"""
    if accuracy >= 0.90:
        return "PASS"
    elif accuracy >= 0.70:
        return "REVIEW"
    else:
        return "FAIL"


def determine_overall_status(qwen_acc: float, cogagent_acc: float) -> tuple:
    """根据两个模型的最高准确率判定 overall_status 和推荐模型。

    Returns:
        (overall_status, recommended_model_key)
    """
    if qwen_acc >= cogagent_acc:
        best_acc = qwen_acc
        recommended = "qwen2vl"
    else:
        best_acc = cogagent_acc
        recommended = "cogagent"

    overall_status = determine_model_status(best_acc)
    return overall_status, recommended


def run_inference_for_model(
    model_key: str,
    model_path: str,
    items: list,
    screenshots_dir: str,
    dry_run: bool,
) -> list:
    """对单个模型运行全部截图推理，返回结果列表。

    Args:
        model_key:       "qwen2vl" 或 "cogagent"
        model_path:      HuggingFace 模型目录路径
        items:           [(filename, ground_truth), ...] 列表
        screenshots_dir: 截图目录路径
        dry_run:         若 True，跳过真实推理，返回 ground truth 结果

    Returns:
        [{"predicted": str, "confidence": float, "correct": bool, "latency_ms": float}, ...]
    """
    results = []

    if dry_run:
        print(f"  [dry-run] 跳过真实推理，使用 ground truth 作为预测结果")
        for fname, gt in items:
            results.append({
                "predicted": gt,
                "confidence": 0.9,
                "correct": True,
                "latency_ms": 1.0,
            })
        return results

    from inference.model import VLMModel
    from inference.pipeline import InferencePipeline

    print(f"  正在加载模型: {model_path}")
    model = VLMModel(model_type=model_key, model_path=model_path)
    model.load()
    pipeline = InferencePipeline(model)
    print(f"  模型加载完成，开始推理 {len(items)} 张截图...")

    for i, (fname, gt) in enumerate(items, 1):
        fpath = os.path.join(screenshots_dir, fname)
        with open(fpath, "rb") as f:
            screenshot_bytes = f.read()

        t0 = time.perf_counter()
        result_json, confidence = pipeline.run(screenshot_bytes, SPIKE_SKILL_CONTEXT, "unknown")
        latency_ms = (time.perf_counter() - t0) * 1000

        if not result_json:
            # pipeline 返回空字符串表示可恢复异常，计为预测错误
            predicted = "unknown"
            confidence = 0.0
        else:
            try:
                data = json.loads(result_json)
                predicted = data.get("state", "unknown")
                confidence = float(data.get("confidence", confidence))
            except (json.JSONDecodeError, AttributeError, ValueError):
                predicted = "unknown"
                confidence = 0.0

        correct = (predicted == gt)
        results.append({
            "predicted": predicted,
            "confidence": confidence,
            "correct": correct,
            "latency_ms": round(latency_ms, 1),
        })
        status_icon = "✓" if correct else "✗"
        print(f"  [{i:2d}/{len(items)}] {fname}: {status_icon} predicted={predicted!r} gt={gt!r} ({latency_ms:.1f}ms)")

    # 显式释放 VRAM：先 del pipeline（持有对 model 的引用），再 del model
    del pipeline, model
    gc.collect()
    try:
        import torch
        torch.cuda.empty_cache()
    except ImportError:
        pass

    print(f"  VRAM 已释放")
    return results


def compute_model_stats(items: list, results: list) -> dict:
    """计算模型推理统计信息。

    Args:
        items:   [(filename, ground_truth), ...] 列表
        results: run_inference_for_model() 返回的结果列表

    Returns:
        {"accuracy": float, "correct": int, "total": int, "avg_latency_ms": float, "status": str}
    """
    total = len(results)
    correct = sum(1 for r in results if r["correct"])
    accuracy = correct / total if total > 0 else 0.0
    avg_latency = sum(r["latency_ms"] for r in results) / total if total > 0 else 0.0
    return {
        "accuracy": round(accuracy, 4),
        "correct": correct,
        "total": total,
        "avg_latency_ms": round(avg_latency, 1),
        "status": determine_model_status(accuracy),
    }


def build_report(
    items: list,
    qwen_results: list,
    cogagent_results: list,
    qwen_stats: dict,
    cogagent_stats: dict,
    overall_status: str,
    recommended: str,
) -> dict:
    """构建完整的 spike_report.json 数据结构。"""
    tz_cst = timezone(timedelta(hours=8))
    timestamp = datetime.now(tz=tz_cst).strftime("%Y-%m-%dT%H:%M:%S+08:00")

    results_list = []
    for (fname, gt), qr, cr in zip(items, qwen_results, cogagent_results):
        results_list.append({
            "filename": fname,
            "ground_truth": gt,
            "qwen2vl": {
                "predicted": qr["predicted"],
                "confidence": qr["confidence"],
                "correct": qr["correct"],
                "latency_ms": qr["latency_ms"],
            },
            "cogagent": {
                "predicted": cr["predicted"],
                "confidence": cr["confidence"],
                "correct": cr["correct"],
                "latency_ms": cr["latency_ms"],
            },
        })

    recommended_display = MODEL_NAME_MAP.get(recommended, recommended)
    if overall_status == "PASS":
        note = f"推荐主选模型: {recommended_display}，可进入 V1.0 开发"
    elif overall_status == "REVIEW":
        note = "优化 Skill 文件描述后重新测试"
    else:
        note = "进入 Story 1.8 LoRA 微调流程"

    return {
        "timestamp": timestamp,
        "total_screenshots": len(items),
        "models": {
            "qwen2vl": qwen_stats,
            "cogagent": cogagent_stats,
        },
        "overall_status": overall_status,
        "recommended_model": recommended_display if overall_status == "PASS" else None,
        "note": note,
        "results": results_list,
    }


def write_report(report: dict, output_path: str) -> None:
    """将 report 写入 JSON 文件。"""
    output_path = os.path.abspath(output_path)
    os.makedirs(os.path.dirname(output_path), exist_ok=True)
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)
    print(f"\n📄 报告已写入: {output_path}")


def update_config_yaml_example(recommended_model_key: str) -> None:
    """若 PASS，更新 config.yaml.example 中的 http_model_name。"""
    import re
    config_path = os.path.join(
        os.path.dirname(os.path.abspath(__file__)), "..", "config.yaml.example"
    )
    if not os.path.exists(config_path):
        print(
            f"⚠️  警告：config.yaml.example 未找到（{config_path}），跳过自动更新",
            file=sys.stderr
        )
        return

    recommended_model_name = MODEL_NAME_MAP.get(recommended_model_key, recommended_model_key)

    with open(config_path, "r", encoding="utf-8") as f:
        content = f.read()

    new_content = re.sub(
        r'(http_model_name:\s*)"[^"]*"',
        f'\\1"{recommended_model_name}"',
        content,
        count=1,
    )

    with open(config_path, "w", encoding="utf-8") as f:
        f.write(new_content)
    print(f"✅ config.yaml.example 已更新：http_model_name = {recommended_model_name}")


def print_summary(qwen_stats: dict, cogagent_stats: dict, overall_status: str, recommended: str) -> None:
    """打印摘要信息。"""
    print("\n" + "=" * 60)
    print("📊 Spike 验证结果摘要")
    print("=" * 60)
    print(f"  Qwen2.5-VL  准确率: {qwen_stats['accuracy']:.1%}  "
          f"({qwen_stats['correct']}/{qwen_stats['total']})  "
          f"平均延迟: {qwen_stats['avg_latency_ms']:.1f}ms  "
          f"[{qwen_stats['status']}]")
    print(f"  CogAgent    准确率: {cogagent_stats['accuracy']:.1%}  "
          f"({cogagent_stats['correct']}/{cogagent_stats['total']})  "
          f"平均延迟: {cogagent_stats['avg_latency_ms']:.1f}ms  "
          f"[{cogagent_stats['status']}]")
    print("=" * 60)

    best_acc = max(qwen_stats["accuracy"], cogagent_stats["accuracy"])
    if overall_status == "PASS":
        recommended_display = MODEL_NAME_MAP.get(recommended, recommended)
        print(f"✅ PASS — 推荐主选模型: {recommended_display}，可进入 V1.0 开发")
    elif overall_status == "REVIEW":
        print(f"⚠️ REVIEW — 准确率: {best_acc:.1%}，建议优化 Skill 描述后重新测试")
    else:
        print(f"❌ FAIL — 准确率: {best_acc:.1%}，需进入 Story 1.8 LoRA 微调")


def main():
    # Windows GBK 编码兼容：强制 stdout/stderr 使用 UTF-8
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "reconfigure"):
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")

    args = parse_args()
    screenshots_dir = os.path.normpath(args.screenshots_dir)
    output_path = os.path.normpath(args.output)

    print(f"🔍 VLM 可行性 Spike 启动")
    print(f"   截图目录: {screenshots_dir}")
    print(f"   输出报告: {output_path}")
    if args.dry_run:
        print("   模式: --dry-run（跳过真实推理）")

    # 加载标注文件，验证截图文件存在
    labels = load_labels(screenshots_dir)
    items = list(labels.items())
    print(f"   截图数量: {len(items)} 张\n")

    # 顺序执行推理：先跑 Qwen2.5-VL，再跑 CogAgent
    print("📌 [1/2] 开始 Qwen2.5-VL 推理...")
    qwen_results = run_inference_for_model(
        model_key="qwen2vl",
        model_path=args.qwen_path,
        items=items,
        screenshots_dir=screenshots_dir,
        dry_run=args.dry_run,
    )

    print("\n📌 [2/2] 开始 CogAgent 推理...")
    cogagent_results = run_inference_for_model(
        model_key="cogagent",
        model_path=args.cogagent_path,
        items=items,
        screenshots_dir=screenshots_dir,
        dry_run=args.dry_run,
    )

    # 计算统计
    qwen_stats = compute_model_stats(items, qwen_results)
    cogagent_stats = compute_model_stats(items, cogagent_results)
    overall_status, recommended = determine_overall_status(
        qwen_stats["accuracy"], cogagent_stats["accuracy"]
    )

    # 构建并写入报告
    report = build_report(
        items, qwen_results, cogagent_results,
        qwen_stats, cogagent_stats,
        overall_status, recommended,
    )
    write_report(report, output_path)

    # PASS 时更新 config.yaml.example（非 dry-run 模式）
    if overall_status == "PASS" and not args.dry_run:
        update_config_yaml_example(recommended)

    # 打印摘要
    print_summary(qwen_stats, cogagent_stats, overall_status, recommended)

    # 退出码：PASS=0, REVIEW=1, FAIL=2
    exit_codes = {"PASS": 0, "REVIEW": 1, "FAIL": 2}
    sys.exit(exit_codes.get(overall_status, 2))


if __name__ == "__main__":
    main()
