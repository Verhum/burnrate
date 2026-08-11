#!/usr/bin/env python3
"""
Generate a Burnrate app icon: Anthropic-logo-shaped mask over concentric
fire-colored rings (yellow center -> orange -> red -> dark red outside).

Uses the actual Anthropic logo SVG path from dashboardicons.com.
"""

from PIL import Image, ImageDraw
import re
import os

SIZE = 1024

FIRE_COLORS = [
    (255, 255, 220),  # pale yellow (very center)
    (255, 255, 140),  # bright yellow
    (255, 230, 60),   # golden yellow
    (255, 200, 0),    # gold
    (255, 165, 0),    # orange
    (255, 130, 0),    # deep orange
    (255, 90, 0),     # red-orange
    (230, 50, 0),     # red
    (190, 20, 0),     # dark red
    (140, 10, 0),     # very dark red
]

# Actual Anthropic logo SVG path (viewBox 0 0 248 248)
# Source: dashboardicons.com/icons/anthropic
ANTHROPIC_SVG_PATH = (
    "M52.4285 162.873L98.7844 136.879L99.5485 134.602L98.7844 133.334"
    "H96.4921L88.7237 132.862L62.2346 132.153L39.3113 131.207"
    "L17.0249 130.026L11.4214 128.844L6.2 121.873L6.7094 118.447"
    "L11.4214 115.257L18.171 115.847L33.0711 116.911L55.485 118.447"
    "L71.6586 119.392L95.728 121.873H99.5485L100.058 120.337"
    "L98.7844 119.392L97.7656 118.447L74.5877 102.732L49.4995 86.1905"
    "L36.3823 76.62L29.3779 71.7757L25.8121 67.2858L24.2839 57.3608"
    "L30.6515 50.2716L39.3113 50.8623L41.4763 51.4531L50.2636 58.1879"
    "L68.9842 72.7209L93.4357 90.6804L97.0015 93.6343L98.4374 92.6652"
    "L98.6571 91.9801L97.0015 89.2625L83.757 65.2772L69.621 40.8192"
    "L63.2534 30.6579L61.5978 24.632L60.579 17.4246"
    "L67.8381 7.49965L71.9133 6.19995L81.7193 7.49965L85.7946 11.0443"
    "L91.9074 24.9865L101.714 46.8451L116.996 76.62L121.453 85.4816"
    "L123.873 93.6343L124.764 96.1155H126.292L126.292 94.6976"
    "L127.566 77.9197L129.858 57.3608L132.15 30.8942L132.915 23.4505"
    "L136.608 14.4708L143.994 9.62643L149.725 12.344L154.437 19.0788"
    "L153.8 23.4505L150.998 41.6463L145.522 70.1215L141.957 89.2625"
    "H143.994L146.414 86.7813L156.093 74.0206L172.266 53.698"
    "L179.398 45.6635L187.803 36.802L193.152 32.5484H203.34"
    "L210.726 43.6549L207.415 55.1159L196.972 68.3492L188.312 79.5739"
    "L175.896 96.2095L168.191 109.585L168.882 110.689L170.738 110.53"
    "L198.755 104.504L213.91 101.787L231.994 98.7149L240.144 102.496"
    "L241.036 106.395L237.852 114.311L218.495 119.037L195.826 123.645"
    "L162.07 131.592L161.696 131.893L162.137 132.547L177.36 133.925"
    "L183.855 134.279H199.774L229.447 136.524L237.215 141.605"
    "L241.8 147.867L241.036 152.711L229.065 158.737L213.019 154.956"
    "L175.45 145.977L162.587 142.787H160.805L160.805 143.85"
    "L171.502 154.366L191.242 172.089L215.82 195.011L217.094 200.682"
    "L213.91 205.172L210.599 204.699L188.949 188.394L180.544 181.069"
    "L161.696 165.118H160.422L160.422 166.772L164.752 173.152"
    "L187.803 207.771L188.949 218.405L187.294 221.832L181.308 223.959"
    "L174.813 222.777L161.187 203.754L147.305 182.486L136.098 163.345"
    "L134.745 164.2L128.075 235.42L125.019 239.082L117.887 241.8"
    "L111.902 237.31L108.718 229.984L111.902 215.452L115.722 196.547"
    "L118.779 181.541L121.58 162.873L123.291 156.636L123.14 156.219"
    "L121.773 156.449L107.699 175.752L86.304 204.699L69.3663 222.777"
    "L65.291 224.431L58.2867 220.768L58.9235 214.27L62.8713 208.48"
    "L86.304 178.705L100.44 160.155L109.551 149.507L109.462 147.967"
    "L108.959 147.924L46.6977 188.512L35.6182 189.93L30.7788 185.44"
    "L31.4156 178.115L33.7079 175.752L52.4285 162.873Z"
)

SVG_VIEWBOX = 248.0


