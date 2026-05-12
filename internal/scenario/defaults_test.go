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
