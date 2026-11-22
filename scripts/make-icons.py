#!/usr/bin/env python3
"""Render web/public/icon.svg (16x16 pixel art made of <rect>s) to the PNG
icons iOS / PWA need. Run after changing icon.svg: scripts/make-icons.py"""
import re
from pathlib import Path
from PIL import Image, ImageDraw

root = Path(__file__).resolve().parent.parent / "web" / "public"
svg = (root / "icon.svg").read_text()
rects = [dict(re.findall(r'(\w+)="([^"]*)"', m)) for m in re.findall(r"<rect ([^>]*)/>", svg)]

def render(size, pad):
    # pad: fraction of the canvas kept empty around the art (maskable icons)
    img = Image.new("RGB", (size, size), "#1d1b19")
    d = ImageDraw.Draw(img)
    unit = size * (1 - 2 * pad) / 16
    off = size * pad
    for r in rects[1:]:  # rects[0] is the rounded background
        x, y = float(r["x"]), float(r["y"])
        w, h = float(r["width"]), float(r["height"])
        d.rectangle([off + x * unit, off + y * unit, off + (x + w) * unit - 1, off + (y + h) * unit - 1], fill=r["fill"])
    return img

render(180, 0.12).save(root / "apple-touch-icon.png")
render(192, 0.12).save(root / "icon-192.png")
render(512, 0.12).save(root / "icon-512.png")
print("wrote apple-touch-icon.png, icon-192.png, icon-512.png")
