package session

import (
	"context"
	"fmt"
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

type Session struct {
	ID       SessionID
	Username string
	ClientID string
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
		session := Session{SessionID(uuid.New()), auth.Username, ""}
		sessions[session.ID] = session
		return &session
	}

	return nil
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

func SetClientID(sessionID SessionID, clientID string) error {
	mutex.Lock()
	defer mutex.Unlock()

	session, ok := sessions[sessionID]
	if !ok {
		return fmt.Errorf("Session with ID %s not found.", sessionID)
	}

	session.ClientID = clientID
	sessions[sessionID] = session

	return nil
}

func Context(ctx context.Context, sessionID SessionID) context.Context {
	return context.WithValue(ctx, sessionIDContextKey, sessionID)
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
		ctx := r.Context()

		if cookie, err := r.Cookie(string(sessionIDCookieKey)); err == nil {
			if sID, err := uuid.Parse(cookie.Value); err == nil {
				ctx = Context(ctx, SessionID(sID))
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
		result = append(result, Session{uuid.UUID(id).String(), session.Username, session.ClientID})
	}

	return result
}
