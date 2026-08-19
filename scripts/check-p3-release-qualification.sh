#!/usr/bin/env bash
set -euo pipefail

umask 077
export LC_ALL=C
export TZ=UTC
export GOTELEMETRY=off
export GOTOOLCHAIN=local
export GOWORK=off

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$root"

fail() { echo "P3 release qualification: $*" >&2; exit 1; }
for cmd in go docker python3 sha256sum; do command -v "$cmd" >/dev/null 2>&1 || fail "$cmd is required"; done
[[ "$(go env GOVERSION)" == "go1.26.5" ]] || fail "Go 1.26.5 is required; got $(go env GOVERSION)"
docker compose version >/dev/null || fail "Docker Compose v2 is required"

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
evidence="${TORGNEXA_P3_EVIDENCE_DIR:-$root/qualification/evidence/p3-$stamp}"
[[ "$evidence" == /* ]] || fail "evidence path must be absolute"
mkdir -p "$evidence"
[[ -z "$(find "$evidence" -mindepth 1 -maxdepth 1 -print -quit)" ]] || fail "evidence directory must be empty"

make check >"$evidence/repository-check.log" 2>&1
TORGNEXA_QUALIFICATION_EVIDENCE_DIR="$evidence/runtime" ./scripts/check-production-qualification.sh >"$evidence/runtime.log" 2>&1
./scripts/check-postgres-backup-restore.sh --evidence-file "$evidence/backup-restore.json" >"$evidence/backup-restore.log" 2>&1
./scripts/check-postgres-upgrade.sh >"$evidence/upgrade.log" 2>&1

python3 - "$evidence" <<'PY'
import hashlib,json,os,platform,subprocess,sys,time
root=sys.argv[1]
files=[]
for base,_,names in os.walk(root):
    for name in sorted(names):
        if name == 'p3-qualification.json': continue
        path=os.path.join(base,name)
        rel=os.path.relpath(path,root)
        with open(path,'rb') as f: digest=hashlib.sha256(f.read()).hexdigest()
        files.append({'path':rel,'sha256':digest})
meta={
 'version':1,'status':'PASS','qualified_at':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),
 'go_version':subprocess.check_output(['go','env','GOVERSION'],text=True).strip(),
 'host':{'system':platform.system(),'release':platform.release(),'machine':platform.machine()},
 'evidence':files,
 'external_hosted_gates':{
   'task_065_protected_oidc_prerelease':'must be verified by the protected release workflow',
   'task_080_required_workflow_ruleset':'must be verified from hosting configuration; not synthesized locally'
 }
}
with open(os.path.join(root,'p3-qualification.json'),'w',encoding='utf-8') as f:
    json.dump(meta,f,indent=2,sort_keys=True); f.write('\n')
PY

echo "P3 repository/topology qualification: PASS"
echo "evidence: $evidence"
