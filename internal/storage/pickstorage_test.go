package storage

import (
	"bytes"
	"strings"
	"testing"
)

func TestPickStorage(t *testing.T) {
	cases := []struct {
		name             string
		storageType      string
		keyringAvailable bool
		wantBackend      string
		wantWarn         []string // substrings that must appear in the warning output
		wantNoWarn       bool     // nothing should be written
	}{
		{
			name:             "default unset, keyring available",
			storageType:      "",
			keyringAvailable: true,
			wantBackend:      "keyring",
			wantNoWarn:       true,
		},
		{
			name:             "default unset, keyring unavailable -> warn fallback",
			storageType:      "",
			keyringAvailable: false,
			wantBackend:      "file",
			wantWarn: []string{
				"system keyring is not available",
				"plaintext file",
				"auth.storage: file",
			},
		},
		{
			name:             "explicit keyring, available",
			storageType:      "keyring",
			keyringAvailable: true,
			wantBackend:      "keyring",
			wantNoWarn:       true,
		},
		{
			name:             "explicit keyring, unavailable -> warn",
			storageType:      "keyring",
			keyringAvailable: false,
			wantBackend:      "file",
			wantWarn:         []string{"system keyring is not available"},
		},
		{
			name:             "explicit file, no warning",
			storageType:      "file",
			keyringAvailable: true,
			wantBackend:      "file",
			wantNoWarn:       true,
		},
		{
			name:             "explicit file is silent even when keyring is unavailable",
			storageType:      "file",
			keyringAvailable: false,
			wantBackend:      "file",
			wantNoWarn:       true,
		},
		{
			name:             "unknown value -> file with warning",
			storageType:      "keychain", // common typo for "keyring"
			keyringAvailable: true,
			wantBackend:      "file",
			wantWarn: []string{
				`unknown auth.storage value "keychain"`,
				"plaintext file storage",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			got := pickStorage(tc.storageType, "", tc.keyringAvailable, &buf)

			var gotName string
			switch got.(type) {
			case *KeyringStorage:
				gotName = "keyring"
			case *FileStorage:
				gotName = "file"
			default:
				t.Fatalf("unexpected storage type %T", got)
			}
			if gotName != tc.wantBackend {
				t.Errorf("backend = %s, want %s", gotName, tc.wantBackend)
			}

			out := buf.String()
			if tc.wantNoWarn {
				if out != "" {
					t.Errorf("expected no warning, got: %q", out)
				}
				return
			}
			for _, want := range tc.wantWarn {
				if !strings.Contains(out, want) {
					t.Errorf("warning missing %q; full output:\n%s", want, out)
				}
			}
		})
	}
}
