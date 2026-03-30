// Package resolve provides helpers for looking up NewtSite objects by name
// across all namespaces using a field index registered at manager startup.
package resolve

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
)

const IndexField = "metadata.name"

var (
	ErrNotFound  = errors.New("NewtSite not found")
	ErrAmbiguous = errors.New("NewtSite name is ambiguous: found in multiple namespaces")
)

func Site(ctx context.Context, c client.Client, name string) (*pangolinv1alpha1.NewtSite, error) {
	var list pangolinv1alpha1.NewtSiteList
	if err := c.List(ctx, &list, client.MatchingFields{IndexField: name}); err != nil {
		return nil, fmt.Errorf("list NewtSites by name %q: %w", name, err)
	}

	switch len(list.Items) {
	case 0:
		return nil, fmt.Errorf("%w: %q", ErrNotFound, name)
	case 1:
		return &list.Items[0], nil
	default:
		namespaces := make([]string, len(list.Items))
		for i, s := range list.Items {
			namespaces[i] = s.Namespace
		}
		return nil, fmt.Errorf("%w: %q exists in namespaces %v", ErrAmbiguous, name, namespaces)
	}
}
