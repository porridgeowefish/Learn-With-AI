"""Package whole-character expressions; animate only the isolated question mark."""
from pathlib import Path
from PIL import Image, ImageDraw
import numpy as np
import math
import json

ROOT=Path(__file__).resolve().parents[3]
OUT=ROOT/'frontend/design/teacher-animation/archive/teachers/expressions'
SIZE=480
BG=(250,247,241)
STATES=['thinking','idea','explaining','done']

def smooth(p):
    p=max(0,min(1,p))
    return p*p*(3-2*p)

def save_gif(frames,durations,path):
    sample=Image.new('RGB',(SIZE*len(frames[::25]),SIZE))
    for i,frame in enumerate(frames[::25]):sample.paste(frame,(SIZE*i,0))
    palette=sample.quantize(colors=256)
    indexed=[f.quantize(palette=palette,dither=Image.Dither.NONE) for f in frames]
    indexed[0].save(path,save_all=True,append_images=indexed[1:],duration=durations,disposal=1,optimize=True)
    g=Image.open(path);total=0
    for i in range(g.n_frames):
        g.seek(i);total+=g.info['duration']
    assert total==sum(durations)
    assert 'loop' not in g.info
    return {'duration_ms':total,'encoded_frames':g.n_frames,'loop':False}

def build(name):
    sheet=Image.open(OUT/f'{name}-sheet.png').convert('RGB')
    w,h=sheet.size
    # Bounds isolate only the native-generated question mark, never a limb.
    qbox=(494,54,565,156) if name=='penguin' else (486,62,563,166)
    # Coordinates are specified for this native 1254px sheet.
    qbox=tuple(round(v*w/1254) for v in qbox)
    question=None;images=[]
    for i,state in enumerate(STATES):
        cell=sheet.crop(((i%2)*w//2,(i//2)*h//2,(i%2+1)*w//2,(i//2+1)*h//2))
        if i==0:
            question=cell.crop(qbox).convert('RGBA')
            a=np.array(question)
            # Warm outline/gold foreground, transparent offwhite surroundings.
            alpha=np.clip((245-a[:,:,2].astype(float))*6,0,255).astype('uint8')
            a[:,:,3]=alpha
            question=Image.fromarray(a)
            question=question.crop(question.getbbox())
            ImageDraw.Draw(cell).rectangle(qbox,fill=cell.getpixel((10,10)))
        # Normalize whole illustrations by outline bounds; no separated body parts.
        a=np.array(cell)
        ys,xs=np.where((a[:,:,0]<155)&(a[:,:,1]<135)&(a[:,:,2]<120))
        bounds=(max(0,int(xs.min())-3),max(0,int(ys.min())-3),int(xs.max())+4,int(ys.max())+4)
        whole=cell.crop(bounds)
        height=370
        whole=whole.resize((round(whole.width*height/whole.height),height),Image.Resampling.LANCZOS)
        base=Image.new('RGB',(SIZE,SIZE),BG)
        base.paste(whole,((SIZE-whole.width)//2,55))
        images.append(base)
    question=question.resize((49,round(question.height*49/question.width)),Image.Resampling.LANCZOS)

    def thinking(t=None):
        frame=images[0].copy()
        if t is None:
            scale,opacity,dy=1.,1.,0
        elif t<.35 or t>=1.9:
            return frame
        elif t<.8:
            p=(t-.35)/.45
            # Short, gentle overshoot when the symbol appears.
            scale=.3+.7*(1-(1-p)**3)+.12*math.sin(math.pi*p)
            opacity=smooth(p*2);dy=10*(1-p)
        elif t<1.5:
            scale,opacity,dy=1.,1.,0
        else:
            p=smooth((t-1.5)/.4)
            scale,opacity,dy=1.,1-p,-5*p
        stamp=question.resize((max(1,round(question.width*scale)),max(1,round(question.height*scale))),Image.Resampling.LANCZOS)
        stamp.putalpha(stamp.getchannel('A').point(lambda v:round(v*opacity)))
        frame.paste(stamp,(round(380-stamp.width/2),round(100-stamp.height/2+dy)),stamp)
        return frame

    for i,state in enumerate(STATES):
        (thinking() if i==0 else images[i]).save(OUT/f'{name}-{state}.png')
    pop=[thinking(i/50) for i in range(100)]
    # Every face/body/hand pixel outside the symbol region remains identical.
    body_region=(70,160,410,440)
    assert pop[0].crop(body_region).tobytes()==pop[40].crop(body_region).tobytes()
    assert pop[0].tobytes()!=pop[40].tobytes()
    report={'thinking':save_gif(pop,[20]*100,OUT/f'{name}-thinking.gif')}
    sequence=pop+images[1:]
    report['expressions']=save_gif(sequence,[20]*100+[2000]*3,OUT/f'{name}-expressions.gif')
    (OUT/f'{name}-expressions.json').write_text(json.dumps(report,indent=2),encoding='utf-8')
    print(name,json.dumps(report))

if __name__=='__main__':
    for name in ['penguin','ghost']:build(name)
