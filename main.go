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

type Room struct {
	Code      string    `json:"code"`
	Host      Player    `json:"host"`
	Guest     *Player   `json:"guest,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Room    *Room  `json:"room,omitempty"`
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

func sendJSON(w http.ResponseWriter, status int, data Response) {
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
	if err := json.NewDecoder(r.Body).Decode(&player); err != nil || strings.TrimSpace(player.ID) == "" {
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
		Code:      code,
		Host:      player,
		CreatedAt: time.Now(),
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
	if err := json.NewDecoder(r.Body).Decode(&player); err != nil || strings.TrimSpace(player.ID) == "" {
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
