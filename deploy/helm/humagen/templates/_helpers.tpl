{{- define "humagen.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "humagen.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "humagen.selectorLabels" -}}
app.kubernetes.io/name: {{ include "humagen.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "humagen.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "humagen.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "humagen.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "humagen.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
The spec documents the pod reads: the System document, then one Platform
document per workspace, separated by "---". The chart omits null store
entries from the values and adds api_version and kind.
*/}}
{{- define "humagen.spec" -}}
{{- $sys := deepCopy (.Values.system | default dict) -}}
{{- $spec := $sys.spec | default dict -}}
{{/* Helm reuse-values may retain null entries that normal coalescing drops.
Omit only null store entries, leaving configured inactive blocks for the
strict spec decoder to refuse. Never mutate the release's values. */}}
{{- range $key, $value := ($spec.stores | default dict) -}}
{{- if eq $value nil -}}
{{- $_ := unset $spec.stores $key -}}
{{- end -}}
{{- end -}}
{{- $auth := $spec.auth | default dict -}}
{{/* An auth block a values file turned off with null, such as oidc under
development, is omitted the same way. */}}
{{- range $key, $value := $auth -}}
{{- if eq $value nil -}}
{{- $_ := unset $auth $key -}}
{{- end -}}
{{- end -}}
{{- $kind := $auth.kind | default "dev" -}}
{{- $loopback := (($.Values.development | default dict).loopback) | default false -}}
{{- if and $loopback (has $kind (list "dev" "mock")) -}}
{{- else if has $kind (list "dev" "mock" "tailscale") -}}
{{- fail (printf "system.spec.auth.kind=%s trusts loopback and cannot serve a pod; use oidc, or development.loopback on a local cluster for dev and mock. An empty kind means dev" $kind) -}}
{{- end -}}
{{- if eq $kind "oidc" -}}
{{- $oidc := $auth.oidc | default dict -}}
{{- $_ := required "system.spec.auth.oidc.issuer is required" $oidc.issuer -}}
{{- $_ := required "system.spec.auth.oidc.client_id is required" $oidc.client_id -}}
{{- $_ := required "system.spec.auth.oidc.audience is required: the API audience access tokens are issued for" $oidc.audience -}}
{{- end -}}
api_version: humagen/v1
kind: System
{{ omit $sys "api_version" "kind" | toYaml }}
{{- range $i, $p := .Values.platforms }}
{{- $_ := required (printf "platforms[%d].metadata.name is required: it is the workspace's tenant" $i) (($p.metadata | default dict).name) }}
---
api_version: humagen/v1
kind: Platform
{{ omit $p "api_version" "kind" | toYaml }}
{{- end }}
{{- end -}}

{{/*
A release the warp chart installed, before the product was renamed, names its
Deployment, Service and volume "warp". This chart would name them otherwise
and so start a second, empty one beside them, so it refuses to, unless
nameOverride keeps the old name. lookup sees the cluster on helm install and
upgrade, Flux and humagen deploy; helm template sees nothing and renders.
*/}}
{{- define "humagen.keepOldNames" -}}
{{- if ne (include "humagen.name" .) "warp" -}}
{{- $found := lookup "apps/v1" "Deployment" .Release.Namespace "" | default dict -}}
{{- range $d := ($found.items | default list) -}}
{{- $sel := (($d.spec | default dict).selector | default dict).matchLabels | default dict -}}
{{- if and (eq (index $sel "app.kubernetes.io/instance" | default "") $.Release.Name) (eq (index $sel "app.kubernetes.io/name" | default "") "warp") -}}
{{- fail (printf "release %s was installed by the warp chart: set nameOverride: warp to keep its Deployment, Service and volume (docs/upgrading-from-warp.md#kubernetes)" $.Release.Name) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
The image must read the spec this chart writes (api_version humagen/v1),
which no release before 0.21.0 does: with Recreate, such an image would take
the server down. A tag that is not a release version (a commit) is not dated.
*/}}
{{- define "humagen.image" -}}
{{- $tag := toString (.Values.image.tag | default .Chart.AppVersion) -}}
{{- if regexMatch "^v?[0-9]+\\.[0-9]+\\.[0-9]+(-[0-9A-Za-z.-]+)?$" $tag -}}
{{- if semverCompare "<0.21.0-0" (trimPrefix "v" $tag) -}}
{{- fail (printf "image tag %s is a release from before the rename, which cannot read the spec this chart writes; use 0.21.0 or later (docs/upgrading-from-warp.md#kubernetes)" $tag) -}}
{{- end -}}
{{- end -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

{{/*
The container's name. The warp chart gave every container the old name,
whatever its nameOverride, so a release it installed keeps the old name for
patches and `kubectl logs -c`. Read from the running Deployment; a new
release, or helm template, takes the chart's name.
*/}}
{{- define "humagen.containerName" -}}
{{- $name := include "humagen.name" . -}}
{{- $live := lookup "apps/v1" "Deployment" .Release.Namespace (include "humagen.fullname" .) | default dict -}}
{{- range ((($live.spec | default dict).template | default dict).spec | default dict).containers | default list -}}
{{- if eq .name "warp" -}}{{- $name = "warp" -}}{{- end -}}
{{- end -}}
{{- $name -}}
{{- end -}}
