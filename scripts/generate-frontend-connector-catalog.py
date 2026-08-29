#!/usr/bin/env python3
"""Generate the non-secret frontend connector catalog from canonical manifests."""

import json
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "frontend/src/generated/connector-catalog.ts"
GO_OUTPUT = ROOT / "internal/platform/connectors/catalog_generated.go"
RUNTIME_SUPPORT = ROOT / "contracts/connectors/builtin-runtime-support-v1.json"
GO_SUPPORT_OUTPUT = ROOT / "internal/platform/builtinruntime/support_generated.go"

CATEGORY_BY_FAMILY = {
    "ai": "ai",
    "classified": "classified",
    "crm": "crm",
    "edo": "edo",
    "erp": "erp",
    "fx": "finance",
    "government": "government",
    "logistics": "logistics",
    "marketplace": "marketplaces",
    "notification": "notifications",
    "payment": "payments",
    "pickup": "logistics",
    "social": "social",
    "storefront": "storefronts",
}


def quoted(value: str) -> str:
    return json.dumps(value, ensure_ascii=False)


entries = []
manifest_documents = []
for path in sorted((ROOT / "connectors").glob("*/*/manifest.json")):
    document = json.loads(path.read_text(encoding="utf-8"))
    required = ("id", "name", "family", "version", "capabilities", "auth")
    if any(key not in document for key in required):
        raise SystemExit(f"incomplete connector manifest: {path.relative_to(ROOT)}")
    if path.parent.name != document["id"]:
        raise SystemExit(f"connector directory/id mismatch: {path.relative_to(ROOT)}")
    expected_category = CATEGORY_BY_FAMILY.get(document["family"])
    if expected_category is None or path.parent.parent.name != expected_category:
        raise SystemExit(f"connector directory/category mismatch: {path.relative_to(ROOT)}")
    presentation_path = path.parent / "presentation.json"
    presentation = None
    if presentation_path.is_file():
        presentation_document = json.loads(presentation_path.read_text(encoding="utf-8"))
        required_presentation = {"logo", "surface", "surface_alt", "foreground", "accent"}
        if set(presentation_document) != required_presentation:
            raise SystemExit(f"invalid connector presentation keys: {presentation_path.relative_to(ROOT)}")
        if any(not isinstance(presentation_document[key], str) for key in required_presentation):
            raise SystemExit(f"connector presentation values must be strings: {presentation_path.relative_to(ROOT)}")
        if any(not re.fullmatch(r"#[0-9A-Fa-f]{6}", presentation_document[key]) for key in ("surface", "surface_alt", "foreground", "accent")):
            raise SystemExit(f"invalid connector presentation color: {presentation_path.relative_to(ROOT)}")
        expected_logo = f"/connector-logos/{document['id']}.svg"
        if presentation_document["logo"] != expected_logo:
            raise SystemExit(f"connector presentation logo must be {expected_logo}: {presentation_path.relative_to(ROOT)}")
        logo_path = ROOT / "frontend" / "public" / expected_logo.lstrip("/")
        if not logo_path.is_file() or logo_path.is_symlink():
            raise SystemExit(f"missing or unsafe connector logo: {logo_path.relative_to(ROOT)}")
        presentation = presentation_document
    if document["family"] == "marketplace" and presentation is None:
        raise SystemExit(f"marketplace connector presentation is required: {path.relative_to(ROOT)}")
    entries.append({
        "id": document["id"],
        "name": document["name"],
        "family": document["family"],
        "version": document["version"],
        "presentation": presentation,
        "capabilities": sorted(set(document["capabilities"])),
        "authKinds": sorted({requirement["kind"] for requirement in document["auth"]}),
        "oauthGrantType": next((requirement.get("oauth2", {}).get("grant_type") for requirement in document["auth"] if requirement["kind"] == "oauth2"), None),
    })
    manifest_documents.append(document)

# Category directories are an implementation detail. Keep the generated
# projections stable by connector id, as they were before providers acquired
# their category level.
order = sorted(range(len(entries)), key=lambda index: entries[index]["id"])
entries = [entries[index] for index in order]
manifest_documents = [manifest_documents[index] for index in order]

