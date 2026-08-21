"""Loopback-only configuration control screen with validated atomic saves."""

from __future__ import annotations

import json
import os
import secrets
import sys
import threading
import webbrowser
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

from slopslap_core import catalog_document

from .config import (
    config_locations, discover_config, discover_global_config, dump_policy,
    initial_policy, load_policy,
)
from .schema import effective_profile_document


_HTML = '''<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>Slopslap configuration</title>
<style>
:root{color-scheme:light dark;font:15px/1.45 system-ui,sans-serif}body{max-width:1100px;margin:2rem auto;padding:0 1rem}
h1{margin-bottom:.2rem}.muted{opacity:.7}fieldset{border:1px solid #8886;border-radius:8px;margin:1rem 0;padding:1rem}
legend{font-weight:700}table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:.45rem;border-bottom:1px solid #8884}
input[type=number]{width:7rem}button{padding:.55rem .9rem;margin-right:.5rem}.status{min-height:1.5rem;font-weight:600}
code{font-size:.85em}.experimental{opacity:.7}
</style>
<body><h1>Slopslap configuration</h1><p class="muted">Per-language scoring mix. Changes are validated by the same policy engine used for analysis.</p>
<div id="controls"></div><p><button id="save">Validate and save</button><button id="reload">Reset</button></p><p class="status" id="status"></p>
<script>
const csrf=__CSRF__; let state;
const esc=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
async function load(){const r=await fetch('/api/state');state=await r.json();render()}
function render(){const root=document.querySelector('#controls');root.innerHTML='';
 for(const [language,profile] of Object.entries(state.effective.languages)){
  const field=document.createElement('fieldset'); field.innerHTML=`<legend>${esc(language)}</legend><table><thead><tr><th>Component</th><th>Enabled</th><th>Severity</th><th>Weight</th><th>Threshold</th><th>Formula</th></tr></thead><tbody></tbody></table>`;
  const body=field.querySelector('tbody'); for(const [id,c] of Object.entries(profile.components)){
   const meta=state.schema.components.find(x=>x.component_id===id); const row=document.createElement('tr'); if(meta.evidence_posture==='experimental')row.className='experimental';
   const severity=['error','warning','info'].map(x=>`<option ${x===c.severity?'selected':''}>${x}</option>`).join('');const formulas=meta.allowed_formulas.map(x=>`<option ${x===c.formula?'selected':''}>${esc(x)}</option>`).join('');
   row.innerHTML=`<td><code>${esc(id)}</code><br><small>${esc(meta.evidence_posture)} · ${esc(meta.support[language])}</small></td><td><input data-lang="${esc(language)}" data-id="${esc(id)}" data-key="enabled" type="checkbox" ${c.enabled?'checked':''}></td><td><select data-lang="${esc(language)}" data-id="${esc(id)}" data-key="severity">${severity}</select></td><td><input data-lang="${esc(language)}" data-id="${esc(id)}" data-key="weight" type="number" min="0" step="0.1" value="${esc(c.weight)}"></td><td>${c.threshold===null?'—':`<input data-lang="${esc(language)}" data-id="${esc(id)}" data-key="threshold" type="number" min="1" step="1" value="${esc(c.threshold)}">`}</td><td><select data-lang="${esc(language)}" data-id="${esc(id)}" data-key="formula">${formulas}</select></td>`;body.append(row)} root.append(field)}}
function overrides(){const result={};document.querySelectorAll('[data-lang]').forEach(input=>{const l=input.dataset.lang,id=input.dataset.id,k=input.dataset.key;result[l]??={};result[l][id]??={};result[l][id][k]=input.type==='checkbox'?input.checked:input.type==='number'?Number(input.value):input.value});return result}
document.querySelector('#save').onclick=async()=>{const status=document.querySelector('#status');status.textContent='Saving…';const r=await fetch('/api/policy',{method:'POST',headers:{'Content-Type':'application/json','X-Slopslap-CSRF':csrf},body:JSON.stringify({overrides:overrides()})});const body=await r.json();status.textContent=r.ok?`Saved ${body.path}`:`Error: ${body.error}`;if(r.ok)await load()};
document.querySelector('#reload').onclick=load;load().catch(e=>document.querySelector('#status').textContent=e);
</script></body></html>'''


