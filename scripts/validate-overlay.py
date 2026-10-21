#!/usr/bin/env python3
"""校验 overlay 是否会「凭空造出」不存在的操作，以及命令名是否冲突。

为什么需要：Restish 的 spec_files 是 deep-merge，overlay 里写了 vendor 中不存在的
path/method 时会静默新增一个假接口（曾因此产生一个缺参数的 PUT）。这个检查必须在
改动 overlay 后立刻跑，否则要到调用时才 404/报错。

用法:
  scripts/validate-overlay.py [vendor.json] [generated.yaml] [overlay.yaml]
退出码: 0 通过，1 有问题。
"""
from __future__ import annotations

import json
import re
import sys
from collections import defaultdict
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
METHODS = {"get", "post", "put", "delete", "patch", "head", "options", "trace"}


def load_json(p: Path):
    return json.loads(p.read_text())


def load_yaml(p: Path):
    return yaml.safe_load(p.read_text())


def default_vendor() -> Path:
    """取仓库内版本号最高的 spec/nexus-*.json（不硬编码版本号）。"""
    def vkey(p: Path) -> list[int]:
        m = re.search(r"nexus-(.+)\.json$", p.name)
        return [int(x) for x in re.findall(r"\d+", m.group(1))] if m else []

    cands = sorted((ROOT / "spec").glob("nexus-*.json"), key=vkey)
    if not cands:
        raise SystemExit("找不到 spec/nexus-*.json（先跑 scripts/install.sh）")
    return cands[-1]


def main() -> int:
    argv = sys.argv[1:]
    vendor_p = Path(argv[0]) if len(argv) > 0 else default_vendor()
    gen_p = Path(argv[1]) if len(argv) > 1 else ROOT / "spec/nexus-tags.generated.yaml"
    over_p = Path(argv[2]) if len(argv) > 2 else ROOT / "spec/nexus-overlay.yaml"

    vendor = load_json(vendor_p)
    gen = load_yaml(gen_p) if gen_p.exists() else {"paths": {}}
    over = load_yaml(over_p)

    vpaths = vendor.get("paths", {})
    gpaths = gen.get("paths", {}) or {}
    opaths = over.get("paths", {}) or {}

    errors: list[str] = []
    warnings: list[str] = []

    # 1) overlay 引用了 vendor 中不存在的 path
    for p in opaths:
        if p not in vpaths:
            errors.append(f"overlay 引用了不存在的 path: {p}")

    # 2) overlay 引用了 vendor 中不存在的 method（deep-merge 会造出假接口）
    for p, item in opaths.items():
        if p not in vpaths:
            continue
        for k in (item or {}):
            if k.lower() in METHODS and k not in vpaths[p]:
                errors.append(
                    f"overlay 为不存在的 method 写了覆盖: {k.upper()} {p}"
                    f"（vendor 中该 path 只有: {sorted(k for k in vpaths[p] if k in METHODS)}）"
                )

    # 3) 同一分组内 x-cli-name 冲突（Restish 会加后缀消歧，但结果难猜）
    seen: dict[tuple[str, str], str] = {}
    for p, item in opaths.items():
        if p not in vpaths:
            continue
        for k, op in (item or {}).items():
            if k.lower() not in METHODS or not isinstance(op, dict):
                continue
            name = op.get("x-cli-name")
            if not name:
                continue
            # 有效分组 = generated 的 tag → overlay 的 tag → vendor 的第一个 tag
            tag = None
            g = gpaths.get(p, {}).get(k) or {}
            if isinstance(g, dict) and g.get("tags"):
                tag = g["tags"][0]
            if isinstance(op.get("tags"), list) and op["tags"]:
                tag = op["tags"][0]
            if tag is None:
                v = vpaths[p].get(k) or {}
                vt = v.get("tags") or []
                tag = vt[0] if vt else "(无分组)"
            key = (tag, name)
            if key in seen:
                errors.append(
                    f'分组 "{tag}" 内命令名冲突: x-cli-name "{name}" '
                    f"同时用于 {seen[key]} 和 {k.upper()} {p}"
                )
            else:
                seen[key] = f"{k.upper()} {p}"

    # 4) 统计信息（提示用）
    ignored = sum(1 for item in opaths.values() if (item or {}).get("x-cli-ignore"))
    n_renamed = sum(
        1
        for item in opaths.values()
        for op in (item or {}).values()
        if isinstance(op, dict) and op.get("x-cli-name")
    )

    if not over.get("components", {}).get("securitySchemes"):
        warnings.append("overlay 未定义 components.securitySchemes，生成的命令可能不发凭据")

    print(f"vendor : {vendor_p.name}  ({len(vpaths)} paths)")
    print(f"生成层  : {gen_p.name}  ({len(gpaths)} paths)")
    print(f"人工层  : {over_p.name}  ({len(opaths)} paths, {n_renamed} 个改名, {ignored} 个 path/op 被隐藏)")

    for w in warnings:
        print(f"警告: {w}")
    for e in errors:
        print(f"错误: {e}", file=sys.stderr)

    if errors:
        print(f"\n校验失败：{len(errors)} 个问题", file=sys.stderr)
        return 1
    print("\n校验通过")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
