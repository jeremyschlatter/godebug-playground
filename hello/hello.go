package hello

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/scanner"
	"go/token"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"appengine"

	gbuild "github.com/gopherjs/gopherjs/build"
	"github.com/gopherjs/gopherjs/compiler"
	"github.com/mailgun/godebug/gen"
)

func init() {
	build.Default.GOROOT = "goroot"
	build.Default.GOPATH = "gopath"
	build.Default.GOOS = "darwin"
	build.Default.GOARCH = "amd64"
	http.HandleFunc("/compile", compile)
	http.HandleFunc("/compile-normal", compileNoDebug)
}

func compileNoDebug(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	progBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		c.Errorf("Failed to read request: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	progBytes, err = compileToJS(c, progBytes)
	if err != nil {
		c.Errorf("Failed to compile to javascript: %v", err)
		http.Error(w, formatError(c, err), http.StatusBadRequest)
		return
	}
	w.Write(progBytes)
	c.Infof("great success")
}

func compile(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	progBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		c.Errorf("Failed to read request: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	progBytes, err = instrumentWithGodebug(c, progBytes)
	if err != nil {
		c.Errorf("Failed to instrument Go code: %v", err)
		http.Error(w, formatError(c, err), http.StatusBadRequest)
		return
	}
	progBytes, err = compileToJS(c, progBytes)
	if err != nil {
		c.Errorf("Failed to compile to javascript: %v", err)
		http.Error(w, formatError(c, err), http.StatusBadRequest)
		return
	}
	w.Write(progBytes)
	c.Infof("great success")
}

func formatError(c appengine.Context, err error) string {
	switch x := err.(type) {
	case nil:
		return ""
	default:
		c.Errorf("Unhandled compilation error: type: %T, value: %v", err, err)
		return "compilation error"
	case compiler.ErrorList:
		list := make(errorList, len(x))
		for i := range list {
			list[i] = x[i]
		}
		return list.Error()
	case scanner.ErrorList:
		list := make(errorList, len(x))
		for i := range list {
			list[i] = x[i]
		}
		return list.Error()
	case errorList:
		return err.Error()
	}
}

type errorList []error

func (list errorList) Error() string {
	var result []string
	for i, e := range list {
		if i == 5 {
			result = append(result, fmt.Sprintf("(and %d more errors)", len(list)-i))
			break
		}
		result = append(result, e.Error())
	}
	return strings.Join(result, "\n")
}

func instrumentWithGodebug(c appengine.Context, b []byte) ([]byte, error) {
	var conf gen.Config
	var parseErrors errorList
	conf.Config.TypeChecker.Error = func(err error) {
		c.Errorf(err.Error())
		parseErrors = append(parseErrors, err)
	}
	f, err := conf.ParseFile("main.go", b)
	if err != nil {
		if len(parseErrors) > 0 {
			return nil, parseErrors
		}
		return nil, err
	}
	conf.CreateFromFiles("main", f)
	prog, err := conf.Load()
	if err != nil {
		if len(parseErrors) > 0 {
			return nil, parseErrors
		}
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

func compileToJS(c appengine.Context, b []byte) ([]byte, error) {
	var err error
	pkg := &gbuild.PackageData{
		Package: &build.Package{
			Name:       "main",
			ImportPath: "main",
			GoFiles:    []string{"main.go"},
		},
	}

	s := gbuild.NewSession(&gbuild.Options{Minify: true})
	s.ImportContext.Import = func(path string) (*compiler.Archive, error) {
		for _, dir := range []string{build.Default.GOROOT, build.Default.GOPATH} {
			filename := filepath.Join(dir, "pkg", build.Default.GOOS+"_js_min", path) + ".a"
			f, err := os.Open(filename)
			if err != nil {
				continue
			}
			defer f.Close()
			a, err := compiler.ReadArchive(filepath.Join(dir, path)+".a", path, f, s.ImportContext.Packages)
			if err != nil {
				c.Errorf("for package %q, ReadArchive gave: %v", path, err)
			}
			return a, err
		}
		return nil, fmt.Errorf("failed to find archive for package %q", path)
	}

	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "main.go", b, 0)
	if err != nil {
		return nil, err
	}

	pkg.Archive, err = compiler.Compile("main", []*ast.File{f}, fs, s.ImportContext, true)
	if err != nil {
		return nil, err
	}

	deps, err := compiler.ImportDependencies(pkg.Archive, s.ImportContext.Import)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	err = compiler.WriteProgramCode(deps, &compiler.SourceMapFilter{Writer: &out})
	return out.Bytes(), err
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
