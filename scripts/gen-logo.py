#!/usr/bin/env python3
"""生成七杀 Qisha 的图标资产：logo.svg / logo-mono.svg / logo.png(512) / favicon.ico(16/32/48/64)。

图形：{7/3} 七芒星——圆周上七个顶点，每隔三点相连，闭合为七个锐角的星。
用法：python3 scripts/gen-logo.py web/public

依赖 Pillow（pip install pillow）；输出 logo.svg / logo-mono.svg / logo.png(512) / favicon.ico(16/32/48/64)。
"""
import math, os, sys
from PIL import Image, ImageDraw

OUT = sys.argv[1] if len(sys.argv) > 1 else "."
CX = CY = 24.0
R = 18.4            # 顶点半径（viewBox 为 -2..54）。不能只按顶点留边距：{7/3} 星的顶角只有
STROKE = 2.4        # 25.7°，尖角会比顶点再向外伸出 MITER_RATIO×线宽，按尖端位置反推半径。
# 线宽 → 尖角伸出量（顶点外角平分线方向）：(w/2)/sin(25.7°/2) = 2.247×w
MITER_RATIO = 1.0 / math.sin(math.pi / 14)
APEX_RADIUS = 23.8  # 尖端到星心的距离，viewBox 半宽为 26，留 2.2 的边距
STOPS = [(0.0, (56, 189, 248)), (0.5, (99, 102, 241)), (1.0, (168, 85, 247))]  # 天青 → 靛蓝 → 紫


def tips(radius=R, cx=CX, cy=CY):
    """七个顶点，从正上方起顺时针。"""
    return [(cx + radius * math.cos(math.radians(-90 + k * 360 / 7)),
             cy + radius * math.sin(math.radians(-90 + k * 360 / 7))) for k in range(7)]


def star_path(pts):
    """{7/3} 连线顺序：0→3→6→2→5→1→4→0。"""
    order = [(0 + 3 * i) % 7 for i in range(7)]
    return [pts[i] for i in order] + [pts[0]]


def fmt(p):
    return f"{p[0]:.2f},{p[1]:.2f}"


def write_svgs():
    pts = " ".join(fmt(p) for p in star_path(tips())[:-1])
    comment = "<!-- 七杀 · 七芒星：圆周七点、每隔三点相连，七个锐角如刃 -->"
    svg = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="-2 -2 52 52">
  <defs>
    <linearGradient id="g" x1="4" y1="4" x2="44" y2="44" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#38bdf8"/>
      <stop offset=".5" stop-color="#6366f1"/>
      <stop offset="1" stop-color="#a855f7"/>
    </linearGradient>
  </defs>
  {comment}
  <polygon fill="none" stroke="url(#g)" stroke-width="{STROKE}" stroke-linejoin="miter" stroke-miterlimit="8" points="{pts}"/>
</svg>
'''
    mono = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="-2 -2 52 52">
  <!-- 七杀 · 七芒星（单色版：currentColor 随主题） -->
  <polygon fill="none" stroke="currentColor" stroke-width="{STROKE}" stroke-linejoin="miter" stroke-miterlimit="8" points="{pts}"/>
</svg>
'''
    with open(os.path.join(OUT, "logo.svg"), "w", encoding="utf-8", newline="\n") as f:
        f.write(svg)
    with open(os.path.join(OUT, "logo-mono.svg"), "w", encoding="utf-8", newline="\n") as f:
        f.write(mono)


def lerp(a, b, t):
    return tuple(int(round(a[i] + (b[i] - a[i]) * t)) for i in range(3))


def color_at(t):
    for (t0, c0), (t1, c1) in zip(STOPS, STOPS[1:]):
        if t <= t1:
            return lerp(c0, c1, (t - t0) / (t1 - t0))
    return STOPS[-1][1]


