package kafka_handler

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/scram"
	"github.com/segmentio/kafka-go/sasl/plain"
	"one/config"
	"one/goroutines"
)

const (
	defaultDialTimeout   = 30 * time.Second
	defaultWriteTimeout  = 30 * time.Second
	maxRetries          = 3
	retryDelay          = time.Second
)

var dnsServers = []string{
	"8.8.8.8:53",
	"8.8.4.4:53",
	"1.1.1.1:53",
	"208.67.222.222:53",
}

func resolveDNS(host string) ([]string, error) {
	for _, dnsServer := range dnsServers {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: time.Second * 2,
				}
				return d.DialContext(ctx, "udp", dnsServer)
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		addrs, err := resolver.LookupHost(ctx, host)
		cancel()

		if err == nil && len(addrs) > 0 {
			log.Printf("DEBUG: Successfully resolved %s using DNS server %s", host, dnsServer)
			return addrs, nil
		}
		log.Printf("DEBUG: Failed to resolve %s using DNS server %s: %v", host, dnsServer, err)
	}
	return nil, fmt.Errorf("failed to resolve host using all DNS servers")
}

type Message struct {
	Content   string
	Timestamp time.Time
}

type MessageCallback struct {
	Callback func(Message)
	Done     chan struct{}
}

type KafkaHandler struct {
	writer      *kafka.Writer
	config      *config.KafkaConfig
	workerPool  *goroutines.WorkerPool
	messages    []Message
	reader      *kafka.Reader
	msgChan     chan Message
	stopChan    chan struct{}
	offsetFile  string
	maxMessages int
	subscribers map[string]MessageCallback
	subMutex    sync.RWMutex
}

func checkBroker(broker string, timeout time.Duration) bool {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial("tcp", broker)
	if err != nil {
		log.Printf("Blimey! TCP connection's gone pear-shaped to %s: %v", broker, err)
		return false
	}
	defer conn.Close()
	log.Printf("Spot on! Connected to %s", broker)
	return true
}

func determineProtocol(cfg *config.KafkaConfig) string {
	if (!cfg.UseSASL && !cfg.UseSSL) {
		return "PLAINTEXT"
	} else if (cfg.UseSASL && !cfg.UseSSL) {
		return "SASL_PLAINTEXT"
	} else if (!cfg.UseSASL && cfg.UseSSL) {
		return "SSL"
	}
	return "SASL_SSL"
}

func createSASLMechanism(cfg *config.KafkaConfig) (sasl.Mechanism, error) {
	username := strings.TrimSpace(cfg.Username)
	password := strings.TrimSpace(cfg.Password)

	switch strings.ToUpper(cfg.Algorithm) {
	case "PLAIN":
		log.Printf("DEBUG: Creating PLAIN mechanism for user: %s", username)
		return plain.Mechanism{
			Username: username,
			Password: password,
		}, nil
	case "SCRAM-SHA-512":
		log.Printf("DEBUG: Creating SCRAM-SHA-512 mechanism for user: %s", username)
		mechanism, err := scram.Mechanism(scram.SHA512, username, password)
		if err != nil {
			return nil, fmt.Errorf("failed to create SCRAM mechanism: %v", err)
		}
		return mechanism, nil
	default:
		return nil, fmt.Errorf("unsupported SASL algorithm: %s", cfg.Algorithm)
	}
}

func ensureTopicExists(cfg *config.KafkaConfig, dialer *kafka.Dialer, broker string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDialTimeout)
	defer cancel()

	conn, err := dialer.DialContext(ctx, "tcp", broker)
	if (err != nil) {
		return fmt.Errorf("failed to dial broker for topic creation: %w", err)
	}
	defer conn.Close()

	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             cfg.Topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	}
	if err := conn.CreateTopics(topicConfigs...); err != nil {
		if (!strings.Contains(err.Error(), "Topic with this name already exists")) {
			return fmt.Errorf("failed to create topic: %w", err)
		}
	}
	return nil
}

func NewKafkaHandler(cfg *config.KafkaConfig) (*KafkaHandler, error) {
	log.Printf("Right then, firing up Kafka with these brokers: %v, topic: %s", cfg.Brokers, cfg.Topic)

	for i, broker := range cfg.Brokers {
		host, port, err := net.SplitHostPort(broker)
		if err == nil && port == "9091" {
			cfg.Brokers[i] = net.JoinHostPort(host, "29091")
			log.Printf("DEBUG: Broker swapped from %s to %s, mate", broker, cfg.Brokers[i])
		}
	}

	tlsConfig, err := createTLSConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("TLS configuration error: %w", err)
	}

	mechanism, err := createSASLMechanism(cfg)
	if err != nil {
		return nil, fmt.Errorf("SASL mechanism error: %w", err)
	}

	dialer := createKafkaDialer(cfg, tlsConfig, mechanism)

	if err := testConnection(dialer, cfg.Brokers[0]); err != nil {
		return nil, err
	}

	if err := ensureTopicExists(cfg, dialer, cfg.Brokers[0]); err != nil {
		return nil, err
	}

	writer := createKafkaWriter(cfg, dialer)

	handler := &KafkaHandler{
		writer:      writer,
		config:      cfg,
		workerPool:  goroutines.NewWorkerPool(cfg.NumWorkers),
		messages:    make([]Message, 0),
		msgChan:     make(chan Message, 100),
		stopChan:    make(chan struct{}),
		maxMessages: 1000,
		subscribers: make(map[string]MessageCallback),
	}

	if err := handler.startMessageReader(); err != nil {
		return nil, fmt.Errorf("failed to start reader: %w", err)
	}

	return handler, nil
}

