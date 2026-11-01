package project

import (
	"strings"
	"testing"
)

func TestDeriveSlug(t *testing.T) {
	// Explicit slug
	slug := DeriveSlug("Auth Refresh 401!", "some prompt", "my-project")
	if slug != "auth-refresh-401" {
		t.Errorf("expected auth-refresh-401, got %s", slug)
	}

	// Auto-derived slug from prompt
	slug = DeriveSlug("", "Please fix the login button styling and alignment", "my-project")
	if !strings.HasPrefix(slug, "login-button") && !strings.HasPrefix(slug, "fix-login-button") {
		t.Errorf("unexpected auto-derived slug: %s", slug)
	}
}
