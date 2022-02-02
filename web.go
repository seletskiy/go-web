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
	web.Use(web.init)
	web.Use(web.Middleware(web.log))
	web.Use(web.Middleware(web.recover))

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

func (web *Web) Middleware(
	middleware func(Handler) Handler,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return web.ServeHandler(
			middleware(
				func(context *Context) error {
					next.ServeHTTP(context.GetWriter(), context.GetRequest())
					return context.err
				},
			),
		)
	}
}

func (web *Web) With(middlewares ...func(Handler) Handler) *Web {
	var proxies []func(http.Handler) http.Handler

	for _, middleware := range middlewares {
		proxies = append(
			proxies,
			web.Middleware(middleware),
		)
	}

	return &Web{
		Router: web.Router.With(proxies...),
	}
}

func (web *Web) ServeHandler(handler Handler) http.HandlerFunc {
	return func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		context := request.Context().Value(ContextKey).(*Context)
		context.err = handler(context)
	}
}

func (web *Web) init(next http.Handler) http.Handler {
	return http.HandlerFunc(
		func(
			writer http.ResponseWriter,
			request *http.Request,
		) {
			context := NewContext(writer, request)
			next.ServeHTTP(writer, context.request)
		},
	)
}

func (web *Web) log(next Handler) Handler {
	return func(context *Context) error {
		var (
			request = context.GetRequest()
			scribe  = middleware.NewWrapResponseWriter(
				context.GetWriter(),
				request.ProtoMajor,
			)
		)

		context.writer = scribe

		var (
			started  = time.Now()
			err      = next(context)
			duration = time.Since(started)
		)

		logger(
			scribe.Status(),
			err,
			"{http} %v %4v %v | %.6f %v | %s",
			scribe.Status(),
			request.Method,
			request.URL.String(),
			duration.Seconds(),
			request.RemoteAddr,
			context.id,
		)

		return err
	}
}

func (web *Web) recover(next Handler) Handler {
	return func(context *Context) (err error) {
		defer func() {
			if cause := recover(); cause != nil {
				var (
					request = context.GetRequest()
					dump, _ = httputil.DumpRequest(request, false)
				)

				err = context.Error(
					http.StatusInternalServerError,
					karma.
						Describe("client", request.RemoteAddr).
						Describe("request", strings.TrimSpace(string(dump))).
						Describe("stack", stack(3)).
						Reason(cause),
					"panic while serving %s",
					request.URL,
				)
			}
		}()

		return next(context)
	}
}

func logger(code int, err error, message string, args ...interface{}) {
	if code >= 500 {
		log.Errorf(err, message, args...)
	} else {
		if err != nil {
			log.Warningf(err, message, args...)
		} else {
			log.Debugf(nil, message, args...)
		}
	}
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
