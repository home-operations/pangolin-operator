package newtsite

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
)

const (
	containerName = "newt"
	debugLevel    = "DEBUG"
)

func newTestSite() *pangolinv1alpha1.NewtSite {
	return &pangolinv1alpha1.NewtSite{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-site",
			Namespace: "default",
		},
		Spec: pangolinv1alpha1.NewtSiteSpec{
			Name: "my-site",
		},
	}
}

func TestBuildDeployment_Defaults(t *testing.T) {
	site := newTestSite()
	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")

	if d.Name != "my-site" {
		t.Errorf("expected name 'my-site', got %q", d.Name)
	}
	if d.Namespace != "test-ns" {
		t.Errorf("expected namespace 'test-ns', got %q", d.Namespace)
	}
	if *d.Spec.Replicas != 1 {
		t.Errorf("expected 1 replica, got %d", *d.Spec.Replicas)
	}
	if len(d.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(d.Spec.Template.Spec.Containers))
	}
	c := d.Spec.Template.Spec.Containers[0]
	if c.Name != containerName {
		t.Errorf("expected container name 'newt', got %q", c.Name)
	}
	if c.Image != "ghcr.io/fosrl/newt:latest" {
		t.Errorf("unexpected default image: %q", c.Image)
	}
	if len(d.Spec.Template.Spec.InitContainers) != 0 {
		t.Errorf("expected no init containers without sidecar, got %d", len(d.Spec.Template.Spec.InitContainers))
	}
}

func TestBuildDeployment_CustomImageAndTag(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.Image = "my-registry/newt"
	site.Spec.Newt.Tag = "v1.2.3"

	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")
	c := d.Spec.Template.Spec.Containers[0]
	if c.Image != "my-registry/newt:v1.2.3" {
		t.Errorf("unexpected image: %q", c.Image)
	}
}

func TestBuildDeployment_CustomReplicas(t *testing.T) {
	site := newTestSite()
	var r int32 = 0
	site.Spec.Newt.Replicas = &r

	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")
	if *d.Spec.Replicas != 0 {
		t.Errorf("expected 0 replicas, got %d", *d.Spec.Replicas)
	}
}

func TestBuildDeployment_EnvVarsFromSecret(t *testing.T) {
	site := newTestSite()
	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")
	c := d.Spec.Template.Spec.Containers[0]

	keys := make(map[string]bool)
	for _, e := range c.Env {
		if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			keys[e.Name] = true
			if e.ValueFrom.SecretKeyRef.Name != "my-site-newt-credentials" {
				t.Errorf("env %s references wrong secret: %q", e.Name, e.ValueFrom.SecretKeyRef.Name)
			}
		}
	}
	for _, required := range []string{"PANGOLIN_ENDPOINT", "NEWT_ID", "NEWT_SECRET"} {
		if !keys[required] {
			t.Errorf("missing env var from secret: %s", required)
		}
	}
}

func TestBuildDeployment_MtuEnvVar(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.Mtu = 1420

	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")
	c := d.Spec.Template.Spec.Containers[0]

	found := false
	for _, e := range c.Env {
		if e.Name == "MTU" && e.Value == "1420" {
			found = true
		}
	}
	if !found {
		t.Error("expected MTU env var to be set to 1420")
	}
}

func TestBuildDeployment_SecurityContext(t *testing.T) {
	site := newTestSite()
	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")
	c := d.Spec.Template.Spec.Containers[0]

	if c.SecurityContext == nil {
		t.Fatal("expected SecurityContext to be set")
	}
	if c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
		t.Error("expected AllowPrivilegeEscalation=false")
	}
	if c.SecurityContext.RunAsNonRoot == nil || !*c.SecurityContext.RunAsNonRoot {
		t.Error("expected RunAsNonRoot=true")
	}
}

