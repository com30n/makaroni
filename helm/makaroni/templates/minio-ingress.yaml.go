{{- if .Values.minio.enabled }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "makaroni.fullname" . }}-minio
  annotations:
    nginx.ingress.kubernetes.io/use-regex: "true"
    nginx.ingress.kubernetes.io/rewrite-target: /$2
    nginx.ingress.kubernetes.io/upstream-vhost: {{ include "makaroni.fullname" . }}-minio:{{ .Values.minio.servicePort }}
spec:
  ingressClassName: nginx
  rules:
    - host: {{ .Values.makaroni.ingress.host }}
      http:
        paths:
          - path: /s3(/|$)(.*)
            pathType: ImplementationSpecific
            backend:
              service:
                name: {{ include "makaroni.fullname" . }}-minio
                port:
                  number: {{ .Values.minio.servicePort }}
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "makaroni.fullname" . }}-minio-console
  annotations:
    nginx.ingress.kubernetes.io/use-regex: "true"
    nginx.ingress.kubernetes.io/rewrite-target: /$2
    nginx.ingress.kubernetes.io/upstream-vhost: {{ include "makaroni.fullname" . }}-minio:{{ .Values.minio.consolePort }}
spec:
  ingressClassName: nginx
  rules:
    - host: {{ .Values.makaroni.ingress.host }}
      http:
        paths:
          - path: /console(/|$)(.*)
            pathType: ImplementationSpecific
            backend:
              service:
                name: {{ include "makaroni.fullname" . }}-minio
                port:
                  number: {{ .Values.minio.consolePort }}
{{- end }}