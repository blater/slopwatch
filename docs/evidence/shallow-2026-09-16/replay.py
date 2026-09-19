#!/usr/bin/env python3
"""Replay archived probes in an isolated directory; never overwrite the baseline."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--binary', default='slopmark')
parser.add_argument('--output', required=True, type=Path)
args = parser.parse_args()
archive = Path(__file__).resolve().parent
if args.output.resolve() == (archive / 'baseline.json').resolve():
    parser.error('choose an output other than the archived baseline')
binary = shutil.which(args.binary)
if binary is None:
    parser.error('analyzer binary not found')
binary = str(Path(binary).resolve())
manifest = json.loads((archive / 'manifest.json').read_text())
for name, expected in manifest['archive_sha256'].items():
    actual = hashlib.sha256((archive / name).read_bytes()).hexdigest()
    if actual != expected:
        parser.error(f'archive checksum mismatch: {name}')
sources = json.loads((archive / 'sources.json').read_text())
with tempfile.TemporaryDirectory(prefix='shallow-probes-') as directory:
    root = Path(directory)
    for relative, source in sources.items():
        target = root / relative
        if not target.resolve().is_relative_to(root.resolve()):
            parser.error(f'invalid source path: {relative}')
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(source)
    result = subprocess.run([binary, '-format', 'json', '-limit', '0', '.'],
                            cwd=root, capture_output=True, text=True, check=True)
    report = json.loads(result.stdout)
    with args.output.open('x') as output:
        output.write(result.stdout)
    print(f'Replayed {len(sources)} sources; wrote {args.output}')
    for item in report['files']:
        for evidence in item['components'].get('module_shallowness', {}).get('evidence', []):
            attrs = evidence.get('attributes', {})
            print(item['path'], evidence['value'], attrs.get('available'), attrs.get('reference_role'))
