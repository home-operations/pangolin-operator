package newtsite

import (
	"fmt"
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
)

const defaultMTU = 1280

// buildDeployment constructs the appsv1.Deployment for the newt tunnel pod.
// secretName is the name of the Secret containing PANGOLIN_ENDPOINT, NEWT_ID, NEWT_SECRET.
func buildDeployment(site *pangolinv1alpha1.NewtSite, secretName string) *appsv1.Deployment {
	spec := site.Spec.Newt

	image := spec.Image
	if image == "" {
		image = "ghcr.io/fosrl/newt"
	}
	tag := spec.Tag
	if tag == "" {
		tag = "latest"
	}

	var replicas int32 = 1
	if spec.Replicas != nil {
		replicas = *spec.Replicas
	}

	logLevel := spec.LogLevel
	if logLevel == "" {
		logLevel = "INFO"
	}

	labels := map[string]string{
		"app.kubernetes.io/name":      "newtsite",
		"app.kubernetes.io/instance":  site.Name,
		"app.kubernetes.io/component": "newt",
		"app.kubernetes.io/part-of":   "pangolin-operator",
	}

	// --- main newt container ---
	env := []corev1.EnvVar{
		{
			Name: "PANGOLIN_ENDPOINT",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "PANGOLIN_ENDPOINT",
				},
			},
		},
		{
			Name: "NEWT_ID",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "NEWT_ID",
				},
			},
		},
		{
			Name: "NEWT_SECRET",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "NEWT_SECRET",
				},
			},
		},
		{
			Name:  "LOG_LEVEL",
			Value: logLevel,
		},
	}
	if spec.Mtu != defaultMTU {
		env = append(env, corev1.EnvVar{
			Name:  "MTU",
			Value: fmt.Sprintf("%d", spec.Mtu),
		})
	}
	if spec.PingInterval != "" {
		env = append(env, corev1.EnvVar{Name: "PING_INTERVAL", Value: spec.PingInterval})
	}
	if spec.PingTimeout != "" {
		env = append(env, corev1.EnvVar{Name: "PING_TIMEOUT", Value: spec.PingTimeout})
	}
	if spec.DNS != "" {
		env = append(env, corev1.EnvVar{Name: "DNS", Value: spec.DNS})
	}
	if spec.AcceptClients {
		env = append(env, corev1.EnvVar{Name: "ACCEPT_CLIENTS", Value: "true"})
	}
	if spec.Interface != "" && spec.Interface != "newt" {
		env = append(env, corev1.EnvVar{Name: "INTERFACE", Value: spec.Interface})
	}
	if spec.Metrics != nil {
		adminAddr := spec.Metrics.AdminAddr
		if adminAddr == "" {
			port := spec.Metrics.Port
			if port == 0 {
				port = 9090
			}
			adminAddr = fmt.Sprintf("0.0.0.0:%d", port)
		}
		env = append(env, corev1.EnvVar{Name: "NEWT_ADMIN_ADDR", Value: adminAddr})
	}

	// Extra env vars are appended last so they can override anything above.
	env = append(env, spec.ExtraEnv...)

	newtResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10m"),
			corev1.ResourceMemory: resource.MustParse("32Mi"),
		},
	}
	if spec.Resources != nil {
		newtResources = *spec.Resources
	}

	containerSecCtx, podSecCtx := buildSecurityContexts(spec)

	newtContainer := corev1.Container{
		Name:            "newt",
		Image:           image + ":" + tag,
		Env:             env,
		SecurityContext: containerSecCtx,
		Resources:       newtResources,
		VolumeMounts:    spec.ExtraVolumeMounts,
	}

	// Expose the metrics port as a named container port when metrics are configured.
	if spec.Metrics != nil {
		port := spec.Metrics.Port
		if port == 0 {
			port = 9090
		}
		newtContainer.Ports = []corev1.ContainerPort{
			{Name: "metrics", ContainerPort: int32(port), Protocol: corev1.ProtocolTCP},
		}
	}

	var podAnnotations map[string]string
	if len(spec.PodAnnotations) > 0 {
		podAnnotations = make(map[string]string, len(spec.PodAnnotations))
		maps.Copy(podAnnotations, spec.PodAnnotations)
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName:            site.Name + "-newtsite",
		AutomountServiceAccountToken:  new(bool),
		TerminationGracePeriodSeconds: new(int64(30)),
		SecurityContext:               podSecCtx,
		HostNetwork:                   spec.UseNativeInterface && spec.HostNetwork,
		HostPID:                       spec.UseNativeInterface && spec.HostPID,
		InitContainers:                spec.InitContainers,
		Containers:                    append([]corev1.Container{newtContainer}, spec.ExtraContainers...),
		Volumes:                       spec.ExtraVolumes,
		NodeSelector:                  spec.NodeSelector,
		Tolerations:                   spec.Tolerations,
		Affinity:                      spec.Affinity,
		TopologySpreadConstraints:     spec.TopologySpreadConstraints,
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      site.Name,
			Namespace: site.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: podSpec,
			},
		},
	}
}

// buildSecurityContexts returns the container- and pod-level security contexts.
// User-provided overrides in NewtSpec take precedence over operator defaults.
func buildSecurityContexts(spec pangolinv1alpha1.NewtSpec) (*corev1.SecurityContext, *corev1.PodSecurityContext) {
	// If the user provided explicit overrides, use them as-is.
	if spec.SecurityContext != nil || spec.PodSecurityContext != nil {
		return spec.SecurityContext, spec.PodSecurityContext
	}

	if spec.UseNativeInterface {
		runAsRoot := int64(0)
		privileged := true
		allowPrivEsc := true
		containerSecCtx := &corev1.SecurityContext{
			RunAsUser:                &runAsRoot,
			Privileged:               &privileged,
			AllowPrivilegeEscalation: &allowPrivEsc,
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"NET_ADMIN", "SYS_MODULE"},
			},
		}
		// No pod-level non-root or seccomp restrictions in native mode.
		return containerSecCtx, nil
	}

	runAsUser := int64(65534)
	runAsNonRoot := true
	readOnly := true
	allowPrivEsc := false
	containerSecCtx := &corev1.SecurityContext{
		RunAsUser:                &runAsUser,
		RunAsNonRoot:             &runAsNonRoot,
		ReadOnlyRootFilesystem:   &readOnly,
		AllowPrivilegeEscalation: &allowPrivEsc,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
	runAsNonRootPod := true
	podSecCtx := &corev1.PodSecurityContext{
		RunAsNonRoot: &runAsNonRootPod,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	return containerSecCtx, podSecCtx
}
