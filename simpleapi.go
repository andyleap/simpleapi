package simpleapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

var allMethods = []string{"CONNECT",
	"DELETE",
	"GET",
	"HEAD",
	"OPTIONS",
	"PATCH",
	"POST",
	"PUT",
	"TRACE"}

type API struct {
	paths *path
}

func New() *API {
	return &API{
		paths: newPath(),
	}
}

type path struct {
	subpaths  map[string]*path
	parameter *path

	methods map[string]*handler
}

func newPath() *path {
	return &path{
		subpaths: map[string]*path{},
		methods:  map[string]*handler{},
	}
}

type handler struct {
	v        reflect.Value
	params   []param
	jsonBody int
}

type param struct {
	path int
	arg  int
}

func (h *handler) call(params []string, w http.ResponseWriter, r *http.Request) {
	t := h.v.Type()
	args := make([]reflect.Value, t.NumIn())
	if h.jsonBody > 0 {
		args[h.jsonBody] = reflect.New(t.In(h.jsonBody)).Elem()
		dec := json.NewDecoder(r.Body)
		dec.Decode(args[h.jsonBody].Interface())
	}
	for _, p := range h.params {
		args[p.arg] = reflect.New(t.In(p.arg)).Elem()
		switch args[p.arg].Kind() {
		case reflect.String:
			args[p.arg].SetString(params[p.path])
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			i, _ := strconv.ParseInt(params[p.path], 10, 64)
			args[p.arg].SetInt(i)
		case reflect.Float32, reflect.Float64:
			f, _ := strconv.ParseFloat(params[p.path], 64)
			args[p.arg].SetFloat(f)
		}
	}
	out := h.v.Call(args)
	if len(out) == 2 {
		if out[1].Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !out[1].IsNil() {
				w.WriteHeader(500)
				return
			}
		}
	}
	if out[0].Kind() != reflect.Chan {
		enc := json.NewEncoder(w)
		enc.Encode(out[0].Interface())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for {
		d, ok := out[0].Recv()
		if !ok {
			break
		}
		out, _ := json.Marshal(d.Interface())
		w.Write([]byte("data: "))
		w.Write(out)
		w.Write([]byte("\n\n"))
	}
}

func (a *API) Route(route string) *Route {
	return &Route{
		api:     a,
		route:   route,
		handler: &handler{},
	}
}

type Route struct {
	api     *API
	route   string
	methods []string

	handler *handler
}

func (r *Route) To(f interface{}) {
	v := reflect.ValueOf(f)
	r.handler.v = v
	r.api.addRoute(r)
}

func (r *Route) Body(arg int) {
	r.handler.jsonBody = arg
}

func (a *API) addRoute(r *Route) {
	pathsections := strings.Split(r.route, "/")
	params := []param{}
	p := a.paths
	paramnum := 0
	for _, v := range pathsections {
		if v == "" {
			continue
		}
		if strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}") {
			arg, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSuffix(v, "}"), "{"))
			if err == nil {
				if arg > 0 {
					params = append(params, param{
						path: paramnum,
						arg:  arg,
					})
				}
				paramnum++
				subp := p.parameter
				if subp == nil {
					subp = newPath()
					p.parameter = subp
				}
				p = subp
				continue
			}
		}
		subp, ok := p.subpaths[v]
		if !ok {
			subp = newPath()
			p.subpaths[v] = subp
		}
		p = subp
	}
	r.handler.params = params
	if len(r.methods) == 0 {
		r.methods = allMethods
	}
	for _, m := range r.methods {
		p.methods[m] = r.handler
	}
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pathsections := strings.Split(r.URL.Path, "/")
	p := a.paths
	params := []string{}
	for _, v := range pathsections {
		if v == "" {
			continue
		}
		if subp, ok := p.subpaths[v]; ok {
			p = subp
			continue
		}
		if p.parameter != nil {
			params = append(params, v)
			p = p.parameter
			continue
		}
		w.WriteHeader(404)
		return
	}
	m, ok := p.methods[r.Method]
	if !ok {
		w.WriteHeader(404)
		return
	}
	m.call(params, w, r)
}
