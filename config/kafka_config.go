package config

import (
    "crypto/tls"
    "time"
    "gopkg.in/yaml.v3"
    "os"
)

type Config struct {
    Kafka   KafkaConfig   `yaml:"kafka"`
    Logger  LoggerConfig  `yaml:"logger"`
    Server  ServerConfig  `yaml:"server"`
}

type LoggerConfig struct {
    Level     string `yaml:"level"`
    Format    string `yaml:"format"`
    Timestamp bool   `yaml:"timestamp"`
    Colors    bool   `yaml:"colors"`
}

type ServerConfig struct {
    Host         string        `yaml:"host"`
    Port         int          `yaml:"port"`
    ReadTimeout  time.Duration `yaml:"read_timeout"`
    WriteTimeout time.Duration `yaml:"write_timeout"`
    IdleTimeout  time.Duration `yaml:"idle_timeout"`
}

type KafkaConfig struct {
    // Basic settings
    Brokers      []string      `yaml:"brokers"`
    Topic        string        `yaml:"topic"`
    NumWorkers   int          `yaml:"num_workers"`
    BatchTimeout time.Duration `yaml:"batch_timeout"`
    ClientID     string        `yaml:"client_id"`

    // SSL/SASL settings
    UseSASL    bool   `yaml:"use_sasl"`
    Username   string `yaml:"username"`
    Password   string `yaml:"password"`
    Algorithm  string `yaml:"algorithm"`
    UseSSL     bool   `yaml:"use_ssl"`
    CertFile   string `yaml:"cert_file"`
    KeyFile    string `yaml:"key_file"`
    CAFile     string `yaml:"ca_file"`
    VerifySSL  bool   `yaml:"verify_ssl"`
}

func NewDefaultConfig() *KafkaConfig {
    return &KafkaConfig{
        Brokers:      []string{"localhost:9092"},
        Topic:        "web_requests",
        NumWorkers:   3,
        BatchTimeout: time.Millisecond * 100,
        UseSASL:      true,  // включаем SASL
        UseSSL:       false,
        VerifySSL:    true,
        ClientID:     "web-service",
        Algorithm:    "PLAIN", // используем PLAIN механизм
    }
}

func (c *KafkaConfig) GetTLSConfig() (*tls.Config, error) {
    if (!c.UseSSL) {
        return nil, nil
    }

    tlsConfig := &tls.Config{
        InsecureSkipVerify: !c.VerifySSL,
    }

    if c.CertFile != "" && c.KeyFile != "" {
        cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
        if err != nil {
            return nil, err
        }
        tlsConfig.Certificates = []tls.Certificate{cert}
    }

    return tlsConfig, nil
}

func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }

    var config Config
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, err
    }

    return &config, nil
}
