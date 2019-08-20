package cypress

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/dchest/captcha"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

var (
	// ErrDupActionName mulitple actions are having the same name for the same controller
	ErrDupActionName = errors.New("action name duplicated")

	// ServerName name of the app
	ServerName = "cypress"

	// ServerVersion version of the server
	ServerVersion = "v0.1.1110"

	// CaptchaKey captcha key in session
	CaptchaKey = "captcha"

	// CaptchaSessionKey captcha alternative session key parameter name
	CaptchaSessionKey = "sessid"

	// NotFoundMsg message to be shown when resource not found
	NotFoundMsg = "Sorry, we are not able to find the resource you requested"

	requestType  = reflect.TypeOf(http.Request{})
	responseType = reflect.TypeOf(Response{})

	errorTemplate, _ = template.New("errorTemplate").Parse(`<!DOCTYPE html>
	<html>
		<head>
			<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
			<meta name="viewport" content="width=device-width, initial-scale=1">
			<title>{{.StatusCode}}-{{.Server}}({{.Version}})</title>
			<style>
			body {
				font-size:12px;
			}
			</style>
		</head>
		<body>
			<h1 style="text-align:center;">{{.StatusCode}}</h1>
			<h2 style="text-align:center;">{{.Message}}</h2>
			<div style="margin:20px 10% 0 10%;text-align:center;color:#999;border-top:solid 1px #c9c9c9;line-height:30px;">
				Powered by {{.Server}} - {{.Version}}
			</div>
		</body>
	</html>`)
)

// Response web response
type Response struct {
	traceID string
	tmplMgr *TemplateManager
	writer  http.ResponseWriter
}

type errorPage struct {
	StatusCode int
	Message    string
	Server     string
	Version    string
}

// ActionHandler action handler for standard routing
type ActionHandler func(request *http.Request, response *Response)

// Action a named http.Handler
type Action struct {
	Name    string
	Handler ActionHandler
}

// Controller a request controller that could provide a set of
// http.Handler to handle http requests based on the action name
type Controller interface {
	ListActions() []Action
}

// ControllerFunc a function that implements Controller interface
type ControllerFunc func() []Action

// ListActions adapt the ControllerFunc to Controller interface
func (f ControllerFunc) ListActions() []Action {
	return f()
}

// CustomHandler a handler that is executed after session is setup/restored
type CustomHandler interface {
	// PipelineWith returns a http.Handler that will execute the given handler
	// after the custom handler code is done.
	PipelineWith(handler http.Handler) http.Handler
}

// CustomHandlerFunc a function that implements CustomHandler
type CustomHandlerFunc func(handler http.Handler) http.Handler

// PipelineWith implements CustomHandler interface
func (h CustomHandlerFunc) PipelineWith(handler http.Handler) http.Handler {
	return h(handler)
}

// WebServer a web server that supports auth & authz, logging,
// session and web sockets
type WebServer struct {
	server             *http.Server
	router             *mux.Router
	securityHandler    *SecurityHandler
	skinManager        *SkinManager
	sessionStore       SessionStore
	sessionTimeout     time.Duration
	registeredHandlers map[string]map[string]ActionHandler
	customHandler      CustomHandler
	captchaDigits      int
	captchaWidth       int
	captchaHeight      int
}

// SendError complete the request by sending an error message to the client
func SendError(writer http.ResponseWriter, statusCode int, errorMsg string) {
	writer.Header().Add("Content-Type", "text/html; charset=UTF-8")
	writer.WriteHeader(statusCode)
	errorTemplate.Execute(writer, &errorPage{statusCode, errorMsg, ServerName, ServerVersion})
}

