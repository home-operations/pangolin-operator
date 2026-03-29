{{- if .Values.controller.metrics.enabled -}}
---
apiVersion: v1
kind: Service
metadata:
  name: {{ include "pangolin-operator.metricsServiceName" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "pangolin-operator.labels" . | nindent 4 }}
  {{- with .Values.controller.metrics.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  type: ClusterIP
  ports:
    - name: metrics
      port: {{ .Values.controller.metrics.port }}
      targetPort: metrics
      protocol: TCP
  selector:
    {{- include "pangolin-operator.selectorLabels" . | nindent 4 }}
{{- end }}
