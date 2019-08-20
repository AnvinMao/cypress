package cypress

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type TestController struct {
}

func (c *TestController) Action1(req *http.Request, resp *Response) {
	resp.DoneWithContent(200, "text/plain", []byte("action1"))
}

func TestAutoController(t *testing.T) {
	tc := &TestController{}
	actions := AsController(tc)()
	if len(actions) != 1 {
		t.Error("expected one action in the list but got", len(actions))
		return
	}
}

type TestUserProvider struct{}

func (p *TestUserProvider) GetName() string {
	return "testProvider"
}

func (p *TestUserProvider) Authenticate(r *http.Request) *UserPrincipal {
	ticket := r.URL.Query().Get("ticket")
	if ticket != "" {
		return &UserPrincipal{
			ID:     ticket,
			Name:   ticket,
			Domain: "test",
			Roles:  make([]string, 0),
		}
	}

	return nil
}

func (p *TestUserProvider) Load(domain, id string) *UserPrincipal {
	return &UserPrincipal{
		ID:     id,
		Domain: domain,
		Name:   id,
		Roles:  make([]string, 0),
	}
}

type TestWsListener struct{}

func (l *TestWsListener) OnConnect(session *WebSocketSession) {
	fmt.Println("a websocket with session id", session.Session.ID, "is connected")
}

func (l *TestWsListener) OnClose(session *WebSocketSession, reason int) {
	fmt.Println("a websocket with session id", session.Session.ID, "has closed with reason", reason)
}

func (l *TestWsListener) OnTextMessage(session *WebSocketSession, message string) {
	err := session.SendTextMessage(message)
	if err != nil {
		fmt.Println("failed to send message due to error", err)
	}
}

func (l *TestWsListener) OnBinaryMessage(session *WebSocketSession, message []byte) {
	err := session.SendBinaryMessage(message)
	if err != nil {
		fmt.Println("failed to send message due to error", err)
	}
}

func testActions(t *testing.T) []Action {
	actions := []Action{
		Action{
			Name: "greeting",
			Handler: ActionHandler(func(r *http.Request, response *Response) {
				response.DoneWithContent(http.StatusAccepted, "text/html", []byte(fmt.Sprintf("<h1>Hello, %s</h1>", r.URL.String())))

				session, _ := r.Context().Value(SessionKey).(*Session)
				if session != nil {
					fmt.Println("SESSID:", session.ID)
				} else {
					t.Error("no session detected while one expected")
				}
			}),
		},
		Action{
			Name: "panic",
			Handler: ActionHandler(func(r *http.Request, response *Response) {
				panic("ask for panic")
			}),
		},
		Action{
			Name: "index",
			Handler: ActionHandler(func(request *http.Request, response *Response) {
				model := &TestModel{
					Title:   "Page Title",
					Message: "Page Content",
				}

				response.DoneWithTemplate(http.StatusOK, "index", model)
			}),
		},
	}

	return actions[:]
}

func printSessionID(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		traceID := GetTraceID(request.Context())
		session := GetSession(request)
		fmt.Println("printSessionID:", traceID, session.ID)
		handler.ServeHTTP(writer, request)
	})
}

func printSessionIDEx(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		traceID := GetTraceID(request.Context())
		session := GetSession(request)
		fmt.Println("printSessionIDEx:", traceID, session.ID)
		handler.ServeHTTP(writer, request)
	})
}

