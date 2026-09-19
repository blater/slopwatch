#!/usr/bin/env python3
"""Packaged-CLI paired checks for audience and supporting-type attribution."""
import json,pathlib,subprocess,tempfile
root=pathlib.Path(__file__).resolve().parents[1]
import argparse
parser=argparse.ArgumentParser(description="Check file-audience invariants with the rebuilt packaged CLI")
parser.add_argument("--binary", type=pathlib.Path, default=root/"build/slopmark")
parser.add_argument("--output", type=pathlib.Path, default=root/"build/audience-acceptance-r28.json")
args=parser.parse_args()
cli=args.binary.resolve()
cases={
'go':('service.go', {'go.mod':'module sample\n\ngo 1.24\n'},'package sample\nfunc Run(x int) int { return x*2 }\n','type data struct { value int }; func (d data) Value() int { return d.value }','package sample\n','package sample\nfunc Run(x int) int { return twice(x) }; func twice(x int) int { return x*2 }','func Identity(x int) int { return x }'),
'java':('Service.java',{},'public class Service { private int state; public int run(int x){ state++; return x*2; } }','class Data { private int value; Data(int value){this.value=value;} int value(){return value;} }','','public class Service { private int state; public int run(int x){state++; return twice(x);} private int twice(int x){return x*2;} }','class Independent { int identity(int x){return x;} }'),
 'typescript':('service.ts',{'tsconfig.json':'{"compilerOptions":{"strict":true},"include":["*.ts"]}'},'export function run(x:number){return x*2;}','class Data { value:number; constructor(value:number){this.value=value;} getValue(){return this.value;} }','','export function run(x:number){return twice(x);} function twice(x:number){return x*2;}','export function identity(x:number){return x;}'),
'rust':('service.rs',{},'pub fn run(x:i32)->i32{x*2}','struct Data { value:i32 } impl Data { fn value(&self)->i32{self.value} }','','pub fn run(x:i32)->i32{twice(x)} fn twice(x:i32)->i32{x*2}','pub fn identity(x:i32)->i32{x}')}
results={}
for lang,(filename,config,direct,support,prefix,extracted,shallow) in cases.items():
 ext=pathlib.Path(filename).suffix
 variants={'direct':{filename:direct},'support_added':{filename:direct+'\n'+support},'support_moved':{filename:direct,'data'+ext:prefix+support},'helper_extracted':{filename:extracted},'unrelated_sibling':{filename:direct,'other'+ext:prefix+shallow},'independent_shallow':{filename:direct+'\n'+shallow}}
 output={}
 for name,files in variants.items():
  with tempfile.TemporaryDirectory(prefix='sw-audience-') as temp:
   p=pathlib.Path(temp)
   for path,source in (config|files).items():(p/path).write_text(source)
   doc=json.loads(subprocess.check_output([str(cli),'-format','json','.'],cwd=p))
   f=next(f for f in doc['files'] if f['path']==filename);c=f['components']['module_shallowness']
   output[name]={'shallow':c.get('raw_max'),'contribution':c.get('contribution'),'estimated':c.get('depth_estimated'), 'boundaries':[{'id':i,'raw':doc['depth'][i].get('raw')} for i in c.get('depth_boundary_ids',[])]}
 results[lang]=output
args.output.write_text(json.dumps(results,indent=2)+'\n')
for lang,variants in results.items():
 print(lang,{name:r['shallow'] for name,r in variants.items()})
 baseline=variants['direct']['shallow']
 for name in ['support_added','support_moved','helper_extracted','unrelated_sibling']:assert variants[name]['shallow']==baseline,(lang,name,variants)
 assert variants['independent_shallow']['shallow']==0,(lang,variants)
