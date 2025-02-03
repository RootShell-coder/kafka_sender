package logger

import (
    "log"
    "net/http"
    "strings"
    "time"
)

func GetClientIP(r *http.Request) string {
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        ips := strings.Split(xff, ",")
        return strings.TrimSpace(ips[0])
    }
    if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
        return xrip
    }
    ip := r.RemoteAddr
    if strings.Contains(ip, ":") {
        ip = strings.Split(ip, ":")[0]
    }
    return ip
}

func Middleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        clientIP := GetClientIP(r)
        log.Printf("[%s] %s %s", clientIP, r.Method, r.URL.Path)

        next(w, r)

        log.Printf("[%s] %s completed in %v", clientIP, r.URL.Path, time.Since(start))
    }
}

func Init() {
    log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
}
