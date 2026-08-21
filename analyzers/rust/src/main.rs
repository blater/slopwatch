//! Safe Rust source analyzer.  It parses lexical source only: no macro expansion,
//! Cargo build, build script, proc macro, or Clippy execution is ever selected.

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::fs;
use std::io::{self, BufRead};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

const VERSION: u32 = 1;
const ANALYZER: &str = "slopslap-rust-structural";
const ANALYZER_VERSION: &str = "0.1.0";

#[derive(Deserialize)] #[serde(deny_unknown_fields)] struct Component { component_id: String, definition_version: String }
#[derive(Deserialize)] #[serde(deny_unknown_fields)] struct Unit { unit_id: String, language: String, source_paths: Vec<String>, #[serde(default)] metadata: Value }
#[derive(Deserialize)] #[serde(deny_unknown_fields)] struct Request {
    #[serde(rename = "type")] kind: String, protocol_version: u32, invocation_id: String,
    workspace: String, units: Vec<Unit>, components: Vec<Component>, #[serde(default)] options: Value, #[serde(default)] limits: Value,
}
#[derive(Serialize)] struct Out<'a> { #[serde(rename = "type")] kind: &'a str, protocol_version: u32, invocation_id: &'a str, #[serde(flatten)] body: Value }

fn emit(id: &str, kind: &str, body: Value) { println!("{}", serde_json::to_string(&Out { kind, protocol_version: VERSION, invocation_id: id, body }).unwrap()); }
fn known(component: &Component) -> bool { matches!((component.component_id.as_str(), component.definition_version.as_str()),
    ("rust_large_function", "rust-v1") | ("rust_deep_nesting", "rust-v1") | ("rust_unsafe_block", "rust-v1") | ("rust_panic_macro", "rust-v1")) }
fn subject(source: &str, start: usize, end: usize, name: &str) -> Value { let before=&source[..start]; let until_end=&source[..end]; json!({"name":name,"symbol":name,"line":before.bytes().filter(|b|*b==b'\n').count()+1,"column":before.rsplit('\n').next().unwrap_or("").chars().count(),"end_line":until_end.bytes().filter(|b|*b==b'\n').count()+1,"end_column":until_end.rsplit('\n').next().unwrap_or("").chars().count()}) }
fn measure(id: &str, unit: &Unit, component: &Component, path: &str, scope: &str, value: u64, sub: Value) {
    emit(id, "measurement", json!({"unit_id":unit.unit_id,"component_id":component.component_id,"definition_version":component.definition_version,"path":path,"scope":scope,"value":value,"subject":sub,"attributes":{},"provenance":{"analyzer":ANALYZER,"analyzer_version":ANALYZER_VERSION,"rule":component.component_id}}));
}
fn coverage(id:&str, unit:&Unit, component:&Component, path:&str, state:&str, reason:&str) { emit(id,"coverage",json!({"unit_id":unit.unit_id,"component_id":component.component_id,"definition_version":component.definition_version,"path":path,"state":state,"reason":reason})); }
fn diagnostic(id:&str, unit:Option<&Unit>, path:Option<&str>, code:&str, severity:&str, message:String) { emit(id,"diagnostic",json!({"severity":severity,"code":code,"message":message,"unit_id":unit.map(|u|u.unit_id.as_str()),"path":path})); }