def gradient(size):
    """沿左上→右下对角的线性渐变，与 SVG 的 gradientUnits=userSpaceOnUse 对应。"""
    img = Image.new("RGB", (size, size))
    px = img.load()
    ux = uy = 1 / math.sqrt(2)
    for y in range(size):
        for x in range(size):
            # viewBox -2..52 映射到 0..size；渐变轴从 (4,4) 到 (44,44)
            vx = -2 + 56 * (x + 0.5) / size
            vy = -2 + 56 * (y + 0.5) / size
            t = ((vx - 4) * ux + (vy - 4) * uy) / (40 * math.sqrt(2))
            px[x, y] = color_at(min(1.0, max(0.0, t)))
    return img


def star_mask(size, stroke_units, ss=4):
    """星形描边的 alpha 蒙版；超采样 ss 倍后缩小得到抗锯齿。顶点补尖角三角形保持锐利。
    顶点半径按尖端落在 APEX_RADIUS 上反推，线宽再大也不会被画布边缘截平。"""
    S = size * ss
    scale = S / 56.0
    P = [((x + 2) * scale, (y + 2) * scale)
         for (x, y) in star_path(tips(APEX_RADIUS - MITER_RATIO * stroke_units))[:7]]
    w = stroke_units * scale
    mask = Image.new("L", (S, S), 0)
    d = ImageDraw.Draw(mask)
    # 闭合折线：P 只有 7 个顶点，必须补回起点，否则缺一条边、两个角出现缺口。
    # 折线端点处是平头而不是圆角连接，角根内侧会缺一块：从另一个顶点再画一遍，
    # 让每个顶点至少在一次绘制里是中间点（有圆角连接）。
    for start in (0, 3):
        Q = P[start:] + P[:start] + [P[start]]
        d.line(Q, fill=255, width=int(round(w)), joint="curve")
    # 每个顶点处两条边的夹角很锐，圆角连接会削平尖端：补一个尖角三角形
    for i in range(7):
        T, N, Pv = P[i], P[(i + 1) % 7], P[(i - 1) % 7]
        def unit(a, b):
            dx, dy = b[0] - a[0], b[1] - a[1]
            L = math.hypot(dx, dy)
            return dx / L, dy / L
        u, v = unit(T, Pv), unit(T, N)
        bis = (u[0] + v[0], u[1] + v[1])
        L = math.hypot(*bis)
        bis = (bis[0] / L, bis[1] / L)                  # 指向角的内部
        cos_half = (u[0] * bis[0] + u[1] * bis[1])      # cos(φ/2)
        miter = (w / 2) / math.sqrt(1 - cos_half * cos_half)
        apex = (T[0] - bis[0] * miter, T[1] - bis[1] * miter)
        def side(vec):                                   # 边的法线中朝外的那一个
            n1, n2 = (-vec[1], vec[0]), (vec[1], -vec[0])
            return n1 if (n1[0] * -bis[0] + n1[1] * -bis[1]) > 0 else n2
        su, sv = side(u), side(v)
        d.polygon([(T[0] + su[0] * w / 2, T[1] + su[1] * w / 2), apex, (T[0] + sv[0] * w / 2, T[1] + sv[1] * w / 2)], fill=255)
    return mask.resize((size, size), Image.LANCZOS)


def main():
    write_svgs()
    big = 512
    grad = gradient(big)
    alpha = star_mask(big, STROKE)
    logo = grad.convert("RGBA")
    logo.putalpha(alpha)
    logo.save(os.path.join(OUT, "logo.png"), optimize=True)
    favicon = Image.new("RGBA", (big, big), (0, 0, 0, 0))
    # 页签图标很小：加粗描边保证 16px 下七个角仍可辨（半径随之自动收小，尖端位置不变）
    small_alpha = star_mask(big, STROKE + 1.6)
    favicon.putalpha(small_alpha)
    favicon.paste(grad, (0, 0), small_alpha)
    favicon.save(os.path.join(OUT, "favicon.ico"), format="ICO",
                 sizes=[(16, 16), (32, 32), (48, 48), (64, 64)])
    print("written:", ", ".join(sorted(f for f in os.listdir(OUT) if f.startswith(("logo", "favicon")))))


if __name__ == "__main__":
    main()
