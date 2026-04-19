"""推理相关单元测试：skill_parser 和 VLMModel 核心逻辑。"""
import os
import sys
import unittest.mock as mock

import pytest

# 确保 vlm/ 目录在 sys.path 中（在 vlm/ 目录外运行 pytest 时需要）
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from inference.skill_parser import (
    REQUIRED_FIELDS,
    SkillParseError,
    parse_skill_content,
)
from inference.model import MODEL_TYPES, VLMModel
from inference.pipeline import InferencePipeline
from conftest import TEST_SKILL_CONTENT


# ──────────────────────────────────────────────
# skill_parser 测试（无需 torch / GPU）
# ──────────────────────────────────────────────

def test_skill_parser_valid():
    """合法 Skill 内容 → 5 个字段均正确解析，body 非空。"""
    result = parse_skill_content(TEST_SKILL_CONTENT)
    assert result["name"] == "match"
    assert result["version"] == "1.0.0"
    assert result["trigger"] == "lobby"
    assert result["confidence_threshold"] == 0.85
    assert result["description"] == "匹配开局流程"
    assert result["body"] != ""


def test_skill_parser_missing_field():
    """缺少 confidence_threshold → 抛出 SkillParseError，错误信息含字段名。"""
    content = """\
---
name: "match"
version: "1.0.0"
trigger: "lobby"
description: "匹配开局流程"
---
正文内容
"""
    with pytest.raises(SkillParseError) as exc_info:
        parse_skill_content(content)
    assert "confidence_threshold" in str(exc_info.value)


def test_skill_parser_invalid_confidence():
    """confidence_threshold: 1.5 → 抛出 SkillParseError。"""
    content = """\
---
name: "match"
version: "1.0.0"
trigger: "lobby"
confidence_threshold: 1.5
description: "匹配开局流程"
---
正文
"""
    with pytest.raises(SkillParseError) as exc_info:
        parse_skill_content(content)
    assert "confidence_threshold" in str(exc_info.value)


def test_skill_parser_no_frontmatter():
    """纯正文无 --- → 抛出 SkillParseError，错误信息提示 frontmatter 缺失（而非 missing field）。"""
    content = "这是一段纯文字，没有 frontmatter。"
    with pytest.raises(SkillParseError) as exc_info:
        parse_skill_content(content)
    assert "frontmatter" in str(exc_info.value)


def test_skill_parser_non_string_field():
    """name 字段为整数 → 抛出 SkillParseError，错误信息含字段名及类型说明。"""
    content = """\
---
name: 123
version: "1.0.0"
trigger: "lobby"
confidence_threshold: 0.85
description: "test"
---
body
"""
    with pytest.raises(SkillParseError) as exc_info:
        parse_skill_content(content)
    assert "name" in str(exc_info.value)


def test_skill_parser_path_traversal(tmp_path):
    """parse_skill_file 提供 base_dir 时，路径遍历攻击应抛出 SkillParseError。"""
    from inference.skill_parser import parse_skill_file

    # 在 tmp_path 内创建合法 skill 文件
    skill_file = tmp_path / "match.md"
    skill_file.write_text(TEST_SKILL_CONTENT, encoding="utf-8")

    # 合法路径：在 base_dir 内，应正常解析
    result = parse_skill_file(str(skill_file), base_dir=str(tmp_path))
    assert result["name"] == "match"

    # 非法路径：遍历到 base_dir 外，应抛出 SkillParseError
    outside = tmp_path.parent / "secret.md"
    outside.write_text(TEST_SKILL_CONTENT, encoding="utf-8")
    with pytest.raises(SkillParseError, match="path traversal"):
        parse_skill_file(str(outside), base_dir=str(tmp_path))


def test_skill_parser_confidence_boundary():
    """confidence_threshold 边界值 0.0、1.0 及整数 0 均合法。"""
    for ct in [0.0, 1.0, 0]:
        content = (
            f"---\nname: m\nversion: \"1.0\"\ntrigger: t\n"
            f"confidence_threshold: {ct}\ndescription: d\n---\nbody"
        )
        result = parse_skill_content(content)
        assert float(result["confidence_threshold"]) == float(ct)


# ──────────────────────────────────────────────
# VLMModel 测试
# ──────────────────────────────────────────────

def test_model_types_defined():
    """MODEL_TYPES 包含 4 个合法值。"""
    assert MODEL_TYPES == {"qwen2vl", "cogagent", "internvl", "generic"}


def test_screenshot_size_limit():
    """传入 > 4MB bytes → ValueError（纯逻辑校验，无需 GPU）。"""
    model = VLMModel("generic", "/fake/path")
    oversized = b"x" * (4 * 1024 * 1024 + 1)
    with pytest.raises(ValueError, match="4MB"):
        model.infer(oversized, "", "")


def test_screenshot_empty():
    """传入空 bytes → ValueError（前置校验，非 PIL 错误）。"""
    model = VLMModel("generic", "/fake/path")
    with pytest.raises(ValueError, match="empty"):
        model.infer(b"", "", "")


def test_model_load_cpu():
    """mock 掉真实加载，验证 generic 模型 load() 走 CPU 路径不抛异常且 is_ready() 为 True。"""
    pytest.importorskip("transformers")
    with mock.patch("torch.cuda.is_available", return_value=False), \
         mock.patch("transformers.AutoProcessor.from_pretrained") as mock_proc, \
         mock.patch("transformers.AutoModelForVision2Seq.from_pretrained") as mock_model:
        mock_proc.return_value = mock.MagicMock()
        mock_model.return_value = mock.MagicMock()
        vlm = VLMModel("generic", "/fake/path")
        vlm.load()
        assert vlm.is_ready() is True
        # 验证 CPU 路径：不应传入 load_in_4bit
        call_kwargs = mock_model.call_args[1]
        assert "load_in_4bit" not in call_kwargs


# ──────────────────────────────────────────────
# InferencePipeline 测试
# ──────────────────────────────────────────────

def test_pipeline_memoryerror_propagates():
    """InferencePipeline.run() 对 MemoryError 必须向上传播（致命异常不吞）。"""
    model = VLMModel("generic", "/fake/path")
    pipeline = InferencePipeline(model)
    with mock.patch.object(model, "infer", side_effect=MemoryError("OOM")):
        with pytest.raises(MemoryError):
            pipeline.run(b"fake", "ctx", "hint")


def test_pipeline_recoverable_error_returns_empty():
    """InferencePipeline.run() 对 RuntimeError 等可恢复异常返回 ("", 0.0)。"""
    model = VLMModel("generic", "/fake/path")
    pipeline = InferencePipeline(model)
    with mock.patch.object(model, "infer", side_effect=RuntimeError("transient")):
        result, confidence = pipeline.run(b"fake", "ctx", "hint")
    assert result == ""
    assert confidence == 0.0