#[allow(dead_code)]
#[derive(Clone)] struct Function { name:String, start:usize, end:usize, body_start:usize, max_depth:u64 }
/// A deliberately conservative lexer: strings/comments are blanked while retaining offsets.
fn scrub(text: &str) -> Vec<u8> {
    let mut out=text.as_bytes().to_vec(); let mut i=0;
    while i<out.len() { if out[i..].starts_with(b"//") { while i<out.len() && out[i]!=b'\n' {out[i]=b' ';i+=1;} }
      else if out[i..].starts_with(b"/*") { out[i]=b' ';out[i+1]=b' ';i+=2; while i+1<out.len()&&!out[i..].starts_with(b"*/") { if out[i]!=b'\n'{out[i]=b' ';}i+=1;} if i+1<out.len(){out[i]=b' ';out[i+1]=b' ';i+=2;} }
      else if out[i]==b'"' {out[i]=b' ';i+=1; while i<out.len(){let c=out[i];if c!=b'\n'{out[i]=b' ';}i+=1;if c==b'\\'{if i<out.len(){out[i]=b' ';i+=1;}}else if c==b'"'{break;}}} else{i+=1;} }
    out
}
fn functions(text:&str)->Vec<Function>{ let bytes=scrub(text); let mut out=Vec::new(); let mut i=0;
 while i+2<bytes.len(){ if &bytes[i..i+2]==b"fn" && (i==0||!bytes[i-1].is_ascii_alphanumeric()&&bytes[i-1]!=b'_') && (i+2==bytes.len()||!bytes[i+2].is_ascii_alphanumeric()&&bytes[i+2]!=b'_') {let start=i; i+=2;while i<bytes.len()&&bytes[i].is_ascii_whitespace(){i+=1;}let ns=i;while i<bytes.len()&&(bytes[i].is_ascii_alphanumeric()||bytes[i]==b'_'){i+=1;}if ns==i{continue}let name=String::from_utf8_lossy(&bytes[ns..i]).to_string(); while i<bytes.len()&&bytes[i]!=b'{'&&bytes[i]!=b';'{i+=1;}if i>=bytes.len()||bytes[i]==b';'{continue}let body=i;let mut d=0i64;let mut max=0;while i<bytes.len(){if bytes[i]==b'{'{d+=1;max=max.max(d)}if bytes[i]==b'}'{d-=1;if d==0{break}}i+=1;}if d==0{out.push(Function{name,start,end:i+1,body_start:body,max_depth:(max-1)as u64});}}else{i+=1;} } out }
