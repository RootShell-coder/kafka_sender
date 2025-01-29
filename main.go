package main

import (
    "encoding/json"
    "html/template"
    "log"
    "net/http"
    "path/filepath"
    "learning_go/one/kafka_handler"
    "learning_go/one/logger"
    "learning_go/one/config"
)

var (
    kafkaHandler *kafka_handler.KafkaHandler
    templates    *template.Template
)

type PageData struct {
    KafkaMessages []kafka_handler.Message
}

func handleHome(w http.ResponseWriter, r *http.Request) {
    // Ignore favicon.ico requests
    if r.URL.Path == "/favicon.ico" {
        return
    }

    data := PageData{
        KafkaMessages: kafkaHandler.GetMessages(),
    }

    if err := templates.ExecuteTemplate(w, "messages.html", data); err != nil {
        log.Printf("Template rendering error: %v", err)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
        return
    }
}

func handleSendMessage(w http.ResponseWriter, r *http.Request) {
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

    if err := kafkaHandler.SendMessage(message); err != nil {
        log.Printf("Error sending to Kafka: %v", err)
        http.Error(w, "Cannot connect to message broker, please try again later", http.StatusServiceUnavailable)
        return
    }

    w.WriteHeader(http.StatusOK)
}

func main() {
    logger.Init()

    // Template initialization
    var err error
    templates, err = template.ParseGlob(filepath.Join("templates", "*.html"))
    if err != nil {
        log.Fatalf("Error loading templates: %v", err)
    }

    cfg := config.NewDefaultConfig()
    kafkaHandler, err = kafka_handler.NewKafkaHandler(cfg)
    if err != nil {
        log.Fatalf("Error initializing Kafka: %v", err)
    }
    defer kafkaHandler.Close()

    http.HandleFunc("/", logger.Middleware(handleHome))
    http.HandleFunc("/send", logger.Middleware(handleSendMessage))

    log.Println("Server started at http://localhost:8080")
    if err := http.ListenAndServe(":8080", nil); err != nil {
        log.Fatal(err)
    }
}
