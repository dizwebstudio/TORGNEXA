#!/usr/bin/env python3
"""Qualify configured connector accounts through the public TORGNEXA API only.

The bearer token is read from TORGNEXA_P4_BEARER_TOKEN and is never retained.
Remote sync is disabled unless both the plan and an explicit danger acknowledgement enable it.

This gate is credentialed at the TORGNEXA boundary: an active account must have
an opaque SecretProvider reference, and the configured required capabilities
must be enabled on that account. The two health checks then exercise the
connector runtime, but do not claim marketplace business read-after-write
qualification; that remains a separate remote evidence gate.
"""
from __future__ import annotations
import argparse, json, os, re, ssl, sys, urllib.error, urllib.parse, urllib.request, uuid
from pathlib import Path
from p4_common import QualificationError, atomic_write_json, now_utc, read_json, reject_secret_shaped_fields

CONNECTOR_ID_RE = re.compile(r"^[a-z0-9][a-z0-9-]{0,62}$")
CAPABILITY_RE = re.compile(r"^[a-z][a-z0-9._-]{0,127}$")

class API:
    def __init__(self, base: str, bearer: str, timeout: float):
        self.base=base.rstrip('/')
        self.bearer=bearer
        self.timeout=timeout
    def call(self, method: str, path: str, body=None, idem=None):
        data=None if body is None else json.dumps(body,separators=(',',':')).encode()
        headers={"Accept":"application/json","Authorization":"Bearer "+self.bearer,"User-Agent":"torgnexa-p4-qualifier/1"}
        if data is not None: headers["Content-Type"]="application/json"
        if idem: headers["Idempotency-Key"]=idem
        req=urllib.request.Request(self.base+path,data=data,headers=headers,method=method)
        try:
            with urllib.request.urlopen(req,timeout=self.timeout,context=ssl.create_default_context()) as r:
                raw=r.read(1024*1024+1)
                if len(raw)>1024*1024: raise QualificationError("API response exceeds 1 MiB")
                return r.status, json.loads(raw or b'{}')
        except urllib.error.HTTPError as e:
            raw=e.read(65536)
            raise QualificationError(f"API {method} {path} returned HTTP {e.code}: {raw[:512].decode('utf-8','replace')}") from e
        except urllib.error.URLError as e:
            raise QualificationError(f"API {method} {path} failed: {e.reason}") from e

def parse_plan(path: str):
    v=read_json(path); reject_secret_shaped_fields(v)
    if v.get("schema_version")!=1: raise QualificationError("connector plan schema_version must be 1")
    unknown=set(v)-{"schema_version","accounts"}
    if unknown: raise QualificationError("unknown connector plan fields: "+",".join(sorted(unknown)))
    items=v.get("accounts")
    if not isinstance(items,list) or not items: raise QualificationError("connector plan requires non-empty accounts")
    out=[]; ids=set()
    for x in items:
        if not isinstance(x,dict): raise QualificationError("connector account entry must be an object")
        unknown_account=set(x)-{"account_id","connector_id","run_sync","required_capabilities"}
        if unknown_account: raise QualificationError("unknown connector account fields: "+",".join(sorted(unknown_account)))
        aid=str(x.get("account_id","")).strip(); cid=str(x.get("connector_id","")).strip()
        if not aid or len(aid)>128 or aid in ids: raise QualificationError("account_id must be unique and <=128 chars")
        if not CONNECTOR_ID_RE.fullmatch(cid): raise QualificationError(f"invalid connector_id: {cid}")
        run_sync=x.get("run_sync",False)
        if not isinstance(run_sync,bool): raise QualificationError("run_sync must be boolean")
        required=x.get("required_capabilities",[])
        if not isinstance(required,list) or any(not isinstance(value,str) or not CAPABILITY_RE.fullmatch(value) for value in required):
            raise QualificationError("required_capabilities must be a list of capability names")
        if len(set(required)) != len(required):
            raise QualificationError("required_capabilities must not contain duplicates")
        ids.add(aid); out.append({"account_id":aid,"connector_id":cid,"run_sync":run_sync,"required_capabilities":sorted(required)})
    return {"accounts":out}

def validate_credentialed_account(current: dict, spec: dict) -> None:
    secret_reference=current.get('secret_reference')
    if not isinstance(secret_reference,str) or not secret_reference.strip():
        raise QualificationError(f"connector account has no SecretProvider reference: {spec['account_id']}")
    settings=current.get('capabilities')
    if not isinstance(settings,list):
        raise QualificationError(f"connector account capability snapshot is missing: {spec['account_id']}")
    enabled={item.get('capability') for item in settings if isinstance(item,dict) and item.get('enabled') is True and isinstance(item.get('capability'),str)}
    missing=sorted(set(spec['required_capabilities'])-enabled)
    if missing:
        raise QualificationError(f"required capabilities are not enabled for {spec['account_id']}: {','.join(missing)}")