fn occurrences(text:&str, token:&str)->Vec<usize>{let bytes=scrub(text); let t=token.as_bytes();(0..bytes.len().saturating_sub(t.len())+1).filter(|&i|&bytes[i..i+t.len()]==t && (i==0||!bytes[i-1].is_ascii_alphanumeric()) && (i+t.len()==bytes.len()||!bytes[i+t.len()].is_ascii_alphanumeric())).collect()}
fn analyze(id:&str, unit:&Unit, path:&str, source:&str, components:&[Component]) { let funcs=functions(source);
 for c in components { if !known(c){coverage(id,unit,c,path,"unavailable","component is not provided by safe Rust syntax analysis");continue} match c.component_id.as_str(){
 "rust_large_function"=>for f in &funcs {let lines=source[f.start..f.end].bytes().filter(|b|*b==b'\n').count() as u64+1;measure(id,unit,c,path,"function",lines,subject(source,f.start,f.end,&f.name));},
 "rust_deep_nesting"=>for f in &funcs {measure(id,unit,c,path,"function",f.max_depth,subject(source,f.start,f.end,&f.name));},
 "rust_unsafe_block"=>for p in occurrences(source,"unsafe") {measure(id,unit,c,path,"expression",1,subject(source,p,p+6,"unsafe"));},
 "rust_panic_macro"=>for token in ["panic","todo","unimplemented"] {for p in occurrences(source,token){measure(id,unit,c,path,"expression",1,subject(source,p,p+token.len(),token));}}, _=>{} } coverage(id,unit,c,path,"complete","safe syntax analysis completed"); }
}
fn canonical_source(workspace:&Path, requested:&str)->Result<(PathBuf,String),String>{if requested.contains('\\')||requested.split('/').any(|p|p==".."||p.is_empty()){return Err("non-canonical source path".into())}let candidate=workspace.join(requested);let meta=fs::symlink_metadata(&candidate).map_err(|e|e.to_string())?;if meta.file_type().is_symlink(){return Err("symlinked source is not allowed".into())}let absolute=candidate.canonicalize().map_err(|e|e.to_string())?;let rel=absolute.strip_prefix(workspace).map_err(|_|"source outside workspace")?.to_string_lossy().replace('\\',"/");if !rel.ends_with(".rs"){return Err("unsupported Rust extension".into())}Ok((absolute,rel))}
fn safe_metadata(workspace:&Path, options:&Value)->Result<(),String>{let cargo=options.get("cargo_path").and_then(Value::as_str).ok_or("bundled cargo_path was not configured")?;let status=Command::new(cargo).args(["metadata","--offline","--no-deps","--format-version","1"]).current_dir(workspace).stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null()).status().map_err(|e|e.to_string())?;if status.success(){Ok(())}else{Err(format!("cargo metadata exited {status}"))}}
fn main(){let line=io::stdin().lock().lines().next();let request:Request=match line.and_then(Result::ok).and_then(|x|serde_json::from_str(&x).ok()){Some(v)=>v,None=>return};let _limits=&request.limits;if request.kind!="request"||request.protocol_version!=VERSION{emit(&request.invocation_id,"terminal",json!({"status":"failure","message":"unsupported request protocol","analyzed_unit_ids":[],"failed_unit_ids":[],"skipped_unit_ids":[]}));return}let workspace=match Path::new(&request.workspace).canonicalize(){Ok(x)=>x,Err(e)=>{emit(&request.invocation_id,"terminal",json!({"status":"failure","message":e.to_string(),"analyzed_unit_ids":[],"failed_unit_ids":[],"skipped_unit_ids":[]}));return}};
 if request.options.get("cargo_metadata").and_then(Value::as_bool)==Some(true){if let Err(e)=safe_metadata(&workspace,&request.options){diagnostic(&request.invocation_id,None,None,"RUST_CARGO_METADATA", "warning",e);}}
 let mut analyzed=Vec::new();let mut failed=Vec::new();for unit in &request.units {let _metadata=&unit.metadata;if unit.language!="rust"{diagnostic(&request.invocation_id,Some(unit),None,"RUST_LANGUAGE","error","unit language is not rust".into());failed.push(unit.unit_id.clone());continue}let mut good=true;for raw in &unit.source_paths {match canonical_source(&workspace,raw){Ok((file,path))=>match fs::read_to_string(file){Ok(source)=>analyze(&request.invocation_id,unit,&path,&source,&request.components),Err(e)=>{diagnostic(&request.invocation_id,Some(unit),Some(&path),"RUST_READ","error",e.to_string());good=false}},Err(e)=>{diagnostic(&request.invocation_id,Some(unit),Some(raw),"RUST_SOURCE","error",e);good=false}}}emit(&request.invocation_id,"execution_plan",json!({"unit_id":unit.unit_id,"parser_modes":["rust-syntax-no-macro-expansion"],"kernels":["source_inventory","lexical_parser","function_facts","rust_design"],"discovered_source_count":unit.source_paths.len(),"parsed_source_count":unit.source_paths.len()}));if good{analyzed.push(unit.unit_id.clone())}else{failed.push(unit.unit_id.clone())}}
 let status=if failed.is_empty(){"success"}else{"failure"};emit(&request.invocation_id,"terminal",json!({"status":status,"message":"safe Rust analysis: macro-generated control flow is not measured","analyzed_unit_ids":analyzed,"failed_unit_ids":failed,"skipped_unit_ids":[]}));}

#[cfg(test)] mod tests { use super::*;
 #[test] fn lexical_kernels_ignore_comments_and_strings(){let s="// fn no() {}\nfn a(){ unsafe { panic!(\"x\") } }\n";let f=functions(s);assert_eq!(f.len(),1);assert_eq!(occurrences(s,"unsafe").len(),1);assert_eq!(occurrences(s,"panic").len(),1);}
 #[test] fn measures_nesting(){let f=functions("fn a(){ if x { while y { z(); } } }");assert_eq!(f[0].max_depth,2);}
 #[test] fn fixture_exposes_only_real_syntax(){let s=include_str!("../fixtures/generic_types.rs");assert_eq!(functions(s).len(),1);assert_eq!(occurrences(s,"unsafe").len(),1);}
}
