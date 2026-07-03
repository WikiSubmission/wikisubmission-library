package handlers

import "testing"

func TestValidatePublicStoreKey(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"offline manifest", "offline/manifest.json", "offline/manifest.json", false},
		{"offline bundle", "offline/quran-en-v1.db", "offline/quran-en-v1.db", false},
		{"leading slash stripped", "/offline/manifest.json", "offline/manifest.json", false},
		{"bare prefix rejected", "offline/", "", true},
		{"private prefix rejected", "private/exports/user-1/export.json", "", true},
		{"root key rejected", "manifest.json", "", true},
		{"traversal rejected", "offline/../private/x", "", true},
		{"prefix-lookalike rejected", "offline-evil/x", "", true},
		{"empty rejected", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validatePublicStoreKey(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got key %q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("key mismatch: got %q, want %q", got, tc.want)
			}
		})
	}
}
