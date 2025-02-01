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
    "one/config"
    "one/goroutines"
    "crypto/tls"
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
    log.Printf("INFO: Initializing Kafka handler with brokers: %v, topic: %s", cfg.Brokers, cfg.Topic)
    log.Printf("INFO: SASL config - enabled: %v, algorithm: %s, username: %s",
        cfg.UseSASL, cfg.Algorithm, cfg.Username)

    // Настраиваем TLS
    tlsConfig := &tls.Config{
        InsecureSkipVerify: true,  // Принудительно отключаем проверку сертификата
    }

    // Настраиваем SASL механизм
    mechanism, err := getSASLMechanism(cfg)
    if err != nil {
        log.Printf("ERROR: Failed to create SASL mechanism: %v", err)
        return nil, fmt.Errorf("SASL mechanism creation failed: %v", err)
    }

    dialer := &kafka.Dialer{
        Timeout:       30 * time.Second,
        DualStack:     true,
        SASLMechanism: mechanism,
        TLS:          tlsConfig,
    }

    // Тестовое подключение с подробным логированием
    log.Printf("INFO: Testing connection to brokers...")
    for _, broker := range cfg.Brokers {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        log.Printf("INFO: Attempting to connect to broker: %s with SASL mechanism: %T",
            broker, mechanism)

        conn, err := dialer.DialContext(ctx, "tcp", broker)
        if err != nil {
            cancel()
            log.Printf("ERROR: Connection failed to %s: %v", broker, err)
            continue
        }

        log.Printf("INFO: Successfully connected to broker: %s", broker)
        conn.Close()
        cancel()
    }

    // Настраиваем Writer
    writer := &kafka.Writer{
        Addr:         kafka.TCP(cfg.Brokers...),
        Topic:        cfg.Topic,
        Balancer:     &kafka.LeastBytes{},
        BatchTimeout: cfg.BatchTimeout,
        BatchSize:    1,
        Transport: &kafka.Transport{
            TLS:         tlsConfig,
            SASL:        mechanism,
            DialTimeout: 30 * time.Second,
        },
        Logger: kafka.LoggerFunc(func(msg string, args ...interface{}) {
            log.Printf("KAFKA: "+msg, args...)
        }),
        ErrorLogger: kafka.LoggerFunc(func(msg string, args ...interface{}) {
            log.Printf("KAFKA ERROR: "+msg, args...)
        }),
    }

    handler := &KafkaHandler{
        writer:     writer,
        config:     cfg,
        workerPool: goroutines.NewWorkerPool(cfg.NumWorkers),
        messages:   make([]Message, 0),
    }

    log.Printf("DEBUG: Kafka handler initialized successfully")
    return handler, nil
}

func getSASLMechanism(cfg *config.KafkaConfig) (sasl.Mechanism, error) {
    log.Printf("DEBUG: Creating SASL mechanism: %s", cfg.Algorithm)

    if (!cfg.UseSASL) {
        log.Printf("INFO: SASL is disabled")
        return nil, nil
    }

    switch cfg.Algorithm {
    case "PLAIN":
        log.Printf("DEBUG: Using PLAIN mechanism with username: %s", cfg.Username)
        return plain.Mechanism{
            Username: cfg.Username,
            Password: cfg.Password,
        }, nil
    case "SCRAM-SHA-512":
        log.Printf("DEBUG: Using SCRAM-SHA-512 mechanism with username: %s", cfg.Username)
        return scram.Mechanism(scram.SHA512, cfg.Username, cfg.Password)
    case "SCRAM-SHA-256":
        return scram.Mechanism(scram.SHA256, cfg.Username, cfg.Password)
    default:
        return nil, fmt.Errorf("unsupported SASL mechanism: %s", cfg.Algorithm)
    }
}

func (h *KafkaHandler) SendMessage(msg string) error {
    log.Printf("INFO: Attempting to send message to topic %s", h.config.Topic)
    log.Printf("DEBUG: Starting to send message: %s", msg)
    resultChan := make(chan error, 1)

    h.workerPool.ExecuteAsync(func() error {
        var err error
        for attempts := 0; attempts < 3; attempts++ {
            log.Printf("DEBUG: Attempt %d to send message", attempts+1)
            ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)

            start := time.Now()
            err = h.writer.WriteMessages(ctx,
                kafka.Message{
                    Value: []byte(msg),
                    Time:  time.Now(),
                },
            )
            duration := time.Since(start)
            log.Printf("DEBUG: Write attempt %d took %v", attempts+1, duration)

            cancel()

            if err == nil {
                log.Printf("DEBUG: Message sent successfully on attempt %d", attempts+1)
                break
            }

            log.Printf("DEBUG: Attempt %d failed: %v (error type: %T)", attempts+1, err, err)
            if attempts < 2 {
                delay := time.Second * time.Duration(attempts+1)
                log.Printf("DEBUG: Waiting %v before next attempt", delay)
                time.Sleep(delay)
            }
        }
        resultChan <- err
        return err
    })

    // Wait for result
    if err := <-resultChan; err != nil {
        log.Printf("ERROR: Failed to send message to Kafka[%s]: %v (error type: %T)",
            h.config.Topic, err, err)
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
