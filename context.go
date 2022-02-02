package web

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/go-chi/chi"
	"github.com/reconquest/karma-go"
	"github.com/xtgo/uuid"
)

type Context struct {
	*karma.Context

	id      string
	writer  http.ResponseWriter
	request *http.Request
	err     error

	data map[string]interface{}
}

type ContextKeyType string

var ContextKey ContextKeyType = "context"

func NewContext(
	writer http.ResponseWriter,
	request *http.Request,
) *Context {
	id := "req_" + hex.EncodeToString(uuid.NewRandom().Bytes())

	ctx := &Context{
		Context: karma.Describe("request_id", id),

		id:     id,
		writer: writer,
	}

	ctx.request = request.WithContext(
		context.WithValue(
			request.Context(),
			ContextKey,
			ctx,
		),
	)

	return ctx
}

func (context *Context) Get(name string) interface{} {
	return context.data[name]
}

func (context *Context) Set(name string, value interface{}) *Context {
	if context.data == nil {
		context.data = map[string]interface{}{}
	}

	context.data[name] = value

	return context
}

func (context *Context) Write(body []byte) (int, error) {
	return context.writer.Write(body)
}

func (context *Context) GetURL() *url.URL {
	return context.request.URL
}

func (context *Context) GetURLParam(key string) string {
	return chi.URLParam(context.request, key)
}

func (context *Context) GetQueryParam(key string) string {
	return context.request.URL.Query().Get(key)
}

func (context *Context) GetRequest() *http.Request {
	return context.request
}

func (context *Context) GetWriter() http.ResponseWriter {
	return context.writer
}

func (context *Context) GetBody() io.ReadCloser {
	return context.request.Body
}

func (context *Context) GetID() string {
	return context.id
}

func (context *Context) Describe(key string, value string) *Context {
	context.Context = context.Context.Describe(key, value)

	return context
}

func (context *Context) OK() error {
	context.writer.WriteHeader(http.StatusOK)

	return nil
}

func (context *Context) Redirect(location string) error {
	context.writer.Header().Set("location", location)

	return nil
}

func (context *Context) NotFound() error {
	context.writer.WriteHeader(http.StatusNotFound)

	return context.
		Format(
			nil,
			"not found: %q",
			context.GetURL(),
		)
}

func (context *Context) InternalError(
	err error,
	message string,
	values ...interface{},
) error {
	return context.Error(
		http.StatusInternalServerError,
		err,
		message,
		values...,
	)
}

func (context *Context) BadRequest(
	err error,
	message string,
	values ...interface{},
) error {
	return context.Error(
		http.StatusBadRequest,
		err,
		message,
		values...,
	)
}

func (context *Context) Error(
	code int,
	err error,
	message string,
	values ...interface{},
) error {
	context.writer.WriteHeader(code)

	{
		// do not send nested error to http client
		err := json.NewEncoder(context.writer).Encode(struct {
			RequestID string `json:"request_id"`
			Error     string `json:"error"`
		}{
			RequestID: context.id,
			Error:     fmt.Sprintf(message, values...),
		})
		if err != nil {
			return karma.Format(
				err,
				"unable to marshal error",
			)
		}
	}

	return context.Format(err, message, values...)
}