def list_all_accounts(api: API):
    items=[]; cursor=''; seen=set()
    for _ in range(1000):
        query={'limit':'100'}
        if cursor: query['cursor']=cursor
        _,page=api.call('GET','/api/v1/connector-accounts?'+urllib.parse.urlencode(query))
        batch=page.get('items')
        if not isinstance(batch,list): raise QualificationError('connector account page items must be a list')
        items.extend(x for x in batch if isinstance(x,dict))
        next_cursor=page.get('next_cursor','')
        if not next_cursor: return items
        if not isinstance(next_cursor,str) or next_cursor in seen: raise QualificationError('invalid/repeated connector account cursor')
        seen.add(next_cursor); cursor=next_cursor
    raise QualificationError('connector account pagination exceeded safety limit')

def main():
    ap=argparse.ArgumentParser(); ap.add_argument('--base-url',required=True); ap.add_argument('--plan',required=True); ap.add_argument('--output',required=True); ap.add_argument('--timeout',type=float,default=20.0)
    a=ap.parse_args()
    parsed=urllib.parse.urlsplit(a.base_url)
    if parsed.scheme!='https' or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment: raise QualificationError("P4 connector qualification requires a credential-free https:// API base URL")
    normalized_base=urllib.parse.urlunsplit((parsed.scheme,parsed.netloc,parsed.path.rstrip('/'),'',''))
    token=os.environ.get('TORGNEXA_P4_BEARER_TOKEN','')
    if len(token)<16: raise QualificationError("TORGNEXA_P4_BEARER_TOKEN is required")
    api=API(normalized_base,token,a.timeout); plan=parse_plan(a.plan); wanted=plan['accounts']
    all_accounts=list_all_accounts(api); byid={x.get('id'):x for x in all_accounts if isinstance(x.get('id'),str)}
    wanted_ids={x['account_id'] for x in wanted}
    unqualified=sorted(str(x.get('id')) for x in all_accounts if x.get('status')=='active' and x.get('id') not in wanted_ids)
    if unqualified: raise QualificationError('active connector accounts omitted from P4 plan: '+','.join(unqualified))
    results=[]
    for spec in wanted:
        current=byid.get(spec['account_id'])
        if not current: raise QualificationError(f"connector account not found: {spec['account_id']}")
        if current.get('connector_id')!=spec['connector_id']: raise QualificationError(f"connector id mismatch for {spec['account_id']}")
        if current.get('status')!='active': raise QualificationError(f"connector account is not active: {spec['account_id']}")
        validate_credentialed_account(current,spec)
        version=current.get('version')
        if not isinstance(version,int) or version<0: raise QualificationError("connector account version missing")
        checked=current
        for attempt in range(2):
            expected=checked.get('version')
            if not isinstance(expected,int): raise QualificationError("post-health version missing")
            idem=f'p4-health-{attempt}-'+uuid.uuid4().hex
            _,checked=api.call('POST','/api/v1/connector-accounts:check',{'account_id':spec['account_id'],'expected_version':expected},idem)
            if checked.get('health_status')!='healthy':
                raise QualificationError(f"connector {spec['account_id']} is not healthy: {checked.get('health_reason_code','unknown')}")
        q=urllib.parse.urlencode({'account_id':spec['account_id'],'limit':'2'})
        _,hist=api.call('GET','/api/v1/connector-accounts:health-history?'+q)
        snapshots=hist.get('items') or []
        if len(snapshots)!=2 or any(x.get('category')!='healthy' for x in snapshots): raise QualificationError(f"two consecutive healthy snapshots required for {spec['account_id']}")
        sync_started=None
        if spec['run_sync']:
            if os.environ.get('TORGNEXA_P4_ALLOW_REMOTE_SYNC')!='I_UNDERSTAND_THIS_MAY_WRITE':
                raise QualificationError("run_sync requested but TORGNEXA_P4_ALLOW_REMOTE_SYNC acknowledgement is absent")
            checked_version=checked.get('version')
            if not isinstance(checked_version,int): raise QualificationError("post-health version missing")
            _,sync=api.call('POST','/api/v1/connector-accounts:sync',{'account_id':spec['account_id'],'expected_version':checked_version},'p4-sync-'+uuid.uuid4().hex)
            sync_started=sync.get('started')
            if not isinstance(sync_started,int) or sync_started<1: raise QualificationError(f"sync did not start for {spec['account_id']}")
        results.append({'account_id':spec['account_id'],'connector_id':spec['connector_id'],'credentialed':True,'credential_source':'SecretProvider','required_capabilities':spec['required_capabilities'],'health_status':'healthy','health_category':'healthy','consecutive_healthy_checks':2,'checked_at':snapshots[0].get('checked_at'),'sync_started':sync_started})
    evidence={'schema_version':1,'status':'PASS','qualified_at':now_utc(),'api_base_url':normalized_base,'accounts':results}
    reject_secret_shaped_fields(evidence)
    atomic_write_json(a.output,evidence); print(f"P4 live connector qualification: PASS ({len(results)} accounts)")
if __name__=='__main__':
    try: main()
    except QualificationError as e: print(f"P4 live connector qualification: {e}",file=sys.stderr); sys.exit(1)
