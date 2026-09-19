#!/usr/bin/env python3
"""Require a reviewed evidence report before changing calibrated numeric weights.

The reviewer field is a review attestation, not authentication. Repository review
must verify it; this check rejects missing/stale evidence, not forged approvals.
"""
from __future__ import annotations
import argparse
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BASELINE = ROOT / 'docs/evidence/shallow-v4/calibration-review-baseline.json'
PROFILE = ROOT / 'go/internal/sourceestimate/calibration_default.json'


def identity(profile: dict) -> str:
    return hashlib.sha256(json.dumps(profile, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def validate_change(baseline: dict, proposed: dict, report: dict | None, root: Path) -> list[str]:
    if {k:v for k,v in baseline.items() if k != 'name'} == {k:v for k,v in proposed.items() if k != 'name'}:
        return []
    if not report:
        return ['numeric weights changed without a reviewed change report']
    errors = []
    if report.get('status') != 'reviewed' or not isinstance(report.get('reviewer'), str) or not report['reviewer'].strip():
        errors.append('change report needs an explicit reviewer attestation')
    for key, value in [('baseline_profile_sha256', identity(baseline)), ('proposed_profile_sha256', identity(proposed))]:
        if report.get(key) != value:
            errors.append(f'{key} is missing or stale')
    for key in ['score_deltas', 'important_ranking_changes', 'mechanism_coverage', 'fresh_independent_examples']:
        item = report.get(key)
        if not isinstance(item, dict) or not str(item.get('assessment', '')).strip():
            errors.append(f'{key} needs a reviewed assessment and hashed artifact')
            continue
        path = (root / str(item.get('artifact', ''))).resolve()
        if not path.is_relative_to(root.resolve()) or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != item.get('sha256'):
            errors.append(f'{key} artifact is missing, changed or outside repository')
    fresh = report.get('fresh_independent_examples', {})
    if not isinstance(fresh, dict) or fresh.get('selected_before_evaluation') is not True or not fresh.get('independent_reviewer') or fresh.get('used_for_tuning') is not False:
        errors.append('fresh examples need independent pre-evaluation selection and no tuning attestation')
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', type=Path, default=BASELINE)
    parser.add_argument('--profile', type=Path, default=PROFILE)
    parser.add_argument('--report', type=Path, default=ROOT / 'docs/evidence/shallow-v4/calibration-change-review.json')
    args = parser.parse_args()
    try:
        report = json.loads(args.report.read_text()) if args.report.exists() else None
        errors = validate_change(json.loads(args.baseline.read_text()), json.loads(args.profile.read_text()), report, ROOT)
    except (ValueError, OSError, TypeError) as exc:
        errors = [str(exc)]
    for error in errors:
        print('FAIL:', error)
    if not errors:
        print('PASS: numeric weights unchanged or reviewed change evidence complete')
    return int(bool(errors))


if __name__ == '__main__':
    raise SystemExit(main())
