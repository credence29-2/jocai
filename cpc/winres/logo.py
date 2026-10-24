from PIL import Image, ImageDraw, ImageFilter, ImageChops
import math, sys

S = 1024  # render size
SS = 2    # supersample
R = S * SS

def lerp(a, b, t): return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))
TEAL, SKY = (46, 230, 197), (76, 201, 255)

def gradient_img(size, c1, c2, diag=True):
    w, h = size
    im = Image.new("RGBA", size)
    px = im.load()
    for y in range(h):
        for x in range(w):
            t = (x / w + y / h) / 2 if diag else y / h
            px[x, y] = lerp(c1, c2, t) + (255,)
    return im

def round_cap_line(d, p1, p2, width, fill):
    d.line([p1, p2], fill=fill, width=width)
    for p in (p1, p2):
        d.ellipse([p[0]-width/2, p[1]-width/2, p[0]+width/2, p[1]+width/2], fill=fill)

def star(d, x, y, s, fill, pinch=0.26):
    d.polygon([(x, y-s), (x+s*pinch, y-s*pinch), (x+s, y), (x+s*pinch, y+s*pinch),
               (x, y+s), (x-s*pinch, y+s*pinch), (x-s, y), (x-s*pinch, y-s*pinch)], fill=fill)

def render(tile=True):
    u = R / 100.0  # unit: percent of canvas
    canvas = Image.new("RGBA", (R, R), (0, 0, 0, 0))
    if tile:
        # dark tile with soft teal glow in the lower left and a faint top highlight
        bg = Image.new("RGBA", (R, R), (12, 17, 23, 255))
        glow = Image.new("RGBA", (R, R), (0, 0, 0, 0))
        gd = ImageDraw.Draw(glow)
        gd.ellipse([-20*u, 40*u, 70*u, 130*u], fill=(46, 230, 197, 90))
        gd.ellipse([50*u, -30*u, 130*u, 50*u], fill=(76, 201, 255, 50))
        glow = glow.filter(ImageFilter.GaussianBlur(28*u))
        bg = Image.alpha_composite(bg, glow)
        mask = Image.new("L", (R, R), 0)
        ImageDraw.Draw(mask).rounded_rectangle([4*u, 4*u, 96*u, 96*u], radius=22*u, fill=255)
        canvas.paste(bg, (0, 0), mask)
        # subtle inner border
        ring = Image.new("RGBA", (R, R), (0, 0, 0, 0))
        ImageDraw.Draw(ring).rounded_rectangle([4*u, 4*u, 96*u, 96*u], radius=22*u, outline=(255, 255, 255, 28), width=int(1.2*u))
        canvas = Image.alpha_composite(canvas, ring)

    # ---- the K mark, drawn as a mask then filled with gradient
    mark = Image.new("L", (R, R), 0)
    md = ImageDraw.Draw(mark)
    w = 15 * u
    stem_x = 30 * u
    round_cap_line(md, (stem_x, 24*u), (stem_x, 76*u), int(w), 255)            # stem
    round_cap_line(md, (stem_x + 2*u, 52*u), (64*u, 23*u), int(w), 255)         # upper arm
    # lower leg that becomes the sweep: a curve from the stem down-right then flattening out
    pts = []
    for i in range(0, 101):
        t = i / 100
        # quadratic bezier: start near stem, control pulls it down, end sweeps right
        p0 = (stem_x + 2*u, 49*u); p1 = (54*u, 84*u); p2 = (95*u, 64*u)
        x = (1-t)**2*p0[0] + 2*(1-t)*t*p1[0] + t**2*p2[0]
        y = (1-t)**2*p0[1] + 2*(1-t)*t*p1[1] + t**2*p2[1]
        pts.append((x, y))
    # taper: width shrinks along the sweep
    for i in range(len(pts)-1):
        t = i / (len(pts)-1)
        ww = w * (1.0 - 0.88 * max(0, (t - 0.22) / 0.78) ** 0.85)
        round_cap_line(md, pts[i], pts[i+1], max(2, int(ww)), 255)

    grad = gradient_img((R, R), TEAL, SKY)
    k = Image.new("RGBA", (R, R), (0, 0, 0, 0))
    k.paste(grad, (0, 0), mark)
    # outer glow behind the K
    glowk = Image.new("RGBA", (R, R), (0, 0, 0, 0))
    glowk.paste((46, 230, 197, 150), (0, 0), mark)
    glowk = glowk.filter(ImageFilter.GaussianBlur(6*u))
    canvas = Image.alpha_composite(canvas, glowk)
    canvas = Image.alpha_composite(canvas, k)

    # sparkles trailing the sweep
    d = ImageDraw.Draw(canvas)
    star(d, 81*u, 29*u, 7.5*u, (255, 255, 255, 250))
    star(d, 91*u, 43*u, 3.4*u, (255, 255, 255, 225))
    star(d, 72*u, 42*u, 2.2*u, (255, 255, 255, 170))
    return canvas.resize((S, S), Image.LANCZOS)

icon = render(tile=True)
icon.resize((256, 256), Image.LANCZOS).save("icon256.png")
icon.resize((64, 64), Image.LANCZOS).save("icon64.png")
icon.resize((160, 160), Image.LANCZOS).save("preview.png")
mark = render(tile=False)
mark.save("mark1024.png")
# preview sheet: icon at several sizes on dark and light
sheet = Image.new("RGBA", (720, 220), (14, 18, 24, 255))
x = 20
for sz in (160, 96, 48, 32, 16):
    sheet.paste(icon.resize((sz, sz), Image.LANCZOS), (x, 110 - sz // 2), icon.resize((sz, sz), Image.LANCZOS))
    x += sz + 28
light = Image.new("RGBA", (720, 220), (240, 243, 247, 255))
x = 20
for sz in (160, 96, 48, 32, 16):
    light.paste(icon.resize((sz, sz), Image.LANCZOS), (x, 110 - sz // 2), icon.resize((sz, sz), Image.LANCZOS))
    x += sz + 28
out = Image.new("RGBA", (720, 440)); out.paste(sheet, (0, 0)); out.paste(light, (0, 220)); out.save("sheet.png")
print("ok")
