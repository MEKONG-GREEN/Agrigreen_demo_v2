#!/usr/bin/env bash
# Actual-image regression control for #287. Never use a local Docker daemon.
set -euo pipefail
if [[ ${GITHUB_ACTIONS:-} != true || ${RUNNER_OS:-} != Linux ||
      ${GITHUB_REPOSITORY:-} != MEKONG-GREEN/Agrigreen_demo_v2 ||
      ${GITHUB_REF:-} != refs/heads/codex/optimizer287zlib ]]; then
  echo 'This canary runs only in its dedicated GitHub Actions branch job.' >&2
  exit 2
fi
[[ $(git rev-parse HEAD) == "$GITHUB_SHA" && -z $(git status --porcelain) ]]
[[ $(trivy --version | head -1) == 'Version: 0.70.0' ]]
out=${1:?Pass a proof directory under RUNNER_TEMP}
mkdir -p "$out"
out=$(realpath "$out")
[[ "$out" == "$(realpath "$RUNNER_TEMP")/"* ]]
base='python:3.12-slim-trixie@sha256:4840d514c583a4d3d8cad3940e68dee6f0f6f3635c12fcd0a3d74c21a5b93a54'
baseline=8d11586a402c70b4b96935b4b2a5becfd6bc332c
floor='1:1.3.dfsg+really1.3.1-1+deb13u1'
old='1:1.3.dfsg+really1.3.1-1+b1'
export out base baseline floor old
git show "$baseline:optimizer/Dockerfile" > "$out/original.input"
cp optimizer/Dockerfile "$out/candidate.input"
python3 - <<'PY'
import os
from pathlib import Path
p = Path(os.environ['out'])
old = b'FROM python:3.12-slim-trixie AS base'
new = ('FROM ' + os.environ['base'] + ' AS base').encode()
for source, name in [('original.input', 'original'), ('candidate.input', 'fixed')]:
    data = (p / source).read_bytes()
    assert data.count(old) == 1
    (p / (name + '.Dockerfile')).write_bytes(data.replace(old, new))
PY
# Change only the upgrade and its three-line guard in an artifact copy, using UTF-8-safe sed.
sed -e 's/--only-upgrade apt libapt-pkg7.0 zlib1g;/--only-upgrade apt libapt-pkg7.0;/' \
    -e '/^    zlib_version=/d' \
    -e '/^    dpkg --compare-versions "\$zlib_version"/d' \
    -e '/^    echo "zlib1g=\$zlib_version"/d' \
    "$out/fixed.Dockerfile" > "$out/mutant.Dockerfile"
cp "$out/fixed.Dockerfile" "$out/restored.Dockerfile"
cmp "$out/fixed.Dockerfile" "$out/restored.Dockerfile"
python3 - <<'PY'
import hashlib, json, os
from pathlib import Path
p = Path(os.environ['out'])
fixed = (p / 'fixed.Dockerfile').read_bytes()
lines = fixed.splitlines(keepends=True)
guard = [line for line in lines if line.startswith((b'    zlib_version=', b'    dpkg --compare-versions "$zlib_version"', b'    echo "zlib1g=$zlib_version"'))]
assert len(guard) == 3
expected = fixed.replace(b'--only-upgrade apt libapt-pkg7.0 zlib1g;', b'--only-upgrade apt libapt-pkg7.0;')
for line in guard:
    expected = expected.replace(line, b'')
assert expected == (p / 'mutant.Dockerfile').read_bytes() and expected != fixed
proof = {'source_sha': os.environ['GITHUB_SHA'], 'baseline_sha': os.environ['baseline'],
         'base': os.environ['base'], 'floor': os.environ['floor'], 'files': {}}
for name in ['original.input', 'candidate.input', 'original.Dockerfile', 'fixed.Dockerfile', 'mutant.Dockerfile', 'restored.Dockerfile']:
    proof['files'][name] = hashlib.sha256((p / name).read_bytes()).hexdigest()
