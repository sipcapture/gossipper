package scenario

import "testing"

func TestLoadNamedInviteMedia(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("invite_media")
	if err != nil {
		t.Fatalf("LoadNamed(invite_media): %v", err)
	}
	if sc.Name != "invite_media" {
		t.Fatalf("scenario name: got %q", sc.Name)
	}
}

func TestLoadNamedInviteMediaEarly(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("invite_media_early")
	if err != nil {
		t.Fatalf("LoadNamed(invite_media_early): %v", err)
	}
	if sc.Name != "invite_media_early" {
		t.Fatalf("scenario name: got %q", sc.Name)
	}
}

func TestLoadNamedInviteMediaEarly180(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("invite_media_early_180")
	if err != nil {
		t.Fatalf("LoadNamed(invite_media_early_180): %v", err)
	}
	if sc.Name != "invite_media_early_180" {
		t.Fatalf("scenario name: got %q", sc.Name)
	}
}

func TestLoadNamedInviteMediaSavpf(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("invite_media_savpf")
	if err != nil {
		t.Fatalf("LoadNamed(invite_media_savpf): %v", err)
	}
	if sc.Name != "invite_media_savpf" {
		t.Fatalf("scenario name: got %q", sc.Name)
	}
}
