{{- if .Values.rbac.create -}}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "pangolin-operator.fullname" . }}-manager-role
  labels:
    {{- include "pangolin-operator.labels" . | nindent 4 }}
rules:
  - apiGroups: [""]
    resources: [events]
    verbs: [create, patch]
  - apiGroups: [""]
    resources: [secrets]
    verbs: [create, delete, get, list, patch, update, watch]
  - apiGroups: [""]
    resources: [serviceaccounts]
    verbs: [create, get, list, patch, update, watch]
  - apiGroups: [""]
    resources: [services]
    verbs: [get, list, watch]
  - apiGroups: [apps]
    resources: [deployments]
    verbs: [create, delete, get, list, patch, update, watch]
  - apiGroups: [gateway.networking.k8s.io]
    resources: [httproutes]
    verbs: [get, list, watch]
  - apiGroups: [pangolin.home-operations.com]
    resources: [newtsites, publicresources, privateresources]
    verbs: [create, delete, get, list, patch, update, watch]
  - apiGroups: [pangolin.home-operations.com]
    resources: [newtsites/finalizers, publicresources/finalizers, privateresources/finalizers]
    verbs: [update]
  - apiGroups: [pangolin.home-operations.com]
    resources: [newtsites/status, publicresources/status, privateresources/status]
    verbs: [get, patch, update]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ include "pangolin-operator.fullname" . }}-manager-rolebinding
  labels:
    {{- include "pangolin-operator.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ include "pangolin-operator.fullname" . }}-manager-role
subjects:
  - kind: ServiceAccount
    name: {{ include "pangolin-operator.serviceAccountName" . }}
    namespace: {{ .Release.Namespace }}
{{- if .Values.controller.leaderElection.enabled }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ include "pangolin-operator.fullname" . }}-leader-election-role
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "pangolin-operator.labels" . | nindent 4 }}
rules:
  - apiGroups: [""]
    resources: [configmaps]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [coordination.k8s.io]
    resources: [leases]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [""]
    resources: [events]
    verbs: [create, patch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ include "pangolin-operator.fullname" . }}-leader-election-rolebinding
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "pangolin-operator.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: {{ include "pangolin-operator.fullname" . }}-leader-election-role
subjects:
  - kind: ServiceAccount
    name: {{ include "pangolin-operator.serviceAccountName" . }}
    namespace: {{ .Release.Namespace }}
{{- end }}
{{- end }}
