"""Exercise backend values after Helm's coalescing phase, as reuse-values does."""

import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

import yaml

chart, helm, humagen = sys.argv[1:]
url = {"url": {"ref": "env:HUMAGEN_POSTGRES_URL"}}
cases = [
    ("memory", {"kind": "memory", "postgres": None}, True),
    ("postgres", {"kind": "postgres", "postgres": url, "memory": None}, True),
    ("removed custom backend", {"kind": "memory", "custom": None}, True),
    ("configured inactive backend", {"kind": "memory", "postgres": url}, False),
    ("false inactive backend", {"kind": "memory", "postgres": False}, False),
    ("empty inactive backend", {"kind": "memory", "postgres": {}}, False),
    ("missing postgres configuration", {"kind": "postgres", "postgres": None}, False),
]
with tempfile.TemporaryDirectory() as directory:
    work = Path(directory)
    copied = work / "chart"
    shutil.copytree(chart, copied)
    # Helm renders templates in reverse filename order. Inject retained values
    # before rendering the ConfigMap; ordinary template flags coalesce null away.
    fixture = copied / "templates" / "zz-reused-values.yaml"
    for name, stores, valid in cases:
        fixture.write_text(
            '{{- $_ := set .Values.system.spec "stores" (fromJson `'
            + json.dumps(stores)
            + '`) -}}\n'
            '{{- $_ := set .Values.system.spec.execution "max_depth" 0 -}}\n'
            '{{- $_ := set (index .Values.platforms 0).spec "workspace" '
            '(fromJson `{"release_notes":false,"skills":[]}`) -}}\n'
        )
        rendered = subprocess.check_output(
            [helm, "template", "humagen", str(copied), "-f",
             str(Path(chart) / "ci/default-values.yaml")], text=True
        )
        configmap = next(doc for doc in yaml.safe_load_all(rendered)
                         if doc and doc["kind"] == "ConfigMap")
        spec = configmap["data"]["humagen.yaml"]
        path = work / "humagen.yaml"
        path.write_text(spec)
        checked = subprocess.run([humagen, "config", "check", "-f", str(path)],
                                 capture_output=True, text=True)
        assert (checked.returncode == 0) == valid, (
            f"{name}: unexpected config check result:\n{checked.stdout}{checked.stderr}"
        )
        if valid:
            system, platform = list(yaml.safe_load_all(spec))
            assert system["spec"]["stores"] == {
                key: value for key, value in stores.items() if value is not None
            }, name
            assert system["spec"]["execution"]["max_depth"] == 0, name
            assert platform["spec"]["workspace"] == {"release_notes": False, "skills": []}, name
        print(f"reused values: {name} passed")