(p / 'inputs.json').write_text(json.dumps(proof, indent=2) + '\n')
PY
docker pull --platform linux/amd64 "$base" > "$out/base-pull.log" 2>&1
docker image inspect --format '{{.Id}} {{json .RepoDigests}}' "$base" > "$out/base-digests.txt"
trivy --version > "$out/trivy-version.txt"
# A single vulnerability database snapshot serves all four controls.
trivy image --download-db-only --cache-dir "$out/trivy-cache" > "$out/trivy-db.log" 2>&1
for variant in original fixed mutant restored; do
  image="agrigreen-optimizer-zlib:${GITHUB_SHA:0:12}-$variant"
  build_code=0
  docker build --load --pull --no-cache --platform linux/amd64 \
    --build-context optcore=lib/optcore -f "$out/$variant.Dockerfile" \
    -t "$image" optimizer > "$out/$variant-build.log" 2>&1 || build_code=$?
  printf '%s\n' "$build_code" > "$out/$variant-build-exit.txt"
  [[ $build_code == 0 ]]
  docker image inspect --format '{{.Id}}' "$image" > "$out/$variant-image-id.txt"
  docker run --rm --network none --entrypoint sh "$image" -ec '
    for p in apt libapt-pkg7.0; do
      v=$(dpkg-query -W -f="\${Version}" "$p")
      dpkg --compare-versions "$v" ge 3.0.4
    done
    dpkg-query -W -f="\${Package}\t\${Version}\n" apt libapt-pkg7.0 zlib1g
    sha256sum /usr/lib/x86_64-linux-gnu/libz.so.1.3.1
    test "$(id -u)" = 10001
  ' > "$out/$variant-packages.txt"
  # Only scanner exit 1 with valid zlib findings counts as RED; errors fail the job.
  code=0
  trivy image --cache-dir "$out/trivy-cache" --skip-db-update --timeout 10m \
    --scanners vuln --severity CRITICAL,HIGH --ignore-unfixed --exit-code 1 \
    --format json --output "$out/$variant-trivy.json" "$image" \
    > "$out/$variant-trivy.log" 2>&1 || code=$?
  printf '%s\n' "$code" > "$out/$variant-trivy-exit.txt"
  VARIANT=$variant python3 - <<'PY'
import json, os, subprocess
from pathlib import Path
p = Path(os.environ['out']); name = os.environ['VARIANT']
report = json.loads((p / (name + '-trivy.json')).read_text())
assert report['SchemaVersion'] == 2 and report['ArtifactType'] == 'container_image'
vulns = [v for result in report.get('Results', []) for v in result.get('Vulnerabilities') or []
         if v['Severity'] in ('HIGH', 'CRITICAL')]
code = int((p / (name + '-trivy-exit.txt')).read_text())
rows = (p / (name + '-packages.txt')).read_text().splitlines()
packages = dict(line.split('\t', 1) for line in rows if '\t' in line)
version = packages['zlib1g']
if name in ('original', 'mutant'):
    assert code == 1, (name, code)
    assert version == os.environ['old'], (name, version)
    assert {(v['PkgName'], v['VulnerabilityID']) for v in vulns} == {
        ('zlib1g', 'CVE-2026-76844'), ('zlib1g', 'CVE-2026-85091')}, (name, vulns)
else:
    assert code == 0 and not vulns, (name, code, vulns)
    subprocess.run(['dpkg', '--compare-versions', version, 'ge', os.environ['floor']], check=True)
    # This reproducible fixture verifies the real vendor library, not edited version/SBOM metadata.
    assert version == os.environ['floor'], ('Vendor fixture changed; verify the newer signed package', version)
    assert rows[-1].split()[0] == '6c13931760210e8b391b63a337dfe4071346c4ad6e3827694edb7b6989bb16dd'
print(name, 'RED' if code else 'PASS', 'zlib1g=' + version)
PY
done
python3 - <<'PY'
import hashlib, json, os
from pathlib import Path
p = Path(os.environ['out'])
def digest(name): return hashlib.sha256((p / name).read_bytes()).hexdigest()
assert (p / 'fixed.Dockerfile').read_bytes() == (p / 'restored.Dockerfile').read_bytes()
assert (p / 'fixed-packages.txt').read_bytes() == (p / 'restored-packages.txt').read_bytes()
assert (p / 'original-packages.txt').read_bytes() == (p / 'mutant-packages.txt').read_bytes()
assert (p / 'original-packages.txt').read_bytes() != (p / 'fixed-packages.txt').read_bytes()
results = {name: {'scan_exit': int((p / (name + '-trivy-exit.txt')).read_text()),
                  'build_exit': int((p / (name + '-build-exit.txt')).read_text()),
                  'image_id': (p / (name + '-image-id.txt')).read_text().strip(),
                  'library_and_packages_sha256': digest(name + '-packages.txt'),
                  'report_sha256': digest(name + '-trivy.json')}
           for name in ['original', 'fixed', 'mutant', 'restored']}
(p / 'result.json').write_text(json.dumps({'source_sha': os.environ['GITHUB_SHA'], 'results': results,
    'exact_restore': True, 'production_ci_still_required': True}, indent=2) + '\n')
PY
# The database is public but large; retain its metadata and digest, not the cache binary.
find "$out/trivy-cache" -type f -name metadata.json -exec cp '{}' "$out/trivy-db-metadata.json" \;
find "$out/trivy-cache" -type f -name trivy.db -exec sha256sum '{}' \; > "$out/trivy-db-sha256.txt"
[[ -z $(git status --porcelain) ]]
