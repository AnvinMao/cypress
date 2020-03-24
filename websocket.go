package cypress

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// WebSocketSession a connected web socket session
type WebSocketSession struct {
	RemoteAddr   string
	User         *UserPrincipal
	Session      *Session
	Context      map[string]interface{}
	connection   *websocket.Conn
	writeTimeout time.Duration
}

// Close close the underlying connection of the WebSocketSession
func (session *WebSocketSession) Close() error {
	return session.connection.Close()
}

// SendTextMessage sends a text message to the remote
func (session *WebSocketSession) SendTextMessage(text string) error {
	if session.writeTimeout > time.Duration(0) {
		session.connection.SetWriteDeadline(time.Now().Add(session.writeTimeout))
	}

	return session.connection.WriteMessage(websocket.TextMessage, []byte(text))
}

// SendBinaryMessage sends a binary message to the remote
func (session *WebSocketSession) SendBinaryMessage(data []byte) error {
	if session.writeTimeout > time.Duration(0) {
		session.connection.SetWriteDeadline(time.Now().Add(session.writeTimeout))
	}

	return session.connection.WriteMessage(websocket.BinaryMessage, data)
}

//WebSocketListener web socket listener that could be used to listen on a specific web socket endpoint
type WebSocketListener interface {
	// OnConnect when a connection is established
	OnConnect(session *WebSocketSession)

	// OnTextMessage when a text message is available in the channel
	OnTextMessage(session *WebSocketSession, text string)

	// OnBinaryMessage when a binary message is available in the channel
	OnBinaryMessage(session *WebSocketSession, data []byte)

	// OnClose when the channel is broken or closed by remote
	OnClose(session *WebSocketSession, reason int)
}

// PingMessageHandler websocket ping message handler, provide API to handle websocket ping messages
type PingMessageHandler interface {
	// OnPingMessage when a ping message is received, no need to send back pong message, which is done automatically
	OnPingMessage(session *WebSocketSession)
}

var upgrader = websocket.Upgrader{}

// WebSocketHandler Web socket handler
// have handler.Handle for router to enable web socket endpoints
type WebSocketHandler struct {
	MessageLimit     int64
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	Listener         WebSocketListener
	WriteCompression bool
}

// Handle handles the incomping web requests and try to upgrade the request into a websocket connection
func (handler *WebSocketHandler) Handle(writer http.ResponseWriter, request *http.Request) {
	conn, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		zap.L().Error("failed to upgrade the incoming connection to a websocket", zap.Error(err))
		writer.WriteHeader(http.StatusBadRequest)
		writer.Write([]byte("<h1>Bad request</h1>"))
		return
	}

	var userPrincipal *UserPrincipal
	var session *Session
	contextValue := request.Context().Value(SessionKey)
	if contextValue != nil {
		var ok bool
		session, ok = contextValue.(*Session)
		if !ok {
			zap.L().Error("invalid session object in SessionKey")
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte("<h1>bad server configuration</h1>"))
			return
		}
	}

	if session == nil {
		zap.L().Error("session handler is required for websocket handler")
		writer.WriteHeader(http.StatusServiceUnavailable)
		writer.Write([]byte("<h1>A http session is required</h1>"))
		return
	}

	contextValue = request.Context().Value(UserPrincipalKey)
	if contextValue != nil {
		userPrincipal, _ = contextValue.(*UserPrincipal)
	}

	if handler.MessageLimit > 0 {
		conn.SetReadLimit(handler.MessageLimit)
	}

	if handler.WriteCompression {
		conn.EnableWriteCompression(true)
	}

	webSocketSession := &WebSocketSession{request.RemoteAddr, userPrincipal, session, make(map[string]interface{}), conn, handler.WriteTimeout}
	handler.Listener.OnConnect(webSocketSession)
	go handler.connectionLoop(webSocketSession)
}

func (handler *WebSocketHandler) connectionLoop(session *WebSocketSession) {
	for {
		if handler.ReadTimeout > time.Duration(0) {
			session.connection.SetReadDeadline(time.Now().Add(handler.ReadTimeout))
		}

		msgType, data, err := session.connection.ReadMessage()
		if err != nil {
			zap.L().Error("failed to read from ws peer", zap.Error(err), zap.String("remoteAddr", session.RemoteAddr))
			handler.Listener.OnClose(session, websocket.CloseAbnormalClosure)
			session.connection.Close()
			return
		}

		switch msgType {
		case websocket.BinaryMessage:
			handler.Listener.OnBinaryMessage(session, data)
			break
		case websocket.TextMessage:
			handler.Listener.OnTextMessage(session, string(data))
			break
		case websocket.CloseMessage:
			handler.Listener.OnClose(session, websocket.CloseNormalClosure)
			session.connection.Close()
			return
		case websocket.PingMessage:
			h, ok := handler.Listener.(PingMessageHandler)
			if ok {
				h.OnPingMessage(session)
			}

			err = session.connection.WriteMessage(websocket.PongMessage, data)
			if err != nil {
				zap.L().Error("not able to write back pong message", zap.String("remoteAddr", session.RemoteAddr))
			}
		default:
			zap.L().Error("not able to handle message type", zap.Int("messageType", msgType), zap.String("remoteAddr", session.RemoteAddr))
			break
		}
	}
}
