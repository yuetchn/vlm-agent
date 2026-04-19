"""VLM 模型加载与推理模块。

支持多模型后端：qwen2vl、cogagent、internvl、generic。
模型路径和类型均通过构造参数传入，不硬编码。
torch / PIL 在使用时才延迟导入，以便 server.py --help 在无 GPU 环境下正常运行。
"""
import io
import json
import logging
import threading

MODEL_TYPES = {"qwen2vl", "cogagent", "internvl", "generic"}

logger = logging.getLogger(__name__)


class VLMModel:
    """VLM 模型封装，支持多后端加载与推理。"""

    def __init__(self, model_type: str, model_path: str):
        if model_type not in MODEL_TYPES:
            raise ValueError(f"unsupported model_type: {model_type}. must be one of {MODEL_TYPES}")
        self._model_type = model_type
        self._model_path = model_path
        self._model = None
        self._processor = None
        self._tokenizer = None
        self._ready_event = threading.Event()
        # _device_map 在 load() 时确定，避免模块导入时触发 torch 导入
        self._device_map: str | None = None

    def load(self) -> None:
        """根据 model_type 加载对应模型，完成后发出就绪信号。"""
        import torch
        self._device_map = "cuda" if torch.cuda.is_available() else "cpu"
        logger.info("loading model type=%s path=%s device_map=%s",
                    self._model_type, self._model_path, self._device_map)
        loaders = {
            "qwen2vl": self._load_qwen2vl,
            "cogagent": self._load_cogagent,
            "internvl": self._load_internvl,
            "generic": self._load_generic,
        }
        try:
            loaders[self._model_type]()
        except Exception:
            # 加载失败时清理已部分初始化的状态
            self._model = None
            self._processor = None
            self._tokenizer = None
            raise
        self._ready_event.set()
        logger.info("model loaded and ready")

    def is_ready(self) -> bool:
        """返回模型是否已加载完成。"""
        return self._ready_event.is_set()

    def infer(self, screenshot_bytes: bytes, skill_context: str, state_hint: str) -> tuple:
        """执行推理，返回 (result_json_str, confidence_float)。

        Args:
            screenshot_bytes: PNG 格式截图字节（非空，不超过 4MB）。
            skill_context: Skill 文件正文（注入 VLM prompt）。
            state_hint: 当前 FSM 状态提示。

        Returns:
            (result_json_str, confidence): result 为 JSON 字符串，confidence 为 [0, 1] float。

        Raises:
            ValueError: 截图为空或超过 4MB 时抛出。
        """
        if len(screenshot_bytes) == 0:
            raise ValueError("screenshot_bytes must not be empty")
        if len(screenshot_bytes) > 4 * 1024 * 1024:
            raise ValueError("screenshot exceeds 4MB gRPC limit")

        from PIL import Image as _Image
        image = _Image.open(io.BytesIO(screenshot_bytes))
        prompt = self._build_prompt(skill_context, state_hint)

        try:
            if self._model_type == "qwen2vl":
                return self._infer_qwen2vl(image, prompt)
            elif self._model_type == "cogagent":
                return self._infer_cogagent(image, prompt)
            elif self._model_type == "internvl":
                return self._infer_internvl(image, prompt)
            else:
                return self._infer_generic(image, prompt)
        except MemoryError:
            raise
        except Exception as e:
            import torch
            if isinstance(e, torch.cuda.OutOfMemoryError):
                raise
            logger.error("inference error: %s", e)
            return ("", 0.0)

    # ──────────────────────────────────────────────
    # 私有：模型加载
    # ──────────────────────────────────────────────

    def _load_qwen2vl(self) -> None:
        from transformers import AutoProcessor, Qwen2VLForConditionalGeneration
        load_kwargs = {"device_map": self._device_map}
        if self._device_map != "cpu":
            load_kwargs["load_in_4bit"] = True
        self._processor = AutoProcessor.from_pretrained(self._model_path)
        self._model = Qwen2VLForConditionalGeneration.from_pretrained(
            self._model_path, **load_kwargs
        )

    def _load_cogagent(self) -> None:
        from transformers import AutoTokenizer, AutoModelForCausalLM, AutoProcessor
        load_kwargs = {"device_map": self._device_map, "trust_remote_code": True}
        if self._device_map != "cpu":
            load_kwargs["load_in_4bit"] = True
        self._tokenizer = AutoTokenizer.from_pretrained(
            self._model_path, trust_remote_code=True
        )
        self._processor = AutoProcessor.from_pretrained(
            self._model_path, trust_remote_code=True
        )
        self._model = AutoModelForCausalLM.from_pretrained(
            self._model_path, **load_kwargs
        )

    def _load_internvl(self) -> None:
        from transformers import AutoModel, AutoTokenizer, AutoProcessor
        load_kwargs = {"device_map": self._device_map, "trust_remote_code": True}
        if self._device_map != "cpu":
            load_kwargs["load_in_8bit"] = True
        self._model = AutoModel.from_pretrained(self._model_path, **load_kwargs)
        self._tokenizer = AutoTokenizer.from_pretrained(
            self._model_path, trust_remote_code=True
        )
        # processor 在 load 时初始化，避免每次推理重复加载
        self._processor = AutoProcessor.from_pretrained(
            self._model_path, trust_remote_code=True
        )

    def _load_generic(self) -> None:
        from transformers import AutoProcessor, AutoModelForVision2Seq
        load_kwargs = {"device_map": self._device_map}
        if self._device_map != "cpu":
            load_kwargs["load_in_4bit"] = True
        self._processor = AutoProcessor.from_pretrained(self._model_path)
        self._model = AutoModelForVision2Seq.from_pretrained(
            self._model_path, **load_kwargs
        )

    # ──────────────────────────────────────────────
    # 私有：推理
    # ──────────────────────────────────────────────

    def _build_prompt(self, skill_context: str, state_hint: str) -> str:
        return (
            f"Current game state: {state_hint}\n\n"
            f"Skill rules:\n{skill_context}\n\n"
            "Analyze the screenshot and return a JSON with keys: state, action, confidence."
        )

    def _extract_confidence(self, outputs, default: float = 0.5) -> float:
        """尝试从输出 logits 中提取置信度，失败时返回默认值 0.5（中性置信度，避免误导）。"""
        try:
            if hasattr(outputs, "scores") and outputs.scores:
                import torch
                probs = torch.softmax(outputs.scores[0][0], dim=-1)
                return float(probs.max().item())
        except Exception:
            pass
        return default

    def _parse_output_json(self, text: str, confidence: float) -> tuple:
        """尝试解析模型输出为标准 JSON，confidence 最终 clamp 到 [0, 1]。"""
        text = text.strip()
        start = text.find("{")
        end = text.rfind("}") + 1
        if start >= 0 and end > start:
            candidate = text[start:end]
            try:
                data = json.loads(candidate)
                if "confidence" not in data:
                    data["confidence"] = confidence
                conf = max(0.0, min(1.0, float(data.get("confidence", confidence))))
                data["confidence"] = conf
                return json.dumps(data), conf
            except json.JSONDecodeError:
                pass
        conf = max(0.0, min(1.0, confidence))
        result = json.dumps({"state": "unknown", "action": "none", "confidence": conf})
        return result, conf

    def _infer_qwen2vl(self, image, prompt: str) -> tuple:
        # 使用 processor(images, text) 方式，apply_chat_template 在多数版本返回 tensor 而非 dict
        inputs = self._processor(
            images=image, text=prompt, return_tensors="pt"
        ).to(self._model.device)
        outputs = self._model.generate(
            **inputs, max_new_tokens=256, return_dict_in_generate=True, output_scores=True
        )
        confidence = self._extract_confidence(outputs)
        generated_ids = outputs.sequences[:, inputs["input_ids"].shape[1]:]
        text = self._processor.batch_decode(generated_ids, skip_special_tokens=True)[0]
        return self._parse_output_json(text, confidence)

    def _infer_cogagent(self, image, prompt: str) -> tuple:
        inputs = self._processor(images=image, text=prompt, return_tensors="pt").to(self._model.device)
        outputs = self._model.generate(**inputs, max_new_tokens=256, return_dict_in_generate=True, output_scores=True)
        confidence = self._extract_confidence(outputs)
        generated_ids = outputs.sequences[:, inputs["input_ids"].shape[1]:]
        text = self._tokenizer.batch_decode(generated_ids, skip_special_tokens=True)[0]
        return self._parse_output_json(text, confidence)

    def _infer_internvl(self, image, prompt: str) -> tuple:
        # 复用 load() 时初始化的 self._processor，避免每次推理重新加载
        pixel_values = self._processor(
            images=image, return_tensors="pt"
        ).pixel_values.to(self._model.device)
        input_ids = self._tokenizer(prompt, return_tensors="pt").input_ids.to(self._model.device)
        outputs = self._model.generate(
            input_ids=input_ids, pixel_values=pixel_values, max_new_tokens=256,
            return_dict_in_generate=True, output_scores=True
        )
        confidence = self._extract_confidence(outputs)
        generated_ids = outputs.sequences[:, input_ids.shape[1]:]
        text = self._tokenizer.batch_decode(generated_ids, skip_special_tokens=True)[0]
        return self._parse_output_json(text, confidence)

    def _infer_generic(self, image, prompt: str) -> tuple:
        inputs = self._processor(images=image, text=prompt, return_tensors="pt").to(self._model.device)
        outputs = self._model.generate(**inputs, max_new_tokens=256, return_dict_in_generate=True, output_scores=True)
        confidence = self._extract_confidence(outputs)
        generated_ids = outputs.sequences[:, inputs["input_ids"].shape[1]:]
        text = self._processor.batch_decode(generated_ids, skip_special_tokens=True)[0]
        return self._parse_output_json(text, confidence)
