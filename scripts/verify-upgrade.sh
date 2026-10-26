#!/usr/bin/env bash
# Run a real pre-rename chart and binary, then exercise both documented upgrades.
# Requires kind, kubectl, helm, flux >= 2.9, Docker, Python 3, yq, and two locally built images.
# OLD_CHECKOUT must be a checkout of v0.20.2; NEW_IMAGE must contain Humagen >= 0.21.
set -euo pipefail
: "${OLD_CHECKOUT:?Set OLD_CHECKOUT to a checkout of v0.20.2}"
: "${OLD_IMAGE:?Set OLD_IMAGE to the pre-rename runtime image}"
: "${NEW_IMAGE:?Set NEW_IMAGE to the current root Dockerfile image}"
ROOT=$(cd "$(dirname "$0")/.." && pwd)
CLUSTER=${VERIFY_CLUSTER:-humagen-rename-verification}
ARTIFACTS=${VERIFY_ARTIFACTS:-$(mktemp -d /tmp/humagen-upgrade.XXXXXX)}
mkdir -p "$ARTIFACTS"
export KUBECONFIG="$ARTIFACTS/kubeconfig"
NEW_COMMIT=${NEW_COMMIT:-$(git -C "$ROOT" rev-parse origin/main)}
cluster_created=0
cleanup() {
  if [[ $cluster_created == 1 && ${KEEP_CLUSTER:-0} != 1 ]]; then
    kind delete cluster --name "$CLUSTER"
  fi
}
trap cleanup EXIT
if kind get clusters | grep -Fxq "$CLUSTER"; then
  kind get kubeconfig --name "$CLUSTER" > "$KUBECONFIG"
else
  cluster_created=1
  kind create cluster --name "$CLUSTER" --kubeconfig "$KUBECONFIG" --wait 120s
fi
POSTGRES_IMAGE=${POSTGRES_IMAGE:-postgres:16-alpine}
platform=$(docker image inspect "$NEW_IMAGE" --format '{{.Os}}/{{.Architecture}}')
for node in $(kind get nodes --name "$CLUSTER"); do
  docker image save "$OLD_IMAGE" "$NEW_IMAGE" "$POSTGRES_IMAGE" | \
    docker exec --privileged -i "$node" ctr --namespace=k8s.io images import \
      --platform "$platform" --digests -
