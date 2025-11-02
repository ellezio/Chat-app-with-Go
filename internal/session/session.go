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

var sessions = make(map[SessionID]Session)
var mutex = &sync.RWMutex{}

var sessionIDContextKey = "sessionID"
var sessionIDCookieKey = "sessionID"

type SessionID uuid.UUID

func (sid SessionID) String() string {
	return uuid.UUID(sid).String()
}

type UserData struct {
	ID   string
	Name string
}

type Session struct {
	ID   SessionID
	User UserData
}

func New() *Session {
	session := Session{
		ID:   SessionID(uuid.New()),
		User: UserData{},
	}

	mutex.Lock()
	sessions[session.ID] = session
	mutex.Unlock()

	return &session
}

func (s *Session) Save() {
	mutex.Lock()
	sessions[s.ID] = *s
	mutex.Unlock()
}

func GetSessionID(ctx context.Context) SessionID {
	if id, ok := ctx.Value(sessionIDContextKey).(SessionID); ok {
		return id
	}

	return SessionID(uuid.Nil)
}

func GetSessionByID(sID SessionID) *Session {
	mutex.RLock()
	defer mutex.RUnlock()

	if sID != SessionID(uuid.Nil) {
		if session, ok := sessions[sID]; ok {
			return &session
		}
	}

	return nil
}

func GetSession(ctx context.Context) *Session {
	id, ok := ctx.Value(sessionIDContextKey).(SessionID)
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

func ContextWithSessionID(ctx context.Context, sessionID SessionID) context.Context {
	return context.WithValue(ctx, sessionIDContextKey, sessionID)
}

func SetSessionCookie(w http.ResponseWriter, session *Session) {
	cookie := http.Cookie{
		Name:   sessionIDCookieKey,
		Value:  session.ID.String(),
		Path:   "/",
		MaxAge: 3600,
	}

	http.SetCookie(w, &cookie)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if cookie, err := r.Cookie(sessionIDCookieKey); err == nil {
			if sID, err := uuid.Parse(cookie.Value); err == nil {
				ctx = ContextWithSessionID(ctx, SessionID(sID))
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
		ID       string `json:"id"`
		Username string `json:"username"`
		ClientID string `json:"client_id"`
	}

	var result = make([]Session, 0, len(sessions))

	for id, session := range sessions {
		result = append(result, Session{uuid.UUID(id).String(), session.User.Name, session.User.ID})
	}

	return result
}
