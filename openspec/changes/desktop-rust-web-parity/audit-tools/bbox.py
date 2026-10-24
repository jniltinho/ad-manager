import sys
from PIL import Image
import os
D=os.path.join(os.path.dirname(os.path.abspath(__file__)), '../../../../docs/screenshots/')
def bbox(f,x0,y0,x1,y1,thr=110,S=2,light=False):
    im=Image.open(D+f).convert('L'); px=im.load()
    xs=[];ys=[]
    for y in range(int(y0*S),int(y1*S)):
        for x in range(int(x0*S),int(x1*S)):
            v=px[x,y]
            if (v>thr if light else v<thr): xs.append(x);ys.append(y)
    if not xs: return None
    return (min(xs)/S,min(ys)/S,(max(xs)+1)/S,(max(ys)+1)/S, (max(xs)+1-min(xs))/S,(max(ys)+1-min(ys))/S)
for a in sys.argv[1:]:
    p=a.split(':'); f=p[0]; c=[float(v) for v in p[1:5]]; thr=int(p[5]) if len(p)>5 else 110; light=len(p)>6
    print(a, bbox(f,*c,thr=thr,light=light))
