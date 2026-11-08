package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type Player struct {
	ID string `json:"id"`
}

type Selection struct {
	PlayerID     string    `json:"player_id"`
	Character    string    `json:"character"`
	CharacterDir string    `json:"character_dir"`
	Palette      int       `json:"palette"`
	CursorX      int       `json:"cursor_x"`
	CursorY      int       `json:"cursor_y"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Room struct {
	Code       string               `json:"code"`
	Host       Player               `json:"host"`
	Guest      *Player              `json:"guest,omitempty"`
	CreatedAt  time.Time            `json:"created_at"`
	Selections map[string]Selection `json:"-"`
}

type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Room    *Room  `json:"room,omitempty"`
}

type SelectionResponse struct {
	Success   bool       `json:"success"`
	Message   string     `json:"message"`
	Selection *Selection `json:"selection,omitempty"`
}

var (
	rooms = make(map[string]*Room)
	mu    sync.Mutex
)

const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCode() string {
	code := make([]byte, 6)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			panic(err)
		}
		code[i] = alphabet[n.Int64()]
	}
	return string(code)
}

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func createRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendJSON(w, 405, Response{Message: "Method not allowed"})
		return
	}

	var player Player
	if err := json.NewDecoder(r.Body).Decode(&player); err != nil ||
		strings.TrimSpace(player.ID) == "" {
		sendJSON(w, 400, Response{Message: "Valid player ID required"})
		return
	}

	mu.Lock()
	defer mu.Unlock()

	var code string
	for {
		code = newCode()
		if rooms[code] == nil {
			break
		}
	}

	room := &Room{
		Code:       code,
		Host:       player,
		CreatedAt:  time.Now(),
		Selections: make(map[string]Selection),
	}
	rooms[code] = room

	sendJSON(w, 201, Response{
		Success: true,
		Message: "Room created successfully",
		Room:    room,
	})
}

func roomHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/rooms/"), "/")
	parts := strings.Split(path, "/")
	code := strings.ToUpper(parts[0])

	if len(parts) == 2 && parts[1] == "join" {
		joinRoom(w, r, code)
		return
	}

	if len(parts) == 2 && parts[1] == "select" {
		selectionHandler(w, r, code)
		return
	}

	if len(parts) == 1 && code != "" {
		getRoom(w, r, code)
		return
	}

	sendJSON(w, 404, Response{Message: "Endpoint not found"})
}

func joinRoom(w http.ResponseWriter, r *http.Request, code string) {
	if r.Method != http.MethodPost {
		sendJSON(w, 405, Response{Message: "Method not allowed"})
		return
	}

	var player Player
	if err := json.NewDecoder(r.Body).Decode(&player); err != nil ||
		strings.TrimSpace(player.ID) == "" {
		sendJSON(w, 400, Response{Message: "Valid player ID required"})
		return
	}

	mu.Lock()
	defer mu.Unlock()

	room := rooms[code]
	if room == nil {
		sendJSON(w, 404, Response{Message: "Room not found"})
		return
	}
	if room.Host.ID == player.ID {
		sendJSON(w, 400, Response{Message: "Host cannot join own room"})
		return
	}
	if room.Guest != nil {
		sendJSON(w, 409, Response{Message: "Room is full"})
		return
	}

	room.Guest = &player
	sendJSON(w, 200, Response{
		Success: true,
		Message: "Joined room successfully",
		Room:    room,
	})
}

func isRoomPlayer(room *Room, id string) bool {
	if room.Host.ID == id {
		return true
	}
	return room.Guest != nil && room.Guest.ID == id
}

func selectionHandler(w http.ResponseWriter, r *http.Request, code string) {
	switch r.Method {
	case http.MethodPost:
		var selection Selection

		if err := json.NewDecoder(r.Body).Decode(&selection); err != nil {
			sendJSON(w, 400, SelectionResponse{Message: "Invalid JSON"})
			return
		}

		selection.PlayerID = strings.TrimSpace(selection.PlayerID)
		selection.Character = strings.TrimSpace(selection.Character)

		if selection.PlayerID == "" || selection.Character == "" {
			sendJSON(w, 400, SelectionResponse{
				Message: "Player ID and character are required",
			})
			return
		}

		mu.Lock()
		defer mu.Unlock()

		room := rooms[code]
		if room == nil {
			sendJSON(w, 404, SelectionResponse{Message: "Room not found"})
			return
		}

		if !isRoomPlayer(room, selection.PlayerID) {
			sendJSON(w, 403, SelectionResponse{Message: "Player is not in this room"})
			return
		}

		selection.UpdatedAt = time.Now()
		room.Selections[selection.PlayerID] = selection

		sendJSON(w, 200, SelectionResponse{
			Success:   true,
			Message:   "Selection updated",
			Selection: &selection,
		})

	case http.MethodGet:
		playerID := strings.TrimSpace(r.URL.Query().Get("player_id"))
		if playerID == "" {
			sendJSON(w, 400, SelectionResponse{Message: "Player ID required"})
			return
		}

		mu.Lock()
		defer mu.Unlock()

		room := rooms[code]
		if room == nil {
			sendJSON(w, 404, SelectionResponse{Message: "Room not found"})
			return
		}

		if !isRoomPlayer(room, playerID) {
			sendJSON(w, 403, SelectionResponse{Message: "Player is not in this room"})
			return
		}

		var opponentID string
		if playerID == room.Host.ID {
			if room.Guest != nil {
				opponentID = room.Guest.ID
			}
		} else {
			opponentID = room.Host.ID
		}

		opponentSelection, exists := room.Selections[opponentID]
		if !exists {
			sendJSON(w, 200, SelectionResponse{
				Success: true,
				Message: "Waiting for opponent selection",
			})
			return
		}

		sendJSON(w, 200, SelectionResponse{
			Success:   true,
			Message:   "Opponent selection received",
			Selection: &opponentSelection,
		})

	default:
		sendJSON(w, 405, SelectionResponse{Message: "Method not allowed"})
	}
}

func getRoom(w http.ResponseWriter, r *http.Request, code string) {
	if r.Method != http.MethodGet {
		sendJSON(w, 405, Response{Message: "Method not allowed"})
		return
	}

	mu.Lock()
	defer mu.Unlock()

	room := rooms[code]
	if room == nil {
		sendJSON(w, 404, Response{Message: "Room not found"})
		return
	}

	sendJSON(w, 200, Response{
		Success: true,
		Message: "Room found",
		Room:    room,
	})
}

func main() {
	http.HandleFunc("/api/rooms", createRoom)
	http.HandleFunc("/api/rooms/", roomHandler)

	fmt.Println("JFE Room Server running on :8080")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			fmt.Fprintln(w, "JFE Room Server is online")
			return
		}
		http.NotFound(w, r)
	})

	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, nil))
}
