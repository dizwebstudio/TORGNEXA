#!/usr/bin/env python3
from __future__ import annotations
import datetime as dt
import importlib.util
import json
import pathlib
import sys
import tempfile
import unittest

HERE=pathlib.Path(__file__).resolve().parent
sys.path.insert(0,str(HERE))
from p4_common import QualificationError, reject_secret_shaped_fields
import p4_hosting_rules
import p4_live_connectors
import p4_root_evidence

class P4PolicyTests(unittest.TestCase):
    def test_hosting_requires_pinned_required_workflow_and_reviews(self):
        sha='a'*40
        value={'schema_version':1,'repository_id':7,'rules':[
            {'type':'deletion'}, {'type':'non_fast_forward'},
            {'type':'pull_request','parameters':{'required_approving_review_count':1,'required_reviewers':[{'file_patterns':['architecture/**','adr/**'],'minimum_approvals':1,'reviewer':{'id':99,'type':'Team'}}]}},
            {'type':'workflows','parameters':{'workflows':[{'path':'.github/workflows/architecture-required.yml','repository_id':7,'sha':sha,'ref':'refs/heads/main'}]}}
        ]}
        result=p4_hosting_rules.verify_applied(value)
        self.assertEqual(result['required_workflows'][0]['sha'],sha)
        broken=json.loads(json.dumps(value)); broken['rules'][-1]['parameters']['workflows'][0]['sha']=''
        with self.assertRaises(QualificationError): p4_hosting_rules.verify_applied(broken)

    def test_connector_plan_forbids_secret_material_and_invalid_provider(self):
        with tempfile.TemporaryDirectory() as d:
            p=pathlib.Path(d)/'p.json'
            p.write_text(json.dumps({'schema_version':1,'accounts':[{'account_id':'wb','connector_id':'wildberries','run_sync':False}]}))
            self.assertEqual(p4_live_connectors.parse_plan(str(p))['accounts'][0]['connector_id'],'wildberries')
            p.write_text(json.dumps({'schema_version':1,'accounts':[{'account_id':'wb','connector_id':'wildberries','api_token':'x'}]}))
            with self.assertRaises(QualificationError): p4_live_connectors.parse_plan(str(p))
            p.write_text(json.dumps({'schema_version':1,'accounts':[{'account_id':'x','connector_id':'INVALID_provider'}]}))
            with self.assertRaises(QualificationError): p4_live_connectors.parse_plan(str(p))

    def test_connector_plan_parses_required_capabilities_and_rejects_duplicates(self):
        with tempfile.TemporaryDirectory() as d:
            p=pathlib.Path(d)/'p.json'
            p.write_text(json.dumps({'schema_version':1,'accounts':[{'account_id':'wb','connector_id':'wildberries','required_capabilities':['products.read','inventory.read']}]}))
            result=p4_live_connectors.parse_plan(str(p))
            self.assertEqual(result['accounts'][0]['required_capabilities'],['inventory.read','products.read'])
            p.write_text(json.dumps({'schema_version':1,'accounts':[{'account_id':'wb','connector_id':'wildberries','required_capabilities':['products.read','products.read']}]}))
            with self.assertRaises(QualificationError): p4_live_connectors.parse_plan(str(p))

    def test_credentialed_account_requires_secret_reference_and_capabilities(self):
        spec={'account_id':'wb','required_capabilities':['products.read','inventory.read']}
        account={'secret_reference':'sec:v1:0123456789abcdef0123456789abcdef','capabilities':[{'capability':'products.read','enabled':True},{'capability':'inventory.read','enabled':True}]}
        p4_live_connectors.validate_credentialed_account(account,spec)
        missing=dict(account); missing.pop('secret_reference')
        with self.assertRaises(QualificationError): p4_live_connectors.validate_credentialed_account(missing,spec)
        disabled={'secret_reference':account['secret_reference'],'capabilities':[{'capability':'products.read','enabled':True},{'capability':'inventory.read','enabled':False}]}
        with self.assertRaises(QualificationError): p4_live_connectors.validate_credentialed_account(disabled,spec)


    def test_connector_pagination_collects_all_pages(self):
        class FakeAPI:
            def __init__(self): self.calls=[]
            def call(self, method, path, body=None, idem=None):
                self.calls.append(path)
                if 'cursor=' not in path:
                    return 200, {'items':[{'id':'a','status':'active'}],'next_cursor':'v1.next'}
                return 200, {'items':[{'id':'b','status':'disabled'}]}
        api=FakeAPI()
        items=p4_live_connectors.list_all_accounts(api)
        self.assertEqual([x['id'] for x in items],['a','b'])
        self.assertEqual(len(api.calls),2)


    def test_root_evidence_binds_subordinate_hashes_and_identity(self):
        with tempfile.TemporaryDirectory() as d:
            root=pathlib.Path(d); (root/'p3').mkdir()
            repo='example/torgnexa'; version='1.2.3'; commit='a'*40; base='https://torgnexa.example.com'
            docs={
                'p3/p3-qualification.json':{'status':'PASS'},
                'github-protection.json':{'status':'PASS','repository':repo},
                'production-posture.json':{'status':'PASS','repository':repo,'release_version':version,'release_commit':commit,'public_base_url':base},
                'release-verification.json':{'status':'PASS','repository':repo,'version':'v'+version,'commit':commit},
                'staged-release.json':{'status':'PASS','repository':repo,'version':version,'draft':True,'release_id':7,'assets':[]},
                'connectors.json':{'status':'PASS','api_base_url':base,'accounts':[]},
            }
            for rel,value in docs.items():
                path=root/rel; path.parent.mkdir(parents=True,exist_ok=True); path.write_text(json.dumps(value)+'\n')
            report=root/'p4-go-live.json'
            p4_root_evidence.compose(root,version,commit,repo,report)
            self.assertEqual(p4_root_evidence.verify(report)['status'],'PASS')
            (root/'connectors.json').write_text(json.dumps({'status':'PASS','api_base_url':base,'accounts':[{'account_id':'changed'}]})+'\n')
            with self.assertRaises(QualificationError): p4_root_evidence.verify(report)

    def test_retained_evidence_rejects_secret_shaped_fields(self):
        with self.assertRaises(QualificationError): reject_secret_shaped_fields({'access_token':'do-not-retain'})
        reject_secret_shaped_fields({'status':'PASS','connector_id':'ozon'})

if __name__=='__main__': unittest.main()
