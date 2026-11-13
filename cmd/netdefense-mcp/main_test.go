package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Any argument used to start the server on the default config, whose server
// is production: --help waited on stdin as an MCP server. Help and the version
// answer and exit, --conf picks the config, and anything else is refused, all
// before a config is loaded.
func TestParseArgs(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "dev.yaml")
	if err := os.WriteFile(conf, []byte("controlplane:\n  host: https://dev.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "typo.yaml")
	for _, tc := range []struct {
		name     string
		args     []string
		wantExit bool
		wantErr  string
		wantOut  string
		wantConf string
	}{
		{name: "no arguments", args: nil},
		{name: "--help", args: []string{"--help"}, wantExit: true, wantOut: "--conf <file>"},
		{name: "-h", args: []string{"-h"}, wantExit: true, wantOut: "Usage:"},
		{name: "--version", args: []string{"--version"}, wantExit: true, wantOut: "netdefense-mcp version "},
		{name: "--conf", args: []string{"--conf", conf}, wantConf: conf},
		{name: "--conf=", args: []string{"--conf=" + conf}, wantConf: conf},
		{name: "--conf of a file that is not there", args: []string{"--conf", missing}, wantErr: "typo.yaml"},
		{name: "an empty --conf=", args: []string{"--conf="}, wantErr: "--conf needs a file"},
		{name: "an empty --conf", args: []string{"--conf", ""}, wantErr: "--conf needs a file"},
		{name: "--conf of a directory", args: []string{"--conf", filepath.Dir(conf)}, wantErr: "not a regular file"},
		{name: "a bare --", args: []string{"--"}},
		{name: "a word after --", args: []string{"--", "help"}, wantErr: `unexpected argument "help"`},
		{name: "an unknown flag", args: []string{"--verbose"}, wantErr: "-verbose"},
		{name: "a word", args: []string{"help"}, wantErr: `unexpected argument "help"`},
		{name: "--conf without a file", args: []string{"--conf"}, wantErr: "conf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			opts, err := parseArgs(tc.args, &out)
			switch {
			case tc.wantExit:
				if !errors.Is(err, errExit) {
					t.Fatalf("err = %v, want the server not to start", err)
				}
			case tc.wantErr != "":
				if err == nil || errors.Is(err, errExit) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one naming %q", err, tc.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("err = %v, want the server to start", err)
				}
			}
			if !strings.Contains(out.String(), tc.wantOut) {
				t.Errorf("output = %q, want %q", out.String(), tc.wantOut)
			}
			if !tc.wantExit && out.Len() > 0 {
				t.Errorf("output = %q: stdout is the MCP channel, nothing may be printed before the server", out.String())
			}
			if tc.wantErr == "" && opts.configPath != tc.wantConf {
				t.Errorf("config path = %q, want %q", opts.configPath, tc.wantConf)
			}
		})
	}
}