func TestBuildDeployment_CustomResources(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.Resources = &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}

	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")
	c := d.Spec.Template.Spec.Containers[0]

	cpu := c.Resources.Requests[corev1.ResourceCPU]
	if cpu.String() != "100m" {
		t.Errorf("expected CPU request 100m, got %s", cpu.String())
	}
}

func TestBuildDeployment_NativeInterface_SecurityContext(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.UseNativeInterface = true

	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")
	c := d.Spec.Template.Spec.Containers[0]

	if c.SecurityContext == nil {
		t.Fatal("expected SecurityContext to be set")
	}
	if c.SecurityContext.Privileged == nil || !*c.SecurityContext.Privileged {
		t.Error("expected Privileged=true in native mode")
	}
	if c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser != 0 {
		t.Error("expected RunAsUser=0 in native mode")
	}
	hasNetAdmin, hasSysModule := false, false
	for _, cap := range c.SecurityContext.Capabilities.Add {
		if cap == "NET_ADMIN" {
			hasNetAdmin = true
		}
		if cap == "SYS_MODULE" {
			hasSysModule = true
		}
	}
	if !hasNetAdmin {
		t.Error("expected NET_ADMIN capability in native mode")
	}
	if !hasSysModule {
		t.Error("expected SYS_MODULE capability in native mode")
	}
	if d.Spec.Template.Spec.SecurityContext != nil {
		t.Error("expected no pod-level SecurityContext in native mode")
	}
}

func TestBuildDeployment_NativeInterface_HostNetworkAndPID(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.UseNativeInterface = true
	site.Spec.Newt.HostNetwork = true
	site.Spec.Newt.HostPID = true

	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")

	if !d.Spec.Template.Spec.HostNetwork {
		t.Error("expected HostNetwork=true")
	}
	if !d.Spec.Template.Spec.HostPID {
		t.Error("expected HostPID=true")
	}
}

func TestBuildDeployment_NativeInterface_HostNetworkIgnoredWithoutNative(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.HostNetwork = true
	site.Spec.Newt.HostPID = true

	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")

	if d.Spec.Template.Spec.HostNetwork {
		t.Error("expected HostNetwork=false when UseNativeInterface is false")
	}
	if d.Spec.Template.Spec.HostPID {
		t.Error("expected HostPID=false when UseNativeInterface is false")
	}
}

func TestBuildDeployment_Labels(t *testing.T) {
	site := newTestSite()
	d := buildDeployment(site, "my-site-newt-credentials", "test-ns")

	labels := d.Labels
	if labels["app.kubernetes.io/name"] != "newtsite" {
		t.Errorf("unexpected app.kubernetes.io/name: %q", labels["app.kubernetes.io/name"])
	}
	if labels["app.kubernetes.io/instance"] != "my-site" {
		t.Errorf("unexpected app.kubernetes.io/instance: %q", labels["app.kubernetes.io/instance"])
	}

	// Selector should match pod labels
	for k, v := range d.Spec.Selector.MatchLabels {
		if d.Spec.Template.Labels[k] != v {
			t.Errorf("selector label %s=%s not found in pod template labels", k, v)
		}
	}
}

func findEnv(envs []corev1.EnvVar, name string) (corev1.EnvVar, bool) {
	for _, e := range envs {
		if e.Name == name {
			return e, true
		}
	}
	return corev1.EnvVar{}, false
}

