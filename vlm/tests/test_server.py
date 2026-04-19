"""gRPC Servicer 单元测试（使用 MockContext，不启动真实 gRPC server）。"""
import os
import sys
import unittest.mock as mock

import grpc
import pytest

# 确保 vlm/ 目录在 sys.path 中
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

# proto 路径
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "proto"))
import vlm_pb2  # noqa: E402

from inference.model import VLMModel
from inference.pipeline import InferencePipeline
from server import VLMServicer, VLM_SERVER_VERSION


class MockContext:
    """模拟 gRPC servicer context，用于测试而无需启动真实 server。"""

    def __init__(self):
        self.code = None
        self.details = None

    def set_code(self, code):
        self.code = code

    def set_details(self, d):
        self.details = d


def _make_servicer_not_loaded():
    """创建一个未加载模型的 VLMServicer。"""
    model = VLMModel("generic", "/fake/path")
    pipeline = InferencePipeline(model)
    return VLMServicer(pipeline)


def test_health_check_before_load():
    """未调用 load() 时，HealthCheck 返回 ready=False。"""
    servicer = _make_servicer_not_loaded()
    ctx = MockContext()
    response = servicer.HealthCheck(vlm_pb2.HealthCheckRequest(), ctx)
    assert response.ready is False


def test_health_check_version():
    """HealthCheck 返回的 version 与 VLM_SERVER_VERSION 一致。"""
    servicer = _make_servicer_not_loaded()
    ctx = MockContext()
    response = servicer.HealthCheck(vlm_pb2.HealthCheckRequest(), ctx)
    assert response.version == VLM_SERVER_VERSION


def test_infer_model_not_ready():
    """未加载模型时 Infer → gRPC context 被设为 UNAVAILABLE。"""
    servicer = _make_servicer_not_loaded()
    ctx = MockContext()
    servicer.Infer(vlm_pb2.InferRequest(), ctx)
    assert ctx.code == grpc.StatusCode.UNAVAILABLE


def test_infer_oversized_screenshot():
    """截图超过 4MB 时 Infer → gRPC context 被设为 INVALID_ARGUMENT。"""
    servicer = _make_servicer_not_loaded()
    ctx = MockContext()
    # mock is_ready() 为 True 以绕过 UNAVAILABLE 检查
    with mock.patch.object(servicer._pipeline.model, "is_ready", return_value=True):
        oversized = b"x" * (4 * 1024 * 1024 + 1)
        servicer.Infer(vlm_pb2.InferRequest(screenshot=oversized), ctx)
    assert ctx.code == grpc.StatusCode.INVALID_ARGUMENT


def test_infer_empty_skill_context():
    """skill_context 为空时 Infer → gRPC context 被设为 INVALID_ARGUMENT。"""
    servicer = _make_servicer_not_loaded()
    ctx = MockContext()
    with mock.patch.object(servicer._pipeline.model, "is_ready", return_value=True):
        servicer.Infer(
            vlm_pb2.InferRequest(screenshot=b"data", skill_context="", state_hint="lobby"),
            ctx,
        )
    assert ctx.code == grpc.StatusCode.INVALID_ARGUMENT
    assert "skill_context" in ctx.details


def test_infer_empty_state_hint():
    """state_hint 为空时 Infer → gRPC context 被设为 INVALID_ARGUMENT。"""
    servicer = _make_servicer_not_loaded()
    ctx = MockContext()
    with mock.patch.object(servicer._pipeline.model, "is_ready", return_value=True):
        servicer.Infer(
            vlm_pb2.InferRequest(screenshot=b"data", skill_context="rules", state_hint=""),
            ctx,
        )
    assert ctx.code == grpc.StatusCode.INVALID_ARGUMENT
    assert "state_hint" in ctx.details
