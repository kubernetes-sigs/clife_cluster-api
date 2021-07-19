package fieldowner

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Wrap wraps a Client with one that sets the field owner.
func Wrap(upstream client.Client, owner string) client.Client {
	return &clientWithOwner{Client: upstream, owner: client.FieldOwner(owner)}
}

type clientWithOwner struct {
	client.Client
	owner client.FieldOwner
}

func (c *clientWithOwner) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return c.Client.Create(ctx, obj, append(opts, c.owner)...)
}

func (c *clientWithOwner) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return c.Client.Update(ctx, obj, append(opts, c.owner)...)
}

func (c *clientWithOwner) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return c.Client.Patch(ctx, obj, patch, append(opts, c.owner)...)
}

func (c *clientWithOwner) Status() client.StatusWriter {
	return &statusWriterWithOwner{StatusWriter: c.Client.Status(), owner: c.owner}
}

type statusWriterWithOwner struct {
	client.StatusWriter
	owner client.FieldOwner
}

func (s *statusWriterWithOwner) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return s.StatusWriter.Update(ctx, obj, append(opts, s.owner)...)
}

func (s *statusWriterWithOwner) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return s.StatusWriter.Patch(ctx, obj, patch, append(opts, s.owner)...)
}
