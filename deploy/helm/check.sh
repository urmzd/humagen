#!/usr/bin/env sh
# Checks the chart: lints it, renders it with each values file in ci/ (and the
# Flux override), validates the manifests with kubeconform when it is on PATH,
# and runs `humagen config check` on the humagen.yaml each render puts in its
# ConfigMap, so the chart cannot render a spec humagen refuses.
#
#   deploy/helm/check.sh            # from the repository root; make chart-check
#
# HELM, KUBECONFORM and HUMAGEN override the commands used.
set -eu

root=$(cd "$(dirname "$0")/../.." && pwd)
chart="$root/deploy/helm/humagen"
helm=${HELM:-helm}
kubeconform=${KUBECONFORM:-kubeconform}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

if [ -n "${HUMAGEN:-}" ]; then
	humagen=$HUMAGEN
else
	(cd "$root" && go build -o "$work/humagen" ./cmd/humagen)
	humagen="$work/humagen"
fi

"$helm" lint --strict "$chart" -f "$chart/ci/default-values.yaml"

for values in "$chart"/ci/*.yaml "$root/deploy/flux/app/values.yaml"; do
	name=$(basename "$(dirname "$values")")-$(basename "$values" .yaml)
	out="$work/$name.yaml"
	"$helm" lint --strict "$chart" -f "$values" >/dev/null
	"$helm" template humagen "$chart" -f "$values" >"$out"
	if command -v "$kubeconform" >/dev/null 2>&1; then
		"$kubeconform" -strict -summary "$out"
	else
		echo "kubeconform not found: skipping schema checks for $name"
	fi
	# The ConfigMap's humagen.yaml, as the pod would read it.
	python3 -I - "$out" "$work/$name.humagen.yaml" <<'EOF'
import sys, yaml
for doc in yaml.safe_load_all(open(sys.argv[1])):
    if doc and doc.get("kind") == "ConfigMap" and "humagen.yaml" in doc.get("data", {}):
        open(sys.argv[2], "w").write(doc["data"]["humagen.yaml"])
        break
else:
    sys.exit("no ConfigMap with humagen.yaml in " + sys.argv[1])
EOF
	"$humagen" config check -f "$work/$name.humagen.yaml"
done

# What the chart must refuse.
refuse() {
	if "$helm" template humagen "$chart" -f "$chart/ci/default-values.yaml" "$@" >/dev/null 2>&1; then
		echo "rendered, but should have refused: $*"
		exit 1
	fi
}
refuse --set replicaCount=2
refuse --set system.spec.auth.kind=dev
refuse --set system.spec.auth.kind=tailscale
refuse --set system.spec.auth.oidc.issuer=
echo "chart ok"