func TestBuildDeployment_OptionalEnvVars(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(s *pangolinv1alpha1.NewtSite)
		envName string
		envVal  string
	}{
		{"PingInterval", func(s *pangolinv1alpha1.NewtSite) { s.Spec.Newt.PingInterval = "60s" }, "PING_INTERVAL", "60s"},
		{"PingTimeout", func(s *pangolinv1alpha1.NewtSite) { s.Spec.Newt.PingTimeout = "5s" }, "PING_TIMEOUT", "5s"},
		{"DNS", func(s *pangolinv1alpha1.NewtSite) { s.Spec.Newt.DNS = "1.1.1.1" }, "DNS", "1.1.1.1"},
		{"AcceptClients", func(s *pangolinv1alpha1.NewtSite) { s.Spec.Newt.AcceptClients = true }, "ACCEPT_CLIENTS", "true"},
		{"Interface", func(s *pangolinv1alpha1.NewtSite) { s.Spec.Newt.Interface = "wg0" }, "INTERFACE", "wg0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := newTestSite()
			tt.mutate(site)
			d := buildDeployment(site, "creds", "test-ns")
			env, ok := findEnv(d.Spec.Template.Spec.Containers[0].Env, tt.envName)
			if !ok {
				t.Fatalf("expected env var %s to be set", tt.envName)
			}
			if env.Value != tt.envVal {
				t.Errorf("expected %s=%q, got %q", tt.envName, tt.envVal, env.Value)
			}
		})
	}
}

func TestBuildDeployment_DefaultInterfaceOmitted(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.Interface = "newt" // default value
	d := buildDeployment(site, "creds", "test-ns")
	if _, ok := findEnv(d.Spec.Template.Spec.Containers[0].Env, "INTERFACE"); ok {
		t.Error("expected INTERFACE env var to be omitted when set to default 'newt'")
	}
}

func TestBuildDeployment_Metrics(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.Metrics = &pangolinv1alpha1.NewtMetricsSpec{Port: 8080}
	d := buildDeployment(site, "creds", "test-ns")
	c := d.Spec.Template.Spec.Containers[0]

	env, ok := findEnv(c.Env, "NEWT_ADMIN_ADDR")
	if !ok {
		t.Fatal("expected NEWT_ADMIN_ADDR env var")
	}
	if env.Value != "0.0.0.0:8080" {
		t.Errorf("expected NEWT_ADMIN_ADDR=0.0.0.0:8080, got %q", env.Value)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != 8080 {
		t.Errorf("expected metrics port 8080, got %v", c.Ports)
	}
}

func TestBuildDeployment_MetricsCustomAdminAddr(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.Metrics = &pangolinv1alpha1.NewtMetricsSpec{AdminAddr: "127.0.0.1:9999"}
	d := buildDeployment(site, "creds", "test-ns")
	env, ok := findEnv(d.Spec.Template.Spec.Containers[0].Env, "NEWT_ADMIN_ADDR")
	if !ok {
		t.Fatal("expected NEWT_ADMIN_ADDR env var")
	}
	if env.Value != "127.0.0.1:9999" {
		t.Errorf("expected custom admin addr, got %q", env.Value)
	}
}

func TestBuildDeployment_ExtraEnvOverrides(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.ExtraEnv = []corev1.EnvVar{{Name: "LOG_LEVEL", Value: debugLevel}}
	d := buildDeployment(site, "creds", "test-ns")
	envs := d.Spec.Template.Spec.Containers[0].Env
	// The last LOG_LEVEL should be the override.
	last := ""
	for _, e := range envs {
		if e.Name == "LOG_LEVEL" {
			last = e.Value
		}
	}
	if last != debugLevel {
		t.Errorf("expected ExtraEnv to override LOG_LEVEL to DEBUG, got %q", last)
	}
}

func TestBuildDeployment_PodAnnotations(t *testing.T) {
	site := newTestSite()
	d := buildDeployment(site, "creds", "test-ns")
	if d.Spec.Template.Annotations != nil {
		t.Error("expected nil annotations when PodAnnotations is empty")
	}

	site.Spec.Newt.PodAnnotations = map[string]string{"custom": "value"}
	d = buildDeployment(site, "creds", "test-ns")
	if d.Spec.Template.Annotations["custom"] != "value" {
		t.Errorf("expected custom annotation, got %v", d.Spec.Template.Annotations)
	}
}

func TestBuildDeployment_SchedulingFields(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.NodeSelector = map[string]string{"zone": "us-east"}
	site.Spec.Newt.Tolerations = []corev1.Toleration{{Key: "key", Operator: corev1.TolerationOpExists}}
	site.Spec.Newt.Affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{},
	}

	d := buildDeployment(site, "creds", "test-ns")
	pod := d.Spec.Template.Spec

	if pod.NodeSelector["zone"] != "us-east" {
		t.Error("expected NodeSelector to be set")
	}
	if len(pod.Tolerations) != 1 {
		t.Error("expected 1 toleration")
	}
	if pod.Affinity == nil || pod.Affinity.NodeAffinity == nil {
		t.Error("expected Affinity to be set")
	}
}

