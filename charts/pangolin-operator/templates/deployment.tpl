---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "pangolin-operator.fullname" . }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "pangolin-operator.labels" . | nindent 4 }}
  {{- with .Values.podAnnotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      {{- include "pangolin-operator.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      annotations:
        kubectl.kubernetes.io/default-container: manager
        {{- with .Values.podAnnotations }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      labels:
        {{- include "pangolin-operator.labels" . | nindent 8 }}
        {{- with .Values.podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
    spec:
      enableServiceLinks: false
      {{- with .Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      serviceAccountName: {{ include "pangolin-operator.serviceAccountName" . }}
      priorityClassName: system-node-critical
      securityContext:
        {{- toYaml .Values.podSecurityContext | nindent 8 }}
      containers:
        - name: manager
          image: {{ include "pangolin-operator.image" . }}
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          command:
            - /manager
          args:
            - --log-level={{ .Values.controller.logLevel }}
            {{- if .Values.controller.leaderElection.enabled }}
            - --leader-elect
            {{- end }}
            - --health-probe-bind-address=:{{ .Values.controller.health.port }}
            {{- if .Values.controller.metrics.enabled }}
            - --metrics-bind-address=:{{ .Values.controller.metrics.port }}
            - --metrics-secure={{ .Values.controller.metrics.secure }}
            {{- end }}
            {{- if .Values.controller.pprof.enabled }}
            - --enable-pprof
            - --pprof-bind-address=:{{ .Values.controller.pprof.port }}
            {{- end }}
          {{- with .Values.securityContext }}
          securityContext:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          envFrom:
            - secretRef:
                name: {{ include "pangolin-operator.credentialsSecretName" . }}
          ports:
            - name: health
              containerPort: {{ .Values.controller.health.port }}
              protocol: TCP
            {{- if .Values.controller.metrics.enabled }}
            - name: metrics
              containerPort: {{ .Values.controller.metrics.port }}
              protocol: TCP
            {{- end }}
            {{- if .Values.controller.pprof.enabled }}
            - name: pprof
              containerPort: {{ .Values.controller.pprof.port }}
              protocol: TCP
            {{- end }}
          livenessProbe:
            {{- toYaml .Values.livenessProbe | nindent 12 }}
          readinessProbe:
            {{- toYaml .Values.readinessProbe | nindent 12 }}
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          {{- with .Values.volumeMounts }}
          volumeMounts:
            {{- toYaml . | nindent 12 }}
          {{- end }}
      {{- with .Values.volumes }}
      volumes:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
