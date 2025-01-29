# Kafka Message Sender

A web application that allows sending and viewing JSON messages through Apache Kafka.

## Features

- Send JSON messages to Kafka topic
- View sent messages with timestamps
- Real-time JSON validation
- Clean and responsive UI
- Asynchronous message handling with worker pool
- Support for Kafka SASL/SSL authentication

## Prerequisites

- Go 1.16 or higher
- Apache Kafka instance
- Web browser with JavaScript enabled

## Configuration

The application can be configured via `config/config.yaml`:

```yaml
kafka:
  brokers: ["localhost:9092"]
  topic: "web_requests"
  use_sasl: true
  algorithm: "PLAIN"
  # ... other options
```

## Running the Application

1. Start your Kafka broker
2. Run the application:
   ```bash
   go run main.go
   ```
3. Open http://localhost:8080 in your browser

## Usage Examples

### Sending Messages

1. Open the web interface
2. Enter a valid JSON message in the input field:
   ```json
   {"user": "john", "action": "login", "timestamp": 1634567890}
   ```
3. Click "Send" button

### Valid JSON Examples

```json
{"key": "value"}
{"name": "John", "age": 30}
{"items": ["apple", "banana"], "total": 2}
```

### Response Handling

- Success: Message will appear in the list below the input form
- Error: Input field will show red border with error message

## Architecture

- **Frontend**: HTML/CSS/JavaScript with real-time JSON validation
- **Backend**: Go web server with Kafka integration
- **Message Processing**: Asynchronous worker pool for Kafka message handling
- **Security**: Support for SASL/SSL Kafka authentication

## Error Handling

The application handles various error scenarios:
- Invalid JSON format
- Kafka connection issues
- Network timeouts
- Server errors

## Security

- SASL authentication support
- SSL/TLS encryption support
- Input validation to prevent invalid data
