#!/usr/bin/env python3
"""
Generate the DMG installer background image for Burnrate.

Produces a 660x400 (1x) PNG. Finder DMG backgrounds are rendered at 1x
regardless of display — a @2x image just makes the window twice as big
with the content clipped.

Icon positions match tauri.conf.json and the Makefile AppleScript:
  App:          (180, 170)
  Applications: (480, 170)
"""

from PIL import Image, ImageDraw, ImageFont
import os

W, H = 660, 400

# White, with a hint of a gradient so the window is not a flat blank. The dark
# version this replaced put grey text on near-black and left Finder drawing its
# icon labels in dark text over it, so neither the labels nor the caption were
# readable.
BG_TOP = (255, 255, 255)
BG_BOTTOM = (246, 246, 248)

# Both of these are chosen against white: the arrow stays visible without
# competing with the icons, and the caption clears WCAG AA at 14px.
ARROW_COLOR = (150, 150, 158)
SUBTITLE_COLOR = (90, 90, 98)

APP_CENTER_X = 180
APPS_CENTER_X = 480
ICON_CENTER_Y = 170


def lerp(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(len(a)))


def draw_gradient_bg(img):
    draw = ImageDraw.Draw(img)
    for y in range(H):
        t = y / H
        color = lerp(BG_TOP, BG_BOTTOM, t)
        draw.line([(0, y), (W, y)], fill=color)


def draw_arrow(draw):
    gap = 70
    x1 = APP_CENTER_X + gap
    x2 = APPS_CENTER_X - gap
    y = ICON_CENTER_Y

    draw.line([(x1, y), (x2, y)], fill=ARROW_COLOR, width=2)

    head_len = 14
    head_w = 8
    points = [
        (x2, y),
        (x2 - head_len, y - head_w),
        (x2 - head_len, y + head_w),
    ]
    draw.polygon(points, fill=ARROW_COLOR)


def draw_text(draw):
    size = 14

    try:
        font = ImageFont.truetype("/System/Library/Fonts/Helvetica.ttc", size)
    except (OSError, IOError):
        try:
            font = ImageFont.truetype("/System/Library/Fonts/SFNSText.ttf", size)
        except (OSError, IOError):
            font = ImageFont.load_default()

    subtitle = "Drag to Applications to install"
    bbox = draw.textbbox((0, 0), subtitle, font=font)
    sw = bbox[2] - bbox[0]
    draw.text(((W - sw) / 2, H - 45), subtitle, fill=SUBTITLE_COLOR, font=font)


def main():
    img = Image.new("RGB", (W, H), BG_TOP)
    draw_gradient_bg(img)
    draw = ImageDraw.Draw(img)
    draw_arrow(draw)
    draw_text(draw)

    out_dir = os.path.dirname(os.path.abspath(__file__))
    out_path = os.path.join(out_dir, "background.png")
    img.save(out_path, "PNG")
    print(f"Saved {out_path} ({W}x{H})")


if __name__ == "__main__":
    main()
