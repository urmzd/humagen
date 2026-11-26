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
{{- $browser := .Values.browser | default dict -}}
{{- $browserWorkspace := include "humagen.browserWorkspace" . -}}
{{- range $i, $p := .Values.platforms }}
{{- $_ := required (printf "platforms[%d].metadata.name is required: it is the workspace's tenant" $i) (($p.metadata | default dict).name) }}
{{- if and $browser.enabled (eq $browserWorkspace $p.metadata.name) }}
{{/* The browser's tool server, in the one workspace it belongs to. Never
mutate the release's values. */}}
{{- $p = deepCopy $p -}}
{{- $pspec := $p.spec | default dict -}}
{{- $name := include "humagen.browserTool" $ -}}
{{- range $t := ($pspec.tools | default list) -}}
{{- if eq ($t.name | default "") $name -}}
{{- fail (printf "browser.enabled adds the tool server %q to workspace %q, which already has one of that name: rename it, or set browser.tool" $name $browserWorkspace) -}}
{{- end -}}
{{- end -}}
{{- $accept := $.Files.Get "files/browser-tools.yaml" | fromYamlArray -}}
{{- $tool := dict "name" $name "kind" "mcp" "mcp" (dict "transport" "http" "endpoint" (printf "http://%s:%s/mcp" (include "humagen.browserFullname" $) (include "humagen.browserPort" $)) "accept" $accept) -}}
{{- $_ := set $pspec "tools" (append ($pspec.tools | default list) $tool) -}}
{{- $_ := set $p "spec" $pspec -}}
{{- end }}
---
api_version: humagen/v1
kind: Platform
{{ omit $p "api_version" "kind" | toYaml }}
{{- end }}
{{- end -}}

{{/* The browser pod's names and labels: a name of its own, so the server's
selector and Service never match it. */}}
{{- define "humagen.browserFullname" -}}
{{- /* The base is cut first, so a long name still ends in -browser and
never becomes the server's own. */ -}}
{{- printf "%s-browser" (include "humagen.fullname" . | trunc 55 | trimSuffix "-") -}}
{{- end -}}

{{- define "humagen.browserSelectorLabels" -}}
app.kubernetes.io/name: {{ printf "%s-browser" (include "humagen.name" . | trunc 55 | trimSuffix "-") }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* The browser's port: a number a port can be. */}}
{{- define "humagen.browserPort" -}}
{{- $raw := (.Values.browser | default dict).port | default 8931 -}}
{{- $port := int $raw -}}
{{- if or (lt $port 1) (gt $port 65535) (ne (toString $port) (toString $raw | trimSuffix ".0")) -}}
{{- fail (printf "browser.port %v is not a port: a whole number from 1 to 65535" $raw) -}}
{{- end -}}
{{- $port -}}
{{- end -}}

{{/* The tool server's name, as Humagen takes a provider's name. */}}
{{- define "humagen.browserTool" -}}
{{- $name := (.Values.browser | default dict).tool | default "browser" | toString -}}
{{- if not (regexMatch "^[a-z][a-z0-9-]{0,31}$" $name) -}}
{{- fail (printf "browser.tool %q is not a tool server's name: a lowercase letter, then lowercase letters, digits and dashes, at most 32 in all" $name) -}}
{{- end -}}
{{- $name -}}
{{- end -}}

{{/*
What the browser may not reach when the values name no list: the private,
link-local, shared and reserved ranges, and the addresses clouds serve
metadata on. A release whose reused values predate the browser has no list,
and gets this one.
*/}}
{{- define "humagen.browserBlockedCIDRs" -}}
- 0.0.0.0/8
- 10.0.0.0/8
- 100.64.0.0/10
- 127.0.0.0/8
- 168.63.129.16/32
- 169.254.0.0/16
- 172.16.0.0/12
- 192.168.0.0/16
- 198.18.0.0/15
- 240.0.0.0/4
{{- end -}}

{{- define "humagen.browserLabels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "humagen.browserSelectorLabels" . }}
app.kubernetes.io/component: browser
app.kubernetes.io/version: {{ (.Values.browser.image | default dict).tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
The workspace the browser belongs to. It is one browser with one page and
one set of sign-ins, so it is never given to more than one workspace: with
several platforms, browser.workspace says which. Empty when the browser is off.
*/}}
{{- define "humagen.browserWorkspace" -}}
{{- $browser := .Values.browser | default dict -}}
{{- if $browser.enabled -}}
{{- $names := list -}}
{{- range $p := .Values.platforms -}}
{{- $names = append $names ((($p | default dict).metadata | default dict).name | default "") -}}
{{- end -}}
{{- if $browser.workspace -}}
{{- if not (has $browser.workspace $names) -}}
{{- fail (printf "browser.workspace %q is not a platform's metadata.name" $browser.workspace) -}}
{{- end -}}
{{- $browser.workspace -}}
{{- else if eq (len $names) 1 -}}
{{- first $names -}}
{{- else -}}
{{- fail "browser.enabled gives one workspace the browser: it is one page and one set of sign-ins. With no platform, or several, name the workspace in browser.workspace" -}}
{{- end -}}
{{- end -}}
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