func TestBuildDeployment_ExtraContainersAndVolumes(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.InitContainers = []corev1.Container{{Name: "init"}}
	site.Spec.Newt.ExtraContainers = []corev1.Container{{Name: "sidecar"}}
	site.Spec.Newt.ExtraVolumes = []corev1.Volume{{Name: "data"}}
	site.Spec.Newt.ExtraVolumeMounts = []corev1.VolumeMount{{Name: "data", MountPath: "/data"}}

	d := buildDeployment(site, "creds", "test-ns")
	pod := d.Spec.Template.Spec

	if len(pod.InitContainers) != 1 || pod.InitContainers[0].Name != "init" {
		t.Error("expected init container")
	}
	if len(pod.Containers) != 2 || pod.Containers[1].Name != "sidecar" {
		t.Error("expected sidecar container")
	}
	if len(pod.Volumes) != 1 {
		t.Error("expected extra volume")
	}
	if len(pod.Containers[0].VolumeMounts) != 1 {
		t.Error("expected extra volume mount on newt container")
	}
}

func TestBuildDeployment_CustomSecurityContextOverride(t *testing.T) {
	site := newTestSite()
	customUser := int64(1000)
	site.Spec.Newt.SecurityContext = &corev1.SecurityContext{RunAsUser: &customUser}

	d := buildDeployment(site, "creds", "test-ns")
	c := d.Spec.Template.Spec.Containers[0]

	if c.SecurityContext == nil || c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser != 1000 {
		t.Error("expected custom SecurityContext to be used")
	}
	if d.Spec.Template.Spec.SecurityContext != nil {
		t.Error("expected no pod SecurityContext when container override is provided")
	}
}

func TestBuildDeployment_DefaultSecurityContext_DropAllCaps(t *testing.T) {
	site := newTestSite()
	d := buildDeployment(site, "creds", "test-ns")
	c := d.Spec.Template.Spec.Containers[0]

	if c.SecurityContext.Capabilities == nil || len(c.SecurityContext.Capabilities.Drop) == 0 {
		t.Fatal("expected capabilities to be set")
	}
	if c.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Errorf("expected Drop ALL, got %v", c.SecurityContext.Capabilities.Drop)
	}
	if c.SecurityContext.ReadOnlyRootFilesystem == nil || !*c.SecurityContext.ReadOnlyRootFilesystem {
		t.Error("expected ReadOnlyRootFilesystem=true")
	}
}

func TestBuildDeployment_DefaultSecurityContext_PodLevel(t *testing.T) {
	site := newTestSite()
	d := buildDeployment(site, "creds", "test-ns")
	ps := d.Spec.Template.Spec.SecurityContext

	if ps == nil {
		t.Fatal("expected pod SecurityContext")
	}
	if ps.RunAsNonRoot == nil || !*ps.RunAsNonRoot {
		t.Error("expected pod RunAsNonRoot=true")
	}
	if ps.SeccompProfile == nil || ps.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Error("expected seccomp RuntimeDefault")
	}
}

func TestBuildDeployment_LogLevel(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.LogLevel = debugLevel
	d := buildDeployment(site, "creds", "test-ns")
	env, ok := findEnv(d.Spec.Template.Spec.Containers[0].Env, "LOG_LEVEL")
	if !ok {
		t.Fatal("expected LOG_LEVEL env var")
	}
	if env.Value != debugLevel {
		t.Errorf("expected LOG_LEVEL=DEBUG, got %q", env.Value)
	}
}