// AsController enumerates all accessible member functions of c
// which has two parameters and *http.Request as the first one
// while *Response as the second one as Actions
func AsController(c interface{}) ControllerFunc {
	return ControllerFunc(func() []Action {
		t := reflect.TypeOf(c)
		actions := make([]Action, 0, 8)
		for i := 0; i < t.NumMethod(); i = i + 1 {
			m := t.Method(i)
			t := m.Func.Type()
			if t.NumIn() != 3 {
				continue
			}

			typeOfParam1 := t.In(1)
			typeOfParam2 := t.In(2)
			if typeOfParam1.Kind() != reflect.Ptr || typeOfParam2.Kind() != reflect.Ptr {
				continue
			}

			typeOfParam1 = typeOfParam1.Elem()
			typeOfParam2 = typeOfParam2.Elem()
			if requestType.AssignableTo(typeOfParam1) &&
				responseType.AssignableTo(typeOfParam2) {
				actions = append(actions, Action{
					Name: strings.ToLower(m.Name[0:1]) + m.Name[1:],
					Handler: ActionHandler(func(request *http.Request, response *Response) {
						args := []reflect.Value{reflect.ValueOf(c), reflect.ValueOf(request), reflect.ValueOf(response)}
						m.Func.Call(args[:])
					}),
				})
			}
		}

		return actions
	})
}

// SetHeader sets a header value for response
func (r *Response) SetHeader(name, value string) {
	r.writer.Header().Add(name, value)
}

// SetCookie add cookie to response
func (r *Response) SetCookie(cookie *http.Cookie) {
	http.SetCookie(r.writer, cookie)
}

// SetStatus sets the response status
func (r *Response) SetStatus(statusCode int) {
	r.writer.WriteHeader(statusCode)
}

// Write writes content to response
func (r *Response) Write(content []byte) {
	r.writer.Write(content)
}

// SetNoCache sets headers for the client not to cache the response
func (r *Response) SetNoCache() {
	r.SetHeader("Expires", "Sat, 6 May 1995 12:00:00 GMT")
	r.SetHeader("Cache-Control", "no-store, no-cache, must-revalidate")
	r.SetHeader("Pragma", "no-cache")
}

// DoneWithRedirect redirects to the specified url
func (r *Response) DoneWithRedirect(req *http.Request, url string, status int) {
	http.Redirect(r.writer, req, url, status)
}

// DoneWithContent sets the status, content-type header and writes
// the content to response
func (r *Response) DoneWithContent(statusCode int, contentType string, content []byte) {
	r.SetStatus(statusCode)
	r.SetHeader("Content-Type", contentType)
	r.Write(content)
}

// DoneWithError response an error page based on errorTemplate to the client
func (r *Response) DoneWithError(statusCode int, msg string) {
	r.SetStatus(statusCode)
	r.SetHeader("Content-Type", "text/html; charset=utf8")
	errorTemplate.Execute(r.writer, &errorPage{statusCode, msg, ServerName, ServerVersion})
}

// DoneWithTemplate sets the status and write the model with the given template name as
// response, the content type is defaulted to text/html
func (r *Response) DoneWithTemplate(statusCode int, name string, model interface{}) {
	tmpl, ok := r.tmplMgr.GetTemplate(name)
	if !ok {
		zap.L().Error("templateNotFound", zap.String("name", name), zap.String("activityId", r.traceID))
		SendError(r.writer, 500, "service configuration error")
		return
	}

	r.SetStatus(statusCode)
	r.SetHeader("Content-Type", "text/html; charset=UTF-8")
	err := tmpl.ExecuteTemplate(r.writer, filepath.Base(name), model)
	if err != nil {
		zap.L().Error("failedToExecuteTemplate", zap.Error(err), zap.String("name", name), zap.String("activityId", r.traceID))
		errorTemplate.Execute(r.writer, &errorPage{statusCode, "template error", ServerName, ServerVersion})
		return
	}
}

// DoneWithJSON sets the status and write the model as json
func (r *Response) DoneWithJSON(statusCode int, obj interface{}) {
	r.SetStatus(statusCode)
	r.SetHeader("Content-Type", "application/json; charset=UTF-8")
	encoder := json.NewEncoder(r.writer)
	err := encoder.Encode(obj)
	if err != nil {
		zap.L().Error("failedToEncodeJson", zap.Error(err), zap.String("activityId", r.traceID))
		r.Write([]byte(""))
	}
}

