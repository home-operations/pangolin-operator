{{- if not .Values.pangolin.existingSecret -}}
---
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "pangolin-operator.fullname" . }}-credentials
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "pangolin-operator.labels" . | nindent 4 }}
stringData:
  PANGOLIN_ENDPOINT: {{ .Values.pangolin.endpoint | quote }}
  PANGOLIN_API_KEY: {{ .Values.pangolin.apiKey | quote }}
  PANGOLIN_ORG_ID: {{ .Values.pangolin.orgId | quote }}
{{- end }}
