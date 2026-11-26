"""Generate gnofy 'split shard' brand assets (SVG + PNG + ICO). Deterministic."""
import sys, os
from PIL import Image, ImageDraw, ImageFilter
OUT = sys.argv[1]; os.makedirs(f"{OUT}/brand", exist_ok=True)
SOL, SOL2, GNO, HOT, BG = "#a892f4", "#6f58c9", "#60ab96", "#8ff5d2", "#050605"
FHOT = "#4fd1a8"  # favicon: reads on light AND dark tabs
LIGHT = {SOL:"#49216b", SOL2:"#7c4fa8", GNO:"#226c57", HOT:"#144134"}

# ---------- full-detail mark, viewBox 0 0 120 120 ----------
def full_shapes():
    S = [("poly", [(60,6),(24,58),(60,58)], SOL), ("poly", [(60,6),(96,58),(60,58)], SOL2),
         ("rect", (14,60.5,92,2.5), HOT)]
    s = 7
    for y in range(66,112,s):
        for x in range(24,96,s):
            cx, cy = x+s/2, y+s/2
            half = 36*(1-(cy-62)/50)
            if abs(cx-60) < half-1:
                S.append(("rect", (x,y,s-1.6,s-1.6), HOT if (x+y)%3==0 else GNO))
    return S, 120

# ---------- simplified favicon mark, viewBox 0 0 32 32 ----------
def fav_shapes():
    S = [("poly", [(16,.5),(4,14.5),(16,14.5)], SOL), ("poly", [(16,.5),(28,14.5),(16,14.5)], SOL2),
         ("rect", (2,15.5,28,1.6), FHOT)]
    rows = [(18,[8,12,16,20]),(22,[10,14,18]),(26,[12,16]),(29,[14])]
    k = 0
    for y, xs in rows:
        for x in xs:
            S.append(("rect", (x,y,3,3) if y<29 else (14.5,29,3,3), FHOT if k%2==0 else GNO)); k += 1
    return S, 32

def to_svg(shapes, vb, bg=None, pad=0):
    v = vb + 2*pad
    o = [f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{-pad} {-pad} {v} {v}" width="{v}" height="{v}">']
    if bg: o.append(f'<rect x="{-pad}" y="{-pad}" width="{v}" height="{v}" fill="{bg}"/>')
    for kind, g, c in shapes:
        if kind == "poly": o.append(f'<polygon points="{" ".join(f"{x:g},{y:g}" for x,y in g)}" fill="{c}"/>')
        else: x,y,w,h = g; o.append(f'<rect x="{x:g}" y="{y:g}" width="{w:g}" height="{h:g}" fill="{c}"/>')
    o.append('</svg>'); return "\n".join(o)+"\n"

def render(shapes, vb, size, bg=None, pad=0.0, glow=False):
    SS = 8; W = size*SS; v = vb*(1+2*pad); sc = W/v; off = vb*pad*sc
    img = Image.new("RGBA", (W,W), bg or (0,0,0,0)); d = ImageDraw.Draw(img)
    def P(x,y): return (off+x*sc, off+y*sc)
    def draw(dr):
        for kind,g,c in shapes:
            if kind=="poly": dr.polygon([P(x,y) for x,y in g], fill=c)
            else: x,y,w,h=g; dr.rectangle([P(x,y), (off+(x+w)*sc-1, off+(y+h)*sc-1)], fill=c)
    if glow:
        layer = Image.new("RGBA",(W,W),(0,0,0,0)); draw(ImageDraw.Draw(layer))
        g = layer.filter(ImageFilter.GaussianBlur(W/60)); a = g.getchannel("A").point(lambda p:int(p*.55)); g.putalpha(a)
        img.alpha_composite(g)
    draw(d)
    return img.resize((size,size), Image.LANCZOS)

full, fvb = full_shapes(); fav, svb = fav_shapes()
B = f"{OUT}/brand"
open(f"{B}/gnofy-mark.svg","w").write(to_svg(full,fvb))
open(f"{B}/gnofy-mark-dark.svg","w").write(to_svg(full,fvb,BG,pad=24))
open(f"{OUT}/favicon.svg","w").write(to_svg(fav,svb))
render(full,fvb,1024).save(f"{B}/gnofy-mark-1024.png")
render(full,fvb,512).save(f"{B}/gnofy-mark-512.png")
render(full,fvb,1024,BG,pad=.2,glow=True).save(f"{B}/gnofy-github-avatar.png")
render(full,fvb,180,BG,pad=.14).save(f"{OUT}/apple-touch-icon.png")
icos = [render(fav,svb,s) for s in (16,32,48)]
icos[2].save(f"{OUT}/favicon.ico", sizes=[(16,16),(32,32),(48,48)], append_images=icos[:2])
render(fav,svb,32).save(f"{OUT}/favicon-32.png")
light=[(k,g,LIGHT.get(c,c)) for k,g,c in full]
open(f"{B}/gnofy-mark-light.svg","w").write(to_svg(light,fvb))
render(light,fvb,1024).save(f"{B}/gnofy-mark-light-1024.png")
print("ok")
