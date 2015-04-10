package hello

import (
	"bytes"
	"fmt"
	"go/build"
	"io"
	"io/ioutil"
	"net/http"

	"appengine"

	"github.com/mailgun/godebug/gen"
)

func init() {
	http.HandleFunc("/compile", compile)
	build.Default.GOROOT = "goroot"
	build.Default.GOPATH = "gopath"
	build.Default.GOOS = "darwin"
	build.Default.GOARCH = "amd64"
}

func compile(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	progBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		c.Errorf("Failed to read request: %v", err)
		http.Error(w, "Failed to read request", http.StatusInternalServerError)
		return
	}
	progBytes, err = instrumentWithGodebug(c, progBytes)
	if err != nil {
		http.Error(w, "Invalid Go program", http.StatusBadRequest)
		return
	}
	w.Write(progBytes)
	c.Infof("great success")
	return
	progBytes, err = compileToJS(progBytes)
	if err != nil {
		http.Error(w, "Compilation to javascript failed", http.StatusInternalServerError)
		return
	}
	w.Write(progBytes)
}

func instrumentWithGodebug(c appengine.Context, b []byte) ([]byte, error) {
	var conf gen.Config
	conf.Config.TypeChecker.Error = func(err error) {
		c.Errorf(err.Error())
	}
	f, err := conf.ParseFile("main.go", b)
	if err != nil {
		return nil, err
	}
	conf.CreateFromFiles("main", f)
	prog, err := conf.Load()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	gen.Generate(prog, func(string) ([]byte, error) {
		return b, nil
	}, func(string, string) io.WriteCloser {
		return nopCloser{&out}
	})
	return out.Bytes(), nil
}

func compileToJS(b []byte) ([]byte, error) {
	return b, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Hello, world!")
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error {
	return nil
}
