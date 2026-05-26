package sql_test

import (
	"context"
	"errors"
	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/subnet"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubnetStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.Subnets()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	sub := subnet.Subnet{
		Name:      "main",
		Spec:      subnet.SubnetSpec{CIDR: "10.0.0.0/24", DefaultGateway: "10.0.0.1"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.Upsert(ctx, sub); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.Get(ctx, "main")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.CIDR != "10.0.0.0/24" {
		t.Errorf("CIDR = %q, want 10.0.0.0/24", got.Spec.CIDR)
	}
}

func TestSubnetChangeNotifier(t *testing.T) {
	b := newTestBackend(t)
	s := b.Subnets()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	var called atomic.Int32
	s.Subscribe(func() { called.Add(1) })

	sub := subnet.Subnet{
		Name:      "test",
		Spec:      subnet.SubnetSpec{CIDR: "10.1.0.0/24"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.Upsert(ctx, sub); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Give goroutine time to fire
	time.Sleep(50 * time.Millisecond)
	if called.Load() == 0 {
		t.Error("ChangeNotifier was not called on Upsert")
	}
}

func TestAuthStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.Auth()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// User
	user := auth.User{Username: "admin", PasswordHash: "hash", Role: auth.RoleAdmin, CreatedAt: now}
	if err := s.UpsertUser(ctx, user); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	got, err := s.GetUser(ctx, "admin")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Role != auth.RoleAdmin {
		t.Errorf("role = %q, want admin", got.Role)
	}

	// Session
	sess := auth.Session{Token: "tok1", Username: "admin", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	gotSess, err := s.GetSession(ctx, "tok1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if gotSess.Username != "admin" {
		t.Errorf("session username = %q", gotSess.Username)
	}

	// Expired session
	expired := auth.Session{Token: "tok2", Username: "admin", CreatedAt: now, ExpiresAt: now.Add(-time.Hour)}
	_ = s.CreateSession(ctx, expired)
	_, err = s.GetSession(ctx, "tok2")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("expired session: got %v, want ErrNotFound", err)
	}

	// AuditEvent
	event := auth.AuditEvent{
		ID: "ev1", Machine: "srv1",
		Action: "create", Actor: "admin", Result: "ok",
		CreatedAt: now,
	}
	if err := s.CreateAuditEvent(ctx, event); err != nil {
		t.Fatalf("CreateAuditEvent: %v", err)
	}
	events, err := s.ListAuditEvents(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("events len = %d, want 1", len(events))
	}
}

func TestSSHKeyStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.SSHKeys()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	k := sshkey.SSHKey{
		Name:      "key1",
		PublicKey: "ssh-ed25519 AAAA...",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Upsert(ctx, k); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PublicKey != k.PublicKey {
		t.Errorf("PublicKey = %q, want %q", got.PublicKey, k.PublicKey)
	}

	keys, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("List len = %d, want 1", len(keys))
	}
}
