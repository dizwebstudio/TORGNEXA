#!/usr/bin/env bash
set -euo pipefail

export TZ=UTC

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd -- "$root"

aggregate="${TORGNEXA_FINANCIAL_WAREHOUSE_EVIDENCE_FILE:-}"
bank="${TORGNEXA_BANK_QUALIFICATION_EVIDENCE_FILE:-}"
acquirer="${TORGNEXA_ACQUIRER_QUALIFICATION_EVIDENCE_FILE:-}"
marketplace_payout="${TORGNEXA_MARKETPLACE_PAYOUT_QUALIFICATION_EVIDENCE_FILE:-}"
fx="${TORGNEXA_FX_QUALIFICATION_EVIDENCE_FILE:-}"
advertising="${TORGNEXA_ADVERTISING_QUALIFICATION_EVIDENCE_FILE:-}"
fbs="${TORGNEXA_FBS_QUALIFICATION_EVIDENCE_FILE:-}"
fbo="${TORGNEXA_FBO_QUALIFICATION_EVIDENCE_FILE:-}"
hardware="${TORGNEXA_HARDWARE_QUALIFICATION_EVIDENCE_FILE:-}"
partner_uat="${TORGNEXA_PARTNER_UAT_QUALIFICATION_EVIDENCE_FILE:-}"
rollback="${TORGNEXA_ROLLBACK_QUALIFICATION_EVIDENCE_FILE:-}"
slo_dr="${TORGNEXA_SLO_DR_QUALIFICATION_EVIDENCE_FILE:-}"
production_support="${TORGNEXA_PRODUCTION_SUPPORT_QUALIFICATION_EVIDENCE_FILE:-}"

require_absolute_file() {
  local name="$1"
  local path="$2"
  if [[ -z "$path" || "$path" != /* ]]; then
    echo "financial/warehouse qualification: $name must be an absolute path" >&2
    exit 2
  fi
  if [[ -L "$path" || ! -f "$path" ]]; then
    echo "financial/warehouse qualification: $name must be a regular non-symlink file" >&2
    exit 2
  fi
}

require_absolute_file TORGNEXA_FINANCIAL_WAREHOUSE_EVIDENCE_FILE "$aggregate"
require_absolute_file TORGNEXA_BANK_QUALIFICATION_EVIDENCE_FILE "$bank"
require_absolute_file TORGNEXA_ACQUIRER_QUALIFICATION_EVIDENCE_FILE "$acquirer"
require_absolute_file TORGNEXA_MARKETPLACE_PAYOUT_QUALIFICATION_EVIDENCE_FILE "$marketplace_payout"
require_absolute_file TORGNEXA_FX_QUALIFICATION_EVIDENCE_FILE "$fx"
require_absolute_file TORGNEXA_ADVERTISING_QUALIFICATION_EVIDENCE_FILE "$advertising"
require_absolute_file TORGNEXA_FBS_QUALIFICATION_EVIDENCE_FILE "$fbs"
require_absolute_file TORGNEXA_FBO_QUALIFICATION_EVIDENCE_FILE "$fbo"
require_absolute_file TORGNEXA_HARDWARE_QUALIFICATION_EVIDENCE_FILE "$hardware"
require_absolute_file TORGNEXA_PARTNER_UAT_QUALIFICATION_EVIDENCE_FILE "$partner_uat"
require_absolute_file TORGNEXA_ROLLBACK_QUALIFICATION_EVIDENCE_FILE "$rollback"
require_absolute_file TORGNEXA_SLO_DR_QUALIFICATION_EVIDENCE_FILE "$slo_dr"
require_absolute_file TORGNEXA_PRODUCTION_SUPPORT_QUALIFICATION_EVIDENCE_FILE "$production_support"

echo "Running provider-neutral financial completeness qualification..."
make financial-completeness-qualification
echo "Running provider-neutral mobile warehouse qualification..."
make mobile-warehouse-qualification

release_commit="$(git rev-parse HEAD)"
validator_args=(
  --input "$aggregate"
  --bank-evidence "$bank"
  --acquirer-evidence "$acquirer"
  --marketplace-payout-evidence "$marketplace_payout"
  --fx-evidence "$fx"
  --advertising-evidence "$advertising"
  --fbs-evidence "$fbs"
  --fbo-evidence "$fbo"
  --hardware-evidence "$hardware"
  --partner-uat-evidence "$partner_uat"
  --rollback-evidence "$rollback"
  --slo-dr-evidence "$slo_dr"
  --production-support-evidence "$production_support"
  --expected-release-commit "$release_commit"
)
if [[ -n "${TORGNEXA_P4_REPOSITORY:-}" ]]; then
  validator_args+=(--expected-repository "$TORGNEXA_P4_REPOSITORY")
fi

PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$root/scripts" \
  python3 "$root/scripts/financial_warehouse_qualification.py" "${validator_args[@]}"

echo "Financial and warehouse external qualification: PASS (synthetic + retained credentialed evidence)"
