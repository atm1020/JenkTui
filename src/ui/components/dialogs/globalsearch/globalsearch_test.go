package globalsearch

import (
	"fmt"
	"net/http"
	"testing"

	appcontext "github.com/atm1020/jenktui/src/context"

	"github.com/atm1020/gojenkins"
)

type deadlineRecordingTransport struct {
	sawDeadline bool
}

func (t *deadlineRecordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	_, t.sawDeadline = request.Context().Deadline()
	return nil, fmt.Errorf("stop after inspecting request context")
}

func TestGlobalSearchUsesRequestDeadline(t *testing.T) {
	transport := &deadlineRecordingTransport{}
	jenkins := gojenkins.CreateJenkins(&http.Client{Transport: transport}, "http://jenkins")
	dialog := &GlobalSearchDialogCmp{
		ctx:            &appcontext.ProgramContext{Jenkins: jenkins},
		isLegacySearch: true,
	}

	_ = dialog.fetchGlobalSearchResults("demo")()

	if !transport.sawDeadline {
		t.Fatal("global search Jenkins request had no deadline")
	}
}
