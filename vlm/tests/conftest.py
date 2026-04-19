"""pytest 共享 fixtures 和测试常量。"""
import pytest


def _cuda_available() -> bool:
    try:
        import torch
        return torch.cuda.is_available()
    except ImportError:
        return False


@pytest.fixture
def gpu_available():
    """返回当前环境是否有 GPU。"""
    return _cuda_available()


@pytest.fixture
def skip_no_gpu():
    """无 GPU 时跳过测试。"""
    if not _cuda_available():
        pytest.skip("no GPU")


# 合法的 Skill 内容字符串，含 5 个 frontmatter 必填字段，用于 parser 测试
TEST_SKILL_CONTENT = """\
---
name: "match"
version: "1.0.0"
trigger: "lobby"
confidence_threshold: 0.85
description: "匹配开局流程"
---
## 行为规则

1. 检测大厅界面
2. 点击匹配按钮
3. 等待匹配完成
"""
