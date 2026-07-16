package gateway

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
)

func TestManagedPoolNameIncludesImageSlug(t *testing.T) {
	name, err := managedPoolName("docker.io/library/python:3.12", "arl", "code", nil, nil)
	if err != nil {
		t.Fatalf("managedPoolName returned error: %v", err)
	}

	if !strings.HasPrefix(name, "python-3-12-") {
		t.Fatalf("managedPoolName = %q, want image slug prefix", name)
	}
	if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
		t.Fatalf("managedPoolName = %q is not a DNS label: %v", name, errs)
	}
}

func TestManagedPoolNameNormalizesDockerLibraryImage(t *testing.T) {
	fromFullRef, err := managedPoolName("docker.io/library/python:3.12", "arl", "code", nil, nil)
	if err != nil {
		t.Fatalf("managedPoolName full ref returned error: %v", err)
	}
	fromShortRef, err := managedPoolName("python:3.12", "arl", "code", nil, nil)
	if err != nil {
		t.Fatalf("managedPoolName short ref returned error: %v", err)
	}

	if fromFullRef != fromShortRef {
		t.Fatalf("managedPoolName full ref = %q, short ref = %q", fromFullRef, fromShortRef)
	}
}

func TestManagedPoolNameHashSeparatesPoolIdentity(t *testing.T) {
	codePool, err := managedPoolName("python:3.12", "arl", "code", nil, nil)
	if err != nil {
		t.Fatalf("managedPoolName code returned error: %v", err)
	}
	defaultPool, err := managedPoolName("python:3.12", "arl", "default", nil, nil)
	if err != nil {
		t.Fatalf("managedPoolName default returned error: %v", err)
	}
	privatePool, err := managedPoolName("python:3.12", "arl", "code", []PrivateContainerSpec{{
		Name:  "db",
		Image: "postgres:16",
	}}, nil)
	if err != nil {
		t.Fatalf("managedPoolName private returned error: %v", err)
	}

	if codePool == defaultPool {
		t.Fatalf("managedPoolName did not separate profile identities: %q", codePool)
	}
	if codePool == privatePool {
		t.Fatalf("managedPoolName did not separate private container identities: %q", codePool)
	}
	for _, name := range []string{codePool, defaultPool, privatePool} {
		if !strings.HasPrefix(name, "python-3-12-") {
			t.Fatalf("managedPoolName = %q, want shared image slug prefix", name)
		}
	}
}

func TestManagedPoolNameTruncatesLongImageSlug(t *testing.T) {
	name, err := managedPoolName("registry.example.com/org/some-extremely-long-runtime-image-name-with-extra-build-metadata:20260703", "arl", "code", nil, nil)
	if err != nil {
		t.Fatalf("managedPoolName returned error: %v", err)
	}

	if len(name) > 63 {
		t.Fatalf("managedPoolName length = %d, want <= 63: %q", len(name), name)
	}
	if !strings.HasPrefix(name, "some-extremely-long-runtime-image-name") {
		t.Fatalf("managedPoolName = %q, want truncated image slug prefix", name)
	}
	if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
		t.Fatalf("managedPoolName = %q is not a DNS label: %v", name, errs)
	}

	templateName := sandboxTemplateName(name)
	if len(templateName) > 63 {
		t.Fatalf("sandboxTemplateName length = %d, want <= 63: %q", len(templateName), templateName)
	}
	if !strings.HasSuffix(templateName, "-template") {
		t.Fatalf("sandboxTemplateName = %q, want -template suffix", templateName)
	}
	if errs := validation.IsDNS1123Label(templateName); len(errs) > 0 {
		t.Fatalf("sandboxTemplateName = %q is not a DNS label: %v", templateName, errs)
	}
}

func TestManagedPoolNameUsesRepositoryNameForDigestImages(t *testing.T) {
	name, err := managedPoolName("ghcr.io/acme/runner@sha256:abcdef", "arl", "code", nil, nil)
	if err != nil {
		t.Fatalf("managedPoolName returned error: %v", err)
	}

	if !strings.HasPrefix(name, "runner-digest-") {
		t.Fatalf("managedPoolName = %q, want repository digest slug prefix", name)
	}
}

func TestSandboxTemplateNamePreservesShortPoolNames(t *testing.T) {
	if got := sandboxTemplateName("code"); got != "code-template" {
		t.Fatalf("sandboxTemplateName = %q, want code-template", got)
	}
}

func TestSandboxTemplateNameTruncatesLongPoolNamesWithHash(t *testing.T) {
	poolName := "some-extremely-long-runtime-image-name-with-extra-build-metadata-123456789abc"
	name := sandboxTemplateName(poolName)

	if len(name) > 63 {
		t.Fatalf("sandboxTemplateName length = %d, want <= 63: %q", len(name), name)
	}
	if !strings.HasSuffix(name, "-template") {
		t.Fatalf("sandboxTemplateName = %q, want -template suffix", name)
	}
	if name == poolName+"-template" {
		t.Fatalf("sandboxTemplateName did not truncate long pool name: %q", name)
	}
	if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
		t.Fatalf("sandboxTemplateName = %q is not a DNS label: %v", name, errs)
	}
}