// NewWebServer creates a web server instance to listen on the
// specified address
func NewWebServer(listenAddr string, skinMgr *SkinManager) *WebServer {
	return &WebServer{
		server: &http.Server{
			Addr: listenAddr,
		},
		router:             mux.NewRouter(),
		securityHandler:    NewSecurityHandler(),
		skinManager:        skinMgr,
		sessionTimeout:     time.Minute * 30,
		registeredHandlers: make(map[string]map[string]ActionHandler),
		customHandler:      nil,
		captchaDigits:      6,
		captchaWidth:       captcha.StdWidth,
		captchaHeight:      captcha.StdHeight,
	}
}

// HandleFunc register a handle function for a path pattern
func (server *WebServer) HandleFunc(path string, f func(w http.ResponseWriter, r *http.Request)) *WebServer {
	server.router.HandleFunc(path, f)
	return server
}

// WithStandardRouting setup a routing as "prefix" + "/{controller:[_a-zA-Z][_a-zA-Z0-9]*}/{action:[_a-zA-Z][_a-zA-Z0-9]*}"
// and the web server will route the requests based on the registered controllers.
func (server *WebServer) WithStandardRouting(prefix string) *WebServer {
	server.router.HandleFunc(prefix+"/{controller:[_a-zA-Z][_a-zA-Z0-9]*}/{action:[_a-zA-Z][_a-zA-Z0-9]*}", server.routeRequest)
	return server
}

// WithCaptchaCustom setup a captcha generator at the given path with custom digits, width and height
func (server *WebServer) WithCaptchaCustom(path string, digits, width, height int) *WebServer {
	server.captchaDigits = digits
	server.captchaWidth = width
	server.captchaHeight = height
	server.router.HandleFunc(path, server.createCaptcha)
	return server
}

// WithCaptcha setup a captcha generator at the given "path" in a 240 x 80 image with six digits chanllege
func (server *WebServer) WithCaptcha(path string) *WebServer {
	server.router.HandleFunc(path, server.createCaptcha)
	return server
}

// RegisterController register a controller for the standard routing
func (server *WebServer) RegisterController(name string, controller Controller) error {
	actions, ok := server.registeredHandlers[name]
	if !ok {
		actions = make(map[string]ActionHandler)
		server.registeredHandlers[name] = actions
	}

	for _, item := range controller.ListActions() {
		_, ok = actions[item.Name]
		if ok {
			return ErrDupActionName
		}

		actions[item.Name] = item.Handler
	}

	return nil
}

// AddUserProvider adds a user provider to security handler
func (server *WebServer) AddUserProvider(provider UserProvider) *WebServer {
	server.securityHandler.AddUserProvider(provider)
	return server
}

// WithAuthz specify the AuthorizationManager to be used by this handler
func (server *WebServer) WithAuthz(authz AuthorizationManager) *WebServer {
	server.securityHandler.WithAuthz(authz)
	return server
}

// WithLoginURL the URL to redirect if the access is denied
func (server *WebServer) WithLoginURL(loginURL string) *WebServer {
	server.securityHandler.WithLoginURL(loginURL)
	return server
}

//WithCustomHandler set or chains a handler to custom handlers chain, the new
// CustomHandler will be added to the tail of custom handlers chain.
func (server *WebServer) WithCustomHandler(handler CustomHandler) *WebServer {
	if server.customHandler == nil {
		server.customHandler = handler
	} else {
		existingHandler := server.customHandler
		server.customHandler = CustomHandlerFunc(func(h http.Handler) http.Handler {
			newHandler := handler.PipelineWith(h)
			return existingHandler.PipelineWith(newHandler)
		})
	}

	return server
}

// AddWsEndoint adds a web socket endpoint to the server
func (server *WebServer) AddWsEndoint(endpoint string, listener WebSocketListener) *WebServer {
	wsHandler := &WebSocketHandler{
		Listener: listener,
	}
	server.router.HandleFunc(endpoint, wsHandler.Handle)
	return server
}

