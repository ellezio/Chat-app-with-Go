/*

User acquire sesstion token by login in.
For http server it is included in cookies.

The session has to be stored outside of http server - in redis or memcache.
Because when scaled horizontaly only one instance will know about user.
I thinking to also add some fingerprint on token to enhance security - in
short to not be able to use same token on diffrent devices.
At this moment it stays in `sessions` variable for simplicity.

*/

package session

import (
	"context"
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

var sessions = make(map[SessionId]Session)
var mutex = &sync.RWMutex{}

var sessionIdContextKey = "sessionId"
var sessionIdCookieKey = "sessionId"

type SessionId uuid.UUID

func (sid SessionId) String() string {
	return uuid.UUID(sid).String()
}

type UserData struct {
	Id   string
	Name string
}

type Session struct {
	Id   SessionId
	User UserData
}

func New() *Session {
	session := Session{
		Id:   SessionId(uuid.New()),
		User: UserData{},
	}

	mutex.Lock()
	sessions[session.Id] = session
	mutex.Unlock()

	return &session
}

func (s *Session) Save() {
	mutex.Lock()
	sessions[s.Id] = *s
	mutex.Unlock()
}

func GetSessionID(ctx context.Context) SessionId {
	if id, ok := ctx.Value(sessionIdContextKey).(SessionId); ok {
		return id
	}

	return SessionId(uuid.Nil)
}

func GetSessionByID(sID SessionId) *Session {
	mutex.RLock()
	defer mutex.RUnlock()

	if sID != SessionId(uuid.Nil) {
		if session, ok := sessions[sID]; ok {
			return &session
		}
	}

	return nil
}

func GetSession(ctx context.Context) *Session {
	id, ok := ctx.Value(sessionIdContextKey).(SessionId)
	if !ok {
		return nil
	}

	mutex.RLock()
	defer mutex.RUnlock()

	session, ok := sessions[id]
	if !ok {
		return nil
	}

	return &session
}

func IsLoggedIn(ctx context.Context) bool {
	session := GetSession(ctx)
	if session == nil {
		return false
	}

	return true
}

func ContextWithSessionId(ctx context.Context, sessionID SessionId) context.Context {
	return context.WithValue(ctx, sessionIdContextKey, sessionID)
}

func SetSessionCookie(w http.ResponseWriter, session *Session) {
	cookie := http.Cookie{
		Name:   sessionIdCookieKey,
		Value:  session.Id.String(),
		Path:   "/",
		MaxAge: 3600,
	}

	http.SetCookie(w, &cookie)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if cookie, err := r.Cookie(sessionIdCookieKey); err == nil {
			if sID, err := uuid.Parse(cookie.Value); err == nil {
				ctx = ContextWithSessionId(ctx, SessionId(sID))
			} else {
				log.Println(err)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetSessions() any {
	mutex.RLock()
	defer mutex.RUnlock()

	type Session struct {
		Id       string `json:"id"`
		Username string `json:"username"`
		ClientId string `json:"clientId"`
	}

	var result = make([]Session, 0, len(sessions))

	for id, session := range sessions {
		result = append(result, Session{uuid.UUID(id).String(), session.User.Name, session.User.Id})
	}

	return result
}
