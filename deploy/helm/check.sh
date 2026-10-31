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

for values in "$chart"/ci/*.yaml "$root/deploy/flux/app/values.yaml" "$root/deploy/tilt/values.yaml"; do
	name=$(basename "$(dirname "$values")")-$(basename "$values" .yaml)
	out="$work/$name.yaml"
	"$helm" lint --strict "$chart" -f "$values" >/dev/null
	"$helm" template humagen "$chart" -f "$values" >"$out"
	if command -v "$kubeconform" >/dev/null 2>&1; then
		# KEDA's ScaledObject and TriggerAuthentication are CRDs kubeconform
		# has no schema for.
		"$kubeconform" -strict -summary -skip ScaledObject,TriggerAuthentication "$out"
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
# tailscale trusts headers from the pod's loopback: only with the Tailscale
# proxy container in the pod (ci/tailscale-values.yaml), never behind an
# Ingress, never under development.loopback, and the proxy only for tailscale.
refuse --set system.spec.auth.kind=tailscale
refuse -f "$chart/ci/tailscale-values.yaml" --set ingress.enabled=true
refuse -f "$chart/ci/tailscale-values.yaml" --set development.loopback=true
refuse --set tailscale.proxy.enabled=true --set tailscale.proxy.image.repository=x --set tailscale.proxy.image.tag=1
# development.loopback lets dev sign-in through only on the pod's loopback,
# never tailscale (no proxy in development), and never behind an Ingress.
refuse -f "$root/deploy/tilt/values.yaml" --set system.spec.auth.kind=tailscale
refuse -f "$root/deploy/tilt/values.yaml" --set ingress.enabled=true
# The grace period must cover the preStop sleep, the drain and the HTTP
# shutdown, and the durations must parse.
refuse --set terminationGracePeriodSeconds=40
refuse --set shutdown.drainGrace=2m
refuse --set shutdown.drainGrace=25
refuse --set shutdown.httpTimeout=500ms
# The server's autoscaler stays at one replica while the stores are
# single-writer.
refuse --set autoscaling.enabled=true --set autoscaling.maxReplicas=2
refuse --set autoscaling.enabled=true --set autoscaling.minReplicas=2
# The worker needs Postgres stores and a bucket; KEDA needs the Secret.
refuse --set worker.enabled=true
refuse -f "$chart/ci/scale-values.yaml" --set system.spec.blobs.kind=disk --set system.spec.blobs.s3=null
refuse -f "$chart/ci/scale-values.yaml" --set existingSecret=
# What the chart renders for each blobs kind and stores kind.
python3 -I - "$chart" "$helm" <<'EOF'
import subprocess, sys, yaml
chart, helm = sys.argv[1:]
def render(*args):
    out = subprocess.check_output([helm, "template", "humagen", chart, "-f", chart + "/ci/default-values.yaml", *args], text=True)
    return [d for d in yaml.safe_load_all(out) if d]
def kinds(docs):
    return {d["kind"] for d in docs}
def data_volume(docs, name="humagen"):
    dep = next(d for d in docs if d["kind"] == "Deployment" and d["metadata"]["name"] == name)
    return next(v for v in dep["spec"]["template"]["spec"]["volumes"] if v["name"] == "data")
def system(docs):
    cm = next(d for d in docs if d["kind"] == "ConfigMap")
    return next(yaml.safe_load_all(cm["data"]["humagen.yaml"]))
docs = render()
assert "PersistentVolumeClaim" in kinds(docs), "disk blobs: no claim"
assert "persistentVolumeClaim" in data_volume(docs), "disk blobs: data is not the claim"
assert system(docs)["spec"]["bus"] == {"kind": "postgres"}, "postgres stores: bus is not postgres"
dep = next(d for d in docs if d["kind"] == "Deployment")
c = dep["spec"]["template"]["spec"]["containers"][0]
assert c["readinessProbe"]["httpGet"]["path"] == "/readyz", "readiness is not /readyz"
assert c["livenessProbe"]["httpGet"]["path"] == "/healthz", "liveness is not /healthz"
assert dep["spec"]["template"]["spec"]["terminationGracePeriodSeconds"] > 40
docs = render("--set", "system.spec.blobs.kind=s3", "--set", "system.spec.blobs.s3.bucket=b")
assert "PersistentVolumeClaim" not in kinds(docs), "s3 blobs: a claim was made"
assert data_volume(docs) == {"name": "data", "emptyDir": {}}, "s3 blobs: data is not an emptyDir"
docs = render("--set", "system.spec.blobs.kind=s3", "--set", "system.spec.blobs.s3.bucket=b", "--set", "persistence.existingClaim=mine")
assert data_volume(docs)["persistentVolumeClaim"]["claimName"] == "mine", "existingClaim ignored"
docs = render("--set", "system.spec.stores.kind=memory", "--set", "system.spec.stores.postgres=null")
assert "bus" not in system(docs)["spec"], "memory stores: a bus was set"
docs = render("-f", chart + "/ci/scale-values.yaml")
dep = next(d for d in docs if d["kind"] == "Deployment" and d["metadata"]["name"] == "humagen")
assert "replicas" not in dep["spec"], "autoscaling: the Deployment sets replicas"
worker = next(d for d in docs if d["kind"] == "Deployment" and d["metadata"]["name"] == "humagen-worker")
assert worker["spec"]["template"]["spec"]["containers"][0]["args"][:2] == ["worker", "-f=/etc/warp/humagen.yaml"]
assert "replicas" not in worker["spec"], "keda: the worker sets replicas"
for kind in ("HorizontalPodAutoscaler", "ScaledObject", "TriggerAuthentication", "PodDisruptionBudget"):
    assert kind in kinds(docs), kind
svc = next(d for d in docs if d["kind"] == "Service")
assert all(not k.endswith("worker") for k in svc["spec"]["selector"].values()), "the Service selects workers"
docs = render("-f", chart + "/ci/tailscale-values.yaml")
dep = next(d for d in docs if d["kind"] == "Deployment")
names = [c["name"] for c in dep["spec"]["template"]["spec"]["containers"]]
assert "tailscale-proxy" in names, "tailscale: no proxy container"
c = dep["spec"]["template"]["spec"]["containers"][0]
assert "-addr=127.0.0.1:8080" in c["args"], "tailscale: humagen is not on loopback"
assert c["readinessProbe"]["exec"]["command"][-1] == "http://127.0.0.1:8080/readyz", "tailscale: readiness path"
svc = next(d for d in docs if d["kind"] == "Service")
assert [p["name"] for p in svc["spec"]["ports"]] == ["webhooks"], "tailscale: the Service exposes humagen"
print("chart renders ok")
EOF
refuse --set system.spec.auth.oidc.issuer=
refuse --set image.tag=0.20.2
refuse --set image.tag=v0.9.0
"$helm" template humagen "$chart" -f "$chart/ci/default-values.yaml" --set image.tag=sha-abc123 >/dev/null
"$helm" template humagen "$chart" -f "$chart/ci/default-values.yaml" --set image.tag=v0.21.0 >/dev/null
# A release the warp chart installed keeps its names with nameOverride: warp:
# the same Deployment, Service, PVC and selector as before the rename.
"$helm" template warp "$chart" -f "$chart/ci/default-values.yaml" --set nameOverride=warp >"$work/upgrade.yaml"
python3 -I - "$work/upgrade.yaml" <<'EOF'
import sys, yaml
seen = {}
for doc in yaml.safe_load_all(open(sys.argv[1])):
    if doc and doc["kind"] in ("Deployment", "Service", "PersistentVolumeClaim", "ConfigMap"):
        seen[doc["kind"]] = doc
for kind in ("Deployment", "Service", "PersistentVolumeClaim", "ConfigMap"):
    if seen.get(kind, {}).get("metadata", {}).get("name") != "warp":
        sys.exit("upgrade render: %s is not named warp" % kind)
for kind, sel in (("Deployment", seen["Deployment"]["spec"]["selector"]["matchLabels"]), ("Service", seen["Service"]["spec"]["selector"])):
    if sel.get("app.kubernetes.io/name") != "warp":
        sys.exit("upgrade render: the %s's selector changed" % kind)
EOF
# A new install is named for humagen.
"$helm" template humagen "$chart" -f "$chart/ci/default-values.yaml" | grep -q '^  name: humagen$' || {
	echo "a new install is not named humagen"
	exit 1
}
python3 -I "$root/deploy/helm/reused_values_test.py" "$chart" "$helm" "$humagen"
echo "chart ok"
