package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/exp/slog"

	"github.com/gorilla/handlers"
	"github.com/gorilla/websocket"
)

//go:embed Resource/web/index.html
var QueHtmlFile []byte

//go:embed Resource/web/default.css
var cssFile []byte

//go:embed Resource/web/DmDisplay.html
var DmDisplayHtml []byte

//go:embed Resource/web/js/NoSleep.min.js
var NoSleepJs []byte

var (
	queueLock sync.Mutex
	dmLock    sync.Mutex
)

var (
	QueueChatChan = make(chan []byte, 50)
	DmChatChan    = make(chan []byte, 50)
	upgrader      = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	QueueConnMap = make(map[*websocket.Conn]bool)
	DmConnMap    = make(map[*websocket.Conn]bool)
)

func StartWebServer() {
	_, _ = http.Get("http://127.0.0.1:100/EXIT")

	handler := handlers.CORS(
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowCredentials(),
	)(WebServer())
	err := http.ListenAndServe(":100", handler)
	if err != nil {
		slog.Error(err.Error())
		return
	}
}

func WebServer() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/LineWs", func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			slog.Error(err.Error())
			return
		}

		QueueConnMap[conn] = true

		err = conn.WriteMessage(websocket.TextMessage, []byte("Connected"))
		if err != nil {
			delete(QueueConnMap, conn)
			return
		}

		defer func(conn *websocket.Conn) {
			err := conn.Close()
			if err != nil {
				slog.Error("Failed to close connection:", err)
				return
			}
		}(conn)

		go func() {
			for {
				_, Message, err := conn.ReadMessage()
				if err != nil {
					return
				}
				switch string(Message) {
				case "ping":
					err := conn.WriteMessage(websocket.TextMessage, []byte("pong"))
					if err != nil {
						return
					}
				}
			}
		}()

		for {
			Chat := <-QueueChatChan
			ConnMapCopy := QueueConnMap
			for w := range ConnMapCopy {
				err = w.WriteMessage(websocket.TextMessage, Chat)
				if err != nil {
					slog.Error("Failed to write message:", err)
					delete(QueueConnMap, w)
				}
			}
		}
	})

	mux.HandleFunc("/DmWs", func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			slog.Error("Websocket Upgrade Err:", err.Error())
			return
		}
		DmConnMap[conn] = true

		err = conn.WriteMessage(websocket.TextMessage, []byte("Connected"))
		if err != nil {
			slog.Error("Websocket Write Err:", err.Error())
			delete(DmConnMap, conn)
			return
		}

		defer func(conn *websocket.Conn) {
			err := conn.Close()
			if err != nil {
				slog.Error("Failed to close connection:", err)
				return
			}
		}(conn)

		go func() {
			for {
				_, Message, err := conn.ReadMessage()
				if err != nil {
					return
				}
				switch string(Message) {
				case "ping":
					err := conn.WriteMessage(websocket.TextMessage, []byte("pong"))
					if err != nil {
						return
					}
				}
			}
		}()

		for {
			Chat := <-DmChatChan
			ConnMapCopy := DmConnMap
			for w := range ConnMapCopy {
				err = w.WriteMessage(websocket.TextMessage, Chat)
				if err != nil {
					slog.Error("Failed to write message:", err)
					delete(DmConnMap, w)
				}
			}
		}
	})

	mux.Handle("/Resource/", http.StripPrefix("/Resource/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Ext(r.URL.Path) == ".png" {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400") // 缓存1天
		}
		http.FileServer(http.Dir("Resource")).ServeHTTP(w, r)
	})))

	// 静态资源处理（修复字体文件404问题）
	mux.Handle("/web/", http.StripPrefix("/web/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 特殊处理字体文件
		if filepath.Ext(r.URL.Path) == ".ttf" {
			w.Header().Set("Content-Type", "font/ttf")
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		http.FileServer(http.Dir("Resource/web")).ServeHTTP(w, r)
	})))

	mux.HandleFunc("/web", func(writer http.ResponseWriter, request *http.Request) {
		_, err := writer.Write(QueHtmlFile)
		if err != nil {
			return
		}
	})

	mux.HandleFunc("/dm", func(writer http.ResponseWriter, request *http.Request) {
		_, err := writer.Write(DmDisplayHtml)
		if err != nil {
			return
		}
	})

	mux.HandleFunc("/default.css", func(writer http.ResponseWriter, request *http.Request) {
		var found bool
		dir, err := os.ReadDir("./")
		if err != nil {
			return
		}
		for _, file := range dir {
			if strings.HasSuffix(file.Name(), ".css") {
				found = true
				readFile, err := os.ReadFile(file.Name())
				if err != nil {
					return
				}
				_, err = writer.Write(readFile)
			}
		}

		if !found {
			_, err := writer.Write(cssFile)
			if err != nil {
				return
			}
		}
	})

	mux.HandleFunc("/NoSleep.min.js", func(writer http.ResponseWriter, request *http.Request) {
		_, err := writer.Write(NoSleepJs)
		if err != nil {
			return
		}
	})

	// 静态同步接口
	mux.HandleFunc("/getAllLine", func(writer http.ResponseWriter, request *http.Request) {
		lineJson, err := json.Marshal(line)
		if err != nil {
			return
		}
		_, err = writer.Write(lineJson)
		if err != nil {
			return
		}
	})

	mux.HandleFunc("/getLineLength", func(writer http.ResponseWriter, request *http.Request) {
		LineLength := len(line.GuardLine) + len(line.GiftLine) + len(line.CommonLine)
		_, err := writer.Write([]byte(strconv.Itoa(LineLength)))
		if err != nil {
			return
		}
	})

	mux.HandleFunc("/getConfig", func(writer http.ResponseWriter, request *http.Request) {
		ConfigJsonByte, err := json.Marshal(globalConfiguration)
		if err != nil {
			return
		}
		_, err = writer.Write(ConfigJsonByte)
		if err != nil {
			return
		}
	})

	mux.HandleFunc("/EXIT", func(writer http.ResponseWriter, request *http.Request) {
		// 添加权限验证
		if request.RemoteAddr != "127.0.0.1" {
			http.Error(writer, "Forbidden", http.StatusForbidden)
			return
		}

		writer.Write([]byte("Closing..."))
	})

	return mux
}
