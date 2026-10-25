package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atm1020/gojenkins"
)

func TestChoiceParamXML(t *testing.T) {
	param := choiceParam("REGION", []string{"east", "west & beyond"}, "Region")
	var decoded struct {
		Choices struct {
			Class string `xml:"class,attr"`
			Array struct {
				Class  string   `xml:"class,attr"`
				Values []string `xml:"string"`
			} `xml:"a"`
		} `xml:"choices"`
	}
	if err := xml.Unmarshal([]byte("<param>"+param.body+"</param>"), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Choices.Class != "java.util.Arrays$ArrayList" || decoded.Choices.Array.Class != "string-array" {
		t.Fatalf("missing Jenkins list/array types: %+v", decoded.Choices)
	}
	if values := decoded.Choices.Array.Values; len(values) != 2 || values[1] != "west & beyond" {
		t.Fatalf("unexpected choices: %v", values)
	}
}

func TestSeedFolders(t *testing.T) {
	for _, mode := range []string{"new", "existing", "html-success", "missing-plugin"} {
		t.Run(mode, func(t *testing.T) {
			creates := 0
			folders := map[string]bool{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasPrefix(r.URL.Path, "/crumbIssuer/"):
					fmt.Fprint(w, `{}`)
				case strings.HasSuffix(r.URL.Path, "/createItem"):
					creates++
					if mode == "missing-plugin" {
						w.Header().Set("X-Error", "No item type com.cloudbees.hudson.plugins.folder.Folder is known")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					parent := strings.TrimSuffix(r.URL.Path, "/createItem")
					if parent != "" && !folders[parent] {
						t.Errorf("parent %s not created", parent)
					}
					folders[parent+"/job/"+r.URL.Query().Get("name")] = true
					if mode == "html-success" {
						http.Redirect(w, r, parent+"/job/"+r.URL.Query().Get("name")+"/", http.StatusFound)
						return
					}
					fmt.Fprint(w, `{}`)
				default:
					if mode == "html-success" && !strings.HasSuffix(r.URL.Path, "/api/json") {
						w.Header().Set("Content-Type", "text/html")
						fmt.Fprint(w, "<html>created</html>")
						return
					}
					path := strings.TrimSuffix(r.URL.Path, "/api/json")
					if mode != "existing" && !folders[path] {
						http.Error(w, "<html>not found</html>", http.StatusNotFound)
						return
					}
					fmt.Fprint(w, `{}`)
				}
			}))
			defer server.Close()
			jenkins := gojenkins.CreateJenkins(server.Client(), server.URL)
			err := seedFolders(context.Background(), jenkins)
			if mode == "missing-plugin" {
				if err == nil || !strings.Contains(err.Error(), "No item type") || !strings.Contains(err.Error(), "--build") {
					t.Fatalf("expected actionable plugin error, got %v", err)
				}
				if creates != 1 {
					t.Fatalf("continued after folder failure: %d creates", creates)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 8
			if mode == "existing" {
				want = 0
			}
			if creates != want {
				t.Fatalf("got %d creates, want %d", creates, want)
			}
		})
	}
}