func createTLSConfig(cfg *config.KafkaConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         tls.VersionTLS13,
		ServerName:         strings.Split(cfg.Brokers[0], ":")[0],
	}

	if err := loadCertificates(cfg, tlsConfig); err != nil {
		return nil, err
	}

	return tlsConfig, nil
}

func loadCertificates(cfg *config.KafkaConfig, tlsConfig *tls.Config) error {
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return fmt.Errorf("certificate load error: %v", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		log.Printf("DEBUG: Loaded client certificate: %s", cfg.CertFile)
	}

	if cfg.CAFile != "" {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return fmt.Errorf("CA file read error: %v", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
		log.Printf("DEBUG: Loaded CA certificate: %s", cfg.CAFile)
	}

	return nil
}

func createKafkaDialer(cfg *config.KafkaConfig, tlsConfig *tls.Config, mechanism sasl.Mechanism) *kafka.Dialer {
	return &kafka.Dialer{
		Timeout:       defaultDialTimeout,
		DualStack:     false,
		SASLMechanism: mechanism,
		TLS:           tlsConfig,
		ClientID:      cfg.ClientID,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: defaultDialTimeout}
				return d.DialContext(ctx, "udp", "8.8.8.8:53")
			},
		},
	}
}

func testConnection(dialer *kafka.Dialer, broker string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDialTimeout)
	defer cancel()

	conn, err := dialer.DialContext(ctx, "tcp", broker)
	if err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}
	defer conn.Close()
	return nil
}

func createKafkaWriter(cfg *config.KafkaConfig, dialer *kafka.Dialer) *kafka.Writer {
	return kafka.NewWriter(kafka.WriterConfig{
		Brokers:         cfg.Brokers,
		Topic:           cfg.Topic,
		Dialer:          dialer,
		WriteTimeout:    10 * time.Second,
		ReadTimeout:     10 * time.Second,
		MaxAttempts:     3,
		BatchSize:       1,
		BatchTimeout:    time.Second,
		RequiredAcks:    -1,
		Async:           false,
		CompressionCodec: nil,
		Logger: kafka.LoggerFunc(func(msg string, args ...interface{}) {
			log.Printf("KAFKA DEBUG: "+msg, args...)
		}),
		ErrorLogger: kafka.LoggerFunc(func(msg string, args ...interface{}) {
			log.Printf("KAFKA ERROR: "+msg, args...)
		}),
	})
}

func getSASLMechanism(cfg *config.KafkaConfig) (sasl.Mechanism, error) {
	if (!cfg.UseSASL) {
		log.Printf("INFO: SASL is disabled")
		return nil, nil
	}

	username := strings.TrimSpace(cfg.Username)
	password := strings.TrimSpace(cfg.Password)

	log.Printf("DEBUG: Creating SASL mechanism: %s for user: %s", cfg.Algorithm, username)
	log.Printf("DEBUG: Using SASL_SSL protocol with SCRAM-SHA-512")

	mechanism, err := scram.Mechanism(scram.SHA512, username, password)
	if (err != nil) {
		log.Printf("ERROR: Failed to create SCRAM-SHA-512 mechanism: %v", err)
		return nil, fmt.Errorf("SCRAM-SHA-512 initialization error: %v", err)
	}

	return mechanism, nil
}

func (h *KafkaHandler) SendMessage(msg string) error {
	resultChan := make(chan error, 1)
	h.workerPool.ExecuteAsync(func() error {
		var err error
		message := kafka.Message{
			Value: []byte(msg),
			Time:  time.Now(),
			Key:   []byte(fmt.Sprintf("key-%d", time.Now().UnixNano())),
		}
		for attempts := 0; attempts < 3; attempts++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err = h.writer.WriteMessages(ctx, message)
			cancel()
			if err == nil {
				break
			}
			if attempts < 2 {
				time.Sleep(500 * time.Millisecond * time.Duration(attempts+1))
			}
		}
		resultChan <- err
		return err
	})
	if err := <-resultChan; err != nil {
		return fmt.Errorf("send error: %w", err)
	}
	return nil
}

func (h *KafkaHandler) GetMessages() []Message {
	return h.messages
}

func (h *KafkaHandler) Close() {
	close(h.stopChan)
	if h.reader != nil {
		offset := h.reader.Offset()
		if err := saveOffset(h.offsetFile, offset); err != nil {
			log.Printf("Offset save error: %v", err)
		}
		h.reader.Close()
	}
	close(h.msgChan)
	h.workerPool.Shutdown()
	if err := h.writer.Close(); err != nil {
		log.Printf("Writer close error: %v", err)
	}
}

