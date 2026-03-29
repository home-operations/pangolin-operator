package newtsite

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
)

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
	if spec.Mtu > 0 {
		env = append(env, corev1.EnvVar{
			Name:  "MTU",
			Value: fmt.Sprintf("%d", spec.Mtu),
		})
	}

	newtResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10m"),
			corev1.ResourceMemory: resource.MustParse("32Mi"),
		},
	}
	if spec.Resources != nil {
		newtResources = *spec.Resources
	}

	var containerSecCtx *corev1.SecurityContext
	var podSecCtx *corev1.PodSecurityContext

	if spec.UseNativeInterface {
		runAsRoot := int64(0)
		privileged := true
		allowPrivEsc := true
		containerSecCtx = &corev1.SecurityContext{
			RunAsUser:                &runAsRoot,
			Privileged:               &privileged,
			AllowPrivilegeEscalation: &allowPrivEsc,
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"NET_ADMIN", "SYS_MODULE"},
			},
		}
		// No pod-level non-root or seccomp restrictions in native mode.
		podSecCtx = nil
	} else {
		runAsUser := int64(65534)
		runAsNonRoot := true
		readOnly := true
		allowPrivEsc := false
		containerSecCtx = &corev1.SecurityContext{
			RunAsUser:                &runAsUser,
			RunAsNonRoot:             &runAsNonRoot,
			ReadOnlyRootFilesystem:   &readOnly,
			AllowPrivilegeEscalation: &allowPrivEsc,
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		}
		runAsNonRootPod := true
		podSecCtx = &corev1.PodSecurityContext{
			RunAsNonRoot: &runAsNonRootPod,
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		}
	}

	newtContainer := corev1.Container{
		Name:            "newt",
		Image:           image + ":" + tag,
		Env:             env,
		SecurityContext: containerSecCtx,
		Resources:       newtResources,
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName:            site.Name + "-newtsite",
		AutomountServiceAccountToken:  new(bool),
		TerminationGracePeriodSeconds: new(int64(30)),
		SecurityContext:               podSecCtx,
		HostNetwork:                   spec.UseNativeInterface && spec.HostNetwork,
		HostPID:                       spec.UseNativeInterface && spec.HostPID,
		Containers:                    []corev1.Container{newtContainer},
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
					Labels: labels,
				},
				Spec: podSpec,
			},
		},
	}
}
