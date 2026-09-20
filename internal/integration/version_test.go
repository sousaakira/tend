package integration

import "testing"

func TestReadInstalledVersionParsesTendMarker(t *testing.T) {
	content := []byte("#!/bin/sh\n# TEND_INTEGRATION_VERSION=10\n")
	version, ok := ReadInstalledVersion(content)
	if !ok {
		t.Fatal("ReadInstalledVersion = false, want true")
	}
	if version != 10 {
		t.Fatalf("version = %d, want 10", version)
	}
}

func TestReadInstalledVersionParsesLegacyHerdrMarker(t *testing.T) {
	content := []byte("// HERDR_INTEGRATION_VERSION=4\n")
	version, ok := ReadInstalledVersion(content)
	if !ok {
		t.Fatal("ReadInstalledVersion = false, want true")
	}
	if version != 4 {
		t.Fatalf("version = %d, want 4", version)
	}
}

func TestReadInstalledVersionIgnoresInvalidMarker(t *testing.T) {
	content := []byte("# TEND_INTEGRATION_VERSION=not-a-number\n")
	if _, ok := ReadInstalledVersion(content); ok {
		t.Fatal("ReadInstalledVersion = true, want false")
	}
}

func TestReadInstalledVersionReturnsFalseWhenMissing(t *testing.T) {
	if _, ok := ReadInstalledVersion([]byte("#!/bin/sh\n")); ok {
		t.Fatal("ReadInstalledVersion = true, want false")
	}
}

func TestReadInstalledVersionFindsFirstValidMarker(t *testing.T) {
	content := []byte("# TEND_INTEGRATION_VERSION=2\n# HERDR_INTEGRATION_VERSION=99\n")
	version, ok := ReadInstalledVersion(content)
	if !ok {
		t.Fatal("ReadInstalledVersion = false, want true")
	}
	if version != 2 {
		t.Fatalf("version = %d, want first marker value 2", version)
	}
}
