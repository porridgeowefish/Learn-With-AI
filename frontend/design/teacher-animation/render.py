"""Render a single-play study sequence from GPT-generated cutout puppet layers.

Uses the existing Pillow/numpy installation. No independent frame generation,
optical-flow interpolation or frame blending. All motion is time-based rigging.
"""
from pathlib import Path
import math
import json
import numpy as np
from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[3]
OUT = ROOT / 'frontend/design/teacher-animation/archive/teachers/learning-sequence'
CHECK = ROOT / 'trash/2026-11-06-search-settings/rig-preview'
SIZE = 480
FPS = 50
DURATION = 9
BG = (250, 247, 241, 255)

def ease(x):
    x = max(0., min(1., x))
    return x*x*x*(x*(x*6-15)+10)

def mix(a, b, p):
    return np.asarray(a) * (1-p) + np.asarray(b) * p

def motion(t):
    """Coordinates refer to wrist/grip center. Holds are exact, not slowed motion."""
    thought = np.array([308., 302.])
    start = np.array([281., 323.])
    rest = np.array([311., 315.])
    parked = np.array([337., 357.])
    if t < 2:
        p = ease(t/2)
        wrist = thought + [8*math.sin(math.pi*p), -3*math.sin(math.pi*p)]
        angle = 2*math.sin(math.pi*p)
        return wrist, angle, 0., parked, -48., 0., 0.
    if t < 3:
        return thought, 0., 0., parked, -48., 0., 0.
    parked_grip = parked + [-18.58,-16.73]
    if t < 3.2:
        p = ease((t-3)/.2)
        return mix(thought,parked_grip,p), 0., 0., parked, -48., 0., 0.
    if t < 3.45:
        p = ease((t-3.2)/.25)
        wrist = mix(parked_grip,start,p)
        angle = -48+72.3*p
        offset = np.array([math.sin(math.radians(angle))*25,-math.cos(math.radians(angle))*25])
        return wrist, 2*p, 4*p, wrist-offset, angle, 0., 0.
    if t < 4.35:
        p = ease((t-3.45)/.9)
        wrist = start + [35*p, math.sin(p*math.pi*8)*.8]
        return wrist, 2., 4., wrist+[-10.3,22.8], 24.3, p, 0.
    if t < 4.65:
        p = ease((t-4.35)/.3)
        wrist = mix(start+[35,0], start+[0,12], p) + [0,-5*math.sin(math.pi*p)]
        return wrist, 2., 4., wrist+[-10.3,22.8], 24.3, 1., 0.
    if t < 5.55:
        p = ease((t-4.65)/.9)
        wrist = start + [30*p, 12+math.sin(p*math.pi*7)*.8]
        return wrist, 2., 4., wrist+[-10.3,22.8], 24.3, 1., p
    if t < 6:
        p = ease((t-5.55)/.45)
        q=ease(min((t-5.55)/.30,1))
        angle=24.3-72.3*q
        tip=mix(start+[30,12]+[-10.3,22.8],parked,q)
        offset=np.array([math.sin(math.radians(angle))*25,-math.cos(math.radians(angle))*25])
        wrist=tip+offset if t<5.85 else mix(parked_grip,rest,ease((t-5.85)/.15))
        return wrist, 2*(1-p), 4*(1-p), tip, angle, 1., 1.
    if t < 7:
        return rest, 0., 0., parked, -48., 1., 1.
    p = ease((t-7)/2)
    # Follow the notes with the hand, then settle. Zero velocity at both ends.
    wrist = rest + [-10*math.sin(math.pi*p), 3*math.sin(math.pi*p)]
    return wrist, -2*math.sin(2*math.pi*p), 4*math.sin(math.pi*p), parked, -48., 1., 1.

