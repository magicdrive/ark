import json,sys,collections
def load(p):
    d=collections.defaultdict(set); eof={}
    for l in open(p):
        r=json.loads(l)
        if r['K']=='attr': d[r['F']].add((r['Name'],r['S'],r['E'],r['VS'],r['VE']))
        if r['K']=='eof': eof[r['F']]=r.get('Consumed',0)
    return d,eof
a,ea=load(sys.argv[1]); b,eb=load(sys.argv[2])
import os
files=sorted(set(a)|set(b)|set(ea)|set(eb))
diff=0; only_a=only_b=0; short=[]
for f in files:
    n=os.path.getsize(f)
    if ea.get(f,n)!=n: short.append((sys.argv[1],f,ea.get(f),n))
    if eb.get(f,n)!=n: short.append((sys.argv[2],f,eb.get(f),n))
    x,y=a.get(f,set()),b.get(f,set())
    if x!=y:
        diff+=1; only_a+=len(x-y); only_b+=len(y-x)
        if diff<=(int(sys.argv[3]) if len(sys.argv)>3 else 5): print('DIFF',f.split('/')[-1],'only',sys.argv[1].split('_')[-1],sorted(x-y)[:4],'only',sys.argv[2].split('_')[-1],sorted(y-x)[:4])
print(f'files={len(files)} differing={diff} attrs_only_first={only_a} attrs_only_second={only_b} not_consumed={len(short)}')
for s in short[:5]: print('SHORT',s)
