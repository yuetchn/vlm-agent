"""推理管道：包裹 VLMModel.infer()，提供统一异常处理。

异常分类：
- 可恢复异常（RuntimeError、ValueError 等）：捕获并记录，返回 ("", 0.0)
- 致命异常（OOM、MemoryError）：不捕获，向上传播触发进程重启
"""
import logging

from .model import VLMModel

logger = logging.getLogger(__name__)


class InferencePipeline:
    """推理管道，封装 VLMModel 并统一处理可恢复异常。"""

    def __init__(self, model: VLMModel):
        self.model = model

    def run(self, screenshot_bytes: bytes, skill_context: str, state_hint: str) -> tuple:
        """执行推理，返回 (result_json_str, confidence_float)。

        可恢复异常（RuntimeError、ValueError 等）被捕获并返回 ("", 0.0)。
        致命异常（OOM、MemoryError）向上传播。

        Args:
            screenshot_bytes: PNG 格式截图字节。
            skill_context: Skill 文件正文。
            state_hint: 当前 FSM 状态提示。

        Returns:
            (result_json_str, confidence)
        """
        try:
            return self.model.infer(screenshot_bytes, skill_context, state_hint)
        except MemoryError:
            raise
        except Exception as exc:
            # 检查是否为 torch OOM（致命，向上传播）
            try:
                import torch
                if isinstance(exc, torch.cuda.OutOfMemoryError):
                    raise
            except ImportError:
                pass
            logger.error("inference pipeline error (recoverable): %s", exc)
            return ("", 0.0)
