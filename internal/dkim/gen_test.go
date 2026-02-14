package dkim

import "testing"

func TestDKIMKeyGeneration(t *testing.T) {
	dkimKeys, err := GenerateDKIMKeysPair()
	if err != nil {
		t.Errorf("Cannot generate key, %v", err)
	}

	if len(dkimKeys.PrivateKey) == 0 {
		t.Errorf("private key is not valid: %v", dkimKeys.PrivateKey)
	}

	if len(dkimKeys.PublicKey) == 0 {
		t.Errorf("public key is not valid: %v", dkimKeys.PublicKey)
	}
}
