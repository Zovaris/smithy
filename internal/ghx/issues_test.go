package ghx

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestIssueListFullPaginatesAndWarnsWhenTruncated(t *testing.T) {
	oldPage, oldLimit, oldWarn := issuePage, issueLimit, issueWarn
	issuePage, issueLimit = 1, 2
	var warn bytes.Buffer
	issueWarn = &warn
	t.Cleanup(func() { issuePage, issueLimit, issueWarn = oldPage, oldLimit, oldWarn })
	var pages []string
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("page")
		pages = append(pages, p)
		fmt.Fprintf(w, `[{"number":%s,"html_url":"u%s","title":"t","body":"b","labels":[{"name":"auto"}]}]`, p, p)
	})
	var got []struct {
		Number int
		Labels []struct{ Name string }
	}
	if err := IssueListFull("auto", &got); err != nil || len(got) != 2 || got[1].Number != 2 || got[0].Labels[0].Name != "auto" {
		t.Fatalf("got %+v %v", got, err)
	}
	if strings.Join(pages, ",") != "1,2" {
		t.Errorf("pages %v", pages)
	}
	if strings.Count(warn.String(), "\n") != 1 || !strings.Contains(warn.String(), "first 2") {
		t.Errorf("warning %q", warn.String())
	}
}
