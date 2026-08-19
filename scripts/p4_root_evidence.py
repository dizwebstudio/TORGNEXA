#!/usr/bin/env python3
"""Compose and re-verify the retained root P4 go-live decision."""
from __future__ import annotations
import argparse, hashlib, json, os, sys
from pathlib import Path
from p4_common import QualificationError, atomic_write_json, now_utc, read_json, reject_secret_shaped_fields, require_repository, require_semver, sha256_file

REQUIRED = (
    'p3/p3-qualification.json',
    'github-protection.json',
    'production-posture.json',
    'release-verification.json',
    'staged-release.json',
    'connectors.json',
)

def _safe_child(root: Path, rel: str) -> Path:
    if not rel or rel.startswith('/') or '\\' in rel or '..' in Path(rel).parts:
        raise QualificationError(f'unsafe P4 evidence path: {rel}')
    raw=root/rel
    if raw.is_symlink(): raise QualificationError(f'P4 evidence symlink is forbidden: {rel}')
    path=raw.resolve()
    if root.resolve() not in path.parents:
        raise QualificationError(f'P4 evidence escapes root: {rel}')
    if not path.is_file():
        raise QualificationError(f'P4 evidence file missing/unsafe: {rel}')
    return path

def _load_subordinate(root: Path):
    loaded={}
    for rel in REQUIRED:
        value=read_json(_safe_child(root,rel)); reject_secret_shaped_fields(value)
        if value.get('status')!='PASS':
            raise QualificationError(f'{rel} is not PASS')
        loaded[rel]=value
    return loaded

def _validate_identity(loaded, version: str, commit: str, repository: str):
    posture=loaded['production-posture.json']; relv=loaded['release-verification.json']; staged=loaded['staged-release.json']; hosted=loaded['github-protection.json']; connectors=loaded['connectors.json']
    if posture.get('release_version')!=version or posture.get('release_commit')!=commit or posture.get('repository')!=repository:
        raise QualificationError('production posture release identity mismatch')
    if relv.get('version')!='v'+version or relv.get('commit')!=commit or relv.get('repository')!=repository:
        raise QualificationError('release verification identity mismatch')
    if staged.get('version')!=version or staged.get('repository')!=repository or staged.get('draft') is not True:
        raise QualificationError('staged release identity mismatch')
    if hosted.get('repository')!=repository:
        raise QualificationError('hosted protection repository mismatch')
    if posture.get('public_base_url','').rstrip('/')!=connectors.get('api_base_url','').rstrip('/'):
        raise QualificationError('production posture/API base URL mismatch')

def compose(root: Path, version: str, commit: str, repository: str, output: Path):
    version=require_semver(version); repository=require_repository(repository)
    if len(commit)!=40 or any(c not in '0123456789abcdef' for c in commit): raise QualificationError('invalid release commit')
    loaded=_load_subordinate(root); _validate_identity(loaded,version,commit,repository)
    evidence=[{'path':rel,'sha256':sha256_file(_safe_child(root,rel))} for rel in REQUIRED]
    value={
        'schema_version':1,'status':'PASS','qualified_at':now_utc(),
        'release_version':version,'release_commit':commit,'repository':repository,
        'evidence':evidence,
        'claim':'This exact tagged release passed repository/topology, hosted protection, production posture, staged-release digest binding, independent release-identity, and live connector gates.'
    }
    reject_secret_shaped_fields(value); atomic_write_json(output,value)

def verify(report: Path):
    if not report.is_file() or report.is_symlink(): raise QualificationError('P4 root report must be a regular non-symlink file')
    value=read_json(report); reject_secret_shaped_fields(value)
    if value.get('schema_version')!=1 or value.get('status')!='PASS': raise QualificationError('P4 root report is not PASS schema v1')
    version=require_semver(str(value.get('release_version',''))); repository=require_repository(str(value.get('repository',''))); commit=str(value.get('release_commit',''))
    if len(commit)!=40 or any(c not in '0123456789abcdef' for c in commit): raise QualificationError('invalid P4 release commit')
    rows=value.get('evidence')
    if not isinstance(rows,list) or len(rows)!=len(REQUIRED): raise QualificationError('P4 root evidence list is incomplete')
    by_path={}
    for row in rows:
        if not isinstance(row,dict) or set(row)!={'path','sha256'}: raise QualificationError('invalid P4 root evidence row')
        rel=row.get('path'); digest=row.get('sha256')
        if rel in by_path: raise QualificationError('duplicate P4 evidence path')
        if not isinstance(digest,str) or len(digest)!=64 or any(c not in '0123456789abcdef' for c in digest): raise QualificationError('invalid P4 evidence digest')
        by_path[rel]=digest
    if set(by_path)!=set(REQUIRED): raise QualificationError('P4 root evidence paths are not exact')
    root=report.parent
    for rel,want in by_path.items():
        if sha256_file(_safe_child(root,rel))!=want: raise QualificationError(f'P4 subordinate evidence digest mismatch: {rel}')
    loaded=_load_subordinate(root); _validate_identity(loaded,version,commit,repository)
    return value

def main():
    ap=argparse.ArgumentParser(); sub=ap.add_subparsers(dest='cmd',required=True)
    c=sub.add_parser('compose'); c.add_argument('--evidence-dir',required=True); c.add_argument('--version',required=True); c.add_argument('--commit',required=True); c.add_argument('--repository',required=True); c.add_argument('--output',required=True)
    v=sub.add_parser('verify'); v.add_argument('--report',required=True)
    a=ap.parse_args()
    if a.cmd=='compose':
        root=Path(a.evidence_dir); output=Path(a.output); compose(root,a.version,a.commit,a.repository,output); print('P4 root evidence composition: PASS')
    else:
        verify(Path(a.report)); print('P4 root evidence verification: PASS')
if __name__=='__main__':
    try: main()
    except QualificationError as e: print(f'P4 root evidence: {e}',file=sys.stderr); sys.exit(1)