// AddStaticResource adds a static resource folder to the server with the given prefix,
// the prefix must be in format of "/prefix/"
func (server *WebServer) AddStaticResource(prefix, dir string) *WebServer {
	server.router.PathPrefix(prefix).Handler(http.StripPrefix(prefix, http.FileServer(http.Dir(dir))))
	return server
}

// WithSessionOptions setup the session options including the session store and session timeout interval
func (server *WebServer) WithSessionOptions(store SessionStore, timeout time.Duration) *WebServer {
	server.sessionStore = store
	server.sessionTimeout = timeout
	return server
}

// Shutdown shutdown the web server
func (server *WebServer) Shutdown() {
	server.server.Shutdown(nil)
}

// Start starts the web server
func (server *WebServer) Start() error {
	server.router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		SendError(w, 404, NotFoundMsg)
	})
	handler := http.Handler(server.securityHandler.WithPipeline(server.router))
	if server.customHandler != nil {
		handler = server.customHandler.PipelineWith(handler)
	}

	handler = NewSessionHandler(handler, server.sessionStore, server.sessionTimeout)
	handler = LoggingHandler(handler)
	handler = handlers.ProxyHeaders(handler)
	http.Handle("/", handler)
	return server.server.ListenAndServe()
}

func (server *WebServer) routeRequest(writer http.ResponseWriter, request *http.Request) {
	routeVars := mux.Vars(request)
	zap.L().Debug("routeRequest", zap.String("controller", routeVars["controller"]), zap.String("action", routeVars["action"]), zap.String("activityId", GetTraceID(request.Context())))
	if routeVars != nil {
		actions, ok := server.registeredHandlers[routeVars["controller"]]
		if ok {
			handler, ok := actions[routeVars["action"]]
			if ok {
				tmplMgr, name := server.skinManager.ApplySelector(request)
				if tmplMgr == nil {
					zap.L().Error("skinNotFound", zap.String("skin", name), zap.String("activityId", GetTraceID(request.Context())))
					SendError(writer, http.StatusInternalServerError, "Bad skin selected for the request")
					return
				}

				response := &Response{
					traceID: GetTraceID(request.Context()),
					tmplMgr: tmplMgr,
					writer:  writer,
				}
				handler(request, response)
				return
			}
		}
	}

	SendError(writer, http.StatusNotFound, NotFoundMsg)
}

func (server *WebServer) createCaptcha(writer http.ResponseWriter, request *http.Request) {
	var session *Session
	var err error
	customSession := false
	sessid := request.FormValue(CaptchaSessionKey)
	if sessid != "" {
		session, err = server.sessionStore.Get(sessid)
		if err != nil && err != ErrSessionNotFound {
			zap.L().Error("failed to lookup session store", zap.Error(err))
			SendError(writer, http.StatusInternalServerError, "unknown error")
			return
		}

		if session == nil {
			session = NewSession(sessid)
		}

		customSession = true
	}

	if session == nil {
		session = GetSession(request)
	}

	if session == nil {
		SendError(writer, http.StatusServiceUnavailable, "session is required for handling captcha")
		return
	}

	digits := captcha.RandomDigits(server.captchaDigits)
	image := captcha.NewImage(session.ID, digits, server.captchaWidth, server.captchaHeight)
	if image == nil {
		SendError(writer, http.StatusInternalServerError, "failed to generate captcha image")
		return
	}

	for i := range digits {
		digits[i] += 48
	}

	session.SetValue(CaptchaKey, string(digits))

	if customSession {
		err := server.sessionStore.Save(session, time.Minute*5)
		if err != nil {
			zap.L().Error("failed to save session", zap.Error(err))
			SendError(writer, http.StatusInternalServerError, "unknown server error")
			return
		}
	}

	writer.Header().Add("Expires", "Sat, 6 May 1995 12:00:00 GMT")
	writer.Header().Add("Cache-Control", "no-store, no-cache, must-revalidate")
	writer.Header().Add("Pragma", "no-cache")
	writer.Header().Add("Content-Type", "image/png")
	image.WriteTo(writer)
}
