package sealedseed

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}
func TestSealRejectsInvalidInput(t *testing.T) {
	_, public := testKey(t)
	for _, tc := range []struct {
		key, host, owner string
		value            []byte
	}{
		{"not a key", "node1", "capi-owner", []byte("secret")},
		{public + public, "node1", "capi-owner", []byte("secret")},
		{public, "", "capi-owner", []byte("secret")},
		{public, "node1", "other", []byte("secret")},
		{public, "node1", "capi-owner", nil},
		{public, "node1", "capi-owner", make([]byte, MaxPlaintext+1)},
	} {
		if _, err := Seal(tc.key, tc.host, tc.owner, tc.value); err == nil {
			t.Fatal("invalid bootstrap accepted")
		}
	}
}
func TestBootEnvironmentDecryptsAndRejectsTampering(t *testing.T) {
	python := os.Getenv("GOMI_BOOTSTRAP_TEST_PYTHON")
	required := python != ""
	if python == "" {
		python = "python3"
	}
	if err := exec.Command(python, "-c", "import cryptography, yaml").Run(); err != nil {
		if required {
			t.Fatal("configured bootstrap Python lacks cryptography/PyYAML")
		}
		t.Skip("Python cryptography/PyYAML needed for boot environment interoperability test")
	}
	script, err := filepath.Abs("../../../../bootenv/scripts/gomi-unseal-bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	key, public := testKey(t)
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key.pem")
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("## template: jinja\n#cloud-config\nwrite_files:\n- path: /root/secret\n  content: CA-PRIVATE-KEY-SECRET\n")
	encoded, err := Seal(public, "node1", "capi-owner", plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("CA-PRIVATE-KEY-SECRET")) {
		t.Fatal("plaintext leaked")
	}
	second, err := Seal(public, "node1", "capi-owner", plaintext)
	if err != nil || bytes.Equal(encoded, second) {
		t.Fatal("encryption is not randomized")
	}
	for _, variant := range []string{"valid", "different-host", "different-owner", "ciphertext", "wrapped-key", "fingerprint", "version", "unknown-field"} {
		t.Run(variant, func(t *testing.T) {
			var document map[string]any
			if err = json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			switch variant {
			case "different-host":
				document["host"] = "node2"
			case "different-owner":
				document["owner"] = "capi-other"
			case "ciphertext":
				document["ciphertext"] = "AAAA" + document["ciphertext"].(string)[4:]
			case "wrapped-key":
				document["wrappedKey"] = "AAAA" + document["wrappedKey"].(string)[4:]
			case "fingerprint":
				document["keyFingerprint"] = strings.Repeat("0", 64)
			case "version":
				document["version"] = 2
			case "unknown-field":
				document["plaintext"] = "unexpected"
			}
			payload, _ := json.Marshal(document)
			input := filepath.Join(dir, variant+".json")
			output := filepath.Join(dir, variant+".yaml")
			if err = os.WriteFile(input, payload, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(output, []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			log, runErr := exec.Command(python, script, "--key", keyFile, "--host", "node1", "--owner", "capi-owner", "--input", input, "--output", output).CombinedOutput()
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "valid" {
				if runErr != nil || !bytes.Equal(got, plaintext) {
					t.Fatalf("decrypt failed: %v %s", runErr, log)
				}
				info, _ := os.Stat(output)
				if info.Mode().Perm() != 0600 {
					t.Fatal("seed permissions are not private")
				}
			} else if runErr == nil || string(got) != "existing" || bytes.Contains(log, []byte("CA-PRIVATE-KEY-SECRET")) {
				t.Fatalf("tamper accepted or leaked: %v", runErr)
			}
		})
	}
}
