"""VLM gRPC 推理服务。

启动顺序：
1. 解析命令行参数
2. 创建 VLMModel 和 InferencePipeline
3. 注册信号处理器（在 server.start() 之前，避免竞态）
4. 启动 gRPC server（先绑定端口，立即响应 HealthCheck）
5. 在后台线程加载模型（加载期间 HealthCheck 返回 ready=false）
6. 等待终止信号
"""
import argparse
import logging
import os
import signal
import sys
import threading
from concurrent import futures

# proto 目录必须在最顶部加入 sys.path，因为 vlm_pb2_grpc.py 内部使用绝对导入 import vlm_pb2
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "proto"))
import vlm_pb2          # noqa: E402
import vlm_pb2_grpc     # noqa: E402

import grpc             # noqa: E402

from inference.model import VLMModel, MODEL_TYPES   # noqa: E402
from inference.pipeline import InferencePipeline    # noqa: E402

VLM_SERVER_VERSION = "1.0.0"

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger(__name__)


class VLMServicer(vlm_pb2_grpc.VLMServiceServicer):
    """gRPC servicer 实现，通过 pipeline.model 访问模型状态。"""

    def __init__(self, pipeline: InferencePipeline):
        self._pipeline = pipeline

    def Infer(self, request, context):
        """处理推理请求。"""
        if not self._pipeline.model.is_ready():
            context.set_code(grpc.StatusCode.UNAVAILABLE)
            context.set_details("model not ready")
            return vlm_pb2.InferResponse()

        if len(request.screenshot) > 4 * 1024 * 1024:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details("screenshot exceeds 4MB gRPC limit")
            return vlm_pb2.InferResponse()

        if not request.skill_context.strip():
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details("skill_context must not be empty")
            return vlm_pb2.InferResponse()

        if not request.state_hint.strip():
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details("state_hint must not be empty")
            return vlm_pb2.InferResponse()

        try:
            result, confidence = self._pipeline.run(
                request.screenshot, request.skill_context, request.state_hint
            )
        except Exception as exc:
            logger.error("unhandled inference error: %s", exc)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(exc))
            return vlm_pb2.InferResponse()

        return vlm_pb2.InferResponse(result=result, confidence=confidence, error_message="")

    def HealthCheck(self, request, context):
        """返回模型就绪状态和版本号。"""
        return vlm_pb2.HealthCheckResponse(
            ready=self._pipeline.model.is_ready(),
            version=VLM_SERVER_VERSION,
        )


def serve(args):
    model = VLMModel(args.model_type, args.model_path)
    pipeline = InferencePipeline(model)
    servicer = VLMServicer(pipeline)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    vlm_pb2_grpc.add_VLMServiceServicer_to_server(servicer, server)
    server.add_insecure_port(f"[::]:{args.port}")

    # 信号处理器在 server.start() 之前注册，避免启动瞬间的竞态窗口
    def _shutdown(signum, frame):
        logger.info("received signal %d, shutting down gracefully", signum)
        server.stop(grace=5)

    signal.signal(signal.SIGINT, _shutdown)
    if hasattr(signal, "SIGTERM"):  # Windows 不支持 SIGTERM
        signal.signal(signal.SIGTERM, _shutdown)

    server.start()
    logger.info("gRPC server started on port %d", args.port)

    # 模型在后台线程加载，异常记录 CRITICAL 日志后向上传播（线程终止，_ready 永不设置）
    def _load_with_logging():
        try:
            model.load()
        except Exception as exc:
            logger.critical("model load FAILED — server will remain UNAVAILABLE: %s", exc, exc_info=True)

    loader = threading.Thread(target=_load_with_logging, daemon=True)
    loader.start()

    server.wait_for_termination()


def parse_args():
    parser = argparse.ArgumentParser(description="VLM gRPC Inference Server")
    parser.add_argument("--port", type=int, required=True, help="gRPC server port")
    parser.add_argument("--model-path", type=str, required=True, help="Path to model directory")
    parser.add_argument(
        "--model-type",
        type=str,
        required=True,
        choices=sorted(MODEL_TYPES),
        help="Model type: qwen2vl / cogagent / internvl / generic",
    )
    return parser.parse_args()


if __name__ == "__main__":
    args = parse_args()
    serve(args)
