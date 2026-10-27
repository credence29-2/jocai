"""Draw the Docket app icon (a page with a folded corner and a D) as .ico and .png.

Run: python scripts/make_icon.py
"""

import os

from PIL import Image, ImageDraw, ImageFont

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "..", "src", "docket", "assets")


def draw(size: int) -> Image.Image:
    s = 4  # supersample
    n = size * s
    im = Image.new("RGBA", (n, n), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    # rounded tile with a vertical gradient
    top, bottom = (35, 99, 235), (20, 60, 170)
    tile = Image.new("RGBA", (n, n))
    td = ImageDraw.Draw(tile)
    for y in range(n):
        t = y / max(1, n - 1)
        td.line([(0, y), (n, y)], fill=tuple(int(a + (b - a) * t) for a, b in zip(top, bottom)) + (255,))
    mask = Image.new("L", (n, n), 0)
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, n - 1, n - 1), radius=int(n * 0.22), fill=255)
    im.paste(tile, (0, 0), mask)
    # page with folded corner
    m = n * 0.22
    x0, y0, x1, y1 = m, n * 0.16, n - m, n - n * 0.16
    fold = (x1 - x0) * 0.3
    d.polygon([(x0, y0), (x1 - fold, y0), (x1, y0 + fold), (x1, y1), (x0, y1)], fill=(255, 255, 255, 255))
    d.polygon([(x1 - fold, y0), (x1 - fold, y0 + fold), (x1, y0 + fold)], fill=(190, 205, 240, 255))
    # the D
    font = None
    for name in ("arialbd.ttf", "DejaVuSans-Bold.ttf", "Arial Bold.ttf"):
        try:
            font = ImageFont.truetype(name, int(n * 0.42))
            break
        except OSError:
            continue
    if font is None:
        font = ImageFont.load_default()
    cx, cy = (x0 + x1) / 2, (y0 + y1) / 2 + n * 0.03
    d.text((cx, cy), "D", font=font, fill=(30, 80, 210, 255), anchor="mm")
    return im.resize((size, size), Image.LANCZOS)


def main():
    os.makedirs(OUT, exist_ok=True)
    big = draw(256)
    big.save(os.path.join(OUT, "docket.png"))
    big.save(os.path.join(OUT, "docket.ico"), sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128),
                                                     (256, 256)])
    print("wrote", OUT)


if __name__ == "__main__":
    main()
