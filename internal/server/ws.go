package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"serverpanel/internal/model"
)

type Hub struct {
	mu      sync.Mutex
	clients map[*wsClient]struct{}
}

func newHub() *Hub {
	return &Hub{clients: map[*wsClient]struct{}{}}
}

type wsClient struct {
	conn  *websocket.Conn
	user  int64
	admin bool

	mu        sync.Mutex
	allowed   map[int64]bool
	allowedAt time.Time
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (h *Hub) serveWS(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := s.parseToken(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "请先登录")
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := &wsClient{conn: conn, user: claims.UserID, admin: claims.Role == model.RoleAdmin}

		h.mu.Lock()
		h.clients[c] = struct{}{}
		h.mu.Unlock()

		for _, m := range s.snapshot() {
			if c.canSee(s, m.HostID) {
				c.write(wsMessage{Type: "metric", Data: m})
			}
		}

		go func() {
			defer func() {
				h.mu.Lock()
				delete(h.clients, c)
				h.mu.Unlock()
				conn.Close()
			}()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
	}
}

// broadcast 把某台机器的新指标发给能看到它的客户端。
func (h *Hub) broadcast(s *Server, m *LiveMetric) {
	h.mu.Lock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	for _, c := range clients {
		if !c.canSee(s, m.HostID) {
			continue
		}
		c.write(wsMessage{Type: "metric", Data: m})
	}
}

func (c *wsClient) write(v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.WriteJSON(v)
}

func (c *wsClient) canSee(s *Server, hostID int64) bool {
	if c.admin {
		return true
	}
	if time.Since(c.allowedAt) > 10*time.Second || c.allowed == nil {
		hosts, err := s.st.HostsForUser(c.user)
		if err == nil {
			set := make(map[int64]bool, len(hosts))
			for _, h := range hosts {
				set[h.ID] = true
			}
			c.allowed = set
			c.allowedAt = time.Now()
		}
	}
	return c.allowed[hostID]
}
