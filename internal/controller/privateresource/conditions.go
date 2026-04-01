package privateresource

import "github.com/home-operations/pangolin-operator/internal/controller/shared"

const (
	conditionReady       = shared.ConditionReady
	reasonReconciled     = shared.ReasonReconciled
	reasonPending        = shared.ReasonPending
	reasonError          = shared.ReasonError
	reasonPermanentError = shared.ReasonPermanentError
)
