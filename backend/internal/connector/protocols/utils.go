package protocols

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/samber/lo"
	"github.com/spf13/cast"
	"go.uber.org/zap"

	myi18n "github.com/veops/oneterm/internal/i18n"
	"github.com/veops/oneterm/internal/model"
	fileservice "github.com/veops/oneterm/internal/service/file"
	gsession "github.com/veops/oneterm/internal/session"
	"github.com/veops/oneterm/pkg/config"
	myErrors "github.com/veops/oneterm/pkg/errors"
	"github.com/veops/oneterm/pkg/logger"
)

var (
	// ErrSessionClosed is a sentinel error for normal session termination
	// This is returned when a session is closed normally (e.g., user exits)
	ErrSessionClosed = errors.New("session closed normally")
	
	Upgrader = websocket.Upgrader{
		HandshakeTimeout: time.Minute,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
		CheckOrigin:      checkWebsocketOrigin,
	}

	wsWriteMutex = &sync.Mutex{}
)

// checkWebsocketOrigin enforces an Origin allow-list for WebSocket upgrades.
//
// Behaviour:
//   - No Origin header (non-browser client): allowed.
//   - "*" present in config.Cfg.Http.AllowedOrigins: any origin allowed (dev only).
//   - Empty allow-list: only same-origin upgrades are accepted (Origin host must
//     match the Host header of the request).
//   - Non-empty allow-list: Origin must match one of the configured entries
//     (compared by scheme://host[:port], case-insensitive).
func checkWebsocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		logger.L().Warn("rejecting websocket upgrade: malformed Origin", zap.String("origin", origin))
		return false
	}
	originNorm := strings.ToLower(parsed.Scheme + "://" + parsed.Host)

	allowed := config.Cfg.Http.AllowedOrigins
	for _, a := range allowed {
		if a == "*" {
			return true
		}
		if strings.EqualFold(strings.TrimRight(a, "/"), originNorm) {
			return true
		}
	}

	if len(allowed) == 0 && strings.EqualFold(parsed.Host, r.Host) {
		return true
	}

	logger.L().Warn("rejecting websocket upgrade: origin not allowed",
		zap.String("origin", origin), zap.String("host", r.Host))
	return false
}

// WriteToMonitors sends data to all monitoring sessions
func WriteToMonitors(monitors *sync.Map, out []byte) {
	monitors.Range(func(k, v any) bool {
		ws, ok := v.(*websocket.Conn)
		if ok && ws != nil {
			ws.WriteMessage(websocket.TextMessage, out)
		}
		return true
	})
}

// CheckTime checks if the current time is within the allowed time range
func CheckTime(data model.AccessAuth) bool {
	now := time.Now()
	in := true
	if (data.Start != nil && now.Before(*data.Start)) || (data.End != nil && now.After(*data.End)) {
		in = false
	}
	if !in {
		return false
	}
	in = false
	has := false
	week, hm := now.Weekday(), now.Format("15:04")
	for _, r := range data.Ranges {
		has = has || len(r.Times) > 0
		if (r.Week+1)%7 == int(week) {
			for _, str := range r.Times {
				ss := strings.Split(str, "~")
				in = in || (len(ss) >= 2 && hm >= ss[0] && hm <= ss[1])
			}
		}
	}
	return !has || in == data.Allow
}

// HandleError handles errors from sessions
func HandleError(ctx *gin.Context, sess *gsession.Session, err error, ws *websocket.Conn, chs *gsession.SessionChans) {
	defer func() {
		defer func() {
			if r := recover(); r != nil {
				logger.L().Error("Recovered from panic in HandleError",
					zap.Any("panic", r),
					zap.String("sessionId", func() string {
						if sess != nil {
							return sess.SessionId
						}
						return ""
					}()))
			}
		}()

		if sess == nil || sess.Chans == nil {
			return
		}
		ch := sess.Chans.AwayChan
		if chs != nil {
			ch = chs.AwayChan
		}

		sess.Once.Do(func() {
			logger.L().Debug("Closing AwayChan from HandleError",
				zap.String("sessionId", sess.SessionId))
			close(ch)
		})
	}()

	if err == nil {
		return
	}
	if sess != nil {
		logger.L().Debug("", zap.String("session_id", sess.SessionId), zap.Error(err))
	}
	ae, ok := err.(*myErrors.ApiError)
	if sess != nil && sess.IsGuacd() && ws != nil {
		ws.WriteMessage(websocket.TextMessage, NewInstruction("error", lo.Ternary(ok, (ae).MessageBase64(ctx), err.Error()), cast.ToString(myErrors.ErrAdminClose)).Bytes())
	} else if sess != nil {
		if ctx.Query("is_monitor") == "true" {
			return
		}
		WriteErrMsg(sess, lo.Ternary(ok, ae.MessageWithCtx(ctx), err.Error()))
	}
}

