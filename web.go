package web

import (
	"compress/gzip"
	"net/http"
	"net/http/httputil"
	"runtime"
	"strings"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/reconquest/karma-go"
	"github.com/seletskiy/go-log"
)

type Handler func(*Context) error

type Web struct {
	chi.Router
}

func New(router chi.Router) *Web {
	web := Web{
		Router: router,
	}

	web.Use(gziphandler.MustNewGzipLevelHandler(gzip.BestCompression))
	web.Use(web.recover)
	web.Use(web.log)

	return &web
}

func (web *Web) Route(pattern string, fn func(mux *Web)) *Web {
	return &Web{
		Router: web.Router.Route(
			pattern,
			func(router chi.Router) {
				fn(&Web{Router: router})
			},
		),
	}
}

func (web *Web) ServeHandler(handler Handler) http.HandlerFunc {
	return func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		context := NewContext(writer, request)
		context.err = handler(context)
	}
}

func (web *Web) log(next http.Handler) http.Handler {
	return http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			var (
				started = time.Now()
				scribe  = middleware.NewWrapResponseWriter(
					writer,
					request.ProtoMajor,
				)
			)

			next.ServeHTTP(scribe, request)
			duration := time.Since(started)

			logger := func(message string, args ...interface{}) {
				log.Debugf(nil, message, args...)
			}

			context := request.Context().Value(ContextKey)

			if context != nil {
				err := context.(*Context).err
				if err != nil {
					logger = func(message string, args ...interface{}) {
						if scribe.Status() >= 500 {
							log.Errorf(err, message, args...)
						} else {
							log.Warningf(err, message, args...)
						}
					}
				}
			}

			logger(
				"{http} %v %4v %v | %.6f %v",
				scribe.Status(),
				request.Method,
				request.URL.String(),
				duration.Seconds(),
				request.RemoteAddr,
			)
		},
	)
}

func (web *Web) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					dump, _ := httputil.DumpRequest(request, false)

					err := karma.
						Describe("client", request.RemoteAddr).
						Describe("request", strings.TrimSpace(string(dump))).
						Describe("stack", stack(3)).
						Reason(err)

					log.Errorf(err, "panic while serving %s", request.URL)
				}
			}()

			next.ServeHTTP(writer, request)
		},
	)
}

func stack(skip int) string {
	buffer := make([]byte, 1024)
	for {
		written := runtime.Stack(buffer, false)
		if written < len(buffer) {
			// call stack contains of goroutine number and set of calls
			//   goroutine NN [running]:
			//   github.com/user/project.(*Type).MethodFoo()
			//        path/to/src.go:line
			//   github.com/user/project.MethodBar()
			//        path/to/src.go:line
			// so if we need to skip 2 calls than we must split stack on
			// following parts:
			//   2(call)+2(call path)+1(goroutine header) + 1(callstack)
			// and extract first and last parts of resulting slice
			stack := strings.SplitN(string(buffer[:written]), "\n", skip*2+2)
			return stack[0] + "\n" + stack[skip*2+1]
		}

		buffer = make([]byte, 2*len(buffer))
	}
}
