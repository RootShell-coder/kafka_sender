package main

import (
	"context"
	"log"

	"github.com/segmentio/kafka-go"
)

type KafkaHandler struct {
	writer *kafka.Writer
}

func NewKafkaHandler() *KafkaHandler {

	writer := kafka.NewWriter(kafka.WriterConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   "web_requests",
	})

	return &KafkaHandler{
		writer: writer,
	}
}

func (h *KafkaHandler) SendMessage(msg string) error {
	err := h.writer.WriteMessages(context.Background(),
		kafka.Message{
			Value: []byte(msg),
		},
	)
	if err != nil {
		log.Printf("Error sending message to Kafka: %v", err)
		return err
	}
	return nil
}

func (h *KafkaHandler) Close() {
	h.writer.Close()
}
