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
{{- $proxy := (($.Values.tailscale | default dict).proxy | default dict).enabled | default false -}}
{{- if and $loopback (has $kind (list "dev" "mock")) -}}
{{- else if and (eq $kind "tailscale") $proxy (not $loopback) -}}
{{- else if eq $kind "tailscale" -}}
{{- fail "system.spec.auth.kind=tailscale trusts the identity headers a proxy on the pod's loopback sets: turn on tailscale.proxy, a Tailscale proxy container in the pod, and leave development.loopback off" -}}
{{- else if has $kind (list "dev" "mock") -}}
{{- fail (printf "system.spec.auth.kind=%s trusts loopback and cannot serve a pod; use oidc, tailscale with tailscale.proxy, or development.loopback on a local cluster for dev and mock. An empty kind means dev" $kind) -}}
{{- end -}}
{{- if and $proxy (ne $kind "tailscale") -}}
{{- fail "tailscale.proxy is only for system.spec.auth.kind=tailscale" -}}
{{- end -}}
{{/* The bus follows the stores: on Postgres, events go to a table on the
same database. A bus kind set in the values wins. */}}
{{- $bus := $spec.bus | default dict -}}
{{- if and (not $bus.kind) (eq (($spec.stores | default dict).kind | default "") "postgres") -}}
{{- $_ := set $spec "bus" (merge (dict "kind" "postgres") $bus) -}}
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
{{- fail (printf "release %s was installed by the warp chart: set nameOverride: warp to keep its Deployment, Service and volume (docs/guides/upgrading-from-warp.md#kubernetes)" $.Release.Name) -}}
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
{{- fail (printf "image tag %s is a release from before the rename, which cannot read the spec this chart writes; use 0.21.0 or later (docs/guides/upgrading-from-warp.md#kubernetes)" $tag) -}}
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

{{/*
"true" when humagen listens on the pod's loopback only: development.loopback,
or tailscale sign-in behind the proxy container. The kubelet cannot reach
that listener, so the probes run inside the pod.
*/}}
{{- define "humagen.loopback" -}}
{{- $kind := ((.Values.system | default dict).spec | default dict).auth | default dict | dig "kind" "" -}}
{{- if or (((.Values.development | default dict).loopback) | default false) (eq $kind "tailscale") -}}true{{- end -}}
{{- end -}}

{{/* The blobs kind the spec selects; humagen's default is disk. */}}
{{- define "humagen.blobsKind" -}}
{{- dig "spec" "blobs" "kind" "disk" (.Values.system | default dict) | default "disk" -}}
{{- end -}}

{{/* The stores kind the spec selects; humagen's default is memory. */}}
{{- define "humagen.storesKind" -}}
{{- dig "spec" "stores" "kind" "memory" (.Values.system | default dict) | default "memory" -}}
{{- end -}}

{{/*
A Go duration of hours, minutes and seconds ("25s", "1m30s", "2h") as whole
seconds. Called with (list "values.key" value).
*/}}
{{- define "humagen.seconds" -}}
{{- $key := index . 0 -}}
{{- $d := toString (index . 1) -}}
{{- if not (regexMatch "^([0-9]+[hms])+$" $d) -}}
{{- fail (printf "%s=%q: give a duration in whole h, m and s, such as 25s or 1m30s" $key $d) -}}
{{- end -}}
{{- $total := 0 -}}
{{- range $part := regexFindAll "[0-9]+[hms]" $d -1 -}}
{{- $n := int (trimSuffix "h" (trimSuffix "m" (trimSuffix "s" $part))) -}}
{{- if hasSuffix "h" $part -}}{{- $total = add $total (mul $n 3600) -}}
{{- else if hasSuffix "m" $part -}}{{- $total = add $total (mul $n 60) -}}
{{- else -}}{{- $total = add $total $n -}}
{{- end -}}
{{- end -}}
{{- $total -}}
{{- end -}}

{{/*
The drain flags and the grace period that must cover them: preStop sleep,
then the drain, then the HTTP shutdown, with a few seconds to spare.
*/}}
{{- define "humagen.drainArgs" -}}
{{- $s := .Values.shutdown | default dict -}}
{{- $drain := include "humagen.seconds" (list "shutdown.drainGrace" ($s.drainGrace | default "25s")) | int -}}
{{- $http := include "humagen.seconds" (list "shutdown.httpTimeout" ($s.httpTimeout | default "10s")) | int -}}
{{- $sleep := int ($s.preStopSleepSeconds | default 0) -}}
{{- $need := add $drain $http $sleep -}}
{{- $grace := int .Values.terminationGracePeriodSeconds -}}
{{- if le $grace $need -}}
{{- fail (printf "terminationGracePeriodSeconds=%d must exceed shutdown.preStopSleepSeconds + shutdown.drainGrace + shutdown.httpTimeout (%d seconds), or the kubelet kills humagen mid-drain" $grace $need) -}}
{{- end -}}
- -drain-grace={{ $s.drainGrace | default "25s" }}
- -shutdown-timeout={{ $s.httpTimeout | default "10s" }}
{{- end -}}

{{/* Worker selector labels: a name of their own, so the server's selector
(which cannot change on upgrade) and its Service never match a worker pod. */}}
{{- define "humagen.workerSelectorLabels" -}}
app.kubernetes.io/name: {{ printf "%s-worker" (include "humagen.name" .) | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "humagen.workerLabels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "humagen.workerSelectorLabels" . }}
app.kubernetes.io/component: worker
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "humagen.workerFullname" -}}
{{- printf "%s-worker" (include "humagen.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
The Tailscale proxy container: the only way into a pod whose humagen listens
on loopback for tailscale sign-in. It joins the tailnet itself and
reverse-proxies to 127.0.0.1:8080, setting the identity headers humagen
trusts. Its files (a Caddyfile) are at /etc/tailscale-proxy, its tailnet
state at tailscale.proxy.stateDir.
*/}}
{{- define "humagen.tailscaleProxy" -}}
{{- $p := .Values.tailscale.proxy -}}
{{- $_ := required "tailscale.proxy.image.repository is required: an image of a Tailscale-aware proxy, such as Caddy built with caddy-tailscale" ($p.image | default dict).repository -}}
- name: tailscale-proxy
  image: {{ printf "%s:%s" $p.image.repository (required "tailscale.proxy.image.tag is required" $p.image.tag) | quote }}
  imagePullPolicy: {{ $p.image.pullPolicy | default "IfNotPresent" }}
  {{- with $p.command }}
  command:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $p.args }}
  args:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $p.env }}
  env:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $p.envFrom }}
  envFrom:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  securityContext:
    {{- toYaml ($p.securityContext | default $.Values.securityContext) | nindent 4 }}
  resources:
    {{- toYaml ($p.resources | default dict) | nindent 4 }}
  volumeMounts:
    - name: tailscale-proxy
      mountPath: /etc/tailscale-proxy
      readOnly: true
    - name: tailscale-state
      mountPath: {{ $p.stateDir | default "/var/lib/tailscale-proxy" }}
    - name: tmp
      mountPath: /tmp
    {{- with $p.volumeMounts }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
{{- end -}}
