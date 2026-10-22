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
document per workspace, separated by "---". Rendered from the values as
written; the chart adds only api_version and kind.
*/}}
{{- define "humagen.spec" -}}
{{- $sys := .Values.system | default dict -}}
{{- $spec := $sys.spec | default dict -}}
{{- $auth := $spec.auth | default dict -}}
{{- $kind := $auth.kind | default "dev" -}}
{{- if has $kind (list "dev" "mock" "tailscale") -}}
{{- fail (printf "system.spec.auth.kind=%s trusts loopback and cannot serve a pod; use oidc. An empty kind means dev" $kind) -}}
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
