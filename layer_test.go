package layer

import (
	"github.com/nbio/st"
	"gopkg.in/vinxi/utils.v0"
	"net/http"
	"testing"
)

type plugin struct {
	middleware interface{}
}

func (p *plugin) Register(mw Pluggable) {
	mw.Use(RequestPhase, p.middleware)
}

func newPlugin(f interface{}) *plugin {
	return &plugin{middleware: f}
}

func TestMiddleware(t *testing.T) {
	mw := New()

	mw.Use(RequestPhase, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("foo", "bar")
			h.ServeHTTP(w, r)
		})
	})

	st.Expect(t, mw.Pool["request"].Len(), 1)

	w := utils.NewWriterStub()
	req := &http.Request{}
	mw.Run("request", w, req, nil)

	st.Expect(t, w.Header().Get("foo"), "bar")
}

func TestNoHandlerRegistered(t *testing.T) {
	mw := New()

	st.Expect(t, mw.Pool["request"], (*Stack)(nil))

	w := utils.NewWriterStub()
	req := &http.Request{}
	mw.Run("request", w, req, nil)

	st.Expect(t, w.Code, 502)
	st.Expect(t, string(w.Body), "vinxi: cannot route request")
}

func TestFinalErrorHandling(t *testing.T) {
	mw := New()

	mw.Use("request", func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("something went wrong")
		})
	})

	st.Expect(t, mw.Pool["request"].Len(), 1)

	w := utils.NewWriterStub()
	req := &http.Request{}
	mw.Run("request", w, req, nil)

	st.Expect(t, w.Code, 500)
	st.Expect(t, w.Body, []byte("vinxi: internal server error"))
}

func TestUseFinalHandler(t *testing.T) {
	mw := New()

	mw.UseFinalHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		w.Write([]byte("vinxi: service unavailable"))
	}))

	w := utils.NewWriterStub()
	req := &http.Request{}
	mw.Run("request", w, req, nil)

	st.Expect(t, w.Code, 503)
	st.Expect(t, w.Body, []byte("vinxi: service unavailable"))
}

func TestRegisterPlugin(t *testing.T) {
	mw := New()

	p := newPlugin(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("foo", "bar")
			h.ServeHTTP(w, r)
		})
	})
	mw.Use(RequestPhase, p)

	st.Expect(t, mw.Pool["request"].Len(), 1)

	w := utils.NewWriterStub()
	req := &http.Request{}
	mw.Run("request", w, req, nil)

	st.Expect(t, w.Header().Get("foo"), "bar")
}

func TestRegisterUnsupportedInterface(t *testing.T) {
	defer func() {
		r := recover()
		st.Expect(t, r, "vinxi: unsupported middleware interface")
	}()

	mw := New()

	mw.Use(RequestPhase, func() {})
}

func TestUsePriority(t *testing.T) {
	mw := New()

	mw.UseFinalHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		w.Write([]byte("vinxi: service unavailable"))
	}))

	array := []int{}

	buildAppendingMiddleware := func(before, after int) interface{} {
		return func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				array = append(array, before)
				h.ServeHTTP(w, r)
				array = append(array, after)
			})
		}
	}
	mw.UsePriority("request", Normal, buildAppendingMiddleware(3, 10))
	mw.UsePriority("request", Tail, buildAppendingMiddleware(5, 8))
	mw.UsePriority("request", Head, buildAppendingMiddleware(1, 12))
	mw.UsePriority("request", Tail, buildAppendingMiddleware(6, 7))
	mw.UsePriority("request", Head, buildAppendingMiddleware(2, 11))
	mw.UsePriority("request", Normal, buildAppendingMiddleware(4, 9))

	w := utils.NewWriterStub()
	req := &http.Request{}
	mw.Run("request", w, req, nil)

	st.Expect(t, array, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
}

func TestSimpleMiddlewareCallChain(t *testing.T) {
	mw := New()

	calls := 0
	fn := func(w http.ResponseWriter, r *http.Request, h http.Handler) {
		calls++
		h.ServeHTTP(w, r)
	}
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	})

	mw.Use(RequestPhase, fn)
	mw.Use(RequestPhase, fn)
	mw.Use(RequestPhase, fn)

	wrt := utils.NewWriterStub()
	req := &http.Request{}

	mw.Run("request", wrt, req, final)
	st.Expect(t, calls, 4)
}

func TestFlush(t *testing.T) {
	mw := New()

	mw.Use("request", func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r)
		})
	})
	mw.Flush()
	st.Expect(t, mw.Pool, Pool{})
}

func BenchmarkLayerRun(b *testing.B) {
	w := utils.NewWriterStub()
	req := &http.Request{}

	mw := New()
	for i := 0; i < 100; i++ {
		mw.Use(RequestPhase, func(h http.Handler) http.Handler {
			return http.HandlerFunc(h.ServeHTTP)
		})
	}

	for n := 0; n < b.N; n++ {
		mw.Run(RequestPhase, w, req, http.HandlerFunc(nil))
	}
}

func BenchmarkStackLayers(b *testing.B) {
	w := utils.NewWriterStub()
	req := &http.Request{}

	handler := func(h http.Handler) http.Handler {
		return http.HandlerFunc(h.ServeHTTP)
	}

	mw := New()
	for i := 0; i < 100; i++ {
		mw.UsePriority(RequestPhase, Head, handler)
		mw.UsePriority(RequestPhase, Normal, handler)
		mw.UsePriority(RequestPhase, Tail, handler)
	}

	for n := 0; n < b.N; n++ {
		mw.Run(RequestPhase, w, req, http.HandlerFunc(nil))
	}
}