def parse_svg_path(path_str):
    """Parse SVG path string into a list of (x, y) points."""
    points = []
    tokens = re.findall(r'[MLHVCSQTAZ]|[-+]?[0-9]*\.?[0-9]+', path_str)

    cx, cy = 0.0, 0.0
    i = 0
    while i < len(tokens):
        tok = tokens[i]
        if tok == 'M':
            cx, cy = float(tokens[i+1]), float(tokens[i+2])
            points.append((cx, cy))
            i += 3
        elif tok == 'L':
            cx, cy = float(tokens[i+1]), float(tokens[i+2])
            points.append((cx, cy))
            i += 3
        elif tok == 'H':
            cx = float(tokens[i+1])
            points.append((cx, cy))
            i += 2
        elif tok == 'V':
            cy = float(tokens[i+1])
            points.append((cx, cy))
            i += 2
        elif tok == 'C':
            # Cubic bezier - skip control points, use endpoint
            cx, cy = float(tokens[i+5]), float(tokens[i+6])
            points.append((cx, cy))
            i += 7
        elif tok == 'Z':
            i += 1
        else:
            # Bare number = implicit L command continuation
            cx, cy = float(tok), float(tokens[i+1])
            points.append((cx, cy))
            i += 2

    return points


def scale_points(points, target_size, padding_frac=0.1):
    """Scale SVG points to target size with padding."""
    padding = target_size * padding_frac
    available = target_size - 2 * padding
    scale = available / SVG_VIEWBOX
    offset_x = padding
    offset_y = padding
    return [(x * scale + offset_x, y * scale + offset_y) for x, y in points]


def lerp_color(c1, c2, t):
    return tuple(int(c1[i] + (c2[i] - c1[i]) * t) for i in range(3))


def fire_color_at_radius(r, max_r):
    t = min(r / max_r, 1.0)
    n = len(FIRE_COLORS) - 1
    idx = t * n
    i = min(int(idx), n - 1)
    frac = idx - i
    return lerp_color(FIRE_COLORS[i], FIRE_COLORS[i + 1], frac)


def create_fire_circle(size):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    cx, cy = size // 2, size // 2
    max_r = size // 2
    draw = ImageDraw.Draw(img)
    for r in range(max_r, 0, -1):
        color = fire_color_at_radius(r, max_r)
        bbox = (cx - r, cy - r, cx + r, cy + r)
        draw.ellipse(bbox, fill=color + (255,))
    return img


def draw_anthropic_logo_mask(size):
    mask = Image.new("L", (size, size), 0)
    draw = ImageDraw.Draw(mask)
    points = parse_svg_path(ANTHROPIC_SVG_PATH)
    scaled = scale_points(points, size, padding_frac=0.08)
    draw.polygon(scaled, fill=255)
    return mask


def add_rounded_rect_mask(img, radius):
    size = img.size[0]
    mask = Image.new("L", (size, size), 0)
    draw = ImageDraw.Draw(mask)
    draw.rounded_rectangle(
        [(0, 0), (size - 1, size - 1)],
        radius=radius,
        fill=255,
    )
    result = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    result.paste(img, mask=mask)
    return result


def create_icon():
    fire = create_fire_circle(SIZE)
    logo_mask = draw_anthropic_logo_mask(SIZE)
    bg_color = (15, 15, 20, 255)
    background = Image.new("RGBA", (SIZE, SIZE), bg_color)
    background.paste(fire, mask=logo_mask)
    corner_radius = int(SIZE * 0.22)
    icon = add_rounded_rect_mask(background, corner_radius)
    return icon


def create_icns(png_path, icns_path):
    iconset_dir = png_path.replace(".png", ".iconset")
    os.makedirs(iconset_dir, exist_ok=True)
    img = Image.open(png_path)
    sizes = [16, 32, 64, 128, 256, 512, 1024]
    for s in sizes:
        resized = img.resize((s, s), Image.LANCZOS)
        if s <= 512:
            resized.save(os.path.join(iconset_dir, f"icon_{s}x{s}.png"))
        if s >= 32:
            half = s // 2
            resized.save(os.path.join(iconset_dir, f"icon_{half}x{half}@2x.png"))
    os.system(f"iconutil -c icns '{iconset_dir}' -o '{icns_path}'")
    os.system(f"rm -rf '{iconset_dir}'")


def create_ico(png_path, ico_path):
    img = Image.open(png_path)
    sizes = [(16, 16), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    icons = [img.resize(s, Image.LANCZOS) for s in sizes]
    icons[0].save(ico_path, format="ICO", sizes=sizes, append_images=icons[1:])


if __name__ == "__main__":
    workdir = os.path.dirname(os.path.abspath(__file__))

    icon = create_icon()

    png_path = os.path.join(workdir, "icon.png")
    icon.save(png_path, "PNG")
    print(f"Saved {png_path}")

    preview = icon.resize((512, 512), Image.LANCZOS)
    preview_path = os.path.join(workdir, "icon_preview.png")
    preview.save(preview_path, "PNG")
    print(f"Saved {preview_path}")

    icns_path = os.path.join(workdir, "icon.icns")
    create_icns(png_path, icns_path)
    print(f"Saved {icns_path}")

    ico_path = os.path.join(workdir, "icon.ico")
    create_ico(png_path, ico_path)
    print(f"Saved {ico_path}")

    # Generate individual size PNGs for Tauri
    for s in [32, 64, 128, 256]:
        resized = icon.resize((s, s), Image.LANCZOS)
        resized.save(os.path.join(workdir, f"{s}x{s}.png"), "PNG")
        print(f"Saved {s}x{s}.png")

    s128_2x = icon.resize((256, 256), Image.LANCZOS)
    s128_2x.save(os.path.join(workdir, "128x128@2x.png"), "PNG")
    print("Saved 128x128@2x.png")

    print("Done!")
