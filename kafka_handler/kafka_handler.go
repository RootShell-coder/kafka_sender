package kafka_handler

import (
    "context"
    "fmt"
    "log"
    "time"
    "github.com/segmentio/kafka-go"
    "github.com/segmentio/kafka-go/sasl"
    "github.com/segmentio/kafka-go/sasl/plain"
    "github.com/segmentio/kafka-go/sasl/scram"
    "learning_go/one/config"
    "learning_go/one/goroutines"
)

type Message struct {
    Content   string
    Timestamp time.Time
}

type KafkaHandler struct {
    writer     *kafka.Writer
    config     *config.KafkaConfig
    workerPool *goroutines.WorkerPool
    messages   []Message // store messages locally
}

func NewKafkaHandler(cfg *config.KafkaConfig) (*KafkaHandler, error) {
    dialer := &kafka.Dialer{
        Timeout:   10 * time.Second,
        ClientID:  cfg.ClientID,
    }

    if cfg.UseSSL {
        tlsConfig, err := cfg.GetTLSConfig()
        if err != nil {
            return nil, err
        }
        dialer.TLS = tlsConfig
    }

    if cfg.UseSASL {
        mechanism, err := getSASLMechanism(cfg)
        if err != nil {
            return nil, err
        }
        dialer.SASLMechanism = mechanism
    }

    writer := &kafka.Writer{
        Addr:         kafka.TCP(cfg.Brokers...),
        Topic:        cfg.Topic,
        BatchTimeout: cfg.BatchTimeout,
        RequiredAcks: kafka.RequireOne,
        Async:        false, // Change to synchronous mode
        Transport: &kafka.Transport{
            SASL: dialer.SASLMechanism,
            TLS:  dialer.TLS,
        },
    }

    handler := &KafkaHandler{
        writer:     writer,
        config:     cfg,
        workerPool: goroutines.NewWorkerPool(cfg.NumWorkers),
        messages:   make([]Message, 0),
    }

    return handler, nil
}

func getSASLMechanism(cfg *config.KafkaConfig) (sasl.Mechanism, error) {
    switch cfg.Algorithm {
    case "PLAIN":
        // For PLAIN without authentication use empty values
        return plain.Mechanism{
            Username: "",  // empty login
            Password: "",  // empty password
        }, nil
    case "SCRAM-SHA-256":
        return scram.Mechanism(scram.SHA256, cfg.Username, cfg.Password)
    case "SCRAM-SHA-512":
        return scram.Mechanism(scram.SHA512, cfg.Username, cfg.Password)
    default:
        return nil, fmt.Errorf("unsupported SASL mechanism: %s", cfg.Algorithm)
    }
}

func (h *KafkaHandler) SendMessage(msg string) error {
    // Create channel for result
    resultChan := make(chan error, 1)

    // Send task to pool
    h.workerPool.ExecuteAsync(func() error {
        ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
        defer cancel()

        err := h.writer.WriteMessages(ctx,
            kafka.Message{
                Value: []byte(msg),
                Time:  time.Now(),
            },
        )
        resultChan <- err
        return err
    })

    // Wait for result
    if err := <-resultChan; err != nil {
        log.Printf("Error sending message to Kafka[%s]: %v", h.config.Topic, err)
        return err
    }

    // Add message to local slice only after successful send
    h.messages = append(h.messages, Message{
        Content:   msg,
        Timestamp: time.Now(),
    })

    return nil
}

func (h *KafkaHandler) GetMessages() []Message {
    return h.messages
}

func (h *KafkaHandler) Close() {
    h.workerPool.Shutdown()
    if err := h.writer.Close(); err != nil {
        log.Printf("Error closing Kafka writer: %v", err)
    }
}
