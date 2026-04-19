"""
train.py — 调用 LLaMA-Factory CLI 执行 QLoRA 微调训练。

用法:
    python train.py --model-path D:/models/Qwen2.5-VL-7B-Instruct
    python train.py --model-path D:/models/Qwen3-VL-4B-Instruct --epochs 5

输出:
    vlm/lora_training/output/lora_weights/   训练权重
    vlm/lora_training/logs/train_{timestamp}.jsonl  训练日志
"""
import argparse
import datetime
import json
import os
import pathlib
import subprocess
import sys

import yaml


SCRIPT_DIR = pathlib.Path(__file__).parent.resolve()
CONFIG_TEMPLATE = SCRIPT_DIR / "qwen2vl_lora_config.yaml"
LOGS_DIR = SCRIPT_DIR / "logs"


def parse_args():
    parser = argparse.ArgumentParser(description="LoRA 微调训练入口（调用 LLaMA-Factory CLI）")
    parser.add_argument(
        "--model-path", required=True,
        help="HuggingFace 模型目录路径（如 D:/models/Qwen2.5-VL-7B-Instruct）"
    )
    parser.add_argument(
        "--epochs", type=int, default=None,
        help="覆盖训练轮数（默认使用 yaml 中的 num_train_epochs）"
    )
    parser.add_argument(
        "--output-dir", default=None,
        help="覆盖 LoRA 权重输出目录（默认 output/lora_weights）"
    )
    parser.add_argument(
        "--lora-rank", type=int, default=None,
        help="覆盖 LoRA rank（默认使用 yaml 配置）"
    )
    return parser.parse_args()


def resolve_llamafactory_cli() -> str:
    """查找 llamafactory-cli 可执行文件路径。

    优先级：lora_venv（独立环境）> 系统 PATH
    """
    lora_venv_cli = SCRIPT_DIR / "lora_venv" / "Scripts" / "llamafactory-cli.exe"
    if lora_venv_cli.exists():
        return str(lora_venv_cli)
    # Unix/Linux 路径
    lora_venv_cli_unix = SCRIPT_DIR / "lora_venv" / "bin" / "llamafactory-cli"
    if lora_venv_cli_unix.exists():
        return str(lora_venv_cli_unix)
    # 使用系统 PATH 中的 llamafactory-cli
    return "llamafactory-cli"


def build_config(args) -> dict:
    """读取配置模板，注入运行时参数。"""
    if not CONFIG_TEMPLATE.exists():
        print(f"ERROR: 配置模板不存在: {CONFIG_TEMPLATE}", file=sys.stderr)
        sys.exit(1)

    with open(CONFIG_TEMPLATE, encoding="utf-8") as f:
        config = yaml.safe_load(f)

    # 注入模型路径（统一用正斜杠，避免 Windows 路径解析问题）
    config["model_name_or_path"] = str(args.model_path).replace("\\", "/")

    # 可选覆盖参数
    if args.epochs is not None:
        config["num_train_epochs"] = args.epochs
    if args.output_dir is not None:
        config["output_dir"] = str(args.output_dir).replace("\\", "/")
    if args.lora_rank is not None:
        config["lora_rank"] = args.lora_rank
        config["lora_alpha"] = args.lora_rank * 2  # 保持 alpha = 2 * rank 的惯例

    # 确保输出/日志目录路径为相对路径（LLaMA-Factory 相对 CWD 解析）
    # 若未设置 output_dir，确保默认值存在
    if "output_dir" not in config:
        config["output_dir"] = "./output/lora_weights"
    if "logging_dir" not in config:
        config["logging_dir"] = "./logs"

    return config


def write_tmp_config(config: dict) -> pathlib.Path:
    """将运行时配置写入临时文件，文件名含 PID 避免并发冲突。

    Returns:
        临时配置文件路径（调用方负责在训练结束后删除）
    """
    tmp_path = SCRIPT_DIR / f"qwen2vl_lora_config_run_{os.getpid()}.yaml"
    tmp_path.write_text(
        yaml.dump(config, allow_unicode=True, default_flow_style=False),
        encoding="utf-8",
    )
    return tmp_path


def run_training(cli_path: str, tmp_config_path: pathlib.Path) -> tuple:
    """启动 LLaMA-Factory 训练进程，实时转发输出并写入 JSONL 日志。

    Args:
        cli_path:        llamafactory-cli 可执行文件路径
        tmp_config_path: 运行时生成的临时训练配置文件路径

    Returns:
        (log_path, returncode)
    """
    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
    log_path = LOGS_DIR / f"train_{timestamp}.jsonl"

    cmd = [cli_path, "train", str(tmp_config_path)]
    print(f"[train.py] 执行命令: {' '.join(cmd)}")
    print(f"[train.py] 日志路径: {log_path}\n")

    proc = subprocess.Popen(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        encoding="utf-8",
        errors="replace",
        cwd=str(SCRIPT_DIR),  # LLaMA-Factory 相对路径从训练目录解析
    )

    with open(log_path, "w", encoding="utf-8") as log_f:
        for line in proc.stdout:
            # 实时输出到控制台
            print(line, end="", flush=True)

            # 尝试解析为 JSON（LLaMA-Factory 的结构化日志行）
            line_stripped = line.strip()
            if not line_stripped:
                continue
            try:
                entry = json.loads(line_stripped)
                log_f.write(json.dumps(entry, ensure_ascii=False) + "\n")
            except json.JSONDecodeError:
                # 非 JSON 行（进度条、纯文本提示等）记录为 message
                log_f.write(json.dumps({"message": line_stripped}) + "\n")
            log_f.flush()

    proc.wait()
    return log_path, proc.returncode


def main():
    # Windows GBK 编码兼容
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    if hasattr(sys.stderr, "reconfigure"):
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")

    args = parse_args()

    # 验证模型路径存在
    model_path = pathlib.Path(args.model_path)
    if not model_path.exists():
        print(f"ERROR: 模型路径不存在: {model_path}", file=sys.stderr)
        sys.exit(1)
    if not (model_path / "config.json").exists():
        print(
            f"ERROR: {model_path} 下未找到 config.json，请确认为 HuggingFace 模型目录",
            file=sys.stderr,
        )
        sys.exit(1)

    # 验证数据集已准备
    data_dir = SCRIPT_DIR / "data"
    train_json = data_dir / "train.json"
    if not train_json.exists():
        print(
            "ERROR: 训练数据不存在，请先运行 prepare_dataset.py：\n"
            "  python prepare_dataset.py",
            file=sys.stderr,
        )
        sys.exit(1)

    cli_path = resolve_llamafactory_cli()
    print(f"[train.py] LLaMA-Factory CLI: {cli_path}")
    print(f"[train.py] 模型路径: {model_path}")

    config = build_config(args)
    tmp_config = write_tmp_config(config)

    try:
        log_path, returncode = run_training(cli_path, tmp_config)
    finally:
        # 无论训练是否成功，都删除临时配置文件
        if tmp_config.exists():
            tmp_config.unlink()

    if returncode != 0:
        print(
            f"\nERROR: 训练失败，退出码 {returncode}，日志见 {log_path}",
            file=sys.stderr,
        )
        sys.exit(returncode)

    print(f"\n[OK] 训练完成")
    print(f"  LoRA 权重: {config.get('output_dir', './output/lora_weights')}")
    print(f"  训练日志: {log_path}")


if __name__ == "__main__":
    main()