// WriteErrMsg writes an error message to the session
func WriteErrMsg(sess *gsession.Session, msg string) {
	chs := sess.Chans
	out := []byte(fmt.Sprintf("\r\n \033[31m %s \x1b[0m", msg))
	chs.OutBuf.Write(out)
	Write(sess)
}

// Write writes data to the session output
// skipRecording: If true, it will skip recording to avoid duplicate recordings of manually recorded content
func Write(sess *gsession.Session, skipRecording ...bool) (err error) {
	chs := sess.Chans
	out := chs.OutBuf.Bytes()

	if sess.SessionType == model.SESSIONTYPE_WEB && sess.Ws != nil {
		if len(out) > 0 || sess.IsGuacd() {
			wsWriteMutex.Lock()
			defer wsWriteMutex.Unlock()
			err = sess.Ws.WriteMessage(websocket.TextMessage, out)
		}
	} else if sess.SessionType == model.SESSIONTYPE_CLIENT && len(out) > 0 {
		_, err = sess.CliRw.Write(out)
	}

	// Only write to recording if skipRecording is not specified or explicitly set to false
	shouldSkip := len(skipRecording) > 0 && skipRecording[0]
	if sess.SshRecoder != nil && len(out) > 0 && !sess.IsGuacd() && !shouldSkip {
		sess.SshRecoder.Write(out)
	}

	WriteToMonitors(sess.Monitors, out)
	chs.OutBuf.Reset()

	return
}

// Read reads data from the session input
func Read(sess *gsession.Session) error {
	chs := sess.Chans
	
	// Handle CLIENT type with non-blocking reads
	if sess.SessionType == model.SESSIONTYPE_CLIENT {
		readChan := make(chan []byte)
		errChan := make(chan error)
		
		go func() {
			for {
				p, err := sess.CliRw.Read()
				if err != nil {
					errChan <- err
					return
				}
				readChan <- p
			}
		}()
		
		for {
			select {
			case <-sess.Gctx.Done():
				return nil
			case <-sess.Chans.AwayChan:
				return nil
			case err := <-errChan:
				return err
			case p := <-readChan:
				chs.InChan <- p
				sess.SetIdle()
			}
		}
	}
	
	// Original logic for WEB type
	for {
		select {
		case <-sess.Gctx.Done():
			return nil
		case <-sess.Chans.AwayChan:
			return nil
		default:
			if sess.SessionType == model.SESSIONTYPE_WEB {
				t, msg, err := sess.Ws.ReadMessage()
				if err != nil {
					return err
				}
				if len(msg) <= 0 {
					continue
				}
				switch t {
				case websocket.TextMessage:
					chs.InChan <- msg
					if msg[0] != '9' && ((sess.IsGuacd() && len(msg) > 0) || (!sess.IsGuacd() && IsActive(msg))) {
						sess.SetIdle() // TODO: performance issue
					}
				}
			}
		}
	}
}

// Instruction represents a Guacamole instruction
type Instruction struct {
	opcode string
	args   []string
}

// NewInstruction creates a new Guacamole instruction
func NewInstruction(opcode string, args ...string) *Instruction {
	return &Instruction{
		opcode: opcode,
		args:   args,
	}
}

// Bytes converts the instruction to a byte array
func (instr *Instruction) Bytes() []byte {
	result := instr.opcode
	for _, arg := range instr.args {
		result += "," + fmt.Sprintf("%d", len(arg)) + "." + arg
	}
	result += ";"
	return []byte(result)
}

// IsActive checks if a message indicates user activity
func IsActive(message []byte) bool {
	return len(message) > 0
}

// OfflineSession makes a session offline
func OfflineSession(ctx *gin.Context, sessionId string, closer string) {
	logger.L().Debug("offline", zap.String("session_id", sessionId), zap.String("closer", closer))
	defer gsession.GetOnlineSession().Delete(sessionId)

	// Clean up session-based file client
	fileservice.DefaultFileService.CloseSessionFileClient(sessionId)

	session := gsession.GetOnlineSessionById(sessionId)
	if session == nil {
		return
	}
	if closer != "" && session.Chans != nil {
		select {
		case session.Chans.CloseChan <- closer:
			break
		case <-time.After(time.Second):
			break
		}
	}
	session.Monitors.Range(func(key, value any) bool {
		ws, ok := value.(*websocket.Conn)
		if ok && ws != nil {
			lang := ctx.PostForm("lang")
			accept := ctx.GetHeader("Accept-Language")
			localizer := i18n.NewLocalizer(myi18n.Bundle, lang, accept)
			cfg := &i18n.LocalizeConfig{
				TemplateData:   map[string]any{"sessionId": sessionId},
				DefaultMessage: myi18n.MsgSessionEnd,
			}
			msg, _ := localizer.Localize(cfg)
			ws.WriteMessage(websocket.TextMessage, []byte(msg))
			ws.Close()
		}
		return true
	})
}