def assets(name):
    atlas = Image.open(OUT / f'{name}-parts.png').convert('RGBA')
    w,h = atlas.size
    parts = []
    for col,row in [(0,0),(1,0),(0,1),(1,1)]:
        right_margin=50 if (col,row)==(0,1) else 0
        part = atlas.crop((col*w//2,row*h//2,(col+1)*w//2-right_margin,(row+1)*h//2))
        bbox = part.getchannel('A').point(lambda a: 255 if a>30 else 0).getbbox()
        parts.append(part.crop(bbox))
    paws=Image.open(OUT/'cute-paws.png').convert('RGBA')
    half=paws.width//2
    paw=paws.crop((0 if name=='penguin' else half,0,half if name=='penguin' else paws.width,paws.height))
    bbox=paw.getchannel('A').point(lambda a:255 if a>30 else 0).getbbox()
    parts[1]=paw.crop(bbox)
    return parts

def affine(canvas, source, matrix):
    inv = np.linalg.inv(matrix)
    layer = source.transform((SIZE,SIZE), Image.Transform.AFFINE,
                             tuple(inv[:2].reshape(-1)), Image.Resampling.BICUBIC)
    canvas.alpha_composite(layer)

def translation(x,y):
    return np.array([[1.,0.,x],[0.,1.,y],[0.,0.,1.]])

def rotation(deg):
    r=math.radians(deg)
    return np.array([[math.cos(r),-math.sin(r),0.], [math.sin(r),math.cos(r),0.], [0.,0.,1.]])

def paw(canvas, source, center, angle=0., mirror=False):
    # Fixed uniform scale: compact silhouette never grows into a forearm.
    scale=46/source.width
    sx=-scale if mirror else scale
    matrix=translation(*center) @ rotation(angle) @ np.diag([sx,scale,1.]) @ translation(-source.width*.5,-source.height*.52)
    affine(canvas,source,matrix)

def render(name, parts, t):
    body,flipper,pencil,book=parts
    wrist,angle,down,tip,penangle,line1,line2=motion(t)
    canvas=Image.new('RGBA',(SIZE,SIZE),BG)
    draw=ImageDraw.Draw(canvas)
    draw.ellipse((116,407,364,425),fill=(235,225,212,255))
    height=310
    width=body.width/body.height*height
    base=translation((SIZE-width)/2,40) @ np.diag([width/body.width,height/body.height,1.])
    pose=translation(240,310+down) @ rotation(angle) @ translation(-240,-310)
    affine(canvas,body,pose @ base)
    # Book stays fixed: all drawn strokes coincide with the pencil tip.
    affine(canvas,book,translation(105,310) @ np.diag([270/book.width,137/book.height,1.]))
    draw=ImageDraw.Draw(canvas)
    for y,progress,length in [(345.8,line1,35),(357.8,line2,30)]:
        if progress>0:
            pts=[]
            for x in np.linspace(0,length*progress,max(2,int(length*progress*2))):
                pts.append((270.7+x,y+math.sin(x/length*math.pi*(8 if y==345.8 else 7))*.8))
            draw.line(pts,fill=(139,113,94,255),width=2)
    paw(canvas,flipper,[181,316],angle=-15,mirror=True)
    # Pencil pivot is its nib, keeping grip and tip attached during writing.
    penmatrix=translation(*tip) @ rotation(penangle) @ np.diag([15/pencil.width,65/pencil.height,1.]) @ translation(-pencil.width*.5,-pencil.height)
    affine(canvas,pencil,penmatrix)
    paw(canvas,flipper,wrist,angle=-8+angle*.7)
    return canvas.convert('RGB')

def build(name):
    parts=assets(name)
    frames=[render(name,parts,i/FPS) for i in range(FPS*DURATION)]
    # Exact endpoint and real holds; non-looping GIF has no NETSCAPE extension.
    frames[-1]=render(name,parts,9.)
    palette_sample=Image.new('RGB',(SIZE*3,SIZE*2))
    for i,t in enumerate([0,1,3.8,4.9,7.8,9]):
        palette_sample.paste(render(name,parts,t),((i%3)*SIZE,(i//3)*SIZE))
    palette=palette_sample.quantize(colors=256,method=Image.Quantize.MEDIANCUT)
    quantized=[f.quantize(palette=palette,dither=Image.Dither.NONE) for f in frames]
    target=OUT / f'{name}-study-v2.gif'
    quantized[0].save(target,save_all=True,append_images=quantized[1:],
                      duration=20,optimize=True,disposal=1)
    frames[-1].save(OUT/f'{name}-study-v2.png')
    palette_sample.resize((960,640)).save(CHECK/f'{name}-contact.png')
    # Verify saved playback, not just render parameters.
    gif=Image.open(target)
    times=[]
    for i in range(gif.n_frames):
        gif.seek(i); times.append(gif.info.get('duration',0))
    assert sum(times)==9000, times
    assert 'loop' not in gif.info
    assert render(name,parts,2.1).tobytes()==render(name,parts,2.9).tobytes()
    assert render(name,parts,6.1).tobytes()==render(name,parts,6.9).tobytes()
    assert abs(motion(4.34)[0][0]-motion(3.45)[0][0])>34
    report={'character':name,'duration_ms':sum(times),'motion_fps':FPS,
            'encoded_frames':gif.n_frames,'loop':False,'size':[SIZE,SIZE],
            'holds_seconds':[[2,3],[6,7]],'end_state':'stopped',
            'method':'fixed-size-round-paws','bytes':target.stat().st_size}
    (OUT/f'{name}-study-v2.json').write_text(json.dumps(report,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(report))

if __name__=='__main__':
    CHECK.mkdir(parents=True,exist_ok=True)
    for name in ['penguin','ghost']:
        build(name)