func (h *KafkaHandler) ReadUnconsumedMessages() error {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     h.config.Brokers,
		Topic:       h.config.Topic,
		GroupID:     "",
		StartOffset: kafka.FirstOffset,
		MaxBytes:    10e6,
	})
	defer reader.Close()
	var count int
	for {
		if count >= h.maxMessages {
			break
		}
		ctx := context.Background()
		m, err := reader.ReadMessage(ctx)
		if err != nil {
			break
		}
		count++
		h.messages = append(h.messages, Message{Content: string(m.Value), Timestamp: m.Time})
	}
	return nil
}

func (h *KafkaHandler) startMessageReader() error {
	offset, err := loadOffset("kafka_offset.txt")
	if err != nil {
		offset = kafka.FirstOffset
	}
	earliestOffset, err := getEarliestOffset(h.config.Brokers[0], h.config.Topic)
	if err != nil {
		earliestOffset = kafka.FirstOffset
	}
	if offset < earliestOffset {
		offset = earliestOffset
	}
	tlsConfig, err := h.config.GetTLSConfig()
	if err != nil {
		return fmt.Errorf("tls error: %w", err)
	}
	mechanism, err := createSASLMechanism(h.config)
	if err != nil {
		return fmt.Errorf("sasl error: %w", err)
	}
	dialer := createKafkaDialer(h.config, tlsConfig, mechanism)
	readerConfig := kafka.ReaderConfig{
		Brokers:        h.config.Brokers,
		Topic:          h.config.Topic,
		GroupID:        "web_ui_consumer_group",
		MinBytes:       h.config.Reader.MinBytes,
		MaxBytes:       h.config.Reader.MaxBytes,
		MaxWait:        h.config.Reader.MaxWait,
		Dialer:         dialer,
		CommitInterval: time.Second,
	}
	h.reader = kafka.NewReader(readerConfig)
	h.offsetFile = "kafka_offset.txt"
	go h.readMessages()
	return nil
}

func getEarliestOffset(broker, topic string) (int64, error) {
	conn, err := kafka.Dial("tcp", broker)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	partitions, err := conn.ReadPartitions(topic)
	if err != nil {
		return 0, err
	}
	earliest := int64(1<<63 - 1)
	for _, p := range partitions {
		c2, err := kafka.DialPartition(context.Background(), "tcp", broker, kafka.Partition{Topic: p.Topic, ID: p.ID})
		if err != nil {
			continue
		}
		offset, err := c2.ReadFirstOffset()
		c2.Close()
		if err != nil {
			continue
		}
		if offset < earliest {
			earliest = offset
		}
	}
	return earliest, nil
}

func (h *KafkaHandler) readMessages() {
	var count int
	delay := time.Second
	lastSave := time.Now()
	for {
		select {
		case <-h.stopChan:
			return
		default:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			m, err := h.reader.ReadMessage(ctx)
			cancel()
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					time.Sleep(time.Second)
					continue
				}
				time.Sleep(delay)
				if delay < time.Minute {
					delay *= 2
				}
				continue
			}
			count++
			delay = time.Second
			if err := h.reader.CommitMessages(ctx, m); err != nil {
				if (!errors.Is(err, context.Canceled)) {
					log.Printf("Commit error: %v", err)
				}
			}
			if count%100 == 0 || time.Since(lastSave) > 5*time.Minute {
				if err := saveOffset(h.offsetFile, m.Offset+1); err != nil {
					log.Printf("Save offset error: %v", err)
				}
				lastSave = time.Now()
			}
			msg := Message{Content: string(m.Value), Timestamp: m.Time}
			h.messages = append(h.messages, msg)
			h.notifySubscribers(msg)
		}
	}
}

func (h *KafkaHandler) Subscribe(id string, callback func(Message)) chan struct{} {
	h.subMutex.Lock()
	defer h.subMutex.Unlock()
	done := make(chan struct{})
	h.subscribers[id] = MessageCallback{Callback: callback, Done: done}
	return done
}

func (h *KafkaHandler) Unsubscribe(id string) {
	h.subMutex.Lock()
	defer h.subMutex.Unlock()
	if sub, ok := h.subscribers[id]; ok {
		close(sub.Done)
		delete(h.subscribers, id)
	}
}

func (h *KafkaHandler) notifySubscribers(msg Message) {
	h.subMutex.RLock()
	defer h.subMutex.RUnlock()
	for _, sub := range h.subscribers {
		select {
		case <-sub.Done:
		default:
			sub.Callback(msg)
		}
	}
}

func loadOffset(filename string) (int64, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return kafka.FirstOffset, nil
		}
		return 0, err
	}
	return strconv.ParseInt(string(data), 10, 64)
}

func saveOffset(filename string, offset int64) error {
	return os.WriteFile(filename, []byte(strconv.FormatInt(offset, 10)), 0644)
}
