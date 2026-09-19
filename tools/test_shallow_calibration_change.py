import hashlib
import tempfile
import unittest
from pathlib import Path
from shallow_calibration_change import identity, validate_change


class CalibrationReviewTests(unittest.TestCase):
    def test_unchanged_weights_do_not_require_new_review(self):
        self.assertEqual(validate_change({'name':'old','x':1}, {'name':'new','x':1}, None, Path('.')), [])

    def test_adjustment_requires_review_and_matching_artifacts(self):
        before, after = {'name':'old','x':1}, {'name':'new','x':1.2}
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            self.assertTrue(validate_change(before, after, None, root))
            artifact=root/'evidence.json';artifact.write_text('{}')
            entry={'assessment':'Reviewed score changes, limitations and applicable mechanism coverage.',
                   'artifact':'evidence.json', 'sha256':hashlib.sha256(artifact.read_bytes()).hexdigest()}
            report={'status':'reviewed','reviewer':'independent-reviewer',
                    'baseline_profile_sha256':identity(before),'proposed_profile_sha256':identity(after),
                    **{k:dict(entry) for k in ['score_deltas','important_ranking_changes','mechanism_coverage','fresh_independent_examples']}}
            report['fresh_independent_examples'].update(selected_before_evaluation=True, independent_reviewer='source-reviewer', used_for_tuning=False)
            self.assertEqual(validate_change(before,after,report,root),[])
            report['proposed_profile_sha256']='stale'
            self.assertTrue(validate_change(before,after,report,root))
            report['proposed_profile_sha256']=identity(after)
            artifact.write_text('changed')
            self.assertTrue(validate_change(before,after,report,root))
            report['fresh_independent_examples']['used_for_tuning']=True
            self.assertTrue(any('tuning' in e for e in validate_change(before,after,report,root)))

if __name__=='__main__': unittest.main()
