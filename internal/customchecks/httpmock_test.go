package customchecks

import (
	"net/http"
	"net/http/httptest"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
)

// newTestClient returns a gitlab.Client wired to an httptest server running
// handler, so custom checks can be exercised end-to-end against canned API
// responses without a real GitLab instance.
func newTestClient(t *testing.T, handler http.HandlerFunc) *gitlab.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := gitlab.NewClient("test-token", gitlab.WithBaseURL(server.URL), gitlab.WithoutRetries())
	if err != nil {
		t.Fatalf("gitlab.NewClient() error = %v", err)
	}

	return client
}
