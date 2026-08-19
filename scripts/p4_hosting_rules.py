#!/usr/bin/env python3
"""Capture/verify the GitHub rules that actually apply to the protected release branch."""
from __future__ import annotations
import argparse, json, os, ssl, sys, urllib.error, urllib.parse, urllib.request
from p4_common import QualificationError, atomic_write_json, now_utc, read_json, reject_secret_shaped_fields, require_repository
API_VERSION='2026-03-10'

def _get(url, headers):
    req=urllib.request.Request(url,headers=headers)
    try:
        with urllib.request.urlopen(req,timeout=20,context=ssl.create_default_context()) as r: return json.load(r)
    except urllib.error.HTTPError as e: raise QualificationError(f"GitHub API returned HTTP {e.code}") from e
    except urllib.error.URLError as e: raise QualificationError(f"GitHub API failed: {e.reason}") from e

def fetch(repository:str, branch:str):
    base=f"https://api.github.com/repos/{repository}"
    headers={'Accept':'application/vnd.github+json','X-GitHub-Api-Version':API_VERSION,'User-Agent':'torgnexa-p4-hosting/1'}
    token=os.environ.get('TORGNEXA_P4_GITHUB_TOKEN','')
    if token: headers['Authorization']='Bearer '+token
    meta=_get(base,headers); rules=_get(base+f"/rules/branches/{urllib.parse.quote(branch,safe='')}",headers)
    if not isinstance(rules,list): raise QualificationError("GitHub rules response must be a list")
    if not isinstance(meta,dict) or not isinstance(meta.get('id'),int): raise QualificationError("GitHub repository metadata missing id")
    return {'schema_version':1,'captured_at':now_utc(),'repository':repository,'repository_id':meta['id'],'branch':branch,'api_version':API_VERSION,'rules':rules}

def verify_applied(v):
    reject_secret_shaped_fields(v)
    if v.get('schema_version')!=1: raise QualificationError('hosting rules schema_version must be 1')
    repo_id=v.get('repository_id')
    if not isinstance(repo_id,int): raise QualificationError('repository_id missing from captured GitHub metadata')
    rules=v.get('rules')
    if not isinstance(rules,list): raise QualificationError('rules must be a list')
    kinds=[r.get('type') for r in rules if isinstance(r,dict)]
    required={'deletion','non_fast_forward','pull_request','workflows'}
    missing=sorted(required-set(kinds))
    if missing: raise QualificationError('missing active branch rules: '+','.join(missing))
    prs=[r for r in rules if isinstance(r,dict) and r.get('type')=='pull_request']
    ok_review=False; architecture_reviewers=[]
    for r in prs:
        p=r.get('parameters') or {}
        approvals=p.get('required_approving_review_count',0)
        for rr in p.get('required_reviewers') or []:
            if not isinstance(rr,dict) or not isinstance(rr.get('minimum_approvals'),int) or rr.get('minimum_approvals',0)<1: continue
            reviewer=rr.get('reviewer') or {}; patterns=rr.get('file_patterns') or []
            if reviewer.get('type')!='Team' or not isinstance(reviewer.get('id'),int): continue
            if not any(isinstance(x,str) and (x.startswith('architecture/') or x in {'architecture/**','architecture/reviews/**','adr/**'}) for x in patterns): continue
            architecture_reviewers.append({'team_id':reviewer['id'],'minimum_approvals':rr['minimum_approvals'],'file_patterns':patterns})
        if isinstance(approvals,int) and approvals>=1 and architecture_reviewers:
            ok_review=True
    if not ok_review: raise QualificationError('pull_request rule must require >=1 general approval plus a required Team reviewer for architecture paths')
    required_path='.github/workflows/architecture-required.yml'
    matched=[]
    for r in rules:
        if not isinstance(r,dict) or r.get('type')!='workflows': continue
        for wf in (r.get('parameters') or {}).get('workflows') or []:
            if not isinstance(wf,dict) or wf.get('path')!=required_path: continue
            source_id=wf.get('repository_id'); sha=str(wf.get('sha',''))
            if not isinstance(source_id,int): continue
            if len(sha)!=40 or any(c not in '0123456789abcdef' for c in sha): continue
            matched.append({'path':required_path,'repository_id':source_id,'sha':sha,'ref':wf.get('ref','')})
    if not matched: raise QualificationError(f'active rules do not prove pinned required workflow {required_path}')
    return {'rule_types':sorted(set(kinds)),'review_protection':True,'architecture_reviewers':architecture_reviewers,'required_workflows':matched}

def main():
    ap=argparse.ArgumentParser(); sub=ap.add_subparsers(dest='cmd',required=True)
    c=sub.add_parser('capture'); c.add_argument('--repository',required=True); c.add_argument('--branch',required=True); c.add_argument('--output',required=True)
    v=sub.add_parser('verify'); v.add_argument('--applied-rules',required=True); v.add_argument('--output',required=True)
    a=ap.parse_args()
    if a.cmd=='capture':
        repo=require_repository(a.repository); branch=a.branch.strip()
        if not branch or len(branch)>255: raise QualificationError('invalid branch')
        data=fetch(repo,branch); reject_secret_shaped_fields(data); atomic_write_json(a.output,data); print('P4 GitHub applied-rules capture: PASS'); return
    applied=read_json(a.applied_rules); repo=require_repository(str(applied.get('repository',''))); branch=str(applied.get('branch',''))
    ar=verify_applied(applied)
    atomic_write_json(a.output,{'schema_version':1,'status':'PASS','qualified_at':now_utc(),'repository':repo,'branch':branch,'applied_rules':ar})
    print('P4 hosted protection qualification: PASS')
if __name__=='__main__':
    try: main()
    except QualificationError as e: print(f"P4 hosted protection qualification: {e}",file=sys.stderr); sys.exit(1)
