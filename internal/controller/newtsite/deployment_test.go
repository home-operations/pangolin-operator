package newtsite

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
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
	d := buildDeployment(site, "my-site-newt-credentials")

	if d.Name != "my-site" {
		t.Errorf("expected name 'my-site', got %q", d.Name)
	}
	if d.Namespace != "default" {
		t.Errorf("expected namespace 'default', got %q", d.Namespace)
	}
	if *d.Spec.Replicas != 1 {
		t.Errorf("expected 1 replica, got %d", *d.Spec.Replicas)
	}
	if len(d.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(d.Spec.Template.Spec.Containers))
	}
	c := d.Spec.Template.Spec.Containers[0]
	if c.Name != "newt" {
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

	d := buildDeployment(site, "my-site-newt-credentials")
	c := d.Spec.Template.Spec.Containers[0]
	if c.Image != "my-registry/newt:v1.2.3" {
		t.Errorf("unexpected image: %q", c.Image)
	}
}

func TestBuildDeployment_CustomReplicas(t *testing.T) {
	site := newTestSite()
	var r int32 = 0
	site.Spec.Newt.Replicas = &r

	d := buildDeployment(site, "my-site-newt-credentials")
	if *d.Spec.Replicas != 0 {
		t.Errorf("expected 0 replicas, got %d", *d.Spec.Replicas)
	}
}

func TestBuildDeployment_EnvVarsFromSecret(t *testing.T) {
	site := newTestSite()
	d := buildDeployment(site, "my-site-newt-credentials")
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

	d := buildDeployment(site, "my-site-newt-credentials")
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
	d := buildDeployment(site, "my-site-newt-credentials")
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

	d := buildDeployment(site, "my-site-newt-credentials")
	c := d.Spec.Template.Spec.Containers[0]

	cpu := c.Resources.Requests[corev1.ResourceCPU]
	if cpu.String() != "100m" {
		t.Errorf("expected CPU request 100m, got %s", cpu.String())
	}
}

func TestBuildDeployment_NativeInterface_SecurityContext(t *testing.T) {
	site := newTestSite()
	site.Spec.Newt.UseNativeInterface = true

	d := buildDeployment(site, "my-site-newt-credentials")
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

	d := buildDeployment(site, "my-site-newt-credentials")

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

	d := buildDeployment(site, "my-site-newt-credentials")

	if d.Spec.Template.Spec.HostNetwork {
		t.Error("expected HostNetwork=false when UseNativeInterface is false")
	}
	if d.Spec.Template.Spec.HostPID {
		t.Error("expected HostPID=false when UseNativeInterface is false")
	}
}

func TestBuildDeployment_Labels(t *testing.T) {
	site := newTestSite()
	d := buildDeployment(site, "my-site-newt-credentials")

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