support_document = json.loads(RUNTIME_SUPPORT.read_text(encoding="utf-8"))
if support_document.get("schema_version") != 1 or not isinstance(support_document.get("connectors"), list):
    raise SystemExit("invalid built-in runtime support contract")
support_by_id = {}
valid_stages = {"ready", "separate_surface", "planned"}
valid_surfaces = {"integrations", "ai_providers", "finance", "social", "crm", "logistics", "marketplace", "classified", "edo", "government", "none"}
for support in support_document["connectors"]:
    connector_id = support.get("connector_id")
    if connector_id in support_by_id or support.get("stage") not in valid_stages or support.get("surface") not in valid_surfaces:
        raise SystemExit(f"invalid or duplicate runtime support entry: {connector_id}")
    capabilities = support.get("operational_capabilities")
    sync = support.get("sync")
    if not isinstance(capabilities, list) or capabilities != sorted(set(capabilities)) or not isinstance(sync, list):
        raise SystemExit(f"invalid runtime support capability/sync list: {connector_id}")
    support_by_id[connector_id] = support

manifest_by_id = {entry["id"]: entry for entry in entries}
if set(support_by_id) != set(manifest_by_id):
    missing = sorted(set(manifest_by_id) - set(support_by_id))
    extra = sorted(set(support_by_id) - set(manifest_by_id))
    raise SystemExit(f"runtime support/catalog mismatch: missing={missing} extra={extra}")
for connector_id, support in support_by_id.items():
    manifest_capabilities = set(manifest_by_id[connector_id]["capabilities"])
    if not set(support["operational_capabilities"]).issubset(manifest_capabilities):
        raise SystemExit(f"runtime support exceeds manifest capabilities: {connector_id}")
    if support["stage"] == "ready" and (support["surface"] != "integrations" or not support["operational_capabilities"] or not support["sync"]):
        raise SystemExit(f"ready connector requires integrations capability and sync evidence: {connector_id}")
    if support["stage"] == "separate_surface" and (support["surface"] == "integrations" or support["sync"]):
        raise SystemExit(f"separate-surface connector cannot use generic sync: {connector_id}")
    if support["stage"] == "separate_surface" and support["surface"] == "social" and not support["operational_capabilities"] and not support.get("health_only", False):
        raise SystemExit(f"social surface requires an executable capability: {connector_id}")
    text_limit = support.get("social_text_max_runes")
    if text_limit is not None and (support["stage"] != "separate_surface" or support["surface"] != "social" or "social.post.text" not in support["operational_capabilities"] or not isinstance(text_limit, int) or isinstance(text_limit, bool) or not 1 <= text_limit <= 50000):
        raise SystemExit(f"invalid social text limit: {connector_id}")
    if support["stage"] == "separate_surface" and support["surface"] == "social" and text_limit is None and not support.get("health_only", False):
        raise SystemExit(f"social text runtime requires an exact text limit: {connector_id}")
    if support["stage"] == "planned" and (support["surface"] != "none" or support["operational_capabilities"] or support["sync"] or "runtime_config_template" in support or text_limit is not None):
        raise SystemExit(f"planned connector must remain fail-closed: {connector_id}")
    if support.get("health_only", False) and (support["stage"] != "separate_surface" or support["operational_capabilities"] or support["sync"]):
        raise SystemExit(f"health-only connector must be a capability-free separate surface: {connector_id}")
    for sync_support in support["sync"]:
        if sync_support.get("entity_type") not in {"products", "prices", "inventory", "orders"} or not sync_support.get("directions") or any(direction not in {"inbound", "outbound"} for direction in sync_support.get("directions", [])) or sorted(set(sync_support.get("directions", []))) != sorted(sync_support.get("directions", [])):
            raise SystemExit(f"invalid runtime sync declaration: {connector_id}")

