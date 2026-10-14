import json,sys,os,collections
s=sys.argv[1]
def tags(p):
    c=collections.Counter();eof={}
    for l in open(p):
        r=json.loads(l)
        if r['K']=='tag': c[r['F']]+=1
        if r['K']=='eof': eof[r['F']]=r
    return c,eof
x,_=tags(f'out/{s}.xnet.jsonl'); t,eof=tags(f'out/{s}.ts.jsonl')
short=[f for f,r in eof.items() if r.get('Consumed',0)!=os.path.getsize(f)]
err=[f for f,r in eof.items() if r.get('HasError')]
fewer=[f for f in eof if t[f]<x[f]]
print(f'{s}: files={len(eof)} has_error={len(err)} short={len(short)} short_without_error={len([f for f in short if f not in err])} fewer_tags={len(fewer)} fewer_tags_without_error={len([f for f in fewer if f not in err])} tags xnet={sum(x.values())} ts={sum(t.values())}')
for f in [f for f in fewer if f not in err][:8]: print('  SILENT-FEWER',f,t[f],x[f])
