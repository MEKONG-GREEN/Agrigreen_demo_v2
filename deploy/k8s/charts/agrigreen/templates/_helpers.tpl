{{/*
Hàm dùng chung. Quy ước: tham số dạng dict có khóa "root" (= $ gốc).
*/}}

{{/* Vùng có được render ở bản phát hành này không: include "agrigreen.on" (dict "root" $ "zone" "pf") → "true" | "" */}}
{{- define "agrigreen.on" -}}
{{- if has .zone .root.Values.global.installZones -}}true{{- end -}}
{{- end -}}

{{/* Tên namespace của vùng. */}}
{{- define "agrigreen.ns" -}}
{{- $ns := index .root.Values.namespaces .zone -}}
{{- if not $ns -}}{{- fail (printf "vùng không có namespace: %s" .zone) -}}{{- end -}}
{{- $ns -}}
{{- end -}}

{{- define "agrigreen.svcSuffix" -}}
svc.{{ .Values.global.clusterDomain }}
{{- end -}}

{{/* Nhãn chung. */}}
{{- define "agrigreen.labels" -}}
app.kubernetes.io/part-of: agrigreen
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .root.Chart.Name .root.Chart.Version }}
agrigreen.vn/zone: {{ .zone }}
{{- end -}}

{{/* Ảnh: {repository, tag?, external?}. Ảnh ngoài (external) giữ nguyên repository, không tiền tố kho nội bộ. */}}
{{- define "agrigreen.image" -}}
{{- $img := .image -}}
{{- $tag := $img.tag | default .root.Values.global.imageTag | default .root.Chart.AppVersion -}}
{{- if $img.external -}}
{{- printf "%s:%s" $img.repository $tag -}}
{{- else -}}
{{- printf "%s/%s:%s" .root.Values.global.imageRegistry $img.repository $tag -}}
{{- end -}}
{{- end -}}

{{/* URL DB: "pooler:X" → PgBouncer (CNPG Pooler) của cụm X; "direct:X" → primary pg-X-rw (khóa mức phiên, outbox). */}}
{{- define "agrigreen.dbURL" -}}
{{- $parts := splitList ":" .ref -}}
{{- $c := index $parts 1 -}}
{{- $cl := index .root.Values.postgres.clusters $c -}}
{{- if not $cl -}}{{- fail (printf "không có cụm PostgreSQL %s" $c) -}}{{- end -}}
{{- $ns := include "agrigreen.ns" (dict "root" .root "zone" $cl.zone) -}}
{{- $host := ternary (printf "pg-%s-pooler" $c) (printf "pg-%s-rw" $c) (eq (index $parts 0) "pooler") -}}
{{- printf "postgres://%s.%s.%s:5432/?sslmode=require" $host $ns (include "agrigreen.svcSuffix" .root) -}}
{{- end -}}

{{/* Giá trị env: tham chiếu DB hoặc chuỗi tpl. */}}
{{- define "agrigreen.envValue" -}}
{{- $v := toString .value -}}
{{- if or (hasPrefix "pooler:" $v) (hasPrefix "direct:" $v) -}}
{{- include "agrigreen.dbURL" (dict "root" .root "ref" $v) -}}
{{- else -}}
{{- tpl $v .root -}}
{{- end -}}
{{- end -}}

{{/* SecurityContext Pod (mục 18: non-root, seccomp). */}}
{{- define "agrigreen.podSecurity" -}}
runAsNonRoot: true
runAsUser: {{ .uid }}
runAsGroup: {{ .uid }}
fsGroup: {{ .uid }}
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{/* SecurityContext container: hệ tệp gốc chỉ đọc, bỏ mọi capability, không nâng quyền. */}}
{{- define "agrigreen.containerSecurity" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
privileged: false
capabilities:
  drop: ["ALL"]
{{- end -}}

{{/* Biến chung của dịch vụ Go (không bí mật). */}}
{{- define "agrigreen.goEnv" -}}
- name: AG_ENV
  value: {{ .Values.global.env | quote }}
- name: AG_OTLP_ENDPOINT
  value: {{ tpl .Values.global.otlpEndpoint . | quote }}
- name: AG_OTLP_INSECURE
  value: "true"
- name: AG_RESIDENCY_ALLOWLIST
  value: {{ .Values.global.residencyAllowlist | quote }}
{{- end -}}

{{/*
Điểm cuối của một luồng → dict YAML:
  inCluster: bool, zone: <vùng của namespace, "" nếu namespace ngoài chart>, namespace, podSelector (object),
  cidrs: [..] (ngoài cụm)
*/}}
{{- define "agrigreen.endpoint" -}}
{{- $root := .root -}}
{{- $e := .ep -}}
{{- if $e.external -}}
inCluster: false
zone: ""
cidrs: {{ index $root.Values.external $e.external | default list | toJson }}
{{- else if $e.selector -}}
{{- $s := index $root.Values.systemSelectors $e.selector -}}
{{- if not $s -}}{{- fail (printf "systemSelectors thiếu %s" $e.selector) -}}{{- end -}}
inCluster: true
{{- if $s.zone }}
zone: {{ $s.zone }}
namespace: {{ include "agrigreen.ns" (dict "root" $root "zone" $s.zone) }}
{{- else }}
zone: ""
namespace: {{ $s.namespace }}
{{- end }}
podSelector:
  matchLabels: {{ $s.pods | toJson }}
{{- else -}}
inCluster: true
zone: {{ $e.zone }}
namespace: {{ include "agrigreen.ns" (dict "root" $root "zone" $e.zone) }}
podSelector:
{{- if $e.app }}
  matchLabels: {"app.kubernetes.io/name": {{ $e.app | quote }}}
{{- else if $e.apps }}
  matchExpressions: [{"key": "app.kubernetes.io/name", "operator": "In", "values": {{ $e.apps | toJson }}}]
{{- else if $e.pg }}
  matchLabels: {"cnpg.io/cluster": {{ printf "pg-%s" $e.pg | quote }}, "cnpg.io/podRole": "instance"}
{{- else if $e.pooler }}
  matchLabels: {"cnpg.io/poolerName": {{ printf "pg-%s-pooler" $e.pooler | quote }}}
{{- else if $e.poolers }}
{{- $names := list -}}
{{- range $e.poolers }}{{ $names = append $names (printf "pg-%s-pooler" .) }}{{ end }}
  matchExpressions: [{"key": "cnpg.io/poolerName", "operator": "In", "values": {{ $names | toJson }}}]
{{- else if $e.pgAll }}
  matchLabels: {"cnpg.io/podRole": "instance"}
{{- else if $e.poolersAll }}
  matchExpressions: [{"key": "cnpg.io/poolerName", "operator": "Exists"}]
{{- else if $e.cnpgAll }}
  matchExpressions: [{"key": "cnpg.io/cluster", "operator": "Exists"}]
{{- else if $e.dbJobs }}
  matchLabels: {"agrigreen.vn/db-job": "true"}
{{- else if $e.all }}
  {}
{{- else -}}
{{- fail (printf "điểm cuối luồng không hợp lệ: %v" $e) -}}
{{- end }}
{{- end -}}
{{- end -}}

{{/* Peer NetworkPolicy từ điểm cuối (đã fromYaml). */}}
{{- define "agrigreen.peers" -}}
{{- if .inCluster -}}
- namespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: {{ .namespace }}
  podSelector: {{ .podSelector | toJson }}
{{- else -}}
{{- range .cidrs }}
- ipBlock:
    cidr: {{ . }}
{{- end }}
{{- end -}}
{{- end -}}
