package util

import (
	"github.com/iakinsey/delver/config"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestExtensionFormatsAndMatching(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{".zip,.pdf, .docx", []string{".zip", ".pdf", ".docx"}},
		{"zip, pdf, docx", []string{".zip", ".pdf", ".docx"}},
		{"zip, tar.gz,    pdf", []string{".zip", ".tar.gz", ".pdf"}},
		{".zip, .tar.gz, pdf,tar.xz", []string{".zip", ".tar.gz", ".pdf", ".tar.xz"}},
		{" ZIP, .zip, ,PDF,", []string{".zip", ".pdf"}},
		{" , ", []string{}},
	} {
		v, err := NormalizeExtensions(tc.in)
		require.NoError(t, err)
		require.Equal(t, tc.want, v)
	}
	for _, in := range []string{".", "..zip", "tar..gz", "*.pdf", "foo/bar", "zip?x", "pdf\nexe"} {
		_, err := NormalizeExtensions(in)
		require.Error(t, err, in)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/file.ZIP?download=1#x", true}, {"/file.tar.gz", true}, {"/file%2Epdf", true},
		{"/file%252Epdf", false}, {"/file.zip.html", false}, {"/zip", false}, {"/file.zip/", false},
		{"/page?file=x.pdf", false}, {"/page#x.pdf", false}, {"/file.gz", false},
	} {
		u, err := url.Parse("http://example.test" + tc.path)
		require.NoError(t, err)
		require.Equal(t, tc.want, ExtensionExcluded(u, []string{".zip", ".tar.gz", ".pdf"}), tc.path)
	}
}
func TestExtensionExclusionsPreventRequestsAndRedirects(t *testing.T) {
	var blocked, allowed atomic.Int64
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			allowed.Add(1)
			http.Redirect(w, r, "/archive.tar.gz", 302)
			return
		}
		blocked.Add(1)
	}))
	defer site.Close()
	p := config.LoadConfig().HTTPClient
	p.ExcludedExtensions = []string{"ZIP", ".tar.gz"}
	p.MaxRetries = 3
	c := NewHTTPClientWithConfig(p)
	for _, path := range []string{"/file.ZIP?x=1", "/file%2ezip", "/redirect"} {
		_, err := c.Perform(site.URL + path)
		require.Error(t, err)
		require.Contains(t, err.Error(), "file extension excluded")
	}
	require.Zero(t, blocked.Load())
	require.EqualValues(t, 1, allowed.Load(), "policy exclusions must not retry redirects")
}
