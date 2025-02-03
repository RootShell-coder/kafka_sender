package config

import (
	"crypto/tls"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	Kafka  KafkaConfig  `yaml:"kafka"`
	Logger LoggerConfig `yaml:"logger"`
	Server ServerConfig `yaml:"server"`
}

type KafkaConfig struct {
	Brokers      []string      `yaml:"brokers"`
	Topic        string        `yaml:"topic"`
	NumWorkers   int           `yaml:"num_workers"`
	BatchTimeout time.Duration `yaml:"batch_timeout"`
	ClientID     string        `yaml:"client_id"`
	UseSASL      bool          `yaml:"use_sasl"`
	Username     string        `yaml:"username"`
	Password     string        `yaml:"password"`
	Algorithm    string        `yaml:"algorithm"`
	UseSSL       bool          `yaml:"use_ssl"`
	CertFile     string        `yaml:"cert_file"`
	KeyFile      string        `yaml:"key_file"`
	CAFile       string        `yaml:"ca_file"`
	VerifySSL    bool          `yaml:"verify_ssl"`
	Reader       struct {
		GroupID  string        `yaml:"group_id"`
		MaxWait  time.Duration `yaml:"max_wait"`
		MinBytes int           `yaml:"min_bytes"`
		MaxBytes int           `yaml:"max_bytes"`
	} `yaml:"reader"`
}

type LoggerConfig struct {
	Level     string `yaml:"level"`
	Format    string `yaml:"format"`
	Timestamp bool   `yaml:"timestamp"`
	Colors    bool   `yaml:"colors"`
}

type ServerConfig struct {
	Host         string        `yaml:"host"`
	Port         int           `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
}

func NewDefaultConfig() *KafkaConfig {
	return &KafkaConfig{
		Brokers:      []string{"sys.polmira.ru:29090"},
		Topic:        "logging.illuminati.kl",
		NumWorkers:   1,
		BatchTimeout: time.Second,
		ClientID:     "kafka_client_logger_1",
		UseSASL:      true,
		UseSSL:       true,
		VerifySSL:    false,
		Algorithm:    "SCRAM-SHA-512",
	}
}

func (c *KafkaConfig) GetTLSConfig() (*tls.Config, error) {
	if !c.UseSSL {
		return nil, nil
	}
	tlsConfig := &tls.Config{InsecureSkipVerify: !c.VerifySSL}
	if c.CertFile != "" && c.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

func LoadConfig(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config error: %w", err)
	}
	var config AppConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config error: %w", err)
	}
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &config, nil
}

func validateConfig(cfg *AppConfig) error {
	if len(cfg.Kafka.Brokers) == 0 {
		return fmt.Errorf("no brokers specified")
	}
	if cfg.Kafka.Topic == "" {
		return fmt.Errorf("topic not specified")
	}
	return nil
}
