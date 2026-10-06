package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/netdefense-io/NDCLI/internal/models"
)

func TestWithCode(t *testing.T) {
	if got := WithCode("TRUST_KEY_WEAK", "weak key"); got != "[TRUST_KEY_WEAK] weak key" {
		t.Errorf("WithCode = %q", got)
	}
	if got := WithCode("", "weak key"); got != "weak key" {
		t.Errorf("an empty code must leave the message alone, got %q", got)
	}
}

// TestSyncApplyFormatters_ShowTheDeviceErrorCode: a device's sync error
// carries the code NDManager gave it. JSON and MCP always had it as a field;
// the human formats printed only the message.
func TestSyncApplyFormatters_ShowTheDeviceErrorCode(t *testing.T) {
	for _, devErr := range []models.SyncError{
		{
			DeviceName: "e2e-a",
			Error:      "TRUST: snippet 'corp-ca' has an invalid certificate",
			Code:       "TRUST_BUILD_INVALID",
			TrustIssues: []models.TrustIssue{
				{Code: "TRUST_KEY_WEAK", Message: "'crt' has an RSA key of 1024 bits", Snippet: []string{"corp-ca"}},
			},
		},
		{
			// NDManager's build refusal for two snippets claiming one device
			// object: identifiers only, all of them in the message.
			DeviceName: "e2e-a",
			Error: "More than one snippet on this device carries the same uuid: " +
				"221f3268-aaaa-4aaa-8aaa-aaaaaaaaaaaa (snippets: corp-web, corp-web-copy; templates: edge). " +
				"Keep one snippet per uuid: delete the others and create them again without a uuid",
			Code: "SNIPPET_UUID_CONFLICT",
		},
		{
			// NDManager's refusal to build a sync for an agent that would
			// apply a RULE, ALIAS or UNBOUND_HOST_OVERRIDE snippet only in
			// part: the snippets and their keys are all in the message.
			DeviceName: "e2e-a",
			Error: "This device's NDAgent (1.22.1) applies these snippets only in part; upgrade it to 1.23.0 " +
				"or later, or edit what is listed: allow-https (RULE: fields it ignores: log)",
			Code: "SNIPPET_AGENT_TOO_OLD",
		},
	} {
		t.Run(devErr.Code, func(t *testing.T) {
			resp := makeSyncResponse(devErr)
			want := "e2e-a: [" + devErr.Code + "] " + devErr.Error + "\n"

			outputs := map[string]string{}
			for name, newFormatter := range map[string]func(*bytes.Buffer) Formatter{
				"simple":   func(b *bytes.Buffer) Formatter { return &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: b}} },
				"detailed": func(b *bytes.Buffer) Formatter { return &DetailedFormatter{BaseFormatter: BaseFormatter{Writer: b}} },
			} {
				var buf bytes.Buffer
				if err := newFormatter(&buf).FormatSyncApply(resp); err != nil {
					t.Fatalf("%s: format failed: %v", name, err)
				}
				outputs[name] = buf.String()
			}
			// TableFormatter writes with fmt.Printf, not through BaseFormatter.Writer.
			outputs["table"] = captureStdout(t, func() {
				if err := (&TableFormatter{}).FormatSyncApply(resp); err != nil {
					t.Fatalf("table: format failed: %v", err)
				}
			})
			for name, out := range outputs {
				if !strings.Contains(out, want) {
					t.Errorf("%s formatter missing %q, got:\n%s", name, want, out)
				}
			}

			var buf bytes.Buffer
			if err := (&JSONFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}).FormatSyncApply(resp); err != nil {
				t.Fatalf("json: format failed: %v", err)
			}
			var doc models.SyncApplyResponse
			if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
				t.Fatalf("json output does not parse: %v\n%s", err, buf.String())
			}
			if len(doc.Errors) != 1 || doc.Errors[0].Code != devErr.Code || doc.Errors[0].Error != devErr.Error {
				t.Errorf("json must carry the code as a field and leave the message alone, got %+v", doc.Errors)
			}
		})
	}
}

// TestSyncApplyFormatters_UncodedDeviceErrorIsUnchanged: an error without a
// code renders exactly as before.
func TestSyncApplyFormatters_UncodedDeviceErrorIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	f := &SimpleFormatter{BaseFormatter: BaseFormatter{Writer: &buf}}
	if err := f.FormatSyncApply(makeSyncResponse(models.SyncError{DeviceName: "e2e-a", Error: "device offline"})); err != nil {
		t.Fatalf("format failed: %v", err)
	}
	if !strings.Contains(buf.String(), "• e2e-a: device offline\n") {
		t.Errorf("uncoded device error changed:\n%s", buf.String())
	}
}
