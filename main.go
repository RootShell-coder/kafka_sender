package main

import (
    "encoding/json"
    "fmt"
    "html/template"
    "log"
    "net/http"
    "one/kafka_handler"
    "one/logger"
    "one/config"
    "github.com/gorilla/websocket"
    "time"
)

var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

type Application struct {
    kafkaHandler *kafka_handler.KafkaHandler
    templates    *template.Template
    config       *config.AppConfig
}

func NewApplication(configPath string) (*Application, error) {
    cfg, err := config.LoadConfig(configPath)
    if err != nil {
        return nil, fmt.Errorf("configuration error: %w", err)
    }

    tmpl, err := template.ParseGlob("templates/*.html")
    if err != nil {
        return nil, fmt.Errorf("template error: %w", err)
    }

    handler, err := kafka_handler.NewKafkaHandler(&cfg.Kafka)
    if err != nil {
        return nil, fmt.Errorf("kafka handler error: %w", err)
    }

    return &Application{
        kafkaHandler: handler,
        templates:    tmpl,
        config:       cfg,
    }, nil
}

func (app *Application) Start() error {
    http.HandleFunc("/", logger.Middleware(app.handleHome))
    http.HandleFunc("/send", logger.Middleware(app.handleSendMessage))
    http.HandleFunc("/ws", app.handleWebSocket)

    addr := fmt.Sprintf("%s:%d", app.config.Server.Host, app.config.Server.Port)
    log.Printf("Server running on %s", addr)

    return http.ListenAndServe(addr, nil)
}

type PageData struct {
    KafkaMessages []kafka_handler.Message
}

func (app *Application) handleHome(w http.ResponseWriter, r *http.Request) {
    // Fetching and flipping the messages so the latest ones pop on top, innit
    messages := app.kafkaHandler.GetMessages()
    reversed := make([]kafka_handler.Message, len(messages))
    for i, msg := range messages {
        reversed[len(messages)-1-i] = msg
    }

    data := PageData{
        KafkaMessages: reversed,
    }

    if err := app.templates.ExecuteTemplate(w, "messages.html", data); err != nil {
        log.Printf("Template error: %v", err)
        http.Error(w, "Server error", http.StatusInternalServerError)
        return
    }
}

func (app *Application) handleSendMessage(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    message := r.FormValue("message")
    if message == "" {
        http.Error(w, "Empty message", http.StatusBadRequest)
        return
    }

    // Validate JSON
    var js map[string]interface{}
    if err := json.Unmarshal([]byte(message), &js); err != nil {
        http.Error(w, "Invalid JSON format", http.StatusBadRequest)
        return
    }

    if err := app.kafkaHandler.SendMessage(message); err != nil {
        log.Printf("Kafka error: %v", err)
        http.Error(w, "Broker error, try later", http.StatusServiceUnavailable)
        return
    }

    w.WriteHeader(http.StatusOK)
}

func (app *Application) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("WebSocket upgrade error: %v", err)
        return
    }

    id := fmt.Sprintf("ws-%d", time.Now().UnixNano())
    done := app.kafkaHandler.Subscribe(id, func(msg kafka_handler.Message) {
        if err := conn.WriteJSON(msg); err != nil {
            log.Printf("WebSocket send error: %v", err)
            app.kafkaHandler.Unsubscribe(id)
            conn.Close()
        }
    })

    // Wait for connection closure
    go func() {
        for {
            if _, _, err := conn.ReadMessage(); err != nil {
                app.kafkaHandler.Unsubscribe(id)
                conn.Close()
                return
            }
        }
    }()

    // Wait for termination signal
    <-done
}

func main() {
    logger.Init()

    app, err := NewApplication("config/config.yml") // swapped the path to config/config.yml, mate
    if err != nil {
        log.Fatalf("Initialization failed: %v", err)
    }
    defer app.kafkaHandler.Close()

    if err := app.Start(); err != nil {
        log.Fatalf("Server error: %v", err)
    }
}
