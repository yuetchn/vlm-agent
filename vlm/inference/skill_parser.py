"""Skill Markdown 文件解析器。

解析 Skill 文件的 YAML frontmatter（5 个必填字段）和正文，
正文用于注入 VLM prompt（skill_context）。
"""
import os
import yaml

REQUIRED_FIELDS = ["name", "version", "trigger", "confidence_threshold", "description"]


class SkillParseError(ValueError):
    pass


_STRING_FIELDS = ["name", "version", "trigger", "description"]


def _split_frontmatter(content: str) -> tuple:
    """将 Skill 内容分割为 frontmatter dict 和正文字符串。"""
    if not content.startswith("---"):
        raise SkillParseError("skill: no YAML frontmatter found (file must start with ---)")
    parts = content.split("---", 2)
    if len(parts) < 3:
        raise SkillParseError("skill: malformed frontmatter (missing closing ---)")
    fm = yaml.safe_load(parts[1]) or {}
    return fm, parts[2].strip()


def parse_skill_content(content: str) -> dict:
    """从字符串解析 Skill，返回包含 5 个必填字段和 body 的 dict。

    Args:
        content: Skill 文件的完整文本内容。

    Returns:
        包含 name、version、trigger、confidence_threshold、description、body 的 dict。

    Raises:
        SkillParseError: 缺少必填字段、字段类型错误或 confidence_threshold 非法时抛出。
    """
    fm, body = _split_frontmatter(content)
    for field in REQUIRED_FIELDS:
        if field not in fm or fm[field] is None or fm[field] == "":
            raise SkillParseError(f"skill: missing required field: {field}")
    for field in _STRING_FIELDS:
        if not isinstance(fm[field], str):
            raise SkillParseError(
                f"skill: field '{field}' must be a string, got {type(fm[field]).__name__}"
            )
    ct = fm["confidence_threshold"]
    if not isinstance(ct, (int, float)) or not (0.0 <= float(ct) <= 1.0):
        raise SkillParseError("skill: confidence_threshold must be float in [0, 1]")
    return {**{k: fm[k] for k in REQUIRED_FIELDS}, "body": body}


def parse_skill_file(path: str, base_dir: str | None = None) -> dict:
    """从文件路径解析 Skill。

    Args:
        path: Skill Markdown 文件的路径。
        base_dir: 若提供，则校验 path 解析后必须位于此目录内，防止路径遍历攻击。
                  推荐调用方传入 skills/ 目录的绝对路径。

    Returns:
        包含 name、version、trigger、confidence_threshold、description、body 的 dict。

    Raises:
        SkillParseError: 路径遍历、缺少必填字段、字段类型错误、编码错误或
                         confidence_threshold 非法时抛出。
    """
    resolved = os.path.realpath(path)
    if base_dir is not None:
        resolved_base = os.path.realpath(base_dir)
        # 确保 resolved 在 resolved_base 目录内（含子目录）
        if not resolved.startswith(resolved_base + os.sep) and resolved != resolved_base:
            raise SkillParseError(
                f"skill: path traversal detected — '{path}' resolves outside base_dir '{base_dir}'"
            )
    try:
        with open(resolved, encoding="utf-8") as f:
            return parse_skill_content(f.read())
    except UnicodeDecodeError as e:
        raise SkillParseError(f"skill: file encoding error (expected UTF-8): {e}") from e
