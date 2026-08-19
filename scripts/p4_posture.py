#!/usr/bin/env python3
"""Validate non-secret production posture evidence for P4."""
from __future__ import annotations
import argparse, datetime as dt, sys, urllib.parse
from p4_common import QualificationError, atomic_write_json, now_utc, read_json, reject_secret_shaped_fields, require_repository, require_semver, require_sha40

def parse_time(v):
    if not isinstance(v,str): raise QualificationError('timestamp missing')
    try: return dt.datetime.fromisoformat(v.replace('Z','+00:00'))
    except ValueError as e: raise QualificationError('invalid RFC3339 timestamp') from e

def main():
    ap=argparse.ArgumentParser(); ap.add_argument('--input',required=True); ap.add_argument('--output',required=True); a=ap.parse_args()
    v=read_json(a.input); reject_secret_shaped_fields(v,allow={'secret_management'})
    if v.get('schema_version')!=1 or v.get('environment')!='production': raise QualificationError('production posture schema/environment mismatch')
    version=require_semver(str(v.get('release_version',''))); commit=require_sha40(str(v.get('release_commit',''))); repo=require_repository(str(v.get('repository','')))
    u=urllib.parse.urlsplit(str(v.get('public_base_url','')))
    if u.scheme!='https' or not u.hostname or u.username or u.password or u.query or u.fragment: raise QualificationError('public_base_url must be credential-free HTTPS origin/path')
    sm=v.get('secret_management') or {}
    if sm.get('backend') not in {'vault','kubernetes-secrets','aws-secrets-manager','gcp-secret-manager','azure-key-vault','hsm-kms','external-secret-operator','other-reviewed'}: raise QualificationError('reviewed production secret backend required')
    if sm.get('plaintext_env_file') is not False or sm.get('rotation_rehearsed') is not True or sm.get('break_glass_documented') is not True: raise QualificationError('secret rotation/break-glass/plaintext posture not qualified')
    ops=v.get('operations') or {}
    for key in ('on_call_owner_defined','rollback_owner_defined','incident_runbook_exercised','monitoring_alerts_verified'):
        if ops.get(key) is not True: raise QualificationError(f'operations.{key} must be true')
    data=v.get('data_protection') or {}
    for key in ('encrypted_backup_store','restore_rehearsed','wal_or_equivalent_recovery','retention_reviewed'):
        if data.get(key) is not True: raise QualificationError(f'data_protection.{key} must be true')
    net=v.get('network') or {}
    for key in ('database_encrypted_in_transit','object_storage_encrypted_in_transit','oidc_https_only'):
        if net.get(key) is not True: raise QualificationError(f'network.{key} must be true')
    approved=parse_time(v.get('approved_at')); now=dt.datetime.now(dt.timezone.utc)
    if approved.tzinfo is None or approved>now+dt.timedelta(minutes=5) or now-approved>dt.timedelta(days=30): raise QualificationError('approved_at must be within the last 30 days')
    out={'schema_version':1,'status':'PASS','qualified_at':now_utc(),'release_version':version,'release_commit':commit,'repository':repo,'public_base_url':urllib.parse.urlunsplit((u.scheme,u.netloc,u.path.rstrip('/'),'','')),'management_backend':sm['backend'],'approved_at':v['approved_at'],'approvers':v.get('approvers',[])}
    if not isinstance(out['approvers'],list) or len(out['approvers'])<2 or any(not isinstance(x,str) or not x.strip() for x in out['approvers']): raise QualificationError('at least two named/role approvers are required')
    reject_secret_shaped_fields(out); atomic_write_json(a.output,out); print('P4 production posture qualification: PASS')
if __name__=='__main__':
    try: main()
    except QualificationError as e: print(f"P4 production posture qualification: {e}",file=sys.stderr); sys.exit(1)
