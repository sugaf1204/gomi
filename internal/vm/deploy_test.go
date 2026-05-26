package vm

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

type fakeCloudImageStorage struct {
	exists            bool
	existsByName      map[string]bool
	createdName       string
	format            string
	sizeBytes         int64
	data              []byte
	deletedName       string
	deleteErr         error
	createHadDeadline bool
}

type concurrentCloudImageStorage struct {
	mu          sync.Mutex
	volumes     map[string]bool
	inflight    map[string]bool
	createCalls int
}

func (s *concurrentCloudImageStorage) VolumeExists(_ context.Context, name string, format string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.volumes[name+"."+format], nil
}

func (s *concurrentCloudImageStorage) CreateVolumeFromReader(_ context.Context, name string, sizeBytes int64, format string, r io.Reader) error {
	key := name + "." + format
	s.mu.Lock()
	if s.volumes[key] || s.inflight[key] {
		s.mu.Unlock()
		return errors.New("duplicate backing volume create")
	}
	s.inflight[key] = true
	s.mu.Unlock()

	if _, err := io.Copy(io.Discard, r); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.inflight, key)
	s.volumes[key] = true
	s.createCalls++
	s.mu.Unlock()
	return nil
}

func (s *concurrentCloudImageStorage) DeleteVolume(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.volumes, name+".qcow2")
	return nil
}

func (s *fakeCloudImageStorage) VolumeExists(_ context.Context, name string, format string) (bool, error) {
	s.createdName = name
	s.format = format
	if s.existsByName != nil {
		return s.existsByName[cloudImageVolumeName(name, format)], nil
	}
	return s.exists, nil
}

func (s *fakeCloudImageStorage) CreateVolumeFromReader(ctx context.Context, name string, sizeBytes int64, format string, r io.Reader) error {
	s.createdName = name
	s.format = format
	s.sizeBytes = sizeBytes
	_, s.createHadDeadline = ctx.Deadline()
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.data = data
	return nil
}

func (s *fakeCloudImageStorage) DeleteVolume(_ context.Context, name string) error {
	s.deletedName = name
	return s.deleteErr
}

type testOSImageStore struct {
	items map[string]osimage.OSImage
}

func (s *testOSImageStore) Upsert(_ context.Context, img osimage.OSImage) error {
	if s.items == nil {
		s.items = map[string]osimage.OSImage{}
	}
	s.items[img.Name] = img
	return nil
}

func (s *testOSImageStore) Get(_ context.Context, name string) (osimage.OSImage, error) {
	img, ok := s.items[name]
	if !ok {
		return osimage.OSImage{}, resource.ErrNotFound
	}
	return img, nil
}

func (s *testOSImageStore) List(context.Context) ([]osimage.OSImage, error) {
	out := make([]osimage.OSImage, 0, len(s.items))
	for _, img := range s.items {
		out = append(out, img)
	}
	return out, nil
}

func (s *testOSImageStore) Delete(_ context.Context, name string) error {
	if _, ok := s.items[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.items, name)
	return nil
}
