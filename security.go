package cypress

import (
	"net/http"
)

// UserPrincipal the security principal of the http session
type UserPrincipal struct {
	// ID user id
	ID string

	// Domain the domain of the user principal
	Domain string

	// Name user's printable name
	Name string

	// Provider user provider
	Provider string

	// Roles roles for the user
	Roles []string

	// Self pointing to the object that owns this object
	// this makes the UserPrincipal extensible and embeddable
	// The container object can put its pointer into this field
	// and could be deferenced by the application code
	Self interface{}
}

// UserProvider an interface used by the framework to resolve UserPrincipal
// from incoming request and also used by loading the UserPrincipal in authorized
// mode by given user id and user domain
type UserProvider interface {
	// GetName gets the name of the provider
	GetName() string

	// Authenticate authenticates the incoming request and
	// returns the UserPrincipal object if security token
	// is found and resolvable by the user provider or nil
	// if nothing can be resolved
	Authenticate(request *http.Request) *UserPrincipal

	//Load loads the user by the specified domain and id
	Load(domain, id string) *UserPrincipal
}

// AuthorizationManager an interface used by the security handler to check
// if the given user has permission to use the specified method and access
// the given path
type AuthorizationManager interface {
	// CheckAccess check the user to see if the user has permission to access
	// path with the given http method
	CheckAccess(user *UserPrincipal, method, path string) bool

	// CheckAnonymousAccessible checks the given path can be accessed by anonymous
	// user with the given http method
	CheckAnonymousAccessible(method, path string) bool
}

// SecurityHandler a http handler to do both authentication and authorization
// for requests
type SecurityHandler struct {
	userProviders []UserProvider
	authzMgr      AuthorizationManager
	pipeline      http.Handler
	loginURL      string
}

// NewSecurityHandler creates an instance of SecurityHandler object without any
// user providers and with nil AuthorizationManager
func NewSecurityHandler() *SecurityHandler {
	return &SecurityHandler{
		userProviders: make([]UserProvider, 0, 2),
		authzMgr:      nil,
	}
}

// AddUserProvider add a user provider to the security handler
func (handler *SecurityHandler) AddUserProvider(userProvider UserProvider) *SecurityHandler {
	handler.userProviders = append(handler.userProviders, userProvider)
	return handler
}

// WithAuthz specify the AuthorizationManager to be used by this handler
func (handler *SecurityHandler) WithAuthz(authz AuthorizationManager) *SecurityHandler {
	handler.authzMgr = authz
	return handler
}

// WithPipeline specify the http.Handler to be called if the request passes
// the security check
func (handler *SecurityHandler) WithPipeline(h http.Handler) *SecurityHandler {
	handler.pipeline = h
	return handler
}

// WithLoginURL the URL to redirect if the access is denied
func (handler *SecurityHandler) WithLoginURL(loginURL string) *SecurityHandler {
	handler.loginURL = loginURL
	return handler
}

// GetUser gets the UserPrincipal for the request
func GetUser(request *http.Request) *UserPrincipal {
	value := request.Context().Value(UserPrincipalKey)
	if value != nil {
		if user, ok := value.(*UserPrincipal); ok {
			return user
		}
	}

	return nil
}

// ServeHTTP implements the http.Handler interface
func (handler *SecurityHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if handler.authzMgr == nil ||
		handler.authzMgr.CheckAnonymousAccessible(request.Method, request.URL.Path) {
		handler.pipeline.ServeHTTP(writer, request)
		return
	}

	var userPrincipal *UserPrincipal
	for _, provider := range handler.userProviders {
		userPrincipal = provider.Authenticate(request)
		if userPrincipal != nil {
			userPrincipal.Provider = provider.GetName()
			break
		}
	}

	if userPrincipal != nil {
		request.Context().(*multiValueCtx).withValue(UserPrincipalKey, userPrincipal)
	}

	if userPrincipal != nil && handler.authzMgr.CheckAccess(userPrincipal, request.Method, request.URL.Path) {
		handler.pipeline.ServeHTTP(writer, request)
	} else {
		if handler.loginURL == "" {
			SendError(writer, http.StatusForbidden, "Access denied")
		} else {
			http.Redirect(writer, request, handler.loginURL, http.StatusTemporaryRedirect)
		}
	}
}