lines = [
    "// Code generated by scripts/generate-frontend-connector-catalog.py; DO NOT EDIT.\n",
    "export interface ConnectorPresentation {\n",
    "  readonly logo: string;\n",
    "  readonly surface: string;\n",
    "  readonly surfaceAlt: string;\n",
    "  readonly foreground: string;\n",
    "  readonly accent: string;\n",
    "}\n\n",
    "export interface ConnectorCatalogEntry {\n",
    "  readonly id: string;\n",
    "  readonly name: string;\n",
    "  readonly family: string;\n",
    "  readonly version: string;\n",
    "  readonly presentation?: ConnectorPresentation;\n",
    "  readonly capabilities: readonly string[];\n",
    "  readonly authKinds: readonly string[];\n",
    "  readonly oauthGrantType?: \"authorization_code\" | \"client_credentials\";\n",
    "  readonly runtime: ConnectorRuntimeSupport;\n",
    "}\n\n",
    "export interface ConnectorRuntimeSyncSupport {\n",
    "  readonly entityType: string;\n",
    "  readonly directions: readonly (\"inbound\" | \"outbound\")[];\n",
    "}\n\n",
    "export interface ConnectorRuntimeSupport {\n",
    "  readonly stage: \"ready\" | \"separate_surface\" | \"planned\";\n",
    "  readonly surface: \"integrations\" | \"ai_providers\" | \"finance\" | \"social\" | \"crm\" | \"logistics\" | \"marketplace\" | \"classified\" | \"edo\" | \"government\" | \"none\";\n",
    "  readonly operationalCapabilities: readonly string[];\n",
    "  readonly sync: readonly ConnectorRuntimeSyncSupport[];\n",
    "  readonly runtimeConfigTemplate?: Readonly<Record<string, unknown>>;\n",
    "  readonly socialTextMaxRunes?: number;\n",
    "  readonly healthOnly?: boolean;\n",
    "}\n\n",
    "export const connectorCatalog: readonly ConnectorCatalogEntry[] = [\n",
]
for entry in entries:
    support = support_by_id[entry["id"]]
    lines.extend([
        "  {\n",
        f"    id: {quoted(entry['id'])},\n",
        f"    name: {quoted(entry['name'])},\n",
        f"    family: {quoted(entry['family'])},\n",
        f"    version: {quoted(entry['version'])},\n",
    ])
    if entry["presentation"]:
        presentation = entry["presentation"]
        lines.extend([
            "    presentation: {\n",
            f"      logo: {quoted(presentation['logo'])},\n",
            f"      surface: {quoted(presentation['surface'])},\n",
            f"      surfaceAlt: {quoted(presentation['surface_alt'])},\n",
            f"      foreground: {quoted(presentation['foreground'])},\n",
            f"      accent: {quoted(presentation['accent'])},\n",
            "    },\n",
        ])
    lines.extend([
        f"    capabilities: [{', '.join(quoted(value) for value in entry['capabilities'])}],\n",
        f"    authKinds: [{', '.join(quoted(value) for value in entry['authKinds'])}],\n",
        *(([f"    oauthGrantType: {quoted(entry['oauthGrantType'])},\n"] if entry["oauthGrantType"] else [])),
        "    runtime: {\n",
        f"      stage: {quoted(support['stage'])},\n",
        f"      surface: {quoted(support['surface'])},\n",
        f"      operationalCapabilities: [{', '.join(quoted(value) for value in support['operational_capabilities'])}],\n",
        "      sync: [\n",
        *(f"        {{entityType: {quoted(value['entity_type'])}, directions: [{', '.join(quoted(direction) for direction in value['directions'])}]}},\n" for value in support["sync"]),
        "      ],\n",
        *(([f"      runtimeConfigTemplate: {json.dumps(support['runtime_config_template'], ensure_ascii=False, separators=(',', ':'))},\n"] if "runtime_config_template" in support else [])),
        *(([f"      socialTextMaxRunes: {support['social_text_max_runes']},\n"] if "social_text_max_runes" in support else [])),
        *((["      healthOnly: true,\n"] if support.get("health_only", False) else [])),
        "    },\n",
        "  },\n",
    ])
