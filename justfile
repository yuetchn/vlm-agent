default:
    @just --list

build:
    go build -race -o dist/new_jzd.exe ./cmd/new_jzd

build-release:
    GOOS=windows go build -ldflags="-H windowsgui" -o dist/new_jzd.exe ./cmd/new_jzd

proto:
    rm -rf internal/vlmpb/vlm.pb.go internal/vlmpb/vlm_grpc.pb.go vlm/proto/vlm_pb2.py vlm/proto/vlm_pb2_grpc.py
    protoc --go_out=. --go-grpc_out=. --go_opt=module=github.com/zerfx/new_jzd --go-grpc_opt=module=github.com/zerfx/new_jzd proto/vlm.proto
    python -m grpc_tools.protoc -Iproto --python_out=vlm/proto/ --grpc_python_out=vlm/proto/ vlm.proto

vlm-setup:
    cd vlm && uv venv .venv --python 3.11 && uv pip install -r requirements.txt

vlm-dev:
    cd vlm && .venv/Scripts/python server.py

test:
    go test ./internal/...

check:
    go run ./cmd/new_jzd --check

spike qwen_path cogagent_path:
    cd vlm && .venv/Scripts/python spike.py --qwen-path "{{qwen_path}}" --cogagent-path "{{cogagent_path}}"

# 准备 LoRA 训练数据集（labels.json → LLaMA-Factory ShareGPT 格式）
lora-prepare:
    cd vlm/lora_training && lora_venv/Scripts/python prepare_dataset.py

# LoRA 微调训练（必须提供模型路径）
# 用法: just lora-train "D:/models/Qwen2.5-VL-7B-Instruct"
lora-train model_path:
    cd vlm/lora_training && lora_venv/Scripts/python train.py --model-path "{{model_path}}"

# 合并 LoRA 权重到 base model
# 用法: just lora-export "D:/models/Qwen2.5-VL-7B-Instruct"
lora-export model_path:
    cd vlm/lora_training && lora_venv/Scripts/python export_model.py --model-path "{{model_path}}"

# 通过 HTTP API 运行 Spike 验证（微调后验证用）
# 用法: just spike-http  或  just spike-http "http://localhost:11434/v1" "qwen2.5-vl-finetuned:latest"
spike-http endpoint="http://localhost:11434/v1" model="qwen2.5-vl-finetuned:latest":
    cd vlm && .venv/Scripts/python spike.py --http-endpoint "{{endpoint}}" --http-model "{{model}}"

clean:
    rm -rf dist/
