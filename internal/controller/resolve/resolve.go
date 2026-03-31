// Package resolve provides helpers for looking up NewtSite objects by name
// using a field index registered at manager startup.
package resolve

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
)

const IndexField = "metadata.name"

var ErrNotFound = errors.New("NewtSite not found")

func Site(ctx context.Context, c client.Client, name string) (*pangolinv1alpha1.NewtSite, error) {
	var list pangolinv1alpha1.NewtSiteList
	if err := c.List(ctx, &list, client.MatchingFields{IndexField: name}); err != nil {
		return nil, fmt.Errorf("list NewtSites by name %q: %w", name, err)
	}

	if len(list.Items) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	return &list.Items[0], nil
}
