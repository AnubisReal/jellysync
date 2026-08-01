package app

import "testing"

func TestConfigStoreSave(t *testing.T) {
	store, err := newConfigStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Mode: "coordinator", NodeName: "Servidor principal", JellyfinURL: "http://jellyfin:8096", JellyfinAPIKey: "secret", AdminPassword: "a-secure-password"}
	if err := store.save(cfg); err != nil {
		t.Fatal(err)
	}
	got := store.get()
	if !got.Configured || got.NodeName != cfg.NodeName {
		t.Fatalf("unexpected config: %#v", got)
	}
}

func TestNodeRequiresCoordinator(t *testing.T) {
	store, err := newConfigStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.save(Config{Mode: "node", NodeName: "Nodo", JellyfinURL: "http://jellyfin:8096", JellyfinAPIKey: "secret", AdminPassword: "a-secure-password"}); err == nil {
		t.Fatal("expected coordinator validation error")
	}
}

func TestPublicConfigDoesNotExposeAPIKey(t *testing.T) {
	store, err := newConfigStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.save(Config{Mode: "coordinator", NodeName: "Nodo", JellyfinURL: "http://jellyfin:8096", JellyfinAPIKey: "secret", AdminPassword: "a-secure-password"}); err != nil {
		t.Fatal(err)
	}
	public := store.public(false)
	if !public.JellyfinConnected {
		t.Fatal("expected Jellyfin to be marked connected")
	}
}
