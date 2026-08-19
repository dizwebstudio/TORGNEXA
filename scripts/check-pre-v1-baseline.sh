#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd -- "$repo_root"
python3 scripts/generate-pre-v1-baseline.py --check
python3 - <<'PY'
import json
from pathlib import Path
active=json.loads(Path('migrations/catalog.json').read_text())
legacy=json.loads(Path('migrations_legacy_pre_v1/catalog.json').read_text())
manifest=json.loads(Path('migrations/baseline-manifest.json').read_text())
active_sql=sorted(Path('migrations').glob('*.sql'))
legacy_sql=sorted(Path('migrations_legacy_pre_v1').glob('*.sql'))
assert len(active['migrations']) >= 11, len(active['migrations'])
assert len(active_sql) == len(active['migrations']), (len(active_sql), len(active['migrations']))
assert len(legacy['migrations']) == 74, len(legacy['migrations'])
assert len(legacy_sql) == 74, len(legacy_sql)
assert manifest['baseline_migration_count'] == 11
assert manifest['legacy_head_version'] == 74
# The squashed baseline (versions 1..11) is immutable; anything past it is an
# ordinary post-baseline migration and is only required to keep the overall
# version sequence contiguous.
assert [m['version'] for m in active['migrations']] == list(range(1, len(active['migrations']) + 1))
for m in active['migrations'][3:]:
    body=(Path('migrations')/m['file']).read_text()
    assert body.count('INSERT INTO migration_history') == 1, m['file']
rebaseline=Path('deploy/postgres/rebaseline-pre-v1.sh').read_text()
for required in [
    'I_UNDERSTAND_THIS_REWRITES_MIGRATION_HISTORY',
    'migration_history_legacy_pre_v1',
    'migration_baseline_evidence',
    'legacy migration history mismatch',
    'LOCK TABLE migration_history IN ACCESS EXCLUSIVE MODE',
]:
    assert required in rebaseline, required
assert rebaseline.index('INSERT INTO migration_history_legacy_pre_v1 SELECT') < rebaseline.index('DELETE FROM migration_history')
migrator=Path('deploy/postgres/migrate.sh').read_text()
assert 'legacy pre-v1 migration history detected' in migrator
assert '/deploy/postgres/rebaseline-pre-v1.sh' in migrator
print('TORGNEXA migration inventory: PASS — 11 active baseline SQL files; 74-file pre-v1 history archived outside runtime migrations')
PY
