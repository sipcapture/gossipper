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

func TestLoadNamedInviteMediaScale(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("invite_media_scale")
	if err != nil {
		t.Fatalf("LoadNamed(invite_media_scale): %v", err)
	}
	if sc.Name != "invite_media_scale" {
		t.Fatalf("scenario name = %q", sc.Name)
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

func TestLoadNamedManagement(t *testing.T) {
	t.Parallel()
	sc, err := LoadNamed("management")
	if err != nil {
		t.Fatalf("LoadNamed(management): %v", err)
	}
	if sc.Name != "management" {
		t.Fatalf("scenario name: got %q", sc.Name)
	}
	if sc.Mode != ModeServer {
		t.Fatalf("mode: got %q want server", sc.Mode)
	}
}
