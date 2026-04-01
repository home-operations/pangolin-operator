package shared

import (
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ResyncInterval is the default periodic reconciliation interval.
	ResyncInterval = 10 * time.Minute

	// ReconcileTimeout is the context timeout for a single reconcile call.
	ReconcileTimeout = 2 * time.Minute

	// Requeue intervals for common scenarios.
	RequeueDependencyWait = 10 * time.Second
	RequeuePermanentError = 5 * time.Minute

	// ConditionReady is the condition type used by all controllers.
	ConditionReady = "Ready"

	// Condition reasons shared across controllers.
	ReasonReconciled     = "Reconciled"
	ReasonPending        = "Pending"
	ReasonError          = "Error"
	ReasonPermanentError = "PermanentError"

	// Label keys used by autodiscover-owned resources.
	LabelOwnerKind = "pangolin.home-operations.com/owner-kind"
	LabelOwnerName = "pangolin.home-operations.com/owner-name"
	LabelSite      = "pangolin.home-operations.com/site"
	LabelManagedBy = "app.kubernetes.io/managed-by"

	// Site types.
	SiteTypeLocal = "local"
	SiteTypeNewt  = "newt"
)

// SetCondition sets a Ready condition on the given conditions slice.
// Returns true if the condition was substantively changed.
func SetCondition(conditions *[]metav1.Condition, status metav1.ConditionStatus, reason, message string, generation int64) bool {
	return apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             status,
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
	})
}
