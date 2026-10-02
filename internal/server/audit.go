package server

import (
	"net"
	"net/http"
	"strings"

	"serverpanel/internal/model"
)

// audit 记一条操作日志。登录失败这类还没有用户的场景，userID 传 0。
func (s *Server) audit(r *http.Request, userID int64, action string, hostID, serverID int64, detail, result string) {
	if userID == 0 {
		if c := claimsOf(r); c != nil {
			userID = c.UserID
		}
	}
	ip := clientIP(r)
	_ = s.st.AddAudit(&model.AuditLog{
		UserID:   userID,
		HostID:   hostID,
		ServerID: serverID,
		Action:   action,
		Detail:   detail,
		Result:   result,
		IP:       ip,
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
