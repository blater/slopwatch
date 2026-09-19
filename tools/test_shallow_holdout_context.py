import copy
import contextlib
import hashlib
import io
import json
import tempfile
from unittest.mock import patch
from pathlib import Path
import unittest
from shallow_holdout_context import context_case, summarize, main
from shallow_graded_acceptance import BenchmarkError, evaluate_report


class ContextEvaluationTests(unittest.TestCase):
    def test_context_retains_targets_labels_and_only_matching_witnesses(self):
        case={'id':'boundary','target':'Example.java','language':'java','group':'high',
              'expected_range':[65,100], 'files':[{'origin':'/repo/src/Example.java','snapshot':'snapshots/Example.java','sha256':'source'}]}
        spec={'witness_snapshots':[{'origin':'/repo/src/Caller.java','snapshot':'witnesses/Caller.java','sha256':'witness'},
                                   {'origin':'/other/Elsewhere.java','snapshot':'witnesses/Elsewhere.java','sha256':'other'}]}
        original=copy.deepcopy(case)
        result=context_case(case,spec,Path('/frozen/manifest.json'),Path('/repo'))
        self.assertEqual(case,original)
        self.assertEqual(result['expected_range'],[65,100])
        self.assertEqual(result['target'],'src/Example.java')
        self.assertEqual([f['path'] for f in result['files']],['src/Example.java','src/Caller.java'])
        self.assertEqual([f['sha256'] for f in result['files']],['source','witness'])

    def test_shared_target_witness_deduplicates_only_identical_bytes(self):
        entry = {"origin": "/repo/A.java", "snapshot": "target/A.java", "sha256": "same"}
        case = {"id": "sample", "files": [entry]}
        spec = {"witness_snapshots": [{**entry, "snapshot": "witness/A.java"}]}
        self.assertEqual(len(context_case(case, spec, Path("/frozen/m.json"), Path("/repo"))["files"]), 1)
        spec["witness_snapshots"][0]["sha256"] = "different"
        with self.assertRaisesRegex(BenchmarkError, "conflicting frozen bytes"):
            context_case(case, spec, Path("/frozen/m.json"), Path("/repo"))

    def test_declared_high_language_cannot_pass_as_not_applicable(self):
        rows = [{"id": "high", "language": "rust", "group": "high", "raw_max": None,
                 "numeric": False, "not_applicable": True, "passed": True, "failures": []}]
        result = summarize(rows, {})
        self.assertFalse(result["passed"])
        self.assertEqual(result["positive_detection"]["by_language"]["rust"]["detected_high"], 0)
        self.assertEqual(result["failures"][0]["kind"], "positive_detection")

    def test_workspace_target_absence_and_rank_failures_stay_failures(self):
        case={'id':'high','target':'Example.java','language':'java','group':'high','expected_range':[65,100]}
        missing=evaluate_report(case,{'files':[]})
        self.assertFalse(missing['passed'])
        self.assertTrue(any('absent' in f for f in missing['failures']))
        rows=[{**case,'raw_max':20,'numeric':True,'passed':False,'failures':['outside band']},
              {'id':'low','language':'java','group':'low','raw_max':70,'numeric':True,'failures':['outside band']}]
        result=summarize(rows,{'relations':[{'higher':'high','lower':'low','minimum_delta':25}]})
        self.assertFalse(result['passed'])
        self.assertEqual(result['positive_detection']['detected_high'],0)
        self.assertTrue(any(f.get('kind')=='comparison' for f in result['failures']))

    def test_frozen_command_adds_witnesses_and_preserves_both_observations(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);frozen=root/'frozen';frozen.mkdir()
            target=frozen/'Example.java';target.write_text('class Example {}')
            witness=frozen/'Caller.java';witness.write_text('class Caller {}')
            digest=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
            manifest=frozen/'manifest.json'
            spec={'purpose':'holdout','status':'independently-reviewed-frozen',
                  'selection_blinded_to_scores':True,'frozen_before_first_evaluation':True,
                  'cases':[{'id':'sample','language':'java','target':'Example.java','group':'high','expected_range':[65,100],
                            'files':[{'path':'Example.java','origin':'/absent/repo/src/Example.java','snapshot':'Example.java','sha256':digest(target)}]}],
                  'witness_snapshots':[{'origin':'/absent/repo/src/Caller.java','snapshot':'Caller.java','sha256':digest(witness)}]}
            manifest.write_text(json.dumps(spec));manifest.with_suffix('.sha256').write_text(digest(manifest))
            context=root/'context.json';context.write_text(json.dumps({'repositories':{'repo':'/absent/repo'},'case_repositories':{'sample':'repo'}}))
            binary=root/'cli'
            binary.write_text('#!/usr/bin/env python3\nimport json,pathlib\np=next(pathlib.Path(".").rglob("Example.java"))\nv=30 if list(pathlib.Path(".").rglob("Caller.java")) else 80\nprint(json.dumps({"files":[{"path":p.as_posix(),"language":"java","rank":1,"score":1,"observed_score":1,"components":{"module_shallowness":{"raw_max":v,"contribution":1,"depth_state":"measured"}}}]}))\n')
            binary.chmod(0o755);output=root/'result.json'
            originals={p:p.read_bytes() for p in (target,witness,manifest)}
            argv=['context','--manifest',str(manifest),'--context',str(context),'--binary',str(binary),'--output',str(output),'--frozen-only']
            with patch('sys.argv',argv),contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(main(),1)
            result=json.loads(output.read_text())
            self.assertEqual(set(result['modes']),{'isolated','witness_context'})
            self.assertEqual(result['modes']['isolated']['results'][0]['raw_max'],80)
            self.assertEqual(result['modes']['witness_context']['results'][0]['raw_max'],30)
            self.assertTrue(result['modes']['witness_context']['failures'])
            self.assertEqual({p:p.read_bytes() for p in originals},originals)
            for protected in [target,witness,manifest,context,binary]:
                blocked=list(argv);blocked[blocked.index('--output')+1]=str(protected)
                with patch('sys.argv',blocked),contextlib.redirect_stderr(io.StringIO()),self.assertRaises(SystemExit):
                    main()

if __name__=='__main__': unittest.main()
