# humagen on a local Kubernetes cluster, through its own Helm chart.
#
#   tilt up        # then open http://127.0.0.1:8080
#
# Needs a local cluster Tilt trusts (kind, k3d, minikube, Docker Desktop,
# Rancher Desktop or OrbStack), Docker, helm, Go and pnpm. The binary and the
# console are built on the host, where Go's and pnpm's caches make a change
# take seconds, and copied into a small image (deploy/tilt/Dockerfile). The
# chart runs with deploy/tilt/values.yaml: development sign-in on the pod's
# loopback, reached only through the port forward, memory stores, example
# agents and the offline model. Nothing needs a key or an identity provider.
#
# deploy/tilt/values.local.yaml, if present, is applied last, for your own
# overrides (git ignores it): a model key's Secret, a branding block, more
# platforms.

image_dir = '.tilt/image'

# The image's context must exist before the first build fills it.
local('mkdir -p %s' % image_dir, quiet=True)

# The binary, for the cluster's architecture, which a local cluster shares
# with this machine.
local_resource(
    'humagen-binary',
    cmd='mkdir -p %s && CGO_ENABLED=0 GOOS=linux go build -trimpath -o %s/humagen ./cmd/humagen' % (image_dir, image_dir),
    deps=['cmd', 'internal', 'pkg', 'fiber', 'humagen.go', 'go.mod', 'go.sum', 'api', 'CHANGELOG.md', 'deploy/chart.go', 'deploy/helm/humagen'],
    labels=['build'],
)

# The console. pnpm install is a no-op when nothing changed.
local_resource(
    'console',
    cmd='pnpm install --frozen-lockfile && pnpm --filter @humagen/console build && rm -rf %s/web && mkdir -p %s && cp -R apps/web/dist %s/web' % (image_dir, image_dir, image_dir),
    deps=['apps/web/src', 'apps/web/public', 'apps/web/index.html', 'apps/web/vite.config.ts', 'apps/web/package.json', 'packages', 'pnpm-lock.yaml'],
    ignore=['**/node_modules', '**/dist', '**/.turbo', 'packages/*/src/generated/README.md'],
    labels=['build'],
)

docker_build(
    'humagen-dev',
    image_dir,
    dockerfile='deploy/tilt/Dockerfile',
)

values = ['deploy/tilt/values.yaml']
if os.path.exists('deploy/tilt/values.local.yaml'):
    values.append('deploy/tilt/values.local.yaml')

k8s_yaml(helm('deploy/helm/humagen', name='humagen', values=values))

k8s_resource(
    'humagen',
    port_forwards=['8080:8080'],
    resource_deps=['humagen-binary', 'console'],
    labels=['app'],
)