func TestWebServer(t *testing.T) {
	// test setup
	// create test folder
	testDir, err := ioutil.TempDir("", "cytpltest")
	if err != nil {
		t.Error("failed to create test dir", err)
		return
	}

	defer os.RemoveAll(testDir)

	// write template files
	err = ioutil.WriteFile(path.Join(testDir, "header.tmpl"), []byte("{{define \"header\"}}{{.}}{{end}}"), os.ModePerm)
	if err != nil {
		t.Error("failed to setup header.tmpl")
		return
	}

	err = ioutil.WriteFile(path.Join(testDir, "index.tmpl"), []byte("{{define \"index\"}}{{template \"header\" .Title}}{{.Message}}{{add 1 1}}{{end}}"), os.ModePerm)
	if err != nil {
		t.Error("failed to setup index.tmpl")
		return
	}

	writer := NewBufferWriter()
	SetupLogger(LogLevelDebug, writer)
	tmplMgr := NewTemplateManager(testDir, ".tmpl", time.Second*10, func(root *template.Template) {
		root.Funcs(template.FuncMap{
			"add": func(a, b int) int {
				return a + b
			},
		})
	}, func(path string) bool {
		return strings.HasSuffix(path, "header.tmpl")
	})
	defer tmplMgr.Close()
	server := NewWebServer(":8099", NewSkinManager(tmplMgr))
	defer server.Shutdown()

	sessionStore := NewInMemorySessionStore()
	defer sessionStore.Close()

	server.AddUserProvider(&TestUserProvider{})
	server.WithSessionOptions(sessionStore, 15*time.Minute)
	server.WithStandardRouting("/web")
	server.WithCaptcha("/captcha")
	server.AddWsEndoint("/ws/echo", &TestWsListener{})
	server.RegisterController("test", ControllerFunc(func() []Action { return testActions(t) }))
	server.RegisterController("test1", AsController(&TestController{}))
	server.WithCustomHandler(CustomHandlerFunc(printSessionID))
	server.WithCustomHandler(CustomHandlerFunc(printSessionIDEx))

	startedChan := make(chan bool)
	go func() {
		startedChan <- true
		if err := server.Start(); err != nil {
			fmt.Println(err)
		}
	}()

	// wait for the server to start
	<-startedChan
	time.Sleep(time.Millisecond * 100)
	resp, err := http.Get("http://localhost:8099/web/test/greeting?ticket=test")
	if err != nil {
		t.Error("server is not started or working properly", err)
		return
	}

	if resp.StatusCode != http.StatusAccepted {
		t.Error("Unexpected http status", resp.Status)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		t.Error("failed to read body", err)
		return
	}

	resp.Body.Close()
	if "<h1>Hello, /web/test/greeting?ticket=test</h1>" != string(body) {
		t.Error("unexpected response", string(body))
		return
	}

	type routerLog struct {
		Message    string `json:"msg"`
		Controller string `json:"controller"`
		Action     string `json:"action"`
		TraceID    string `json:"activityId"`
	}

	type apiLog struct {
		Message    string `json:"msg"`
		TraceID    string `json:"activityId"`
		URI        string `json:"requestUri"`
		Path       string `json:"path"`
		Method     string `json:"requestMethod"`
		User       string `json:"user"`
		StatusCode int    `json:"responseStatus"`
	}

	if len(writer.Buffer) != 5 {
		t.Error("expecting 5 log items but got", len(writer.Buffer))
		return
	}

	log1 := routerLog{}
	log2 := apiLog{}
	err = json.Unmarshal(writer.Buffer[3], &log1)
	if err != nil {
		t.Error("bad log item", err)
		return
	}

	if "test" != log1.Controller {
		t.Error("expecting test but got", log1.Controller)
		return
	}

	if "greeting" != log1.Action {
		t.Error("expecting greeting but got", log1.Action)
		return
	}

	err = json.Unmarshal(writer.Buffer[4], &log2)

	if err != nil {
		t.Error("bad log item", err)
		return
	}

	if "/web/test/greeting" != log2.Path {
		t.Error("expecting /web/test/greeting but got", log2.Path)
		return
	}

	if 202 != log2.StatusCode {
		t.Error("expecting 202 but got", log2.StatusCode)
		return
	}

	if log1.TraceID != log2.TraceID {
		t.Error(log1.TraceID, log2.TraceID, "expecting to be matched")
		return
	}

	resp, err = http.Get("http://localhost:8099/web/test1/action1")
	if err != nil {
		t.Error("server is not started or working properly", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		t.Error("Unexpected http status", resp.Status)
		return
	}

	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		t.Error("failed to read body", err)
		return
	}

	resp.Body.Close()
	if "action1" != string(body) {
		t.Error("unexpected response", string(body))
		return
	}

	resp, err = http.Get("http://localhost:8099/web/test1/action2")
	if err != nil {
		t.Error("server is not started or working properly", err)
		return
	}

	if resp.StatusCode != http.StatusNotFound {
		t.Error("Unexpected http status", resp.Status)
		return
	}

	resp.Body.Close()

	resp, err = http.Get("http://localhost:8099/web/test/index")
	if err != nil {
		t.Error("server is not started or working properly", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		t.Error("Unexpected http status", resp.Status)
		return
	}

	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Error("failed to read body")
		return
	}

	if "Page TitlePage Content2" != string(body) {
		t.Error("unexpected response body", string(body))
		return
	}

	resp, err = http.Get("http://localhost:8099/captcha?sessid=abc123")
	if err != nil {
		t.Error("server is not started or working properly", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		t.Error("Unexpected http status", resp.Status)
		return
	}

	sess, _ := sessionStore.Get("abc123")
	val, _ := sess.GetValue("captcha")
	fmt.Println("challenge", val.(string))

	defer resp.Body.Close()

	resp, err = http.Get("http://localhost:8099/captcha")
	if err != nil {
		t.Error("server is not started or working properly", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		t.Error("Unexpected http status", resp.Status)
		return
	}

	defer resp.Body.Close()

	// try websocket
	c, _, err := websocket.DefaultDialer.Dial("ws://localhost:8099/ws/echo", nil)
	if err != nil {
		t.Error("dial:", err)
		return
	}

	defer c.Close()
	c.WriteMessage(websocket.TextMessage, []byte("Hello, websocket!"))
	msgType, msg, err := c.ReadMessage()
	if msgType != websocket.TextMessage || err != nil || string(msg) != "Hello, websocket!" {
		t.Error("failed to read back the message")
	}
}