def _configuration_path(configuration: str | None) -> Path:
  current = Path.cwd()
  emit_warning = lambda message: print(f"warning: {message}", file=sys.stderr)
  if configuration == "global":
    config_path = discover_global_config(current, warn=emit_warning)
    if config_path is None:
      config_path = config_locations(current).xdg
  elif configuration is not None:
    supplied = Path(configuration).expanduser()
    config_path = (current / supplied).resolve() if not supplied.is_absolute() else supplied.resolve()
  else:
    config_path = discover_config(current, warn=emit_warning)
    if config_path is None:
      config_path = config_locations(current).local
  if config_path.exists() and not config_path.is_file():
    raise ValueError(f"configuration path is not a file: {config_path}")
  if config_path.is_file():
    return config_path
  config_path.parent.mkdir(parents=True, exist_ok=True)
  config_path.write_text(initial_policy(), encoding="utf-8")
  print(f"Created {config_path}")
  return config_path


def _merge(policy: dict[str, Any], overrides: dict[str, Any]) -> dict[str, Any]:
  result = dict(policy)
  result.setdefault("schema", 1)
  result.setdefault("extends", "slopslap-balanced-v1")
  languages = dict(result.get("languages", {}))
  for language, components in overrides.items():
    language_table = dict(languages.get(language, {}))
    existing = dict(language_table.get("components", {}))
    for component_id, values in components.items():
      existing[component_id] = dict(values)
    language_table["components"] = existing
    languages[language] = language_table
  result["languages"] = languages
  return result


def serve(configuration: str | None, port: int, *, open_browser: bool = True) -> int:
  if not 0 <= port <= 65535:
    raise ValueError("port must be between 0 and 65535")
  config_path = _configuration_path(configuration)
  token = secrets.token_urlsafe(32)

  class Handler(BaseHTTPRequestHandler):
    def _reply(self, status: HTTPStatus, payload: bytes, content_type: str) -> None:
      self.send_response(status)
      self.send_header("Content-Type", content_type)
      self.send_header("Content-Length", str(len(payload)))
      self.send_header("Cache-Control", "no-store")
      self.send_header("X-Content-Type-Options", "nosniff")
      self.send_header("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'")
      self.end_headers()
      self.wfile.write(payload)

    def _json(self, status: HTTPStatus, document: Any) -> None:
      self._reply(status, json.dumps(document).encode(), "application/json; charset=utf-8")

    def do_GET(self) -> None:  # noqa: N802
      if self.path == "/":
        page = _HTML.replace("__CSRF__", json.dumps(token)).encode()
        self._reply(HTTPStatus.OK, page, "text/html; charset=utf-8")
      elif self.path == "/api/state":
        policy = dict(load_policy(config_path if config_path.is_file() else None))
        self._json(HTTPStatus.OK, {"path": str(config_path), "policy": policy,
                                  "schema": catalog_document(),
                                  "effective": effective_profile_document(policy, ["java", "typescript", "rust"])})
      else:
        self._json(HTTPStatus.NOT_FOUND, {"error": "not found"})

    def do_POST(self) -> None:  # noqa: N802
      host = self.headers.get("Host", "")
      origin = self.headers.get("Origin", "")
      if (self.path != "/api/policy"
          or self.headers.get("X-Slopslap-CSRF") != token
          or not host.startswith("127.0.0.1:")
          or origin != f"http://{host}"):
        self._json(HTTPStatus.FORBIDDEN, {"error": "invalid request token"})
        return
      try:
        length = int(self.headers.get("Content-Length", "0"))
        if length <= 0 or length > 1_000_000:
          raise ValueError("invalid request size")
        body = json.loads(self.rfile.read(length))
        if set(body) != {"overrides"} or not isinstance(body["overrides"], dict):
          raise ValueError("request must contain only an overrides object")
        current = dict(load_policy(config_path if config_path.is_file() else None))
        updated = _merge(current, body["overrides"])
        effective_profile_document(updated, ["java", "typescript", "rust"])
        encoded = dump_policy(updated)
        temporary = config_path.with_suffix(config_path.suffix + ".tmp")
        temporary.write_text(encoded, encoding="utf-8")
        os.replace(temporary, config_path)
        self._json(HTTPStatus.OK, {"path": str(config_path)})
      except Exception as error:
        self._json(HTTPStatus.BAD_REQUEST, {"error": str(error)})

    def log_message(self, format: str, *args: object) -> None:
      return

  server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
  url = f"http://127.0.0.1:{server.server_port}/"
  print(f"Slopslap configuration UI: {url}")
  print("Press Ctrl-C to stop.")
  if open_browser:
    threading.Timer(0.1, lambda: webbrowser.open(url)).start()
  try:
    server.serve_forever()
  except KeyboardInterrupt:
    pass
  finally:
    server.server_close()
  return 0
