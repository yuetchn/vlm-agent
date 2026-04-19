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

clean:
    rm -rf dist/