lines.append("] as const;\n")
rendered = "".join(lines)
catalog_json = json.dumps(manifest_documents, ensure_ascii=False, separators=(",", ":"))
go_rendered = f'''// Code generated by scripts/generate-frontend-connector-catalog.py; DO NOT EDIT.
package connectors

import (
\t"encoding/json"
\t"sync"
)

const generatedCatalogJSON = `{catalog_json}`

var generatedCatalog struct {{
\tsync.Once
\tmanifests []Manifest
\tbyID      map[string]Manifest
\terr       error
}}

func loadGeneratedCatalog() {{
\tgeneratedCatalog.err = json.Unmarshal([]byte(generatedCatalogJSON), &generatedCatalog.manifests)
\tgeneratedCatalog.byID = make(map[string]Manifest, len(generatedCatalog.manifests))
\tif generatedCatalog.err != nil {{
\t\treturn
\t}}
\tfor index, manifest := range generatedCatalog.manifests {{
\t\tif err := manifest.Validate(); err != nil {{
\t\t\tgeneratedCatalog.err = err
\t\t\treturn
\t\t}}
\t\tmanifest = manifest.Canonical()
\t\tgeneratedCatalog.manifests[index] = manifest
\t\tgeneratedCatalog.byID[manifest.ID] = manifest
\t}}
}}

// CatalogManifests returns the reviewed provider-neutral connector metadata.
func CatalogManifests() ([]Manifest, error) {{
\tgeneratedCatalog.Do(loadGeneratedCatalog)
\tif generatedCatalog.err != nil {{
\t\treturn nil, generatedCatalog.err
\t}}
\treturn append([]Manifest(nil), generatedCatalog.manifests...), nil
}}

// CatalogManifest resolves one canonical manifest without importing providers.
func CatalogManifest(id string) (Manifest, error) {{
\tgeneratedCatalog.Do(loadGeneratedCatalog)
\tif generatedCatalog.err != nil {{
\t\treturn Manifest{{}}, generatedCatalog.err
\t}}
\tmanifest, ok := generatedCatalog.byID[id]
\tif !ok {{
\t\treturn Manifest{{}}, ErrConnectorNotFound
\t}}
\treturn manifest, nil
}}
'''
go_support_lines = [
    "// Code generated by scripts/generate-frontend-connector-catalog.py; DO NOT EDIT.\n",
    "package builtinruntime\n\n",
    "var generatedRuntimeSupport = map[string]Support{\n",
]
for connector_id in sorted(support_by_id):
    support = support_by_id[connector_id]
    map_key = f"{quoted(connector_id)}:"
    capabilities = ", ".join(quoted(value) for value in support["operational_capabilities"])
    sync_values = []
    for value in support["sync"]:
        directions = ", ".join(quoted(direction) for direction in value["directions"])
        sync_values.append(f'{{EntityType: {quoted(value["entity_type"])}, Directions: []string{{{directions}}}}}')
    go_support_lines.append(
        f'\t{map_key:<21}{{ConnectorID: {quoted(connector_id)}, Stage: SupportStage({quoted(support["stage"])}), Surface: {quoted(support["surface"])}, '
        f'OperationalCapabilities: []string{{{capabilities}}}, Sync: []SyncSupport{{{", ".join(sync_values)}}}, '
        f'RuntimeConfigRequired: {str("runtime_config_template" in support).lower()}, SocialTextMaxRunes: {support.get("social_text_max_runes", 0)}, HealthOnly: {str(support.get("health_only", False)).lower()}}},\n'
    )
go_support_lines.append("}\n")
go_support_rendered = "".join(go_support_lines)
if "--check" in sys.argv[1:]:
    if not OUTPUT.is_file() or OUTPUT.read_text(encoding="utf-8") != rendered:
        raise SystemExit("frontend connector catalog is stale; run scripts/generate-frontend-connector-catalog.py")
    if not GO_OUTPUT.is_file() or GO_OUTPUT.read_text(encoding="utf-8") != go_rendered:
        raise SystemExit("Go connector catalog is stale; run scripts/generate-frontend-connector-catalog.py")
    if not GO_SUPPORT_OUTPUT.is_file() or GO_SUPPORT_OUTPUT.read_text(encoding="utf-8") != go_support_rendered:
        raise SystemExit("Go runtime support catalog is stale; run scripts/generate-frontend-connector-catalog.py")
    print(f"Frontend connector catalog: PASS ({len(entries)} connectors)")
else:
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(rendered, encoding="utf-8")
    GO_OUTPUT.write_text(go_rendered, encoding="utf-8")
    GO_SUPPORT_OUTPUT.write_text(go_support_rendered, encoding="utf-8")
    print(f"Generated {OUTPUT.relative_to(ROOT)} with {len(entries)} connectors")
