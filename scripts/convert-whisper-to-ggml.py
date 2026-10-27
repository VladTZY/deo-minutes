#!/usr/bin/env python3
"""Convert a Hugging Face Whisper fine-tune (safetensors) to whisper.cpp ggml.

Port of whisper.cpp v1.9.5 models/convert-h5-to-ggml.py that reads the
weights with numpy + safetensors instead of PyTorch/transformers (PyTorch no
longer ships for Intel Macs). Same output format and tensor naming.

Usage: convert-whisper-to-ggml.py <hf-model-dir> <mel_filters.npz> <out.bin>
Needs: pip install numpy safetensors
"""
import json
import struct
import sys
from pathlib import Path

import numpy as np
from safetensors.numpy import load_file

CONV_MAP = {
    "self_attn.k_proj": "attn.key",
    "self_attn.q_proj": "attn.query",
    "self_attn.v_proj": "attn.value",
    "self_attn.out_proj": "attn.out",
    "self_attn_layer_norm": "attn_ln",
    "encoder_attn.q_proj": "cross_attn.query",
    "encoder_attn.v_proj": "cross_attn.value",
    "encoder_attn.out_proj": "cross_attn.out",
    "encoder_attn_layer_norm": "cross_attn_ln",
    "fc1": "mlp.0",
    "fc2": "mlp.2",
    "final_layer_norm": "mlp_ln",
    "encoder.layer_norm.bias": "encoder.ln_post.bias",
    "encoder.layer_norm.weight": "encoder.ln_post.weight",
    "encoder.embed_positions.weight": "encoder.positional_embedding",
    "decoder.layer_norm.bias": "decoder.ln.bias",
    "decoder.layer_norm.weight": "decoder.ln.weight",
    "decoder.embed_positions.weight": "decoder.positional_embedding",
    "decoder.embed_tokens.weight": "decoder.token_embedding.weight",
    "proj_out.weight": "decoder.proj.weight",
}

KEEP_F32 = {"encoder.conv1.bias", "encoder.conv2.bias", "encoder.positional_embedding", "decoder.positional_embedding"}


def bytes_to_unicode():
    bs = list(range(ord("!"), ord("~") + 1)) + list(range(ord("¡"), ord("¬") + 1)) + list(range(ord("®"), ord("ÿ") + 1))
    cs = bs[:]
    n = 0
    for b in range(2**8):
        if b not in bs:
            bs.append(b)
            cs.append(2**8 + n)
            n += 1
    return dict(zip(bs, [chr(c) for c in cs]))


def ggml_name(name: str) -> str:
    nn = name.split(".")[1:]  # drop the leading "model."
    if nn[1] == "layers":
        nn[1] = "blocks"
        sub = ".".join(nn[3:-1])
        if sub == "encoder_attn.k_proj":
            mapped = "attn.key" if nn[0] == "encoder" else "cross_attn.key"
        else:
            mapped = CONV_MAP[sub]
        return ".".join(nn[:3] + [mapped] + nn[-1:])
    joined = ".".join(nn)
    return CONV_MAP.get(joined, joined)


def main():
    if len(sys.argv) != 4:
        sys.exit(__doc__)
    model_dir, mel_path, out_path = Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3])
    hp = json.loads((model_dir / "config.json").read_text(encoding="utf8"))
    max_length = hp.get("max_length") or hp.get("max_target_positions", 448)
    n_mels = hp["num_mel_bins"]
    with np.load(mel_path) as f:
        filters = f[f"mel_{n_mels}"].astype(np.float32)
    tokens = json.loads((model_dir / "vocab.json").read_text(encoding="utf8"))
    weights = load_file(str(model_dir / "model.safetensors"))

    with open(out_path, "wb") as out:
        out.write(struct.pack("i", 0x67676D6C))  # "ggml"
        for v in (hp["vocab_size"], hp["max_source_positions"], hp["d_model"], hp["encoder_attention_heads"],
                  hp["encoder_layers"], int(max_length), hp["d_model"], hp["decoder_attention_heads"],
                  hp["decoder_layers"], n_mels, 1):  # last: use_f16
            out.write(struct.pack("i", v))
        out.write(struct.pack("ii", *filters.shape))
        filters.tofile(out)

        decoder = {v: k for k, v in bytes_to_unicode().items()}
        out.write(struct.pack("i", len(tokens)))
        for tok, _ in sorted(tokens.items(), key=lambda kv: kv[1]):
            text = bytearray(decoder[c] for c in tok)
            out.write(struct.pack("i", len(text)))
            out.write(text)

        for src in weights:
            if src == "proj_out.weight":  # tied to the token embedding
                continue
            name = ggml_name(src)
            data = np.squeeze(weights[src]).astype(np.float16)
            if name in ("encoder.conv1.bias", "encoder.conv2.bias"):
                data = data.reshape(data.shape[0], 1)
            n_dims = data.ndim
            ftype = 1
            if n_dims < 2 or name in KEEP_F32:
                data = data.astype(np.float32)
                ftype = 0
            raw = name.encode("utf-8")
            out.write(struct.pack("iii", n_dims, len(raw), ftype))
            for i in range(n_dims):
                out.write(struct.pack("i", data.shape[n_dims - 1 - i]))
            out.write(raw)
            data.tofile(out)
    print(f"wrote {out_path}")


if __name__ == "__main__":
    main()
