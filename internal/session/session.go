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

var sessionContextKey = "username"
var sessionIDCookieKey = "sessionID"

type SessionID uuid.UUID

type Session struct {
	ID       SessionID
	Username string
}

func (self *Session) GetID() string {
	return uuid.UUID(self.ID).String()
}

type AuthData struct {
	Username string
}

func New(auth AuthData) *Session {
	mutex.Lock()
	defer mutex.Unlock()

	if auth.Username != "" {
		session := Session{SessionID(uuid.New()), auth.Username}
		sessions[session.ID] = session
		return &session
	}

	return nil
}

func getSessionByID(sID string) *Session {
	mutex.RLock()
	defer mutex.RUnlock()

	id, err := uuid.Parse(sID)
	if err != nil {
		log.Println(err)
		return nil
	}

	if SessionID(id) != SessionID(uuid.Nil) {
		if session, ok := sessions[SessionID(id)]; ok {
			return &session
		}
	}

	return nil
}

func GetSession(ctx context.Context) *Session {
	if session, ok := ctx.Value(sessionContextKey).(*Session); ok {
		return session
	}

	return nil
}

func GetUsername(ctx context.Context) string {
	session := GetSession(ctx)
	if session == nil {
		return ""
	}

	return session.Username
}

func IsLoggedIn(ctx context.Context) bool {
	session := GetSession(ctx)
	if session == nil {
		return false
	}

	return true
}

func SetSessionCookie(w http.ResponseWriter, session *Session) {
	cookie := http.Cookie{
		Name:   sessionIDCookieKey,
		Value:  session.GetID(),
		Path:   "/",
		MaxAge: 3600,
	}

	http.SetCookie(w, &cookie)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var session *Session

		if cookie, err := r.Cookie(string(sessionIDCookieKey)); err == nil {
			session = getSessionByID(cookie.Value)
		}

		ctx := context.WithValue(r.Context(), sessionContextKey, session)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetSessions() any {
	mutex.RLock()
	defer mutex.RUnlock()

	type Session struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}

	var result = make([]Session, 0, len(sessions))

	for id, session := range sessions {
		result = append(result, Session{uuid.UUID(id).String(), session.Username})
	}

	return result
}
