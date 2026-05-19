#!/usr/bin/env python3
# Copyright 2026 ScitiX
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Compress public WebP images into sibling *-compressed.webp assets.

Run with:
  uv run --with pillow python scripts/compress-images.py
"""

from __future__ import annotations

import argparse
from pathlib import Path

from PIL import Image


def compressed_name(path: Path) -> Path:
    return path.with_name(f"{path.stem}-compressed.webp")


def compress_image(path: Path, *, width: int, quality: int) -> tuple[Path, tuple[int, int]]:
    with Image.open(path) as image:
        image = image.convert("RGB")
        image.thumbnail((width, width), Image.Resampling.LANCZOS)
        output = compressed_name(path)
        image.save(output, "WEBP", quality=quality, method=6)
        return output, image.size


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--public-dir", type=Path, default=Path("public"))
    parser.add_argument("--width", type=int, default=2048)
    parser.add_argument("--quality", type=int, default=76)
    parser.add_argument("images", nargs="*")
    args = parser.parse_args()

    images = args.images or sorted(
        str(path.relative_to(args.public_dir))
        for path in args.public_dir.glob("**/*.webp")
        if not path.stem.endswith("-compressed")
    )

    if not images:
        print(f"No uncompressed WebP images found in {args.public_dir}.")
        return

    for image_name in images:
        source = args.public_dir / image_name
        output, size = compress_image(source, width=args.width, quality=args.quality)
        source_kb = source.stat().st_size / 1024
        output_kb = output.stat().st_size / 1024
        print(f"{source.name} -> {output.name}: {size[0]}x{size[1]}, {source_kb:.1f}KB -> {output_kb:.1f}KB")


if __name__ == "__main__":
    main()
