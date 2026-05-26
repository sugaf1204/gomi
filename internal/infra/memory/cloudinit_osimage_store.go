package memory

import (
	"context"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
	"sort"
)

type CloudInitStore struct{ b *Backend }

var _ cloudinit.Store = (*CloudInitStore)(nil)

func (s *CloudInitStore) Upsert(_ context.Context, t cloudinit.CloudInitTemplate) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.cloudInits[t.Name] = t
	return nil
}

func (s *CloudInitStore) Get(_ context.Context, name string) (cloudinit.CloudInitTemplate, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	t, ok := s.b.cloudInits[name]
	if !ok {
		return cloudinit.CloudInitTemplate{}, resource.ErrNotFound
	}
	return t, nil
}

func (s *CloudInitStore) List(_ context.Context) ([]cloudinit.CloudInitTemplate, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]cloudinit.CloudInitTemplate, 0, len(s.b.cloudInits))
	for _, t := range s.b.cloudInits {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *CloudInitStore) Delete(_ context.Context, name string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if _, ok := s.b.cloudInits[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.b.cloudInits, name)
	return nil
}

// --- OSImageStore ---

type OSImageStore struct{ b *Backend }

var _ osimage.Store = (*OSImageStore)(nil)

func (s *OSImageStore) Upsert(_ context.Context, img osimage.OSImage) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.osimages[img.Name] = img
	return nil
}

func (s *OSImageStore) Get(_ context.Context, name string) (osimage.OSImage, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	img, ok := s.b.osimages[name]
	if !ok {
		return osimage.OSImage{}, resource.ErrNotFound
	}
	return img, nil
}

func (s *OSImageStore) List(_ context.Context) ([]osimage.OSImage, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]osimage.OSImage, 0, len(s.b.osimages))
	for _, img := range s.b.osimages {
		out = append(out, img)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *OSImageStore) Delete(_ context.Context, name string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if _, ok := s.b.osimages[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.b.osimages, name)
	return nil
}

// --- DHCPLeaseStore ---