done
cat > "$ARTIFACTS/old-values.yaml" <<YAML
image:
  repository: ${OLD_IMAGE%:*}
  tag: ${OLD_IMAGE##*:}
  pullPolicy: Never
existingSecret: warp-env
system:
  spec:
    server:
      public_url: https://humagen.example.invalid
    auth:
      oidc:
        issuer: https://accounts.google.com
        client_id: verification
        audience: verification
persistence:
  size: 128Mi
YAML
# Check the exact old spec and the new chart rendered with the old values first.
helm template warp "$OLD_CHECKOUT/deploy/helm/warp" -f "$ARTIFACTS/old-values.yaml" \
  --show-only templates/configmap.yaml > "$ARTIFACTS/old-configmap.yaml"
yq -r '.data["warp.yaml"]' "$ARTIFACTS/old-configmap.yaml" > "$ARTIFACTS/old-spec.yaml"
helm template warp "$ROOT/deploy/helm/humagen" \
  -f "$OLD_CHECKOUT/deploy/helm/warp/values.yaml" -f "$ARTIFACTS/old-values.yaml" \
  --set nameOverride=warp --set "image.repository=${NEW_IMAGE%:*}" \
  --set "image.tag=${NEW_IMAGE##*:}" --show-only templates/configmap.yaml \
  > "$ARTIFACTS/new-configmap.yaml"
yq -r '.data["humagen.yaml"]' "$ARTIFACTS/new-configmap.yaml" > "$ARTIFACTS/new-spec.yaml"
for phase in old new; do
  image=$OLD_IMAGE
  command=/usr/local/bin/warp
  if [[ $phase == new ]]; then image=$NEW_IMAGE; command=/usr/local/bin/humagen; fi
  docker run --rm -i -e WARP_OIDC_CLIENT_SECRET=verification-only \
    -e WARP_POSTGRES_URL=postgres://verification:verification@verification-postgres:5432/humagen?sslmode=disable \
    --entrypoint /bin/sh "$image" -c 'cat > /tmp/spec.yaml; exec "$@"' \
    preflight "$command" config check -f /tmp/spec.yaml < "$ARTIFACTS/$phase-spec.yaml" \
    > "$ARTIFACTS/$phase-config-check.log" 2>&1
  echo "$phase runtime: rendered spec passed config check"
done
prepare_namespace() {
  kubectl create namespace "$1"
  kubectl -n "$1" create secret generic warp-env \
    --from-literal=WARP_OIDC_CLIENT_SECRET=verification-only \
    --from-literal=WARP_POSTGRES_URL=postgres://verification:verification@verification-postgres:5432/humagen?sslmode=disable \
    --from-literal=preserved=before-rename
  kubectl -n "$1" apply -f - <<YAML
apiVersion: v1
kind: Service
metadata:
  name: verification-postgres
spec:
  selector: {app: verification-postgres}
  ports: [{port: 5432, targetPort: 5432}]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: verification-postgres
spec:
  replicas: 1
  selector:
    matchLabels: {app: verification-postgres}
  template:
    metadata:
      labels: {app: verification-postgres}
    spec:
      containers:
        - name: postgres
          image: $POSTGRES_IMAGE
          imagePullPolicy: Never
          env:
            - {name: POSTGRES_USER, value: verification}
            - {name: POSTGRES_PASSWORD, value: verification}
            - {name: POSTGRES_DB, value: humagen}
          ports: [{containerPort: 5432}]
          readinessProbe:
            exec: {command: [pg_isready, -U, verification, -d, humagen]}
            periodSeconds: 2
            timeoutSeconds: 2
          resources:
            requests: {cpu: 100m, memory: 128Mi}
            limits: {memory: 512Mi}
          volumeMounts: [{name: data, mountPath: /var/lib/postgresql/data}]
      volumes: [{name: data, emptyDir: {}}]
YAML
  kubectl -n "$1" rollout status deployment/verification-postgres --timeout=120s
}
snapshot() {
  local namespace=$1 phase=$2
  kubectl -n "$namespace" get deployment warp -o json > "$ARTIFACTS/$namespace-$phase-deployment.json"
  kubectl -n "$namespace" get pvc warp -o json > "$ARTIFACTS/$namespace-$phase-pvc.json"
  kubectl -n "$namespace" get secret warp-env -o json > "$ARTIFACTS/$namespace-$phase-secret.json"
  kubectl -n "$namespace" get pods -l app.kubernetes.io/instance=warp -o json > "$ARTIFACTS/$namespace-$phase-pods.json"
  kubectl -n "$namespace" logs deployment/warp -c warp > "$ARTIFACTS/$namespace-$phase.log"
}
seed_marker() {
  kubectl -n "$1" exec deployment/warp -c warp -- sh -c 'printf before-rename > /var/lib/warp/upgrade-marker'
}
verify_kept() {
  local namespace=$1
  kubectl -n "$namespace" rollout status deployment/warp --timeout=180s
  snapshot "$namespace" after
  python3 - "$ARTIFACTS" "$namespace" <<'PY'
import json, pathlib, sys
path, namespace = pathlib.Path(sys.argv[1]), sys.argv[2]
for kind in ('deployment', 'pvc', 'secret'):
    before = json.loads((path / f'{namespace}-before-{kind}.json').read_text())
    after = json.loads((path / f'{namespace}-after-{kind}.json').read_text())
    assert before['metadata']['uid'] == after['metadata']['uid'], f'{namespace}: {kind} was replaced'
    if kind == 'secret':
        assert before['data'] == after['data'], f'{namespace}: secret data changed'
    print(f'{namespace}: {kind} UID preserved: {after["metadata"]["uid"]}')
pods = json.loads((path / f'{namespace}-after-pods.json').read_text())['items']
assert len(pods) == 1, f'{namespace}: expected one pod'
assert pods[0]['status']['phase'] == 'Running'
assert all(c['ready'] for c in pods[0]['status']['containerStatuses'])
assert pods[0]['spec']['containers'][0]['name'] == 'warp'
print(f'{namespace}: pod ready; container name warp preserved')
PY
  [[ $(kubectl -n "$namespace" exec deployment/warp -c warp -- cat /var/lib/warp/upgrade-marker) == before-rename ]]
  kubectl -n "$namespace" exec deployment/warp -c warp -- wget -qO- http://127.0.0.1:8080/healthz
  kubectl -n "$namespace" logs deployment/warp -c warp > "$ARTIFACTS/$namespace-after.log"
  echo "$namespace: volume data and /healthz preserved"
}
prepare_namespace warp-helm
helm install warp "$OLD_CHECKOUT/deploy/helm/warp" -n warp-helm \
  -f "$ARTIFACTS/old-values.yaml" --wait --timeout 180s
seed_marker warp-helm
snapshot warp-helm before
helm upgrade warp "$ROOT/deploy/helm/humagen" -n warp-helm --reuse-values \
  --set nameOverride=warp --set "image.repository=${NEW_IMAGE%:*}" \
  --set "image.tag=${NEW_IMAGE##*:}" --wait --atomic --timeout 180s
verify_kept warp-helm


# Reuse the null tombstone from a healthy memory release without new overrides.
cat > "$ARTIFACTS/memory-values.yaml" <<YAML
system:
  spec:
    stores:
      kind: memory
      postgres: null
YAML
prepare_namespace warp-helm-memory
helm install warp "$OLD_CHECKOUT/deploy/helm/warp" -n warp-helm-memory \
  -f "$ARTIFACTS/old-values.yaml" -f "$ARTIFACTS/memory-values.yaml" --wait --timeout 180s
seed_marker warp-helm-memory
snapshot warp-helm-memory before
helm upgrade warp "$ROOT/deploy/helm/humagen" -n warp-helm-memory --reuse-values \
  --set nameOverride=warp --set "image.repository=${NEW_IMAGE%:*}" \
  --set "image.tag=${NEW_IMAGE##*:}" --wait --atomic --timeout 180s
verify_kept warp-helm-memory
# Allow the local chart regression to run before its branch is on the Flux source.
if [[ ${VERIFY_FLUX:-1} == 0 ]]; then
  echo "Postgres and memory Helm upgrades passed. Evidence: $ARTIFACTS"
  exit 0
fi

flux install --components=source-controller,helm-controller --timeout=5m
prepare_namespace warp-flux
kubectl apply -f - <<YAML
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: humagen
  namespace: flux-system
spec:
  interval: 1m
  url: https://github.com/urmzd/humagen
  ref:
    tag: v0.20.2
YAML
# Helm's YAML parser provides the values as JSON without adding a Python YAML dependency.
helm template warp "$OLD_CHECKOUT/deploy/helm/warp" -f "$ARTIFACTS/old-values.yaml" >/dev/null
python3 - "$ARTIFACTS/old-values.yaml" "$OLD_IMAGE" <<'PY' > "$ARTIFACTS/helmrelease.json"
import json,sys
image=sys.argv[2]; repository,tag=image.rsplit(':',1)
print(json.dumps({'apiVersion':'helm.toolkit.fluxcd.io/v2','kind':'HelmRelease',
 'metadata':{'name':'warp','namespace':'warp-flux'},
 'spec':{'interval':'1m','releaseName':'warp','chart':{'spec':{
 'chart':'./deploy/helm/warp','reconcileStrategy':'Revision',
 'sourceRef':{'kind':'GitRepository','name':'humagen','namespace':'flux-system'}}},
 'values':{'image':{'repository':repository,'tag':tag,'pullPolicy':'Never'},
 'existingSecret':'warp-env','persistence':{'size':'128Mi'},'system':{'spec':{
 'server':{'public_url':'https://humagen.example.invalid'},
 'auth':{'oidc':{'issuer':'https://accounts.google.com','client_id':'verification','audience':'verification'}}}}}}}))
PY
kubectl apply -f "$ARTIFACTS/helmrelease.json"
flux reconcile source git humagen -n flux-system --timeout=180s
flux reconcile helmrelease warp -n warp-flux --with-source --timeout=300s
seed_marker warp-flux
snapshot warp-flux before
kubectl -n flux-system patch gitrepository humagen --type=merge \
  -p "{\"spec\":{\"ref\":{\"tag\":null,\"commit\":\"$NEW_COMMIT\"}}}"
flux reconcile source git humagen -n flux-system --timeout=180s
python3 - "$ARTIFACTS/helmrelease.json" "$NEW_IMAGE" <<'PY' > "$ARTIFACTS/helmrelease-upgraded.json"
import json,sys
release=json.load(open(sys.argv[1])); repository,tag=sys.argv[2].rsplit(':',1)
release['spec']['chart']['spec']['chart']='./deploy/helm/humagen'
release['spec']['upgrade']={'chartNameChangeStrategy':'InPlaceUpdate'}
release['spec']['values']['nameOverride']='warp'
release['spec']['values']['image'].update(repository=repository,tag=tag)
print(json.dumps(release))
PY
kubectl apply -f "$ARTIFACTS/helmrelease-upgraded.json"
flux reconcile helmrelease warp -n warp-flux --with-source --timeout=300s
verify_kept warp-flux
flux version > "$ARTIFACTS/flux-version.txt"
helm list -A > "$ARTIFACTS/helm-releases.txt"
echo "Helm and Flux upgrade verification passed. Evidence: $ARTIFACTS"
