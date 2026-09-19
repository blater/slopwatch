#!/usr/bin/env python3
"""Check data-only value roles and behavioral negatives through the packaged CLI."""
import json,subprocess,tempfile
from pathlib import Path
root=Path(__file__).resolve().parents[1]
cases=[
 ('java','Point.java','public record Point(int x, int y) {}',True),
 ('java','Point.java','public record Point<T>(T value) implements java.io.Serializable {}',True),
 ('java','Point.java','public class Point<T> { public final T value; public Point(T value){this.value=value;} }',True),
 ('java','Choice.java','public enum Choice { A, B; private static final int COUNT=values().length; public static int count(){return COUNT;} }',True),
 ('java','Point.java','public record Point(int x) { public Point { if(x<0) throw new IllegalArgumentException(); } }',False),
 ('java','Choice.java','public enum Choice { A(1), B(2); private final int code; Choice(int code){this.code=code;} public int code(){return code;} }',True),
 ('java','Choice.java','public enum Choice { A; public int twice(int x){return x*2;} }',False),
 ('go','value.go','package value\ntype Point struct { X int; Y int }',True),
 ('go','value.go','package value\ntype Status int\nconst (Empty Status = iota; Ready)',True),
 ('go','value.go','package value\ntype Status int\nconst (Empty Status = iota; Ready)\nfunc Twice(x int) int {return x*2}',False),
 ('typescript','value.ts','export class Point { readonly x:number; constructor(x:number){this.x=x;} }',True),
 ('typescript','value.ts','export enum Status { Empty, Ready }',True),
 ('typescript','value.ts','export class Value<T> {constructor(public readonly value:T){}}',True),
 ('typescript','value.ts','export class Value<T> {constructor(public readonly value:T=audit()){}} function audit():any {return 1;}',False),
 ('typescript','value.ts','export class Point { private x:number=0; value():number{return this.x;} constructor(seed:number=audit()) {} } function audit(){console.log(1); return 1;}',False),
 ('typescript','value.ts','export enum Status { Empty, Ready } export function twice(x:number){return x*2;}',False),
 ('rust','lib.rs','pub struct Point {pub x:i32,pub y:i32}',True),
 ('rust','lib.rs','pub enum Status { Empty, Value(i32), Point{x:i32,y:i32} }',True),
 ('rust','lib.rs','pub enum Status { Empty } impl Status {pub fn twice(x:i32)->i32{x*2}}',False),
]
rows=[]
for language,name,source,passive in cases:
 with tempfile.TemporaryDirectory(prefix='slopwatch-value-') as temp:
  path=Path(temp);(path/name).write_text(source)
  if language=='go':(path/'go.mod').write_text('module example.org/value\n\ngo 1.22\n')
  report=json.loads(subprocess.check_output([str(root/'build/slopmark'),'-format','json','.'],cwd=path))
  f=next(f for f in report['files'] if f['path']==name);c=f['components']['module_shallowness']
  proofs=[e for key in c.get('depth_boundary_ids',[]) for e in report['depth'][key].get('evidence',[]) if e.get('kind') in ['passive-result-carrier-v1','passive-value-object-v1','passive-enum-v1'] and e.get('status')=='proven']
  passed=bool(proofs)==passive and isinstance(c.get('raw_max'),(int,float)) and ((c['raw_max']==0 and c['contribution']==0) if passive else c['raw_max']>0)
  rows.append(dict(language=language,source=source,expected_passive=passive,shallow=c.get('raw_max'),contribution=c['contribution'],proofs=proofs,passed=passed))
(root/'build/shallow-value-acceptance.json').write_text(json.dumps(rows,indent=2)+'\n')
print(json.dumps([{k:v for k,v in x.items() if k not in ['source','proofs']} for x in rows],indent=2))
assert all(x['passed'] for x in rows)
