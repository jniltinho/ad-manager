import sys
from PIL import Image
import os
D=os.path.join(os.path.dirname(os.path.abspath(__file__)), '../../../../docs/screenshots/')
_c={}
def img(f):
    if f not in _c: _c[f]=Image.open(D+f).convert('RGB')
    return _c[f]
def runs(f,axis,pos,a=0,b=None,tol=24,minlen=2,S=2):
    """axis 'h': scan row y=pos (logical) from x=a..b; 'v': column x=pos. prints runs in logical px"""
    im=img(f); W,H=im.size; px=im.load()
    p=int(pos*S)
    n=W if axis=='h' else H
    b=n//S if b is None else b
    out=[];cur=None;start=None
    for i in range(int(a*S),int(b*S)):
        c=px[i,p] if axis=='h' else px[p,i]
        if cur is None or max(abs(c[k]-cur[k]) for k in range(3))>tol:
            if cur is not None: out.append((start,i,cur))
            cur=c;start=i
    out.append((start,int(b*S),cur))
    s=[]
    for st,en,c in out:
        if en-st>=minlen: s.append(f"{st/S:.1f}-{en/S:.1f}({(en-st)/S:.1f}) #{c[0]:02x}{c[1]:02x}{c[2]:02x}")
    print(f"{f} {axis}@{pos}: "+" | ".join(s))
if __name__=='__main__':
    f,axis,pos=sys.argv[1:4]; rest=[float(x) for x in sys.argv[4:]]
    a=rest[0] if len(rest)>0 else 0; b=rest[1] if len(rest)>1 else None
    tol=int(rest[2]) if len(rest)>2 else 24; ml=int(rest[3]) if len(rest)>3 else 2
    runs(f,axis,float(pos),a,b,tol,ml)
