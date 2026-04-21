"""
export_model.py — 将 LoRA 权重合并到 base model，输出完整模型。

用法:
    python export_model.py --model-path D:/models/Qwen2.5-VL-7B-Instruct
    python export_model.py --model-path D:/models/Qwen3-VL-4B-Instruct \
        --lora-path output/lora_weights \
        --output-path output/merged

输出:
    vlm/lora_training/output/merged/   合并后的完整模型（可直接用 Ollama 加载）
"""
import argparse
import pathlib
import sys


SCRIPT_DIR = pathlib.Path(__file__).parent.resolve()


def parse_args():
    parser = argparse.ArgumentParser(description="合并 LoRA 权重到 base model")
    parser.add_argument(
        "--model-path", required=True,
        help="base model HuggingFace 目录路径（如 D:/models/Qwen2.5-VL-7B-Instruct）"
    )
    parser.add_argument(
        "--lora-path",
        default=str(SCRIPT_DIR / "output" / "lora_weights"),
        help="LoRA 权重目录（默认 output/lora_weights）"
    )
    parser.add_argument(
        "--output-path",
        default=str(SCRIPT_DIR / "output" / "merged"),
        help="合并后模型输出目录（默认 output/merged）"
    )
    return parser.parse_args()


def main():
    # Windows GBK 编码兼容
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "reconfigure"):
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")

    args = parse_args()

    base_model_path = pathlib.Path(args.model_path)
    lora_path = pathlib.Path(args.lora_path)
    output_path = pathlib.Path(args.output_path)

    # 验证路径
    if not base_model_path.exists():
        print(f"ERROR: base model 路径不存在: {base_model_path}", file=sys.stderr)
        sys.exit(1)
    if not (base_model_path / "config.json").exists():
        print(
            f"ERROR: {base_model_path} 下未找到 config.json，请确认为 HuggingFace 模型目录",
            file=sys.stderr,
        )
        sys.exit(1)
    if not lora_path.exists():
        print(
            f"ERROR: LoRA 权重目录不存在: {lora_path}\n"
            "请先运行 train.py 完成训练",
            file=sys.stderr,
        )
        sys.exit(1)
    if not (lora_path / "adapter_config.json").exists():
        print(
            f"ERROR: LoRA 权重目录存在但缺少 adapter_config.json: {lora_path}\n"
            "可能训练未完成或中途中断，请重新运行 train.py",
            file=sys.stderr,
        )
        sys.exit(1)

    output_path.mkdir(parents=True, exist_ok=True)

    print(f"[export_model.py] Base model : {base_model_path}")
    print(f"[export_model.py] LoRA 权重  : {lora_path}")
    print(f"[export_model.py] 输出目录   : {output_path}")
    print()

    try:
        from peft import PeftModel
        from transformers import AutoProcessor, AutoModelForVision2Seq
        import torch
    except ImportError as e:
        print(
            f"ERROR: 缺少依赖包 ({e})\n"
            "请在 lora_venv 中安装:\n"
            "  pip install peft transformers torch",
            file=sys.stderr,
        )
        sys.exit(1)

    print("[1/4] 加载 base model（bfloat16，不量化）...")
    model = AutoModelForVision2Seq.from_pretrained(
        str(base_model_path),
        torch_dtype=torch.bfloat16,
        device_map="cpu",  # 合并在 CPU 上进行，避免 VRAM 限制
        trust_remote_code=True,
    )

    print("[2/4] 加载 LoRA adapter...")
    model = PeftModel.from_pretrained(model, str(lora_path))

    print("[3/4] 合并 LoRA 权重到 base model...")
    model = model.merge_and_unload()

    print(f"[4/4] 保存合并后模型到 {output_path}...")
    model.save_pretrained(str(output_path), safe_serialization=True)

    # 同时保存 processor（tokenizer + image processor）
    processor = AutoProcessor.from_pretrained(str(base_model_path), trust_remote_code=True)
    processor.save_pretrained(str(output_path))

    print(f"\n[OK] 模型导出完成: {output_path}")
    print("     可用 Ollama 加载，步骤见 README.md")


if __name__ == "__main__":
    main()
