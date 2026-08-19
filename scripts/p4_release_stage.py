#!/usr/bin/env python3
"""Prove that the GitHub draft contains the exact locally verified release bytes."""
from __future__ import annotations
import argparse, json, os, ssl, sys, urllib.error, urllib.request
from pathlib import Path
from p4_common import QualificationError, atomic_write_json, now_utc, read_json, reject_secret_shaped_fields, require_repository, require_semver, sha256_file
API_VERSION='2026-03-10'

def get_json(url, token):
    req=urllib.request.Request(url,headers={'Accept':'application/vnd.github+json','Authorization':'Bearer '+token,'X-GitHub-Api-Version':API_VERSION,'User-Agent':'torgnexa-p4-stage/1'})
    try:
        with urllib.request.urlopen(req,timeout=20,context=ssl.create_default_context()) as r: return json.load(r)
    except urllib.error.HTTPError as e: raise QualificationError(f'GitHub release API returned HTTP {e.code}') from e
    except urllib.error.URLError as e: raise QualificationError(f'GitHub release API failed: {e.reason}') from e

def main():
    ap=argparse.ArgumentParser(); ap.add_argument('--repository',required=True); ap.add_argument('--version',required=True); ap.add_argument('--evidence-dir',required=True); ap.add_argument('--bundle',required=True); ap.add_argument('--output',required=True); a=ap.parse_args()
    repo=require_repository(a.repository); version=require_semver(a.version); token=os.environ.get('TORGNEXA_P4_GITHUB_RELEASE_TOKEN','')
    if len(token)<16: raise QualificationError('TORGNEXA_P4_GITHUB_RELEASE_TOKEN is required to inspect draft releases')
    evidence=Path(a.evidence_dir); bundle=Path(a.bundle)
    manifest=read_json(evidence/'evidence.json')
    if manifest.get('release',{}).get('repository')!=repo or manifest.get('release',{}).get('version')!='v'+version: raise QualificationError('local release evidence identity mismatch')
    releases=get_json(f'https://api.github.com/repos/{repo}/releases?per_page=100',token)
    matches=[x for x in releases if isinstance(x,dict) and x.get('tag_name')=='v'+version]
    if len(matches)!=1 or matches[0].get('draft') is not True: raise QualificationError('exactly one draft release must exist for the tag')
    release=matches[0]; asset_rows=[x for x in release.get('assets',[]) if isinstance(x,dict)]
    names=[x.get('name') for x in asset_rows]
    if any(not isinstance(x,str) or not x for x in names) or len(names)!=len(set(names)): raise QualificationError('staged release contains unnamed/duplicate assets')
    assets={x['name']:x for x in asset_rows}
    local={'evidence.json':evidence/'evidence.json', f'torgnexa_{version}_release-evidence.tar.gz':bundle}
    for subject in manifest.get('subjects',[]):
        if subject.get('type')!='binary': continue
        path=str(subject.get('path',''))
        if not path.startswith('artifacts/') or '/' in path[len('artifacts/'):]: raise QualificationError('unsafe binary subject path')
        local[path.split('/')[-1]]=evidence/path
    extras=sorted(set(assets)-set(local)); missing=sorted(set(local)-set(assets))
    if extras or missing: raise QualificationError(f'staged asset set mismatch; missing={missing} extras={extras}')
    checked=[]
    for name,path in sorted(local.items()):
        asset=assets.get(name)
        if not asset or asset.get('state')!='uploaded': raise QualificationError(f'staged asset missing/not uploaded: {name}')
        digest=asset.get('digest'); expected='sha256:'+sha256_file(path)
        if digest!=expected: raise QualificationError(f'staged asset digest mismatch: {name}')
        size=path.stat().st_size
        if asset.get('size')!=size: raise QualificationError(f'staged asset size mismatch: {name}')
        checked.append({'name':name,'asset_id':asset.get('id'),'size':size,'sha256':expected[7:]})
    out={'schema_version':1,'status':'PASS','qualified_at':now_utc(),'repository':repo,'version':version,'release_id':release.get('id'),'draft':True,'assets':checked}
    reject_secret_shaped_fields(out); atomic_write_json(a.output,out); print(f'P4 staged release qualification: PASS ({len(checked)} assets)')
if __name__=='__main__':
    try: main()
    except QualificationError as e: print(f'P4 staged release qualification: {e}',file=sys.stderr); sys.exit(1)
